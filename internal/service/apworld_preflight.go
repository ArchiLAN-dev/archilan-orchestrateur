package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"archilan.fr/orchestrateur/internal/storage"
)

// Apworld preflight statuses (story 9.38). "skipped" means the check could not run
// because no template YAML exists (template generation already failed at upload);
// the UI must show it as "unknown", not as "passed".
const (
	PreflightStatusPending = "pending"
	PreflightStatusPassed  = "passed"
	PreflightStatusFailed  = "failed"
	PreflightStatusSkipped = "skipped"
)

// preflightErrorExcerptMax bounds the stored stderr excerpt. The Python traceback sits at
// the end of stderr, so keeping the tail keeps the actionable part while the meta sidecar
// stays small.
const preflightErrorExcerptMax = 2000

// preflightErrorLineMax bounds a single line before the tail is taken. One generator line
// can be enormous - a fill error lists every (location, item) pair - and a plain tail cut
// then lands inside that line, throwing away the traceback AND the head of the message,
// which is exactly where the actionable text sits (story 9.43). Capping per line first
// keeps the structure and the head, and only sacrifices the middle of the dump.
const preflightErrorLineMax = 300

// preflightErrorExcerpt caps every line, then returns the last maxLen characters.
// Rune-based throughout so a cut never produces invalid UTF-8.
//
// The machine-readable lines (###ARCHILAN-FAILURE### / ###ARCHILAN-WARNING###) are never cut and always
// kept, at the end: the API decodes their JSON, and a DKC3 failure line naming its required file is
// already longer than a capped line (story 44.3). The image bounds them itself.
func preflightErrorExcerpt(errText string, maxLen int) string {
	var plain, sentinels []string
	for _, line := range strings.Split(errText, "\n") {
		if isSentinelLine(line) {
			sentinels = append(sentinels, line)
			continue
		}
		runes := []rune(line)
		if len(runes) > preflightErrorLineMax {
			line = string(runes[:preflightErrorLineMax]) + "…"
		}
		plain = append(plain, line)
	}
	capped := strings.Join(plain, "\n")
	tail := strings.Join(sentinels, "\n")

	if maxLen > 0 {
		budget := maxLen
		if tail != "" {
			budget -= len([]rune(tail)) + 1
		}
		if budget < 0 {
			budget = 0
		}
		if runes := []rune(capped); len(runes) > budget {
			capped = string(runes[len(runes)-budget:])
		}
	}
	switch {
	case tail == "":
		return capped
	case capped == "":
		return tail
	default:
		return capped + "\n" + tail
	}
}

func isSentinelLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "###ARCHILAN-FAILURE###") || strings.HasPrefix(trimmed, "###ARCHILAN-WARNING###")
}

// RunApworldPreflight executes the solo test generation for an uploaded apworld and
// persists the verdict in its meta sidecar (story 9.38). Used by the post-upload hook and
// the on-demand re-check endpoint. The returned meta carries the fresh verdict.
//
// Failure semantics (AC6): only a completed check writes passed/failed/skipped. An
// infrastructure error (docker, storage) is returned to the caller and leaves the stored
// status untouched (pending/previous value) so the verdict never lies.
//
// requiredFiles are the files an admin provided for this run only (story 44.6); the verdict says so.
//
// runID is the run that owns the verdict (openRun); a run that is no longer the last started writes nothing.
//
// withPartner adds a second player, PartnerYAML (story 38.30), for a meta-game that cannot generate solo.
func (s *Service) RunApworldPreflight(ctx context.Context, hash string, requiredFiles []RequiredFile, withPartner bool, runID string) (storage.ApworldMeta, error) {
	if s.storage == nil {
		return storage.ApworldMeta{}, ErrStorageNotConfigured
	}

	data, err := s.storage.DownloadApworld(ctx, hash)
	if err != nil {
		return storage.ApworldMeta{}, fmt.Errorf("download apworld %s: %w", hash, err)
	}

	template, found, err := s.storage.DownloadApworldTemplate(ctx, hash)
	if err != nil {
		return storage.ApworldMeta{}, fmt.Errorf("download template %s: %w", hash, err)
	}
	if !found || len(template) == 0 {
		// No template to generate with: the check cannot run (AC2 "skipped").
		return s.storeApworldPreflight(ctx, hash, PreflightStatusSkipped, "", "", "", verdictInputs{}, runID)
	}

	// Same container-concurrency budget as the slot preflights (story 9.42 AC6).
	s.preflightSem <- struct{}{}
	defer func() { <-s.preflightSem }()
	s.markRunStarted(ctx, hash, runID)

	genCtx, cancel := context.WithTimeout(ctx, s.cfg.PreflightTimeout)
	defer cancel()

	inputs := verdictInputs{withFiles: len(requiredFiles) > 0, withPartner: withPartner}
	var partner []byte
	if withPartner {
		partner = []byte(PartnerYAML)
	}
	imageID, warning, genErr := s.docker.PreflightGenerate(genCtx, data, hash, template, dockerRequiredFiles(requiredFiles), partner)
	if genErr == nil {
		return s.storeApworldPreflight(ctx, hash, PreflightStatusPassed, "", preflightErrorExcerpt(warning, preflightErrorExcerptMax), imageID, inputs, runID)
	}

	// A timeout counts as failure (AC1): a world that cannot generate a solo seed within
	// the bound would block a real multi-slot generation for even longer.
	if genCtx.Err() != nil {
		genErr = fmt.Errorf("preflight timed out after %s: %w", s.cfg.PreflightTimeout, genErr)
	}
	return s.storeApworldPreflight(ctx, hash, PreflightStatusFailed, preflightErrorExcerpt(genErr.Error(), preflightErrorExcerptMax), "", imageID, inputs, runID)
}

// StartApworldPreflight marks the verdict pending and runs the check in the background
// (same fire-and-forget pattern as the option-type introspection at upload). Infrastructure
// errors are logged and leave the previous verdict in place (AC6).
//
// The staged files it is given are taken before anything changes (story 44.6): an unknown or expired
// one refuses the run with ErrRequiredFileExpired and leaves the verdict as it was.
//
// Review of story 44.6: a run without files does not start while one with files is pending (it would only
// replace a meaningful verdict with a missing-file failure); the new run owns the verdict, so an older one
// finishing later writes nothing; and a run that cannot complete puts the previous verdict back instead of
// leaving it pending for good.
func (s *Service) StartApworldPreflight(hash string, requiredFileIDs []string, withPartner bool) error {
	if s.storage == nil {
		return ErrStorageNotConfigured
	}
	bgCtx := context.Background()
	previous := storage.ApworldPreflight{}
	if meta, err := s.storage.GetApworldMeta(bgCtx, hash); err == nil && meta.Preflight != nil {
		previous = *meta.Preflight
	}
	if len(requiredFileIDs) == 0 && previous.Status == PreflightStatusPending && previous.PendingWithFiles {
		s.log.Info("apworld preflight without files skipped: a run with files is pending", "hash", hash)
		return nil
	}

	requiredFiles, done, err := s.takeRequiredFiles(requiredFileIDs)
	if err != nil {
		return err
	}
	runID, err := newRunID()
	if err != nil {
		done()
		return err
	}

	s.preflightMetaMu.Lock()
	_, markErr := s.storage.MutateApworldPreflight(bgCtx, hash, openRun(runID, len(requiredFiles) > 0))
	s.preflightMetaMu.Unlock()
	if markErr != nil {
		s.log.Warn("failed to mark apworld preflight pending", "hash", hash, "err", markErr)
	}
	go func() {
		defer done()
		if _, err := s.RunApworldPreflight(bgCtx, hash, requiredFiles, withPartner, runID); err != nil {
			s.log.Warn("apworld preflight did not complete", "hash", hash, "err", err)
			s.restoreApworldPreflight(bgCtx, hash, runID, previous)
		}
	}()
	return nil
}

// markRunStarted tells the waiting apart from the generating (story 38.33): the admin sees a test that runs.
// Only informative: a failure to write it never stops the test.
func (s *Service) markRunStarted(ctx context.Context, hash, runID string) {
	s.preflightMetaMu.Lock()
	defer s.preflightMetaMu.Unlock()
	if _, err := s.storage.MutateApworldPreflight(ctx, hash, startRun(runID, time.Now())); err != nil {
		s.log.Warn("failed to mark apworld preflight started", "hash", hash, "err", err)
	}
}

// restoreApworldPreflight puts back the verdict a run that could not complete had replaced, if that run still
// owns it: an infrastructure error says nothing about the apworld (story 9.38 AC6).
func (s *Service) restoreApworldPreflight(ctx context.Context, hash, runID string, previous storage.ApworldPreflight) {
	s.preflightMetaMu.Lock()
	defer s.preflightMetaMu.Unlock()
	if _, err := s.storage.MutateApworldPreflight(ctx, hash, func(p *storage.ApworldPreflight) {
		if ownsRun(p, runID) {
			*p = previous
		}
	}); err != nil {
		s.log.Warn("failed to restore the apworld preflight verdict", "hash", hash, "err", err)
	}
}

// newRunID names one run of an apworld preflight.
func newRunID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate preflight run id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// GetApworldMetaByHash returns the meta sidecar for one apworld (verdict included).
func (s *Service) GetApworldMetaByHash(ctx context.Context, hash string) (storage.ApworldMeta, error) {
	if s.storage == nil {
		return storage.ApworldMeta{}, ErrStorageNotConfigured
	}
	return s.storage.GetApworldMeta(ctx, hash)
}

// OverrideApworldPreflight toggles the admin "force allow" flag on a failed verdict (AC4).
func (s *Service) OverrideApworldPreflight(ctx context.Context, hash string, overridden bool) (storage.ApworldMeta, error) {
	if s.storage == nil {
		return storage.ApworldMeta{}, ErrStorageNotConfigured
	}
	return s.storage.MutateApworldPreflight(ctx, hash, func(p *storage.ApworldPreflight) {
		p.Overridden = overridden
	})
}

// storeApworldPreflight records a completed verdict, stamped with the image of the container
// that produced it (story 38.8), not with whatever the tag points to by now.
func (s *Service) storeApworldPreflight(ctx context.Context, hash, status, errExcerpt, warning, imageID string, inputs verdictInputs, runID string) (storage.ApworldMeta, error) {
	rt := verdictImage(status, s.cfg.APImage, imageID)
	s.preflightMetaMu.Lock()
	defer s.preflightMetaMu.Unlock()
	return s.storage.MutateApworldPreflight(ctx, hash, func(p *storage.ApworldPreflight) {
		if !ownsRun(p, runID) {
			// A newer run was started meanwhile: its verdict is the one that counts.
			return
		}
		applyVerdict(p, status, errExcerpt, warning, time.Now(), rt)
		recordInputs(p, status, inputs)
		p.RunID = ""
		p.PendingWithFiles = false
	})
}

// PartnerYAML is the second player of a test generation with a partner (story 38.30): A Short Hike, a world
// of the release itself, so nothing else to ship, no file to read, and its default options generate.
const PartnerYAML = "name: Partner\ngame: A Short Hike\nA Short Hike: {}\n"

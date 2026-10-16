package service

import (
	"context"
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
func (s *Service) RunApworldPreflight(ctx context.Context, hash string) (storage.ApworldMeta, error) {
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
		return s.storeApworldPreflight(ctx, hash, PreflightStatusSkipped, "", "", "")
	}

	// Same container-concurrency budget as the slot preflights (story 9.42 AC6).
	s.preflightSem <- struct{}{}
	defer func() { <-s.preflightSem }()

	genCtx, cancel := context.WithTimeout(ctx, s.cfg.PreflightTimeout)
	defer cancel()

	imageID, warning, genErr := s.docker.PreflightGenerate(genCtx, data, hash, template)
	if genErr == nil {
		return s.storeApworldPreflight(ctx, hash, PreflightStatusPassed, "", preflightErrorExcerpt(warning, preflightErrorExcerptMax), imageID)
	}

	// A timeout counts as failure (AC1): a world that cannot generate a solo seed within
	// the bound would block a real multi-slot generation for even longer.
	if genCtx.Err() != nil {
		genErr = fmt.Errorf("preflight timed out after %s: %w", s.cfg.PreflightTimeout, genErr)
	}
	return s.storeApworldPreflight(ctx, hash, PreflightStatusFailed, preflightErrorExcerpt(genErr.Error(), preflightErrorExcerptMax), "", imageID)
}

// StartApworldPreflight marks the verdict pending and runs the check in the background
// (same fire-and-forget pattern as the option-type introspection at upload). Infrastructure
// errors are logged and leave the previous verdict in place (AC6).
func (s *Service) StartApworldPreflight(hash string) {
	if s.storage == nil {
		return
	}
	bgCtx := context.Background()
	if _, err := s.storage.MutateApworldPreflight(bgCtx, hash, markPending); err != nil {
		s.log.Warn("failed to mark apworld preflight pending", "hash", hash, "err", err)
	}
	go func() {
		if _, err := s.RunApworldPreflight(bgCtx, hash); err != nil {
			s.log.Warn("apworld preflight did not complete", "hash", hash, "err", err)
		}
	}()
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
func (s *Service) storeApworldPreflight(ctx context.Context, hash, status, errExcerpt, warning, imageID string) (storage.ApworldMeta, error) {
	rt := verdictImage(status, s.cfg.APImage, imageID)
	return s.storage.MutateApworldPreflight(ctx, hash, func(p *storage.ApworldPreflight) {
		applyVerdict(p, status, errExcerpt, warning, time.Now(), rt)
	})
}

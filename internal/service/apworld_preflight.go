package service

import (
	"context"
	"fmt"
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

// preflightErrorExcerpt returns the last maxLen bytes of errText (whole string when shorter).
func preflightErrorExcerpt(errText string, maxLen int) string {
	if maxLen <= 0 || len(errText) <= maxLen {
		return errText
	}
	return errText[len(errText)-maxLen:]
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
		return s.storeApworldPreflight(ctx, hash, PreflightStatusSkipped, "")
	}

	genCtx, cancel := context.WithTimeout(ctx, s.cfg.PreflightTimeout)
	defer cancel()

	genErr := s.docker.PreflightGenerate(genCtx, data, hash, template)
	if genErr == nil {
		return s.storeApworldPreflight(ctx, hash, PreflightStatusPassed, "")
	}

	// A timeout counts as failure (AC1): a world that cannot generate a solo seed within
	// the bound would block a real multi-slot generation for even longer.
	if genCtx.Err() != nil {
		genErr = fmt.Errorf("preflight timed out after %s: %w", s.cfg.PreflightTimeout, genErr)
	}
	return s.storeApworldPreflight(ctx, hash, PreflightStatusFailed, preflightErrorExcerpt(genErr.Error(), preflightErrorExcerptMax))
}

// StartApworldPreflight marks the verdict pending and runs the check in the background
// (same fire-and-forget pattern as the option-type introspection at upload). Infrastructure
// errors are logged and leave the previous verdict in place (AC6).
func (s *Service) StartApworldPreflight(hash string) {
	if s.storage == nil {
		return
	}
	bgCtx := context.Background()
	if _, err := s.storage.MutateApworldPreflight(bgCtx, hash, func(p *storage.ApworldPreflight) {
		p.Status = PreflightStatusPending
		p.Error = ""
	}); err != nil {
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

func (s *Service) storeApworldPreflight(ctx context.Context, hash, status, errExcerpt string) (storage.ApworldMeta, error) {
	return s.storage.MutateApworldPreflight(ctx, hash, func(p *storage.ApworldPreflight) {
		p.Status = status
		p.Error = errExcerpt
		p.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	})
}

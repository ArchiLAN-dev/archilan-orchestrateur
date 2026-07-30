package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Slot preflight job statuses (story 9.42): a solo test generation of one player's real
// YAML, queried by the central API until it settles.
const (
	SlotPreflightPending = "pending"
	SlotPreflightPassed  = "passed"
	SlotPreflightFailed  = "failed"
)

// slotPreflightTTL bounds how long a job result stays queryable. Results are short-lived
// by design: the API polls within minutes and persists the verdict on its side.
const slotPreflightTTL = 30 * time.Minute

// SlotPreflight is the in-memory state of one slot preflight job. Jobs do not survive an
// orchestrateur restart: a poller getting a 404 must treat the verdict as unknown.
type SlotPreflight struct {
	Status    string
	Error     string
	CreatedAt time.Time
}

// StartSlotPreflight queues a solo test generation for the given player YAML (story 9.42).
// apworldHash is empty for official worlds bundled in the generation image. Returns the job
// id to poll with GetSlotPreflight.
func (s *Service) StartSlotPreflight(playerYaml []byte, apworldHash string) (string, error) {
	if len(playerYaml) == 0 {
		return "", fmt.Errorf("empty player yaml")
	}
	if apworldHash != "" && s.storage == nil {
		return "", ErrStorageNotConfigured
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	id := hex.EncodeToString(buf)

	now := time.Now()
	s.slotPreflightMu.Lock()
	s.pruneSlotPreflightsLocked(now)
	s.slotPreflights[id] = &SlotPreflight{Status: SlotPreflightPending, CreatedAt: now}
	s.slotPreflightMu.Unlock()

	go s.runSlotPreflight(id, playerYaml, apworldHash)

	return id, nil
}

// GetSlotPreflight returns the job state; ok is false for unknown or expired ids.
func (s *Service) GetSlotPreflight(id string) (SlotPreflight, bool) {
	s.slotPreflightMu.Lock()
	defer s.slotPreflightMu.Unlock()
	job, ok := s.slotPreflights[id]
	if !ok {
		return SlotPreflight{}, false
	}
	return *job, true
}

func (s *Service) runSlotPreflight(id string, playerYaml []byte, apworldHash string) {
	// Preflight containers share one concurrency budget (AC6) with the upload check.
	s.preflightSem <- struct{}{}
	defer func() { <-s.preflightSem }()

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.PreflightTimeout)
	defer cancel()

	var apworldData []byte
	if apworldHash != "" {
		data, err := s.storage.DownloadApworld(ctx, apworldHash)
		if err != nil {
			s.finishSlotPreflight(id, SlotPreflightFailed, preflightErrorExcerpt(fmt.Sprintf("download apworld %s: %v", apworldHash, err), preflightErrorExcerptMax))
			return
		}
		apworldData = data
	}

	if err := s.docker.PreflightGenerate(ctx, apworldData, apworldHash, playerYaml); err != nil {
		msg := err.Error()
		if ctx.Err() != nil {
			msg = fmt.Sprintf("preflight timed out after %s: %s", s.cfg.PreflightTimeout, msg)
		}
		s.finishSlotPreflight(id, SlotPreflightFailed, preflightErrorExcerpt(msg, preflightErrorExcerptMax))
		return
	}

	s.finishSlotPreflight(id, SlotPreflightPassed, "")
}

func (s *Service) finishSlotPreflight(id, status, errText string) {
	s.slotPreflightMu.Lock()
	defer s.slotPreflightMu.Unlock()
	if job, ok := s.slotPreflights[id]; ok {
		job.Status = status
		job.Error = errText
	}
}

// pruneSlotPreflightsLocked drops expired jobs; callers hold slotPreflightMu.
func (s *Service) pruneSlotPreflightsLocked(now time.Time) {
	for id, job := range s.slotPreflights {
		if now.Sub(job.CreatedAt) > slotPreflightTTL {
			delete(s.slotPreflights, id)
		}
	}
}

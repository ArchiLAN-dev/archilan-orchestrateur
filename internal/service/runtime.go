package service

import (
	"context"
	"time"

	"archilan.fr/orchestrateur/internal/storage"
)

// RuntimeInfo names the Archipelago image the orchestrator runs (story 38.8): the configured
// reference (`AP_IMAGE`) and the id of the local image it points to. The reference alone says
// nothing locally (`archipelago:latest`) and misses a re-pushed tag; the id covers both.
type RuntimeInfo struct {
	APImage   string
	APImageID string
}

type imageInspector interface {
	ImageID(ctx context.Context, ref string) (string, error)
}

// Runtime returns the image in use. An inspection failure leaves the id empty and is only
// logged: it must never keep a verdict from being recorded.
func (s *Service) Runtime(ctx context.Context) RuntimeInfo {
	info := RuntimeInfo{APImage: s.cfg.APImage}
	if s.images == nil {
		return info
	}
	id, err := s.images.ImageID(ctx, s.cfg.APImage)
	if err != nil {
		s.log.Warn("could not inspect the archipelago image", "image", s.cfg.APImage, "err", err)
		return info
	}
	info.APImageID = id
	return info
}

// applyVerdict writes a completed verdict with the image it ran on. The admin's override survives a
// failed or skipped verdict and is cleared by a passed one (story 38.10).
func applyVerdict(p *storage.ApworldPreflight, status, errExcerpt, warning string, checkedAt time.Time, rt RuntimeInfo) {
	p.Status = status
	p.Error = errExcerpt
	p.Warning = warning
	p.CheckedAt = checkedAt.UTC().Format(time.RFC3339)
	p.Image = rt.APImage
	p.ImageID = rt.APImageID
	// The override is a force-allow for a failed verdict (story 38.10). A pass leaves it nothing to
	// allow; kept, it made the rolling test skip the version and silenced any later failure.
	if status == PreflightStatusPassed {
		p.Overridden = false
	}
}

// verdictImage names the image a finished verdict ran on (story 38.8 review): the configured
// reference and the id of the container's own image. A skipped verdict ran nothing.
func verdictImage(status, ref, imageID string) RuntimeInfo {
	if status == PreflightStatusSkipped {
		return RuntimeInfo{}
	}
	return RuntimeInfo{APImage: ref, APImageID: imageID}
}

// markPending opens a new run of the verdict: its outcome and the image it ran on are unknown
// again. The admin's override survives (only the override endpoint toggles it).
func markPending(p *storage.ApworldPreflight) {
	p.Status = PreflightStatusPending
	p.Error = ""
	p.Warning = ""
	p.CheckedAt = ""
	p.Image = ""
	p.ImageID = ""
}

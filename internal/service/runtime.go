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
	APImage   string `json:"apImage"`
	APImageID string `json:"apImageId"`
}

type imageInspector interface {
	ImageID(ctx context.Context, ref string) (string, error)
}

// Runtime returns the image in use. An inspection failure leaves the id empty and is only
// logged: it must never keep a verdict from being recorded.
func (s *Service) Runtime(ctx context.Context) RuntimeInfo {
	info := RuntimeInfo{APImage: s.cfg.APImage}
	id, err := s.images.ImageID(ctx, s.cfg.APImage)
	if err != nil {
		s.log.Warn("could not inspect the archipelago image", "image", s.cfg.APImage, "err", err)
		return info
	}
	info.APImageID = id
	return info
}

// applyVerdict writes a completed verdict with the image it ran on, keeping the admin's
// override (only the override endpoint toggles it).
func applyVerdict(p *storage.ApworldPreflight, status, errExcerpt string, checkedAt time.Time, rt RuntimeInfo) {
	p.Status = status
	p.Error = errExcerpt
	p.CheckedAt = checkedAt.UTC().Format(time.RFC3339)
	p.Image = rt.APImage
	p.ImageID = rt.APImageID
}

package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"archilan.fr/orchestrateur/internal/config"
	"archilan.fr/orchestrateur/internal/storage"
)

type fakeImages struct {
	id  string
	err error
}

func (f fakeImages) ImageID(context.Context, string) (string, error) { return f.id, f.err }

func runtimeService(images imageInspector) *Service {
	return &Service{
		cfg:    &config.Config{APImage: "ghcr.io/archilan-dev/archipelago:0.16.1"},
		images: images,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// Story 38.8: the API knows which Archipelago image the orchestrator runs.
func TestRuntimeReportsTheImageAndItsID(t *testing.T) {
	got := runtimeService(fakeImages{id: "sha256:abc123"}).Runtime(context.Background())

	want := RuntimeInfo{APImage: "ghcr.io/archilan-dev/archipelago:0.16.1", APImageID: "sha256:abc123"}
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// An inspection failure must never block a verdict: the reference is still known.
func TestRuntimeKeepsTheReferenceWhenInspectionFails(t *testing.T) {
	got := runtimeService(fakeImages{err: errors.New("no such image")}).Runtime(context.Background())

	if got.APImage != "ghcr.io/archilan-dev/archipelago:0.16.1" || got.APImageID != "" {
		t.Errorf("expected the reference without id, got %+v", got)
	}
}

func TestACompletedVerdictRecordsTheImageItRanOn(t *testing.T) {
	p := storage.ApworldPreflight{Status: PreflightStatusPending, Overridden: true}
	at := time.Date(2026, 9, 26, 4, 0, 0, 0, time.UTC)

	applyVerdict(&p, PreflightStatusFailed, "FillError", at, RuntimeInfo{APImage: "archipelago:0.16.1", APImageID: "sha256:abc123"})

	want := storage.ApworldPreflight{
		Status:     PreflightStatusFailed,
		Error:      "FillError",
		CheckedAt:  "2026-09-26T04:00:00Z",
		Overridden: true,
		Image:      "archipelago:0.16.1",
		ImageID:    "sha256:abc123",
	}
	if p != want {
		t.Errorf("expected %+v, got %+v", want, p)
	}
}

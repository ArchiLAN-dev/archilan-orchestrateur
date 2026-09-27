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

	applyVerdict(&p, PreflightStatusFailed, "FillError", "", at, RuntimeInfo{APImage: "archipelago:0.16.1", APImageID: "sha256:abc123"})

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

// Story 38.10: the override is the admin's "force allow" for a failed verdict. Once the version
// passes, it has nothing left to allow - and kept, it made the rolling catalogue test skip the
// version for good and silenced any later failure as "ignored". A pass clears it; a failure keeps
// it (see above), so a forced version stays selectable.
func TestAPassedVerdictClearsTheOverride(t *testing.T) {
	p := storage.ApworldPreflight{Status: PreflightStatusPending, Overridden: true}

	applyVerdict(&p, PreflightStatusPassed, "", "", time.Date(2026, 9, 27, 1, 41, 0, 0, time.UTC), RuntimeInfo{APImage: "archipelago:0.16.3"})

	if p.Overridden {
		t.Errorf("a passed verdict kept the override: %+v", p)
	}
}

// A skipped verdict tested nothing: it proves nothing either way, so the override stays.
func TestASkippedVerdictKeepsTheOverride(t *testing.T) {
	p := storage.ApworldPreflight{Status: PreflightStatusPending, Overridden: true}

	applyVerdict(&p, PreflightStatusSkipped, "", "", time.Date(2026, 9, 27, 1, 41, 0, 0, time.UTC), RuntimeInfo{})

	if !p.Overridden {
		t.Errorf("a skipped verdict dropped the override: %+v", p)
	}
}

// Story 38.8 review: a skipped verdict ran nothing, so it names no image; a finished one names the
// image of the container that actually ran.
func TestOnlyAVerdictThatRanNamesAnImage(t *testing.T) {
	ran := verdictImage(PreflightStatusFailed, "archipelago:latest", "sha256:ran")
	if ran != (RuntimeInfo{APImage: "archipelago:latest", APImageID: "sha256:ran"}) {
		t.Errorf("expected the image that ran, got %+v", ran)
	}
	if skipped := verdictImage(PreflightStatusSkipped, "archipelago:latest", "sha256:ran"); skipped != (RuntimeInfo{}) {
		t.Errorf("a skipped verdict ran nothing, got %+v", skipped)
	}
}

// Story 38.12: a pass with a warning (accessibility not met, as the Launcher allows) keeps its text; the
// next verdict replaces it, whatever its outcome.
func TestAPassedVerdictCarriesItsWarningUntilTheNextOne(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	p := storage.ApworldPreflight{Status: PreflightStatusPending}

	applyVerdict(&p, PreflightStatusPassed, "", "Missing: [A]", at, RuntimeInfo{APImage: "archipelago:0.16.4"})
	if p.Status != PreflightStatusPassed || p.Warning != "Missing: [A]" {
		t.Fatalf("expected a pass with its warning, got %+v", p)
	}

	applyVerdict(&p, PreflightStatusPassed, "", "", at, RuntimeInfo{APImage: "archipelago:0.16.4"})
	if p.Warning != "" {
		t.Errorf("a clean pass kept the old warning: %+v", p)
	}
}

// A verdict being recomputed is unknown: it must not keep claiming the image of the previous run.
func TestAPendingVerdictForgetsThePreviousImage(t *testing.T) {
	p := storage.ApworldPreflight{Status: PreflightStatusPassed, Error: "old", Warning: "old warning", Overridden: true, Image: "archipelago:0.16.0", ImageID: "sha256:old"}

	markPending(&p)

	want := storage.ApworldPreflight{Status: PreflightStatusPending, Overridden: true}
	if p != want {
		t.Errorf("expected %+v, got %+v", want, p)
	}
}

func TestRuntimeWithoutAnInspectorKeepsTheReference(t *testing.T) {
	got := runtimeService(nil).Runtime(context.Background())

	if got != (RuntimeInfo{APImage: "ghcr.io/archilan-dev/archipelago:0.16.1"}) {
		t.Errorf("expected the reference alone, got %+v", got)
	}
}

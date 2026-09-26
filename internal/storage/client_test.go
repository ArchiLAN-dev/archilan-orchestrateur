package storage

import (
	"encoding/json"
	"testing"
)

// Story 38.8: a sidecar written before verdicts carried their image still reads, without one.
func TestLegacySidecarWithoutImageIsReadable(t *testing.T) {
	raw := []byte(`{"hash":"h1","game":"Crystal Project","preflight":{"status":"passed","checkedAt":"2026-07-01T10:00:00Z"}}`)

	var meta ApworldMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Preflight == nil || meta.Preflight.Status != "passed" {
		t.Fatalf("expected the verdict to survive, got %+v", meta.Preflight)
	}
	if meta.Preflight.Image != "" || meta.Preflight.ImageID != "" {
		t.Errorf("expected no image, got %q / %q", meta.Preflight.Image, meta.Preflight.ImageID)
	}
}

func TestAVerdictCarriesItsImageInTheSidecar(t *testing.T) {
	raw, err := json.Marshal(ApworldPreflight{Status: "passed", Image: "archipelago:0.16.1", ImageID: "sha256:abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(raw) != `{"status":"passed","image":"archipelago:0.16.1","imageId":"sha256:abc123"}` {
		t.Errorf("unexpected sidecar %s", raw)
	}
}

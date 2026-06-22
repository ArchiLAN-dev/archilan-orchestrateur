package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

const luigiTemplate = `game: Luigi's Mansion
Luigi's Mansion:
  toadsanity:
    false: 50
    true: 0
  rank_requirement:
    rank_h: 50
    rank_g: 0
    rank_f: 0
  progression_balancing:
    # Minimum value is 0
    # Maximum value is 99
    normal: 50 # equivalent to 50
`

func TestApworldOptionKeys_knownKeys(t *testing.T) {
	keys := apworldOptionKeys([]byte(luigiTemplate))

	for _, want := range []string{"toadsanity", "rank_requirement", "progression_balancing"} {
		if !keys[want] {
			t.Errorf("expected %q to be a valid key", want)
		}
	}
}

func TestApworldOptionKeys_unknownKey(t *testing.T) {
	keys := apworldOptionKeys([]byte(luigiTemplate))

	if keys["toadanity"] {
		t.Error("typo 'toadanity' should not be a valid key")
	}
	if keys[""] {
		t.Error("empty string should not be a valid key")
	}
}

func TestApworldOptionKeys_emptyTemplate(t *testing.T) {
	keys := apworldOptionKeys([]byte{})
	if len(keys) != 0 {
		t.Errorf("expected empty key set, got %d keys", len(keys))
	}
}

func TestBuildPlayerYaml_scalarValues(t *testing.T) {
	out, err := buildPlayerYaml("Jean", "Luigi's Mansion", map[string]any{
		"toadsanity": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name: Jean") {
		t.Errorf("missing name field: %s", out)
	}
	if !strings.Contains(out, "toadsanity: true") {
		t.Errorf("missing toadsanity: %s", out)
	}
}

func TestBuildPlayerYaml_weightedValues(t *testing.T) {
	out, err := buildPlayerYaml("Jean", "Luigi's Mansion", map[string]any{
		"rank_requirement": map[string]any{
			"rank_h": float64(70),
			"rank_f": float64(30),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rank_h: 70") {
		t.Errorf("missing rank_h weight: %s", out)
	}
	if !strings.Contains(out, "rank_f: 30") {
		t.Errorf("missing rank_f weight: %s", out)
	}
}

func TestBuildPlayerYaml_emptyValues(t *testing.T) {
	out, err := buildPlayerYaml("Jean", "Luigi's Mansion", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name: Jean") {
		t.Errorf("missing name field: %s", out)
	}
	if !strings.Contains(out, "game: Luigi") {
		t.Errorf("missing game field: %s", out)
	}
}

func TestServerOptions_marshalApplyRoundTrip(t *testing.T) {
	cheat := false
	hint := 25
	loc := 2
	shutdown := 1800
	compat := 1
	req := LaunchRequest{
		SessionID:           "s1",
		ServerPassword:      "pw",
		AdminPassword:       "admin",
		ReleaseMode:         "goal",
		CollectMode:         "auto",
		RemainingMode:       "goal",
		CountdownMode:       "auto",
		DisableItemCheat:    &cheat,
		HintCost:            &hint,
		LocationCheckPoints: &loc,
		AutoShutdown:        &shutdown,
		Compatibility:       &compat,
	}

	blob, err := marshalServerOptions(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Apply onto a fresh request, as relaunch-from-save does, and check every option survives.
	got := LaunchRequest{SessionID: "s1", ServerPassword: "pw", AdminPassword: "admin"}
	if err := applyServerOptions(&got, blob); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got.ReleaseMode != "goal" || got.CollectMode != "auto" || got.RemainingMode != "goal" || got.CountdownMode != "auto" {
		t.Errorf("string modes not replayed: %+v", got)
	}
	if got.AutoShutdown == nil || *got.AutoShutdown != 1800 {
		t.Errorf("autoShutdown = %v, want 1800", got.AutoShutdown)
	}
	if got.HintCost == nil || *got.HintCost != 25 {
		t.Errorf("hintCost = %v, want 25", got.HintCost)
	}
	if got.LocationCheckPoints == nil || *got.LocationCheckPoints != 2 {
		t.Errorf("locationCheckPoints = %v, want 2", got.LocationCheckPoints)
	}
	if got.DisableItemCheat == nil || *got.DisableItemCheat {
		t.Errorf("disableItemCheat = %v, want false (a pointer-to-false must survive omitempty)", got.DisableItemCheat)
	}
	if got.Compatibility == nil || *got.Compatibility != 1 {
		t.Errorf("compatibility = %v, want 1", got.Compatibility)
	}
}

func TestApplyServerOptions_invalidBlobErrors(t *testing.T) {
	req := LaunchRequest{SessionID: "s1"}
	if err := applyServerOptions(&req, "not-json"); err == nil {
		t.Fatal("expected an error on a corrupt blob")
	}
}

func TestTarToZipAndBack_roundTrip(t *testing.T) {
	// Build a Docker-style tar of /data/output with two files.
	var tbuf bytes.Buffer
	tw := tar.NewWriter(&tbuf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "output/", Mode: 0755})
	files := map[string]string{
		"AP_42.archipelago":     "MULTIDATA",
		"AP_42_P1_Jean.apemerald": "PATCHBYTES",
		"AP_42_Spoiler.txt":     "SPOILER",
	}
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "output/" + name, Mode: 0644, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()

	// tar → zip (flat, no output/ prefix)
	zipData, err := tarToZip(tbuf.Bytes())
	if err != nil {
		t.Fatalf("tarToZip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[f.Name] = string(b)
	}
	for name, content := range files {
		if got[name] != content {
			t.Errorf("zip entry %q = %q, want %q", name, got[name], content)
		}
	}

	// zip → output tar (entries under output/)
	outTar, err := zipToOutputTar(zipData)
	if err != nil {
		t.Fatalf("zipToOutputTar: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(outTar))
	gotTar := map[string]string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, _ := io.ReadAll(tr)
		gotTar[hdr.Name] = string(b)
	}
	for name, content := range files {
		if gotTar["output/"+name] != content {
			t.Errorf("tar entry output/%s = %q, want %q", name, gotTar["output/"+name], content)
		}
	}
}

func TestIsZipArtifact(t *testing.T) {
	if !isZipArtifact("archive.zip", nil) {
		t.Error("expected .zip filename to be detected as zip")
	}
	if !isZipArtifact("x", []byte{'P', 'K', 0x03, 0x04, 0x00}) {
		t.Error("expected PK magic to be detected as zip")
	}
	if isZipArtifact("AP_42.archipelago", []byte("MULTIDATA")) {
		t.Error("expected .archipelago to not be detected as zip")
	}
}

func TestBuildOutputArtifact_singleBundleZipReturnedAsIs(t *testing.T) {
	bundle := []byte("PK\x03\x04 fake-ap-bundle-bytes")
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "output/", Mode: 0755})
	_ = tw.WriteHeader(&tar.Header{Name: "output/AP_123.zip", Mode: 0644, Size: int64(len(bundle))})
	_, _ = tw.Write(bundle)
	_ = tw.Close()

	art, err := buildOutputArtifact(tb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(art, bundle) {
		t.Errorf("expected the AP bundle zip returned as-is (%d bytes), got %d", len(bundle), len(art))
	}
}

func TestBuildOutputArtifact_looseFilesZipped(t *testing.T) {
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "output/", Mode: 0755})
	files := map[string]string{"AP_1.archipelago": "MD", "AP_1_P1.apemerald": "PATCH", "AP_1_Spoiler.txt": "SP"}
	for n, c := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "output/" + n, Mode: 0644, Size: int64(len(c))})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()

	art, err := buildOutputArtifact(tb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(art), int64(len(art)))
	if err != nil {
		t.Fatalf("expected a valid flat zip: %v", err)
	}
	if len(zr.File) != len(files) {
		t.Errorf("expected %d entries, got %d", len(files), len(zr.File))
	}
}

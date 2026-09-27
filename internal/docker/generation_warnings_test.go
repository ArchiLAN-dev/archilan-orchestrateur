package docker

import "testing"

// Story 38.12: a generation the official Launcher produces with a warning passes here too; the generator
// then says so on stderr, one sentinel line per warning, and the verdict carries its text.
func TestGenerationWarningsAreReadFromTheirSentinelLines(t *testing.T) {
	stderr := []byte("DEBUG worlds loaded: [...]\n" +
		"Warning: Could not access required locations for accessibility check. Missing: [A, B]\n" +
		`###ARCHILAN-WARNING### {"type": "accessibility", "message": "Could not access required locations for accessibility check. Missing: [A, B]", "missing": ["A", "B"]}` + "\n" +
		`###ARCHILAN-WARNING### {"type": "accessibility", "message": "second"}` + "\n")

	got := generationWarnings(stderr)

	want := "Could not access required locations for accessibility check. Missing: [A, B]\nsecond"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestAGenerationWithoutWarningHasNone(t *testing.T) {
	if got := generationWarnings([]byte("DEBUG worlds loaded\nWarning: data package skipped\n")); got != "" {
		t.Errorf("a plain stderr line is not a generation warning, got %q", got)
	}
}

func TestAMalformedWarningLineIsKeptAsText(t *testing.T) {
	got := generationWarnings([]byte("###ARCHILAN-WARNING### not json\n"))

	if got != "not json" {
		t.Errorf("expected the raw text rather than nothing, got %q", got)
	}
}

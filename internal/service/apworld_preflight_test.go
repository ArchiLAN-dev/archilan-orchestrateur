package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreflightErrorExcerpt_shortStringUnchanged(t *testing.T) {
	got := preflightErrorExcerpt("boom", 2000)
	if got != "boom" {
		t.Errorf("expected %q, got %q", "boom", got)
	}
}

// A fill error is one enormous line. A plain tail cut used to land inside it and drop the
// head - the only actionable part. Per-line capping keeps that head (story 9.43).
func TestPreflightErrorExcerpt_longSingleLineKeepsItsHead(t *testing.T) {
	long := "FillError: Not all progression items reachable. Unreachable: " + strings.Repeat("(Quest A, Item B), ", 500)

	got := preflightErrorExcerpt(long, preflightErrorExcerptMax)

	if !strings.HasPrefix(got, "FillError: Not all progression items reachable.") {
		t.Errorf("expected the head to survive, got %q", got[:80])
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected an ellipsis marking the cut, got %q", got[len(got)-20:])
	}
	if n := utf8.RuneCountInString(got); n != preflightErrorLineMax+1 {
		t.Errorf("expected %d runes, got %d", preflightErrorLineMax+1, n)
	}
}

func TestPreflightErrorExcerpt_multiLineKeepsTheTail(t *testing.T) {
	log := strings.Repeat("noise line\n", 500) + "Traceback (most recent call last):\nOptionError: fix your yaml"

	got := preflightErrorExcerpt(log, 200)

	if !strings.HasSuffix(got, "OptionError: fix your yaml") {
		t.Errorf("expected the last lines to survive, got %q", got)
	}
	if utf8.RuneCountInString(got) != 200 {
		t.Errorf("expected 200 runes, got %d", utf8.RuneCountInString(got))
	}
}

// The traceback structure must survive next to a huge line: capping is per line, so short
// lines around the dump are untouched.
func TestPreflightErrorExcerpt_capsOnlyTheLongLine(t *testing.T) {
	log := "Traceback (most recent call last):\n" +
		strings.Repeat("x", 5000) + "\n" +
		"Exception in <bound method W.generate_early ...> for player 1, named Player1."

	got := preflightErrorExcerpt(log, preflightErrorExcerptMax)
	lines := strings.Split(got, "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "Traceback (most recent call last):" {
		t.Errorf("short leading line must be untouched, got %q", lines[0])
	}
	if utf8.RuneCountInString(lines[1]) != preflightErrorLineMax+1 {
		t.Errorf("long line should be capped, got %d runes", utf8.RuneCountInString(lines[1]))
	}
	if !strings.HasSuffix(lines[2], "named Player1.") {
		t.Errorf("short trailing line must be untouched, got %q", lines[2])
	}
}

func TestPreflightErrorExcerpt_neverCutsMidRune(t *testing.T) {
	// Accented text: a byte-based cut would produce invalid UTF-8.
	got := preflightErrorExcerpt(strings.Repeat("é", 4000), preflightErrorExcerptMax)

	if !utf8.ValidString(got) {
		t.Error("excerpt must stay valid UTF-8")
	}
}

// Story 44.3: the real failure line of DKC3, which names the ROM its world needs. Longer than a capped
// line, it was cut and its JSON could no longer be read.
const dkc3FailureLine = `###ARCHILAN-FAILURE### {"type": "FileNotFoundError", "message": "Donkey Kong Country 3 - Dixie Kong's Double Trouble! (USA) (En,Fr).sfc does not exist, but DK3Settings.rom_file is required", "requiredFile": {"setting": "DK3Settings.rom_file", "file": "Donkey Kong Country 3 - Dixie Kong's Double Trouble! (USA) (En,Fr).sfc", "extension": ".sfc", "description": "DKC3 (US) ROM File", "md5s": ["120abf304f0c40fe059f6a192ed4f947"]}}`

func TestPreflightErrorExcerpt_keepsTheFailureLineWhole(t *testing.T) {
	log := "Traceback (most recent call last):\n" + strings.Repeat("x", 500) + "\nFileNotFoundError: boom\n" + dkc3FailureLine

	got := preflightErrorExcerpt(log, preflightErrorExcerptMax)

	if !strings.HasSuffix(got, "\n"+dkc3FailureLine) {
		t.Fatalf("the failure line must end the excerpt whole, got %q", got)
	}
	if !strings.Contains(got, strings.Repeat("x", preflightErrorLineMax)+"…") {
		t.Error("ordinary lines are still capped")
	}
}

func TestPreflightErrorExcerpt_keepsTheFailureLineBeyondTheTail(t *testing.T) {
	log := strings.Repeat("line\n", 2000) + dkc3FailureLine

	got := preflightErrorExcerpt(log, 600)

	if !strings.HasSuffix(got, dkc3FailureLine) {
		t.Fatalf("the failure line must survive the tail cut, got %q", got)
	}
	if n := len([]rune(got)); n > 600 {
		t.Errorf("excerpt is %d runes, want at most 600", n)
	}
}

func TestPreflightErrorExcerpt_keepsWarningLinesWhole(t *testing.T) {
	warning := "###ARCHILAN-WARNING### " + `{"type": "accessibility", "message": "` + strings.Repeat("m", 400) + `"}`

	got := preflightErrorExcerpt("Warning: something\n"+warning, preflightErrorExcerptMax)

	if !strings.HasSuffix(got, warning) {
		t.Fatalf("the warning line must stay whole, got %q", got)
	}
}

func TestPreflightErrorExcerpt_nonPositiveMaxKeepsEverythingButCapsLines(t *testing.T) {
	got := preflightErrorExcerpt("abc", 0)
	if got != "abc" {
		t.Errorf("expected unchanged string for maxLen 0, got %q", got)
	}
}

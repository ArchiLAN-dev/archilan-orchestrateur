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

func TestPreflightErrorExcerpt_nonPositiveMaxKeepsEverythingButCapsLines(t *testing.T) {
	got := preflightErrorExcerpt("abc", 0)
	if got != "abc" {
		t.Errorf("expected unchanged string for maxLen 0, got %q", got)
	}
}

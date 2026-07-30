package service

import (
	"strings"
	"testing"
)

func TestPreflightErrorExcerpt_shortStringUnchanged(t *testing.T) {
	got := preflightErrorExcerpt("boom", 2000)
	if got != "boom" {
		t.Errorf("expected %q, got %q", "boom", got)
	}
}

func TestPreflightErrorExcerpt_keepsTheTail(t *testing.T) {
	long := strings.Repeat("x", 3000) + "Exception: the actionable part"
	got := preflightErrorExcerpt(long, 100)
	if len(got) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(got))
	}
	if !strings.HasSuffix(got, "Exception: the actionable part") {
		t.Errorf("excerpt should keep the tail, got %q", got)
	}
}

func TestPreflightErrorExcerpt_exactBoundary(t *testing.T) {
	s := strings.Repeat("y", 50)
	if got := preflightErrorExcerpt(s, 50); got != s {
		t.Errorf("expected unchanged string at exact boundary, got %q", got)
	}
}

func TestPreflightErrorExcerpt_nonPositiveMaxIsNoop(t *testing.T) {
	if got := preflightErrorExcerpt("abc", 0); got != "abc" {
		t.Errorf("expected unchanged string for maxLen 0, got %q", got)
	}
}

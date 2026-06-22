package db

import (
	"testing"
	"time"
)

func TestSessionServerOptions_roundTrip(t *testing.T) {
	d, err := New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	defer d.Close()

	now := time.Now().UTC()
	if err := d.InsertSession(&Session{SessionID: "s1", Status: "generated", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// A freshly inserted session carries no options yet.
	got, err := d.GetSessionServerOptions("s1")
	if err != nil {
		t.Fatalf("get (empty): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil options, got %q", *got)
	}

	blob := `{"autoShutdown":1800,"releaseMode":"goal"}`
	if err := d.SaveSessionServerOptions("s1", blob); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err = d.GetSessionServerOptions("s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || *got != blob {
		t.Fatalf("got %v, want %q", got, blob)
	}
}

func TestGetSessionServerOptions_unknownSession(t *testing.T) {
	d, err := New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	defer d.Close()

	got, err := d.GetSessionServerOptions("nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown session, got %q", *got)
	}
}
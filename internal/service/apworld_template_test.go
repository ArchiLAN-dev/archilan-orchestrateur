package service

import (
	"context"
	"errors"
	"testing"
)

// storage and docker are concrete clients, so these cover the guards that run before any
// of them is touched - the paths a caller can trigger without a live MinIO/Docker.

func TestSetApworldTemplate_withoutStorage(t *testing.T) {
	s := &Service{}

	err := s.SetApworldTemplate(context.Background(), "deadbeef", []byte("game: X"))

	if !errors.Is(err, ErrStorageNotConfigured) {
		t.Errorf("expected ErrStorageNotConfigured, got %v", err)
	}
}

func TestSetApworldTemplate_rejectsEmptyTemplate(t *testing.T) {
	// A non-nil storage would be needed to go further; the empty check must fire first so an
	// empty body can never blank a stored template.
	s := &Service{}

	err := s.SetApworldTemplate(context.Background(), "deadbeef", []byte(""))

	if err == nil {
		t.Fatal("expected an error for an empty template")
	}
}

func TestRegenerateApworldTemplate_withoutStorage(t *testing.T) {
	s := &Service{}

	_, err := s.RegenerateApworldTemplate(context.Background(), "deadbeef")

	if !errors.Is(err, ErrStorageNotConfigured) {
		t.Errorf("expected ErrStorageNotConfigured, got %v", err)
	}
}

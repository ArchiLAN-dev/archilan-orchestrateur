package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"archilan.fr/orchestrateur/internal/config"
)

// Story 44.3: the files the image's own worlds declare are listed once per image.

type fakeLister struct {
	calls int
	out   string
	err   error
}

func (f *fakeLister) ListRequiredFiles(context.Context) ([]byte, error) {
	f.calls++
	return []byte(f.out), f.err
}

const listing = `{"Ocarina of Time": [{"setting": "OOTSettings.rom_file", "file": "oot.z64", "extension": ".z64", "description": "Ocarina of Time ROM File", "md5s": ["5bd1fe107bf8106b2ab6650abecd54d6"]}]}`

func declaredService(lister *fakeLister, images *fakeImages) *Service {
	return &Service{
		cfg:            &config.Config{APImage: "archipelago:latest"},
		log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		images:         images,
		declaredLister: lister,
	}
}

func TestOfficialDeclaredFiles_listsOncePerImage(t *testing.T) {
	lister := &fakeLister{out: listing}
	images := &fakeImages{id: "sha256:a"}
	svc := declaredService(lister, images)

	games, image, err := svc.OfficialDeclaredFiles(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if image != "sha256:a" || len(games["Ocarina of Time"]) != 1 || games["Ocarina of Time"][0].MD5s[0] != "5bd1fe107bf8106b2ab6650abecd54d6" {
		t.Fatalf("unexpected listing %v %q", games, image)
	}
	_, _, _ = svc.OfficialDeclaredFiles(context.Background())
	if lister.calls != 1 {
		t.Fatalf("listed %d times for one image, want 1", lister.calls)
	}

	images.id = "sha256:b"
	_, _, _ = svc.OfficialDeclaredFiles(context.Background())
	if lister.calls != 2 {
		t.Fatalf("a new image must be listed again (calls %d)", lister.calls)
	}
}

func TestOfficialDeclaredFiles_aFailureIsNotCached(t *testing.T) {
	lister := &fakeLister{err: errors.New("boom")}
	svc := declaredService(lister, &fakeImages{id: "sha256:a"})

	if _, _, err := svc.OfficialDeclaredFiles(context.Background()); err == nil {
		t.Fatal("expected the listing error")
	}
	lister.err, lister.out = nil, listing
	if games, _, err := svc.OfficialDeclaredFiles(context.Background()); err != nil || len(games) != 1 {
		t.Fatalf("a later call must list again: %v %v", games, err)
	}
}

func TestParseIntrospectedDeclaredFiles(t *testing.T) {
	got := parseIntrospectedDeclaredFiles([]byte(`{"options": {}, "requiredFiles": [{"setting": "DK3Settings.rom_file", "file": "dkc3.sfc", "extension": ".sfc"}]}`))
	if len(got) != 1 || got[0].Setting != "DK3Settings.rom_file" {
		t.Fatalf("unexpected %v", got)
	}
	if got := parseIntrospectedDeclaredFiles([]byte(`{"options": {}}`)); got == nil || len(got) != 0 {
		t.Fatalf("a sidecar predating story 44.3 declares nothing, got %v", got)
	}
}

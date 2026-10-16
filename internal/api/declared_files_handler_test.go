package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"archilan.fr/orchestrateur/internal/service"
)

type fakeDeclared struct{ err error }

func (fakeDeclared) GetApworldDeclaredFiles(context.Context, string) []service.DeclaredFile {
	return []service.DeclaredFile{{Setting: "DK3Settings.rom_file", File: "dkc3.sfc", Extension: ".sfc"}}
}

func (f fakeDeclared) OfficialDeclaredFiles(context.Context) (map[string][]service.DeclaredFile, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return map[string][]service.DeclaredFile{"Ocarina of Time": {{Setting: "OOTSettings.rom_file"}}}, "sha256:a", nil
}

// Story 44.3: the files a world declares, for an apworld and for the image's own worlds.
func TestDeclaredFilesRoutes(t *testing.T) {
	rec := httptest.NewRecorder()
	handleGetApworldDeclaredFiles(fakeDeclared{})(rec, httptest.NewRequest(http.MethodGet, "/apworlds/abc/required-files", nil))
	var apworld DeclaredFilesResponse
	_ = json.NewDecoder(rec.Body).Decode(&apworld)
	if rec.Code != http.StatusOK || len(apworld.RequiredFiles) != 1 || apworld.RequiredFiles[0].Setting != "DK3Settings.rom_file" {
		t.Fatalf("apworld: %d %+v", rec.Code, apworld)
	}

	rec = httptest.NewRecorder()
	handleOfficialDeclaredFiles(fakeDeclared{})(rec, httptest.NewRequest(http.MethodGet, "/runtime/required-files", nil))
	var official OfficialDeclaredFilesResponse
	_ = json.NewDecoder(rec.Body).Decode(&official)
	if rec.Code != http.StatusOK || official.Image != "sha256:a" || len(official.Games["Ocarina of Time"]) != 1 {
		t.Fatalf("official: %d %+v", rec.Code, official)
	}

	rec = httptest.NewRecorder()
	handleOfficialDeclaredFiles(fakeDeclared{err: errors.New("boom")})(rec, httptest.NewRequest(http.MethodGet, "/runtime/required-files", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a failed listing must be 503, got %d", rec.Code)
	}
}

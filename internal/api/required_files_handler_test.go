package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"archilan.fr/orchestrateur/internal/service"
)

type fakeStager struct {
	setting, filename string
	data              []byte
	err               error
	released          []string
}

func (f *fakeStager) StageRequiredFile(setting, filename string, data []byte) (string, time.Time, error) {
	f.setting, f.filename, f.data = setting, filename, data
	return "abc", time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC), f.err
}

func (f *fakeStager) ReleaseRequiredFile(id string) { f.released = append(f.released, id) }

func stageRequest(t *testing.T, setting string, file []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if setting != "" {
		_ = mw.WriteField("setting", setting)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile("file", "dkc3.sfc")
		_, _ = fw.Write(file)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/required-files", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// Story 44.2: the launcher's file is held and named by an id.
func TestStageRequiredFile_returnsTheStagedID(t *testing.T) {
	stager := &fakeStager{}
	rec := httptest.NewRecorder()

	handleStageRequiredFile(stager)(rec, stageRequest(t, "DK3Settings.rom_file", []byte("rom")))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var body StagedRequiredFileResponse
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.StagedFileID != "abc" || body.ExpiresAt != "2026-10-15T12:00:00Z" {
		t.Errorf("body = %+v", body)
	}
	if stager.setting != "DK3Settings.rom_file" || stager.filename != "dkc3.sfc" || string(stager.data) != "rom" {
		t.Errorf("staged %q %q %q", stager.setting, stager.filename, stager.data)
	}
}

func TestStageRequiredFile_refusesAnOversizedFile(t *testing.T) {
	rec := httptest.NewRecorder()

	handleStageRequiredFile(&fakeStager{})(rec, stageRequest(t, "DK3Settings.rom_file", make([]byte, service.RequiredFileMaxBytes+1)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestStageRequiredFile_needsTheSettingAndTheFile(t *testing.T) {
	for name, req := range map[string]*http.Request{
		"no setting": stageRequest(t, "", []byte("rom")),
		"no file":    stageRequest(t, "DK3Settings.rom_file", nil),
		"json":       httptest.NewRequest(http.MethodPost, "/required-files", bytes.NewBufferString("{}")),
	} {
		rec := httptest.NewRecorder()
		handleStageRequiredFile(&fakeStager{})(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func TestStageRequiredFile_mapsTheServiceRefusals(t *testing.T) {
	cases := map[error]int{
		service.ErrInvalidRequiredFile: http.StatusBadRequest,
		service.ErrRequiredFilesFull:   http.StatusServiceUnavailable,
	}
	for err, want := range cases {
		rec := httptest.NewRecorder()
		handleStageRequiredFile(&fakeStager{err: err})(rec, stageRequest(t, "DK3Settings.rom_file", []byte("rom")))
		if rec.Code != want {
			t.Errorf("%v: status = %d, want %d", err, rec.Code, want)
		}
	}
}

func TestReleaseRequiredFile_isIdempotent(t *testing.T) {
	stager := &fakeStager{}
	for range 2 {
		rec := httptest.NewRecorder()
		handleReleaseRequiredFile(stager)(rec, httptest.NewRequest(http.MethodDelete, "/required-files/abc", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	}
}

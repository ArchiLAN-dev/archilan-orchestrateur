package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"archilan.fr/orchestrateur/internal/service"
	"archilan.fr/orchestrateur/internal/storage"
	"github.com/go-chi/chi/v5"
)

type fakePreflights struct {
	ids     []string
	hash    string
	yaml    string
	err     error
	started bool
}

func (f *fakePreflights) GetApworldMetaByHash(context.Context, string) (storage.ApworldMeta, error) {
	return storage.ApworldMeta{}, nil
}

func (f *fakePreflights) StartApworldPreflight(hash string, ids []string) error {
	f.hash, f.ids, f.started = hash, ids, f.err == nil
	return f.err
}

func (f *fakePreflights) StartSlotPreflight(yaml []byte, hash string, ids []string) (string, error) {
	f.yaml, f.hash, f.ids = string(yaml), hash, ids
	if f.err != nil {
		return "", f.err
	}
	return "job1", nil
}

func preflightRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/apworlds/abc/preflight", bytes.NewBufferString(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("hash", "abc")
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// Story 44.6: the admin's re-check carries the ids of the files it was given.
func TestRunApworldPreflight_passesTheStagedFiles(t *testing.T) {
	svc := &fakePreflights{}
	rec := httptest.NewRecorder()

	handleRunApworldPreflight(svc)(rec, preflightRequest(`{"requiredFileIds":["f1"]}`))

	if rec.Code != http.StatusAccepted || !slices.Equal(svc.ids, []string{"f1"}) {
		t.Fatalf("status = %d, ids = %v", rec.Code, svc.ids)
	}
}

// The re-check without a body still works as before (upload hook, rolling test).
func TestRunApworldPreflight_withoutABodyStartsWithoutFiles(t *testing.T) {
	svc := &fakePreflights{}
	rec := httptest.NewRecorder()

	handleRunApworldPreflight(svc)(rec, preflightRequest(""))

	if rec.Code != http.StatusAccepted || !svc.started || len(svc.ids) != 0 {
		t.Fatalf("status = %d, started = %v, ids = %v", rec.Code, svc.started, svc.ids)
	}
}

func TestRunApworldPreflight_anExpiredFileAsksToSendItAgain(t *testing.T) {
	svc := &fakePreflights{err: service.ErrRequiredFileExpired}
	rec := httptest.NewRecorder()

	handleRunApworldPreflight(svc)(rec, preflightRequest(`{"requiredFileIds":["gone"]}`))

	var body ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusUnprocessableEntity || body.Error != "required_file_expired" {
		t.Fatalf("status = %d, body = %+v", rec.Code, body)
	}
}

func TestStartSlotPreflight_passesTheStagedFiles(t *testing.T) {
	svc := &fakePreflights{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/preflight-generations", bytes.NewBufferString(`{"playerYaml":"game: X","apworldHash":"abc","requiredFileIds":["f1"]}`))

	handleStartSlotPreflight(svc)(rec, req)

	if rec.Code != http.StatusAccepted || !slices.Equal(svc.ids, []string{"f1"}) || svc.hash != "abc" {
		t.Fatalf("status = %d, svc = %+v", rec.Code, svc)
	}
}

func TestStartSlotPreflight_tooManyFilesIsABadRequest(t *testing.T) {
	svc := &fakePreflights{err: service.ErrTooManyRequiredFiles}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/preflight-generations", bytes.NewBufferString(`{"playerYaml":"game: X","requiredFileIds":["a","b","c","d","e"]}`))

	handleStartSlotPreflight(svc)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

// The verdict tells a pass obtained with the admin's file apart.
func TestTheListedVerdictSaysItRanWithAFile(t *testing.T) {
	got := apiPreflight(&storage.ApworldPreflight{Status: "passed", WithRequiredFiles: true})

	if !got.WithRequiredFiles {
		t.Errorf("expected withRequiredFiles, got %+v", got)
	}
}

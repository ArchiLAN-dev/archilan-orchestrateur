package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"archilan.fr/orchestrateur/internal/config"
	"archilan.fr/orchestrateur/internal/service"
)

type fakeLogs struct {
	gotSession, gotKind string
	gotTail             int
	logs                service.SessionLogs
	err                 error
}

func (f *fakeLogs) SessionLogs(_ context.Context, sessionID, kind string, tail int) (service.SessionLogs, error) {
	f.gotSession, f.gotKind, f.gotTail = sessionID, kind, tail
	if f.err != nil {
		return service.SessionLogs{}, f.err
	}
	if _, err := service.SessionContainerName(sessionID, kind); err != nil {
		return service.SessionLogs{}, err
	}
	return f.logs, nil
}

func serveLogs(src sessionLogSource, target string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Get("/sessions/{sessionId}/logs", handleSessionLogs(src))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// Story 17.33: the admin reads the server's logs, the last 200 lines by default.
func TestSessionLogsReturnsTheContainerLogs(t *testing.T) {
	src := &fakeLogs{logs: service.SessionLogs{Container: "ap-server-s1", Found: true, Logs: "hosting\n"}}

	rec := serveLogs(src, "/sessions/s1/logs?container=ap")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body SessionLogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body != (SessionLogsResponse{Container: "ap-server-s1", Found: true, Logs: "hosting\n"}) {
		t.Errorf("unexpected body %+v", body)
	}
	if src.gotSession != "s1" || src.gotKind != "ap" || src.gotTail != 200 {
		t.Errorf("unexpected call %q %q %d", src.gotSession, src.gotKind, src.gotTail)
	}
}

func TestSessionLogsPassesTheTail(t *testing.T) {
	src := &fakeLogs{logs: service.SessionLogs{Container: "archilan-bridge-s1"}}

	serveLogs(src, "/sessions/s1/logs?container=bridge&tail=50")

	if src.gotTail != 50 {
		t.Errorf("expected tail 50, got %d", src.gotTail)
	}
}

// A missing container is an empty answer, not a 404 a client would read as a missing session.
func TestSessionLogsOfAMissingContainerIsNotAnError(t *testing.T) {
	rec := serveLogs(&fakeLogs{logs: service.SessionLogs{Container: "archilan-gen-s1", Found: false}}, "/sessions/s1/logs?container=gen")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"container\":\"archilan-gen-s1\",\"found\":false,\"logs\":\"\"}\n" {
		t.Errorf("unexpected body %s", got)
	}
}

func TestSessionLogsRefusesAnUnknownContainer(t *testing.T) {
	for _, target := range []string{"/sessions/s1/logs", "/sessions/s1/logs?container=db"} {
		rec := serveLogs(&fakeLogs{}, target)
		if rec.Code != http.StatusBadRequest || !json.Valid(rec.Body.Bytes()) || rec.Body.String() != "{\"error\":\"invalid_container\"}\n" {
			t.Errorf("%s: expected 400 invalid_container, got %d %s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestSessionLogsRefusesATailOutOfBounds(t *testing.T) {
	for _, tail := range []string{"0", "2001", "abc"} {
		rec := serveLogs(&fakeLogs{}, "/sessions/s1/logs?container=ap&tail="+tail)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("tail=%s: expected 400, got %d", tail, rec.Code)
		}
	}
}

func TestSessionLogsReportsADockerFailure(t *testing.T) {
	rec := serveLogs(&fakeLogs{err: errors.New("docker down")}, "/sessions/s1/logs?container=ap")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// The route sits behind the API key, like every route but /health.
func TestSessionLogsRouteRequiresTheAPIKey(t *testing.T) {
	rec := httptest.NewRecorder()

	NewRouter(&config.Config{APIKey: "secret"}, &service.Service{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/s1/logs?container=ap", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without the key, got %d", rec.Code)
	}
}

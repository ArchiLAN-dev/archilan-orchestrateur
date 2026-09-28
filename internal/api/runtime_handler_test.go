package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"archilan.fr/orchestrateur/internal/config"
	"archilan.fr/orchestrateur/internal/service"
	"archilan.fr/orchestrateur/internal/storage"
)

type fakeRuntime struct{ info service.RuntimeInfo }

func (f fakeRuntime) Runtime(context.Context) service.RuntimeInfo { return f.info }

// Story 38.8: the API asks which Archipelago image is in use.
func TestRuntimeHandlerReturnsImageAndID(t *testing.T) {
	rec := httptest.NewRecorder()

	handleRuntime(fakeRuntime{info: service.RuntimeInfo{APImage: "ghcr.io/archilan-dev/archipelago:0.16.1", APImageID: "sha256:abc123"}})(
		rec, httptest.NewRequest(http.MethodGet, "/runtime", nil),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body RuntimeResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.APImage != "ghcr.io/archilan-dev/archipelago:0.16.1" || body.APImageID != "sha256:abc123" {
		t.Errorf("unexpected body %+v", body)
	}
}

// The route exists and sits behind the API key, like every route but /health.
func TestRuntimeRouteRequiresTheAPIKey(t *testing.T) {
	rec := httptest.NewRecorder()

	NewRouter(&config.Config{APIKey: "secret"}, &service.Service{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without the key, got %d", rec.Code)
	}
}

// Story 38.8: the listed verdict tells which image produced it; an older one has none.
func TestTheListedVerdictCarriesItsImage(t *testing.T) {
	got := apiPreflight(&storage.ApworldPreflight{Status: "passed", Image: "archipelago:0.16.1", ImageID: "sha256:abc123"})

	if got.Image != "archipelago:0.16.1" || got.ImageID != "sha256:abc123" {
		t.Errorf("expected the image on the verdict, got %+v", got)
	}

	raw, _ := json.Marshal(apiPreflight(&storage.ApworldPreflight{Status: "passed"}))
	if string(raw) != `{"status":"passed","overridden":false}` {
		t.Errorf("a verdict without image must not invent one, got %s", raw)
	}
}

// Story 38.12: a pass the generator warned about says so to the central API.
func TestTheListedVerdictCarriesItsWarning(t *testing.T) {
	got := apiPreflight(&storage.ApworldPreflight{Status: "passed", Warning: "Missing: [A]"})

	if got.Warning != "Missing: [A]" {
		t.Errorf("expected the warning on the verdict, got %+v", got)
	}
}

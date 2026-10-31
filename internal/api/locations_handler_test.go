package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"archilan.fr/orchestrateur/internal/service"
)

// Story 38.31: the locations endpoint tells a world without locations apart from an introspection that is missing
// or unreadable.

type fakeLocations struct {
	locations []string
	err       error
}

func (f fakeLocations) GetApworldLocations(context.Context, string) ([]string, error) {
	return f.locations, f.err
}

func locationsStatus(t *testing.T, svc apworldLocationsReader) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/apworlds/{hash}/locations", handleGetApworldLocations(svc))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apworlds/abc/locations", nil))
	return rec.Code, rec.Body.String()
}

func TestLocations_aWorldWithoutLocationsIsAnEmptyList(t *testing.T) {
	code, body := locationsStatus(t, fakeLocations{locations: []string{}})

	if code != http.StatusOK || body != "{\"locations\":[]}\n" {
		t.Fatalf("code = %d, body = %q", code, body)
	}
}

func TestLocations_neverIntrospectedIs404(t *testing.T) {
	if code, _ := locationsStatus(t, fakeLocations{err: service.ErrIntrospectionNotFound}); code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

func TestLocations_unreadableIs500(t *testing.T) {
	err := fmt.Errorf("%w: invalid character", service.ErrIntrospectionUnreadable)
	if code, _ := locationsStatus(t, fakeLocations{err: err}); code != http.StatusInternalServerError {
		t.Fatalf("code = %d", code)
	}
}

func TestLocations_noStorageIs503(t *testing.T) {
	if code, _ := locationsStatus(t, fakeLocations{err: service.ErrStorageNotConfigured}); code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", code)
	}
}

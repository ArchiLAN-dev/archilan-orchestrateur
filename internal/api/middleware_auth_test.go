package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"archilan.fr/orchestrateur/internal/config"
)

func TestDocsNeedTheAPIKey(t *testing.T) {
	router := NewRouter(&config.Config{APIKey: "secret", CORSOrigins: []string{"https://archilan.fr"}}, nil)

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/docs/index.html", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /docs = %d, want 401", anonymous.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/docs/index.html", nil)
	request.Header.Set("Authorization", "Bearer secret")
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, request)
	if authorized.Code == http.StatusUnauthorized {
		t.Errorf("/docs with the key = %d", authorized.Code)
	}
}

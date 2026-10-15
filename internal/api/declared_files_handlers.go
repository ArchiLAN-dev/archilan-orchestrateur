package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"archilan.fr/orchestrateur/internal/service"
)

// declaredFilesSource is what the declared-file routes need from the service (story 44.3).
type declaredFilesSource interface {
	GetApworldDeclaredFiles(ctx context.Context, hash string) []service.DeclaredFile
	OfficialDeclaredFiles(ctx context.Context) (map[string][]service.DeclaredFile, string, error)
}

// handleGetApworldDeclaredFiles godoc
// @Summary     Files an apworld declares
// @Description Story 44.3: the host.yaml files (a ROM...) the apworld's world declares, read at introspection.
// @Description A superset of what a generation needs. Empty until the apworld is (re)introspected.
// @Tags        apworlds
// @Param       hash path string true "Apworld SHA-256 hash"
// @Produce     json
// @Success     200 {object} DeclaredFilesResponse
// @Security    BearerAuth
// @Router      /apworlds/{hash}/required-files [get]
func handleGetApworldDeclaredFiles(src declaredFilesSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, DeclaredFilesResponse{RequiredFiles: src.GetApworldDeclaredFiles(r.Context(), chi.URLParam(r, "hash"))})
	}
}

// handleOfficialDeclaredFiles godoc
// @Summary     Files the image's own worlds declare
// @Description Story 44.3: by game name, the host.yaml files the worlds shipped with the Archipelago image
// @Description declare (A Link to the Past, Ocarina of Time...). Listed once per image in a throwaway container.
// @Tags        system
// @Produce     json
// @Success     200 {object} OfficialDeclaredFilesResponse
// @Failure     503 {object} ErrorResponse "The image could not list them"
// @Security    BearerAuth
// @Router      /runtime/required-files [get]
func handleOfficialDeclaredFiles(src declaredFilesSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		games, image, err := src.OfficialDeclaredFiles(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, OfficialDeclaredFilesResponse{Image: image, Games: games})
	}
}

package api

import (
	"context"
	"net/http"

	"archilan.fr/orchestrateur/internal/service"
)

// runtimeSource is what the runtime handler needs from the service, so it can be tested alone.
type runtimeSource interface {
	Runtime(ctx context.Context) service.RuntimeInfo
}

// handleRuntime godoc
// @Summary     Archipelago image in use
// @Description Returns the Archipelago image the orchestrator runs: the configured reference
// @Description (AP_IMAGE) and the id of the local image it points to. The id is empty when the
// @Description image could not be inspected. Apworld verdicts carry the same pair (story 38.8).
// @Tags        system
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} RuntimeResponse
// @Failure     401 {object} ErrorResponse
// @Router      /runtime [get]
func handleRuntime(src runtimeSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := src.Runtime(r.Context())
		writeJSON(w, http.StatusOK, RuntimeResponse{APImage: info.APImage, APImageID: info.APImageID})
	}
}

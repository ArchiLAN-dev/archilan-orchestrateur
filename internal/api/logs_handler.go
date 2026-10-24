package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"archilan.fr/orchestrateur/internal/service"
)

const (
	defaultLogTail = 200
	maxLogTail     = 2000
)

// sessionLogSource is what the logs handler needs from the service, so it can be tested alone.
type sessionLogSource interface {
	SessionLogs(ctx context.Context, sessionID, kind string, tail int) (service.SessionLogs, error)
}

// SessionLogsResponse is returned by GET /sessions/{sessionId}/logs.
type SessionLogsResponse struct {
	Container string `json:"container" example:"ap-server-abc123"`
	Found     bool   `json:"found" example:"true"`
	Logs      string `json:"logs" example:"2026-10-24T10:00:00.000000000Z Hosting game at 0.0.0.0:38281"`
}

// handleSessionLogs godoc
// @Summary     Logs of a session container
// @Description Returns the last lines of one of the session's containers (story 17.33): the Archipelago server
// @Description (ap), the bridge (bridge) or the generation (gen). stdout and stderr are interleaved in order,
// @Description each line prefixed with Docker's timestamp. A container that does not exist is not an error:
// @Description found is false and logs is empty.
// @Tags        sessions
// @Produce     json
// @Security    BearerAuth
// @Param       sessionId path  string true  "Session ID"
// @Param       container query string true  "Container kind" Enums(ap, bridge, gen)
// @Param       tail      query int    false "Number of lines (1-2000, default 200)"
// @Success     200 {object} SessionLogsResponse
// @Failure     400 {object} ErrorResponse
// @Failure     401 {object} ErrorResponse
// @Failure     500 {object} ErrorResponse
// @Router      /sessions/{sessionId}/logs [get]
func handleSessionLogs(src sessionLogSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tail := defaultLogTail
		if raw := r.URL.Query().Get("tail"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > maxLogTail {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_tail"})
				return
			}
			tail = n
		}

		logs, err := src.SessionLogs(r.Context(), chi.URLParam(r, "sessionId"), r.URL.Query().Get("container"), tail)
		if errors.Is(err, service.ErrInvalidContainer) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_container"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, SessionLogsResponse{Container: logs.Container, Found: logs.Found, Logs: logs.Logs})
	}
}

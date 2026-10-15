package api

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"archilan.fr/orchestrateur/internal/service"
)

// requiredFileStager is the part of the service the required-file routes need (story 44.2).
type requiredFileStager interface {
	StageRequiredFile(setting, filename string, data []byte) (string, time.Time, error)
	ReleaseRequiredFile(id string)
}

// maxSettingBytes bounds the setting form field (Group.key).
const maxSettingBytes = 256

// handleStageRequiredFile godoc
// @Summary     Hold a file a world reads at generation
// @Description Story 44.2: a ROM the launcher provides. Held in memory only, for 30 minutes, until the
// @Description generation that names its id consumes it; copied into that generation container only, never
// @Description stored. Send the setting field before the file.
// @Tags        required-files
// @Accept      multipart/form-data
// @Produce     json
// @Param       setting formData string true "host.yaml setting the failure named, e.g. DK3Settings.rom_file"
// @Param       file    formData file   true "The file (64 MiB max)"
// @Success     201 {object} StagedRequiredFileResponse
// @Failure     400 {object} ErrorResponse
// @Failure     413 {object} ErrorResponse
// @Failure     503 {object} ErrorResponse "Too many files held"
// @Security    BearerAuth
// @Router      /required-files [post]
func handleStageRequiredFile(svc requiredFileStager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A hard cap on the whole body: ParseMultipartForm would spill the file to a temporary file on disk.
		r.Body = http.MaxBytesReader(w, r.Body, service.RequiredFileMaxBytes+(1<<20))
		reader, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, "multipart form expected")
			return
		}

		var setting, filename string
		var data []byte
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeStageReadError(w, err)
				return
			}
			switch part.FormName() {
			case "setting":
				raw, err := io.ReadAll(io.LimitReader(part, maxSettingBytes))
				if err != nil {
					writeStageReadError(w, err)
					return
				}
				setting = string(raw)
			case "file":
				filename = part.FileName()
				data, err = io.ReadAll(io.LimitReader(part, service.RequiredFileMaxBytes+1))
				if err != nil {
					writeStageReadError(w, err)
					return
				}
				if len(data) > service.RequiredFileMaxBytes {
					writeError(w, http.StatusRequestEntityTooLarge, "file too large")
					return
				}
			}
			_ = part.Close()
		}
		if setting == "" || len(data) == 0 {
			writeError(w, http.StatusBadRequest, "setting and file are required")
			return
		}

		id, expiresAt, err := svc.StageRequiredFile(setting, filename, data)
		switch {
		case errors.Is(err, service.ErrInvalidRequiredFile):
			writeError(w, http.StatusBadRequest, "invalid setting or file")
			return
		case errors.Is(err, service.ErrRequiredFilesFull):
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "required_files_full"})
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, StagedRequiredFileResponse{
			StagedFileID: id,
			ExpiresAt:    expiresAt.UTC().Format(time.RFC3339),
		})
	}
}

func writeStageReadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid multipart form")
}

// handleReleaseRequiredFile godoc
// @Summary     Drop a held file
// @Description Story 44.2: the launch it was held for was abandoned. Idempotent.
// @Tags        required-files
// @Param       id path string true "Staged file id"
// @Success     204
// @Security    BearerAuth
// @Router      /required-files/{id} [delete]
func handleReleaseRequiredFile(svc requiredFileStager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc.ReleaseRequiredFile(chi.URLParam(r, "id"))
		w.WriteHeader(http.StatusNoContent)
	}
}

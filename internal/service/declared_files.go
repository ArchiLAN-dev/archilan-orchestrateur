package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Files the worlds declare they read (story 44.3), statically, without generating: almost always a ROM.
// A superset of what a generation needs - most only serve the client - so the API only demands one a
// failed generation proved necessary. Same shape as the requiredFile record of a failure (story 44.1).

// DeclaredFile is one file a world declares in its host.yaml settings.
type DeclaredFile struct {
	Setting     string   `json:"setting"` // Group.key, e.g. DK3Settings.rom_file
	File        string   `json:"file"`
	Extension   string   `json:"extension"`
	Description string   `json:"description,omitempty"`
	MD5s        []string `json:"md5s,omitempty"`
}

// declaredFilesLister runs the image's listing of the files its own worlds declare.
type declaredFilesLister interface {
	ListRequiredFiles(ctx context.Context) ([]byte, error)
}

// officialDeclaredFiles caches the listing per image: it only changes with the image.
type officialDeclaredFiles struct {
	mu      sync.Mutex
	imageID string
	games   map[string][]DeclaredFile
}

// GetApworldDeclaredFiles returns the files an uploaded apworld declares, read from its introspection
// sidecar. Empty when storage is off, the sidecar is missing, or it predates story 44.3.
func (s *Service) GetApworldDeclaredFiles(ctx context.Context, hash string) []DeclaredFile {
	if s.storage == nil {
		return []DeclaredFile{}
	}
	raw, found, err := s.storage.DownloadApworldOptionTypes(ctx, hash)
	if err != nil || !found {
		return []DeclaredFile{}
	}
	return parseIntrospectedDeclaredFiles(raw)
}

func parseIntrospectedDeclaredFiles(raw []byte) []DeclaredFile {
	var parsed struct {
		RequiredFiles []DeclaredFile `json:"requiredFiles"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.RequiredFiles == nil {
		return []DeclaredFile{}
	}
	return parsed.RequiredFiles
}

// OfficialDeclaredFiles returns, by game name, the files the worlds shipped with the image declare, and
// the id of that image. Computed once per image in a throwaway container; concurrent callers wait for it.
func (s *Service) OfficialDeclaredFiles(ctx context.Context) (map[string][]DeclaredFile, string, error) {
	imageID := s.Runtime(ctx).APImageID

	s.officialFiles.mu.Lock()
	defer s.officialFiles.mu.Unlock()
	if s.officialFiles.games != nil && imageID != "" && s.officialFiles.imageID == imageID {
		return s.officialFiles.games, imageID, nil
	}

	raw, err := s.declaredLister.ListRequiredFiles(ctx)
	if err != nil {
		return nil, imageID, fmt.Errorf("list declared files: %w", err)
	}
	games := map[string][]DeclaredFile{}
	if err := json.Unmarshal(raw, &games); err != nil {
		return nil, imageID, fmt.Errorf("decode declared files: %w", err)
	}
	// An image that could not be inspected is listed every time: nothing tells when it changes.
	if imageID != "" {
		s.officialFiles.imageID = imageID
		s.officialFiles.games = games
	}
	return games, imageID, nil
}

package service

import (
	"context"
	"fmt"
)

// SetApworldTemplate replaces the YAML template stored next to an apworld (story 9.45).
// The central API is the source of truth for what players receive; keeping the stored copy
// in sync matters because the upload preflight tests THIS file - otherwise a template an
// admin fixed would keep failing its own check.
func (s *Service) SetApworldTemplate(ctx context.Context, hash string, template []byte) error {
	if s.storage == nil {
		return ErrStorageNotConfigured
	}
	if len(template) == 0 {
		return fmt.Errorf("empty template")
	}
	// Make sure the apworld exists before writing a template beside it.
	if _, err := s.storage.GetApworldMeta(ctx, hash); err != nil {
		return fmt.Errorf("unknown apworld %s: %w", hash, err)
	}

	return s.storage.UploadApworldTemplate(ctx, hash, template)
}

// RegenerateApworldTemplate re-runs template generation against the apworld already in
// storage and replaces the stored template (story 9.46). Used to undo an edit and to repair
// games whose template failed at upload - the template is otherwise produced only once.
//
// On failure nothing is written: a world that cannot produce a template must not blank an
// existing one. The error carries the generator's stderr so the caller can show why.
func (s *Service) RegenerateApworldTemplate(ctx context.Context, hash string) ([]byte, error) {
	if s.storage == nil {
		return nil, ErrStorageNotConfigured
	}

	data, err := s.storage.DownloadApworld(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("download apworld %s: %w", hash, err)
	}

	template, err := s.docker.GenerateTemplate(ctx, data, hash)
	if err != nil {
		return nil, fmt.Errorf("regenerate template: %w", err)
	}
	if len(template) == 0 {
		return nil, fmt.Errorf("regenerate template: generator produced an empty template")
	}

	if err := s.storage.UploadApworldTemplate(ctx, hash, template); err != nil {
		return nil, fmt.Errorf("store regenerated template: %w", err)
	}

	return template, nil
}

// ReintrospectApworldOptions re-runs option introspection against the apworld already in storage
// and replaces the stored types sidecar (story 9.53).
//
// Introspection otherwise happens exactly once, in the background goroutine of UploadApworld, so a
// world introspected by an older image keeps its old answer forever. The only way to refresh it was
// to re-upload the same bytes the server already holds - which is what this replaces.
//
// On failure nothing is written. The sidecar carries more than the newest field: range bounds,
// option types and the location list all live in it, so blanking it on a bad run would cost the
// editor far more than the refresh was worth.
func (s *Service) ReintrospectApworldOptions(ctx context.Context, hash string) ([]byte, error) {
	if s.storage == nil {
		return nil, ErrStorageNotConfigured
	}

	data, err := s.storage.DownloadApworld(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("download apworld %s: %w", hash, err)
	}

	typesJSON, err := s.docker.IntrospectOptions(ctx, data, hash)
	if err != nil {
		return nil, fmt.Errorf("reintrospect options: %w", err)
	}
	if len(typesJSON) == 0 {
		return nil, fmt.Errorf("reintrospect options: introspection produced an empty result")
	}

	if err := s.storage.UploadApworldOptionTypes(ctx, hash, typesJSON); err != nil {
		return nil, fmt.Errorf("store introspected options: %w", err)
	}

	return typesJSON, nil
}

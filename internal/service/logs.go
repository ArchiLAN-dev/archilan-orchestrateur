package service

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidContainer is returned for a container kind other than ap, bridge or gen.
var ErrInvalidContainer = errors.New("invalid container")

// containerLogReader reads a container's logs; the docker client in prod.
type containerLogReader interface {
	Logs(ctx context.Context, name string, tail int) (string, bool, error)
}

// SessionLogs is what an admin reads of one container of a session (story 17.33).
type SessionLogs struct {
	Container string
	Found     bool
	Logs      string
}

// SessionContainerName is the Docker name the orchestrator gives to a session's container of that kind.
func SessionContainerName(sessionID, kind string) (string, error) {
	switch kind {
	case "ap":
		return fmt.Sprintf("ap-server-%s", sessionID), nil
	case "bridge":
		return fmt.Sprintf("archilan-bridge-%s", sessionID), nil
	case "gen":
		return fmt.Sprintf("archilan-gen-%s", sessionID), nil
	}
	return "", ErrInvalidContainer
}

// SessionLogs returns the last tail lines of one of the session's containers. A container that does not exist
// (never launched, or removed since) is not an error: Found is false.
func (s *Service) SessionLogs(ctx context.Context, sessionID, kind string, tail int) (SessionLogs, error) {
	name, err := SessionContainerName(sessionID, kind)
	if err != nil {
		return SessionLogs{}, err
	}
	logs, found, err := s.logs.Logs(ctx, name, tail)
	if err != nil {
		return SessionLogs{}, fmt.Errorf("read logs of %s: %w", name, err)
	}
	return SessionLogs{Container: name, Found: found, Logs: logs}, nil
}

package service

import (
	"context"
	"errors"
	"testing"
)

type fakeLogReader struct {
	gotName string
	gotTail int
	logs    string
	found   bool
}

func (f *fakeLogReader) Logs(_ context.Context, name string, tail int) (string, bool, error) {
	f.gotName, f.gotTail = name, tail
	return f.logs, f.found, nil
}

// Story 17.33: each kind reads the container the orchestrator really created for the session.
func TestSessionLogsReadsTheContainerOfThatKind(t *testing.T) {
	for kind, want := range map[string]string{"ap": "ap-server-s1", "bridge": "archilan-bridge-s1", "gen": "archilan-gen-s1"} {
		reader := &fakeLogReader{logs: "line\n", found: true}

		got, err := (&Service{logs: reader}).SessionLogs(context.Background(), "s1", kind, 50)

		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if reader.gotName != want || reader.gotTail != 50 {
			t.Errorf("%s: read %q tail %d, want %q", kind, reader.gotName, reader.gotTail, want)
		}
		if got != (SessionLogs{Container: want, Found: true, Logs: "line\n"}) {
			t.Errorf("%s: unexpected %+v", kind, got)
		}
	}
}

func TestSessionLogsRefusesAnUnknownKindWithoutReadingAnything(t *testing.T) {
	reader := &fakeLogReader{}

	_, err := (&Service{logs: reader}).SessionLogs(context.Background(), "s1", "db", 50)

	if !errors.Is(err, ErrInvalidContainer) {
		t.Errorf("expected ErrInvalidContainer, got %v", err)
	}
	if reader.gotName != "" {
		t.Errorf("nothing should be read, read %q", reader.gotName)
	}
}

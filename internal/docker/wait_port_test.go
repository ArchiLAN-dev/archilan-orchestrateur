package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeExec answers the three calls of an exec; exitCode gives the code of the nth probe.
func fakeExec(t *testing.T, seen *atomic.Value, exitCode func(n int32) int) http.HandlerFunc {
	t.Helper()
	probes := &atomic.Int32{}
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/ap1/exec"):
			var body execCreateBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			seen.Store(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"e1"}`))
		case strings.HasSuffix(r.URL.Path, "/exec/e1/start"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/exec/e1/json"):
			_ = json.NewEncoder(w).Encode(execInspect{ExitCode: exitCode(probes.Add(1))})
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	}
}

// Story 50.6: the orchestrateur shares no network with a session, the probe has to run inside.
func TestWaitForPortProbesFromInsideTheContainer(t *testing.T) {
	seen := &atomic.Value{}
	c, _ := testClient(t, fakeExec(t, seen, func(int32) int { return 0 }))

	if !c.WaitForPort(context.Background(), "ap1", 38281, time.Second) {
		t.Fatal("expected the port to be seen")
	}

	body := seen.Load().(execCreateBody)
	if body.User != "1000:1000" {
		t.Errorf("User = %q, the probe must not run as root", body.User)
	}
	if body.Cmd[1] != "-I" || !strings.Contains(body.Cmd[3], "38281") {
		t.Errorf("Cmd = %v", body.Cmd)
	}
}

func TestWaitForPortRetriesUntilTheServerListens(t *testing.T) {
	seen := &atomic.Value{}
	c, calls := testClient(t, fakeExec(t, seen, func(n int32) int {
		if n == 1 {
			return 1
		}
		return 0
	}))

	if !c.WaitForPort(context.Background(), "ap1", 38281, 5*time.Second) {
		t.Fatal("expected the second probe to succeed")
	}
	if calls.Load() != 6 {
		t.Errorf("calls = %d, want two probes of three calls", calls.Load())
	}
}

func TestWaitForPortGivesUpAtTheDeadline(t *testing.T) {
	seen := &atomic.Value{}
	c, _ := testClient(t, fakeExec(t, seen, func(int32) int { return 1 }))

	if c.WaitForPort(context.Background(), "ap1", 38281, 100*time.Millisecond) {
		t.Fatal("a port nothing listens on must not count as ready")
	}
}

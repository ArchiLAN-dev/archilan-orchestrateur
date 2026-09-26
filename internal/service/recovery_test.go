package service

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"archilan.fr/orchestrateur/internal/config"
	"archilan.fr/orchestrateur/internal/db"
	"archilan.fr/orchestrateur/internal/portpool"
	"archilan.fr/orchestrateur/internal/webhook"
)

type fakeStopper struct{ stopped []string }

func (f *fakeStopper) Stop(_ context.Context, ref string) error {
	f.stopped = append(f.stopped, ref)
	return nil
}

func recoveryService(t *testing.T, sessions ...*db.Session) (*Service, *db.DB, *portpool.Pool, *fakeStopper) {
	t.Helper()
	d, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	now := time.Now().UTC()
	for _, sess := range sessions {
		sess.CreatedAt, sess.UpdatedAt = now, now
		if err := d.InsertSession(sess); err != nil {
			t.Fatalf("insert %s: %v", sess.SessionID, err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := portpool.New(25000, 25009)
	stopper := &fakeStopper{}
	return &Service{
		db:         d,
		pool:       pool,
		containers: stopper,
		webhook:    webhook.New("", "", log),
		cfg:        &config.Config{},
		log:        log,
	}, d, pool, stopper
}

func port(p int) *int { return &p }

// Story 17.26: a launch interrupted by a restart is crashed BEFORE the ports are reserved, so its
// port goes back to the pool instead of staying held by a dead session until the next restart.
// Its containers are stopped by name: a leftover one would keep the host port bound and collide
// with the next session handed that port.
func TestRecoverFromDB_crashesInterruptedLaunchesBeforeReservingPorts(t *testing.T) {
	svc, d, pool, stopper := recoveryService(t,
		&db.Session{SessionID: "live", Status: "running", BridgePort: port(25000), APPort: port(35000)},
		&db.Session{SessionID: "interrupted", Status: "launching", BridgePort: port(25001), APPort: port(35001)},
	)

	if err := svc.RecoverFromDB(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	sess, _ := d.GetSession("interrupted")
	if sess.Status != "crashed" {
		t.Fatalf("interrupted launch status = %q, want crashed", sess.Status)
	}
	for _, name := range []string{"archilan-bridge-interrupted", "ap-server-interrupted"} {
		if !slices.Contains(stopper.stopped, name) {
			t.Errorf("container %s was not stopped (stopped: %v)", name, stopper.stopped)
		}
	}
	if got, _ := pool.Acquire("next"); got != 25001 {
		t.Fatalf("next launch got port %d, want the interrupted launch's freed 25001", got)
	}
}

// A session reset to "generated" for a relaunch still carries its previous bridge_port, which the
// pool may since have handed to a live session. Reserving it at boot would steal that port's
// ownership: the live owner could then never release it.
func TestRecoverFromDB_reservesOnlyRunningSessions(t *testing.T) {
	svc, _, pool, _ := recoveryService(t,
		&db.Session{SessionID: "live", Status: "running", BridgePort: port(25000), APPort: port(35000)},
		&db.Session{SessionID: "stale", Status: "generated", BridgePort: port(25000), APPort: port(35000)},
		&db.Session{SessionID: "idle", Status: "stopped", BridgePort: port(25001), APPort: port(35001)},
	)

	if err := svc.RecoverFromDB(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if pool.ReleaseFor(25000, "stale") {
		t.Fatal("the stale generated session owns the live session's port")
	}
	if !pool.ReleaseFor(25000, "live") {
		t.Fatal("the running session does not own its port after recovery")
	}
	if got, _ := pool.Acquire("next"); got != 25000 {
		t.Fatalf("next launch got port %d, want 25000 (25001 belongs to no live session either)", got)
	}
}

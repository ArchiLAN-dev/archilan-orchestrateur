package service

import (
	"context"
	"testing"
	"time"

	"archilan.fr/orchestrateur/internal/db"
)

// Story 17.27: a paused or crashed session keeps its port for PORT_RESERVATION_TTL, so its relaunch
// reuses the same address; the reservation is recorded, expires, and gives way when the pool is full.

const twoWeeks = 14 * 24 * time.Hour

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func reservations(t *testing.T, d *db.DB, at time.Time) map[string]db.PortReservation {
	t.Helper()
	list, err := d.ActivePortReservations(at)
	if err != nil {
		t.Fatalf("list reservations: %v", err)
	}
	byID := map[string]db.PortReservation{}
	for _, r := range list {
		byID[r.SessionID] = r
	}
	return byID
}

func TestReservePort_keepsThePortForItsSessionUntilTheDeadline(t *testing.T) {
	paused := &db.Session{SessionID: "paused", Status: "stopped", BridgePort: port(25000)}
	svc, d, pool, _ := recoveryService(t, paused)
	svc.cfg.PortReservationTTL = twoWeeks
	pool.Reserve(25000, "paused")

	svc.reservePort(paused, now)

	r, ok := reservations(t, d, now)["paused"]
	if !ok || r.Port != 25000 || !r.Until.Equal(now.Add(twoWeeks)) {
		t.Fatalf("reservation = %+v (found %v), want 25000 until %v", r, ok, now.Add(twoWeeks))
	}
	if other, _ := svc.acquirePort("other", now); other == 25000 {
		t.Fatal("another session got the reserved port")
	}
	if back, _ := svc.acquirePort("paused", now.Add(24*time.Hour)); back != 25000 {
		t.Fatalf("relaunch got %d, want its reserved 25000", back)
	}
}

func TestReservePort_aZeroTtlKeepsTheFormerBehaviour(t *testing.T) {
	paused := &db.Session{SessionID: "paused", Status: "stopped", BridgePort: port(25000)}
	svc, d, pool, _ := recoveryService(t, paused)
	pool.Reserve(25000, "paused")

	svc.reservePort(paused, now) // cfg.PortReservationTTL is 0

	if len(reservations(t, d, now)) != 0 {
		t.Fatal("a reservation was recorded with a zero TTL")
	}
	if got, _ := svc.acquirePort("other", now); got != 25000 {
		t.Fatalf("next launch got %d, want the released 25000", got)
	}
}

func TestDropPortReservation_givesThePortUp(t *testing.T) {
	paused := &db.Session{SessionID: "paused", Status: "stopped", BridgePort: port(25000)}
	svc, d, pool, _ := recoveryService(t, paused)
	svc.cfg.PortReservationTTL = twoWeeks
	pool.Reserve(25000, "paused")
	svc.reservePort(paused, now)

	svc.dropPortReservation(paused)

	if len(reservations(t, d, now)) != 0 {
		t.Fatal("the reservation is still recorded")
	}
	if got, _ := svc.acquirePort("other", now); got != 25000 {
		t.Fatalf("next launch got %d, want the given-up 25000", got)
	}
}

func TestAcquirePort_aFullPoolTakesBackTheOldestReservationAndForgetsIt(t *testing.T) {
	oldest := &db.Session{SessionID: "oldest", Status: "stopped", BridgePort: port(25000)}
	recent := &db.Session{SessionID: "recent", Status: "stopped", BridgePort: port(25001)}
	svc, d, pool, _ := recoveryService(t, oldest, recent)
	svc.cfg.PortReservationTTL = twoWeeks
	for p := 25002; p <= 25009; p++ { // the harness pool is 25000-25009
		pool.Reserve(p, "live")
	}
	pool.Reserve(25000, "oldest")
	svc.reservePort(oldest, now)
	pool.Reserve(25001, "recent")
	svc.reservePort(recent, now.Add(time.Hour))

	got, err := svc.acquirePort("new", now.Add(2*time.Hour))
	if err != nil || got != 25000 {
		t.Fatalf("acquirePort() = %d, %v; want the oldest reservation's 25000", got, err)
	}
	left := reservations(t, d, now)
	if _, ok := left["oldest"]; ok {
		t.Fatal("the taken-back reservation is still recorded")
	}
	if _, ok := left["recent"]; !ok {
		t.Fatal("the recent reservation was lost")
	}
}

func TestExpirePortReservations_releasesAndForgetsTheExpiredOnes(t *testing.T) {
	old := &db.Session{SessionID: "old", Status: "stopped", BridgePort: port(25000)}
	young := &db.Session{SessionID: "young", Status: "stopped", BridgePort: port(25001)}
	svc, d, pool, _ := recoveryService(t, old, young)
	svc.cfg.PortReservationTTL = twoWeeks
	pool.Reserve(25000, "old")
	svc.reservePort(old, now)
	pool.Reserve(25001, "young")
	svc.reservePort(young, now.Add(7*24*time.Hour))

	at := now.Add(twoWeeks)
	svc.expirePortReservations(at)

	left := reservations(t, d, now)
	if _, ok := left["old"]; ok {
		t.Fatal("the expired reservation is still recorded")
	}
	if _, ok := left["young"]; !ok {
		t.Fatal("the live reservation was dropped")
	}
	if got, _ := svc.acquirePort("next", at); got != 25000 {
		t.Fatalf("next launch got %d, want the expired 25000", got)
	}
}

func TestRecoverFromDB_reloadsTheReservationsOfPausedSessions(t *testing.T) {
	future := time.Now().UTC().Add(twoWeeks)
	svc, d, _, _ := recoveryService(t,
		&db.Session{SessionID: "live", Status: "running", BridgePort: port(25000), APPort: port(35000)},
		&db.Session{SessionID: "paused", Status: "stopped", BridgePort: port(25001), APPort: port(35001)},
		&db.Session{SessionID: "clash", Status: "stopped", BridgePort: port(25000), APPort: port(35000)},
	)
	_ = d.SetPortReservation("paused", future)
	_ = d.SetPortReservation("clash", future) // its port went to "live" meanwhile: cannot be held

	if err := svc.RecoverFromDB(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if got, _ := svc.acquirePort("other", time.Now().UTC()); got == 25001 {
		t.Fatal("the paused session's port was handed to another session after a restart")
	}
	if got, _ := svc.acquirePort("paused", time.Now().UTC()); got != 25001 {
		t.Fatalf("paused relaunch got %d, want its reserved 25001", got)
	}
	if _, ok := reservations(t, d, time.Now().UTC())["clash"]; ok {
		t.Fatal("a reservation on a port now running elsewhere was kept on record")
	}
}

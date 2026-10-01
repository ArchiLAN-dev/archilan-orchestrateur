package db

import (
	"testing"
	"time"
)

// Story 17.27: the port a paused session keeps is recorded with its deadline, so a restart of the
// orchestrateur does not lose it.

func newReservationDB(t *testing.T, sessions ...*Session) *DB {
	t.Helper()
	d, err := New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	now := time.Now().UTC()
	for _, s := range sessions {
		s.CreatedAt, s.UpdatedAt = now, now
		if err := d.InsertSession(s); err != nil {
			t.Fatalf("insert %s: %v", s.SessionID, err)
		}
	}
	return d
}

func intPtr(v int) *int { return &v }

func TestPortReservations_onlyLiveOnesOfPausedOrCrashedSessionsAreListed(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	d := newReservationDB(t,
		&Session{SessionID: "paused", Status: "stopped", BridgePort: intPtr(25001)},
		&Session{SessionID: "crashed", Status: "crashed", BridgePort: intPtr(25002)},
		&Session{SessionID: "expired", Status: "stopped", BridgePort: intPtr(25003)},
		&Session{SessionID: "relaunched", Status: "running", BridgePort: intPtr(25004)},
	)
	for id, until := range map[string]time.Time{
		"paused":     now.Add(48 * time.Hour),
		"crashed":    now.Add(time.Hour),
		"expired":    now.Add(-time.Minute),
		"relaunched": now.Add(time.Hour),
	} {
		if err := d.SetPortReservation(id, until); err != nil {
			t.Fatalf("set %s: %v", id, err)
		}
	}

	got, err := d.ActivePortReservations(now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]PortReservation{}
	for _, r := range got {
		byID[r.SessionID] = r
	}
	if len(got) != 2 || byID["paused"].Port != 25001 || byID["crashed"].Port != 25002 {
		t.Fatalf("ActivePortReservations() = %+v, want paused on 25001 and crashed on 25002", got)
	}
	if !byID["paused"].Until.Equal(now.Add(48 * time.Hour)) {
		t.Fatalf("paused until = %v, want %v", byID["paused"].Until, now.Add(48*time.Hour))
	}
}

func TestPortReservations_clearedOnRelaunchAndOnDemand(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	d := newReservationDB(t,
		&Session{SessionID: "a", Status: "stopped", BridgePort: intPtr(25001)},
		&Session{SessionID: "b", Status: "stopped", BridgePort: intPtr(25002)},
	)
	_ = d.SetPortReservation("a", now.Add(time.Hour))
	_ = d.SetPortReservation("b", now.Add(time.Hour))

	// A relaunch consumes the reservation: the port is in use again, not reserved.
	if err := d.UpdateSessionLaunching("a", 25001, 35001, "", "", now.Add(time.Minute)); err != nil {
		t.Fatalf("launching: %v", err)
	}
	_ = d.UpdateSessionStopped("a")
	if err := d.ClearPortReservation("b"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	got, _ := d.ActivePortReservations(now)
	if len(got) != 0 {
		t.Fatalf("ActivePortReservations() = %+v, want none", got)
	}
}

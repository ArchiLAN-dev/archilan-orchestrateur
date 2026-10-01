package portpool

import (
	"testing"
	"time"
)

// Story 17.27: a paused session keeps its port, reserved until a deadline, and gets it back on relaunch.

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestHold_theOwnerGetsItsPortBackOnRelaunch(t *testing.T) {
	p := New(25000, 25009)
	port, _ := p.Acquire("paused")
	if !p.Hold(port, "paused", t0.Add(14*24*time.Hour)) {
		t.Fatal("Hold() = false, want true for the session using the port")
	}

	// Another launch does not take the held port.
	if other, _ := p.Acquire("other"); other == port {
		t.Fatalf("another session got the held port %d", port)
	}

	got, evicted, err := p.AcquireFor("paused", t0.Add(24*time.Hour))
	if err != nil || got != port || evicted != nil {
		t.Fatalf("AcquireFor(owner) = %d, %v, %v; want %d, nil, nil", got, evicted, err, port)
	}
}

func TestHold_refusesAPortOwnedByAnotherSession(t *testing.T) {
	p := New(25000, 25009)
	port, _ := p.Acquire("live")
	if p.Hold(port, "stale", t0) {
		t.Fatal("Hold() took the port of a running session")
	}
	if !p.ReleaseFor(port, "live") {
		t.Fatal("the running session no longer owns its port")
	}
}

func TestHold_ofAFreePortWorks(t *testing.T) {
	// Boot recovery re-holds ports that the fresh pool has never handed out.
	p := New(25000, 25009)
	if !p.Hold(25003, "paused", t0) {
		t.Fatal("Hold() = false for a free port")
	}
	if got, _, _ := p.AcquireFor("paused", t0.Add(-time.Hour)); got != 25003 {
		t.Fatalf("owner got %d, want 25003", got)
	}
}

func TestReleaseFor_leavesAHoldAlone(t *testing.T) {
	// RelaunchFromSave cleans up a stale *used* entry with ReleaseFor; that must not drop the hold.
	p := New(25000, 25009)
	port, _ := p.Acquire("paused")
	p.Hold(port, "paused", t0.Add(time.Hour))
	p.ReleaseFor(port, "paused")

	if got, _, _ := p.AcquireFor("paused", t0); got != port {
		t.Fatalf("owner got %d after ReleaseFor, want its held %d", got, port)
	}
}

func TestForget_dropsTheHoldOfItsOwnerOnly(t *testing.T) {
	p := New(25000, 25000)
	p.Hold(25000, "paused", t0.Add(time.Hour))

	if p.Forget(25000, "someone-else") {
		t.Fatal("Forget() dropped another session's hold")
	}
	if !p.Forget(25000, "paused") {
		t.Fatal("Forget() = false for the owner")
	}
	if got, evicted, err := p.AcquireFor("next", t0); err != nil || got != 25000 || evicted != nil {
		t.Fatalf("after Forget, AcquireFor = %d, %v, %v; want a plain free 25000", got, evicted, err)
	}
}

func TestExpire_releasesHoldsPastTheirDeadline(t *testing.T) {
	p := New(25000, 25009)
	p.Hold(25000, "old", t0)
	p.Hold(25001, "recent", t0.Add(48*time.Hour))

	expired := p.Expire(t0.Add(time.Hour))

	if len(expired) != 1 || expired[0].Port != 25000 || expired[0].SessionID != "old" {
		t.Fatalf("Expire() = %+v, want only old on 25000", expired)
	}
	if got, _, _ := p.AcquireFor("next", t0.Add(time.Hour)); got != 25000 {
		t.Fatalf("next launch got %d, want the expired 25000", got)
	}
	if got, _, _ := p.AcquireFor("recent", t0.Add(time.Hour)); got != 25001 {
		t.Fatalf("recent got %d, want its still-held 25001", got)
	}
}

func TestAcquireFor_aFullPoolTakesBackTheOldestHold(t *testing.T) {
	p := New(25000, 25002)
	p.Hold(25000, "middle", t0.Add(2*time.Hour))
	p.Hold(25001, "oldest", t0.Add(1*time.Hour))
	if _, err := p.Acquire("live"); err != nil { // 25002, the last free port
		t.Fatalf("Acquire(live) error = %v", err)
	}

	got, evicted, err := p.AcquireFor("new", t0)
	if err != nil {
		t.Fatalf("AcquireFor() error = %v, want the oldest hold taken back", err)
	}
	if got != 25001 || evicted == nil || evicted.SessionID != "oldest" || evicted.Port != 25001 {
		t.Fatalf("AcquireFor() = %d, %+v; want 25001 taken from oldest", got, evicted)
	}
	// The dispossessed session relaunches on a new port, like before the story.
	if back, _, _ := p.AcquireFor("oldest", t0); back == 25001 {
		t.Fatal("the dispossessed session still got its old port")
	}
}

func TestAcquireFor_neverTakesARunningSessionsPort(t *testing.T) {
	p := New(25000, 25001)
	p.Acquire("live-1")
	p.Acquire("live-2")

	if _, _, err := p.AcquireFor("new", t0); err != ErrExhausted {
		t.Fatalf("AcquireFor() error = %v, want ErrExhausted with only running sessions", err)
	}
}

func TestAcquireFor_prefersAFreePortToAnotherSessionsHold(t *testing.T) {
	p := New(25000, 25001)
	p.Hold(25000, "paused", t0.Add(time.Hour))

	got, evicted, _ := p.AcquireFor("new", t0)
	if got != 25001 || evicted != nil {
		t.Fatalf("AcquireFor() = %d, %+v; want the free 25001 and no eviction", got, evicted)
	}
}

package portpool

import "testing"

func TestAcquire_returnsLowestFreePort(t *testing.T) {
	p := New(25000, 25002)
	port, err := p.Acquire("s1")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if port != 25000 {
		t.Fatalf("Acquire() = %d, want 25000", port)
	}
}

func TestAcquire_exhaustedReturnsError(t *testing.T) {
	p := New(25000, 25000)
	if _, err := p.Acquire("s1"); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	if _, err := p.Acquire("s2"); err != ErrExhausted {
		t.Fatalf("second Acquire() error = %v, want ErrExhausted", err)
	}
}

func TestReleaseFor_releasesWhenOwnerMatches(t *testing.T) {
	p := New(25000, 25000)
	port, _ := p.Acquire("s1")
	if !p.ReleaseFor(port, "s1") {
		t.Fatalf("ReleaseFor() = false, want true for owning session")
	}
	// Port must now be re-acquirable.
	if _, err := p.Acquire("s2"); err != nil {
		t.Fatalf("Acquire() after ReleaseFor error = %v, want nil", err)
	}
}

// TestReleaseFor_isNoOpForStaleOwner is the regression guard for the launch-deadline race:
// after the sweeper frees a stuck launch's port and a new session re-Acquires it, the original
// launch goroutine's late release must NOT steal the port from the new owner.
func TestReleaseFor_isNoOpForStaleOwner(t *testing.T) {
	p := New(25000, 25000)
	port, _ := p.Acquire("s1") // s1 launches
	p.ReleaseFor(port, "s1")   // sweeper crashes s1, frees the port
	rePort, err := p.Acquire("s2")
	if err != nil || rePort != port {
		t.Fatalf("re-Acquire = (%d, %v), want (%d, nil)", rePort, err, port)
	}
	// s1's stale goroutine releases late - must be a no-op, port still owned by s2.
	if p.ReleaseFor(port, "s1") {
		t.Fatalf("ReleaseFor() = true for stale owner, want false (must not steal s2's port)")
	}
	if _, err := p.Acquire("s3"); err != ErrExhausted {
		t.Fatalf("Acquire() = %v, want ErrExhausted (port must still belong to s2)", err)
	}
}
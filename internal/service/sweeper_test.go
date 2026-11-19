package service

import (
	"testing"

	"archilan.fr/orchestrateur/internal/db"
	"archilan.fr/orchestrateur/internal/docker"
)

func TestApExitOutcome(t *testing.T) {
	tests := []struct {
		name string
		info *docker.ContainerStatus
		want string
	}{
		{"clean auto_shutdown exits 0 -> idle", &docker.ContainerStatus{Running: false, ExitCode: 0}, outcomeIdle},
		{"non-zero exit -> crash", &docker.ContainerStatus{Running: false, ExitCode: 1}, outcomeCrash},
		{"sigterm (143) -> crash", &docker.ContainerStatus{Running: false, ExitCode: 143}, outcomeCrash},
		{"vanished container (nil) -> crash", nil, outcomeCrash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apExitOutcome(tt.info); got != tt.want {
				t.Errorf("apExitOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A stop takes the AP server down on purpose: the sweeper must not read it as a crash (prod, 2026-11-19: a
// finished game the API stopped was reported crashed while its 17 s stop ran).
func TestStoppedOnPurpose_aStopUnderWayIsNotACrash(t *testing.T) {
	svc, _, _, _ := recoveryService(t, &db.Session{SessionID: "s1", Status: "running"})

	done := svc.beginStop("s1")
	if !svc.stoppedOnPurpose("s1") {
		t.Fatal("a session being stopped was read as down by itself")
	}
	done()
	if svc.stoppedOnPurpose("s1") {
		t.Fatal("a running session whose stop mark is cleared must be checked again")
	}
}

func TestStoppedOnPurpose_aStopFinishedSinceTheListingIsNotACrash(t *testing.T) {
	svc, d, _, _ := recoveryService(t, &db.Session{SessionID: "s1", Status: "running"})
	if err := d.UpdateSessionStopped("s1"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if !svc.stoppedOnPurpose("s1") {
		t.Fatal("a session stopped since the listing was read as crashed")
	}
	if !svc.stoppedOnPurpose("deleted") {
		t.Fatal("a session deleted since the listing was read as crashed")
	}
}

func TestStoppedOnPurpose_aRunningSessionDownByItselfIsChecked(t *testing.T) {
	svc, _, _, _ := recoveryService(t, &db.Session{SessionID: "s1", Status: "running"})

	if svc.stoppedOnPurpose("s1") {
		t.Fatal("a real crash was taken for a stop")
	}
}

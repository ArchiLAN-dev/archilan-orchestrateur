package service

import (
	"testing"

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

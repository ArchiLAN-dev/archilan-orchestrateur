package service

import (
	"context"
	"fmt"
	"time"

	"archilan.fr/orchestrateur/internal/db"
	"archilan.fr/orchestrateur/internal/docker"
	"archilan.fr/orchestrateur/internal/webhook"
)

// RunSweeper performs boot recovery and then periodically checks for stuck/dead sessions.
func (s *Service) RunSweeper(ctx context.Context) {
	// Boot recovery: crash all sessions stuck in transit (goroutines died with old process)
	if err := s.db.CrashAllTransitSessions(); err != nil {
		s.log.Error("boot recovery failed", "err", err)
	}

	ticker := time.NewTicker(s.cfg.SweeperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Service) sweep(ctx context.Context) {
	s.sweepTransit(ctx)
	s.sweepRunning(ctx)
}

func (s *Service) sweepTransit(ctx context.Context) {
	expired, err := s.db.ListExpiredSessionsInTransit(time.Now().UTC())
	if err != nil {
		s.log.Error("sweeper: list expired sessions failed", "err", err)
		return
	}

	for _, sess := range expired {
		switch sess.Status {
		case "generating":
			// Resolve container reference: prefer stored ID, fall back to well-known name.
			// GenerationJobID is only stored after GenerateMultiworld returns (container already
			// removed by then), so during a live generation the field is always empty - look up
			// by name instead so the sweeper can verify liveness correctly.
			containerRef := fmt.Sprintf("archilan-gen-%s", sess.SessionID)
			if sess.GenerationJobID != nil && *sess.GenerationJobID != "" {
				containerRef = *sess.GenerationJobID
			}
			info, _ := s.docker.Inspect(ctx, containerRef)
			if info != nil && info.Running {
				// Still running - extend deadline by 20% of the timeout
				newDeadline := time.Now().Add(s.cfg.GenerationTimeout / 5)
				_ = s.db.ExtendSessionDeadline(sess.SessionID, newDeadline)
				s.log.Info("sweeper: generation still running, extending deadline", "session_id", sess.SessionID)
				continue
			}
			// Container dead or unknown - crash
			s.log.Warn("sweeper: generation deadline exceeded, crashing session", "session_id", sess.SessionID)
			_ = s.db.UpdateSessionCrashed(sess.SessionID)
			s.webhook.Send(ctx, webhook.Payload{
				Event:     "session.crashed",
				SessionID: sess.SessionID,
				Error:     "generation deadline exceeded",
			})

		case "launching":
			// The launch goroutine is presumed dead (e.g. the process restarted), so it will not
			// run its own cleanup. Stop any containers it managed to create before releasing the
			// port: their IDs are only persisted once the session reaches "running", so address them
			// by their well-known names. A container left running keeps its host port bound at the
			// Docker level and would collide with the next session that Acquires the freed port.
			s.log.Warn("sweeper: launch deadline exceeded, crashing session", "session_id", sess.SessionID)
			_ = s.docker.Stop(ctx, fmt.Sprintf("archilan-bridge-%s", sess.SessionID))
			_ = s.docker.Stop(ctx, fmt.Sprintf("ap-server-%s", sess.SessionID))
			if sess.BridgePort != nil {
				// Guarded release: if the stuck launch goroutine is still alive and races us, only
				// the owner-matching release wins, so a freed-then-reassigned port is never stolen.
				s.pool.ReleaseFor(*sess.BridgePort, sess.SessionID)
			}
			_ = s.db.UpdateSessionCrashed(sess.SessionID)
			s.webhook.Send(ctx, webhook.Payload{
				Event:     "session.crashed",
				SessionID: sess.SessionID,
				Error:     "launch deadline exceeded",
			})
		}
	}
}

func (s *Service) sweepRunning(ctx context.Context) {
	sessions, err := s.db.ListRunningSessionsForReconciliation()
	if err != nil {
		s.log.Error("sweeper: list running sessions failed", "err", err)
		return
	}

	for _, sess := range sessions {
		if sess.APContainerID == nil {
			continue
		}
		info, err := s.docker.Inspect(ctx, *sess.APContainerID)
		if err != nil {
			continue // inspect error, skip
		}
		if info == nil || !info.Running {
			switch apExitOutcome(info) {
			case outcomeIdle:
				s.idleFromAutoShutdown(ctx, sess)
			default:
				s.crashRunningSession(ctx, sess, "AP server container died")
			}
		}
	}
}

const (
	outcomeIdle  = "idle"
	outcomeCrash = "crash"
)

// apExitOutcome classifies a non-running AP server container. A clean exit (code 0) is
// Archipelago's own auto_shutdown after inactivity → the session is idle and resumable from
// the save on its retained volume. Any other exit (non-zero) or a vanished container (nil)
// is a real crash.
func apExitOutcome(info *docker.ContainerStatus) string {
	if info != nil && info.ExitCode == 0 {
		return outcomeIdle
	}
	return outcomeCrash
}

// idleFromAutoShutdown handles an AP server that exited cleanly via its auto_shutdown:
// stop the now-idle bridge, remove both containers, release the port, keep the volume (it
// holds the .apsave), and tell the API the session is idle (resumable via relaunch-from-save).
func (s *Service) idleFromAutoShutdown(ctx context.Context, sess *db.Session) {
	s.log.Info("sweeper: AP auto_shutdown, marking session idle", "session_id", sess.SessionID)
	if sess.BridgeContainerID != nil {
		_ = s.docker.Stop(ctx, *sess.BridgeContainerID)
	}
	// Remove the idle containers (the AP already exited) but keep the volume - its .apsave is
	// what relaunch-from-save resumes from.
	s.removeSessionContainers(ctx, sess)
	if sess.BridgePort != nil {
		s.pool.Release(*sess.BridgePort)
	}
	_ = s.db.UpdateSessionStopped(sess.SessionID)
	s.webhook.Send(ctx, webhook.Payload{
		Event:     "session.idle",
		SessionID: sess.SessionID,
	})
}

// crashRunningSession handles a running session whose AP server died unexpectedly.
func (s *Service) crashRunningSession(ctx context.Context, sess *db.Session, reason string) {
	s.log.Warn("sweeper: AP server died, crashing session", "session_id", sess.SessionID)
	// Stop both containers BEFORE releasing the port. A crashed session keeps its containers
	// for log inspection, but a still-running bridge - or the AP server's own on-failure restart
	// loop - keeps the host port bound at the Docker level. Releasing the port to the pool while
	// the old containers still hold it lets the next session Acquire that exact port and then
	// collide on bind ("port already in use"). Stopping (not removing) frees the host binding and
	// halts the restart policy while preserving the containers for diagnostics.
	if sess.BridgeContainerID != nil {
		_ = s.docker.Stop(ctx, *sess.BridgeContainerID)
	}
	if sess.APContainerID != nil {
		_ = s.docker.Stop(ctx, *sess.APContainerID)
	}
	if sess.BridgePort != nil {
		s.pool.ReleaseFor(*sess.BridgePort, sess.SessionID)
	}
	_ = s.db.UpdateSessionCrashed(sess.SessionID)
	s.webhook.Send(ctx, webhook.Payload{
		Event:     "session.crashed",
		SessionID: sess.SessionID,
		Error:     reason,
	})
}

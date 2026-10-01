package service

import (
	"time"

	"archilan.fr/orchestrateur/internal/db"
)

// Story 17.27: a paused (auto_shutdown) or crashed session keeps its port for PORT_RESERVATION_TTL,
// so its relaunch comes back on the same address. The orchestrateur still alone picks the port: the
// reservation is a preference inside its pool, recorded so a restart does not lose it. It never caps
// the number of games - a full pool takes back the oldest reservation - and a zero TTL restores the
// former behaviour (every launch on a fresh port).

// reservePort keeps the session's port for its relaunch, or releases it when reservations are off.
// Called when the session stops running without being given up: idle (auto_shutdown) or crash.
func (s *Service) reservePort(sess *db.Session, at time.Time) {
	if sess.BridgePort == nil {
		return
	}
	if s.cfg.PortReservationTTL <= 0 {
		s.pool.ReleaseFor(*sess.BridgePort, sess.SessionID)
		return
	}
	until := at.Add(s.cfg.PortReservationTTL)
	if !s.pool.Hold(*sess.BridgePort, sess.SessionID, until) {
		// The port already belongs to another session: nothing of ours to keep.
		return
	}
	if err := s.db.SetPortReservation(sess.SessionID, until); err != nil {
		s.log.Warn("port reservation: record failed", "session_id", sess.SessionID, "err", err)
	}
	s.log.Info("port reserved for relaunch", "session_id", sess.SessionID, "port", *sess.BridgePort, "until", until)
}

// dropPortReservation gives the session's port up, used or reserved: a manual stop or a deletion.
func (s *Service) dropPortReservation(sess *db.Session) {
	if sess.BridgePort != nil {
		// Owner-guarded both ways: a stale bridge_port may since belong to another session.
		s.pool.ReleaseFor(*sess.BridgePort, sess.SessionID)
		s.pool.Forget(*sess.BridgePort, sess.SessionID)
	}
	if err := s.db.ClearPortReservation(sess.SessionID); err != nil {
		s.log.Warn("port reservation: clear failed", "session_id", sess.SessionID, "err", err)
	}
}

// acquirePort picks the port of a launch: the session's own reservation first, then a free port,
// then the oldest reservation of another session, which is forgotten on record.
func (s *Service) acquirePort(sessionID string, at time.Time) (int, error) {
	port, evicted, err := s.pool.AcquireFor(sessionID, at)
	if err != nil {
		return 0, err
	}
	if evicted != nil {
		s.log.Info("port reservation taken back for a launch: pool full",
			"port", evicted.Port, "from_session", evicted.SessionID, "to_session", sessionID)
		if err := s.db.ClearPortReservation(evicted.SessionID); err != nil {
			s.log.Warn("port reservation: clear failed", "session_id", evicted.SessionID, "err", err)
		}
	}
	return port, nil
}

// expirePortReservations releases the reservations past their deadline. Run by the sweeper.
func (s *Service) expirePortReservations(at time.Time) {
	for _, e := range s.pool.Expire(at) {
		s.log.Info("port reservation expired", "session_id", e.SessionID, "port", e.Port)
		if err := s.db.ClearPortReservation(e.SessionID); err != nil {
			s.log.Warn("port reservation: clear failed", "session_id", e.SessionID, "err", err)
		}
	}
}

// recoverPortReservations rebuilds the pool's holds at boot, after the running sessions took their
// ports. A reservation whose port now runs another session cannot be honoured and is forgotten.
func (s *Service) recoverPortReservations(at time.Time) error {
	list, err := s.db.ActivePortReservations(at)
	if err != nil {
		return err
	}
	for _, r := range list {
		if s.pool.Hold(r.Port, r.SessionID, r.Until) {
			s.log.Info("recovered port reservation from db", "session_id", r.SessionID, "port", r.Port, "until", r.Until)
			continue
		}
		s.log.Warn("port reservation dropped at boot: port in use", "session_id", r.SessionID, "port", r.Port)
		if err := s.db.ClearPortReservation(r.SessionID); err != nil {
			s.log.Warn("port reservation: clear failed", "session_id", r.SessionID, "err", err)
		}
	}
	return nil
}

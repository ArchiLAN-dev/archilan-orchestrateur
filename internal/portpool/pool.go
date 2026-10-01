package portpool

import (
	"errors"
	"sync"
	"time"
)

var ErrExhausted = errors.New("port pool exhausted")

// hold is a port kept for a paused session until a deadline (story 17.27).
type hold struct {
	sessionID string
	until     time.Time
}

// Eviction names a hold that was dropped: expired, or taken back for another launch.
type Eviction struct {
	Port      int
	SessionID string
}

type Pool struct {
	mu    sync.Mutex
	start int
	end   int
	used  map[int]string // port → sessionID, for sessions launching or running
	held  map[int]hold   // port → paused session keeping it for its relaunch (story 17.27)
}

func New(start, end int) *Pool {
	return &Pool{
		start: start,
		end:   end,
		used:  make(map[int]string),
		held:  make(map[int]hold),
	}
}

// Reserve marks a port as in use without acquiring it from the pool.
// Used when recovering existing sessions from the DB at startup.
func (p *Pool) Reserve(port int, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used[port] = sessionID
}

// Acquire returns a port for the session and marks it as used. See AcquireFor.
func (p *Pool) Acquire(sessionID string) (int, error) {
	port, _, err := p.AcquireFor(sessionID, time.Now().UTC())
	return port, err
}

// AcquireFor returns a port for the session and marks it as used, in this order (story 17.27):
//  1. the port the session itself holds from its pause - a relaunch keeps its address;
//  2. the lowest free port;
//  3. the oldest hold of another session, taken back so that holds never cap the number of games.
//     That session gets a new port on its own relaunch, as before holds existed; the eviction is
//     returned so the caller can forget it on record.
//
// A running session's port is never taken. With every port running, it returns ErrExhausted.
func (p *Pool) AcquireFor(sessionID string, now time.Time) (int, *Eviction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for port, h := range p.held {
		if h.sessionID == sessionID {
			delete(p.held, port)
			p.used[port] = sessionID
			return port, nil, nil
		}
	}

	for port := p.start; port <= p.end; port++ {
		if p.free(port) {
			p.used[port] = sessionID
			return port, nil, nil
		}
	}

	oldest, found := 0, false
	for port, h := range p.held {
		if !found || h.until.Before(p.held[oldest].until) || (h.until.Equal(p.held[oldest].until) && port < oldest) {
			oldest, found = port, true
		}
	}
	if !found {
		return 0, nil, ErrExhausted
	}
	evicted := &Eviction{Port: oldest, SessionID: p.held[oldest].sessionID}
	delete(p.held, oldest)
	p.used[oldest] = sessionID
	return oldest, evicted, nil
}

// Hold keeps the port for the session until the deadline, typically when it pauses. The session
// must be the one using the port, or the port must be free (boot recovery re-holds ports a fresh
// pool never handed out). A port used or held by another session is refused.
func (p *Pool) Hold(port int, sessionID string, until time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if owner, ok := p.used[port]; ok && owner != sessionID {
		return false
	}
	if h, ok := p.held[port]; ok && h.sessionID != sessionID {
		return false
	}
	delete(p.used, port)
	p.held[port] = hold{sessionID: sessionID, until: until}
	return true
}

// Forget drops the session's hold on the port, e.g. on a manual stop or a deletion. It reports
// whether a hold was dropped.
func (p *Pool) Forget(port int, sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.held[port]; ok && h.sessionID == sessionID {
		delete(p.held, port)
		return true
	}
	return false
}

// Expire drops the holds whose deadline is reached and returns them.
func (p *Pool) Expire(now time.Time) []Eviction {
	p.mu.Lock()
	defer p.mu.Unlock()
	var expired []Eviction
	for port, h := range p.held {
		if !h.until.After(now) {
			expired = append(expired, Eviction{Port: port, SessionID: h.sessionID})
			delete(p.held, port)
		}
	}
	return expired
}

// Release frees a port back to the pool, whether used or held.
func (p *Pool) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, port)
	delete(p.held, port)
}

// ReleaseFor frees a port only if it is still held by the given session. It reports
// whether the release happened. This guards against a stale cleanup path releasing a port
// that has since been re-Acquired by a different session: e.g. the sweeper crashes a stuck
// launch and frees its port, a new session Acquires that exact port, then the original launch
// goroutine finally fails and tries to release it again - an unguarded Release would steal the
// port from the new session. ReleaseFor makes that late release a no-op.
//
// It only frees a port in use: a pause hold (story 17.27) is left alone, so cleaning up a
// stale entry before a relaunch does not cost the session its address. Forget drops a hold.
func (p *Pool) ReleaseFor(port int, sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if owner, ok := p.used[port]; ok && owner == sessionID {
		delete(p.used, port)
		return true
	}
	return false
}

func (p *Pool) free(port int) bool {
	if _, ok := p.used[port]; ok {
		return false
	}
	_, ok := p.held[port]
	return !ok
}

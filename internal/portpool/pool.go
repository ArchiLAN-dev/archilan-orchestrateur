package portpool

import (
	"errors"
	"sync"
)

var ErrExhausted = errors.New("port pool exhausted")

type Pool struct {
	mu    sync.Mutex
	start int
	end   int
	used  map[int]string // port → sessionID
}

func New(start, end int) *Pool {
	return &Pool{
		start: start,
		end:   end,
		used:  make(map[int]string),
	}
}

// Reserve marks a port as in use without acquiring it from the pool.
// Used when recovering existing sessions from the DB at startup.
func (p *Pool) Reserve(port int, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used[port] = sessionID
}

// Acquire returns a free port and marks it as used.
func (p *Pool) Acquire(sessionID string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port := p.start; port <= p.end; port++ {
		if _, ok := p.used[port]; !ok {
			p.used[port] = sessionID
			return port, nil
		}
	}
	return 0, ErrExhausted
}

// Release frees a port back to the pool.
func (p *Pool) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, port)
}

// ReleaseFor frees a port only if it is still held by the given session. It reports
// whether the release happened. This guards against a stale cleanup path releasing a port
// that has since been re-Acquired by a different session: e.g. the sweeper crashes a stuck
// launch and frees its port, a new session Acquires that exact port, then the original launch
// goroutine finally fails and tries to release it again - an unguarded Release would steal the
// port from the new session. ReleaseFor makes that late release a no-op.
func (p *Pool) ReleaseFor(port int, sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if owner, ok := p.used[port]; ok && owner == sessionID {
		delete(p.used, port)
		return true
	}
	return false
}

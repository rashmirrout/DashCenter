// Broadcaster fan-out for migration session updates. Mirror of
// orchestrator.Broadcaster (PC-G3) — same bounded-buffer + non-blocking
// publish discipline. PC-G6 StreamMigrationSession subscribers register
// here; every phase advance / rollback / abort publishes the updated
// session.
package migration

import (
	"sync"
)

const defaultSubBuffer = 32

type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[*sub]struct{}
}

type sub struct {
	ch        chan Session
	sessionID string // empty = all sessions
	dropped   int
	closedMu  sync.Mutex
	closed    bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subscribers: map[*sub]struct{}{}}
}

// Subscribe registers a session-update receiver. sessionID="" means
// "all sessions". Returns the receive channel and a cancel function
// that MUST be called.
func (b *Broadcaster) Subscribe(sessionID string) (<-chan Session, func()) {
	s := &sub{ch: make(chan Session, defaultSubBuffer), sessionID: sessionID}
	b.mu.Lock()
	b.subscribers[s] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		s.closedMu.Lock()
		if !s.closed {
			s.closed = true
			close(s.ch)
		}
		s.closedMu.Unlock()
		b.mu.Lock()
		delete(b.subscribers, s)
		b.mu.Unlock()
	}
	return s.ch, cancel
}

// Publish fans the session out. Non-blocking: full per-sub buffers
// drop and bump sub.dropped (no operator visibility today; PD will
// surface it via /admin/health).
func (b *Broadcaster) Publish(s Session) {
	b.mu.Lock()
	subs := make([]*sub, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, sb := range subs {
		if sb.sessionID != "" && sb.sessionID != s.ID {
			continue
		}
		sb.closedMu.Lock()
		if sb.closed {
			sb.closedMu.Unlock()
			continue
		}
		select {
		case sb.ch <- s:
		default:
			sb.dropped++
		}
		sb.closedMu.Unlock()
	}
}

func (b *Broadcaster) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

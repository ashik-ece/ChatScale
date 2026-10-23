package hub

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ashik-ece/chatscale/internal/metrics"
	"github.com/ashik-ece/chatscale/internal/ws"
)

// Room is the set of clients watching one chat room.
type Room struct {
	mu      sync.RWMutex
	clients map[*ws.Client]struct{}
	dropped atomic.Uint64
	cancel  context.CancelFunc // stops the room's fan-out goroutine
	metrics *metrics.Metrics
}

func newRoom(m *metrics.Metrics) *Room {
	return &Room{clients: make(map[*ws.Client]struct{}), metrics: m}
}

func (r *Room) Add(c *ws.Client) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
}

// Remove deletes c and reports whether it was a member.
func (r *Room) Remove(c *ws.Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.clients[c]
	delete(r.clients, c)
	return ok
}

func (r *Room) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// Broadcast sends data to every client without blocking. A client whose buffer
// is full is a slow consumer: the message is dropped for it and counted.
func (r *Room) Broadcast(data []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		// Only a full buffer is a slow consumer; clients already closing are skipped.
		if c.TrySend(data) == ws.Full {
			r.dropped.Add(1)
			r.metrics.MessagesDropped.Inc()
		}
	}
}

func (r *Room) closeAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.clients {
		c.Close()
	}
}

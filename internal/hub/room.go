package hub

import (
	"sync"
	"sync/atomic"

	"github.com/ashik-ece/chatscale/internal/ws"
)

// Room is the set of clients watching one chat room.
type Room struct {
	mu      sync.RWMutex
	clients map[*ws.Client]struct{}
	dropped atomic.Uint64
}

func newRoom() *Room {
	return &Room{clients: make(map[*ws.Client]struct{})}
}

func (r *Room) Add(c *ws.Client) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
}

func (r *Room) Remove(c *ws.Client) {
	r.mu.Lock()
	delete(r.clients, c)
	r.mu.Unlock()
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
		if !c.TrySend(data) {
			r.dropped.Add(1)
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

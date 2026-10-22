// Package hub manages rooms and routes messages between clients.
package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

// Hub owns all rooms. A mutex-guarded map is used instead of sync.Map so that
// creating a room, adding the first client and deleting an empty room are
// atomic; Phase 3 hangs the Redis subscribe/unsubscribe lifecycle off those
// transitions.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room

	limiter *ratelimit.Store
	trie    *filter.Trie
	banned  sync.Map // userID -> struct{}
}

func New(limiter *ratelimit.Store, trie *filter.Trie) *Hub {
	return &Hub{rooms: make(map[string]*Room), limiter: limiter, trie: trie}
}

func (h *Hub) Allow(userID string) bool       { return h.limiter.Check(userID) }
func (h *Hub) IsTimedOut(userID string) bool  { return h.limiter.IsTimedOut(userID) }
func (h *Hub) BanUser(userID string)          { h.banned.Store(userID, struct{}{}) }
func (h *Hub) IsBanned(userID string) bool    { _, ok := h.banned.Load(userID); return ok }
func (h *Hub) Filter(s string) (string, bool) { return h.trie.Filter(s) }

func (h *Hub) TimeoutUser(userID string, d time.Duration) { h.limiter.Timeout(userID, d) }

func (h *Hub) Register(c *ws.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[c.RoomID]
	if !ok {
		room = newRoom()
		h.rooms[c.RoomID] = room
	}
	room.Add(c)
}

// Unregister removes the client and deletes its room once empty. It is safe to
// call more than once.
func (h *Hub) Unregister(c *ws.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[c.RoomID]
	if !ok {
		return
	}
	room.Remove(c)
	if room.Len() == 0 {
		delete(h.rooms, c.RoomID)
	}
}

func (h *Hub) Broadcast(msg message.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal message: %v", err)
		return
	}
	h.mu.Lock()
	room := h.rooms[msg.RoomID]
	h.mu.Unlock()
	if room != nil {
		room.Broadcast(data)
	}
}

// Shutdown asks every client to close; each WritePump sends a 1001 frame.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()
	for _, r := range rooms {
		r.closeAll()
	}
}

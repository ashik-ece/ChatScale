// Package hub manages rooms and routes messages between clients.
package hub

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/ws"
)

// Hub owns all rooms. A mutex-guarded map is used instead of sync.Map so that
// creating a room, adding the first client and deleting an empty room are
// atomic; Phase 3 hangs the Redis subscribe/unsubscribe lifecycle off those
// transitions.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

func New() *Hub {
	return &Hub{rooms: make(map[string]*Room)}
}

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

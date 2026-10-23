// Package hub manages rooms and routes messages between clients.
package hub

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/metrics"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

const (
	subscribeTimeout = 2 * time.Second
	publishTimeout   = 2 * time.Second
)

// Publisher sends a room message to every node, including this one.
type Publisher interface {
	Publish(ctx context.Context, roomID string, msg message.Message) error
}

// Subscriber delivers a room's published messages to this node.
type Subscriber interface {
	Subscribe(ctx context.Context, roomID string) (<-chan []byte, error)
	Unsubscribe(roomID string)
}

// Hub owns all rooms on this node. A mutex-guarded map is used instead of
// sync.Map so that creating a room, subscribing it and deleting it when empty
// are atomic.
type Hub struct {
	nodeID string
	pub    Publisher
	sub    Subscriber

	ctx    context.Context // parent of every room's fan-out context
	cancel context.CancelFunc

	mu    sync.Mutex
	rooms map[string]*Room

	// pubMu is read-locked around each publish; Shutdown write-locks it to
	// wait for in-flight publishes and refuse new ones.
	pubMu  sync.RWMutex
	closed bool

	limiter *ratelimit.Store
	trie    *filter.Trie
	banned  sync.Map // userID -> struct{}
	metrics *metrics.Metrics
}

func New(limiter *ratelimit.Store, trie *filter.Trie, pub Publisher, sub Subscriber, nodeID string, m *metrics.Metrics) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		nodeID:  nodeID,
		pub:     pub,
		sub:     sub,
		ctx:     ctx,
		cancel:  cancel,
		rooms:   make(map[string]*Room),
		limiter: limiter,
		trie:    trie,
		metrics: m,
	}
}

// Allow reports whether userID may send now. The caller checks IsTimedOut
// first, so a false result here means the token bucket was empty.
func (h *Hub) Allow(userID string) bool {
	if h.limiter.Check(userID) {
		return true
	}
	h.metrics.RateLimitRejections.Inc()
	return false
}

func (h *Hub) IsTimedOut(userID string) bool  { return h.limiter.IsTimedOut(userID) }
func (h *Hub) BanUser(userID string)          { h.banned.Store(userID, struct{}{}) }
func (h *Hub) IsBanned(userID string) bool    { _, ok := h.banned.Load(userID); return ok }
func (h *Hub) Filter(s string) (string, bool) { return h.trie.Filter(s) }

func (h *Hub) TimeoutUser(userID string, d time.Duration) { h.limiter.Timeout(userID, d) }

// Register adds the client to its room. The first client in a room subscribes
// this node to the room's channel and starts the fan-out goroutine. It fails
// if that subscription cannot be established.
func (h *Hub) Register(c *ws.Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[c.RoomID]
	if !ok {
		subCtx, cancelSub := context.WithTimeout(h.ctx, subscribeTimeout)
		ch, err := h.sub.Subscribe(subCtx, c.RoomID)
		cancelSub()
		if err != nil {
			return fmt.Errorf("subscribe room %s: %w", c.RoomID, err)
		}
		room = newRoom(h.metrics)
		var fanCtx context.Context
		fanCtx, room.cancel = context.WithCancel(h.ctx)
		StartFanOut(fanCtx, ch, room)
		h.rooms[c.RoomID] = room
		h.metrics.ActiveRooms.Inc()
	}
	room.Add(c)
	h.metrics.ActiveConnections.Inc()
	return nil
}

// Unregister removes the client. When the last client leaves, the room's
// subscription and fan-out goroutine are torn down. It is safe to call more
// than once.
func (h *Hub) Unregister(c *ws.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[c.RoomID]
	if !ok {
		return
	}
	if room.Remove(c) {
		h.metrics.ActiveConnections.Dec()
	}
	if room.Len() == 0 {
		delete(h.rooms, c.RoomID)
		h.metrics.ActiveRooms.Dec()
		room.cancel()
		h.sub.Unsubscribe(c.RoomID)
	}
}

// Broadcast publishes msg to the room's channel. Delivery to clients, on this
// node and every other, happens in each node's fan-out goroutine.
func (h *Hub) Broadcast(msg message.Message) {
	h.pubMu.RLock()
	defer h.pubMu.RUnlock()
	if h.closed {
		return
	}

	msg.NodeID = h.nodeID
	ctx, cancel := context.WithTimeout(h.ctx, publishTimeout)
	defer cancel()
	if err := h.pub.Publish(ctx, msg.RoomID, msg); err != nil {
		log.Printf("publish room=%s: %v", msg.RoomID, err)
		return
	}
	h.metrics.MessagesTotal.WithLabelValues(msg.RoomID).Inc()
}

// Shutdown closes every client (each WritePump sends a 1001 frame), then waits
// for in-flight publishes and rejects any later ones.
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

	h.pubMu.Lock()
	h.closed = true
	h.pubMu.Unlock()
}

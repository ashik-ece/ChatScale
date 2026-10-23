package pubsub

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/ashik-ece/chatscale/internal/message"
)

// MemoryBus is an in-process stand-in for Redis Pub/Sub, used by tests. Several
// hubs can share one bus, each through its own subscriber, to simulate nodes.
type MemoryBus struct {
	mu   sync.Mutex
	subs map[string]map[*MemorySubscriber]struct{} // roomID -> subscribers
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[string]map[*MemorySubscriber]struct{})}
}

func (b *MemoryBus) Publish(_ context.Context, roomID string, msg message.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs[roomID] {
		select {
		case s.chans[roomID] <- data:
		default:
		}
	}
	return nil
}

// Channels lists rooms with at least one subscriber, like PUBSUB CHANNELS.
func (b *MemoryBus) Channels() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for room, set := range b.subs {
		if len(set) > 0 {
			out = append(out, Channel(room))
		}
	}
	sort.Strings(out)
	return out
}

// NewSubscriber returns a subscriber that behaves like one gateway node's Redis connection.
func (b *MemoryBus) NewSubscriber() *MemorySubscriber {
	return &MemorySubscriber{bus: b, chans: make(map[string]chan []byte)}
}

type MemorySubscriber struct {
	bus   *MemoryBus
	chans map[string]chan []byte // guarded by bus.mu
}

func (s *MemorySubscriber) Subscribe(_ context.Context, roomID string) (<-chan []byte, error) {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	ch := make(chan []byte, subscriberBuffer)
	s.chans[roomID] = ch
	if s.bus.subs[roomID] == nil {
		s.bus.subs[roomID] = make(map[*MemorySubscriber]struct{})
	}
	s.bus.subs[roomID][s] = struct{}{}
	return ch, nil
}

func (s *MemorySubscriber) Unsubscribe(roomID string) {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if ch, ok := s.chans[roomID]; ok {
		close(ch)
		delete(s.chans, roomID)
	}
	delete(s.bus.subs[roomID], s)
}

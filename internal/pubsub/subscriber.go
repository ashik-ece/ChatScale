package pubsub

import (
	"context"
	"log"
	"sync"

	"github.com/redis/go-redis/v9"
)

const subscriberBuffer = 256

// Subscriber keeps one Redis subscription per room.
type Subscriber struct {
	client *redis.Client

	mu   sync.Mutex
	subs map[string]*redis.PubSub
}

func NewSubscriber(client *redis.Client) *Subscriber {
	return &Subscriber{client: client, subs: make(map[string]*redis.PubSub)}
}

// Subscribe starts receiving roomID's messages. It returns only once Redis has
// confirmed the subscription, so nothing published afterwards is missed. The
// returned channel is closed by Unsubscribe.
func (s *Subscriber) Subscribe(ctx context.Context, roomID string) (<-chan []byte, error) {
	ps := s.client.Subscribe(ctx, Channel(roomID))
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, err
	}

	s.mu.Lock()
	s.subs[roomID] = ps
	s.mu.Unlock()

	out := make(chan []byte, subscriberBuffer)
	in := ps.Channel()
	go func() {
		defer close(out)
		for m := range in {
			// Never block here: if the fan-out goroutine has stopped, a blocking
			// send would leak this goroutine. Pub/Sub is at-most-once anyway.
			select {
			case out <- []byte(m.Payload):
			default:
				log.Printf("pubsub: dropping message for room %s, fan-out is behind", roomID)
			}
		}
	}()
	return out, nil
}

// Unsubscribe drops the room's subscription and closes its channel.
func (s *Subscriber) Unsubscribe(roomID string) {
	s.mu.Lock()
	ps := s.subs[roomID]
	delete(s.subs, roomID)
	s.mu.Unlock()
	if ps != nil {
		_ = ps.Close()
	}
}

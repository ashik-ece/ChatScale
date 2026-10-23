// Package pubsub moves chat messages between gateway nodes through Redis.
package pubsub

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"

	"github.com/ashik-ece/chatscale/internal/message"
)

// Channel returns the Redis channel that carries a room's messages.
func Channel(roomID string) string { return "room:" + roomID }

// Publisher publishes room messages to Redis.
type Publisher struct {
	client *redis.Client
}

func NewPublisher(client *redis.Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) Publish(ctx context.Context, roomID string, msg message.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p.client.Publish(ctx, Channel(roomID), data).Err()
}

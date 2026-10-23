package hub

import (
	"context"
	"time"
)

// StartFanOut forwards every message arriving from the room's pub/sub channel
// to the room's local clients. This is the only path by which a message reaches
// a client: ReadPump publishes and never delivers locally, so a viewer on the
// sender's own node sees exactly what viewers on other nodes see.
func StartFanOut(ctx context.Context, in <-chan []byte, room *Room) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-in:
				if !ok {
					return
				}
				start := time.Now()
				room.Broadcast(data)
				room.metrics.FanoutLatency.Observe(time.Since(start).Seconds())
			}
		}
	}()
}

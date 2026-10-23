package loadgen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	reconnectBase = 500 * time.Millisecond // same backoff as the React client
	reconnectMax  = 30 * time.Second
	reconnectJit  = time.Second
)

var contentKey = []byte(`"content":"`)

type client struct {
	id     int
	user   string
	sender bool
	conn   atomic.Pointer[websocket.Conn]

	// Written only by this client's own goroutines, read by the sampler.
	received atomic.Uint64
	sent     atomic.Uint64
	expected atomic.Uint64 // deliveries owed for this client's sends
}

// parseSentAt pulls the sentAtNano out of a chat frame whose content is
// "<clientMsgId>:<sentAtNano>". It avoids a full JSON decode: at hundreds of
// thousands of frames per second the decoder would dominate the measurement.
func parseSentAt(frame []byte) (int64, bool) {
	i := bytes.Index(frame, contentKey)
	if i < 0 {
		return 0, false
	}
	rest := frame[i+len(contentKey):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return 0, false
	}
	content := rest[:j]
	k := bytes.LastIndexByte(content, ':')
	if k < 0 || k == len(content)-1 {
		return 0, false
	}
	var n int64
	for _, ch := range content[k+1:] {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int64(ch-'0')
	}
	return n, true
}

// run keeps the client connected until ctx ends, reconnecting on failure.
func (c *client) run(ctx context.Context, g *Gen) {
	defer g.wg.Done()
	if c.sender {
		go c.sendLoop(ctx, g)
	}

	target := c.id % len(g.cfg.Targets)
	var lostAt time.Time // set when an established connection drops
	failures := 0

	for ctx.Err() == nil {
		conn, err := g.dial(ctx, g.cfg.Targets[target], c.user)
		if err != nil {
			g.failedDials.Add(1)
			target = (target + 1) % len(g.cfg.Targets)
			if !sleep(ctx, backoff(failures)) {
				return
			}
			failures++
			continue
		}
		failures = 0
		c.conn.Store(conn)
		g.noteConnected(+1)

		stop := context.AfterFunc(ctx, func() { conn.Close() })
		c.readLoop(conn, g, &lostAt)
		stop()

		c.conn.Store(nil)
		g.noteConnected(-1)
		conn.Close()
		if ctx.Err() != nil {
			return
		}
		lostAt = time.Now()
		target = (target + 1) % len(g.cfg.Targets)
		if !sleep(ctx, backoff(0)) {
			return
		}
	}
}

// readLoop consumes frames until the connection fails. The first frame after a
// reconnect closes out that client's reconnect-time sample.
func (c *client) readLoop(conn *websocket.Conn, g *Gen, lostAt *time.Time) {
	buf := make([]byte, 1024)
	for {
		_, r, err := conn.NextReader()
		if err != nil {
			return
		}
		n := 0
		for {
			m, err := r.Read(buf[n:])
			n += m
			if err != nil || n == len(buf) {
				if err == nil {
					_, _ = io.Copy(io.Discard, r) // oversized frame: ignore the tail
				}
				break
			}
		}
		now := time.Now().UnixNano()

		sentAt, ok := parseSentAt(buf[:n])
		if !ok {
			g.errorFrames.Add(1) // error envelope or foreign frame
			continue
		}
		c.received.Add(1)
		g.hist.record(c.id, now-sentAt)

		if !lostAt.IsZero() {
			g.noteReconnect(time.Since(*lostAt))
			*lostAt = time.Time{}
		}
	}
}

// sendLoop publishes at the configured rate on whichever connection is current.
func (c *client) sendLoop(ctx context.Context, g *Gen) {
	interval := time.Duration(float64(time.Second) / g.cfg.MsgRate)
	if !sleep(ctx, time.Duration(rand.Int63n(int64(interval)))) { // stagger senders
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	seq := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		conn := c.conn.Load()
		if conn == nil {
			continue
		}
		seq++
		id := fmt.Sprintf("s%d-%d", c.id, seq)
		payload := fmt.Sprintf(`{"type":"chat","content":"%s:%d","clientMsgId":"%s"}`, id, time.Now().UnixNano(), id)

		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
			continue
		}
		c.sent.Add(1)
		c.expected.Add(uint64(g.connected.Load()))
	}
}

func backoff(attempt int) time.Duration {
	d := reconnectBase << min(attempt, 10)
	if d > reconnectMax {
		d = reconnectMax
	}
	return d + time.Duration(rand.Int63n(int64(reconnectJit)))
}

// sleep waits for d or ctx, reporting whether the full wait elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

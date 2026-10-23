// Package ws holds the per-connection WebSocket plumbing: one Client, one
// ReadPump goroutine and one WritePump goroutine.
package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/gorilla/websocket"
)

const sendBufferSize = 256

// Client is a single WebSocket connection. The ReadPump is the only caller of
// conn.ReadMessage and the WritePump the only caller of conn.WriteMessage,
// because gorilla/websocket does not support concurrent writers.
//
// The send channel is never closed: producers do non-blocking sends, and the
// WritePump stops on ctx cancellation, so there is no send-on-closed-channel race.
type Client struct {
	conn   *websocket.Conn
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc

	UserID string
	RoomID string
	ConnID string
	IsMod  bool
}

func NewClient(conn *websocket.Conn, userID, roomID string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	connID := newConnID()
	if userID == "" {
		userID = "anon-" + connID
	}
	return &Client{
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
		ctx:    ctx,
		cancel: cancel,
		UserID: userID,
		RoomID: roomID,
		ConnID: connID,
	}
}

// SendResult is the outcome of a non-blocking send to a client.
type SendResult int

const (
	Sent   SendResult = iota
	Full              // buffer full: the client is a slow consumer
	Closed            // client is shutting down; not a delivery failure
)

// TrySend queues data for the WritePump without blocking.
func (c *Client) TrySend(data []byte) SendResult {
	select {
	case <-c.ctx.Done():
		return Closed
	default:
	}
	select {
	case c.send <- data:
		return Sent
	default:
		return Full
	}
}

// SendJSON queues v for this client only. Replies go through the send channel,
// never straight to the connection, so the WritePump stays the sole writer.
func (c *Client) SendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.TrySend(data)
}

// Close asks both pumps to stop; the WritePump sends a 1001 close frame.
func (c *Client) Close() { c.cancel() }

func newConnID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

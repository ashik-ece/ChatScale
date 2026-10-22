package ws

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ashik-ece/chatscale/internal/message"
)

const (
	readLimit = 512
	pongWait  = 60 * time.Second
)

// Hub is what the pumps need from the room manager.
type Hub interface {
	Register(c *Client)
	Unregister(c *Client)
	Broadcast(msg message.Message)
}

// ReadPump reads messages from the connection and hands them to the hub until
// the connection fails or the client is closed.
func ReadPump(c *Client, h Hub) {
	defer func() {
		h.Unregister(c)
		c.cancel()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(readLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				log.Printf("read error conn=%s user=%s: %v", c.ConnID, c.UserID, err)
			}
			return
		}

		var msg message.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue // ignore malformed frames
		}

		// Identity and routing come from the connection, never from the payload.
		msg.Type = message.TypeChat
		msg.UserID = c.UserID
		msg.RoomID = c.RoomID
		msg.Timestamp = time.Now().UnixMilli()
		h.Broadcast(msg)
	}
}

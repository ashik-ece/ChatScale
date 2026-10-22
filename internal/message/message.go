// Package message defines the wire format shared by the gateway and its clients.
package message

const (
	TypeChat   = "chat"
	TypeSystem = "system"
	TypeError  = "error"
)

// Message is the JSON envelope sent over the WebSocket.
type Message struct {
	Type      string `json:"type"`
	UserID    string `json:"userId"`
	RoomID    string `json:"roomId"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

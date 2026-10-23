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
	NodeID    string `json:"nodeId,omitempty"` // gateway node that published it
}

const (
	ReasonRateLimited = "rate_limited"
	ReasonTimedOut    = "timed_out"
	ReasonBadCommand  = "bad_command"
)

// ErrorEnvelope is sent back to a single client when its message is rejected.
type ErrorEnvelope struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

func NewError(reason string) ErrorEnvelope {
	return ErrorEnvelope{Type: TypeError, Reason: reason}
}

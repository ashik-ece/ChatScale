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

	// ClientMsgID is chosen by the sender and echoed back unchanged, so the
	// sender can match the server's copy to its optimistic local one.
	ClientMsgID string `json:"clientMsgId,omitempty"`
}

const (
	ReasonRateLimited = "rate_limited"
	ReasonTimedOut    = "timed_out"
	ReasonBadCommand  = "bad_command"
)

// ErrorEnvelope is sent back to a single client when its message is rejected.
type ErrorEnvelope struct {
	Type        string `json:"type"`
	Reason      string `json:"reason"`
	ClientMsgID string `json:"clientMsgId,omitempty"` // the rejected message, if known
}

func NewError(reason, clientMsgID string) ErrorEnvelope {
	return ErrorEnvelope{Type: TypeError, Reason: reason, ClientMsgID: clientMsgID}
}

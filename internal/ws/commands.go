package ws

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ashik-ece/chatscale/internal/message"
)

// handleCommand runs a moderator command such as "!ban alice" or
// "!timeout alice 60" and replies to the moderator only.
func handleCommand(c *Client, h Hub, content string) {
	fields := strings.Fields(content)

	switch {
	case len(fields) == 2 && fields[0] == "!ban":
		h.BanUser(fields[1])
		reply(c, fmt.Sprintf("banned %s", fields[1]))

	case len(fields) == 3 && fields[0] == "!timeout":
		secs, err := strconv.Atoi(fields[2])
		if err != nil || secs <= 0 {
			c.SendJSON(message.NewError(message.ReasonBadCommand, ""))
			return
		}
		h.TimeoutUser(fields[1], time.Duration(secs)*time.Second)
		reply(c, fmt.Sprintf("timed out %s for %ds", fields[1], secs))

	default:
		c.SendJSON(message.NewError(message.ReasonBadCommand, ""))
	}
}

func reply(c *Client, text string) {
	c.SendJSON(message.Message{
		Type:      message.TypeSystem,
		RoomID:    c.RoomID,
		Content:   text,
		Timestamp: time.Now().UnixMilli(),
	})
}

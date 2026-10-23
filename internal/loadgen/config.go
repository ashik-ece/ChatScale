// Package loadgen drives many WebSocket clients against the gateways and
// measures end-to-end delivery latency, loss and reconnect behaviour.
package loadgen

import (
	"io"
	"time"
)

// Config describes one load test run.
type Config struct {
	// Targets are gateway base URLs (ws://host:port). Connections are spread
	// across them round-robin, and a client that loses its connection moves to
	// the next target, as it would behind a load balancer.
	Targets []string
	Room    string

	Connections  int           // final number of connections
	Start        int           // connections opened immediately (0 means all of them)
	RampStep     int           // connections added every RampInterval until Connections
	RampInterval time.Duration // 0 disables ramping

	Senders int     // how many of the connections also publish
	MsgRate float64 // messages per second per sender

	Duration time.Duration

	// PrometheusURL, when set, is queried for server-side drop counts and
	// latency. StopDropsPerMin > 0 ends the run once the cluster drops more
	// than that many messages in a minute (the "ceiling search").
	PrometheusURL   string
	StopDropsPerMin float64

	// StopP99Ms > 0 also ends a ramping run once a connection level's own p99
	// exceeds it. Overload here shows up as queueing latency long before any
	// buffer-full drops, so a drop threshold alone never trips.
	StopP99Ms float64

	DialConcurrency int

	Scenario string
	Run      int

	Log io.Writer `json:"-"` // progress lines; nil is silent
}

func (c *Config) applyDefaults() {
	if c.Start <= 0 || c.Start > c.Connections {
		c.Start = c.Connections
	}
	if c.Room == "" {
		c.Room = "loadtest"
	}
	if c.DialConcurrency <= 0 {
		c.DialConcurrency = 256
	}
	if c.Senders > c.Connections {
		c.Senders = c.Connections
	}
}

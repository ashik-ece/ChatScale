// Package metrics defines the Prometheus instruments exposed on /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics is the set of instruments for one gateway node. Every series carries
// a constant node label so a dashboard can break results down per node.
type Metrics struct {
	ActiveConnections   prometheus.Gauge
	MessagesTotal       *prometheus.CounterVec
	FanoutLatency       prometheus.Histogram
	MessagesDropped     prometheus.Counter
	RateLimitRejections prometheus.Counter
	ActiveRooms         prometheus.Gauge
}

// fanoutBuckets span 5µs to 1s: delivering to a few local channels takes
// microseconds, far below the 5ms floor of the default buckets.
var fanoutBuckets = []float64{
	0.000005, 0.00001, 0.000025, 0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1,
}

// New registers the instruments on reg. Tests pass a fresh registry so
// instances do not collide.
func New(nodeID string, reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	labels := prometheus.Labels{"node": nodeID}

	return &Metrics{
		ActiveConnections: f.NewGauge(prometheus.GaugeOpts{
			Name:        "chatscale_active_connections",
			Help:        "Current open WebSocket connections.",
			ConstLabels: labels,
		}),
		MessagesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name:        "chatscale_messages_total",
			Help:        "Messages published to Redis, by room.",
			ConstLabels: labels,
		}, []string{"room"}),
		FanoutLatency: f.NewHistogram(prometheus.HistogramOpts{
			Name:        "chatscale_fanout_latency_seconds",
			Help:        "Time from the fan-out goroutine receiving a message to the last local client's channel send.",
			Buckets:     fanoutBuckets,
			ConstLabels: labels,
		}),
		MessagesDropped: f.NewCounter(prometheus.CounterOpts{
			Name:        "chatscale_messages_dropped_total",
			Help:        "Messages dropped because a client's send buffer was full.",
			ConstLabels: labels,
		}),
		RateLimitRejections: f.NewCounter(prometheus.CounterOpts{
			Name:        "chatscale_rate_limit_rejections_total",
			Help:        "Messages rejected by the per-user token bucket.",
			ConstLabels: labels,
		}),
		ActiveRooms: f.NewGauge(prometheus.GaugeOpts{
			Name:        "chatscale_active_rooms",
			Help:        "Rooms with at least one local client (and so a Redis subscription).",
			ConstLabels: labels,
		}),
	}
}

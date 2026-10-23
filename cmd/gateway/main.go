package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/hub"
	"github.com/ashik-ece/chatscale/internal/metrics"
	"github.com/ashik-ece/chatscale/internal/pubsub"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

func main() {
	addr := ":" + getenv("PORT", "8080")
	origins := strings.Split(getenv("ALLOWED_ORIGINS", ""), ",")

	trie, err := filter.LoadBannedWords(getenv("BANNED_WORDS_PATH", "configs/banned_words.txt"))
	if err != nil {
		log.Fatalf("load banned words: %v", err)
	}
	limiter := ratelimit.NewStore(ratelimit.ConfigFromEnv())

	nodeID := getenv("NODE_ID", "node-1")
	rdb := redis.NewClient(&redis.Options{Addr: getenv("REDIS_ADDR", "localhost:6379")})
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}
	cancelPing()

	h := hub.New(limiter, trie, pubsub.NewPublisher(rdb), pubsub.NewSubscriber(rdb), nodeID, metrics.New(nodeID, prometheus.DefaultRegisterer))
	upgrader := ws.NewUpgrader(origins)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, upgrader))
	// pprof, for goroutine-leak checks (registered explicitly: we don't use DefaultServeMux).
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("gateway %s listening on %s", nodeID, addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")

	// Hijacked WebSocket connections are not tracked by srv.Shutdown, so close
	// them through the hub first (this also drains in-flight Redis publishes)
	// and give the 1001 frames a moment to flush.
	h.Shutdown()
	time.Sleep(500 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
	if err := rdb.Close(); err != nil {
		log.Printf("redis close: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

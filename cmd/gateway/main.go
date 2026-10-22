package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/hub"
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

	h := hub.New(limiter, trie)
	upgrader := ws.NewUpgrader(origins)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, upgrader))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("gateway listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")

	// Hijacked WebSocket connections are not tracked by srv.Shutdown, so close
	// them through the hub first and give the 1001 frames a moment to flush.
	h.Shutdown()
	time.Sleep(500 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

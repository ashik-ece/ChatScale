package ws

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// NewUpgrader builds an upgrader that accepts requests with no Origin header
// (CLI tools), localhost origins, and any origin host listed in allowedOrigins.
// A "*" entry allows everything.
func NewUpgrader(allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		CheckOrigin:      originChecker(allowedOrigins),
	}
}

func originChecker(allowed []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}
		for _, a := range allowed {
			a = strings.TrimSpace(a)
			if a == "*" || strings.EqualFold(a, host) || strings.EqualFold(a, u.Host) {
				return true
			}
		}
		return false
	}
}

// ServeWS upgrades GET /ws/{roomID} and starts the connection's pumps.
// The optional ?user= query parameter sets the user ID.
func ServeWS(h Hub, up websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roomID := r.PathValue("roomID")
		if roomID == "" {
			http.Error(w, "missing room", http.StatusBadRequest)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade failed: %v", err)
			return
		}
		c := NewClient(conn, r.URL.Query().Get("user"), roomID)
		h.Register(c)
		go WritePump(c)
		go ReadPump(c, h)
	}
}

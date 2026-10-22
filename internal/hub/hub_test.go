package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ashik-ece/chatscale/internal/hub"
	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/ws"
)

func newServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, ws.NewUpgrader(nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func dial(t *testing.T, srv *httptest.Server, room, user string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + room + "?user=" + user
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func read(c *websocket.Conn, d time.Duration) (message.Message, error) {
	var m message.Message
	_ = c.SetReadDeadline(time.Now().Add(d))
	err := c.ReadJSON(&m)
	return m, err
}

func TestBroadcastAndRoomIsolation(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	other := dial(t, srv, "room2", "carol")
	time.Sleep(50 * time.Millisecond) // let registrations land

	payload, _ := json.Marshal(message.Message{Content: "hello"})
	if err := a.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]*websocket.Conn{"sender": a, "peer": b} {
		m, err := read(c, time.Second)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m.Content != "hello" || m.UserID != "alice" || m.RoomID != "room1" || m.Type != message.TypeChat {
			t.Errorf("%s got %+v", name, m)
		}
	}

	if m, err := read(other, 200*time.Millisecond); err == nil {
		t.Errorf("room2 client received a room1 message: %+v", m)
	}
}

func TestSenderCannotSpoofIdentity(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	payload, _ := json.Marshal(message.Message{Type: "system", UserID: "admin", RoomID: "room2", Content: "x"})
	_ = a.WriteMessage(websocket.TextMessage, payload)

	m, err := read(a, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.UserID != "alice" || m.RoomID != "room1" || m.Type != message.TypeChat {
		t.Errorf("identity was spoofed: %+v", m)
	}
}

func TestShutdownSendsGoingAway(t *testing.T) {
	srv, h := newServer(t)
	a := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	h.Shutdown()

	_ = a.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := a.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Errorf("expected 1001 close, got %v", err)
	}
}

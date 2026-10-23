package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/hub"
	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

func newServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	trie := filter.NewTrie()
	trie.Insert("badword")
	h := hub.New(ratelimit.NewStore(ratelimit.Config{Capacity: 5, RefillRate: 2}), trie)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, ws.NewUpgrader(nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func dial(t *testing.T, srv *httptest.Server, room, user string) *websocket.Conn {
	t.Helper()
	return dialQuery(t, srv, room, "user="+user)
}

func dialMod(t *testing.T, srv *httptest.Server, room, user string) *websocket.Conn {
	t.Helper()
	return dialQuery(t, srv, room, "user="+user+"&mod=true")
}

func dialQuery(t *testing.T, srv *httptest.Server, room, query string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + room + "?" + query
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

func say(t *testing.T, c *websocket.Conn, content string) {
	t.Helper()
	payload, _ := json.Marshal(message.Message{Content: content})
	if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}
}

func readRaw(t *testing.T, c *websocket.Conn, d time.Duration) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	var m map[string]any
	if err := c.ReadJSON(&m); err != nil {
		return nil
	}
	return m
}

func TestRateLimitSixthMessage(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 6; i++ {
		say(t, a, "msg")
	}

	chat, errs := 0, 0
	for i := 0; i < 6; i++ {
		m := readRaw(t, a, time.Second)
		switch {
		case m == nil:
			t.Fatalf("expected 6 frames on sender, got %d", i)
		case m["type"] == "error":
			if m["reason"] != "rate_limited" {
				t.Errorf("unexpected reason %v", m["reason"])
			}
			errs++
		default:
			chat++
		}
	}
	if chat != 5 || errs != 1 {
		t.Errorf("sender got %d chat / %d errors, want 5 / 1", chat, errs)
	}

	got := 0
	for readRaw(t, b, 200*time.Millisecond) != nil {
		got++
	}
	if got != 5 {
		t.Errorf("peer received %d messages, want 5", got)
	}
}

func TestBannedWordIsMasked(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	time.Sleep(50 * time.Millisecond)

	say(t, a, "you are a BadWord, friend")
	m, err := read(b, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.Content != "you are a ****, friend" {
		t.Errorf("content = %q", m.Content)
	}
}

func TestModBanSilentlyDropsUser(t *testing.T) {
	srv, _ := newServer(t)
	mod := dialMod(t, srv, "room1", "mod")
	bad := dial(t, srv, "room1", "testuser123")
	good := dial(t, srv, "room1", "bob")
	time.Sleep(50 * time.Millisecond)

	say(t, mod, "!ban testuser123")
	if m := readRaw(t, mod, time.Second); m == nil || m["type"] != "system" {
		t.Fatalf("mod expected system confirmation, got %v", m)
	}

	say(t, bad, "i am banned")
	say(t, good, "hello")

	// Everyone sees bob's message; nobody sees the banned user's, and the
	// banned user gets no error back.
	for name, c := range map[string]*websocket.Conn{"mod": mod, "good": good} {
		m, err := read(c, time.Second)
		if err != nil || m.UserID != "bob" {
			t.Errorf("%s: got %+v err=%v, want message from bob", name, m, err)
		}
	}
	m, err := read(bad, time.Second)
	if err != nil || m.UserID != "bob" {
		t.Errorf("banned user should still receive others' messages, got %+v err=%v", m, err)
	}
}

func TestModTimeout(t *testing.T) {
	srv, _ := newServer(t)
	mod := dialMod(t, srv, "room1", "mod")
	target := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	say(t, mod, "!timeout alice 60")
	readRaw(t, mod, time.Second) // confirmation

	say(t, target, "hi")
	m := readRaw(t, target, time.Second)
	if m == nil || m["type"] != "error" || m["reason"] != "timed_out" {
		t.Errorf("timed-out user got %v, want timed_out error", m)
	}
}

func TestNonModCommandIsPlainChat(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	time.Sleep(50 * time.Millisecond)

	say(t, a, "!ban bob")
	m, err := read(b, time.Second)
	if err != nil || m.Content != "!ban bob" || m.Type != message.TypeChat {
		t.Fatalf("got %+v err=%v, want ordinary chat", m, err)
	}

	read(a, time.Second) // alice's own echo of "!ban bob"

	say(t, b, "still here")
	if m, err := read(a, time.Second); err != nil || m.Content != "still here" {
		t.Errorf("bob should not be banned, got %+v err=%v", m, err)
	}
}

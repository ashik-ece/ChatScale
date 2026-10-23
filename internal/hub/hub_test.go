package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/hub"
	"github.com/ashik-ece/chatscale/internal/message"
	"github.com/ashik-ece/chatscale/internal/metrics"
	"github.com/ashik-ece/chatscale/internal/pubsub"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

func newServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	return newNode(t, pubsub.NewMemoryBus(), "test-node")
}

// newNode starts one gateway node attached to bus; nodes sharing a bus behave
// like gateways sharing a Redis.
func newNode(t *testing.T, bus *pubsub.MemoryBus, nodeID string) (*httptest.Server, *hub.Hub) {
	t.Helper()
	srv, h, _ := newNodeWithMetrics(t, bus, nodeID)
	return srv, h
}

func newNodeWithMetrics(t *testing.T, bus *pubsub.MemoryBus, nodeID string) (*httptest.Server, *hub.Hub, *metrics.Metrics) {
	t.Helper()
	m := metrics.New(nodeID, prometheus.NewRegistry())
	trie := filter.NewTrie()
	trie.Insert("badword")
	h := hub.New(ratelimit.NewStore(ratelimit.Config{Capacity: 5, RefillRate: 2}), trie, bus, bus.NewSubscriber(), nodeID, m)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, ws.NewUpgrader(nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h, m
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

func TestCrossNodeDelivery(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	srv1, _ := newNode(t, bus, "node-1")
	srv2, _ := newNode(t, bus, "node-2")
	srv3, _ := newNode(t, bus, "node-3")

	sender := dial(t, srv1, "room1", "alice")
	onNode2 := dial(t, srv2, "room1", "bob")
	onNode3 := dial(t, srv3, "room1", "carol")
	otherRoom := dial(t, srv2, "room2", "dave")
	time.Sleep(50 * time.Millisecond)

	say(t, sender, "across nodes")

	for name, c := range map[string]*websocket.Conn{"node-1": sender, "node-2": onNode2, "node-3": onNode3} {
		m, err := read(c, time.Second)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m.Content != "across nodes" || m.NodeID != "node-1" {
			t.Errorf("%s got %+v, want content from node-1", name, m)
		}
	}
	if m, err := read(otherRoom, 200*time.Millisecond); err == nil {
		t.Errorf("room2 client received %+v", m)
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	srv, _ := newNode(t, bus, "node-1")

	if got := bus.Channels(); len(got) != 0 {
		t.Fatalf("expected no channels at start, got %v", got)
	}

	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	waitChannels(t, bus, []string{"room:room1"})

	a.Close()
	time.Sleep(100 * time.Millisecond)
	if got := bus.Channels(); len(got) != 1 {
		t.Fatalf("room must stay subscribed while a client remains, got %v", got)
	}

	b.Close()
	waitChannels(t, bus, nil)
}

func waitChannels(t *testing.T, bus *pubsub.MemoryBus, want []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := bus.Channels()
		if len(got) == len(want) && (len(want) == 0 || got[0] == want[0]) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("channels = %v, want %v", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSurvivingNodesKeepWorkingWhenOneStops(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	srv1, _ := newNode(t, bus, "node-1")
	srv2, h2 := newNode(t, bus, "node-2")
	srv3, _ := newNode(t, bus, "node-3")

	a := dial(t, srv1, "room1", "alice")
	dial(t, srv2, "room1", "bob")
	c := dial(t, srv3, "room1", "carol")
	time.Sleep(50 * time.Millisecond)

	h2.Shutdown()
	srv2.CloseClientConnections()
	time.Sleep(100 * time.Millisecond)

	say(t, a, "still up")
	for name, conn := range map[string]*websocket.Conn{"node-1": a, "node-3": c} {
		if m, err := read(conn, time.Second); err != nil || m.Content != "still up" {
			t.Errorf("%s got %+v err=%v", name, m, err)
		}
	}
}

func TestClientMsgIDIsEchoedOnChatAndError(t *testing.T) {
	srv, _ := newServer(t)
	a := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	ids := []string{"id-a", "id-b", "id-c", "id-d", "id-e", "id-f"}
	for _, id := range ids {
		payload, _ := json.Marshal(message.Message{Content: "x", ClientMsgID: id})
		if err := a.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Fatal(err)
		}
	}

	// Chat echoes travel through pub/sub while errors are queued directly, so
	// their relative order is not guaranteed; clients match on clientMsgId.
	types := map[string]any{}
	for range ids {
		m := readRaw(t, a, time.Second)
		if m == nil {
			t.Fatal("missing frame")
		}
		id, _ := m["clientMsgId"].(string)
		types[id] = m["type"]
	}
	for _, id := range ids[:5] {
		if types[id] != "chat" {
			t.Errorf("%s: got %v, want chat echo", id, types[id])
		}
	}
	if types["id-f"] != "error" {
		t.Errorf("id-f: got %v, want rate-limit error", types["id-f"])
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectionAndRoomGauges(t *testing.T) {
	srv, _, m := newNodeWithMetrics(t, pubsub.NewMemoryBus(), "n")

	a := dial(t, srv, "room1", "alice")
	b := dial(t, srv, "room1", "bob")
	c := dial(t, srv, "room2", "carol")
	eventually(t, "3 connections / 2 rooms", func() bool {
		return testutil.ToFloat64(m.ActiveConnections) == 3 && testutil.ToFloat64(m.ActiveRooms) == 2
	})

	a.Close()
	b.Close()
	eventually(t, "1 connection / 1 room", func() bool {
		return testutil.ToFloat64(m.ActiveConnections) == 1 && testutil.ToFloat64(m.ActiveRooms) == 1
	})
	c.Close()
	eventually(t, "0 connections / 0 rooms", func() bool {
		return testutil.ToFloat64(m.ActiveConnections) == 0 && testutil.ToFloat64(m.ActiveRooms) == 0
	})
}

func TestMessageAndRateLimitCounters(t *testing.T) {
	srv, _, m := newNodeWithMetrics(t, pubsub.NewMemoryBus(), "n")
	a := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 8; i++ { // capacity is 5
		say(t, a, "x")
	}
	eventually(t, "5 published and 3 rejected", func() bool {
		return testutil.ToFloat64(m.MessagesTotal.WithLabelValues("room1")) == 5 &&
			testutil.ToFloat64(m.RateLimitRejections) == 3
	})
	if got := testutil.ToFloat64(m.MessagesDropped); got != 0 {
		t.Errorf("dropped = %v, want 0", got)
	}
}

func TestFanoutLatencyIsObserved(t *testing.T) {
	srv, _, m := newNodeWithMetrics(t, pubsub.NewMemoryBus(), "n")
	a := dial(t, srv, "room1", "alice")
	time.Sleep(50 * time.Millisecond)

	say(t, a, "hi")
	eventually(t, "a fan-out observation", func() bool {
		return testutil.CollectAndCount(m.FanoutLatency) > 0 && histogramCount(t, m) == 1
	})
}

func histogramCount(t *testing.T, m *metrics.Metrics) uint64 {
	t.Helper()
	var pb dto.Metric
	if err := m.FanoutLatency.Write(&pb); err != nil {
		t.Fatal(err)
	}
	return pb.GetHistogram().GetSampleCount()
}

func TestSlowConsumerIncrementsDropCounter(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	m := metrics.New("n", prometheus.NewRegistry())
	trie := filter.NewTrie()
	h := hub.New(ratelimit.NewStore(ratelimit.Config{Capacity: 5, RefillRate: 2}), trie, bus, bus.NewSubscriber(), "n", m)

	// A client with no WritePump never drains its send channel: a slow consumer.
	slow := ws.NewClient(nil, "slow", "room1")
	if err := h.Register(slow); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Unregister(slow) })

	for i := 0; i < 1000; i++ {
		h.Broadcast(message.Message{RoomID: "room1", Content: "x"})
		time.Sleep(100 * time.Microsecond)
	}
	eventually(t, "drops to be counted", func() bool {
		return testutil.ToFloat64(m.MessagesDropped) > 0
	})
}

func TestClosingClientIsNotCountedAsDrop(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	m := metrics.New("n", prometheus.NewRegistry())
	h := hub.New(ratelimit.NewStore(ratelimit.Config{Capacity: 5, RefillRate: 2}), filter.NewTrie(), bus, bus.NewSubscriber(), "n", m)

	c := ws.NewClient(nil, "leaving", "room1")
	if err := h.Register(c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Unregister(c) })
	c.Close() // closing, but still registered, as during a disconnect

	for i := 0; i < 50; i++ {
		h.Broadcast(message.Message{RoomID: "room1", Content: "x"})
	}
	eventually(t, "messages to be published", func() bool {
		return testutil.ToFloat64(m.MessagesTotal.WithLabelValues("room1")) == 50
	})
	time.Sleep(100 * time.Millisecond)
	if got := testutil.ToFloat64(m.MessagesDropped); got != 0 {
		t.Errorf("dropped = %v, want 0 for a closing client", got)
	}
}

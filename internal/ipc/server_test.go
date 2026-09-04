package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
	"github.com/HsiuCheYu/quietdm.nvim/internal/transport"
)

// stubHandler is a session stand-in.
type stubHandler struct {
	mu        sync.Mutex
	connected bool
	rooms     []model.Room
	messages  []model.Message
	sendErr   error
	marked    []string
}

func (h *stubHandler) Send(_ context.Context, roomID, body string) (string, error) {
	if h.sendErr != nil {
		return "", h.sendErr
	}
	return "$sent", nil
}

func (h *stubHandler) MarkRead(_ context.Context, roomID, eventID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.marked = append(h.marked, roomID+":"+eventID)
	return nil
}

func (h *stubHandler) History(context.Context, string, int) ([]model.Message, error) {
	return h.messages, nil
}

func (h *stubHandler) Rooms(context.Context) ([]model.Room, error) { return h.rooms, nil }

func (h *stubHandler) Knows(roomID string) bool { return roomID == "!r:localhost" }

func (h *stubHandler) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connected
}

type testClient struct {
	conn net.Conn
	r    *bufio.Reader
	t    *testing.T
}

func (c *testClient) send(v any) {
	c.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *testClient) read() map[string]any {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(line, &out); err != nil {
		c.t.Fatalf("decode %q: %v", line, err)
	}
	return out
}

// readUntil skips broadcasts until the wanted event type shows up.
func (c *testClient) readUntil(kind string) map[string]any {
	c.t.Helper()
	for i := 0; i < 20; i++ {
		ev := c.read()
		if ev["t"] == kind {
			return ev
		}
	}
	c.t.Fatalf("never saw a %q event", kind)
	return nil
}

func newServer(t *testing.T, h *stubHandler) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "sock")
	srv := NewServer(path, "quietdmd/test", h, nil)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = srv.Close()
	})
	return srv, path
}

func dial(t *testing.T, path string) *testClient {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &testClient{conn: conn, r: bufio.NewReader(conn), t: t}
}

func hello(t *testing.T, c *testClient) map[string]any {
	t.Helper()
	c.send(Command{T: CmdHello, ID: "1", Proto: ProtoVersion, Client: "test"})
	return c.readUntil(EvReady)
}

func TestSocketPermissions(t *testing.T) {
	_, path := newServer(t, &stubHandler{connected: true})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("socket dir mode = %o, want 700", perm)
	}
}

func TestHelloIsRequiredFirst(t *testing.T) {
	_, path := newServer(t, &stubHandler{connected: true})
	c := dial(t, path)
	c.send(Command{T: CmdRooms, ID: "1"})
	ev := c.read()
	if ev["t"] != EvError || ev["code"] != ErrBadRequest {
		t.Fatalf("got %v, want a bad_request error", ev)
	}
}

func TestHelloRejectsAnotherProtocolVersion(t *testing.T) {
	_, path := newServer(t, &stubHandler{connected: true})
	c := dial(t, path)
	c.send(Command{T: CmdHello, ID: "1", Proto: 99})
	ev := c.read()
	if ev["t"] != EvError || ev["code"] != ErrBadProto {
		t.Fatalf("got %v, want bad_proto", ev)
	}
}

func TestReadyCarriesConnectionState(t *testing.T) {
	h := &stubHandler{connected: false}
	_, path := newServer(t, h)
	c := dial(t, path)
	ev := hello(t, c)
	if ev["connected"] != false {
		t.Errorf("connected = %v, want false", ev["connected"])
	}
	if ev["proto"] != float64(ProtoVersion) {
		t.Errorf("proto = %v", ev["proto"])
	}
	if ev["daemon"] != "quietdmd/test" {
		t.Errorf("daemon = %v", ev["daemon"])
	}
}

func TestRoomsAndHistoryAnswerTheCaller(t *testing.T) {
	h := &stubHandler{
		connected: true,
		rooms:     []model.Room{{Room: "!r:localhost", Display: "m.chen", Unread: 2, LastTS: 100}},
		messages:  []model.Message{{Room: "!r:localhost", Event: "$1", Body: "hi"}},
	}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)

	c.send(Command{T: CmdRooms, ID: "2"})
	ev := c.readUntil(EvRooms)
	if ev["id"] != "2" {
		t.Errorf("id = %v, want the request id echoed back", ev["id"])
	}
	rooms := ev["rooms"].([]any)
	if len(rooms) != 1 || rooms[0].(map[string]any)["display"] != "m.chen" {
		t.Errorf("rooms = %v", rooms)
	}

	c.send(Command{T: CmdHistory, ID: "3", Room: "!r:localhost", Limit: 5})
	ev = c.readUntil(EvHistory)
	if ev["room"] != "!r:localhost" || len(ev["messages"].([]any)) != 1 {
		t.Errorf("history = %v", ev)
	}
}

func TestSendAcksAndUnknownRoomFails(t *testing.T) {
	h := &stubHandler{connected: true}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)

	c.send(Command{T: CmdSend, ID: "2", Room: "!r:localhost", Body: "hi"})
	ev := c.readUntil(EvAck)
	if ev["event"] != "$sent" {
		t.Errorf("ack = %v", ev)
	}

	c.send(Command{T: CmdSend, ID: "3", Room: "!nope", Body: "hi"})
	ev = c.readUntil(EvError)
	if ev["code"] != ErrUnknownRoom {
		t.Errorf("code = %v, want unknown_room", ev["code"])
	}
}

func TestSendWhileOfflineFails(t *testing.T) {
	h := &stubHandler{connected: false}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)
	c.send(Command{T: CmdSend, ID: "2", Room: "!r:localhost", Body: "hi"})
	ev := c.readUntil(EvError)
	if ev["code"] != ErrOffline {
		t.Errorf("code = %v, want offline", ev["code"])
	}
}

func TestErrorsCarryTheRequestID(t *testing.T) {
	h := &stubHandler{connected: true, sendErr: transport.ErrUnknownRoom}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)
	c.send(Command{T: CmdSend, ID: "42", Room: "!r:localhost", Body: "hi"})
	ev := c.readUntil(EvError)
	if ev["id"] != "42" || ev["code"] != ErrUnknownRoom {
		t.Errorf("error = %v", ev)
	}
}

func TestBroadcastReachesEveryGreetedClient(t *testing.T) {
	h := &stubHandler{connected: true}
	srv, path := newServer(t, h)
	a, b := dial(t, path), dial(t, path)
	hello(t, a)
	hello(t, b)

	srv.BroadcastMessage(model.Message{Room: "!r:localhost", Event: "$1", Display: "m.chen", Body: "hi"})
	for _, c := range []*testClient{a, b} {
		ev := c.readUntil(EvMessage)
		if ev["event"] != "$1" || ev["display"] != "m.chen" {
			t.Errorf("message = %v", ev)
		}
	}
}

func TestBroadcastSkipsClientsBeforeHello(t *testing.T) {
	h := &stubHandler{connected: true}
	srv, path := newServer(t, h)
	quiet := dial(t, path)

	srv.BroadcastMessage(model.Message{Room: "!r:localhost", Event: "$1"})
	_ = quiet.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, err := quiet.r.ReadBytes('\n'); err == nil {
		t.Fatal("a client that has not said hello must receive nothing")
	}
}

func TestMarkReadIsAcknowledged(t *testing.T) {
	h := &stubHandler{connected: true}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)
	c.send(Command{T: CmdMarkRead, ID: "2", Room: "!r:localhost", Event: "$1"})
	if ev := c.readUntil(EvAck); ev["id"] != "2" {
		t.Errorf("ack = %v", ev)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.marked) != 1 || h.marked[0] != "!r:localhost:$1" {
		t.Errorf("marked = %v", h.marked)
	}
}

func TestOverlongLineDropsTheConnection(t *testing.T) {
	h := &stubHandler{connected: true}
	_, path := newServer(t, h)
	c := dial(t, path)
	hello(t, c)
	// One line over the 64 KiB protocol limit.
	c.send(Command{T: CmdSend, ID: "2", Room: "!r:localhost", Body: strings.Repeat("x", MaxLine)})
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			return // the daemon hung up, which is what the protocol requires
		}
		if strings.Contains(string(line), `"t":"ack"`) {
			t.Fatal("an over-long line must not be accepted")
		}
	}
}

func TestListenRefusesToStealALiveSocket(t *testing.T) {
	h := &stubHandler{connected: true}
	_, path := newServer(t, h)
	second := NewServer(path, "quietdmd/test", h, nil)
	if err := second.Listen(); err == nil {
		_ = second.Close()
		t.Fatal("want an error when another daemon is already listening")
	}
}

func TestListenClearsAStaleSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sock")
	// A file left behind by a daemon that did not shut down cleanly.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(path, "quietdmd/test", &stubHandler{}, nil)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	_ = srv.Close()
}

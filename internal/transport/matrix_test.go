package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

const (
	testUser  = "@me:localhost"
	testRoom  = "!r:localhost"
	testPeer  = "@mia:localhost"
	testToken = "syt_token"
)

// fakeHomeserver is the handful of Matrix endpoints this transport actually
// touches. It is enough to drive the whole thing — sync, send, receipts,
// reconnect — without standing up Synapse.
type fakeHomeserver struct {
	*httptest.Server

	// syncs hands out one response body per /sync request. A request with
	// nothing queued blocks until the client gives up, which is what a real
	// long-poll does.
	syncs chan string

	mu       sync.Mutex
	since    []string
	sent     []map[string]any
	receipts []string
	failSync bool
	// roomName and extraMember turn the room into a named group, which is
	// named differently from a one-to-one conversation.
	roomName    string
	extraMember bool
}

func newFakeHomeserver(t *testing.T) *fakeHomeserver {
	t.Helper()
	hs := &fakeHomeserver{syncs: make(chan string, 8)}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /_matrix/client/v3/account/whoami", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"user_id": testUser, "device_id": "DEVICE"})
	})
	mux.HandleFunc("POST /_matrix/client/v3/user/{userID}/filter", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"filter_id": "f1"})
	})
	mux.HandleFunc("GET /_matrix/client/v3/joined_rooms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"joined_rooms": []string{testRoom}})
	})
	mux.HandleFunc("GET /_matrix/client/v3/rooms/{roomID}/joined_members", func(w http.ResponseWriter, r *http.Request) {
		joined := map[string]any{
			testUser: map[string]string{"display_name": "me"},
			testPeer: map[string]string{"display_name": "Mia Chen"},
		}
		hs.mu.Lock()
		if hs.extraMember {
			joined["@bo:localhost"] = map[string]string{"display_name": "Bo"}
		}
		hs.mu.Unlock()
		writeJSON(w, map[string]any{"joined": joined})
	})
	mux.HandleFunc("GET /_matrix/client/v3/rooms/{roomID}/state/m.room.name/{$}", func(w http.ResponseWriter, r *http.Request) {
		hs.mu.Lock()
		name := hs.roomName
		hs.mu.Unlock()
		if name == "" {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"errcode": "M_NOT_FOUND", "error": "no name"})
			return
		}
		writeJSON(w, map[string]string{"name": name})
	})
	mux.HandleFunc("PUT /_matrix/client/v3/rooms/{roomID}/send/{eventType}/{txnID}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["__room"] = r.PathValue("roomID")
		hs.mu.Lock()
		hs.sent = append(hs.sent, body)
		hs.mu.Unlock()
		writeJSON(w, map[string]string{"event_id": "$sent"})
	})
	mux.HandleFunc("POST /_matrix/client/v3/rooms/{roomID}/receipt/{receiptType}/{eventID}", func(w http.ResponseWriter, r *http.Request) {
		hs.mu.Lock()
		hs.receipts = append(hs.receipts, r.PathValue("receiptType")+" "+r.PathValue("eventID"))
		hs.mu.Unlock()
		writeJSON(w, map[string]any{})
	})
	mux.HandleFunc("GET /_matrix/client/v3/sync", func(w http.ResponseWriter, r *http.Request) {
		hs.mu.Lock()
		hs.since = append(hs.since, r.URL.Query().Get("since"))
		fail := hs.failSync
		hs.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, map[string]string{"errcode": "M_UNKNOWN", "error": "homeserver is down"})
			return
		}
		select {
		case body := <-hs.syncs:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case <-r.Context().Done():
		}
	})

	hs.Server = httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return hs
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// syncBody builds a /sync response carrying one timeline event.
func syncBody(nextBatch, eventJSON string) string {
	return fmt.Sprintf(`{"next_batch":%q,"rooms":{"join":{%q:{"timeline":{"events":[%s]}}}}}`,
		nextBatch, testRoom, eventJSON)
}

func messageJSON(id, sender, msgtype, body string) string {
	return fmt.Sprintf(`{"type":"m.room.message","event_id":%q,"sender":%q,
		"origin_server_ts":1757000000000,"content":{"msgtype":%q,"body":%q}}`,
		id, sender, msgtype, body)
}

func (hs *fakeHomeserver) sinceValues() []string {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return append([]string(nil), hs.since...)
}

func newTestMatrix(t *testing.T, hs *fakeHomeserver, dir string) *Matrix {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	m, err := NewMatrix(MatrixOptions{
		Homeserver: hs.URL,
		UserID:     testUser,
		DeviceID:   "DEVICE",
		Token:      testToken,
		SessionDB:  filepath.Join(dir, "matrix.db"),
		MinBackoff: 5 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Failures are the test's business, not the HTTP layer's.
	m.cli.DefaultHTTPRetries = 0
	t.Cleanup(func() { m.Close() })
	return m
}

// next waits for one event of the given type, ignoring the others.
func next(t *testing.T, events <-chan Event, want EventType) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("the event channel closed while waiting for %s", want)
			}
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func TestMatrixNeedsHomeserverUserAndToken(t *testing.T) {
	base := MatrixOptions{Homeserver: "http://localhost:8008", UserID: testUser, Token: testToken, SessionDB: filepath.Join(t.TempDir(), "m.db")}
	for _, tc := range []struct {
		name   string
		mutate func(*MatrixOptions)
	}{
		{"no homeserver", func(o *MatrixOptions) { o.Homeserver = "" }},
		{"no user", func(o *MatrixOptions) { o.UserID = "" }},
		{"no token", func(o *MatrixOptions) { o.Token = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			tc.mutate(&opts)
			if _, err := NewMatrix(opts); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// A one-to-one room is named after the other member's user ID: the session
// layer then applies the alias map, exactly as it does for the mock transport.
func TestMatrixNamesADirectRoomAfterTheOtherMember(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")

	rooms, err := m.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].ID != testRoom || rooms[0].Display != testPeer {
		t.Fatalf("rooms = %+v", rooms)
	}
}

// A group room has no single contact to be named after, so it falls back to the
// room name. Groups do not fit in one line of blame text; see the roadmap.
func TestMatrixNamesAGroupRoomAfterTheRoom(t *testing.T) {
	hs := newFakeHomeserver(t)
	hs.extraMember = true
	hs.roomName = "lunch crew"
	m := newTestMatrix(t, hs, "")

	rooms, err := m.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Display != "lunch crew" {
		t.Fatalf("rooms = %+v", rooms)
	}

	hs.mu.Lock()
	hs.roomName = ""
	hs.mu.Unlock()
	rooms, err = m.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Display != testRoom {
		t.Fatalf("an unnamed group falls back to the room ID: %+v", rooms)
	}
}

func TestMatrixDeliversTimelineMessages(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The first response establishes the sync position; only what comes after
	// it counts as news.
	hs.syncs <- syncBody("s1", messageJSON("$old", testPeer, "m.text", "old news"))
	hs.syncs <- syncBody("s2", messageJSON("$1", testPeer, "m.text", "晚上要吃什麼"))

	ev := next(t, events, EventMessage)
	want := model.Message{
		Room:   testRoom,
		Event:  "$1",
		Sender: testPeer,
		Body:   "晚上要吃什麼",
		TS:     1757000000000,
		Kind:   model.KindText,
	}
	if ev.Message != want {
		t.Fatalf("got %+v, want %+v", ev.Message, want)
	}
}

// The first sync returns whatever the homeserver still holds. Announcing that
// backlog would light up the statusline for a conversation that ended days ago.
func TestMatrixIgnoresTheInitialSyncBacklog(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hs.syncs <- syncBody("s1", messageJSON("$old", testPeer, "m.text", "old news"))
	hs.syncs <- syncBody("s2", messageJSON("$new", testPeer, "m.text", "new"))

	if ev := next(t, events, EventMessage); ev.Message.Event != "$new" {
		t.Fatalf("the backlog leaked through: %+v", ev.Message)
	}
}

func TestMatrixMarksOwnMessagesAndMediaKinds(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hs.syncs <- syncBody("s1", messageJSON("$0", testPeer, "m.text", "hello"))
	hs.syncs <- syncBody("s2", messageJSON("$mine", testUser, "m.text", "七點見"))
	hs.syncs <- syncBody("s3", messageJSON("$pic", testPeer, "m.image", "IMG_0421.jpg"))

	if ev := next(t, events, EventMessage); !ev.Message.Own {
		t.Errorf("a message from our own user ID must be marked own: %+v", ev.Message)
	}
	ev := next(t, events, EventMessage)
	if ev.Message.Kind != model.KindImage {
		t.Errorf("kind = %q, want image", ev.Message.Kind)
	}
	if ev.Message.Own {
		t.Errorf("a message from the contact is not own: %+v", ev.Message)
	}
}

func TestMatrixSendAndMarkRead(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	ctx := context.Background()

	id, err := m.Send(ctx, testRoom, "七點拉麵店見")
	if err != nil {
		t.Fatal(err)
	}
	if id != "$sent" {
		t.Errorf("event id = %q", id)
	}
	if err := m.MarkRead(ctx, testRoom, "$1"); err != nil {
		t.Fatal(err)
	}
	// An empty event ID is not a receipt for the whole room; it is nothing.
	if err := m.MarkRead(ctx, testRoom, ""); err != nil {
		t.Fatal(err)
	}

	hs.mu.Lock()
	defer hs.mu.Unlock()
	if len(hs.sent) != 1 || hs.sent[0]["body"] != "七點拉麵店見" || hs.sent[0]["msgtype"] != "m.text" {
		t.Fatalf("sent = %+v", hs.sent)
	}
	if hs.sent[0]["__room"] != testRoom {
		t.Errorf("sent to room %v", hs.sent[0]["__room"])
	}
	if len(hs.receipts) != 1 || hs.receipts[0] != "m.read $1" {
		t.Fatalf("receipts = %v", hs.receipts)
	}
}

// A connection problem only ever moves a flag; nothing about it may reach the
// screen (invariant I3). What the transport owes the session layer is an
// accurate flag, and a reconnection without being asked.
func TestMatrixReportsTheLinkGoingDownAndComingBack(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hs.syncs <- syncBody("s1", "")
	if ev := next(t, events, EventConnected); !ev.Connected {
		t.Fatal("the first successful sync must report connected")
	}

	hs.mu.Lock()
	hs.failSync = true
	hs.mu.Unlock()
	if ev := next(t, events, EventConnected); ev.Connected {
		t.Fatal("a failing homeserver must report disconnected")
	}

	hs.mu.Lock()
	hs.failSync = false
	hs.mu.Unlock()
	hs.syncs <- syncBody("s2", "")
	if ev := next(t, events, EventConnected); !ev.Connected {
		t.Fatal("the transport must reconnect on its own")
	}
}

// The sync position is persisted, so a restarted daemon picks up what it missed
// instead of replaying the conversation from the beginning.
func TestMatrixResumesFromTheStoredSyncPosition(t *testing.T) {
	hs := newFakeHomeserver(t)
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	m := newTestMatrix(t, hs, dir)
	events, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hs.syncs <- syncBody("s1", "")
	hs.syncs <- syncBody("s2", messageJSON("$1", testPeer, "m.text", "hi"))
	next(t, events, EventMessage)
	cancel()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	m2 := newTestMatrix(t, hs, dir)
	events2, err := m2.Start(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	hs.syncs <- syncBody("s3", messageJSON("$2", testPeer, "m.text", "still here"))
	// A resumed daemon is not doing an initial sync, so nothing is skipped.
	if ev := next(t, events2, EventMessage); ev.Message.Event != "$2" {
		t.Fatalf("got %+v", ev.Message)
	}
	for _, since := range hs.sinceValues() {
		if since == "s2" {
			return
		}
	}
	t.Fatalf("the second run did not resume from the stored token: %v", hs.sinceValues())
}

// The session database holds decryption keys, so it is the last file on the
// machine that should be readable by anyone else.
func TestMatrixSessionDBPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "matrix.db")
	db, err := openSessionDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("session db mode = %o, want 600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %o, want 700", perm)
	}
}

func TestPickleKeyIsCreatedOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "pickle.key")
	first, err := loadOrCreatePickleKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != pickleKeySize {
		t.Fatalf("key is %d bytes, want %d", len(first), pickleKeySize)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("pickle key mode = %o, want 600", perm)
	}
	// A regenerated key would make every stored Olm session unreadable.
	second, err := loadOrCreatePickleKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("the pickle key changed between runs")
	}
}

func TestPickleKeyRejectsAWrongSizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pickle.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreatePickleKey(path); err == nil {
		t.Fatal("a truncated key file must be reported, not silently replaced")
	}
}

// systemd will start the daemon and the homeserver in the same second. A
// daemon that dies because the network is not up yet is a daemon the user has
// to babysit, so neither Start nor Rooms may fail on an unreachable server.
func TestMatrixStartsWithoutAReachableHomeserver(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	hs.Close() // nothing is listening any more

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := m.Start(ctx); err != nil {
		t.Fatalf("Start must not fail on an unreachable homeserver: %v", err)
	}
	rooms, err := m.Rooms(ctx)
	if err != nil {
		t.Fatalf("Rooms must not fail on an unreachable homeserver: %v", err)
	}
	if len(rooms) != 0 {
		t.Fatalf("rooms = %+v", rooms)
	}
}

// A room list fetched once must survive the homeserver going away, or a
// reconnect would leave the frontend unable to reply to anyone.
func TestMatrixKeepsTheLastKnownRoomList(t *testing.T) {
	hs := newFakeHomeserver(t)
	m := newTestMatrix(t, hs, "")
	if _, err := m.Rooms(context.Background()); err != nil {
		t.Fatal(err)
	}
	hs.Close()
	rooms, err := m.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Display != testPeer {
		t.Fatalf("rooms = %+v", rooms)
	}
}

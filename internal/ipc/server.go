package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
	"github.com/HsiuCheYu/quietdm.nvim/internal/transport"
)

// Handler is the daemon logic the server exposes over the socket. The session
// layer implements it.
type Handler interface {
	Send(ctx context.Context, roomID, body string) (string, error)
	MarkRead(ctx context.Context, roomID, eventID string) error
	History(ctx context.Context, roomID string, limit int) ([]model.Message, error)
	Rooms(ctx context.Context) ([]model.Room, error)
	Knows(roomID string) bool
	Connected() bool
}

// clientQueue is how many events may pile up for one frontend before the
// oldest are dropped. A slow client must never block the broadcast loop
// (docs/design/02-architecture.md).
const clientQueue = 64

// Server accepts frontend connections on a Unix socket.
type Server struct {
	path    string
	version string
	handler Handler
	log     *slog.Logger

	ln net.Listener

	mu      sync.Mutex
	clients map[*client]struct{}
	closed  bool
}

// NewServer builds a server for the given socket path.
func NewServer(path, version string, h Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	return &Server{
		path:    path,
		version: version,
		handler: h,
		log:     log,
		clients: make(map[*client]struct{}),
	}
}

// Listen creates the socket directory (0700), clears a stale socket left by a
// previous run, and starts listening with 0600 permissions.
func (s *Server) Listen() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod socket dir: %w", err)
	}
	if err := s.clearStale(); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	s.ln = ln
	return nil
}

// clearStale removes a socket file nobody is listening on. If something does
// answer, another daemon is already running and we refuse to take over.
func (s *Server) clearStale() error {
	if _, err := os.Stat(s.path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket: %w", err)
	}
	conn, err := net.DialTimeout("unix", s.path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("another quietdmd is already listening on %s", s.path)
	}
	if err := os.Remove(s.path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

// Addr reports the socket path in use.
func (s *Server) Addr() string { return s.path }

// Serve accepts connections until ctx is cancelled or the listener closes.
func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		return errors.New("ipc: Listen must be called before Serve")
	}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if s.isClosed() || ctx.Err() != nil {
				return nil
			}
			return err
		}
		c := newClient(conn, s)
		s.add(c)
		go c.serve(ctx)
	}
}

// Close shuts the listener, drops every client, and removes the socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = make(map[*client]struct{})
	s.mu.Unlock()

	if s.ln != nil {
		_ = s.ln.Close()
	}
	for _, c := range clients {
		c.close()
	}
	_ = os.Remove(s.path)
	return nil
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) add(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
}

func (s *Server) remove(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

// BroadcastMessage pushes a message to every frontend past hello.
func (s *Server) BroadcastMessage(m model.Message) { s.broadcast(NewMessage(m)) }

// BroadcastRoom pushes a room state change to every frontend past hello.
func (s *Server) BroadcastRoom(r model.Room) { s.broadcast(NewRoom(r)) }

func (s *Server) broadcast(v any) {
	line, err := encode(v)
	if err != nil {
		s.log.Warn("encode broadcast", "err", err)
		return
	}
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		if c.greeted() {
			c.enqueue(line)
		}
	}
}

// encode renders one NDJSON line.
func encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxLine {
		return nil, fmt.Errorf("event exceeds %d bytes", MaxLine)
	}
	return append(b, '\n'), nil
}

// client is one connected frontend.
type client struct {
	conn net.Conn
	srv  *Server
	out  chan []byte

	mu      sync.Mutex
	hello   bool
	closed  bool
	dropped int
}

func newClient(conn net.Conn, srv *Server) *client {
	return &client{conn: conn, srv: srv, out: make(chan []byte, clientQueue)}
}

func (c *client) greeted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hello
}

// enqueue queues a line, dropping this client's oldest event when its queue is
// full rather than stalling the daemon.
func (c *client) enqueue(line []byte) {
	for {
		select {
		case c.out <- line:
			return
		default:
		}
		select {
		case <-c.out:
			c.mu.Lock()
			c.dropped++
			n := c.dropped
			c.mu.Unlock()
			c.srv.log.Warn("dropped event for slow client", "dropped", n)
		default:
			// The reader drained it in the meantime; try again.
		}
	}
}

func (c *client) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.Close()
}

func (c *client) serve(ctx context.Context) {
	defer func() {
		c.srv.remove(c)
		c.close()
	}()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		w := bufio.NewWriter(c.conn)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-c.out:
				if !ok {
					return
				}
				if _, err := w.Write(line); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	}()

	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 4096), MaxLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if !c.dispatch(ctx, line) {
			break
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		c.srv.log.Debug("client read ended", "err", err)
	}
	c.close()
	<-writerDone
}

// dispatch handles one command and reports whether the connection may stay
// open.
func (c *client) dispatch(ctx context.Context, line []byte) bool {
	var cmd Command
	if err := json.Unmarshal(line, &cmd); err != nil {
		c.send(NewError("", ErrBadRequest, "malformed json"))
		return false
	}
	if cmd.T != CmdHello && !c.greeted() {
		c.send(NewError(cmd.ID, ErrBadRequest, "hello required first"))
		return false
	}

	switch cmd.T {
	case CmdHello:
		return c.handleHello(cmd)
	case CmdSend:
		c.handleSend(ctx, cmd)
	case CmdMarkRead:
		c.handleMarkRead(ctx, cmd)
	case CmdHistory:
		c.handleHistory(ctx, cmd)
	case CmdRooms:
		c.handleRooms(ctx, cmd)
	default:
		c.send(NewError(cmd.ID, ErrBadRequest, "unknown command "+cmd.T))
	}
	return true
}

func (c *client) handleHello(cmd Command) bool {
	if cmd.Proto != ProtoVersion {
		c.send(NewError(cmd.ID, ErrBadProto, fmt.Sprintf("daemon speaks proto %d", ProtoVersion)))
		return false
	}
	c.mu.Lock()
	c.hello = true
	c.mu.Unlock()
	c.srv.log.Debug("client hello", "client", cmd.Client)
	c.send(NewReady(cmd.ID, c.srv.handler.Connected(), c.srv.version))
	return true
}

func (c *client) handleSend(ctx context.Context, cmd Command) {
	if cmd.Room == "" || cmd.Body == "" {
		c.send(NewError(cmd.ID, ErrBadRequest, "send needs room and body"))
		return
	}
	if !c.srv.handler.Knows(cmd.Room) {
		c.send(NewError(cmd.ID, ErrUnknownRoom, "room not found"))
		return
	}
	if !c.srv.handler.Connected() {
		c.send(NewError(cmd.ID, ErrOffline, "not connected"))
		return
	}
	id, err := c.srv.handler.Send(ctx, cmd.Room, cmd.Body)
	if err != nil {
		c.send(NewError(cmd.ID, codeFor(err), err.Error()))
		return
	}
	c.send(NewAck(cmd.ID, id))
}

func (c *client) handleMarkRead(ctx context.Context, cmd Command) {
	if cmd.Room == "" {
		c.send(NewError(cmd.ID, ErrBadRequest, "mark_read needs room"))
		return
	}
	if !c.srv.handler.Knows(cmd.Room) {
		c.send(NewError(cmd.ID, ErrUnknownRoom, "room not found"))
		return
	}
	if err := c.srv.handler.MarkRead(ctx, cmd.Room, cmd.Event); err != nil {
		c.send(NewError(cmd.ID, codeFor(err), err.Error()))
		return
	}
	c.send(NewAck(cmd.ID, ""))
}

func (c *client) handleHistory(ctx context.Context, cmd Command) {
	if cmd.Room == "" {
		c.send(NewError(cmd.ID, ErrBadRequest, "history needs room"))
		return
	}
	limit := cmd.Limit
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	if limit > MaxHistoryLimit {
		limit = MaxHistoryLimit
	}
	msgs, err := c.srv.handler.History(ctx, cmd.Room, limit)
	if err != nil {
		c.send(NewError(cmd.ID, codeFor(err), err.Error()))
		return
	}
	c.send(NewHistory(cmd.ID, cmd.Room, msgs))
}

func (c *client) handleRooms(ctx context.Context, cmd Command) {
	rooms, err := c.srv.handler.Rooms(ctx)
	if err != nil {
		c.send(NewError(cmd.ID, codeFor(err), err.Error()))
		return
	}
	c.send(NewRooms(cmd.ID, rooms))
}

func codeFor(err error) string {
	switch {
	case errors.Is(err, transport.ErrUnknownRoom):
		return ErrUnknownRoom
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrOffline
	}
	return ErrInternal
}

func (c *client) send(v any) {
	line, err := encode(v)
	if err != nil {
		c.srv.log.Warn("encode reply", "err", err)
		return
	}
	c.enqueue(line)
}

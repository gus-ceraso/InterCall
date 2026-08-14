package websocket

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	intercall "github.com/cerasos/intercall/go"
)

// Handler upgrades authenticated HTTP requests into negotiated InterCall
// connections. Authentication and authorization remain the surrounding HTTP
// application's responsibility.
type Handler struct {
	export intercall.ExportBinding
	imp    intercall.ImportBinding
	err    error

	mu     sync.Mutex
	closed bool
	setup  map[intercall.ByteStream]context.CancelFunc
	active map[*intercall.Connection]context.CancelFunc
	wg     sync.WaitGroup
}

// NewHandler constructs a reusable WebSocket InterCall handler. Invalid
// bindings are reported as HTTP 500 by ServeHTTP rather than causing a panic.
func NewHandler(export intercall.ExportBinding, imp intercall.ImportBinding) *Handler {
	h := &Handler{
		export: export,
		imp:    imp,
		setup:  make(map[intercall.ByteStream]context.CancelFunc),
		active: make(map[*intercall.Connection]context.CancelFunc),
	}
	h.err = validateNegotiatedBindings(context.Background(), export, imp)
	return h
}

func (h *Handler) begin() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("websocket: handler is shut down: %w", errHandlerClosed)
	}
	h.wg.Add(1)
	return nil
}

func (h *Handler) registerSetup(stream intercall.ByteStream, cancel context.CancelFunc) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.setup[stream] = cancel
	return true
}

func (h *Handler) promote(stream intercall.ByteStream, conn *intercall.Connection, cancel context.CancelFunc) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.setup, stream)
	if h.closed {
		return false
	}
	h.active[conn] = cancel
	return true
}

func (h *Handler) remove(stream intercall.ByteStream, conn *intercall.Connection) {
	h.mu.Lock()
	delete(h.setup, stream)
	if conn != nil {
		delete(h.active, conn)
	}
	h.mu.Unlock()
}

// ServeHTTP upgrades one request, negotiates the server-side interfaces, and
// serves the connection until it terminates. Errors after upgrade close the
// transport without attempting another HTTP response.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "intercall: nil handler", http.StatusInternalServerError)
		return
	}
	if r == nil {
		http.Error(w, "intercall: nil request", http.StatusInternalServerError)
		return
	}
	if err := h.begin(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer h.wg.Done()
	if h.err != nil {
		http.Error(w, h.err.Error(), http.StatusInternalServerError)
		return
	}

	base := context.WithoutCancel(r.Context())
	connCtx, cancel := context.WithCancel(base)
	stream, err := AcceptStream(connCtx, w, r, nil)
	if err != nil {
		cancel()
		return
	}
	if !h.registerSetup(stream, cancel) {
		cancel()
		_ = stream.Close()
		return
	}
	conn, err := intercall.NewNegotiatedServerConnection(connCtx, stream, h.export, h.imp)
	if err != nil {
		h.remove(stream, nil)
		cancel()
		_ = stream.Close()
		return
	}
	if !h.promote(stream, conn, cancel) {
		_ = conn.Close()
		_ = conn.Wait()
		cancel()
		return
	}
	_ = conn.Wait()
	h.remove(stream, conn)
	cancel()
}

// Shutdown rejects future upgrades, closes setup streams and active
// connections, and waits for handler executions until ctx expires.
func (h *Handler) Shutdown(ctx context.Context) error {
	if h == nil {
		return fmt.Errorf("websocket: nil handler: %w", intercall.ErrInvalidArgument)
	}
	if ctx == nil {
		return fmt.Errorf("websocket: nil shutdown context: %w", intercall.ErrInvalidArgument)
	}
	h.mu.Lock()
	h.closed = true
	setups := make([]struct {
		stream intercall.ByteStream
		cancel context.CancelFunc
	}, 0, len(h.setup))
	for stream, cancel := range h.setup {
		setups = append(setups, struct {
			stream intercall.ByteStream
			cancel context.CancelFunc
		}{stream, cancel})
	}
	active := make([]struct {
		conn   *intercall.Connection
		cancel context.CancelFunc
	}, 0, len(h.active))
	for conn, cancel := range h.active {
		active = append(active, struct {
			conn   *intercall.Connection
			cancel context.CancelFunc
		}{conn, cancel})
	}
	h.mu.Unlock()
	for _, item := range setups {
		item.cancel()
		_ = item.stream.Close()
	}
	for _, item := range active {
		item.cancel()
		_ = item.conn.Close()
	}
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var errHandlerClosed = fmt.Errorf("websocket: handler closed")

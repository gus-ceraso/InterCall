package websocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	intercall "github.com/cerasos/intercall/go"
	coderws "github.com/coder/websocket"
)

// DefaultMessageLimit is the maximum WebSocket message size used by the
// high-level defaults: one maximum InterCall frame plus its 24-byte header.
const DefaultMessageLimit int64 = 64*1024*1024 + 24

type stream struct {
	conn    *coderws.Conn
	netConn net.Conn
	cancel  context.CancelFunc

	closeOnce sync.Once
	closeErr  error

	callbackMu    sync.Mutex
	callbackStop  func() bool
	callbackDone  chan struct{}
	callbackEnded bool
}

var _ intercall.ByteStream = (*stream)(nil)

func normalizeMessageLimit(limit int64) (int64, error) {
	if limit == 0 {
		return DefaultMessageLimit, nil
	}
	if limit == -1 {
		return -1, nil
	}
	if limit < 0 {
		return 0, fmt.Errorf("websocket: invalid message limit %d: %w", limit, intercall.ErrInvalidArgument)
	}
	return limit, nil
}

func newStream(parent context.Context, conn *coderws.Conn, limit int64) (*stream, error) {
	if parent == nil {
		return nil, fmt.Errorf("websocket: nil stream context: %w", intercall.ErrInvalidArgument)
	}
	limit, err := normalizeMessageLimit(limit)
	if err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	ioCtx, cancel := context.WithCancel(parent)
	netConn := coderws.NetConn(ioCtx, conn, coderws.MessageBinary)
	// NetConn deliberately resets the read limit to unlimited.
	conn.SetReadLimit(limit)
	s := &stream{
		conn:         conn,
		netConn:      netConn,
		cancel:       cancel,
		callbackDone: make(chan struct{}),
	}
	s.callbackStop = context.AfterFunc(parent, func() {
		s.closeOwned()
		close(s.callbackDone)
	})
	return s, nil
}

func (s *stream) Read(p []byte) (int, error)  { return s.netConn.Read(p) }
func (s *stream) Write(p []byte) (int, error) { return s.netConn.Write(p) }

func (s *stream) stopCallback() {
	s.callbackMu.Lock()
	if s.callbackEnded {
		s.callbackMu.Unlock()
		return
	}
	stop := s.callbackStop
	s.callbackStop = nil
	s.callbackEnded = true
	s.callbackMu.Unlock()
	if stop != nil && !stop() {
		<-s.callbackDone
	} else {
		close(s.callbackDone)
	}
}

func (s *stream) closeOwned() {
	s.closeOnce.Do(func() {
		s.cancel()
		var first error
		if err := s.conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
			first = err
		}
		if err := s.netConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) && first == nil {
			first = err
		}
		s.closeErr = first
	})
}

// Close immediately terminates the WebSocket and is safe to call repeatedly
// and concurrently. It does not perform a graceful WebSocket close
// handshake.
func (s *stream) Close() error {
	if s == nil {
		return fmt.Errorf("websocket: nil stream: %w", intercall.ErrInvalidArgument)
	}
	s.stopCallback()
	s.closeOwned()
	return s.closeErr
}

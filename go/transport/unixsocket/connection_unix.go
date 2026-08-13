//go:build unix

package unixsocket

import (
	"context"
	"fmt"
	"sync"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

const defaultPhaseTimeout = 10 * time.Second

type closeOnceStream struct {
	stream intercall.ByteStream
	once   sync.Once
	err    error
}

var _ intercall.ByteStream = (*closeOnceStream)(nil)

func (s *closeOnceStream) Read(p []byte) (int, error)  { return s.stream.Read(p) }
func (s *closeOnceStream) Write(p []byte) (int, error) { return s.stream.Write(p) }
func (s *closeOnceStream) Close() error {
	s.once.Do(func() { s.err = s.stream.Close() })
	return s.err
}

func validateConnectionArguments(ctx context.Context, export intercall.ExportBinding, imp intercall.ImportBinding) error {
	if ctx == nil {
		return fmt.Errorf("unixsocket: nil connection context: %w", intercall.ErrInvalidArgument)
	}
	if export == (intercall.ExportBinding{}) {
		return fmt.Errorf("unixsocket: zero export binding: %w", intercall.ErrInvalidArgument)
	}
	if imp == (intercall.ImportBinding{}) {
		return fmt.Errorf("unixsocket: zero import binding: %w", intercall.ErrInvalidArgument)
	}
	if _, ok := export.InterfaceID(); !ok {
		return fmt.Errorf("unixsocket: export binding has no interface ID: %w", intercall.ErrInvalidArgument)
	}
	if _, ok := imp.InterfaceID(); !ok {
		return fmt.Errorf("unixsocket: import binding has no interface ID: %w", intercall.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Dial establishes a Unix stream and performs interface negotiation before
// returning an InterCall connection. The caller's context owns the returned
// connection for its entire lifetime.
func Dial(ctx context.Context, path string, export intercall.ExportBinding, imp intercall.ImportBinding) (*intercall.Connection, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := validateConnectionArguments(ctx, export, imp); err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, defaultPhaseTimeout)
	defer cancel()
	stream, err := DialStream(dialCtx, path)
	if err != nil {
		return nil, err
	}
	owner := &closeOnceStream{stream: stream}
	conn, err := intercall.NewNegotiatedClientConnection(ctx, owner, export, imp)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	return conn, nil
}

// AcceptConnection accepts one Unix stream and performs server-role
// interface negotiation. Accept itself is not context-interruptible; close
// the listener to unblock a blocked accept. Once a socket is accepted, ctx
// owns its negotiation and resulting connection.
func (l *Listener) AcceptConnection(ctx context.Context, export intercall.ExportBinding, imp intercall.ImportBinding) (*intercall.Connection, error) {
	if err := validateConnectionArguments(ctx, export, imp); err != nil {
		return nil, err
	}
	stream, err := l.AcceptStream()
	if err != nil {
		return nil, err
	}
	owner := &closeOnceStream{stream: stream}
	if err := ctx.Err(); err != nil {
		_ = owner.Close()
		return nil, err
	}
	conn, err := intercall.NewNegotiatedServerConnection(ctx, owner, export, imp)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	return conn, nil
}

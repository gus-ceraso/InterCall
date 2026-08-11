package intercall

import (
	"context"
	"sync"
)

// Connection is the opaque runtime handle for one InterCall connection.
//
// A connection owns its byte stream and exactly one export and one import
// binding, neither of which can change after construction. It has two
// lifecycle conditions, active and terminal: terminal selection fixes one
// permanent cause, closes the stream exactly once, and cancels handler
// contexts. A nonnil *Connection is bound into contexts with
// WithConnection and retrieved with ConnectionFromContext; the nil
// *Connection zero value is invalid. Construction and the call and shutdown
// methods are part of the generated-code SPI.
type Connection struct {
	// Terminal selection state, guarded by mu.
	mu       sync.Mutex
	terminal chan struct{} // closed exactly once when the first cause is selected
	cause    error         // permanent terminal cause; non-nil once terminal is closed

	// writeMu is the connection-wide write gate: requests and responses
	// share it, exactly one frame write proceeds at a time, and frames never
	// interleave. It is acquired after mu whenever both are held, so the
	// lock order is mu then writeMu.
	writeMu sync.Mutex

	// Immutable ownership state, fixed at construction.
	stream ByteStream
	export ExportBinding
	imp    ImportBinding

	// Context state, fixed at construction.
	ctx            context.Context // construction context watched by the observer
	connCtx        context.Context // parent of handler contexts
	cancelHandlers context.CancelFunc

	// observerExit closes when the context observer goroutine exits.
	observerExit chan struct{}
}

// newConnection is the construction core behind the public connection
// constructor. It validates its arguments before taking ownership of the
// stream: a nil context or stream interface, a zero export or import
// binding, and an already available ctx.Err() are all rejected before the
// stream is claimed. On success it stores exactly one export and one import
// handle, derives the connection context, and starts the sole
// context-observer goroutine.
func newConnection(ctx context.Context, stream ByteStream, export ExportBinding, imp ImportBinding) (*Connection, error) {
	if ctx == nil {
		return nil, ErrInvalidArgument
	}
	if stream == nil {
		return nil, ErrInvalidArgument
	}
	if export == (ExportBinding{}) {
		return nil, ErrInvalidArgument
	}
	if imp == (ImportBinding{}) {
		return nil, ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	connCtx, cancel := context.WithCancel(ctx)
	c := &Connection{
		terminal:       make(chan struct{}),
		stream:         stream,
		export:         export,
		imp:            imp,
		ctx:            ctx,
		connCtx:        connCtx,
		cancelHandlers: cancel,
		observerExit:   make(chan struct{}),
	}
	go c.observeContext()
	return c, nil
}

// selectTerminal attempts to select err as the permanent terminal cause. All
// terminal events — explicit close, read or write failure, EOF or half-close,
// protocol error, and context cancellation — use this same lock-protected
// selection, and err must be non-nil. The first selector wins; it closes the
// terminal channel, closes the owned stream exactly once, and cancels
// handler contexts. A stream cleanup error never replaces or joins the
// cause. Losers return immediately without attempting teardown.
func (c *Connection) selectTerminal(err error) {
	c.mu.Lock()
	if c.cause != nil {
		c.mu.Unlock()
		return
	}
	c.cause = err
	close(c.terminal)
	c.mu.Unlock()

	// Teardown, exactly once, by the winner, outside the lock. The stream
	// cleanup error is suppressed.
	_ = c.stream.Close()
	c.cancelHandlers()
}

// observeContext is the context-observer goroutine started by construction.
// It waits for either the construction context's Done channel or the
// terminal-selection channel. Context cancellation attempts terminal
// selection with the exact ctx.Err() value; the runtime never uses
// context.Cause or wraps that value. A nil Done channel, as from
// context.Background, simply disables the context case. When another event
// wins selection, the observer rechecks terminal state under the same lock
// and exits without attempting a new cause; when context cancellation wins,
// the observer completes selection and teardown before exiting.
func (c *Connection) observeContext() {
	defer close(c.observerExit)

	done := c.ctx.Done()
	if done == nil {
		<-c.terminal
		return
	}
	select {
	case <-done:
		c.selectTerminal(c.ctx.Err())
	case <-c.terminal:
	}
}

// newHandlerContext derives one bound, per-handler context from the
// connection context. The connection cancels every derived context at
// terminal selection; the returned cancel func completes an individual
// handler. The handler context carries the connection through
// WithConnection.
func (c *Connection) newHandlerContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(c.connCtx)
	return WithConnection(ctx, c), cancel
}

// checkImport verifies that a caller's import handle is exactly the
// connection's stored import binding. A zero or different handle returns
// ErrBindingMismatch. Argument and binding validation happens before any
// terminal-state inspection.
func (c *Connection) checkImport(imp ImportBinding) error {
	if imp != c.imp {
		return ErrBindingMismatch
	}
	return nil
}

package intercall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// countingStream is a controllable ByteStream for lifecycle tests. IC-03
// never reads or writes the stream, so Read and Write are inert; Close is
// counted and may report a cleanup error.
type countingStream struct {
	mu       sync.Mutex
	closes   int
	closeErr error
}

var _ ByteStream = (*countingStream)(nil)

func (s *countingStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *countingStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *countingStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return s.closeErr
}

func (s *countingStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// newTestConnection constructs a connection with the given context, stream,
// and bindings, failing the test on error, and guarantees terminal selection
// at test end so the observer can never strand.
func newTestConnection(t *testing.T, ctx context.Context, s *countingStream, export ExportBinding, imp ImportBinding) *Connection {
	t.Helper()
	c, err := newConnection(ctx, s, export, imp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })
	return c
}

// newTestConn is newTestConnection with fresh bindings.
func newTestConn(t *testing.T, ctx context.Context, s *countingStream) *Connection {
	t.Helper()
	return newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())
}

// waitTerminal blocks until terminal selection completes, with a timeout.
func waitTerminal(t *testing.T, c *Connection) {
	t.Helper()
	select {
	case <-c.terminal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal selection")
	}
}

// waitObserver blocks until the context observer exits, with a timeout.
func waitObserver(t *testing.T, c *Connection) {
	t.Helper()
	select {
	case <-c.observerExit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for context-observer exit")
	}
}

// finish terminates c with cause and waits for selection and observer exit.
func finish(t *testing.T, c *Connection, cause error) {
	t.Helper()
	c.selectTerminal(cause)
	waitTerminal(t, c)
	waitObserver(t, c)
}

// TestLifecycleConstructorValidation pins that nil context, nil stream, and
// zero bindings are rejected with ErrInvalidArgument before the stream is
// owned: no connection is returned and the stream is never closed.
func TestLifecycleConstructorValidation(t *testing.T) {
	imp := NewImportBinding()
	export := newTestExport(t)
	s := &countingStream{}

	cases := []struct {
		name   string
		ctx    context.Context
		stream ByteStream
		export ExportBinding
		imp    ImportBinding
	}{
		{"nil context", nil, s, export, imp},
		{"nil stream", context.Background(), nil, export, imp},
		{"zero export binding", context.Background(), s, ExportBinding{}, imp},
		{"zero import binding", context.Background(), s, export, ImportBinding{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := newConnection(tc.ctx, tc.stream, tc.export, tc.imp)
			if err != ErrInvalidArgument {
				t.Errorf("err = %v, want ErrInvalidArgument", err)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Error("errors.Is(err, ErrInvalidArgument) = false")
			}
			if c != nil {
				t.Error("failed construction returned a connection")
			}
		})
	}
	if s.closeCount() != 0 {
		t.Errorf("failed constructions closed the stream %d times; validation must precede ownership", s.closeCount())
	}
}

// TestLifecycleConstructorReturnsAvailableContextError pins that an already
// available ctx.Err() is returned before ownership: the exact value, never
// context.Cause, and never ErrInvalidArgument.
func TestLifecycleConstructorReturnsAvailableContextError(t *testing.T) {
	s := &countingStream{}
	export := newTestExport(t)
	imp := NewImportBinding()

	// Canceled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := newConnection(ctx, s, export, imp)
	if err != context.Canceled {
		t.Errorf("canceled ctx: err = %v, want context.Canceled", err)
	}
	if c != nil {
		t.Error("canceled ctx: construction returned a connection")
	}

	// Canceled with a cause: the exact ctx.Err(), never context.Cause.
	cause := errors.New("custom cancel cause")
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	cancel2(cause)
	if context.Cause(ctx2) != cause {
		t.Fatal("premise: context.Cause must return the custom cause")
	}
	c2, err2 := newConnection(ctx2, s, export, imp)
	if err2 != context.Canceled {
		t.Errorf("cause-canceled ctx: err = %v, want context.Canceled", err2)
	}
	if err2 == cause {
		t.Error("construction returned context.Cause instead of ctx.Err()")
	}
	if c2 != nil {
		t.Error("cause-canceled ctx: construction returned a connection")
	}

	// Expired deadline: context.DeadlineExceeded, never its cause.
	deadlineCause := errors.New("custom deadline cause")
	ctx3, cancel3 := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Minute), deadlineCause)
	defer cancel3()
	c3, err3 := newConnection(ctx3, s, export, imp)
	if err3 != context.DeadlineExceeded {
		t.Errorf("expired deadline: err = %v, want context.DeadlineExceeded", err3)
	}
	if c3 != nil {
		t.Error("expired deadline: construction returned a connection")
	}

	if s.closeCount() != 0 {
		t.Errorf("stream closed %d times; rejected construction must not take ownership", s.closeCount())
	}
}

// TestLifecycleContextCancellationSelectsExactCause pins that context
// cancellation terminates the connection with exactly ctx.Err(), that the
// observer performs selection and teardown, and that a later selection
// attempt changes nothing.
func TestLifecycleContextCancellationSelectsExactCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &countingStream{}
	c := newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())

	cancel()
	waitTerminal(t, c)
	waitObserver(t, c)

	if c.cause != context.Canceled {
		t.Errorf("cause = %v, want context.Canceled", c.cause)
	}
	if c.cause != ctx.Err() {
		t.Errorf("cause = %v, want the exact ctx.Err() value %v", c.cause, ctx.Err())
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}

	c.selectTerminal(ErrClosed)
	if c.cause != context.Canceled {
		t.Errorf("cause changed to %v after a losing attempt", c.cause)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times after a losing attempt, want 1", s.closeCount())
	}
}

// TestLifecycleCancelCauseIgnored pins that a cause-bearing cancellation
// yields exactly context.Canceled at terminal selection, never
// context.Cause.
func TestLifecycleCancelCauseIgnored(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	c := newTestConn(t, ctx, &countingStream{})

	cause := errors.New("custom cause")
	cancel(cause)
	waitTerminal(t, c)
	waitObserver(t, c)

	if context.Cause(ctx) != cause {
		t.Fatal("premise: context.Cause must return the custom cause")
	}
	if c.cause != context.Canceled {
		t.Errorf("cause = %v, want exactly context.Canceled, never context.Cause", c.cause)
	}
	if c.cause == cause {
		t.Error("terminal cause adopted context.Cause")
	}
}

// TestLifecycleDeadlineSelectsExactCause pins that a cause-bearing deadline
// yields exactly context.DeadlineExceeded at terminal selection, never its
// cause.
func TestLifecycleDeadlineSelectsExactCause(t *testing.T) {
	cause := errors.New("custom deadline cause")
	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(20*time.Millisecond), cause)
	defer cancel()
	c := newTestConn(t, ctx, &countingStream{})

	waitTerminal(t, c)
	waitObserver(t, c)

	if context.Cause(ctx) != cause {
		t.Fatal("premise: context.Cause must return the custom cause")
	}
	if c.cause != context.DeadlineExceeded {
		t.Errorf("cause = %v, want exactly context.DeadlineExceeded", c.cause)
	}
	if c.cause != ctx.Err() {
		t.Errorf("cause = %v, want the exact ctx.Err() value %v", c.cause, ctx.Err())
	}
}

// TestLifecycleObserverExitsOnTerminalWithNilDone pins that a nil Done
// channel, as from context.Background, disables the context case while the
// observer still exits when terminal selection happens.
func TestLifecycleObserverExitsOnTerminalWithNilDone(t *testing.T) {
	ctx := context.Background()
	if ctx.Done() != nil {
		t.Fatal("premise: context.Background has a nil Done channel")
	}
	s := &countingStream{}
	c := newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())

	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	waitObserver(t, c)

	if c.cause != ErrClosed {
		t.Errorf("cause = %v, want ErrClosed", c.cause)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestLifecycleCloseWithoutCancellation pins that terminal selection by a
// non-context event wakes the observer through the terminal channel without
// the construction context being canceled.
func TestLifecycleCloseWithoutCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &countingStream{}
	c := newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())

	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	waitObserver(t, c)

	if c.cause != ErrClosed {
		t.Errorf("cause = %v, want ErrClosed", c.cause)
	}
	if ctx.Err() != nil {
		t.Error("construction context was unexpectedly canceled")
	}
}

// TestLifecycleCancelAfterTerminalDoesNotReplaceCause pins that a
// cancellation arriving after terminal selection wakes the observer, which
// exits without replacing the permanent cause.
func TestLifecycleCancelAfterTerminalDoesNotReplaceCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &countingStream{}
	c := newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())

	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	cancel()
	waitObserver(t, c)

	if c.cause != ErrClosed {
		t.Errorf("cause = %v, want ErrClosed to remain permanent", c.cause)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestLifecycleCloseCancellationRace races explicit close against context
// cancellation under the race detector and pins first-cause selection: the
// permanent cause is exactly one of the two candidates and the stream is
// closed exactly once.
func TestLifecycleCloseCancellationRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		s := &countingStream{}
		c := newTestConnection(t, ctx, s, newTestExport(t), NewImportBinding())

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cancel()
		}()
		go func() {
			defer wg.Done()
			c.selectTerminal(ErrClosed)
		}()
		wg.Wait()

		waitTerminal(t, c)
		waitObserver(t, c)

		if c.cause != context.Canceled && c.cause != ErrClosed {
			t.Fatalf("iteration %d: cause = %v, want exactly context.Canceled or ErrClosed", i, c.cause)
		}
		if s.closeCount() != 1 {
			t.Fatalf("iteration %d: stream closed %d times, want exactly 1", i, s.closeCount())
		}
	}
}

// TestLifecycleStreamClosedExactlyOnce races many selectors and pins that
// exactly one wins: the stream closes once and a cause is selected.
func TestLifecycleStreamClosedExactlyOnce(t *testing.T) {
	const racers = 16
	s := &countingStream{}
	c := newTestConn(t, context.Background(), s)

	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.selectTerminal(sentinel(fmt.Sprintf("racer %d", i)))
		}(i)
	}
	wg.Wait()
	waitTerminal(t, c)
	waitObserver(t, c)

	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
	if c.cause == nil {
		t.Error("no terminal cause selected")
	}
}

// TestLifecycleCleanupErrorSuppressed pins that a stream cleanup error never
// replaces or joins the selected terminal cause.
func TestLifecycleCleanupErrorSuppressed(t *testing.T) {
	cleanupErr := errors.New("cleanup failure")
	s := &countingStream{closeErr: cleanupErr}
	c := newTestConn(t, context.Background(), s)

	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	waitObserver(t, c)

	if c.cause != ErrClosed {
		t.Errorf("cause = %v, want ErrClosed", c.cause)
	}
	if c.cause == cleanupErr {
		t.Error("cleanup error replaced the terminal cause")
	}
	if errors.Is(c.cause, cleanupErr) {
		t.Error("cleanup error joined the terminal cause")
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestLifecycleTerminalCauseIsPermanent pins that once a cause is selected,
// every later selection attempt is a no-op.
func TestLifecycleTerminalCauseIsPermanent(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})
	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	waitObserver(t, c)

	for i := 0; i < 16; i++ {
		c.selectTerminal(sentinel("later"))
	}
	if c.cause != ErrClosed {
		t.Errorf("cause = %v, want ErrClosed to stay permanent", c.cause)
	}
}

// TestLifecycleHandlerContextsCanceledOnTerminal pins the handler-context
// foundation: a handler context derives from the connection context, carries
// the connection binding, and is canceled when the connection terminates.
func TestLifecycleHandlerContextsCanceledOnTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestConn(t, ctx, &countingStream{})

	hctx, hcancel := c.newHandlerContext()
	defer hcancel()
	if got, err := ConnectionFromContext(hctx); err != nil || got != c {
		t.Fatalf("handler context lookup = (%p, %v), want (%p, nil)", got, err, c)
	}
	if hctx.Err() != nil {
		t.Fatal("handler context canceled before terminal selection")
	}

	cancel()
	waitTerminal(t, c)
	waitObserver(t, c)

	select {
	case <-hctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("handler context not canceled at terminal selection")
	}
	if hctx.Err() != context.Canceled {
		t.Errorf("handler context err = %v, want context.Canceled", hctx.Err())
	}
}

// TestLifecycleCloseCancelsHandlerContexts pins that terminal selection by
// explicit close also cancels handler contexts.
func TestLifecycleCloseCancelsHandlerContexts(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})

	hctx, hcancel := c.newHandlerContext()
	defer hcancel()

	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	waitObserver(t, c)

	select {
	case <-hctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("handler context not canceled by terminal selection")
	}
	if hctx.Err() != context.Canceled {
		t.Errorf("handler context err = %v, want context.Canceled", hctx.Err())
	}
}

// TestLifecycleImportBindingIdentity pins the binding identity checks: the
// connection stores exactly the constructed handles, copies retain identity,
// and a zero or different import handle is ErrBindingMismatch before any
// terminal-state inspection.
func TestLifecycleImportBindingIdentity(t *testing.T) {
	imp := NewImportBinding()
	other := NewImportBinding()
	export := newTestExport(t)
	s := &countingStream{}
	c := newTestConnection(t, context.Background(), s, export, imp)

	if c.imp != imp {
		t.Error("stored import handle differs from the constructed handle")
	}
	if c.export != export {
		t.Error("stored export handle differs from the constructed handle")
	}

	if err := c.checkImport(imp); err != nil {
		t.Errorf("checkImport(exact) = %v, want nil", err)
	}
	cp := imp
	if err := c.checkImport(cp); err != nil {
		t.Errorf("checkImport(copy) = %v, want nil", err)
	}
	if err := c.checkImport(ImportBinding{}); err != ErrBindingMismatch {
		t.Errorf("checkImport(zero) = %v, want ErrBindingMismatch", err)
	}
	if err := c.checkImport(other); err != ErrBindingMismatch {
		t.Errorf("checkImport(different) = %v, want ErrBindingMismatch", err)
	}
	if !errors.Is(c.checkImport(other), ErrBindingMismatch) {
		t.Error("errors.Is(checkImport(different), ErrBindingMismatch) = false")
	}

	// Termination changes neither stored handle.
	finish(t, c, ErrClosed)
	if c.imp != imp || c.export != export {
		t.Error("stored handles changed after terminal selection")
	}
}

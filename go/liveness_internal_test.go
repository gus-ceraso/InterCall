package intercall

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stalledStream is a controllable ByteStream for the connection-close
// liveness tests. Write signals the first entry on entered, then blocks
// until the test releases it (releaseWrites) or the stream closes
// (closedCh, only when closeUnblocks is set); a Write that proceeds after
// the stream closed reports io.ErrClosedPipe. Read parks until the stream
// closes or the test releases it, so the receive loop never races terminal
// selection with an early EOF. Close counts the call; with blockClose set
// it signals closeEntered and parks on releaseClose before completing, so
// the test can hold terminal teardown mid-flight; a completed Close marks
// the stream closed and, when closeUnblocks is set, releases stalled
// writes. writeReturned closes when the first Write returns, proving that a
// stalled write was released.
type stalledStream struct {
	mu sync.Mutex

	entered       chan struct{} // closed once when the first Write is entered
	release       chan struct{} // closed to release stalled Writes
	closeEntered  chan struct{} // closed once when Close is entered
	closeRelease  chan struct{} // closed to release a blocked Close
	readRelease   chan struct{} // closed to release a parked Read
	closedCh      chan struct{} // closed when the stream is actually closed
	writeReturned chan struct{} // closed once when the first Write returns

	blockClose    bool // Close parks on closeRelease before completing
	closeUnblocks bool // a completed Close releases stalled Writes

	closed   bool
	closes   int
	recorded []byte

	enteredOnce       sync.Once
	closeEnteredOnce  sync.Once
	writeReturnedOnce sync.Once
	releaseOnce       sync.Once
	closeReleaseOnce  sync.Once
	readReleaseOnce   sync.Once
	closedOnce        sync.Once
}

var _ ByteStream = (*stalledStream)(nil)

func newStalledStream(blockClose, closeUnblocks bool) *stalledStream {
	return &stalledStream{
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
		closeEntered:  make(chan struct{}),
		closeRelease:  make(chan struct{}),
		readRelease:   make(chan struct{}),
		closedCh:      make(chan struct{}),
		writeReturned: make(chan struct{}),
		blockClose:    blockClose,
		closeUnblocks: closeUnblocks,
	}
}

func (s *stalledStream) Read(p []byte) (int, error) {
	select {
	case <-s.readRelease:
	case <-s.closedCh:
	}
	return 0, io.EOF
}

func (s *stalledStream) Write(p []byte) (int, error) {
	s.enteredOnce.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-s.closedCh:
	}
	s.writeReturnedOnce.Do(func() { close(s.writeReturned) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.recorded = append(s.recorded, p...)
	return len(p), nil
}

func (s *stalledStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	s.closeEnteredOnce.Do(func() { close(s.closeEntered) })
	if s.blockClose {
		<-s.closeRelease
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.closeUnblocks {
		s.closedOnce.Do(func() { close(s.closedCh) })
	}
	return nil
}

// releaseWrites releases every stalled Write; repeated calls are no-ops.
func (s *stalledStream) releaseWrites() { s.releaseOnce.Do(func() { close(s.release) }) }

// releaseClose releases a blocked Close; repeated calls are no-ops.
func (s *stalledStream) releaseClose() { s.closeReleaseOnce.Do(func() { close(s.closeRelease) }) }

// releaseRead releases a parked Read; repeated calls are no-ops.
func (s *stalledStream) releaseRead() { s.readReleaseOnce.Do(func() { close(s.readRelease) }) }

func (s *stalledStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

func (s *stalledStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// TestConnectionCloseStalledWriter reproduces, with deterministic barriers,
// writer A blocked in Write, writer B waiting for write admission, and a
// concurrent Close: terminal is published and Close returns, the stream
// close — the only releaser, the test never releases the write — unblocks
// A's stalled write, B abandons its wait, and every operation settles with
// exactly ErrClosed.
func TestConnectionCloseStalledWriter(t *testing.T) {
	s := newStalledStream(false, true) // Close completes and releases stalled writes
	t.Cleanup(func() {
		s.releaseWrites()
		s.releaseClose()
		s.releaseRead()
	})
	c, err := NewConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	// A holds the gate with a stalled write.
	aDone := make(chan error, 1)
	go func() {
		aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
	}()
	<-s.entered

	// B waits for admission behind A: A holds the gate, so B cannot be
	// admitted before Close.
	bEncoded := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		bDone <- c.Call(context.Background(), c.imp, 2, func() ([]byte, error) {
			close(bEncoded)
			return []byte("b"), nil
		}, okDecoder)
	}()
	<-bEncoded

	// Concurrent Close: publication is synchronous, so the permanent cause
	// is fixed when Close returns; Close never waits for the stalled
	// write, the blocked gate, or the cleanup owner.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if cause != ErrClosed {
		t.Fatalf("cause = %v, want exactly ErrClosed", cause)
	}

	// A's stalled write is released only by the stream close: the test
	// never releases it, so the returned write proves the transport
	// unblock contract. A and B settle with the exact cause; B was never
	// admitted and consumed no ID.
	<-s.writeReturned
	if err := <-aDone; err != ErrClosed {
		t.Fatalf("A's call = %v, want exactly ErrClosed", err)
	}
	if err := <-bDone; err != ErrClosed {
		t.Fatalf("B's call = %v, want exactly ErrClosed", err)
	}
	c.mu.Lock()
	next := c.nextID
	c.mu.Unlock()
	if next != 1 {
		t.Errorf("nextID = %d, want 1: B must not be admitted after terminal", next)
	}
	if pendingLen(c) != 0 {
		t.Error("teardown left a pending entry")
	}
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestConnectionCloseBlockedGateWaiter pins that Close never waits for a
// blocked gate or a blocked Write: while writer A is stalled in Write
// holding the gate and B waits for admission, Close returns with A's write
// still stalled, B abandons with the exact cause, and A settles only after
// the test releases the write. This stream deliberately does not release
// stalled writes on Close — the transport unblock contract is proven by
// TestConnectionCloseStalledWriter — so the gate provably remains held when
// Close returns.
func TestConnectionCloseBlockedGateWaiter(t *testing.T) {
	s := newStalledStream(false, false) // Close never releases the stalled write
	t.Cleanup(func() {
		s.releaseWrites()
		s.releaseClose()
		s.releaseRead()
	})
	c, err := NewConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	aDone := make(chan error, 1)
	go func() {
		aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
	}()
	<-s.entered

	bEncoded := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		bDone <- c.Call(context.Background(), c.imp, 2, func() ([]byte, error) {
			close(bEncoded)
			return []byte("b"), nil
		}, okDecoder)
	}()
	<-bEncoded

	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	// A's write is still stalled: nothing but the test's release can make
	// it return, so Close provably did not wait for the gate or the write.
	select {
	case <-s.writeReturned:
		t.Fatal("Close waited for the stalled write")
	default:
	}

	// B abandons its gate wait with the exact cause, without an ID.
	if err := <-bDone; err != ErrClosed {
		t.Fatalf("B's call = %v, want exactly ErrClosed", err)
	}
	c.mu.Lock()
	next := c.nextID
	c.mu.Unlock()
	if next != 1 {
		t.Errorf("nextID = %d, want 1: B must not be admitted after terminal", next)
	}

	// The test releases A's stalled write; A settles with the exact cause.
	s.releaseWrites()
	if err := <-aDone; err != ErrClosed {
		t.Fatalf("A's call = %v, want exactly ErrClosed", err)
	}
	if pendingLen(c) != 0 {
		t.Error("teardown left a pending entry")
	}
	// This stream never unblocks reads on Close (the transport unblock
	// contract is proven by TestConnectionCloseStalledWriter), so release
	// the parked receive-loop read before Wait.
	s.releaseRead()
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestConnectionCloseBlockedStreamClose pins the blocked-close contract:
// while the stream's Close is deliberately blocked, the Close method
// remains prompt, Wait waits for the complete cleanup, and a per-call
// cancellation and a response that race an already pending call can neither
// claim it after terminal publication — the call receives the permanent
// cause.
func TestConnectionCloseBlockedStreamClose(t *testing.T) {
	s := newStalledStream(true, true) // Close parks until the test releases it
	t.Cleanup(func() {
		s.releaseWrites()
		s.releaseClose()
		s.releaseRead()
	})
	c, err := NewConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	// P is admitted and its write completes before Close.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pDone := make(chan error, 1)
	go func() {
		pDone <- c.Call(ctx, c.imp, 1, func() ([]byte, error) { return []byte("p"), nil }, okDecoder)
	}()
	<-s.entered
	s.releaseWrites() // P's write completes; the stream is not closed yet
	waitPending(t, c, 1)

	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	// The cleanup owner is now provably stuck inside the stream's blocked
	// Close: publication and every transferred entry's completion happened
	// before the close, but teardown is not finished.
	select {
	case <-s.closeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never entered the stream Close")
	}
	select {
	case <-c.teardown:
		t.Fatal("teardown completed while the stream Close was blocked")
	default:
	}

	// Wait waits for the complete cleanup and therefore stays blocked.
	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Wait() }()
	select {
	case err := <-waitDone:
		t.Fatalf("Wait returned %v while the stream Close was blocked", err)
	default:
	}

	// Cancellation and a response race the already pending call: after
	// terminal publication neither can claim it, and it receives the
	// permanent cause.
	responseWon := make(chan bool, 1)
	start := make(chan struct{})
	go func() {
		<-start
		responseWon <- c.claimResponse(0, 0, []byte("late response"))
	}()
	go func() {
		<-start
		cancel()
	}()
	close(start)
	if err := <-pDone; err != ErrClosed {
		t.Fatalf("P's call = %v, want exactly ErrClosed, never per-call cancellation", err)
	}
	if claimed := <-responseWon; claimed {
		t.Fatal("a response claimed the call after terminal publication")
	}
	if pendingLen(c) != 0 {
		t.Error("teardown left a pending entry")
	}

	// Releasing the blocked stream Close completes cleanup: the stream
	// closes, teardown finishes, and Wait returns the exact cause.
	s.releaseClose()
	if err := <-waitDone; err != ErrClosed {
		t.Fatalf("Wait() = %v, want exactly ErrClosed", err)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestTerminalPublicationClaimsPendingBeforeCleanup pins that terminal
// publication, not cleanup, claims every existing pending entry: the
// pending map is empty the moment Close returns, while the cleanup owner is
// still provably stuck in the blocked stream Close. Neither a later
// response nor per-call cancellation can claim a transferred entry, and
// every transferred call receives the permanent cause.
func TestTerminalPublicationClaimsPendingBeforeCleanup(t *testing.T) {
	s := newStalledStream(true, true) // hold cleanup mid-flight
	t.Cleanup(func() {
		s.releaseWrites()
		s.releaseClose()
		s.releaseRead()
	})
	c, err := NewConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	dones := make([]chan error, 2)
	for i, ctx := range []context.Context{ctx1, ctx2} {
		dones[i] = make(chan error, 1)
		go func(i int, ctx context.Context) {
			dones[i] <- c.Call(ctx, c.imp, uint64(i+1), func() ([]byte, error) { return []byte("p"), nil }, okDecoder)
		}(i, ctx)
	}
	<-s.entered
	s.releaseWrites() // both writes complete once released
	waitPending(t, c, 2)

	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	// Publication claimed every entry under the state lock before Close
	// returned: the pending map is empty even though the cleanup owner has
	// not finished (it is stuck in the blocked stream Close).
	if got := pendingLen(c); got != 0 {
		t.Fatalf("pending map holds %d entries after terminal publication, want 0", got)
	}
	select {
	case <-s.closeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never entered the stream Close")
	}
	select {
	case <-c.teardown:
		t.Fatal("teardown completed while the stream Close was blocked")
	default:
	}

	// Neither a response for either transferred ID nor per-call
	// cancellation can claim an entry after publication; every call
	// receives the permanent cause.
	for id := uint64(0); id < 2; id++ {
		if claimed := c.claimResponse(id, 0, []byte("late")); claimed {
			t.Fatalf("response for transferred ID %d claimed an entry", id)
		}
	}
	cancel1()
	cancel2()
	for i, done := range dones {
		if err := <-done; err != ErrClosed {
			t.Fatalf("call %d = %v, want exactly ErrClosed", i, err)
		}
	}

	// Releasing the blocked stream Close completes cleanup; Wait then
	// returns the exact cause.
	s.releaseClose()
	if got := c.Wait(); got != ErrClosed {
		t.Fatalf("Wait() = %v, want exactly ErrClosed", got)
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestCallGateWaitCancellation pins that a call canceled while waiting for
// the write gate gets its exact context error without an ID or frame: the
// gate wait observes the call context's Done, and the post-acquire
// admission recheck makes the same outcome hold when the gate opens as the
// cancellation fires.
func TestCallGateWaitCancellation(t *testing.T) {
	newGateConn := func() (*Connection, *blockingStream) {
		s := &blockingStream{
			entered: make(chan struct{}),
			release: make(chan struct{}),
			call:    func(p []byte) (int, error) { return len(p), nil },
		}
		c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.selectTerminal(ErrClosed) })
		return c, s
	}
	// A holds the gate with a stalled write; B waits for admission behind
	// A. startB blocks until B's encoder signals, so B is then at or
	// inside the gate wait.
	startB := func(c *Connection, ctx context.Context) chan error {
		bEncoded := make(chan struct{})
		bDone := make(chan error, 1)
		go func() {
			bDone <- c.Call(ctx, c.imp, 2, func() ([]byte, error) {
				close(bEncoded)
				return []byte("b"), nil
			}, okDecoder)
		}()
		<-bEncoded
		return bDone
	}
	// A's write is released only after B's cancellation, so B's wait must
	// observe the context: the exact error, no ID, no frame.
	finish := func(t *testing.T, c *Connection, s *blockingStream, aDone, bDone chan error, want error) {
		t.Helper()
		if err := <-bDone; err != want {
			t.Fatalf("waiting call returned %v, want exactly %v", err, want)
		}
		if !c.claimResponse(0, 0, nil) {
			t.Fatal("claimResponse(0) did not match A's pending call")
		}
		if err := <-aDone; err != nil {
			t.Fatalf("A's call failed: %v", err)
		}
		frames := splitFrames(t, s.bytes())
		if len(frames) != 1 {
			t.Fatalf("recorded %d frames, want only A's frame", len(frames))
		}
		c.mu.Lock()
		next := c.nextID
		c.mu.Unlock()
		if next != 1 {
			t.Errorf("nextID = %d, want 1: no ID may be allocated while waiting for the write gate", next)
		}
	}

	t.Run("canceled while gate held", func(t *testing.T) {
		c, s := newGateConn()
		aDone := make(chan error, 1)
		go func() {
			aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
		}()
		<-s.entered

		bCtx, bCancel := context.WithCancel(context.Background())
		bDone := startB(c, bCtx)
		bCancel()
		close(s.release)
		finish(t, c, s, aDone, bDone, context.Canceled)
	})

	t.Run("deadline while gate held", func(t *testing.T) {
		c, s := newGateConn()
		aDone := make(chan error, 1)
		go func() {
			aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
		}()
		<-s.entered

		bCtx, bCancel := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
		defer bCancel()
		bDone := startB(c, bCtx)
		<-bCtx.Done()
		close(s.release)
		finish(t, c, s, aDone, bDone, context.DeadlineExceeded)
	})

	t.Run("gate opens as cancellation fires", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			c, s := newGateConn()
			aDone := make(chan error, 1)
			go func() {
				aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
			}()
			<-s.entered

			bCtx, bCancel := context.WithCancel(context.Background())
			bDone := startB(c, bCtx)
			// The context is already canceled when the gate opens: the
			// wait's select and the post-acquire admission recheck must
			// both yield the exact context error, without an ID or frame.
			bCancel()
			close(s.release)
			finish(t, c, s, aDone, bDone, context.Canceled)
		}
	})
}

// TestHandlerGateWaitTerminal pins that a handler waiting at the write gate
// abandons its response after terminal selection: with writer A stalled in
// Write holding the gate, the handler dispatches, waits at the gate,
// abandons on terminal, and never enters the stream write.
func TestHandlerGateWaitTerminal(t *testing.T) {
	stream := newPipeStream()
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var writeOnce sync.Once
	var writeCalls atomic.Int32
	stream.write = func(p []byte) (int, error) {
		writeCalls.Add(1)
		writeOnce.Do(func() { close(writeEntered) })
		<-writeRelease
		return len(p), nil
	}
	dispatched := make(chan struct{})
	c := newReceiveTestConn(t, stream, func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		close(dispatched)
		return 0, []byte("response")
	})

	// A holds the gate with a stalled request write.
	aDone := make(chan error, 1)
	go func() {
		aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder)
	}()
	<-writeEntered

	// The handler dispatches, then waits at the gate behind A.
	handlerDone := make(chan struct{})
	go func() {
		c.handleRequest(7, 0x42, []byte("ping"))
		close(handlerDone)
	}()
	<-dispatched

	// Terminal selection: the handler waiting at the gate abandons its
	// response and never writes.
	c.selectTerminal(ErrClosed)
	waitTerminal(t, c)
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not abandon its gate wait after terminal selection")
	}
	if got := writeCalls.Load(); got != 1 {
		t.Errorf("write hook entered %d times, want exactly once (A's write); the handler must never write after terminal", got)
	}
	if got := len(stream.bytes()); got != 0 {
		t.Errorf("handler wrote %d bytes after terminal selection", got)
	}

	// A's stalled write completes when the test releases it; A settles with
	// the exact terminal cause, and A's request frame is the only frame.
	close(writeRelease)
	if err := <-aDone; err != ErrClosed {
		t.Fatalf("A's call = %v, want exactly ErrClosed", err)
	}
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want exactly A's request frame", len(frames))
	}
	hdr, err := parseFrameHeader(frames[0][:frameHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if hdr.kind != requestFrame || hdr.requestID != 0 {
		t.Errorf("recorded frame = %+v, want A's request frame with ID 0", hdr)
	}

	stream.closeWriter()
	waitReceiveExit(t, c)
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

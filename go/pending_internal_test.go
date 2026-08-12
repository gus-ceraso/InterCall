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

// recordingDecoder records the exception key and payload it was invoked
// with, and returns a per-test error.
type recordingDecoder struct {
	mu      sync.Mutex
	calls   int
	key     uint64
	payload []byte
	err     error
}

func (d *recordingDecoder) decode(key uint64, payload []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.key = key
	d.payload = append([]byte(nil), payload...)
	return d.err
}

func (d *recordingDecoder) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// pendingLen reports the number of pending entries under the connection
// lock.
func pendingLen(c *Connection) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// waitPending blocks until the pending map holds exactly want entries.
func waitPending(t *testing.T, c *Connection, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := pendingLen(c); got == want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d pending entries, have %d", want, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPendingResponseClaim pins the response claim hook: a matched response
// claims the entry, runs the decoder with the exact key and owned payload,
// and completes the call with nil, with the decoder's closure writes visible
// before Call returns.
func TestPendingResponseClaim(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)

	var gotKey uint64
	var gotPayload []byte
	dec := func(key uint64, payload []byte) error {
		gotKey = key
		gotPayload = append([]byte(nil), payload...)
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 3, func() ([]byte, error) { return []byte("req"), nil }, dec)
	}()
	waitPending(t, c, 1)

	if claimed := c.claimResponse(0, 0x5a5a, []byte("response bytes")); !claimed {
		t.Fatal("claimResponse(0) did not match the pending call")
	}
	if err := <-done; err != nil {
		t.Fatalf("call returned %v, want nil", err)
	}
	if gotKey != 0x5a5a {
		t.Errorf("decoder saw key %#x, want %#x", gotKey, 0x5a5a)
	}
	if string(gotPayload) != "response bytes" {
		t.Errorf("decoder saw payload %q, want %q", gotPayload, "response bytes")
	}
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending")
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("successful response terminated the connection")
	}
}

// TestPendingResponseOutOfOrder pins that responses may claim pending calls
// in any order: each entry is claimed by its own ID.
func TestPendingResponseOutOfOrder(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)

	dones := make([]chan error, 2)
	for i := range dones {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(i+1),
				func() ([]byte, error) { return []byte{byte('a' + i)}, nil }, okDecoder)
		}(i)
	}
	waitPending(t, c, 2)

	if claimed := c.claimResponse(1, 0, []byte("second")); !claimed {
		t.Fatal("claimResponse(1) did not match")
	}
	if claimed := c.claimResponse(0, 0, []byte("first")); !claimed {
		t.Fatal("claimResponse(0) did not match")
	}
	for i := range dones {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d returned %v, want nil", i, err)
		}
	}
	if pendingLen(c) != 0 {
		t.Error("pending entries remain after all responses")
	}
}

// TestPendingResponseUnmatched pins the opaque unmatched-response rule: a
// response for an unknown ID — including an ID retired by completion or
// cancellation — is not claimed, its decoder never runs, and no state
// changes.
func TestPendingResponseUnmatched(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	dec := &recordingDecoder{}

	if claimed := c.claimResponse(99, 0, []byte("unknown")); claimed {
		t.Fatal("claimResponse(unknown ID) matched")
	}
	if dec.callCount() != 0 {
		t.Fatalf("decoder invoked %d times for an unmatched response", dec.callCount())
	}
	if pendingLen(c) != 0 {
		t.Fatal("unmatched response changed the pending map")
	}

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return nil, nil }, dec.decode)
	}()
	waitPending(t, c, 1)
	if !c.claimResponse(0, 0, nil) {
		t.Fatal("claimResponse(0) did not match")
	}
	if err := <-done; err != nil {
		t.Fatalf("call returned %v, want nil", err)
	}
	// A later duplicate response for the completed ID is unmatched.
	if claimed := c.claimResponse(0, 0, nil); claimed {
		t.Fatal("duplicate response for a completed ID matched")
	}
	if dec.callCount() != 1 {
		t.Errorf("decoder invoked %d times, want once", dec.callCount())
	}
}

// TestPendingResponseDecoderErrorTerminal pins that a decoder error on a
// matched response terminates the connection with a cause wrapping
// ErrProtocol and completes the call with the permanent terminal cause.
func TestPendingResponseDecoderErrorTerminal(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	decoderErr := errors.New("invalid value")
	dec := &recordingDecoder{err: decoderErr}

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, dec.decode)
	}()
	waitPending(t, c, 1)

	if claimed := c.claimResponse(0, 0, []byte("bad")); !claimed {
		t.Fatal("claimResponse(0) did not match")
	}
	err := <-done
	if err == nil {
		t.Fatal("call returned nil after a decoder error")
	}
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("err = %v, want a matched-response error wrapping ErrProtocol", err)
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if err != cause {
		t.Errorf("call outcome %v differs from the permanent terminal cause %v", err, cause)
	}
	if !errors.Is(cause, ErrProtocol) {
		t.Errorf("terminal cause %v does not wrap ErrProtocol", cause)
	}
	waitTerminal(t, c)
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending after decoder error")
	}
}

// TestPendingResponseDecoderPanicTerminal pins that a decoder panic on a
// matched response terminates the connection and completes the call with the
// permanent terminal cause.
func TestPendingResponseDecoderPanicTerminal(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	panicDecoder := func(uint64, []byte) error {
		panic("decoder blew up")
	}

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, panicDecoder)
	}()
	waitPending(t, c, 1)

	if claimed := c.claimResponse(0, 0, nil); !claimed {
		t.Fatal("claimResponse(0) did not match")
	}
	err := <-done
	if err == nil {
		t.Fatal("call returned nil after a decoder panic")
	}
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("err = %v, want a matched-response error wrapping ErrProtocol", err)
	}
	waitTerminal(t, c)
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending after decoder panic")
	}
}

// TestPendingCancelRetiresID pins per-call cancellation: cancelling the call
// context while the call waits claims the entry, returns the exact context
// error, retires the ID permanently, and leaves the connection active; a
// later response for the retired ID is unmatched and opaque.
func TestPendingCancelRetiresID(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- c.Call(ctx, c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder) }()
		waitPending(t, c, 1)

		cancel()
		if err := <-done; err != context.Canceled {
			t.Fatalf("call returned %v, want exactly context.Canceled", err)
		}
		if pendingLen(c) != 0 {
			t.Error("canceled entry still pending")
		}
		c.mu.Lock()
		next, active := c.nextID, c.cause == nil
		c.mu.Unlock()
		if next != 1 {
			t.Errorf("nextID = %d, want 1: the retired ID must not be reused", next)
		}
		if !active {
			t.Error("per-call cancellation terminated the connection")
		}
		if claimed := c.claimResponse(0, 0, nil); claimed {
			t.Fatal("response for the retired ID matched")
		}
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- c.Call(ctx, c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder) }()
		waitPending(t, c, 1)

		if err := <-done; err != context.DeadlineExceeded {
			t.Fatalf("call returned %v, want exactly context.DeadlineExceeded", err)
		}
		if pendingLen(c) != 0 {
			t.Error("expired entry still pending")
		}
		c.mu.Lock()
		active := c.cause == nil
		c.mu.Unlock()
		if !active {
			t.Error("per-call deadline terminated the connection")
		}
	})
}

// TestPendingCancelDuringWriteDefersToOutcome pins that after admission the
// per-call context cannot interrupt the write: a cancellation that fires
// while the frame write is blocked completes the write first, and the call
// then returns the exact context error after claiming the entry.
func TestPendingCancelDuringWriteDefersToOutcome(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder) }()
	<-s.entered

	// The entry is admitted and the write is in progress; cancellation must
	// not interrupt it.
	waitPending(t, c, 1)
	cancel()
	select {
	case err := <-done:
		t.Fatalf("call returned %v while the write was still blocked", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(s.release)
	if err := <-done; err != context.Canceled {
		t.Fatalf("call returned %v, want exactly context.Canceled", err)
	}
	// The full frame was still written: the context cannot interrupt the
	// write after admission.
	frames := splitFrames(t, s.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want the complete frame", len(frames))
	}
	if pendingLen(c) != 0 {
		t.Error("canceled entry still pending")
	}
	if claimed := c.claimResponse(0, 0, nil); claimed {
		t.Fatal("response for the canceled ID matched")
	}
}

// TestPendingFullDuplexResponseDuringWrite pins the full-duplex rule: a
// response that claims the entry while the frame write is still in progress
// remains this call's outcome even though the write subsequently fails and
// terminates the connection.
func TestPendingFullDuplexResponseDuringWrite(t *testing.T) {
	writeErr := io.ErrClosedPipe
	s := &blockingStream{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		call:    func(p []byte) (int, error) { return 0, writeErr },
	}
	c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder)
	}()
	<-s.entered

	// The response claims the entry during the blocked write.
	if claimed := c.claimResponse(0, 0, []byte("early")); !claimed {
		t.Fatal("claimResponse(0) during the write did not match")
	}
	close(s.release)

	if err := <-done; err != nil {
		t.Fatalf("call returned %v, want nil: the response must remain the outcome", err)
	}
	waitTerminal(t, c)
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if !errors.Is(cause, writeErr) {
		t.Errorf("terminal cause %v does not wrap the write error", cause)
	}
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending")
	}
}

// TestPendingFullDuplexDecoderErrorDuringWrite pins that a decoder failure
// on a response claimed during the write terminates the connection and
// becomes the call's outcome, even though the write itself also fails.
func TestPendingFullDuplexDecoderErrorDuringWrite(t *testing.T) {
	s := &blockingStream{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		call:    func(p []byte) (int, error) { return 0, io.ErrClosedPipe },
	}
	c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	dec := &recordingDecoder{err: errors.New("bad response")}
	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, dec.decode)
	}()
	<-s.entered

	if claimed := c.claimResponse(0, 0, []byte("bad")); !claimed {
		t.Fatal("claimResponse(0) during the write did not match")
	}
	close(s.release)

	err = <-done
	if err == nil {
		t.Fatal("call returned nil after the decoder failed")
	}
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("err = %v, want a matched-response error wrapping ErrProtocol", err)
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if err != cause {
		t.Errorf("call outcome %v differs from the permanent terminal cause %v", err, cause)
	}
	waitTerminal(t, c)
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending")
	}
}

// TestPendingAdmissionPointTeardown pins the admission-point rule: once the
// entry is inserted under the connection lock, terminal teardown may claim
// it before the subsequent stream write enters; the write then succeeds or
// fails, and the terminal cause is the call's outcome either way.
func TestPendingAdmissionPointTeardown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		writeErr error
	}{
		{"write fails after teardown", io.ErrClosedPipe},
		{"write succeeds after teardown", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &blockingStream{
				entered: make(chan struct{}),
				release: make(chan struct{}),
				call: func(p []byte) (int, error) {
					if tc.writeErr != nil {
						return 0, tc.writeErr
					}
					return len(p), nil
				},
			}
			c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.selectTerminal(ErrClosed) })
			cause := sentinel("teardown won")

			done := make(chan error, 1)
			go func() {
				done <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, okDecoder)
			}()
			<-s.entered

			// Teardown claims the admitted entry before the write proceeds.
			c.selectTerminal(cause)
			close(s.release)

			if err := <-done; err != cause {
				t.Fatalf("call returned %v, want the terminal cause %v", err, cause)
			}
			waitTerminal(t, c)
			if pendingLen(c) != 0 {
				t.Error("teardown left a pending entry")
			}
		})
	}
}

// TestPendingTerminalClaimsAll pins terminal teardown: selection completes
// every unclaimed pending call with the permanent cause, and later responses
// for those IDs are unmatched.
func TestPendingTerminalClaimsAll(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	cause := sentinel("terminal claims")

	dones := make([]chan error, 2)
	for i := range dones {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(i+1),
				func() ([]byte, error) { return []byte{byte('a' + i)}, nil }, okDecoder)
		}(i)
	}
	waitPending(t, c, 2)

	c.selectTerminal(cause)
	for i := range dones {
		if err := <-dones[i]; err != cause {
			t.Fatalf("call %d returned %v, want the terminal cause %v", i, err, cause)
		}
	}
	if pendingLen(c) != 0 {
		t.Error("teardown left pending entries")
	}
	if claimed := c.claimResponse(0, 0, nil); claimed {
		t.Fatal("response after teardown matched a claimed entry")
	}
	if s.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", s.closeCount())
	}
}

// TestPendingExclusiveOutcomes races a response claim, per-call
// cancellation, and terminal teardown against one admitted call and pins the
// pending-map ownership rule: exactly one map removal owns the outcome, the
// call's result is exactly that outcome, and no other claimant affects it.
func TestPendingExclusiveOutcomes(t *testing.T) {
	for i := 0; i < 25; i++ {
		s := &recordingStream{}
		c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.selectTerminal(ErrClosed) })

		ctx, cancel := context.WithCancel(context.Background())
		dec := &recordingDecoder{}
		done := make(chan error, 1)
		terminalCause := sentinel(fmt.Sprintf("terminal %d", i))
		go func() {
			done <- c.Call(ctx, c.imp, 1, func() ([]byte, error) { return []byte("req"), nil }, dec.decode)
		}()
		waitPending(t, c, 1)

		claimed := make(chan bool, 1)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			claimed <- c.claimResponse(0, 0, []byte("resp"))
		}()
		go func() {
			defer wg.Done()
			cancel()
		}()
		go func() {
			defer wg.Done()
			c.selectTerminal(terminalCause)
		}()
		wg.Wait()

		result := <-done
		responseWon := <-claimed
		if responseWon != (result == nil) {
			t.Fatalf("iteration %d: claimResponse = %v but result = %v; exactly one removal must own the outcome", i, responseWon, result)
		}
		switch {
		case result == nil:
			// The response claimed the entry and its decoder succeeded.
			if dec.callCount() != 1 {
				t.Fatalf("iteration %d: decoder invoked %d times after a matched response", i, dec.callCount())
			}
		case result == context.Canceled:
			// Per-call cancellation claimed the entry; the decoder never
			// ran, and the exact context error is the outcome.
			if dec.callCount() != 0 {
				t.Fatalf("iteration %d: decoder invoked %d times after cancellation won", i, dec.callCount())
			}
		case result == terminalCause:
			// Terminal teardown claimed the entry with its permanent cause.
			if dec.callCount() != 0 {
				t.Fatalf("iteration %d: decoder invoked %d times after teardown won", i, dec.callCount())
			}
		default:
			t.Fatalf("iteration %d: unexpected outcome %v", i, result)
		}
		if pendingLen(c) != 0 {
			t.Fatalf("iteration %d: %d pending entries remain after the outcome", i, pendingLen(c))
		}
	}
}

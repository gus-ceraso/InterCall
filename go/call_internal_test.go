package intercall

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// recordingStream is a ByteStream for outgoing-call tests: it records every
// accepted byte, Write always accepts the complete buffer, Read is inert,
// and Close is counted.
type recordingStream struct {
	mu       sync.Mutex
	closes   int
	recorded []byte
}

var _ ByteStream = (*recordingStream)(nil)

func (s *recordingStream) Read(p []byte) (int, error) { return 0, io.EOF }

func (s *recordingStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.recorded = append(s.recorded, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *recordingStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

func (s *recordingStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

func (s *recordingStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// blockingStream is a ByteStream whose first Write blocks until release
// closes, then delegates to a per-test hook, recording every accepted byte.
// It models a slow full-duplex transport for the write-gate, admission, and
// full-duplex response tests.
type blockingStream struct {
	mu       sync.Mutex
	entered  chan struct{} // closed once when the first Write is entered
	release  chan struct{} // closed by the test to let the write proceed
	call     func(p []byte) (int, error)
	recorded []byte
}

var _ ByteStream = (*blockingStream)(nil)

func (s *blockingStream) Read(p []byte) (int, error) { return 0, io.EOF }

func (s *blockingStream) Write(p []byte) (int, error) {
	select {
	case <-s.entered:
	default:
		close(s.entered)
	}
	<-s.release
	n, err := s.call(p)
	if n >= 0 && n <= len(p) {
		s.mu.Lock()
		s.recorded = append(s.recorded, p[:n]...)
		s.mu.Unlock()
	}
	return n, err
}

func (s *blockingStream) Close() error { return nil }

func (s *blockingStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

// newCallTestConn constructs an active connection over a recording stream
// and guarantees terminal selection at test end so the observer can never
// strand.
func newCallTestConn(t *testing.T, s *recordingStream) *Connection {
	t.Helper()
	c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })
	return c
}

// callCounter is a request encoder that counts invocations and returns a
// fixed payload or error.
type callCounter struct {
	mu      sync.Mutex
	calls   int
	payload []byte
	err     error
}

func (e *callCounter) encode() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return e.payload, e.err
}

func (e *callCounter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// okDecoder accepts every response.
func okDecoder(uint64, []byte) error { return nil }

// waitRecorded blocks until at least want bytes are recorded on the stream.
func waitRecorded(t *testing.T, s *recordingStream, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := len(s.bytes()); got >= want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d recorded bytes, have %d", want, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// splitFrames parses recorded bytes into complete frames, verifying that
// every header and payload length is consistent.
func splitFrames(t *testing.T, b []byte) [][]byte {
	t.Helper()
	var frames [][]byte
	for len(b) > 0 {
		hdr, err := parseFrameHeader(b[:frameHeaderSize])
		if err != nil {
			t.Fatalf("malformed recorded frame header: %v", err)
		}
		n := frameHeaderSize + int(hdr.payloadLength)
		if n > len(b) {
			t.Fatalf("recorded frame declares %d payload bytes but only %d remain", hdr.payloadLength, len(b)-frameHeaderSize)
		}
		frames = append(frames, b[:n])
		b = b[n:]
	}
	return frames
}

// TestCallArgumentValidation pins step 1: a nil receiver, nil context, zero
// procedure key, and nil encoder or decoder all return ErrInvalidArgument
// before any terminal-state inspection, without invoking the encoder or
// writing bytes.
func TestCallArgumentValidation(t *testing.T) {
	imp := NewImportBinding()
	enc := &callCounter{payload: []byte("payload")}

	base := func(c *Connection) error {
		return c.Call(context.Background(), imp, 1, enc.encode, okDecoder)
	}
	if err := base(nil); err != ErrInvalidArgument {
		t.Errorf("nil receiver: err = %v, want ErrInvalidArgument", err)
	}
	if !errors.Is(base(nil), ErrInvalidArgument) {
		t.Error("errors.Is(nil receiver, ErrInvalidArgument) = false")
	}

	s := &recordingStream{}
	c := newCallTestConn(t, s)

	if err := c.Call(nil, imp, 1, enc.encode, okDecoder); err != ErrInvalidArgument {
		t.Errorf("nil context: err = %v, want ErrInvalidArgument", err)
	}
	if err := c.Call(context.Background(), imp, 0, enc.encode, okDecoder); err != ErrInvalidArgument {
		t.Errorf("zero procedure key: err = %v, want ErrInvalidArgument", err)
	}
	if err := c.Call(context.Background(), imp, 1, nil, okDecoder); err != ErrInvalidArgument {
		t.Errorf("nil encoder: err = %v, want ErrInvalidArgument", err)
	}
	if err := c.Call(context.Background(), imp, 1, enc.encode, nil); err != ErrInvalidArgument {
		t.Errorf("nil decoder: err = %v, want ErrInvalidArgument", err)
	}

	if enc.count() != 0 {
		t.Errorf("encoder invoked %d times by invalid calls", enc.count())
	}
	if len(s.bytes()) != 0 {
		t.Error("invalid calls wrote bytes")
	}
	if c.nextID != 0 {
		t.Errorf("invalid calls consumed %d request IDs", c.nextID)
	}
	if len(c.pending) != 0 {
		t.Errorf("invalid calls left %d pending entries", len(c.pending))
	}
}

// TestCallBindingMismatch pins step 1's import identity check: a zero or
// different import handle returns ErrBindingMismatch before terminal-state
// inspection and before the encoder, on both active and terminal
// connections.
func TestCallBindingMismatch(t *testing.T) {
	enc := &callCounter{payload: []byte("payload")}

	for _, tc := range []struct {
		name   string
		finish bool
	}{
		{"active connection", false},
		{"terminal connection", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordingStream{}
			c := newCallTestConn(t, s)
			if tc.finish {
				finish(t, c, ErrClosed)
			}
			other := NewImportBinding()

			for _, imp := range []ImportBinding{{}, other} {
				err := c.Call(context.Background(), imp, 1, enc.encode, okDecoder)
				if err != ErrBindingMismatch {
					t.Errorf("err = %v, want ErrBindingMismatch", err)
				}
				if !errors.Is(err, ErrBindingMismatch) {
					t.Error("errors.Is(err, ErrBindingMismatch) = false")
				}
			}
			if enc.count() != 0 {
				t.Errorf("encoder invoked %d times after binding mismatch", enc.count())
			}
			if len(s.bytes()) != 0 {
				t.Error("binding mismatch wrote bytes")
			}
		})
	}
}

// TestCallTerminalAndContextPrecedence pins step 2: an already selected
// terminal cause wins over an already canceled call context, otherwise the
// exact context error wins, and neither invokes the encoder.
func TestCallTerminalAndContextPrecedence(t *testing.T) {
	cause := sentinel("permanent cause")
	enc := &callCounter{payload: []byte("payload")}

	t.Run("canceled context on active connection", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := c.Call(ctx, c.imp, 1, enc.encode, okDecoder)
		if err != context.Canceled {
			t.Errorf("err = %v, want exactly context.Canceled", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Error("errors.Is(err, context.Canceled) = false")
		}
		if enc.count() != 0 {
			t.Errorf("encoder invoked %d times for a canceled call", enc.count())
		}
		if len(s.bytes()) != 0 || c.nextID != 0 {
			t.Error("canceled call consumed an ID or wrote bytes")
		}
	})

	t.Run("expired deadline on active connection", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
		defer cancel()
		err := c.Call(ctx, c.imp, 1, enc.encode, okDecoder)
		if err != context.DeadlineExceeded {
			t.Errorf("err = %v, want exactly context.DeadlineExceeded", err)
		}
		if enc.count() != 0 {
			t.Errorf("encoder invoked %d times for an expired call", enc.count())
		}
		if len(s.bytes()) != 0 || c.nextID != 0 {
			t.Error("expired call consumed an ID or wrote bytes")
		}
	})

	t.Run("terminal cause wins over canceled context", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		finish(t, c, cause)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := c.Call(ctx, c.imp, 1, enc.encode, okDecoder)
		if err != cause {
			t.Errorf("err = %v, want the terminal cause %v", err, cause)
		}
		if enc.count() != 0 {
			t.Errorf("encoder invoked %d times on a terminal connection", enc.count())
		}
		if len(s.bytes()) != 0 {
			t.Error("terminal connection wrote bytes")
		}
	})

	t.Run("terminal cause with valid context", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		finish(t, c, cause)
		err := c.Call(context.Background(), c.imp, 1, enc.encode, okDecoder)
		if err != cause {
			t.Errorf("err = %v, want the terminal cause %v", err, cause)
		}
		if enc.count() != 0 {
			t.Errorf("encoder invoked %d times on a terminal connection", enc.count())
		}
	})
}

// TestCallEncoderExactError pins step 4: the encoder's exact error returns
// directly, with no ID allocated, no frame built or written, no write gate
// entered, and no terminal selection.
func TestCallEncoderExactError(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	encoderErr := errors.New("encoder failure")
	enc := &callCounter{err: encoderErr}

	err := c.Call(context.Background(), c.imp, 1, enc.encode, okDecoder)
	if err != encoderErr {
		t.Errorf("err = %v, want the encoder's exact error", err)
	}
	if !errors.Is(err, encoderErr) {
		t.Error("errors.Is(err, encoderErr) = false")
	}
	if enc.count() != 1 {
		t.Errorf("encoder invoked %d times, want exactly once", enc.count())
	}
	if len(s.bytes()) != 0 {
		t.Error("encoder error wrote bytes")
	}
	if c.nextID != 0 {
		t.Errorf("encoder error consumed %d request IDs", c.nextID)
	}
	if len(c.pending) != 0 {
		t.Errorf("encoder error left %d pending entries", len(c.pending))
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("encoder error terminated the connection")
	}
}

// TestCallPostEncodeChecks pins step 5: termination or cancellation that
// happens while a successful encoder runs is returned by the post-encode
// check without an ID or frame, and without a write.
func TestCallPostEncodeChecks(t *testing.T) {
	blocking := func(started, release chan struct{}) func() ([]byte, error) {
		return func() ([]byte, error) {
			close(started)
			<-release
			return []byte("payload"), nil
		}
	}

	t.Run("terminal during encode", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		cause := sentinel("terminal during encode")

		go func() {
			done <- c.Call(context.Background(), c.imp, 1, blocking(started, release), okDecoder)
		}()
		<-started
		c.selectTerminal(cause)
		close(release)

		if err := <-done; err != cause {
			t.Errorf("err = %v, want the terminal cause %v", err, cause)
		}
		if len(s.bytes()) != 0 {
			t.Error("post-encode terminal check wrote bytes")
		}
		if c.nextID != 0 {
			t.Errorf("post-encode terminal check consumed %d request IDs", c.nextID)
		}
		if len(c.pending) != 0 {
			t.Errorf("post-encode terminal check left %d pending entries", len(c.pending))
		}
	})

	t.Run("cancellation during encode", func(t *testing.T) {
		s := &recordingStream{}
		c := newCallTestConn(t, s)
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			done <- c.Call(ctx, c.imp, 1, blocking(started, release), okDecoder)
		}()
		<-started
		cancel()
		close(release)

		if err := <-done; err != context.Canceled {
			t.Errorf("err = %v, want exactly context.Canceled", err)
		}
		if len(s.bytes()) != 0 {
			t.Error("post-encode cancellation check wrote bytes")
		}
		if c.nextID != 0 {
			t.Errorf("post-encode cancellation check consumed %d request IDs", c.nextID)
		}
		if len(c.pending) != 0 {
			t.Errorf("post-encode cancellation check left %d pending entries", len(c.pending))
		}
		c.mu.Lock()
		active := c.cause == nil
		c.mu.Unlock()
		if !active {
			t.Error("per-call cancellation terminated the connection")
		}
	})
}

// TestCallEncoderAtMostOnce pins step 3: the encoder is invoked exactly once
// per admitted call, and a successful call writes exactly one complete
// request frame with the allocated ID.
func TestCallEncoderAtMostOnce(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	enc := &callCounter{payload: []byte("once")}

	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), c.imp, 0x42, enc.encode, okDecoder) }()
	waitRecorded(t, s, frameHeaderSize+4)
	if enc.count() != 1 {
		t.Fatalf("encoder invoked %d times before the response, want once", enc.count())
	}
	if !c.claimResponse(0, 0, nil) {
		t.Fatal("claimResponse(0) did not match the pending call")
	}
	if err := <-done; err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if enc.count() != 1 {
		t.Errorf("encoder invoked %d times after completion, want once", enc.count())
	}

	frames := splitFrames(t, s.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want 1", len(frames))
	}
	want := buildFrame(requestFrame, 0, 0x42, []byte("once"))
	if !bytes.Equal(frames[0], want) {
		t.Errorf("frame = %x, want %x", frames[0], want)
	}
}

// TestCallWritesOrderedFrames pins monotonic 63-bit ID allocation: three
// concurrent calls receive distinct IDs 0, 1, and 2, frames carry their
// allocated IDs, and completed IDs are never reused.
func TestCallWritesOrderedFrames(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)

	keys := []uint64{0x1111, 0x2222, 0x3333}
	payloads := [][]byte{[]byte("aaa"), []byte("bb"), []byte("c")}
	dones := make([]chan error, len(keys))
	for i := range keys {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, keys[i],
				func() ([]byte, error) { return payloads[i], nil }, okDecoder)
		}(i)
	}
	total := 0
	for _, p := range payloads {
		total += frameHeaderSize + len(p)
	}
	waitRecorded(t, s, total)

	// The write gate admits calls in scheduling order, so any call may hold
	// any ID; parse the recorded frames and claim every recorded ID.
	frames := splitFrames(t, s.bytes())
	if len(frames) != len(keys) {
		t.Fatalf("recorded %d frames, want %d", len(frames), len(keys))
	}
	ids := make([]uint64, 0, len(frames))
	seen := make(map[uint64]bool)
	for i, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if hdr.kind != requestFrame {
			t.Errorf("frame %d kind = %v, want request", i, hdr.kind)
		}
		if seen[hdr.requestID] {
			t.Errorf("request ID %d reused", hdr.requestID)
		}
		seen[hdr.requestID] = true
		if hdr.requestID > 2 {
			t.Errorf("frame %d has out-of-range ID %d", i, hdr.requestID)
		}
		// The frame's key and payload must belong to the same call.
		matched := false
		for j := range keys {
			if hdr.key == keys[j] && bytes.Equal(f[frameHeaderSize:], payloads[j]) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("frame %d key %#x with payload %q matches no call", i, hdr.key, f[frameHeaderSize:])
		}
		ids = append(ids, hdr.requestID)
	}
	for i := range ids {
		if !c.claimResponse(ids[i], 0, nil) {
			t.Fatalf("claimResponse(%d) did not match a pending call", ids[i])
		}
	}
	for i := range keys {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	c.mu.Lock()
	next := c.nextID
	c.mu.Unlock()
	if next != uint64(len(keys)) {
		t.Errorf("nextID = %d, want %d (IDs must never be reused)", next, len(keys))
	}
}

// TestCallGateSerializes pins the write gate for outgoing calls: two
// concurrent calls over a one-byte-at-a-time stream never interleave; the
// recorded bytes are exactly one complete frame followed by the other.
func TestCallGateSerializes(t *testing.T) {
	w := &scriptedWriter{call: func(p []byte) (int, error) { return 1, nil }}
	stream := &scriptedStream{write: w.Write}
	c := newWriteTestConn(t, stream)

	payloads := [][]byte{bytes.Repeat([]byte{0xaa}, 64), bytes.Repeat([]byte{0xbb}, 64)}
	dones := make([]chan error, len(payloads))
	for i := range payloads {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(i+1),
				func() ([]byte, error) { return payloads[i], nil }, okDecoder)
		}(i)
	}
	total := 0
	for _, p := range payloads {
		total += frameHeaderSize + len(p)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(w.bytes()) < total {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for both frames to be recorded")
		}
		time.Sleep(time.Millisecond)
	}

	// Both calls are admitted; claim every recorded ID.
	frames := splitFrames(t, w.bytes())
	if len(frames) != 2 {
		t.Fatalf("recorded %d frames, want 2", len(frames))
	}
	ids := make([]uint64, 0, 2)
	for i, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if hdr.kind != requestFrame {
			t.Errorf("frame %d kind = %v, want request", i, hdr.kind)
		}
		// The frame must be exactly one call's complete frame: key 1 with
		// the 0xaa payload or key 2 with the 0xbb payload.
		want := buildFrame(requestFrame, hdr.requestID, 1, payloads[0])
		if !bytes.Equal(f, want) {
			want = buildFrame(requestFrame, hdr.requestID, 2, payloads[1])
			if !bytes.Equal(f, want) {
				t.Errorf("frame %d is not one complete call frame: got %x", i, f)
			}
		}
		ids = append(ids, hdr.requestID)
	}
	if ids[0] == ids[1] {
		t.Errorf("both frames carry request ID %d", ids[0])
	}
	for i := 0; i < len(payloads); i++ {
		if !c.claimResponse(uint64(i), 0, nil) {
			t.Fatalf("claimResponse(%d) did not match a pending call", i)
		}
	}
	for i := range payloads {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	// splitFrames already proved the recorded bytes are exactly two
	// complete contiguous frames, so the concurrent calls never interleaved.
}

// TestCallNoAllocationBeforeWriteAdmission pins step 6: while a call waits
// for the write gate, no ID is allocated and no frame is written; a
// cancellation that fires during the wait wins without consuming an ID.
func TestCallNoAllocationBeforeWriteAdmission(t *testing.T) {
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

	// A holds the gate with a slow write.
	aDone := make(chan error, 1)
	go func() {
		aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
	}()
	<-s.entered

	// B's encoder runs, then B waits for the gate.
	bEncoded := make(chan struct{})
	bDone := make(chan error, 1)
	bCtx, bCancel := context.WithCancel(context.Background())
	go func() {
		bDone <- c.Call(bCtx, c.imp, 2, func() ([]byte, error) {
			close(bEncoded)
			return []byte("b"), nil
		}, okDecoder)
	}()
	<-bEncoded

	// Cancel B while it waits for the gate; the cancellation must win
	// without an ID or frame. Then release A's slow write.
	bCancel()
	close(s.release)

	if err := <-bDone; err != context.Canceled {
		t.Fatalf("waiting call returned %v, want exactly context.Canceled", err)
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
	if hdr, err := parseFrameHeader(frames[0][:frameHeaderSize]); err != nil || hdr.requestID != 0 {
		t.Errorf("A's frame ID = %v (err %v), want 0; B must not have consumed an ID", hdr.requestID, err)
	}
	c.mu.Lock()
	next := c.nextID
	c.mu.Unlock()
	if next != 1 {
		t.Errorf("nextID = %d, want 1: no ID may be allocated while waiting for the write gate", next)
	}
}

// TestCallExhaustionBeforeAdmissionNoFrame pins final-ID exhaustion: while a
// call waits for the gate, the final ID is allocated by the gate holder; the
// waiting call then returns ErrRequestIDsExhausted without writing a frame.
func TestCallExhaustionBeforeAdmissionNoFrame(t *testing.T) {
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
	c.mu.Lock()
	c.nextID = idMask
	c.mu.Unlock()

	// A holds the gate and allocates the final ID.
	aDone := make(chan error, 1)
	go func() {
		aDone <- c.Call(context.Background(), c.imp, 1, func() ([]byte, error) { return []byte("a"), nil }, okDecoder)
	}()
	<-s.entered

	// B waits for the gate; after admission it finds the IDs exhausted.
	bDone := make(chan error, 1)
	bEncoded := make(chan struct{})
	go func() {
		bDone <- c.Call(context.Background(), c.imp, 2, func() ([]byte, error) {
			close(bEncoded)
			return []byte("b"), nil
		}, okDecoder)
	}()
	<-bEncoded
	close(s.release)

	if err := <-bDone; err != ErrRequestIDsExhausted {
		t.Fatalf("exhausted call returned %v, want ErrRequestIDsExhausted", err)
	} else if !errors.Is(err, ErrRequestIDsExhausted) {
		t.Fatal("errors.Is(err, ErrRequestIDsExhausted) = false")
	}
	if !c.claimResponse(idMask, 0, nil) {
		t.Fatal("claimResponse(final ID) did not match A's pending call")
	}
	if err := <-aDone; err != nil {
		t.Fatalf("A's call failed: %v", err)
	}

	frames := splitFrames(t, s.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want only A's final-ID frame", len(frames))
	}
	if hdr, err := parseFrameHeader(frames[0][:frameHeaderSize]); err != nil || hdr.requestID != idMask {
		t.Errorf("A's frame ID = %v (err %v), want %#x", hdr.requestID, err, idMask)
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("ID exhaustion terminated the connection")
	}
}

// TestCallFinalIDExhaustion pins the durable exhaustion contract: after the
// final ID is allocated and completed, the next call returns
// ErrRequestIDsExhausted without writing a frame, and the connection stays
// active.
func TestCallFinalIDExhaustion(t *testing.T) {
	s := &recordingStream{}
	c := newCallTestConn(t, s)
	c.mu.Lock()
	c.nextID = idMask
	c.mu.Unlock()

	done := make(chan error, 1)
	enc := &callCounter{payload: []byte("last")}
	go func() { done <- c.Call(context.Background(), c.imp, 7, enc.encode, okDecoder) }()
	waitRecorded(t, s, frameHeaderSize+4)

	if !c.claimResponse(idMask, 0, nil) {
		t.Fatal("claimResponse(final ID) did not match the pending call")
	}
	if err := <-done; err != nil {
		t.Fatalf("final call failed: %v", err)
	}

	before := len(s.bytes())
	err := c.Call(context.Background(), c.imp, 8, enc.encode, okDecoder)
	if err != ErrRequestIDsExhausted {
		t.Errorf("err = %v, want ErrRequestIDsExhausted", err)
	}
	if !errors.Is(err, ErrRequestIDsExhausted) {
		t.Error("errors.Is(err, ErrRequestIDsExhausted) = false")
	}
	if enc.count() != 2 {
		t.Errorf("encoder invoked %d times, want twice (once per call)", enc.count())
	}
	if len(s.bytes()) != before {
		t.Error("exhausted call wrote bytes")
	}
	if len(c.pending) != 0 {
		t.Errorf("exhausted call left %d pending entries", len(c.pending))
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("ID exhaustion terminated the connection")
	}
}

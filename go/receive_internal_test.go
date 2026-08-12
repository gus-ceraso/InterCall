package intercall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pipeStream is a full-duplex ByteStream for receive-dispatch tests. Its
// read side is one end of an io.Pipe, so the test writes exactly the frames
// the receive loop should consume and gets backpressure: a feed blocks until
// the loop has consumed every byte. Write records every accepted byte and
// may delegate to a per-test hook. Close closes the pipe's read end,
// unblocking the receive loop, and counts calls. Read tracks concurrent
// readers so tests can pin the sole-reader contract.
type pipeStream struct {
	r        *io.PipeReader
	w        *io.PipeWriter
	write    func(p []byte) (int, error) // optional write hook
	mu       sync.Mutex
	recorded []byte
	closes   int
	closeErr error

	inflight int32
	maxReads int32
}

var _ ByteStream = (*pipeStream)(nil)

func newPipeStream() *pipeStream {
	r, w := io.Pipe()
	return &pipeStream{r: r, w: w}
}

func (s *pipeStream) Read(p []byte) (int, error) {
	n := atomic.AddInt32(&s.inflight, 1)
	for {
		m := atomic.LoadInt32(&s.maxReads)
		if n <= m || atomic.CompareAndSwapInt32(&s.maxReads, m, n) {
			break
		}
	}
	defer atomic.AddInt32(&s.inflight, -1)
	return s.r.Read(p)
}

func (s *pipeStream) Write(p []byte) (int, error) {
	if s.write != nil {
		n, err := s.write(p)
		s.mu.Lock()
		if n > 0 && n <= len(p) {
			s.recorded = append(s.recorded, p[:n]...)
		}
		s.mu.Unlock()
		return n, err
	}
	s.mu.Lock()
	s.recorded = append(s.recorded, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *pipeStream) Close() error {
	s.mu.Lock()
	s.closes++
	err := s.closeErr
	s.mu.Unlock()
	_ = s.r.CloseWithError(io.ErrClosedPipe)
	return err
}

// feed writes one frame into the pipe; it returns only after the receive
// loop has consumed every byte.
func (s *pipeStream) feed(t *testing.T, frame []byte) {
	t.Helper()
	if _, err := s.w.Write(frame); err != nil {
		t.Fatalf("feeding a frame into the pipe: %v", err)
	}
}

// closeWriter closes the test side of the pipe, so the receive loop's next
// read reports io.EOF.
func (s *pipeStream) closeWriter() {
	_ = s.w.Close()
}

func (s *pipeStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

func (s *pipeStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func (s *pipeStream) maxConcurrentReads() int32 { return atomic.LoadInt32(&s.maxReads) }

// gatedStream is a ByteStream for the receive-loop lifecycle tests. Read
// parks on release until the test unblocks it, then reports readErr; Write
// records every accepted byte; Close counts calls and reports closeErr
// without unblocking a parked Read, so tests control exactly when the
// receive loop proceeds.
type gatedStream struct {
	mu       sync.Mutex
	readErr  error
	recorded []byte
	closes   int
	closeErr error

	entered     chan struct{} // closed when the first Read is entered
	release     chan struct{} // Read parks here until unblocked
	enteredOnce sync.Once
	releaseOnce sync.Once
}

var _ ByteStream = (*gatedStream)(nil)

func newGatedStream(readErr error) *gatedStream {
	return &gatedStream{
		readErr: readErr,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *gatedStream) Read(p []byte) (int, error) {
	s.enteredOnce.Do(func() { close(s.entered) })
	<-s.release
	return 0, s.readErr
}

func (s *gatedStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.recorded = append(s.recorded, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *gatedStream) Close() error {
	s.mu.Lock()
	s.closes++
	err := s.closeErr
	s.mu.Unlock()
	return err
}

// unblock releases a parked Read exactly once.
func (s *gatedStream) unblock() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *gatedStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

func (s *gatedStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// stagedFrameStream serves one complete request frame to the receive loop
// across the two reads of readFrame (the 24-byte header, then the payload)
// and parks the payload read until releasePayload, so a test can select
// terminal while the frame is still being buffered and then let the loop
// finish buffering it. Every later read parks until releaseReads and then
// returns io.EOF. Writes are recorded and Close is counted.
type stagedFrameStream struct {
	frame []byte

	mu       sync.Mutex
	recorded []byte
	closes   int

	headerRead     chan struct{} // closed when the header is served
	payloadRelease chan struct{} // the payload read parks here
	payloadServed  chan struct{} // closed when the payload is served
	readRelease    chan struct{} // later reads park here
	servedHeader   bool
	servedPayload  bool
	payloadOnce    sync.Once
	readOnce       sync.Once
}

var _ ByteStream = (*stagedFrameStream)(nil)

func newStagedFrameStream(frame []byte) *stagedFrameStream {
	return &stagedFrameStream{
		frame:          frame,
		headerRead:     make(chan struct{}),
		payloadRelease: make(chan struct{}),
		payloadServed:  make(chan struct{}),
		readRelease:    make(chan struct{}),
	}
}

func (s *stagedFrameStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	if !s.servedHeader {
		s.servedHeader = true
		n := copy(p, s.frame[:frameHeaderSize])
		s.mu.Unlock()
		close(s.headerRead)
		return n, nil
	}
	if !s.servedPayload {
		s.mu.Unlock()
		<-s.payloadRelease
		s.mu.Lock()
		s.servedPayload = true
		n := copy(p, s.frame[frameHeaderSize:])
		s.mu.Unlock()
		close(s.payloadServed)
		return n, nil
	}
	s.mu.Unlock()
	<-s.readRelease
	return 0, io.EOF
}

func (s *stagedFrameStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.recorded = append(s.recorded, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *stagedFrameStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

// releasePayload lets the parked payload read complete, exactly once.
func (s *stagedFrameStream) releasePayload() { s.payloadOnce.Do(func() { close(s.payloadRelease) }) }

// releaseReads lets later parked reads complete with io.EOF, exactly once.
func (s *stagedFrameStream) releaseReads() { s.readOnce.Do(func() { close(s.readRelease) }) }

func (s *stagedFrameStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.recorded...)
}

func (s *stagedFrameStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// probeStream counts Read and Close calls so a rejected construction can
// prove that no receive loop started and the stream was never owned.
type probeStream struct {
	mu     sync.Mutex
	reads  int
	closes int
}

var _ ByteStream = (*probeStream)(nil)

func (s *probeStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	return 0, io.EOF
}

func (s *probeStream) Write(p []byte) (int, error) { return len(p), nil }

func (s *probeStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

// succeedDispatch accepts every incoming request with a no-payload success
// response.
func succeedDispatch(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil }

// echoDispatch responds with the request's own key and payload.
func echoDispatch(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
	return key, payload
}

// newReceiveTestConn constructs a connection through the public
// NewConnection constructor over the given stream with a fresh import
// binding and the given dispatch, guaranteeing terminal selection at test
// end so the observer and receive loop can never strand.
func newReceiveTestConn(t *testing.T, stream ByteStream, dispatch Dispatch) *Connection {
	t.Helper()
	export, err := NewExportBinding(dispatch)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConnection(context.Background(), stream, export, NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })
	return c
}

// waitBytes polls until the stream has recorded at least want bytes.
func waitBytes(t *testing.T, bytes func() []byte, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := len(bytes()); got >= want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d recorded bytes, have %d", want, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitIncoming blocks until the incoming-ID active set holds exactly want
// entries.
func waitIncoming(t *testing.T, c *Connection, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		got := len(c.incoming)
		c.mu.Unlock()
		if got == want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d active incoming IDs, have %d", want, got)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitReceiveExit blocks until the receive loop exits, with a timeout.
func waitReceiveExit(t *testing.T, c *Connection) {
	t.Helper()
	select {
	case <-c.receiveExit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the receive loop to exit")
	}
}

// waitEntered blocks until the stream reports its first Read, with a
// timeout.
func waitEntered(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first read")
	}
}

// TestConnectionConstructorValidation pins the public constructor contract:
// a nil context or stream interface and a zero export or import binding are
// rejected with ErrInvalidArgument, and an already available ctx.Err() is
// returned exactly, all before the stream is claimed: no stream close and no
// receive-loop read may occur.
func TestConnectionConstructorValidation(t *testing.T) {
	imp := NewImportBinding()
	export := newTestExport(t)
	s := &probeStream{}

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
			c, err := NewConnection(tc.ctx, tc.stream, tc.export, tc.imp)
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

	// Canceled context: the exact ctx.Err() before ownership.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := NewConnection(ctx, s, export, imp)
	if err != context.Canceled {
		t.Errorf("canceled ctx: err = %v, want context.Canceled", err)
	}
	if c != nil {
		t.Error("canceled ctx: construction returned a connection")
	}

	if s.closes != 0 {
		t.Errorf("failed constructions closed the stream %d times", s.closes)
	}
	if s.reads != 0 {
		t.Errorf("failed constructions started a receive loop (%d reads)", s.reads)
	}
}

// TestConnectionReceiveLoopStartsBeforeReturn pins that NewConnection starts
// the sole receive loop before returning: after construction, the loop reads
// without any further action, and an EOF read terminates the connection with
// an exact EOF transport cause that Wait reports.
func TestConnectionReceiveLoopStartsBeforeReturn(t *testing.T) {
	stream := newGatedStream(io.EOF)
	export, err := NewExportBinding(succeedDispatch)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConnection(context.Background(), stream, export, NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.unblock)
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	waitEntered(t, stream.entered)

	stream.unblock()
	waitTerminal(t, c)
	waitReceiveExit(t, c)
	if err := c.Wait(); err == nil {
		t.Fatal("Wait() = nil; it must never return nil")
	} else if !errors.Is(err, io.EOF) {
		t.Errorf("Wait() = %v, want an EOF transport cause", err)
	} else if errors.Is(err, ErrProtocol) {
		t.Error("EOF classified as a protocol error")
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestConnectionSoleReader pins the only-one-reader contract: a mix of
// incoming requests, outgoing calls, and responses on one stream never reads
// from two goroutines, and every frame is complete and contiguous.
func TestConnectionSoleReader(t *testing.T) {
	stream := newPipeStream()
	var handled atomic.Int32
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		handled.Add(1)
		return key, payload
	}
	c := newReceiveTestConn(t, stream, dispatch)

	// Two outbound calls race two incoming requests over one stream.
	callPayloads := [][]byte{[]byte{0xaa}, []byte{0xbb}}
	dones := make([]chan error, len(callPayloads))
	for i := range dones {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(0x100+i),
				func() ([]byte, error) { return callPayloads[i], nil }, okDecoder)
		}(i)
	}
	waitBytes(t, stream.bytes, len(callPayloads)*(frameHeaderSize+1))

	reqPayloads := [][]byte{[]byte("in one"), []byte("in two")}
	for i := range reqPayloads {
		stream.feed(t, buildFrame(requestFrame, uint64(10+i), uint64(0x300+i), reqPayloads[i]))
	}
	waitBytes(t, stream.bytes, 2*(frameHeaderSize+1)+2*(frameHeaderSize+6))

	// Both outgoing calls get their responses; everything settles.
	for i := range dones {
		stream.feed(t, buildFrame(responseFrame, uint64(i), 0, nil))
	}
	for i := range dones {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	if handled.Load() != 2 {
		t.Errorf("dispatch ran %d times, want 2", handled.Load())
	}
	if stream.maxConcurrentReads() != 1 {
		t.Errorf("max concurrent reads = %d, want exactly 1 (sole receive loop)", stream.maxConcurrentReads())
	}

	frames := splitFrames(t, stream.bytes())
	if len(frames) != 4 {
		t.Fatalf("recorded %d frames, want 4 complete frames", len(frames))
	}
	reqFrames, respFrames := 0, 0
	seenReq := make(map[uint64]bool)
	for _, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		payload := f[frameHeaderSize:]
		switch hdr.kind {
		case requestFrame:
			reqFrames++
			// Any call may hold any allocated ID; match by key and payload.
			matched := false
			for j := range callPayloads {
				if hdr.key == uint64(0x100+j) && bytes.Equal(payload, callPayloads[j]) {
					matched = true
					break
				}
			}
			if !matched || hdr.requestID >= 2 || seenReq[hdr.requestID] {
				t.Errorf("unexpected request frame %+v payload %v", hdr, payload)
			}
			seenReq[hdr.requestID] = true
		case responseFrame:
			respFrames++
			if hdr.requestID < 10 || hdr.requestID >= 10+uint64(len(reqPayloads)) || hdr.key != uint64(0x300+hdr.requestID-10) || !bytes.Equal(payload, reqPayloads[hdr.requestID-10]) {
				t.Errorf("unexpected response frame %+v payload %q", hdr, payload)
			}
		}
	}
	if reqFrames != 2 || respFrames != 2 {
		t.Errorf("recorded %d request and %d response frames, want 2 and 2", reqFrames, respFrames)
	}
	if pendingLen(c) != 0 {
		t.Error("pending entries remain after all responses")
	}
	waitIncoming(t, c, 0)

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveResponseOutOfOrder pins that matched responses may arrive in
// any order: each response claims its own pending entry by ID, completes its
// call in the receive goroutine, and delivers the exact key and owned
// payload to that call's decoder.
func TestReceiveResponseOutOfOrder(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, succeedDispatch)

	payloads := []string{"zero", "one", "two"}
	order := make(chan string, len(payloads))
	dones := make([]chan error, len(payloads))
	for i := range dones {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(0x10+i),
				func() ([]byte, error) { return []byte(payloads[i]), nil },
				func(key uint64, payload []byte) error {
					if key != 0 {
						return fmt.Errorf("decoder saw key %#x, want 0", key)
					}
					order <- string(payload)
					return nil
				})
		}(i)
	}
	total := 0
	for _, p := range payloads {
		total += frameHeaderSize + len(p)
	}
	waitBytes(t, stream.bytes, total)

	// Responses arrive in reverse ID order. Which call holds which ID is
	// scheduling-dependent, but the receive loop decodes in arrival order:
	// "two", then "zero", then "one", regardless of the pending map.
	stream.feed(t, buildFrame(responseFrame, 2, 0, []byte("two")))
	stream.feed(t, buildFrame(responseFrame, 0, 0, []byte("zero")))
	stream.feed(t, buildFrame(responseFrame, 1, 0, []byte("one")))

	for i := range dones {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	for i, want := range []string{"two", "zero", "one"} {
		if got := <-order; got != want {
			t.Errorf("decoder %d saw %q, want %q", i, got, want)
		}
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("out-of-order responses terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveUnknownIDOpaque pins the README opaque-unmatched rule: a
// response whose request ID has no pending entry is consumed as opaque
// bytes — its exception key and payload are never validated and never reach
// a decoder — and the connection stays active.
func TestReceiveUnknownIDOpaque(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, succeedDispatch)
	dec := &recordingDecoder{}

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 7,
			func() ([]byte, error) { return []byte("req"), nil }, dec.decode)
	}()
	waitBytes(t, stream.bytes, frameHeaderSize+3)

	// An unknown-ID response carrying arbitrary key and payload bytes must
	// not terminate the connection, must not run a decoder, and must not
	// disturb the pending call.
	stream.feed(t, buildFrame(responseFrame, 99, 0xdeadbeef, []byte{0xff, 0x00, 0xfe}))

	stream.feed(t, buildFrame(responseFrame, 0, 0, []byte("ok")))
	if err := <-done; err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if dec.callCount() != 1 {
		t.Errorf("decoder invoked %d times, want exactly once for the matched response", dec.callCount())
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("opaque unmatched response terminated the connection")
	}
	if pendingLen(c) != 0 {
		t.Error("pending entries remain after the matched response")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveDecoderErrorTerminal pins that a decoder error on a matched
// response is terminal: the connection terminates with a cause wrapping
// ErrProtocol, the call completes with that exact permanent cause, and Wait
// reports it.
func TestReceiveDecoderErrorTerminal(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, succeedDispatch)
	decoderErr := errors.New("invalid matched response value")

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 7,
			func() ([]byte, error) { return []byte("req"), nil },
			func(uint64, []byte) error { return decoderErr })
	}()
	waitBytes(t, stream.bytes, frameHeaderSize+3)

	stream.feed(t, buildFrame(responseFrame, 0, 0, []byte{0x01}))
	err := <-done
	if err == nil {
		t.Fatal("call returned nil after a decoder error")
	}
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("call outcome = %v, want a matched-response error wrapping ErrProtocol", err)
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if err != cause {
		t.Errorf("call outcome %v differs from the permanent terminal cause %v", err, cause)
	}
	if pendingLen(c) != 0 {
		t.Error("claimed entry still pending after the decoder error")
	}
	waitTerminal(t, c)
	waitReceiveExit(t, c)
	waitTeardown(t, c)
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
	if got := c.Wait(); got != cause {
		t.Errorf("Wait() = %v, want the exact permanent cause %v", got, cause)
	}
}

// TestReceiveDecoderPanicTerminal pins that a decoder panic on a matched
// response is terminal: the receive loop recovers it, terminates the
// connection with a cause wrapping ErrProtocol, and completes the call with
// that exact cause.
func TestReceiveDecoderPanicTerminal(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, succeedDispatch)

	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 7,
			func() ([]byte, error) { return []byte("req"), nil },
			func(uint64, []byte) error { panic("decoder blew up") })
	}()
	waitBytes(t, stream.bytes, frameHeaderSize+3)

	stream.feed(t, buildFrame(responseFrame, 0, 0, nil))
	err := <-done
	if err == nil {
		t.Fatal("call returned nil after a decoder panic")
	}
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("call outcome = %v, want a matched-response error wrapping ErrProtocol", err)
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if err != cause {
		t.Errorf("call outcome %v differs from the permanent terminal cause %v", err, cause)
	}
	waitTerminal(t, c)
	waitReceiveExit(t, c)
	if got := c.Wait(); got != cause {
		t.Errorf("Wait() = %v, want the exact permanent cause %v", got, cause)
	}
}

// TestReceiveIncomingDuplicateBeforeWrite pins that reuse of an incoming
// request ID before the prior response enters write admission is a
// terminal protocol error: the first request's handler is still inside
// dispatch, the duplicate never reaches dispatch, the first handler
// abandons its response at write admission after terminal selection, and
// Wait reports the exact protocol cause.
func TestReceiveIncomingDuplicateBeforeWrite(t *testing.T) {
	stream := newPipeStream()
	dispatchEntered := make(chan struct{})
	dispatchRelease := make(chan struct{})
	var dispatched atomic.Int32
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		close(dispatchEntered)
		<-dispatchRelease
		return 0, nil
	}
	c := newReceiveTestConn(t, stream, dispatch)

	// The first request's handler is reserved but has not entered write
	// admission: its dispatch is still running.
	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-dispatchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first handler never reached dispatch")
	}

	// Reuse of ID 5 before the prior response enters write admission is a
	// terminal protocol error; the duplicate never reaches dispatch.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))
	waitTerminal(t, c)
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if !errors.Is(cause, ErrProtocol) {
		t.Fatalf("terminal cause = %v, want a protocol error wrapping ErrProtocol", cause)
	}
	if dispatched.Load() != 1 {
		t.Errorf("dispatch ran %d times, want exactly once", dispatched.Load())
	}

	// The first handler finishes dispatch, but terminal state prevents its
	// response write: no bytes are ever written.
	close(dispatchRelease)
	waitReceiveExit(t, c)
	waitTeardown(t, c)
	if got := len(stream.bytes()); got != 0 {
		t.Errorf("handler wrote %d bytes after terminal selection, want none", got)
	}
	if got := c.Wait(); got != cause {
		t.Errorf("Wait() = %v, want the exact protocol cause %v", got, cause)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
	waitIncoming(t, c, 0)
}

// TestReceiveIncomingReuseAfterWrite pins that reuse of an incoming request
// ID after the earlier response write completed is allowed: both requests
// dispatch and both responses are written.
func TestReceiveIncomingReuseAfterWrite(t *testing.T) {
	stream := newPipeStream()
	var dispatched atomic.Int32
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return key, payload
	}
	c := newReceiveTestConn(t, stream, dispatch)

	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("first")))
	waitBytes(t, stream.bytes, frameHeaderSize+5)
	waitIncoming(t, c, 0) // released only after the complete write

	// ID 5 is reusable now that the earlier response write completed.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("second")))
	waitBytes(t, stream.bytes, 2*frameHeaderSize+11)
	waitIncoming(t, c, 0)

	if dispatched.Load() != 2 {
		t.Errorf("dispatch ran %d times, want 2", dispatched.Load())
	}
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 2 {
		t.Fatalf("recorded %d frames, want 2", len(frames))
	}
	for i, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		wantKey := uint64(0x42 + i)
		wantPayload := []byte("first")
		if i == 1 {
			wantPayload = []byte("second")
		}
		if hdr.kind != responseFrame || hdr.requestID != 5 || hdr.key != wantKey || !bytes.Equal(f[frameHeaderSize:], wantPayload) {
			t.Errorf("frame %d = %+v payload %q, want id 5 key %#x payload %q", i, hdr, f[frameHeaderSize:], wantKey, wantPayload)
		}
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("allowed incoming-ID reuse terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveIncomingReuseAtDelivery pins the ordered admission point for
// reuse during an active response write: a controllable stream exposes the
// complete response bytes to the simulated peer while delaying the writer's
// return; the peer immediately sends the reused ID; the duplicate is fully
// buffered as the one deferred next generation without terminating the
// connection, does not dispatch before the write completes, and is admitted
// after the successful write completion even though it arrived before the
// final local Write returned.
func TestReceiveIncomingReuseAtDelivery(t *testing.T) {
	stream := newPipeStream()
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var blocked atomic.Bool
	var peerMu sync.Mutex
	var peerBytes []byte
	stream.write = func(p []byte) (int, error) {
		// The simulated peer accepts the complete frame bytes before the
		// writer's return is delayed.
		peerMu.Lock()
		peerBytes = append(peerBytes, p...)
		peerMu.Unlock()
		if blocked.CompareAndSwap(false, true) {
			close(writeEntered)
			<-writeRelease
		}
		return len(p), nil
	}
	var dispatched atomic.Int32
	c := newReceiveTestConn(t, stream, func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return key, payload
	})

	// The first generation's response write is active: the peer already
	// has the complete first response, but the writer has not returned.
	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first response write never started")
	}
	if got := len(stream.bytes()); got != 0 {
		t.Fatalf("writer returned early: %d bytes recorded", got)
	}
	peerMu.Lock()
	peerSaw := append([]byte(nil), peerBytes...)
	peerMu.Unlock()
	if want := buildFrame(responseFrame, 5, 0x42, []byte("one")); !bytes.Equal(peerSaw, want) {
		t.Fatalf("peer received %d bytes, want the complete first response (%d)", len(peerSaw), len(want))
	}

	// The peer immediately sends the reused ID while the write is active:
	// the duplicate is deferred, never dispatched early, and does not
	// terminate the connection.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))
	if got := dispatched.Load(); got != 1 {
		t.Fatalf("deferred request dispatched before the prior write completed (%d dispatches)", got)
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Fatal("duplicate during an active response write terminated the connection")
	}

	// Successful write completion admits the deferred request even though
	// it arrived before the final local Write returned.
	close(writeRelease)
	waitBytes(t, stream.bytes, 2*(frameHeaderSize+3))
	waitIncoming(t, c, 0)
	if got := dispatched.Load(); got != 2 {
		t.Errorf("dispatch ran %d times, want exactly 2", got)
	}
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 2 {
		t.Fatalf("recorded %d frames, want 2", len(frames))
	}
	for i, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		wantKey := uint64(0x42 + i)
		wantPayload := []byte("one")
		if i == 1 {
			wantPayload = []byte("two")
		}
		if hdr.kind != responseFrame || hdr.requestID != 5 || hdr.key != wantKey || !bytes.Equal(f[frameHeaderSize:], wantPayload) {
			t.Errorf("frame %d = %+v payload %q, want id 5 key %#x payload %q", i, hdr, f[frameHeaderSize:], wantKey, wantPayload)
		}
	}
	c.mu.Lock()
	active = c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("deferred reuse terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveIncomingWriteFailureWithDuplicate pins that a duplicate
// waiting on a response write that then fails is discarded under the exact
// terminal cause and never dispatches: the write failure is the permanent
// cause, the deferred generation is dropped under terminal state, and the
// connection settles with exactly the first dispatch and no frames.
func TestReceiveIncomingWriteFailureWithDuplicate(t *testing.T) {
	writeErr := errors.New("response write failure")
	stream := newPipeStream()
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	stream.write = func(p []byte) (int, error) {
		close(writeEntered)
		<-writeRelease
		return 0, writeErr
	}
	var dispatched atomic.Int32
	c := newReceiveTestConn(t, stream, func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return key, payload
	})

	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first response write never started")
	}

	// The duplicate is deferred while the write is active; the write then
	// fails.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))
	close(writeRelease)
	waitTerminal(t, c)
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if !errors.Is(cause, writeErr) {
		t.Fatalf("terminal cause = %v, want the exact write failure", cause)
	}

	// The deferred duplicate is discarded under the exact terminal cause
	// and never dispatches; nothing was ever written.
	if got := dispatched.Load(); got != 1 {
		t.Errorf("dispatch ran %d times, want exactly once (the deferred request must never dispatch)", got)
	}
	if got := len(stream.bytes()); got != 0 {
		t.Errorf("stream recorded %d bytes, want none", got)
	}
	waitReceiveExit(t, c)
	waitTeardown(t, c)
	if got := c.Wait(); got != cause {
		t.Errorf("Wait() = %v, want the exact write-failure cause %v", got, cause)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
	waitIncoming(t, c, 0)
}

// TestReceiveDeferredReuseDoesNotBlockResponses pins that deferring a
// duplicate does not park the sole receive loop: a matched response placed
// after the deferred duplicate is claimed, decoded, and completed while the
// prior response writer is still blocked, and the deferred generation is
// admitted only after that writer completes.
func TestReceiveDeferredReuseDoesNotBlockResponses(t *testing.T) {
	stream := newPipeStream()
	var dispatched atomic.Int32
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return key, payload
	}
	c := newReceiveTestConn(t, stream, dispatch)

	// One outgoing call whose request frame is on the wire before the
	// first generation's write, so its matched response can be placed
	// right after the deferred duplicate.
	done := make(chan error, 1)
	go func() {
		done <- c.Call(context.Background(), c.imp, 0x10,
			func() ([]byte, error) { return []byte("call"), nil }, okDecoder)
	}()
	waitBytes(t, stream.bytes, frameHeaderSize+4)

	// Install the blocking write hook only after the call's request frame
	// is recorded: it exposes the complete response bytes to the peer
	// while delaying the writer's return.
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var blocked atomic.Bool
	stream.write = func(p []byte) (int, error) {
		if blocked.CompareAndSwap(false, true) {
			close(writeEntered)
			<-writeRelease
		}
		return len(p), nil
	}

	// The first generation (ID 5) writes; a duplicate of ID 5 is deferred
	// while that write is active.
	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first response write never started")
	}
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))

	// The matched response placed after the deferred duplicate is
	// processed before the prior response writer may return: the receive
	// loop did not park on the deferred generation.
	stream.feed(t, buildFrame(responseFrame, 0, 0, nil))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("matched response was not processed while the prior response writer was blocked")
	}
	if got := dispatched.Load(); got != 1 {
		t.Fatalf("deferred request dispatched before the prior write completed (%d dispatches)", got)
	}

	// The prior writer completes; the deferred request is admitted and its
	// response follows on the wire.
	close(writeRelease)
	waitBytes(t, stream.bytes, 2*(frameHeaderSize+3)+frameHeaderSize+4)
	waitIncoming(t, c, 0)
	if got := dispatched.Load(); got != 2 {
		t.Errorf("dispatch ran %d times, want exactly 2", got)
	}
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 3 {
		t.Fatalf("recorded %d frames, want 3", len(frames))
	}
	// The recorded frames are exactly the call's request, the first
	// generation's response, and the deferred generation's response, in
	// that order, never interleaved.
	reqHdr, err := parseFrameHeader(frames[0][:frameHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if reqHdr.kind != requestFrame || reqHdr.requestID != 0 || reqHdr.key != 0x10 || !bytes.Equal(frames[0][frameHeaderSize:], []byte("call")) {
		t.Errorf("frame 0 = %+v payload %q, want the call's request", reqHdr, frames[0][frameHeaderSize:])
	}
	for i, f := range frames[1:] {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		wantKey := uint64(0x42 + i)
		wantPayload := []byte("one")
		if i == 1 {
			wantPayload = []byte("two")
		}
		if hdr.kind != responseFrame || hdr.requestID != 5 || hdr.key != wantKey || !bytes.Equal(f[frameHeaderSize:], wantPayload) {
			t.Errorf("frame %d = %+v payload %q, want id 5 key %#x payload %q", i+1, hdr, f[frameHeaderSize:], wantKey, wantPayload)
		}
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("deferred reuse with a matched response terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestReceiveThirdIncomingGenerationRejected pins that a further same-ID
// request cannot queue while one generation is deferred: the third request
// is a terminal protocol error, the deferred generation never dispatches
// even after the blocked write completes, and the first response remains
// the only frame ever written.
func TestReceiveThirdIncomingGenerationRejected(t *testing.T) {
	stream := newPipeStream()
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var blocked atomic.Bool
	stream.write = func(p []byte) (int, error) {
		if blocked.CompareAndSwap(false, true) {
			close(writeEntered)
			<-writeRelease
		}
		return len(p), nil
	}
	var dispatched atomic.Int32
	c := newReceiveTestConn(t, stream, func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return key, payload
	})

	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first response write never started")
	}
	// The second same-ID request is deferred while the write is active.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))
	// The third same-ID request cannot queue: it is terminal.
	stream.feed(t, buildFrame(requestFrame, 5, 0x44, []byte("three")))
	waitTerminal(t, c)
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if !errors.Is(cause, ErrProtocol) {
		t.Fatalf("terminal cause = %v, want a protocol error wrapping ErrProtocol", cause)
	}
	if got := dispatched.Load(); got != 1 {
		t.Errorf("dispatch ran %d times, want exactly once", got)
	}

	// The blocked write completes and its response is the only frame ever
	// written; the deferred and third generations never dispatch.
	close(writeRelease)
	waitBytes(t, stream.bytes, frameHeaderSize+3)
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want exactly the first response", len(frames))
	}
	hdr, err := parseFrameHeader(frames[0][:frameHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if hdr.kind != responseFrame || hdr.requestID != 5 || hdr.key != 0x42 || !bytes.Equal(frames[0][frameHeaderSize:], []byte("one")) {
		t.Errorf("recorded response = %+v payload %q, want id 5 key 0x42 payload %q", hdr, frames[0][frameHeaderSize:], "one")
	}
	if got := dispatched.Load(); got != 1 {
		t.Errorf("deferred generation dispatched after terminal (%d dispatches)", got)
	}
	waitReceiveExit(t, c)
	waitTeardown(t, c)
	if got := c.Wait(); got != cause {
		t.Errorf("Wait() = %v, want the exact protocol cause %v", got, cause)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
	waitIncoming(t, c, 0)
}

// TestReceiveStopsAfterTerminal pins that terminal selection and request
// admission are one ordered decision: Close wins the race against a request
// frame that is still being buffered, and once the frame is fully buffered
// after terminal has won it is never dispatched — no dispatch and no
// provider launch, and the receive loop stops processing.
func TestReceiveStopsAfterTerminal(t *testing.T) {
	stream := newStagedFrameStream(buildFrame(requestFrame, 9, 0x42, []byte("buffered")))
	t.Cleanup(stream.releasePayload)
	t.Cleanup(stream.releaseReads)
	var dispatched atomic.Int32
	c := newReceiveTestConn(t, stream, func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		return 0, nil
	})

	// The receive loop has consumed the header; the payload read is parked,
	// so the request frame is not yet fully buffered.
	select {
	case <-stream.headerRead:
	case <-time.After(5 * time.Second):
		t.Fatal("receive loop never read the frame header")
	}

	// Close wins the race: terminal is selected while the request frame is
	// still being buffered.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	// The loop finishes buffering the complete frame, then observes
	// terminal at admission: the buffered request is never dispatched and
	// the loop stops processing.
	stream.releasePayload()
	select {
	case <-stream.payloadServed:
	case <-time.After(5 * time.Second):
		t.Fatal("receive loop never finished buffering the frame")
	}
	waitReceiveExit(t, c)
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if got := dispatched.Load(); got != 0 {
		t.Errorf("dispatch ran %d times after terminal selection, want none", got)
	}
	if got := len(stream.bytes()); got != 0 {
		t.Errorf("handler wrote %d bytes after terminal selection, want none", got)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestReceiveTerminalDrainsIncoming pins that teardown leaves no incoming
// or deferred entries, even when a handler ignores cancellation: terminal
// publication drains the reserved ID and the deferred generation under the
// connection lock before Close returns; the blocked write completes and
// discards its deferred generation under terminal state; and the handler
// that ignores cancellation finishes and abandons its response at write
// admission. Only the first generation ever dispatches or writes.
func TestReceiveTerminalDrainsIncoming(t *testing.T) {
	stream := newPipeStream()
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var blocked atomic.Bool
	stream.write = func(p []byte) (int, error) {
		if blocked.CompareAndSwap(false, true) {
			close(writeEntered)
			<-writeRelease
		}
		return len(p), nil
	}
	dispatchEntered := make(chan struct{})
	dispatchRelease := make(chan struct{})
	var dispatched atomic.Int32
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		dispatched.Add(1)
		if key == 0x45 {
			close(dispatchEntered)
			<-dispatchRelease // deliberately ignores handler-context cancellation
		}
		return 0, nil
	}
	c := newReceiveTestConn(t, stream, dispatch)

	// Generation 1 (ID 5): the response write is active and blocked.
	stream.feed(t, buildFrame(requestFrame, 5, 0x42, []byte("one")))
	select {
	case <-writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first response write never started")
	}
	// A duplicate of ID 5 during that write is the one deferred generation.
	stream.feed(t, buildFrame(requestFrame, 5, 0x43, []byte("two")))
	// A fresh request (ID 6) whose provider ignores cancellation.
	stream.feed(t, buildFrame(requestFrame, 6, 0x45, nil))
	select {
	case <-dispatchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("second handler never reached dispatch")
	}

	// Terminal publication drains every incoming and deferred entry under
	// the connection lock before Close returns.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	c.mu.Lock()
	left := len(c.incoming)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d incoming/deferred entries remain after terminal publication, want none", left)
	}

	// The handler that ignores cancellation finishes and abandons its
	// response at write admission; the blocked write completes and its
	// deferred generation is discarded under terminal state.
	close(dispatchRelease)
	close(writeRelease)
	waitReceiveExit(t, c)
	waitTeardown(t, c)
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if got := dispatched.Load(); got != 2 {
		t.Errorf("dispatch ran %d times, want exactly 2 (the deferred request must never dispatch)", got)
	}
	waitBytes(t, stream.bytes, frameHeaderSize)
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want exactly the first response", len(frames))
	}
	hdr, err := parseFrameHeader(frames[0][:frameHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if hdr.kind != responseFrame || hdr.requestID != 5 || hdr.key != 0 || len(frames[0]) != frameHeaderSize {
		t.Errorf("recorded response = %+v with %d-byte payload, want id 5 key 0 with an empty payload", hdr, len(frames[0])-frameHeaderSize)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
	waitIncoming(t, c, 0)
}

// TestHandlerDispatch pins the handler contract: the runtime invokes the
// export dispatch with a bound, uncanceled handler context and the exact key
// and owned payload; the response is fully encoded before the write, the
// incoming ID is released only after the complete write, and the handler
// context is canceled when the handler finishes.
func TestHandlerDispatch(t *testing.T) {
	stream := newPipeStream()
	entered := make(chan struct{})
	release := make(chan struct{})
	var hctx context.Context
	dispatch := func(ctx context.Context, key uint64, payload []byte) (uint64, []byte) {
		hctx = ctx
		close(entered)
		<-release
		if key != 0x42 || string(payload) != "hello" {
			panic("dispatch did not receive the exact key and payload")
		}
		return 0x1234, []byte("world")
	}
	c := newReceiveTestConn(t, stream, dispatch)

	stream.feed(t, buildFrame(requestFrame, 3, 0x42, []byte("hello")))
	<-entered

	// While the handler runs, its context is bound and not canceled.
	got, err := ConnectionFromContext(hctx)
	if err != nil || got != c {
		t.Fatalf("handler context lookup = (%p, %v), want (%p, nil)", got, err, c)
	}
	if hctx.Err() != nil {
		t.Fatal("handler context canceled while the handler ran")
	}

	close(release)
	waitBytes(t, stream.bytes, frameHeaderSize+5)
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 1 {
		t.Fatalf("recorded %d frames, want exactly the response", len(frames))
	}
	hdr, err := parseFrameHeader(frames[0][:frameHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if hdr.kind != responseFrame || hdr.requestID != 3 || hdr.key != 0x1234 {
		t.Errorf("response header = %+v, want responseFrame id 3 key 0x1234", hdr)
	}
	if string(frames[0][frameHeaderSize:]) != "world" {
		t.Errorf("response payload = %q, want %q", frames[0][frameHeaderSize:], "world")
	}

	// The ID is released after the write; the handler context is canceled
	// when the handler finishes.
	waitIncoming(t, c, 0)
	select {
	case <-hctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("handler context not canceled after handler completion")
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("successful dispatch terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestHandlerDispatchPanicRecovery pins the runtime recovery around the
// complete dispatch: a panicking dispatch produces the no-payload
// internal_exception response, does not terminate the connection, and a
// later request is handled normally.
func TestHandlerDispatchPanicRecovery(t *testing.T) {
	stream := newPipeStream()
	dispatch := func(_ context.Context, key uint64, payload []byte) (uint64, []byte) {
		if key == 0x42 {
			panic("provider blew up")
		}
		return 0, nil
	}
	c := newReceiveTestConn(t, stream, dispatch)

	stream.feed(t, buildFrame(requestFrame, 1, 0x42, nil))
	stream.feed(t, buildFrame(requestFrame, 2, 0x43, nil))
	waitBytes(t, stream.bytes, 2*frameHeaderSize)
	waitIncoming(t, c, 0)

	frames := splitFrames(t, stream.bytes())
	if len(frames) != 2 {
		t.Fatalf("recorded %d frames, want 2", len(frames))
	}
	panicResp, okResp := false, false
	for _, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		if hdr.kind != responseFrame {
			t.Errorf("frame header = %+v, want a response", hdr)
			continue
		}
		switch hdr.key {
		case internalExceptionKey:
			panicResp = true
			if len(f) != frameHeaderSize {
				t.Errorf("internal_exception response has a %d-byte payload, want none", len(f)-frameHeaderSize)
			}
		case 0:
			okResp = true
			if len(f) != frameHeaderSize {
				t.Errorf("success response has a %d-byte payload, want none", len(f)-frameHeaderSize)
			}
		default:
			t.Errorf("unexpected response key %#x", hdr.key)
		}
	}
	if !panicResp {
		t.Error("panicking dispatch did not produce an internal_exception response")
	}
	if !okResp {
		t.Error("later request was not handled normally")
	}
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("dispatch panic recovery terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestHandlerIgnoresCancellationCannotWriteAfterTerminal pins that terminal
// state prevents a later response write: a handler whose dispatch ignores
// handler-context cancellation finishes after terminal selection, but its
// response is abandoned at write admission and no bytes are ever written.
func TestHandlerIgnoresCancellationCannotWriteAfterTerminal(t *testing.T) {
	stream := newPipeStream()
	entered := make(chan struct{})
	release := make(chan struct{})
	var hctx context.Context
	dispatch := func(ctx context.Context, key uint64, payload []byte) (uint64, []byte) {
		hctx = ctx
		close(entered)
		<-release // deliberately ignores handler-context cancellation
		return 0, []byte("late")
	}
	c := newReceiveTestConn(t, stream, dispatch)

	stream.feed(t, buildFrame(requestFrame, 7, 0x42, nil))
	<-entered

	c.selectTerminal(ErrClosed)

	// The handler context is canceled at terminal selection even though the
	// dispatch ignores it.
	select {
	case <-hctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("handler context not canceled at terminal selection")
	}

	// The handler finishes and attempts its response write, but terminal
	// state prevents it: no response bytes ever appear.
	close(release)
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		if got := len(stream.bytes()); got != 0 {
			t.Fatalf("handler wrote %d bytes after terminal selection", got)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	waitReceiveExit(t, c)
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestHandlerTerminalRaces stresses handler completion against terminal
// selection: every admitted handler completes its whole response write, a
// handler still at admission abandons, the permanent cause stays exactly the
// selected one, and every recorded byte is one complete response frame.
func TestHandlerTerminalRaces(t *testing.T) {
	for i := 0; i < 25; i++ {
		stream := newPipeStream()
		c := newReceiveTestConn(t, stream, echoDispatch)
		cause := sentinel(fmt.Sprintf("terminal race %d", i))

		const n = 8
		for id := uint64(0); id < n; id++ {
			stream.feed(t, buildFrame(requestFrame, id, id, []byte{byte(id)}))
		}

		c.selectTerminal(cause)
		waitTerminal(t, c)
		waitReceiveExit(t, c)
		if got := c.Wait(); got != cause {
			t.Fatalf("iteration %d: Wait() = %v, want exactly %v", i, got, cause)
		}

		frames := splitFrames(t, stream.bytes())
		for j, f := range frames {
			hdr, err := parseFrameHeader(f[:frameHeaderSize])
			if err != nil {
				t.Fatalf("iteration %d frame %d: %v", i, j, err)
			}
			if hdr.kind != responseFrame || hdr.requestID >= n || hdr.key != hdr.requestID {
				t.Fatalf("iteration %d frame %d: header %+v", i, j, hdr)
			}
			if !bytes.Equal(f[frameHeaderSize:], []byte{byte(hdr.requestID)}) {
				t.Fatalf("iteration %d frame %d: payload %v", i, j, f[frameHeaderSize:])
			}
		}
		if pendingLen(c) != 0 {
			t.Errorf("iteration %d: %d pending entries remain", i, pendingLen(c))
		}
	}
}

// TestReceiveFullDuplex pins the full-duplex lifecycle: outgoing calls and
// incoming requests run concurrently on one connection, share the write
// gate, and never interleave on the wire.
func TestReceiveFullDuplex(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, echoDispatch)

	callPayloads := [][]byte{[]byte("call a"), []byte("call b"), []byte("call c")}
	dones := make([]chan error, len(callPayloads))
	for i := range dones {
		dones[i] = make(chan error, 1)
		go func(i int) {
			dones[i] <- c.Call(context.Background(), c.imp, uint64(0x50+i),
				func() ([]byte, error) { return callPayloads[i], nil }, okDecoder)
		}(i)
	}

	reqPayloads := [][]byte{[]byte("req one"), []byte("req two"), []byte("req three")}
	total := 0
	for i := range reqPayloads {
		stream.feed(t, buildFrame(requestFrame, uint64(20+i), uint64(0x60+i), reqPayloads[i]))
		total += frameHeaderSize + len(callPayloads[i]) + frameHeaderSize + len(reqPayloads[i])
	}
	waitBytes(t, stream.bytes, total)

	// Responses for the outgoing calls arrive in reverse order.
	for i := len(dones) - 1; i >= 0; i-- {
		stream.feed(t, buildFrame(responseFrame, uint64(i), 0, nil))
	}
	for i := range dones {
		if err := <-dones[i]; err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	// Every recorded frame is complete and contiguous: requests and
	// responses never interleave, and each side's frames match exactly.
	frames := splitFrames(t, stream.bytes())
	if len(frames) != 2*len(callPayloads) {
		t.Fatalf("recorded %d frames, want %d", len(frames), 2*len(callPayloads))
	}
	reqFrames, respFrames := 0, 0
	seenReq := make(map[uint64]bool)
	for _, f := range frames {
		hdr, err := parseFrameHeader(f[:frameHeaderSize])
		if err != nil {
			t.Fatal(err)
		}
		payload := f[frameHeaderSize:]
		switch hdr.kind {
		case requestFrame:
			reqFrames++
			// Any call may hold any allocated ID; match by key and payload.
			matched := false
			for j := range callPayloads {
				if hdr.key == uint64(0x50+j) && bytes.Equal(payload, callPayloads[j]) {
					matched = true
					break
				}
			}
			if !matched || hdr.requestID >= uint64(len(callPayloads)) || seenReq[hdr.requestID] {
				t.Errorf("unexpected request frame %+v payload %q", hdr, payload)
			}
			seenReq[hdr.requestID] = true
		case responseFrame:
			respFrames++
			if hdr.requestID < 20 || hdr.requestID >= 20+uint64(len(reqPayloads)) || hdr.key != uint64(0x60+hdr.requestID-20) || !bytes.Equal(payload, reqPayloads[hdr.requestID-20]) {
				t.Errorf("unexpected response frame %+v payload %q", hdr, payload)
			}
		}
	}
	if reqFrames != len(callPayloads) || respFrames != len(reqPayloads) {
		t.Errorf("recorded %d request and %d response frames, want %d and %d",
			reqFrames, respFrames, len(callPayloads), len(reqPayloads))
	}
	if pendingLen(c) != 0 {
		t.Error("pending entries remain after all responses")
	}
	waitIncoming(t, c, 0)
	c.mu.Lock()
	active := c.cause == nil
	c.mu.Unlock()
	if !active {
		t.Error("full-duplex traffic terminated the connection")
	}

	stream.closeWriter()
	waitTerminal(t, c)
}

// TestConnectionCloseNonwaiting pins that Close returns nil without waiting
// for the receive loop: the loop is still parked in its read when Close
// returns, and the permanent cause is exactly ErrClosed.
func TestConnectionCloseNonwaiting(t *testing.T) {
	stream := newGatedStream(io.EOF)
	c, err := NewConnection(context.Background(), stream, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.unblock)
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	waitEntered(t, stream.entered)

	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	// The receive loop is still parked: Close did not wait for it.
	select {
	case <-c.receiveExit:
		t.Fatal("Close waited for the receive loop")
	default:
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if cause != ErrClosed {
		t.Errorf("cause = %v, want exactly ErrClosed", cause)
	}

	// The loop proceeds only when the test unblocks the read; Wait then
	// reports the exact cause.
	stream.unblock()
	waitReceiveExit(t, c)
	if got := c.Wait(); got != ErrClosed {
		t.Errorf("Wait() = %v, want exactly ErrClosed", got)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestWaitWaitsForReceiveLoopAndObserver pins that Wait blocks until the
// receive loop exits and the context observer exits, and returns the exact
// permanent cause: terminal selection alone is not enough while the loop is
// still parked in its read.
func TestWaitWaitsForReceiveLoopAndObserver(t *testing.T) {
	stream := newGatedStream(io.EOF)
	c, err := NewConnection(context.Background(), stream, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.unblock)
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	waitEntered(t, stream.entered)

	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Wait() }()
	c.selectTerminal(ErrClosed)

	// Teardown and observer exit complete, but the receive loop is still
	// parked, so Wait must not return.
	select {
	case err := <-waitDone:
		t.Fatalf("Wait returned %v before the receive loop exited", err)
	case <-time.After(20 * time.Millisecond):
	}

	stream.unblock()
	if err := <-waitDone; err != ErrClosed {
		t.Fatalf("Wait() = %v, want exactly ErrClosed", err)
	}
	select {
	case <-c.receiveExit:
	default:
		t.Error("receive loop not exited when Wait returned")
	}
	select {
	case <-c.observerExit:
	default:
		t.Error("context observer not exited when Wait returned")
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestWaitExactCauseFromEOF pins that an EOF read is the permanent terminal
// cause: Wait returns the exact wrapped EOF value, classified as a transport
// failure rather than a protocol error.
func TestWaitExactCauseFromEOF(t *testing.T) {
	stream := newPipeStream()
	c := newReceiveTestConn(t, stream, succeedDispatch)

	stream.feed(t, buildFrame(requestFrame, 1, 0x42, []byte("ping")))
	waitBytes(t, stream.bytes, frameHeaderSize)
	stream.closeWriter()

	waitTerminal(t, c)
	waitReceiveExit(t, c)
	err := c.Wait()
	if err == nil {
		t.Fatal("Wait() = nil; it must never return nil")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("Wait() = %v, want an EOF transport cause", err)
	}
	if errors.Is(err, ErrProtocol) {
		t.Error("EOF classified as a protocol error")
	}
	c.mu.Lock()
	cause := c.cause
	c.mu.Unlock()
	if err != cause {
		t.Errorf("Wait() %v differs from the permanent cause %v", err, cause)
	}
}

// TestWaitExactCauseFromContextCancellation pins that context cancellation
// terminates the connection and Wait returns exactly context.Canceled after
// the observer and receive loop exit.
func TestWaitExactCauseFromContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := newPipeStream()
	export, err := NewExportBinding(succeedDispatch)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConnection(ctx, stream, export, NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })

	cancel()
	waitTerminal(t, c)
	waitReceiveExit(t, c)
	if err := c.Wait(); err != context.Canceled {
		t.Errorf("Wait() = %v, want exactly context.Canceled", err)
	}
	if stream.closeCount() != 1 {
		t.Errorf("stream closed %d times, want exactly 1", stream.closeCount())
	}
}

// TestConnectionNilReceiver pins that Close and Wait on a nil receiver
// return ErrInvalidArgument.
func TestConnectionNilReceiver(t *testing.T) {
	var c *Connection
	if err := c.Close(); err != ErrInvalidArgument {
		t.Errorf("Close() on nil receiver = %v, want ErrInvalidArgument", err)
	}
	if !errors.Is(c.Close(), ErrInvalidArgument) {
		t.Error("errors.Is(Close on nil receiver, ErrInvalidArgument) = false")
	}
	if err := c.Wait(); err != ErrInvalidArgument {
		t.Errorf("Wait() on nil receiver = %v, want ErrInvalidArgument", err)
	}
	if !errors.Is(c.Wait(), ErrInvalidArgument) {
		t.Error("errors.Is(Wait on nil receiver, ErrInvalidArgument) = false")
	}
}

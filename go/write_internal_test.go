package intercall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// scriptedWriter records every accepted byte and delegates the byte count
// and error to a per-test script.
type scriptedWriter struct {
	mu   sync.Mutex
	buf  []byte
	call func(p []byte) (int, error)
}

func (w *scriptedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.call(p)
	if n >= 0 && n <= len(p) {
		w.buf = append(w.buf, p[:n]...)
	}
	return n, err
}

func (w *scriptedWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf...)
}

// scriptedStream is a ByteStream whose Write behavior is scripted per test.
// Read is inert EOF and Close is a no-op, matching the lifecycle stream
// contract for tests that never read.
type scriptedStream struct {
	write func(p []byte) (int, error)
}

var _ ByteStream = (*scriptedStream)(nil)

func (s *scriptedStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *scriptedStream) Write(p []byte) (int, error) { return s.write(p) }
func (s *scriptedStream) Close() error                { return nil }

// newWriteTestConn constructs an active connection over a scripted stream
// and guarantees terminal selection at test end.
func newWriteTestConn(t *testing.T, s ByteStream) *Connection {
	t.Helper()
	c, err := newConnection(context.Background(), s, newTestExport(t), NewImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.selectTerminal(ErrClosed) })
	return c
}

// TestWriteFullComplete pins the full-write loop: the complete frame is
// accepted whether the writer takes it in one call, in small chunks, or not
// at all for an empty input.
func TestWriteFullComplete(t *testing.T) {
	frame := buildFrame(requestFrame, 1, 2, []byte("full write payload"))

	w := &scriptedWriter{call: func(p []byte) (int, error) { return len(p), nil }}
	if err := writeFull(w, frame); err != nil {
		t.Fatalf("single write: %v", err)
	}
	if got := w.bytes(); !bytes.Equal(got, frame) {
		t.Errorf("single write recorded %x, want %x", got, frame)
	}

	chunked := &scriptedWriter{call: func(p []byte) (int, error) {
		if len(p) > 3 {
			return 3, nil
		}
		return len(p), nil
	}}
	if err := writeFull(chunked, frame); err != nil {
		t.Fatalf("chunked write: %v", err)
	}
	if got := chunked.bytes(); !bytes.Equal(got, frame) {
		t.Errorf("chunked write recorded %x, want %x", got, frame)
	}

	empty := &scriptedWriter{call: func(p []byte) (int, error) { return 0, nil }}
	if err := writeFull(empty, nil); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	if len(empty.bytes()) != 0 {
		t.Error("empty write recorded bytes")
	}
}

// TestWriteFullShortWriteProgress pins that a short write without an error
// is progress: the loop continues until the complete frame is accepted.
func TestWriteFullShortWriteProgress(t *testing.T) {
	frame := buildFrame(requestFrame, 1, 2, []byte("one byte at a time"))
	w := &scriptedWriter{call: func(p []byte) (int, error) { return 1, nil }}
	if err := writeFull(w, frame); err != nil {
		t.Fatal(err)
	}
	if got := w.bytes(); !bytes.Equal(got, frame) {
		t.Errorf("recorded %x, want the complete frame %x", got, frame)
	}
}

// TestWriteFullWriterError pins that any writer error is terminal and
// preserved for errors.Is, including an error after a partial write and an
// error returned together with the full byte count.
func TestWriteFullWriterError(t *testing.T) {
	underlying := errors.New("stream write failure")
	frame := buildFrame(requestFrame, 1, 2, []byte("payload"))
	cases := []struct {
		name string
		call func(p []byte) (int, error)
	}{
		{"zero bytes then error", func(p []byte) (int, error) { return 0, underlying }},
		{"partial then error", func(p []byte) (int, error) { return 3, underlying }},
		{"all bytes then error", func(p []byte) (int, error) { return len(p), underlying }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &scriptedWriter{call: tc.call}
			err := writeFull(w, frame)
			if err == nil {
				t.Fatal("writeFull accepted a failing writer")
			}
			if !errors.Is(err, underlying) {
				t.Errorf("err = %v, want the stream error preserved", err)
			}
			if errors.Is(err, ErrProtocol) {
				t.Error("write failure classified as a protocol error")
			}
		})
	}
}

// TestWriteFullNoProgress pins that a zero-count write without an error is
// terminal immediately, both at the start of a frame and after progress,
// instead of looping forever.
func TestWriteFullNoProgress(t *testing.T) {
	frame := buildFrame(requestFrame, 1, 2, []byte("payload"))

	never := &scriptedWriter{call: func(p []byte) (int, error) { return 0, nil }}
	if err := writeFull(never, frame); !errors.Is(err, errWriteNoProgress) {
		t.Errorf("stalled writer err = %v, want errWriteNoProgress", err)
	}

	var calls int
	after := &scriptedWriter{call: func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return 2, nil
		}
		return 0, nil
	}}
	if err := writeFull(after, frame); !errors.Is(err, errWriteNoProgress) {
		t.Errorf("progress-then-stall err = %v, want errWriteNoProgress", err)
	}
}

// TestWriteFullPartialDiagnostics pins cumulative write accounting: a
// partial-write diagnostic reports the cumulative accepted bytes against
// the original frame size, never the remainder of the failing call, while
// preserving the writer's exact error.
func TestWriteFullPartialDiagnostics(t *testing.T) {
	underlying := errors.New("stream write failure")
	frame := buildFrame(requestFrame, 1, 2, []byte("fragmented payload"))
	wantTotal := len(frame)

	cases := []struct {
		name    string
		call    func(p []byte) (int, error)
		wantMsg string
	}{
		{
			"firstCallPartial",
			func(p []byte) (int, error) { return 3, underlying },
			fmt.Sprintf("partial write after 3 of %d bytes", wantTotal),
		},
		{
			"cumulativeAcrossShortWrites",
			func(p []byte) (int, error) {
				if len(p) == wantTotal {
					return 10, nil
				}
				if len(p) == wantTotal-10 {
					return 5, nil
				}
				return 2, underlying
			},
			fmt.Sprintf("partial write after %d of %d bytes", 17, wantTotal),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &scriptedWriter{call: tc.call}
			err := writeFull(w, frame)
			if err == nil {
				t.Fatal("writeFull accepted a failing writer")
			}
			if !errors.Is(err, underlying) {
				t.Errorf("err = %v, want the stream error preserved", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want message containing %q", err, tc.wantMsg)
			}
			if errors.Is(err, errInvalidWriteCount) || errors.Is(err, errWriteNoProgress) {
				t.Errorf("err = %v, misclassified as a count or progress failure", err)
			}
		})
	}
}

// TestWriteFullInvalidCount pins that an impossible byte count — negative or
// larger than the frame remainder — is terminal and classified as invalid
// even when the writer also returns an error.
func TestWriteFullInvalidCount(t *testing.T) {
	frame := buildFrame(requestFrame, 1, 2, []byte("payload"))
	underlying := errors.New("stream write failure")

	cases := []struct {
		name string
		call func(p []byte) (int, error)
	}{
		{"oversized count", func(p []byte) (int, error) { return len(p) + 1, nil }},
		{"negative count", func(p []byte) (int, error) { return -1, nil }},
		{"oversized count with error", func(p []byte) (int, error) { return len(p) + 1, underlying }},
		{"negative count with error", func(p []byte) (int, error) { return -1, underlying }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &scriptedWriter{call: tc.call}
			if err := writeFull(w, frame); !errors.Is(err, errInvalidWriteCount) {
				t.Errorf("err = %v, want errInvalidWriteCount", err)
			}
		})
	}
}

// TestWriteFrameGateSerializes pins the connection-wide write gate: two
// concurrent frame writes over a one-byte-at-a-time stream never interleave;
// the recorded bytes are exactly one complete frame followed by the other.
func TestWriteFrameGateSerializes(t *testing.T) {
	frames := [][]byte{
		buildFrame(requestFrame, 1, 0x1111, bytes.Repeat([]byte{0xaa}, 64)),
		buildFrame(responseFrame, 2, 0x2222, bytes.Repeat([]byte{0xbb}, 64)),
	}
	var (
		mu       sync.Mutex
		recorded []byte
	)
	stream := &scriptedStream{write: func(p []byte) (int, error) {
		mu.Lock()
		recorded = append(recorded, p[:1]...)
		mu.Unlock()
		runtime.Gosched()
		return 1, nil
	}}
	c := newWriteTestConn(t, stream)

	start := make(chan struct{})
	errs := make([]error, len(frames))
	var wg sync.WaitGroup
	for i := range frames {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = c.writeFrame(frames[i])
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	mu.Lock()
	got := append([]byte(nil), recorded...)
	mu.Unlock()

	joined1 := append(append([]byte(nil), frames[0]...), frames[1]...)
	joined2 := append(append([]byte(nil), frames[1]...), frames[0]...)
	if !bytes.Equal(got, joined1) && !bytes.Equal(got, joined2) {
		t.Errorf("concurrent frames interleaved: got %d bytes, want %x or %x", len(got), joined1, joined2)
	}
}

// TestWriteFrameGateReleasedOnError pins that the write gate is released on
// every path, including a failing write: the next frame still completes.
func TestWriteFrameGateReleasedOnError(t *testing.T) {
	underlying := errors.New("first write fails")
	var calls int
	stream := &scriptedStream{write: func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return 0, underlying
		}
		return len(p), nil
	}}
	c := newWriteTestConn(t, stream)
	frame := buildFrame(requestFrame, 1, 2, []byte("payload"))

	if err := c.writeFrame(frame); !errors.Is(err, underlying) {
		t.Fatalf("first write err = %v, want the stream error", err)
	}
	if err := c.writeFrame(frame); err != nil {
		t.Fatalf("second write after an error: %v; the gate must be released on every path", err)
	}
}

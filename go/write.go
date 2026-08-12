package intercall

import (
	"context"
	"fmt"
	"io"
)

// Private write-gate error classifications, selected when a stream write
// violates the full-write contract in SPEC.md Frame writing and generated
// codecs. Either condition is terminal.
var (
	// errInvalidWriteCount reports a stream write that returned an
	// impossible byte count: negative or larger than the frame remainder.
	errInvalidWriteCount sentinel = "intercall: invalid stream write count"

	// errWriteNoProgress reports a stream write that returned zero bytes
	// with no error, so the frame write made no progress.
	errWriteNoProgress sentinel = "intercall: frame write made no progress"
)

// writeGate is the connection-wide write gate shared by outgoing request
// frames and incoming response frames: exactly one frame write proceeds at a
// time and frames never interleave. It is a one-token channel: acquiring
// sends the single token, releasing receives it, so exactly one goroutine
// holds the gate between acquire and release. The connection state lock is
// never held while waiting for the gate or while calling stream Write or
// Close; the lock order when both are held is gate first, then mu.
type writeGate struct {
	token chan struct{}
}

// newWriteGate returns an unheld connection-wide write gate.
func newWriteGate() writeGate {
	return writeGate{token: make(chan struct{}, 1)}
}

// acquireWriteGate waits until this goroutine holds the connection-wide
// write gate or until an observed condition wins the wait: terminal
// selection and, for outgoing calls, the call context's Done. The
// connection state lock is never held while waiting. On a nil return the
// caller holds the gate and must call releaseWriteGate exactly once; on an
// error return the gate is not held and the error is the exact winner — the
// permanent terminal cause or the call context's exact error. A nil ctx
// disables the context case, so handler gate waits observe terminal
// selection only.
func (c *Connection) acquireWriteGate(ctx context.Context) error {
	select {
	case c.gate.token <- struct{}{}:
		return nil
	case <-c.terminal:
		return c.terminalCause()
	case <-gateCtxDone(ctx):
		return ctx.Err()
	}
}

// releaseWriteGate releases the connection-wide write gate held by this
// goroutine, waking the next gate waiter. The caller must hold the gate,
// proven by a nil return from acquireWriteGate, and must release exactly
// once.
func (c *Connection) releaseWriteGate() {
	<-c.gate.token
}

// gateCtxDone returns ctx.Done(), and nil for a nil context so the select
// case is disabled: a nil ctx means the gate wait observes terminal
// selection only.
func gateCtxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// writeFull writes b to w until the complete frame is accepted or the writer
// reports an error, an invalid byte count, or no progress. It loops over
// short writes, treating a positive count without an error as progress
// toward the full frame. A writer error, an impossible byte count, or a
// zero-count write without an error is terminal; any error after a partial
// frame is terminal too. The impossible-count classification is checked
// before the writer error, so an invalid byte count is reported as such even
// when the writer also returns an error, and partial-write diagnostics
// report the cumulative accepted bytes against the original frame size,
// never the remainder of a single call. The caller holds the
// connection-wide write gate while the frame is written, so frames never
// interleave.
func writeFull(w io.Writer, b []byte) error {
	total := 0
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return fmt.Errorf("intercall: write frame: invalid byte count %d for %d-byte remainder: %w", n, len(b), errInvalidWriteCount)
		}
		if err != nil {
			if n > 0 && n < len(b) {
				return fmt.Errorf("intercall: write frame: partial write after %d of %d bytes: %w", total+n, total+len(b), err)
			}
			return fmt.Errorf("intercall: write frame: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("intercall: write frame: no progress: %w", errWriteNoProgress)
		}
		total += n
		b = b[n:]
	}
	return nil
}

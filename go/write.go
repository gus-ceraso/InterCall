package intercall

import (
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

// writeFull writes b to w until the complete frame is accepted or the writer
// reports an error, an invalid byte count, or no progress. It loops over
// short writes, treating a positive count without an error as progress
// toward the full frame. A writer error, an impossible byte count, or a
// zero-count write without an error is terminal; any error after a partial
// frame is terminal too. The caller holds the connection-wide write gate
// while the frame is written, so frames never interleave.
func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			if n > 0 && n < len(b) {
				return fmt.Errorf("intercall: write frame: partial write after %d of %d bytes: %w", n, len(b), err)
			}
			return fmt.Errorf("intercall: write frame: %w", err)
		}
		if n < 0 || n > len(b) {
			return fmt.Errorf("intercall: write frame: invalid byte count %d for %d-byte remainder: %w", n, len(b), errInvalidWriteCount)
		}
		if n == 0 {
			return fmt.Errorf("intercall: write frame: no progress: %w", errWriteNoProgress)
		}
		b = b[n:]
	}
	return nil
}

// writeFrame writes one complete frame while holding the connection-wide
// write gate, releasing it on every path including errors. The frame must be
// fully built — encoded payload combined with its header — before the gate
// is acquired, so encoding never fails after a frame write begins. A write
// failure is terminal and must be selected as the connection's permanent
// cause by the caller.
func (c *Connection) writeFrame(frame []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFull(c.stream, frame)
}

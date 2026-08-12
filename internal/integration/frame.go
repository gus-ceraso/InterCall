package integration

import (
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// Frame layout mirrors README.md "Frames": every header is exactly 24
// bytes of three little-endian uint64 fields at offsets 0, 8, and 16,
// the most significant bit of the request_id field distinguishes a
// response from a request, and the payload follows the header. These
// helpers are the malformed-stream surface of the harness: the tests
// build request and response frames byte for byte from the test side of
// a stream, including frames no generated codec would ever produce.
const (
	frameHeaderSize = 24
	responseBit     = uint64(1) << 63
	idMask          = uint64(0x7fffffffffffffff)

	// maxRawPayload bounds what readRawFrame allocates for one payload.
	// The connection only ever writes small well-formed frames; a larger
	// wire length in a header the connection produced is a harness bug.
	maxRawPayload = 1 << 20
)

// rawFrame is one frame read from the test side of a stream: the
// request ID with the response bit cleared, the frame's key, and the
// complete owned payload.
type rawFrame struct {
	response bool
	id       uint64
	key      uint64
	payload  []byte
}

// buildRequestFrame assembles one complete request frame.
func buildRequestFrame(id, procedureKey uint64, payload []byte) []byte {
	return buildFrame(false, id, procedureKey, payload)
}

// buildResponseFrame assembles one complete response frame.
func buildResponseFrame(id, exceptionKey uint64, payload []byte) []byte {
	return buildFrame(true, id, exceptionKey, payload)
}

func buildFrame(response bool, id, key uint64, payload []byte) []byte {
	frame := make([]byte, frameHeaderSize+len(payload))
	rawID := id & idMask
	if response {
		rawID |= responseBit
	}
	binary.LittleEndian.PutUint64(frame[0:8], rawID)
	binary.LittleEndian.PutUint64(frame[8:16], key)
	binary.LittleEndian.PutUint64(frame[16:24], uint64(len(payload)))
	copy(frame[frameHeaderSize:], payload)
	return frame
}

// writeRawFrame writes one complete frame from the test side of a
// stream, failing the test on a write error.
func writeRawFrame(t *testing.T, w io.Writer, frame []byte) {
	t.Helper()
	if _, err := w.Write(frame); err != nil {
		t.Fatalf("writing a frame from the test side: %v", err)
	}
}

// readRawFrame performs a full read of one complete frame from the test
// side of a stream. It blocks until a complete frame is available or
// the stream reports an error; an incomplete frame is the reader's
// error.
func readRawFrame(r io.Reader) (rawFrame, error) {
	var hdr [frameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return rawFrame{}, err
	}
	rawID := binary.LittleEndian.Uint64(hdr[0:8])
	length := binary.LittleEndian.Uint64(hdr[16:24])
	if length > maxRawPayload {
		return rawFrame{}, io.ErrUnexpectedEOF
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return rawFrame{}, err
	}
	return rawFrame{
		response: rawID&responseBit != 0,
		id:       rawID & idMask,
		key:      binary.LittleEndian.Uint64(hdr[8:16]),
		payload:  payload,
	}, nil
}

// expectRawFrame reads one complete frame with a failsafe deadline and
// fails the test if the stream closes first or nothing arrives.
func expectRawFrame(t *testing.T, r io.Reader) rawFrame {
	t.Helper()
	type result struct {
		frame rawFrame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := readRawFrame(r)
		ch <- result{f, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("reading a frame from the test side: %v", res.err)
		}
		return res.frame
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for a frame from the connection")
		return rawFrame{}
	}
}

// expectPeerClosed drains the test side of a stream until it reports
// the error of the connection's terminal close, failing the test if no
// error arrives within a failsafe deadline. A frame the connection
// writes before closing is drained silently.
func expectPeerClosed(t *testing.T, r io.Reader) {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		var buf [1]byte
		for {
			if _, err := r.Read(buf[:]); err != nil {
				return
			}
		}
	}()
	select {
	case <-ch:
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for the connection to close its stream end")
	}
}

// Wire value helpers for hand-built payloads. The harness builds the
// arguments of raw requests and decodes the payloads of raw responses
// with these exact README.md encodings: little-endian fixed-width
// integers and a uint64 byte length followed by UTF-8 bytes.

func appendWireUint64(dst []byte, v uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return append(dst, b[:]...)
}

func appendWireUint32(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}

func appendWireString(dst []byte, s string) []byte {
	return append(appendWireUint64(dst, uint64(len(s))), s...)
}

func wireUint64(src []byte) (uint64, []byte, error) {
	if len(src) < 8 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return binary.LittleEndian.Uint64(src[:8]), src[8:], nil
}

func wireUint32(src []byte) (uint32, []byte, error) {
	if len(src) < 4 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return binary.LittleEndian.Uint32(src[:4]), src[4:], nil
}

func wireInt32(src []byte) (int32, []byte, error) {
	v, rest, err := wireUint32(src)
	return int32(v), rest, err
}

func wireString(src []byte) (string, []byte, error) {
	n, rest, err := wireUint64(src)
	if err != nil {
		return "", nil, err
	}
	if n > uint64(len(rest)) {
		return "", nil, io.ErrUnexpectedEOF
	}
	return string(rest[:n]), rest[n:], nil
}

// nan64 is a noncanonical quiet NaN bit pattern: the canonical quiet
// NaN with an extra payload bit set. Decoders must reject it.
const nan64 = 0x7ff8000000000001

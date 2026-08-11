package intercall

import (
	"encoding/binary"
	"fmt"
	"io"
)

// frameKind selects the two frame layouts defined in README.md Frames. The
// most significant bit of the request_id field is clear in a request and set
// in a response; there is no separate frame-kind field on the wire.
type frameKind uint8

const (
	requestFrame frameKind = iota
	responseFrame
)

// Frame layout constants. Every header is exactly 24 bytes: three
// little-endian uint64 fields at offsets 0, 8, and 16 with no padding.
// payloadLength counts only the bytes immediately following the header, so
// value decoding is bounded by it and never consumes bytes from another
// frame.
const (
	frameHeaderSize = 24

	// responseBit is the most significant bit of the request_id field. It is
	// clear in a request and set in a response.
	responseBit uint64 = 1 << 63

	// idMask keeps the 63 request-ID bits below the response bit. A request
	// ID ranges from 0x0000000000000000 through 0x7fffffffffffffff, and a
	// response copies the request's ID into those bits.
	idMask uint64 = 0x7fffffffffffffff

	// maxInt is the largest native int value. Wire lengths are checked
	// against it before conversion, arithmetic, allocation, or iteration, as
	// README.md Failures and Limits requires.
	maxInt = int(^uint(0) >> 1)
)

// frameHeader is one parsed 24-byte frame header.
//
// requestID carries the 63-bit request ID with the response bit already
// cleared, and kind records which layout that bit selected. The key is the
// procedure key for a request and the exception key for a response.
type frameHeader struct {
	kind          frameKind
	requestID     uint64
	key           uint64
	payloadLength uint64
}

// parseFrameHeader decodes exactly 24 bytes as one frame header. The three
// fields are little-endian uint64s at offsets 0, 8, and 16; the most
// significant bit of the request_id field selects the frame layout and is
// cleared in the returned request ID. The wire payload length is validated
// against the native int size before any conversion, allocation, or
// iteration; an impossible length is a protocol error.
func parseFrameHeader(b []byte) (frameHeader, error) {
	if len(b) != frameHeaderSize {
		return frameHeader{}, fmt.Errorf("intercall: frame header is %d bytes, want %d: %w", len(b), frameHeaderSize, ErrProtocol)
	}
	rawID := binary.LittleEndian.Uint64(b[0:8])
	length := binary.LittleEndian.Uint64(b[16:24])
	if length > uint64(maxInt) {
		return frameHeader{}, fmt.Errorf("intercall: frame payload length %d exceeds native int: %w", length, ErrProtocol)
	}
	hdr := frameHeader{
		requestID:     rawID & idMask,
		key:           binary.LittleEndian.Uint64(b[8:16]),
		payloadLength: length,
	}
	if rawID&responseBit != 0 {
		hdr.kind = responseFrame
	} else {
		hdr.kind = requestFrame
	}
	return hdr, nil
}

// readFramePayload performs a full read of exactly length payload bytes into
// a fresh owned buffer that the caller may retain; the runtime never reuses
// a frame buffer. The wire length is revalidated against the native int size
// before conversion or allocation. An impossible length is a protocol error;
// an incomplete payload is a transport failure that wraps the reader's exact
// error.
func readFramePayload(r io.Reader, length uint64) ([]byte, error) {
	if length > uint64(maxInt) {
		return nil, fmt.Errorf("intercall: frame payload length %d exceeds native int: %w", length, ErrProtocol)
	}
	buf := make([]byte, int(length))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("intercall: read frame payload: %w", err)
	}
	return buf, nil
}

// readFrame performs a full read of one complete frame: the 24-byte header
// followed by the complete payload. A header or payload shorter than its
// declared size is a transport failure wrapping the reader's exact error; an
// impossible wire length is a protocol error. The returned payload is owned:
// it is a fresh allocation of exactly the declared length, and the reader's
// position ends exactly at the next frame, never inside this one.
func readFrame(r io.Reader) (frameHeader, []byte, error) {
	var buf [frameHeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return frameHeader{}, nil, fmt.Errorf("intercall: read frame header: %w", err)
	}
	hdr, err := parseFrameHeader(buf[:])
	if err != nil {
		return frameHeader{}, nil, err
	}
	payload, err := readFramePayload(r, hdr.payloadLength)
	if err != nil {
		return frameHeader{}, nil, err
	}
	return hdr, payload, nil
}

// buildFrame builds one complete owned frame: the 24-byte header followed by
// the complete payload, in one contiguous buffer. The request ID is the
// 63-bit ID with the response bit cleared, and the kind selects that bit on
// the wire. The payload is copied, so the returned frame never aliases the
// caller's buffer; mutable provider values are observed in the single
// encoding pass that produced the payload.
func buildFrame(kind frameKind, requestID, key uint64, payload []byte) []byte {
	frame := make([]byte, frameHeaderSize+len(payload))
	rawID := requestID & idMask
	if kind == responseFrame {
		rawID |= responseBit
	}
	binary.LittleEndian.PutUint64(frame[0:8], rawID)
	binary.LittleEndian.PutUint64(frame[8:16], key)
	binary.LittleEndian.PutUint64(frame[16:24], uint64(len(payload)))
	copy(frame[frameHeaderSize:], payload)
	return frame
}

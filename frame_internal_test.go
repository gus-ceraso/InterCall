package intercall

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// chunkReader delivers its data in fixed-size chunks, fragmenting every
// frame across many Read calls.
type chunkReader struct {
	data  []byte
	chunk int
	off   int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data)-r.off {
		n = len(r.data) - r.off
	}
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

// stallingReader delivers its data in chunks but returns (0, nil) once, as
// an io.Reader may occasionally do; full-read semantics must tolerate it.
type stallingReader struct {
	data    []byte
	chunk   int
	off     int
	stalled bool
}

func (r *stallingReader) Read(p []byte) (int, error) {
	if !r.stalled {
		r.stalled = true
		return 0, nil
	}
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data)-r.off {
		n = len(r.data) - r.off
	}
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

// failingReader delivers at most after bytes of data and then reports err.
type failingReader struct {
	data  []byte
	after int
	off   int
	err   error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.off >= r.after {
		return 0, r.err
	}
	n := len(p)
	if n > r.after-r.off {
		n = r.after - r.off
	}
	if n > len(r.data)-r.off {
		n = len(r.data) - r.off
	}
	copy(p, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

// countingReader returns its header exactly once and counts every Read
// call, proving that a rejected frame never causes a further read.
type countingReader struct {
	header []byte
	reads  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 1 {
		return copy(p, r.header), nil
	}
	return 0, io.EOF
}

// TestParseFrameHeaderLayout pins the exact 24-byte layout from README.md
// Frames: three little-endian uint64 fields at offsets 0, 8, and 16 with no
// padding.
func TestParseFrameHeaderLayout(t *testing.T) {
	if frameHeaderSize != 24 {
		t.Fatalf("frameHeaderSize = %d, want exactly 24", frameHeaderSize)
	}
	raw := make([]byte, frameHeaderSize)
	binary.LittleEndian.PutUint64(raw[0:8], 0x0102030405060708)
	binary.LittleEndian.PutUint64(raw[8:16], 0x1122334455667788)
	binary.LittleEndian.PutUint64(raw[16:24], 0x99)
	hdr, err := parseFrameHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.kind != requestFrame {
		t.Errorf("kind = %v, want requestFrame", hdr.kind)
	}
	if hdr.requestID != 0x0102030405060708 {
		t.Errorf("requestID = %#x, want 0x0102030405060708", hdr.requestID)
	}
	if hdr.key != 0x1122334455667788 {
		t.Errorf("key = %#x, want 0x1122334455667788", hdr.key)
	}
	if hdr.payloadLength != 0x99 {
		t.Errorf("payloadLength = %#x, want 0x99", hdr.payloadLength)
	}
}

// TestFrameHeaderResponseBit pins the response-bit contract: the most
// significant bit of the request_id field is clear in a request and set in a
// response, and the remaining 63 bits form the request ID. Parsing clears
// the bit; building sets it from the kind.
func TestFrameHeaderResponseBit(t *testing.T) {
	cases := []struct {
		raw  uint64
		kind frameKind
		id   uint64
	}{
		{0x0000000000000000, requestFrame, 0x0000000000000000},
		{0x0000000000000001, requestFrame, 0x0000000000000001},
		{0x7fffffffffffffff, requestFrame, 0x7fffffffffffffff},
		{0x8000000000000000, responseFrame, 0x0000000000000000},
		{0x8000000000000001, responseFrame, 0x0000000000000001},
		{0xffffffffffffffff, responseFrame, 0x7fffffffffffffff},
	}
	for _, tc := range cases {
		raw := make([]byte, frameHeaderSize)
		binary.LittleEndian.PutUint64(raw[0:8], tc.raw)
		hdr, err := parseFrameHeader(raw)
		if err != nil {
			t.Fatalf("raw %#x: %v", tc.raw, err)
		}
		if hdr.kind != tc.kind {
			t.Errorf("raw %#x: kind = %v, want %v", tc.raw, hdr.kind, tc.kind)
		}
		if hdr.requestID != tc.id {
			t.Errorf("raw %#x: requestID = %#x, want %#x", tc.raw, hdr.requestID, tc.id)
		}
		built := buildFrame(tc.kind, tc.id, 0, nil)
		if got := binary.LittleEndian.Uint64(built[0:8]); got != tc.raw {
			t.Errorf("build(%v, %#x): raw request_id = %#x, want %#x", tc.kind, tc.id, got, tc.raw)
		}
	}
}

// TestParseFrameHeaderWrongSize pins that a header of any size other than
// exactly 24 bytes is a structural protocol error.
func TestParseFrameHeaderWrongSize(t *testing.T) {
	for _, n := range []int{0, 1, 23, 25, 32} {
		_, err := parseFrameHeader(make([]byte, n))
		if !errors.Is(err, ErrProtocol) {
			t.Errorf("parseFrameHeader(%d bytes) err = %v, want ErrProtocol", n, err)
		}
	}
}

// TestFramePayloadLengthNativeCheck pins that a wire payload length is
// validated against the native int size before conversion, allocation, or
// iteration: the largest representable length is accepted by the header
// parse, and one more is a protocol error rejected without touching the
// reader.
func TestFramePayloadLengthNativeCheck(t *testing.T) {
	header := func(length uint64) []byte {
		raw := make([]byte, frameHeaderSize)
		binary.LittleEndian.PutUint64(raw[16:24], length)
		return raw
	}
	if _, err := parseFrameHeader(header(uint64(maxInt))); err != nil {
		t.Errorf("parseFrameHeader(maxInt length) = %v, want nil", err)
	}

	over := uint64(maxInt) + 1
	if _, err := parseFrameHeader(header(over)); !errors.Is(err, ErrProtocol) {
		t.Errorf("parseFrameHeader(over length) = %v, want ErrProtocol", err)
	}

	// readFramePayload must apply the same check before any reader
	// interaction and before allocation.
	r := &countingReader{header: header(over)}
	if _, err := readFramePayload(r, over); !errors.Is(err, ErrProtocol) {
		t.Errorf("readFramePayload(over length) = %v, want ErrProtocol", err)
	}
	if r.reads != 0 {
		t.Errorf("readFramePayload touched the reader %d times, want 0", r.reads)
	}
}

// TestReadFrameFullRead pins full-read semantics: a frame arrives correctly
// no matter how the transport fragments it, including across a zero-byte
// read with no error.
func TestReadFrameFullRead(t *testing.T) {
	frame := buildFrame(requestFrame, 7, 9, []byte("fragmented payload"))
	for _, chunk := range []int{1, 2, 3, 5, 7, 11, 24, 25} {
		t.Run(fmt.Sprintf("chunk%d", chunk), func(t *testing.T) {
			hdr, payload, err := readFrame(&chunkReader{data: frame, chunk: chunk})
			if err != nil {
				t.Fatal(err)
			}
			if hdr.kind != requestFrame || hdr.requestID != 7 || hdr.key != 9 {
				t.Errorf("hdr = %+v, want request 7 key 9", hdr)
			}
			if hdr.payloadLength != uint64(len("fragmented payload")) {
				t.Errorf("payloadLength = %d", hdr.payloadLength)
			}
			if string(payload) != "fragmented payload" {
				t.Errorf("payload = %q", payload)
			}
		})
	}

	hdr, payload, err := readFrame(&stallingReader{data: frame, chunk: 4})
	if err != nil {
		t.Fatal(err)
	}
	if hdr.requestID != 7 || string(payload) != "fragmented payload" {
		t.Errorf("stalled read: hdr = %+v, payload = %q", hdr, payload)
	}
}

// TestReadFrameTruncated pins that an incomplete header is a transport
// failure: io.EOF when no byte is available and io.ErrUnexpectedEOF when
// only part of the 24 bytes arrive.
func TestReadFrameTruncated(t *testing.T) {
	header := buildFrame(requestFrame, 1, 2, []byte("x"))[:frameHeaderSize]
	for n := 0; n < frameHeaderSize; n++ {
		_, _, err := readFrame(bytes.NewReader(header[:n]))
		if err == nil {
			t.Fatalf("%d-byte header accepted", n)
		}
		want := io.EOF
		if n > 0 {
			want = io.ErrUnexpectedEOF
		}
		if !errors.Is(err, want) {
			t.Errorf("%d-byte header: err = %v, want %v", n, err, want)
		}
		if errors.Is(err, ErrProtocol) {
			t.Errorf("%d-byte header: truncation classified as a protocol error", n)
		}
	}
}

// TestReadFrameTruncatedPayload pins that an incomplete payload is a
// transport failure while the declared length is natively representable.
func TestReadFrameTruncatedPayload(t *testing.T) {
	header := buildFrame(requestFrame, 1, 2, make([]byte, 8))[:frameHeaderSize]
	for n := 0; n < 8; n++ {
		input := append(append([]byte(nil), header...), make([]byte, n)...)
		_, _, err := readFrame(bytes.NewReader(input))
		if err == nil {
			t.Fatalf("%d payload bytes accepted for an 8-byte payload", n)
		}
		want := io.EOF
		if n > 0 {
			want = io.ErrUnexpectedEOF
		}
		if !errors.Is(err, want) {
			t.Errorf("%d payload bytes: err = %v, want %v", n, err, want)
		}
	}
}

// TestReadFrameStreamError pins that a stream error during the header or
// payload is a transport failure wrapping the reader's exact error, never a
// protocol error.
func TestReadFrameStreamError(t *testing.T) {
	streamErr := errors.New("stream failure")
	frame := buildFrame(responseFrame, 3, 4, []byte("payload"))
	cases := []struct {
		after  int
		prefix string
	}{
		{0, "read frame header"},
		{5, "read frame header"},
		{24, "read frame payload"},
		{30, "read frame payload"},
	}
	for _, tc := range cases {
		_, _, err := readFrame(&failingReader{data: frame, after: tc.after, err: streamErr})
		if !errors.Is(err, streamErr) {
			t.Errorf("after %d bytes: err = %v, want the stream error", tc.after, err)
		}
		if errors.Is(err, ErrProtocol) {
			t.Errorf("after %d bytes: stream failure classified as a protocol error", tc.after)
		}
		if err == nil || !strings.Contains(err.Error(), tc.prefix) {
			t.Errorf("after %d bytes: err = %v, want operation prefix %q", tc.after, err, tc.prefix)
		}
	}
}

// TestReadFrameOversizedLengthNoRead pins that an oversized wire length is
// rejected as a protocol error immediately after the header, before any
// payload read or allocation.
func TestReadFrameOversizedLengthNoRead(t *testing.T) {
	header := make([]byte, frameHeaderSize)
	binary.LittleEndian.PutUint64(header[16:24], uint64(maxInt)+1)
	r := &countingReader{header: header}
	_, _, err := readFrame(r)
	if !errors.Is(err, ErrProtocol) {
		t.Errorf("err = %v, want ErrProtocol", err)
	}
	if r.reads != 1 {
		t.Errorf("reader was touched %d times, want exactly the single header read", r.reads)
	}
}

// TestReadFrameOwnedPayload pins owned payload buffering: every payload is a
// fresh allocation of exactly the declared length, and consecutive frames
// never share a buffer.
func TestReadFrameOwnedPayload(t *testing.T) {
	first := buildFrame(requestFrame, 1, 2, []byte("first payload"))
	second := buildFrame(responseFrame, 3, 4, []byte("second payload"))
	_, p1, err := readFrame(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	_, p2, err := readFrame(bytes.NewReader(second))
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != len("first payload") || cap(p1) != len(p1) {
		t.Errorf("first payload len/cap = %d/%d, want exact allocation", len(p1), cap(p1))
	}
	if len(p2) != len("second payload") || cap(p2) != len(p2) {
		t.Errorf("second payload len/cap = %d/%d, want exact allocation", len(p2), cap(p2))
	}
	if &p1[0] == &p2[0] {
		t.Error("consecutive frames share one payload buffer")
	}
	if string(p1) != "first payload" || string(p2) != "second payload" {
		t.Errorf("payloads = %q, %q", p1, p2)
	}
}

// TestReadFrameConsumesExactlyPayload pins that reading one frame consumes
// exactly its payload and leaves the next frame's bytes untouched on the
// stream.
func TestReadFrameConsumesExactlyPayload(t *testing.T) {
	f1 := buildFrame(requestFrame, 1, 2, []byte("one"))
	f2 := buildFrame(responseFrame, 3, 4, []byte("two"))
	combined := append(append([]byte(nil), f1...), f2...)
	r := bytes.NewReader(combined)

	hdr1, p1, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if hdr1.requestID != 1 || string(p1) != "one" {
		t.Errorf("first frame = (%+v, %q)", hdr1, p1)
	}
	hdr2, p2, err := readFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	if hdr2.kind != responseFrame || hdr2.requestID != 3 || string(p2) != "two" {
		t.Errorf("second frame = (%+v, %q)", hdr2, p2)
	}
	if r.Len() != 0 {
		t.Errorf("%d bytes remain after two exact frames", r.Len())
	}
}

// TestBuildFrameLayout pins the exact header bytes a builder emits: fields
// at offsets 0, 8, and 16, little-endian, with the response bit selected by
// the kind.
func TestBuildFrameLayout(t *testing.T) {
	frame := buildFrame(requestFrame, 0x0102030405060708, 0x1122334455667788, []byte{1, 2, 3})
	if len(frame) != frameHeaderSize+3 {
		t.Fatalf("frame length = %d, want %d", len(frame), frameHeaderSize+3)
	}
	if got := binary.LittleEndian.Uint64(frame[0:8]); got != 0x0102030405060708 {
		t.Errorf("request_id = %#x", got)
	}
	if got := binary.LittleEndian.Uint64(frame[8:16]); got != 0x1122334455667788 {
		t.Errorf("key = %#x", got)
	}
	if got := binary.LittleEndian.Uint64(frame[16:24]); got != 3 {
		t.Errorf("payload_length = %#x", got)
	}
	if !bytes.Equal(frame[24:], []byte{1, 2, 3}) {
		t.Errorf("payload = %v", frame[24:])
	}

	resp := buildFrame(responseFrame, 1, 0, nil)
	if len(resp) != frameHeaderSize {
		t.Fatalf("empty response frame length = %d, want %d", len(resp), frameHeaderSize)
	}
	if got := binary.LittleEndian.Uint64(resp[0:8]); got != 0x8000000000000001 {
		t.Errorf("response request_id = %#x, want 0x8000000000000001", got)
	}
}

// TestBuildFrameCopiesPayload pins that a built frame owns its payload bytes
// and never aliases the encoder's buffer.
func TestBuildFrameCopiesPayload(t *testing.T) {
	payload := []byte{1, 2, 3}
	frame := buildFrame(requestFrame, 1, 2, payload)
	payload[0] = 99
	if frame[frameHeaderSize] != 1 {
		t.Error("frame aliases the caller's payload buffer")
	}
}

// TestFrameRoundTrip pins build-then-parse round trips for both kinds,
// including the ID-mask property: the parsed ID is the 63-bit ID.
func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		kind    frameKind
		id, key uint64
		payload []byte
	}{
		{requestFrame, 0, 0, nil},
		{requestFrame, 1, 0x0159eb91a98f8f42, []byte{0x34, 0x12}},
		{responseFrame, 0x7fffffffffffffff, 0x583fb304d69368ca, nil},
		{responseFrame, 42, 7, bytes.Repeat([]byte{0xab}, 100)},
	}
	for _, tc := range cases {
		frame := buildFrame(tc.kind, tc.id, tc.key, tc.payload)
		hdr, payload, err := readFrame(bytes.NewReader(frame))
		if err != nil {
			t.Fatalf("(%v, %d): %v", tc.kind, tc.id, err)
		}
		if hdr.kind != tc.kind || hdr.requestID != tc.id || hdr.key != tc.key {
			t.Errorf("(%v, %d): hdr = %+v", tc.kind, tc.id, hdr)
		}
		if hdr.payloadLength != uint64(len(tc.payload)) {
			t.Errorf("(%v, %d): payloadLength = %d, want %d", tc.kind, tc.id, hdr.payloadLength, len(tc.payload))
		}
		if !bytes.Equal(payload, tc.payload) {
			t.Errorf("(%v, %d): payload = %v, want %v", tc.kind, tc.id, payload, tc.payload)
		}
	}
}

// TestFrameREADMEWireExample pins the exact bytes of the README wire example:
// the echo request, its success response, and its no-payload exception
// response.
func TestFrameREADMEWireExample(t *testing.T) {
	request := buildFrame(requestFrame, 1, 0x0159eb91a98f8f42, []byte{0x34, 0x12})
	wantRequest := []byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // request_id
		0x42, 0x8f, 0x8f, 0xa9, 0x91, 0xeb, 0x59, 0x01, // procedure_key
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // payload_length
		0x34, 0x12, // payload
	}
	if !bytes.Equal(request, wantRequest) {
		t.Errorf("request frame = %x, want %x", request, wantRequest)
	}

	success := buildFrame(responseFrame, 1, 0, []byte{0x34, 0x12})
	wantSuccess := []byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, // request_id
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // exception_key
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // payload_length
		0x34, 0x12, // payload
	}
	if !bytes.Equal(success, wantSuccess) {
		t.Errorf("success frame = %x, want %x", success, wantSuccess)
	}

	failed := buildFrame(responseFrame, 1, 0x583fb304d69368ca, nil)
	wantFailed := []byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, // request_id
		0xca, 0x68, 0x93, 0xd6, 0x04, 0xb3, 0x3f, 0x58, // exception_key
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // payload_length
	}
	if !bytes.Equal(failed, wantFailed) {
		t.Errorf("exception frame = %x, want %x", failed, wantFailed)
	}
}

// maxFuzzPayload bounds payload allocation inside FuzzReadFrame so that a
// mutated header declaring a natively representable but enormous length can
// never force a multi-gigabyte allocation during fuzzing.
const maxFuzzPayload = 4096

// FuzzReadFrame exercises the private frame reader with arbitrary bytes.
// Inputs shorter than a header exercise the truncated-input path; a
// header-level protocol error (an oversized wire length) must surface
// without payload reads or allocation; and a natively bounded payload length
// exercises the full read, including truncated and complete payloads. On
// success the returned payload must be an owned exact-length buffer whose
// bytes match the input, proving value decoding never consumes another
// frame.
func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x42, 0x8f, 0x8f, 0xa9, 0x91, 0xeb, 0x59, 0x01,
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x34, 0x12,
	})
	f.Add([]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x34, 0x12,
	})
	f.Add([]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	})
	f.Add([]byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	})
	f.Add([]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80,
	})
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x42, 0x8f, 0x8f})
	f.Add([]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xaa, 0xbb,
	})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < frameHeaderSize {
			_, _, err := readFrame(bytes.NewReader(data))
			if err == nil {
				t.Fatalf("readFrame accepted %d-byte truncated input", len(data))
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("truncated input: err = %v, want EOF or ErrUnexpectedEOF", err)
			}
			return
		}
		hdr, err := parseFrameHeader(data[:frameHeaderSize])
		if err != nil {
			// A header-level protocol error must surface from readFrame
			// without consuming or allocating the payload.
			_, _, got := readFrame(bytes.NewReader(data))
			if !errors.Is(got, ErrProtocol) {
				t.Fatalf("invalid header: readFrame err = %v, want ErrProtocol", got)
			}
			return
		}
		if hdr.payloadLength > maxFuzzPayload {
			// Natively representable but unbounded; never allocate it.
			return
		}
		_, payload, err := readFrame(bytes.NewReader(data))
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("payload read: err = %v, want EOF or ErrUnexpectedEOF", err)
			}
			return
		}
		if uint64(len(payload)) != hdr.payloadLength {
			t.Fatalf("payload length = %d, want %d", len(payload), hdr.payloadLength)
		}
		if !bytes.Equal(payload, data[frameHeaderSize:frameHeaderSize+len(payload)]) {
			t.Fatal("payload bytes differ from the input")
		}
		if cap(payload) != len(payload) {
			t.Fatal("payload buffer is not owned (cap != len)")
		}
	})
}

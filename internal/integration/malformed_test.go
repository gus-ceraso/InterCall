package integration

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cerasos/intercall"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/internal/integration/fixtures/provider"
	"github.com/cerasos/intercall/internal/syntax"
)

// TestMalformed drives one real connection from the test side of its
// stream with raw frames: unknown procedures, malformed arguments,
// provider failures, malformed matched responses versus opaque
// unmatched responses, a duplicate incoming request ID, partial I/O,
// and an impossible wire length. The raw requests are dispatched by the
// checked-in generated export binding exactly like peer traffic; the
// raw responses are decoded by the checked-in generated import binding
// exactly like peer responses.
func TestMalformed(t *testing.T) {
	pingKey := syntax.ProcedureKey("ping")
	echoKey := syntax.ProcedureKey("echo")
	waitKey := syntax.ProcedureKey("wait")
	measureKey := syntax.ProcedureKey("measure")

	// UnknownProcedure: a fully framed unknown key receives the fixed
	// procedure_not_found exception and the connection survives.
	t.Run("UnknownProcedure", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		writeRawFrame(t, peer, buildRequestFrame(3, 0xdeadbeef, nil))
		f := expectRawFrame(t, peer)
		if !f.response || f.id != 3 || f.key != syntax.ExceptionKey("procedure_not_found") || len(f.payload) != 0 {
			t.Fatalf("unknown-procedure response = %+v, want response 3 with an empty procedure_not_found payload", f)
		}
		// The connection survives: a ping round trip still works.
		writeRawFrame(t, peer, buildRequestFrame(4, pingKey, nil))
		f = expectRawFrame(t, peer)
		if !f.response || f.id != 4 || f.key != 0 || len(f.payload) != 0 {
			t.Fatalf("ping response = %+v, want response 4 with an empty success payload", f)
		}
		closeAndWait(t, conn)
	})

	// MalformedArguments: truncated or trailing argument payloads select
	// the fixed invalid_arguments exception without invoking the
	// provider, and the connection survives.
	t.Run("MalformedArguments", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		// A truncated string argument: one length byte where the decoder
		// needs eight.
		writeRawFrame(t, peer, buildRequestFrame(1, echoKey, []byte{0x05}))
		f := expectRawFrame(t, peer)
		if !f.response || f.id != 1 || f.key != syntax.ExceptionKey("invalid_arguments") || len(f.payload) != 0 {
			t.Fatalf("truncated-arguments response = %+v, want response 1 with an empty invalid_arguments payload", f)
		}
		// A valid string argument plus one trailing byte.
		writeRawFrame(t, peer, buildRequestFrame(2, echoKey, append(appendWireString(nil, "hi"), 0x00)))
		f = expectRawFrame(t, peer)
		if !f.response || f.id != 2 || f.key != syntax.ExceptionKey("invalid_arguments") || len(f.payload) != 0 {
			t.Fatalf("trailing-arguments response = %+v, want response 2 with an empty invalid_arguments payload", f)
		}
		// The provider was never invoked with the malformed payloads: a
		// well-formed request right after them succeeds.
		writeRawFrame(t, peer, buildRequestFrame(3, echoKey, appendWireString(nil, "ok")))
		f = expectRawFrame(t, peer)
		if !f.response || f.id != 3 || f.key != 0 {
			t.Fatalf("echo response = %+v, want response 3 with a success key", f)
		}
		if v, rest, err := wireString(f.payload); err != nil || len(rest) != 0 || v != "ok" {
			t.Fatalf("echo payload = %q, %v", v, err)
		}
		closeAndWait(t, conn)
	})

	// ProviderFailures: provider panics and unmatchable errors select
	// the fixed internal_exception, while declared exceptions map to
	// their own keys and payloads.
	t.Run("ProviderFailures", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		internalKey := syntax.ExceptionKey("internal_exception")
		for i, mode := range []string{"panic", "wrapped", "typed_nil", "bad_utf8"} {
			id := uint64(i + 1)
			writeRawFrame(t, peer, buildRequestFrame(id, echoKey, appendWireString(nil, mode)))
			f := expectRawFrame(t, peer)
			if !f.response || f.id != id || f.key != internalKey || len(f.payload) != 0 {
				t.Fatalf("echo(%s) response = %+v, want response %d with an empty internal_exception payload", mode, f, id)
			}
		}
		// The denied sentinel maps to its own no-payload key.
		writeRawFrame(t, peer, buildRequestFrame(5, echoKey, appendWireString(nil, "denied")))
		f := expectRawFrame(t, peer)
		if !f.response || f.id != 5 || f.key != syntax.ExceptionKey("denied") || len(f.payload) != 0 {
			t.Fatalf("echo(denied) response = %+v, want response 5 with an empty denied payload", f)
		}
		// The failed record payload maps to its key with its exact bytes.
		writeRawFrame(t, peer, buildRequestFrame(6, echoKey, appendWireString(nil, "failed")))
		f = expectRawFrame(t, peer)
		if !f.response || f.id != 6 || f.key != syntax.ExceptionKey("failed") {
			t.Fatalf("echo(failed) response = %+v, want response 6 with the failed key", f)
		}
		if code, rest, err := wireInt32(f.payload); err != nil || code != 7 {
			t.Fatalf("failed code = %d, %v", code, err)
		} else if msg, rest, err := wireString(rest); err != nil || len(rest) != 0 || msg != "boom" {
			t.Fatalf("failed message = %q, %v", msg, err)
		}
		// The connection survives.
		writeRawFrame(t, peer, buildRequestFrame(7, pingKey, nil))
		f = expectRawFrame(t, peer)
		if !f.response || f.id != 7 || f.key != 0 {
			t.Fatalf("ping response = %+v, want response 7 with a success key", f)
		}
		closeAndWait(t, conn)
	})

	// MalformedMatchedResponse: a response matching a pending request is
	// malformed when its payload does not decode exactly, and the
	// receiver closes the connection after it. Every defect below is a
	// terminal protocol error.
	t.Run("MalformedMatchedResponse", func(t *testing.T) {
		t.Run("InvalidUTF8", func(t *testing.T) {
			conn, peer := newRawPeer(t)
			done := make(chan error, 1)
			go func() {
				_, err := e2eimport.Echo(bind(conn), "hello")
				done <- err
			}()
			req := expectRawFrame(t, peer)
			if req.response || req.id != 0 || req.key != echoKey {
				t.Fatalf("echo request = %+v, want request 0 with the echo key", req)
			}
			// A success payload whose string body is not valid UTF-8.
			writeRawFrame(t, peer, buildResponseFrame(req.id, 0, append(appendWireUint64(nil, 1), 0xff)))
			assertProtocolTerminal(t, conn, peer, done)
		})
		t.Run("UndeclaredExceptionKey", func(t *testing.T) {
			conn, peer := newRawPeer(t)
			done := make(chan error, 1)
			go func() {
				_, err := e2eimport.Echo(bind(conn), "hello")
				done <- err
			}()
			req := expectRawFrame(t, peer)
			writeRawFrame(t, peer, buildResponseFrame(req.id, 0x0102030405060708, nil))
			assertProtocolTerminal(t, conn, peer, done)
		})
		t.Run("TrailingBytes", func(t *testing.T) {
			conn, peer := newRawPeer(t)
			done := make(chan error, 1)
			go func() {
				_, err := e2eimport.Echo(bind(conn), "hello")
				done <- err
			}()
			req := expectRawFrame(t, peer)
			writeRawFrame(t, peer, buildResponseFrame(req.id, 0, append(appendWireString(nil, "hi"), 0)))
			assertProtocolTerminal(t, conn, peer, done)
		})
		t.Run("NoncanonicalNaN", func(t *testing.T) {
			conn, peer := newRawPeer(t)
			done := make(chan error, 1)
			go func() {
				_, err := e2eimport.Measure(bind(conn), 1, 2)
				done <- err
			}()
			req := expectRawFrame(t, peer)
			if req.response || req.id != 0 || req.key != measureKey {
				t.Fatalf("measure request = %+v, want request 0 with the measure key", req)
			}
			writeRawFrame(t, peer, buildResponseFrame(req.id, 0, appendWireUint64(nil, nan64)))
			assertProtocolTerminal(t, conn, peer, done)
		})
	})

	// OpaqueUnmatchedResponse: a response whose ID does not correspond
	// to a pending request is consumed as opaque bytes and never
	// validated, so the connection survives; the same holds for a
	// duplicate response to a completed request.
	t.Run("OpaqueUnmatchedResponse", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		ctx := bind(conn)
		done := make(chan error, 1)
		go func() { done <- e2eimport.Ping(ctx) }()
		req := expectRawFrame(t, peer)
		if req.response || req.id != 0 || req.key != pingKey {
			t.Fatalf("ping request = %+v, want request 0 with the ping key", req)
		}
		// An unmatched response with an undeclared key and garbage
		// payload must be ignored entirely.
		writeRawFrame(t, peer, buildResponseFrame(req.id+1, 0x0102030405060708, []byte{0xde, 0xad, 0xbe, 0xef}))
		// The correct response still completes the call.
		writeRawFrame(t, peer, buildResponseFrame(req.id, 0, nil))
		if err := <-done; err != nil {
			t.Fatalf("Ping = %v, want nil", err)
		}
		// A duplicate response for the completed request ID is likewise
		// opaque.
		writeRawFrame(t, peer, buildResponseFrame(req.id, 0x0102030405060708, []byte{0xff}))
		// The connection still serves a new call: answer the second ping
		// from the test side.
		ping2 := make(chan error, 1)
		go func() { ping2 <- e2eimport.Ping(ctx) }()
		req2 := expectRawFrame(t, peer)
		if req2.response || req2.id != req.id+1 || req2.key != pingKey {
			t.Fatalf("second ping request = %+v, want request %d with the ping key", req2, req.id+1)
		}
		writeRawFrame(t, peer, buildResponseFrame(req2.id, 0, nil))
		if err := <-ping2; err != nil {
			t.Fatalf("second Ping = %v, want nil", err)
		}
		closeAndWait(t, conn)
	})

	// DuplicateIncomingID: reusing an incoming request ID before the
	// earlier response write completes is a terminal protocol error.
	t.Run("DuplicateIncomingID", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		// The first request blocks its handler in wait(5).
		writeRawFrame(t, peer, buildRequestFrame(0, waitKey, appendWireUint32(nil, 5)))
		eventually(t, "wait 5 to register", func() bool { return provider.IsWaiting(5) })
		// The second request reuses the active ID before any response.
		writeRawFrame(t, peer, buildRequestFrame(0, pingKey, nil))
		if err := conn.Wait(); !errors.Is(err, intercall.ErrProtocol) || !strings.Contains(err.Error(), "already active") {
			t.Fatalf("Wait = %v, want a protocol error for the active incoming ID", err)
		}
		// The blocked handler never writes a response; the stream end
		// closes instead.
		expectPeerClosed(t, peer)
		// Release the blocked handler so its goroutine exits.
		provider.ReleaseWait(5)
	})

	// PartialIO: fragmented delivery is a complete frame, while a
	// truncated header or payload and a clean EOF are transport
	// failures with the reader's exact error.
	t.Run("PartialIO", func(t *testing.T) {
		// A request delivered one byte at a time is still one frame.
		conn, peer := newRawPeer(t)
		frame := buildRequestFrame(1, echoKey, appendWireString(nil, "frag"))
		for i := range frame {
			writeRawFrame(t, peer, frame[i:i+1])
		}
		f := expectRawFrame(t, peer)
		if !f.response || f.id != 1 || f.key != 0 {
			t.Fatalf("fragmented echo response = %+v", f)
		}
		if v, rest, err := wireString(f.payload); err != nil || len(rest) != 0 || v != "frag" {
			t.Fatalf("fragmented echo payload = %q, %v", v, err)
		}
		closeAndWait(t, conn)

		// A truncated header is a transport failure wrapping the
		// reader's exact unexpected-EOF error.
		conn, peer = newRawPeer(t)
		writeRawFrame(t, peer, buildRequestFrame(2, pingKey, nil)[:10])
		if err := peer.Close(); err != nil {
			t.Fatalf("closing the test side: %v", err)
		}
		if err := conn.Wait(); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("truncated header cause = %v, want an io.ErrUnexpectedEOF cause", err)
		}

		// A truncated payload is likewise a transport failure.
		conn, peer = newRawPeer(t)
		partial := buildRequestFrame(3, echoKey, appendWireString(nil, "payload"))
		writeRawFrame(t, peer, partial[:frameHeaderSize+3])
		if err := peer.Close(); err != nil {
			t.Fatalf("closing the test side: %v", err)
		}
		if err := conn.Wait(); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("truncated payload cause = %v, want an io.ErrUnexpectedEOF cause", err)
		}

		// A clean EOF without a partial frame is io.EOF.
		conn, peer = newRawPeer(t)
		if err := peer.Close(); err != nil {
			t.Fatalf("closing the test side: %v", err)
		}
		if err := conn.Wait(); !errors.Is(err, io.EOF) {
			t.Fatalf("clean EOF cause = %v, want an io.EOF cause", err)
		}
	})

	// ImpossiblePayloadLength: a wire length beyond the native int size
	// is a protocol error before any conversion or allocation.
	t.Run("ImpossiblePayloadLength", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		hdr := make([]byte, frameHeaderSize)
		binary.LittleEndian.PutUint64(hdr[0:8], 0)
		binary.LittleEndian.PutUint64(hdr[8:16], pingKey)
		binary.LittleEndian.PutUint64(hdr[16:24], uint64(1)<<63)
		writeRawFrame(t, peer, hdr)
		if err := conn.Wait(); !errors.Is(err, intercall.ErrProtocol) || !strings.Contains(err.Error(), "exceeds native int") {
			t.Fatalf("impossible length cause = %v, want a protocol error", err)
		}
	})

	requireNoLeaks(t)
}

// assertProtocolTerminal drives one malformed-matched-response scenario
// to its terminal state: the pending call completes with the permanent
// protocol cause, Wait reports the same cause, and the connection's
// stream end closes.
func assertProtocolTerminal(t *testing.T, conn *intercall.Connection, peer io.Reader, done <-chan error) {
	t.Helper()
	if err := <-done; !errors.Is(err, intercall.ErrProtocol) {
		t.Fatalf("call error = %v, want an ErrProtocol cause", err)
	}
	if err := conn.Wait(); !errors.Is(err, intercall.ErrProtocol) {
		t.Fatalf("Wait = %v, want an ErrProtocol cause", err)
	}
	expectPeerClosed(t, peer)
}

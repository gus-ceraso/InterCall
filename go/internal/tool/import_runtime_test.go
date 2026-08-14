package tool

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"
	"unicode/utf8"

	"github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/internal/syntax"
	importfixture "github.com/cerasos/intercall/go/internal/tool/importfixture"
)

// This file tests the compiled import fixture as generated code: the
// immutable import binding singleton, the generated callers over a real
// connection and byte stream (context connection lookup, no-closure
// construction on absence, caller result storage, every exception form,
// the fixed sentinel mapping, encoder errors, and malformed-response
// termination), the one canonical chunked base64url semantic constant,
// and the direct response decoder acceptance table.

// exceptionKey returns the exact FNV-0 key of one fixture exception.
func exceptionKey(name string) uint64 { return syntax.ExceptionKey(name) }

// TestImportBindingIdentity verifies the immutable import binding
// singleton: ImportBinding returns the same non-zero handle on every
// call, and independently constructed handles are distinct.
func TestImportBindingIdentity(t *testing.T) {
	a := importfixture.ImportBinding()
	b := importfixture.ImportBinding()
	if a != b {
		t.Fatal("ImportBinding returned different handles on repeated calls")
	}
	if a == (intercall.ImportBinding{}) {
		t.Fatal("the generated import binding handle is zero")
	}
	if a == intercall.NewImportBinding() {
		t.Fatal("an independently constructed import handle is not distinct")
	}
}

// importStream is the client end of an in-memory full-duplex byte
// stream. Reads come from one io.Pipe, writes go to the other, and Close
// unblocks both the connection's receive loop and the test peer.
type importStream struct {
	toPeer   *io.PipeWriter
	fromPeer *io.PipeReader
}

func (s *importStream) Read(p []byte) (int, error)  { return s.fromPeer.Read(p) }
func (s *importStream) Write(p []byte) (int, error) { return s.toPeer.Write(p) }
func (s *importStream) Close() error {
	_ = s.toPeer.Close() // unblocks the peer's pending read
	return s.fromPeer.CloseWithError(io.ErrClosedPipe)
}

// importPeer is the test transport peer: it reads request frames from
// the client's writes and writes response frames back.
type importPeer struct {
	r *io.PipeReader // client-to-peer direction
	w *io.PipeWriter // peer-to-client direction
}

func newImportDuplex() (*importStream, *importPeer) {
	toPeerR, toPeerW := io.Pipe()
	fromPeerR, fromPeerW := io.Pipe()
	return &importStream{toPeer: toPeerW, fromPeer: fromPeerR},
		&importPeer{r: toPeerR, w: fromPeerW}
}

// readImportFrame reads one complete request frame: the 24-byte header
// with full-read semantics and the complete payload.
func readImportFrame(r io.Reader) (id, procKey uint64, payload []byte, err error) {
	var hdr [24]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	id = binary.LittleEndian.Uint64(hdr[0:8]) & 0x7fffffffffffffff
	procKey = binary.LittleEndian.Uint64(hdr[8:16])
	n := binary.LittleEndian.Uint64(hdr[16:24])
	if n > 1<<20 {
		return 0, 0, nil, fmt.Errorf("import test: oversized request payload")
	}
	payload = make([]byte, int(n))
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, 0, nil, err
	}
	return id, procKey, payload, nil
}

// writeImportResponse writes one complete response frame with the
// response bit set on the request ID.
func writeImportResponse(w io.Writer, id, excKey uint64, payload []byte) error {
	var hdr [24]byte
	binary.LittleEndian.PutUint64(hdr[0:8], id|1<<63)
	binary.LittleEndian.PutUint64(hdr[8:16], excKey)
	binary.LittleEndian.PutUint64(hdr[16:24], uint64(len(payload)))
	buf := make([]byte, 0, 24+len(payload))
	buf = append(buf, hdr[:]...)
	buf = append(buf, payload...)
	_, err := w.Write(buf)
	return err
}

// serveImportPeer drives one peer loop until the client closes: each
// request is answered through respond, and every write failure is
// reported on errCh.
func serveImportPeer(peer *importPeer, respond func(procKey uint64, payload []byte) (uint64, []byte), errCh chan<- error) {
	for {
		id, procKey, payload, err := readImportFrame(peer.r)
		if err != nil {
			return // the client closed the stream
		}
		excKey, resp := respond(procKey, payload)
		if err := writeImportResponse(peer.w, id, excKey, resp); err != nil {
			errCh <- err
			return
		}
	}
}

// newImportCallContext constructs one connection over an in-memory
// duplex pair with the fixture's import binding and starts the test
// peer. It returns a context bound to the connection and a cleanup that
// closes the connection, waits for complete teardown, and reports peer
// failures.
func newImportCallContext(t *testing.T, respond func(procKey uint64, payload []byte) (uint64, []byte)) (context.Context, func()) {
	t.Helper()
	client, peer := newImportDuplex()
	export, err := intercall.NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) {
		t.Error("import fixture: the client connection received a request frame")
		return 0, nil
	})
	if err != nil {
		t.Fatalf("NewExportBinding: %v", err)
	}
	conn, err := intercall.NewConnection(context.Background(), client, export, importfixture.ImportBinding())
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	errCh := make(chan error, 1)
	go serveImportPeer(peer, respond, errCh)
	cleanup := func() {
		_ = conn.Close()
		_ = conn.Wait()
		select {
		case err := <-errCh:
			t.Errorf("import test peer: %v", err)
		default:
		}
	}
	return intercall.WithConnection(context.Background(), conn), cleanup
}

// peerError is a responder helper: it records a peer-side failure and
// returns the fixed internal_exception response so the call still
// completes.
func peerError(t *testing.T, format string, args ...any) (uint64, []byte) {
	t.Helper()
	t.Errorf(format, args...)
	return exceptionKey("internal_exception"), nil
}

// mustCodec runs one codec and fails the test on error. Responders run
// on the peer goroutine, so they report through t.Errorf rather than
// aborting.
func mustCodec(t *testing.T, enc []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Errorf("codec: %v", err)
	}
	return enc
}

// mustVec runs one generated encoder at test-body level for a statically
// valid value and panics on error, so table payloads stay single
// expressions. It is never used on the peer goroutine.
func mustVec(enc []byte, err error) []byte {
	if err != nil {
		panic(fmt.Sprintf("test vector codec: %v", err))
	}
	return enc
}

// TestImportCallerNoConnection verifies the generated caller's context
// connection lookup: without a bound connection it returns
// intercall.ErrNoConnection without invoking the request encoder, so an
// invalid parameter value produces the same missing-connection error
// rather than an encoder error. A nil context returns ErrInvalidArgument
// from the runtime lookup.
func TestImportCallerNoConnection(t *testing.T) {
	_, err := importfixture.Echo(context.Background(), "hello")
	if !errors.Is(err, intercall.ErrNoConnection) {
		t.Fatalf("Echo without a connection = %v, want ErrNoConnection", err)
	}
	_, err = importfixture.Echo(context.Background(), "\xff\xfe")
	if !errors.Is(err, intercall.ErrNoConnection) {
		t.Fatalf("Echo without a connection and invalid UTF-8 = %v, want ErrNoConnection (the encoder must not run)", err)
	}
	_, err = importfixture.Echo(nil, "hello")
	if !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("Echo with a nil context = %v, want ErrInvalidArgument", err)
	}
}

// TestImportFixtureRuntime exercises the generated callers over real
// connections: caller result storage, the no-result and zero-width
// paths, every application exception form, the fixed sentinel mapping,
// encoder errors that keep the connection alive, and malformed-response
// termination.
func TestImportFixtureRuntime(t *testing.T) {
	t.Run("echo success", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("echo") {
				return peerError(t, "peer received procedure key %#x, want echo", procKey)
			}
			v, rest, err := importfixture.Codecs.DecodeString(payload)
			if err != nil || len(rest) != 0 {
				return peerError(t, "peer cannot decode the echo request: %v, rest %d", err, len(rest))
			}
			enc, err := importfixture.Codecs.EncodeString(nil, v)
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Echo(ctx, "hello, intercall")
		if err != nil {
			t.Fatalf("Echo = %v", err)
		}
		if out != "hello, intercall" {
			t.Fatalf("Echo stored %q, want the decoded request value", out)
		}
	})

	t.Run("add decodes request", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("add") {
				return peerError(t, "peer received procedure key %#x, want add", procKey)
			}
			a, rest, err := importfixture.Codecs.DecodeInt64(payload)
			if err != nil {
				return peerError(t, "decoding a: %v", err)
			}
			b, rest, err := importfixture.Codecs.DecodeInt64(rest)
			if err != nil || len(rest) != 0 {
				return peerError(t, "decoding b: %v, rest %d", err, len(rest))
			}
			enc, err := importfixture.Codecs.EncodeInt64(nil, a+b)
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Add(ctx, 40, 2)
		if err != nil {
			t.Fatalf("Add = %v", err)
		}
		if out != 42 {
			t.Fatalf("Add = %d, want 42", out)
		}
	})

	t.Run("ping no result", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("ping") || len(payload) != 0 {
				return peerError(t, "peer received a ping request with key %#x and %d payload bytes", procKey, len(payload))
			}
			return 0, nil
		})
		defer cleanup()
		if err := importfixture.Ping(ctx); err != nil {
			t.Fatalf("Ping = %v", err)
		}
	})

	t.Run("paint anonymous records", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("paint") {
				return peerError(t, "peer received procedure key %#x, want paint", procKey)
			}
			origin, rest, err := importfixture.Codecs.DecodePaintOrigin(payload)
			if err != nil {
				return peerError(t, "decoding origin: %v", err)
			}
			size, rest, err := importfixture.Codecs.DecodePaintSize(rest)
			if err != nil || len(rest) != 0 {
				return peerError(t, "decoding size: %v, rest %d", err, len(rest))
			}
			if origin.X != 1 || origin.Y != 2 || size.Width != 3 || size.Height != 4 {
				return peerError(t, "peer decoded origin %+v size %+v", origin, size)
			}
			enc, err := importfixture.Codecs.EncodePaintResult(nil, importfixture.PaintResult{Width: size.Width, Height: size.Height})
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Paint(ctx,
			importfixture.PaintOrigin{X: 1, Y: 2},
			importfixture.PaintSize{Width: 3, Height: 4})
		if err != nil {
			t.Fatalf("Paint = %v", err)
		}
		if out != (importfixture.PaintResult{Width: 3, Height: 4}) {
			t.Fatalf("Paint = %+v, want the decoded result", out)
		}
	})

	t.Run("fetch named types", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("fetch") {
				return peerError(t, "peer received procedure key %#x, want fetch", procKey)
			}
			id, rest, err := importfixture.Codecs.DecodeUserID(payload)
			if err != nil || len(rest) != 0 {
				return peerError(t, "decoding user id: %v, rest %d", err, len(rest))
			}
			if id != 7 {
				return peerError(t, "peer decoded user id %d, want 7", id)
			}
			enc, err := importfixture.Codecs.EncodeBlob(nil, importfixture.Blob{1, 2, 3})
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Fetch(ctx, 7)
		if err != nil {
			t.Fatalf("Fetch = %v", err)
		}
		if !bytesEqual(out, importfixture.Blob{1, 2, 3}) {
			t.Fatalf("Fetch = %v, want the decoded blob", out)
		}
	})

	t.Run("stamp zero-width result", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("stamp") || len(payload) != 0 {
				return peerError(t, "peer received a stamp request with key %#x and %d payload bytes", procKey, len(payload))
			}
			return 0, nil
		})
		defer cleanup()
		if _, err := importfixture.Stamp(ctx); err != nil {
			t.Fatalf("Stamp = %v", err)
		}
	})

	t.Run("load zero-width list", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("load") {
				return peerError(t, "peer received procedure key %#x, want load", procKey)
			}
			count, rest, err := importfixture.Codecs.DecodeUint64(payload)
			if err != nil || len(rest) != 0 {
				return peerError(t, "decoding count: %v, rest %d", err, len(rest))
			}
			if count != 2 {
				return peerError(t, "peer decoded count %d, want 2", count)
			}
			enc, err := importfixture.Codecs.EncodeListEmpty(nil, []importfixture.Empty{{}, {}})
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Load(ctx, 2)
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		if len(out) != 2 || out == nil {
			t.Fatalf("Load = %v, want a nonnil slice of 2 empties", out)
		}
	})

	t.Run("tiny zero-width parameter", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			if procKey != syntax.ProcedureKey("tiny") {
				return peerError(t, "peer received procedure key %#x, want tiny", procKey)
			}
			if len(payload) != 0 {
				return peerError(t, "peer received %d payload bytes for a zero-width parameter, want 0", len(payload))
			}
			enc, err := importfixture.Codecs.EncodeUint32(nil, 9)
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		out, err := importfixture.Tiny(ctx, importfixture.Empty{})
		if err != nil {
			t.Fatalf("Tiny = %v", err)
		}
		if out != 9 {
			t.Fatalf("Tiny = %d, want 9", out)
		}
	})

	t.Run("denied sentinel", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			return exceptionKey("denied"), nil
		})
		defer cleanup()
		out, err := importfixture.Echo(ctx, "x")
		if err != importfixture.Denied {
			t.Fatalf("Echo = %q, %v; want the Denied sentinel", out, err)
		}
		if err.Error() != "denied" {
			t.Fatalf("Denied.Error() = %q, want the exact wire name", err.Error())
		}
		if out != "" {
			t.Fatalf("Echo stored %q on an exception, want the zero result", out)
		}
	})

	t.Run("failed record payload", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			enc, err := importfixture.Codecs.EncodeFailedPayload(nil, importfixture.FailedPayload{Code: 7, Message: "boom"})
			return exceptionKey("failed"), mustCodec(t, enc, err)
		})
		defer cleanup()
		_, err := importfixture.Echo(ctx, "x")
		var fe *importfixture.Failed
		if !errors.As(err, &fe) {
			t.Fatalf("Echo = %v, want *Failed", err)
		}
		if fe.Code != 7 || fe.Message != "boom" {
			t.Fatalf("Failed payload = %+v, want code 7 message boom", fe)
		}
		if err.Error() != "failed" {
			t.Fatalf("Failed.Error() = %q, want the exact wire name", err.Error())
		}
	})

	t.Run("overloaded named payload", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			enc, err := importfixture.Codecs.EncodeNames(nil, importfixture.Names{"a", "b"})
			return exceptionKey("overloaded"), mustCodec(t, enc, err)
		})
		defer cleanup()
		_, err := importfixture.Echo(ctx, "x")
		var oe *importfixture.Overloaded
		if !errors.As(err, &oe) {
			t.Fatalf("Echo = %v, want *Overloaded", err)
		}
		if len(oe.Payload) != 2 || oe.Payload[0] != "a" || oe.Payload[1] != "b" {
			t.Fatalf("Overloaded payload = %v, want the decoded names", oe.Payload)
		}
		if err.Error() != "overloaded" {
			t.Fatalf("Overloaded.Error() = %q, want the exact wire name", err.Error())
		}
	})

	t.Run("fixed sentinel mapping", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			wire string
			want error
		}{
			{"procedure_not_found", "procedure_not_found", intercall.ErrProcedureNotFound},
			{"invalid_arguments", "invalid_arguments", intercall.ErrInvalidArguments},
			{"internal_exception", "internal_exception", intercall.ErrInternalException},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
					return exceptionKey(tc.wire), nil
				})
				defer cleanup()
				_, err := importfixture.Echo(ctx, "x")
				if err != tc.want {
					t.Fatalf("Echo with exception %q = %v, want the root sentinel", tc.wire, err)
				}
				if err.Error() != tc.wire {
					t.Errorf("sentinel %q Error() = %q", tc.wire, err.Error())
				}
			})
		}
	})

	t.Run("encoder error keeps the connection alive", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			v, rest, err := importfixture.Codecs.DecodeString(payload)
			if err != nil || len(rest) != 0 {
				return peerError(t, "peer cannot decode the echo request: %v, rest %d", err, len(rest))
			}
			enc, err := importfixture.Codecs.EncodeString(nil, v)
			return 0, mustCodec(t, enc, err)
		})
		defer cleanup()
		_, err := importfixture.Echo(ctx, "\xff\xfe")
		if !errors.Is(err, importfixture.ErrUTF8) {
			t.Fatalf("Echo with invalid UTF-8 = %v, want the encoder's UTF-8 error", err)
		}
		// The failed encode wrote no frame and allocated no ID: the
		// connection still serves a later call.
		out, err := importfixture.Echo(ctx, "still alive")
		if err != nil || out != "still alive" {
			t.Fatalf("Echo after an encoder error = %q, %v; the connection must stay alive", out, err)
		}
	})

	t.Run("malformed response terminates", func(t *testing.T) {
		ctx, cleanup := newImportCallContext(t, func(procKey uint64, payload []byte) (uint64, []byte) {
			enc, err := importfixture.Codecs.EncodePaintResult(nil, importfixture.PaintResult{Width: 3, Height: 4})
			return 0, append(mustCodec(t, enc, err), 0x00) // one trailing byte
		})
		defer cleanup()
		_, err := importfixture.Paint(ctx,
			importfixture.PaintOrigin{X: 1, Y: 2},
			importfixture.PaintSize{Width: 3, Height: 4})
		if !errors.Is(err, intercall.ErrProtocol) {
			t.Fatalf("Paint with a trailing-byte response = %v, want ErrProtocol", err)
		}
		// The decoder failure selected the permanent terminal cause: a
		// later call returns it without writing.
		_, err = importfixture.Echo(ctx, "x")
		if !errors.Is(err, intercall.ErrProtocol) {
			t.Fatalf("Echo after a matched-response failure = %v, want the terminal ErrProtocol", err)
		}
	})
}

// bytesEqual reports whether two byte slices are equal, nil-safe.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestImportSemanticMetadata verifies the one canonical chunked base64url
// constant of the compiled fixture: canonical unpadded base64url, valid
// UTF-8, a successfully validated decoded interface that matches its
// canonical reformatting byte for byte, and byte equality with the
// canonical body of the fixture source.
func TestImportSemanticMetadata(t *testing.T) {
	payload := importfixture.SemanticPayload()
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("the semantic constant is not base64url: %v", err)
	}
	if base64.RawURLEncoding.EncodeToString(raw) != payload {
		t.Fatal("the semantic constant is not canonical unpadded base64url")
	}
	if !utf8.Valid(raw) {
		t.Fatal("the decoded semantic constant is not valid UTF-8")
	}
	f, err := syntax.Parse("import.intercall", raw)
	if err != nil {
		t.Fatalf("the decoded semantic constant does not parse: %v", err)
	}
	syntax.AttachDocs(f)
	if err := syntax.Validate(f); err != nil {
		t.Fatalf("the decoded semantic constant does not validate: %v", err)
	}
	if formatted := syntax.Format(f); !bytesEqual(formatted, raw) {
		t.Fatal("the decoded semantic constant is not canonical: it differs from its canonical reformatting")
	}
	want := canonicalBodyOf(t, "import.intercall", importFixtureSource)
	if !bytesEqual(raw, want) {
		t.Fatal("the semantic constant does not represent the fixture interface")
	}
}

// TestImportResponseDecoders drives every generated response decoder
// directly: the success arm accepts exactly the encoded result, every
// declared exception arm accepts exactly its encoded payload, and every
// decoder rejects trailing bytes, truncated values, an undeclared key,
// and a nonempty payload for a no-payload exception.
func TestImportResponseDecoders(t *testing.T) {
	denied := exceptionKey("denied")
	failed := exceptionKey("failed")
	overloaded := exceptionKey("overloaded")
	fixedKeys := []uint64{
		exceptionKey("procedure_not_found"),
		exceptionKey("invalid_arguments"),
		exceptionKey("internal_exception"),
	}
	failedPayload := mustVec(importfixture.Codecs.EncodeFailedPayload(nil, importfixture.FailedPayload{Code: 1, Message: "x"}))
	namesPayload := mustVec(importfixture.Codecs.EncodeNames(nil, importfixture.Names{"a"}))

	procs := []struct {
		name    string
		dec     func(key uint64, payload []byte) (any, error, error)
		success []byte
	}{
		{"echo", importfixture.Responses.DecodeEcho, mustVec(importfixture.Codecs.EncodeString(nil, "hi"))},
		{"add", importfixture.Responses.DecodeAdd, mustVec(importfixture.Codecs.EncodeInt64(nil, 5))},
		{"sample", importfixture.Responses.DecodeSample, mustVec(importfixture.Codecs.EncodeBytes(nil, []byte{1, 2}))},
		{"wave", importfixture.Responses.DecodeWave, mustVec(importfixture.Codecs.EncodeUint32(nil, 3))},
		{"paint", importfixture.Responses.DecodePaint, mustVec(importfixture.Codecs.EncodePaintResult(nil, importfixture.PaintResult{Width: 3, Height: 4}))},
		{"locate", importfixture.Responses.DecodeLocate, mustVec(importfixture.Codecs.EncodePoint(nil, importfixture.Point{X: 1, Y: 2}))},
		{"fetch", importfixture.Responses.DecodeFetch, mustVec(importfixture.Codecs.EncodeBlob(nil, importfixture.Blob{9}))},
		{"ping", importfixture.Responses.DecodePing, nil},
		{"stamp", importfixture.Responses.DecodeStamp, nil},
		{"load", importfixture.Responses.DecodeLoad, mustVec(importfixture.Codecs.EncodeListEmpty(nil, []importfixture.Empty{{}, {}}))},
		{"tiny", importfixture.Responses.DecodeTiny, mustVec(importfixture.Codecs.EncodeUint32(nil, 7))},
	}
	for _, p := range procs {
		t.Run(p.name, func(t *testing.T) {
			// Success: the exact encoded result is accepted and stored.
			out, exc, err := p.dec(0, p.success)
			if err != nil || exc != nil {
				t.Fatalf("success: err = %v, exc = %v, want acceptance", err, exc)
			}
			if p.name == "echo" && out != "hi" {
				t.Fatalf("echo stored %v, want the decoded value", out)
			}
			if p.name == "paint" && out != (importfixture.PaintResult{Width: 3, Height: 4}) {
				t.Fatalf("paint stored %v, want the decoded record", out)
			}
			// Trailing bytes after the value are rejected.
			if _, _, err := p.dec(0, append(append([]byte{}, p.success...), 0x00)); err == nil {
				t.Fatal("success with one trailing byte was accepted")
			}
			// Truncated success values are rejected.
			if p.success != nil {
				if _, _, err := p.dec(0, p.success[:len(p.success)-1]); err == nil {
					t.Fatal("a truncated success payload was accepted")
				}
			}
			// The empty payload is only valid for no-result and
			// zero-width procedures.
			_, _, err = p.dec(0, nil)
			if p.name == "ping" || p.name == "stamp" {
				if err != nil {
					t.Fatalf("the empty success payload of %s was rejected: %v", p.name, err)
				}
			} else if err == nil {
				t.Fatalf("%s accepted an empty success payload", p.name)
			}
			// Every declared exception arm accepts exactly its payload.
			if _, exc, err := p.dec(denied, nil); err != nil || exc == nil {
				t.Fatalf("denied: err = %v, exc = %v, want acceptance", err, exc)
			}
			if _, _, err := p.dec(denied, []byte{1}); err == nil {
				t.Fatal("denied with a nonempty payload was accepted")
			}
			if _, exc, err := p.dec(failed, failedPayload); err != nil || exc == nil {
				t.Fatalf("failed: err = %v, exc = %v, want acceptance", err, exc)
			}
			if _, _, err := p.dec(failed, failedPayload[:len(failedPayload)-1]); err == nil {
				t.Fatal("a truncated failed payload was accepted")
			}
			if _, exc, err := p.dec(overloaded, namesPayload); err != nil || exc == nil {
				t.Fatalf("overloaded: err = %v, exc = %v, want acceptance", err, exc)
			}
			for _, k := range fixedKeys {
				if _, exc, err := p.dec(k, nil); err != nil || exc == nil {
					t.Fatalf("fixed key %#x: err = %v, exc = %v, want acceptance", k, err, exc)
				}
				if _, _, err := p.dec(k, []byte{1}); err == nil {
					t.Fatalf("fixed key %#x with a nonempty payload was accepted", k)
				}
			}
			// An undeclared key is rejected.
			if _, _, err := p.dec(0xdeadbeef, nil); err == nil {
				t.Fatal("an undeclared exception key was accepted")
			}
		})
	}
}

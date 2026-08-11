package tool

import (
	"bytes"
	"testing"

	importfixture "github.com/cerasos/intercall/internal/tool/importfixture"
)

// FuzzImportResponseDecoder fuzzes the generated response decoder of the
// paint procedure, the fixture procedure with anonymous inline records
// and every exception form.
//
// The input is one exception key and one response payload. The
// invariants are: no input may make the decoder panic or misbehave; a
// nil decoder result implies the key is zero or a declared exception key
// of the interface; the success arm stores exactly the value encoded by
// the payload (re-encoding it reproduces the payload byte for byte, so
// the decoder consumes the payload exactly); and every accepted
// exception arm stores a nonnil exception.
func FuzzImportResponseDecoder(f *testing.F) {
	paintResult := mustVec(importfixture.Codecs.EncodePaintResult(nil, importfixture.PaintResult{Width: 3, Height: 4}))
	failedPayload := mustVec(importfixture.Codecs.EncodeFailedPayload(nil, importfixture.FailedPayload{Code: 7, Message: "boom"}))
	namesPayload := mustVec(importfixture.Codecs.EncodeNames(nil, importfixture.Names{"a", "b"}))

	// Seed corpus: known-good and hostile inputs for every switch arm.
	for _, seed := range []struct {
		key     uint64
		payload []byte
	}{
		{0, paintResult},
		{0, nil},
		{0, []byte{1, 2, 3}},           // truncated record
		{0, append(paintResult, 0x00)}, // trailing byte
		{exceptionKey("denied"), nil},
		{exceptionKey("denied"), []byte{0}}, // no-payload exception with bytes
		{exceptionKey("failed"), failedPayload},
		{exceptionKey("failed"), failedPayload[:len(failedPayload)-1]}, // truncated payload
		{exceptionKey("overloaded"), namesPayload},
		{exceptionKey("overloaded"), []byte{0xff, 0xff}}, // count exceeds the payload
		{exceptionKey("procedure_not_found"), nil},
		{exceptionKey("invalid_arguments"), nil},
		{exceptionKey("internal_exception"), nil},
		{0xdeadbeef, nil},         // undeclared key
		{0xdeadbeef, paintResult}, // undeclared key with a valid value
	} {
		f.Add(seed.key, seed.payload)
	}

	f.Fuzz(func(t *testing.T, key uint64, payload []byte) {
		out, exc, err := importfixture.Responses.DecodePaint(key, payload)
		if err != nil {
			return
		}
		switch key {
		case 0:
			if exc != nil {
				t.Fatalf("the success arm stored an exception %v", exc)
			}
			pr, ok := out.(importfixture.PaintResult)
			if !ok {
				t.Fatalf("the success arm stored %T, want PaintResult", out)
			}
			enc, err := importfixture.Codecs.EncodePaintResult(nil, pr)
			if err != nil {
				t.Fatalf("re-encoding the decoded result failed: %v", err)
			}
			if !bytes.Equal(enc, payload) {
				t.Fatalf("re-encode % x does not match the accepted payload % x", enc, payload)
			}
		case exceptionKey("denied"), exceptionKey("failed"), exceptionKey("overloaded"),
			exceptionKey("procedure_not_found"), exceptionKey("invalid_arguments"),
			exceptionKey("internal_exception"):
			if exc == nil {
				t.Fatalf("the exception arm for key %#x stored no exception", key)
			}
		default:
			t.Fatalf("the decoder accepted the undeclared exception key %#x", key)
		}
	})
}

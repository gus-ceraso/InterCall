package tool

import (
	"bytes"
	"testing"

	exportfixture "github.com/cerasos/intercall/internal/tool/exportfixture"
)

// FuzzExportRequestDecoder fuzzes the generated request decoder of the
// paint procedure, the fixture procedure with anonymous inline records.
//
// The input is one request payload. The invariants are: no input may
// make the decoder panic or misbehave; a nil decoder error implies the
// payload decoded to exact parameter values whose re-encoding
// reproduces the consumed prefix byte for byte (so the decoder consumes
// exactly one value per parameter and leaves the exact remainder, which
// the dispatch rejects as invalid_arguments); and the decoder never
// consumes more than the payload.
func FuzzExportRequestDecoder(f *testing.F) {
	origin := mustVec(exportfixture.Codecs.EncodePaintOrigin(nil, exportfixture.PaintOrigin{X: 1, Y: 2}))
	size := mustVec(exportfixture.Codecs.EncodePaintSize(nil, exportfixture.PaintSize{Width: 3, Height: 4}))
	valid := append(origin, size...)

	// Seed corpus: known-good and hostile inputs.
	for _, seed := range [][]byte{
		valid,
		nil,
		{},
		{0x00},                                // truncated origin
		origin,                                // missing size
		valid[:len(valid)-1],                  // truncated size
		append(append([]byte{}, valid...), 0), // trailing byte
		{0xff, 0xff, 0xff},                    // huge list-like garbage
		{0x7f, 0x00, 0x00, 0x00},              // negative-ish int32 bytes
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		o, s, rest, err := exportfixture.Requests.DecodePaint(payload)
		if err != nil {
			return
		}
		if len(rest) > len(payload) {
			t.Fatalf("the decoder consumed more than the payload: rest %d of %d", len(rest), len(payload))
		}
		encOrigin, err := exportfixture.Codecs.EncodePaintOrigin(nil, o)
		if err != nil {
			t.Fatalf("re-encoding the decoded origin failed: %v", err)
		}
		encSize, err := exportfixture.Codecs.EncodePaintSize(nil, s)
		if err != nil {
			t.Fatalf("re-encoding the decoded size failed: %v", err)
		}
		consumed := append(encOrigin, encSize...)
		if len(consumed) != len(payload)-len(rest) {
			t.Fatalf("the decoder consumed %d bytes, want %d", len(consumed), len(payload)-len(rest))
		}
		if !bytes.Equal(consumed, payload[:len(consumed)]) {
			t.Fatalf("re-encoding the decoded values does not reproduce the consumed prefix")
		}
	})
}

package tool

import (
	"bytes"
	"testing"

	codecfixture "github.com/cerasos/intercall/go/internal/tool/fixture"
)

// codecPair binds one generated codec pair to the erased signatures the
// fuzz harness drives.
type codecPair struct {
	name string
	enc  func(any) ([]byte, error)
	dec  func([]byte) (any, []byte, error)
}

// fuzzCodecPairs enumerates every generated codec pair of the compiled
// fixture in the same order the vectors test documents.
var fuzzCodecPairs = []codecPair{
	{"int8", func(v any) ([]byte, error) { return codecs.EncodeInt8(nil, v.(int8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt8(b); return v, r, err }},
	{"int16", func(v any) ([]byte, error) { return codecs.EncodeInt16(nil, v.(int16)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt16(b); return v, r, err }},
	{"int32", func(v any) ([]byte, error) { return codecs.EncodeInt32(nil, v.(int32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt32(b); return v, r, err }},
	{"int64", func(v any) ([]byte, error) { return codecs.EncodeInt64(nil, v.(int64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt64(b); return v, r, err }},
	{"uint8", func(v any) ([]byte, error) { return codecs.EncodeUint8(nil, v.(uint8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint8(b); return v, r, err }},
	{"uint16", func(v any) ([]byte, error) { return codecs.EncodeUint16(nil, v.(uint16)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint16(b); return v, r, err }},
	{"uint32", func(v any) ([]byte, error) { return codecs.EncodeUint32(nil, v.(uint32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint32(b); return v, r, err }},
	{"uint64", func(v any) ([]byte, error) { return codecs.EncodeUint64(nil, v.(uint64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint64(b); return v, r, err }},
	{"float32", func(v any) ([]byte, error) { return codecs.EncodeFloat32(nil, v.(float32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFloat32(b); return v, r, err }},
	{"float64", func(v any) ([]byte, error) { return codecs.EncodeFloat64(nil, v.(float64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFloat64(b); return v, r, err }},
	{"string", func(v any) ([]byte, error) { return codecs.EncodeString(nil, v.(string)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeString(b); return v, r, err }},
	{"bytes", func(v any) ([]byte, error) { return codecs.EncodeBytes(nil, v.([]byte)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeBytes(b); return v, r, err }},
	{"user_id", func(v any) ([]byte, error) { return codecs.EncodeUserID(nil, v.(codecfixture.UserID)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUserID(b); return v, r, err }},
	{"name", func(v any) ([]byte, error) { return codecs.EncodeName(nil, v.(codecfixture.Name)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeName(b); return v, r, err }},
	{"point", func(v any) ([]byte, error) { return codecs.EncodePoint(nil, v.(codecfixture.Point)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodePoint(b); return v, r, err }},
	{"pixel", func(v any) ([]byte, error) { return codecs.EncodePixel(nil, v.(codecfixture.Pixel)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodePixel(b); return v, r, err }},
	{"empty", func(v any) ([]byte, error) { return codecs.EncodeEmpty(nil, v.(codecfixture.Empty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeEmpty(b); return v, r, err }},
	{"names", func(v any) ([]byte, error) { return codecs.EncodeNames(nil, v.(codecfixture.Names)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeNames(b); return v, r, err }},
	{"matrix", func(v any) ([]byte, error) { return codecs.EncodeMatrix(nil, v.(codecfixture.Matrix)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeMatrix(b); return v, r, err }},
	{"blob", func(v any) ([]byte, error) { return codecs.EncodeBlob(nil, v.(codecfixture.Blob)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeBlob(b); return v, r, err }},
	{"customer_id", func(v any) ([]byte, error) { return codecs.EncodeCustomerID(nil, v.(codecfixture.CustomerID)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeCustomerID(b); return v, r, err }},
	{"list_int32", func(v any) ([]byte, error) { return codecs.EncodeListInt32(nil, v.([]int32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListInt32(b); return v, r, err }},
	{"failed_payload", func(v any) ([]byte, error) { return codecs.EncodeFailedPayload(nil, v.(codecfixture.FailedPayload)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFailedPayload(b); return v, r, err }},
	{"list_pixel", func(v any) ([]byte, error) { return codecs.EncodeListPixel(nil, v.([]codecfixture.Pixel)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListPixel(b); return v, r, err }},
	{"list_name", func(v any) ([]byte, error) { return codecs.EncodeListName(nil, v.([]codecfixture.Name)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListName(b); return v, r, err }},
	{"list_string", func(v any) ([]byte, error) { return codecs.EncodeListString(nil, v.([]string)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListString(b); return v, r, err }},
	{"record_xy", func(v any) ([]byte, error) { return codecs.EncodeRecordXY(nil, v.(codecfixture.RecordXY)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordXY(b); return v, r, err }},
	{"record_rgba", func(v any) ([]byte, error) { return codecs.EncodeRecordRGBA(nil, v.(codecfixture.RecordRGBA)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordRGBA(b); return v, r, err }},
	{"record_wh", func(v any) ([]byte, error) { return codecs.EncodeRecordWH(nil, v.(codecfixture.RecordWH)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordWH(b); return v, r, err }},
	{"locate_box", func(v any) ([]byte, error) { return codecs.EncodeLocateBox(nil, v.(codecfixture.LocateBox)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeLocateBox(b); return v, r, err }},
	{"locate_result", func(v any) ([]byte, error) { return codecs.EncodeLocateResult(nil, v.(codecfixture.LocateResult)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeLocateResult(b); return v, r, err }},
	{"grid_rows", func(v any) ([]byte, error) { return codecs.EncodeGridRows(nil, v.([]codecfixture.GridRow)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeGridRows(b); return v, r, err }},
	{"grid_row", func(v any) ([]byte, error) { return codecs.EncodeGridRow(nil, v.(codecfixture.GridRow)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeGridRow(b); return v, r, err }},
	{"list_uint8", func(v any) ([]byte, error) { return codecs.EncodeListUint8(nil, v.([]uint8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListUint8(b); return v, r, err }},
	{"record_empty", func(v any) ([]byte, error) { return codecs.EncodeRecordEmpty(nil, v.(codecfixture.RecordEmpty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordEmpty(b); return v, r, err }},
	{"list_empty", func(v any) ([]byte, error) { return codecs.EncodeListEmpty(nil, v.([]codecfixture.Empty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListEmpty(b); return v, r, err }},
	{"tiny_note", func(v any) ([]byte, error) { return codecs.EncodeTinyNote(nil, v.(codecfixture.TinyNote)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeTinyNote(b); return v, r, err }},
}

// FuzzGeneratedDecoder fuzzes every generated bounded decoder of the
// compiled codec codecfixture.
//
// The first byte of an input selects one codec; the remaining bytes are
// that codec's payload. The invariant is that a successful decode is
// byte-exact: re-encoding the decoded value must reproduce exactly the
// consumed prefix of the payload, and no input may make a decoder panic.
// Decoders are also allocation-bounded by construction: declared lengths
// and counts are checked against the payload before any allocation, and
// zero-width lists allocate only their native length.
func FuzzGeneratedDecoder(f *testing.F) {
	// The seed corpus pairs each codec index with valid wire vectors and
	// hostile inputs from the malformed corpus, so every decoder starts
	// from known-good and known-bad prefixes.
	seeds := []struct {
		idx     int
		payload []byte
	}{
		{0, []byte{0x80}},
		{0, nil},
		{1, []byte{0xfe, 0xff}},
		{1, []byte{0x01}},
		{2, []byte{0xfe, 0xff, 0xff, 0xff}},
		{2, []byte{0x04, 0x03, 0x02, 0x01}},
		{2, []byte{0x01, 0x02, 0x03}},
		{3, []byte{0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{3, []byte{0x01}},
		{4, []byte{0xff}},
		{5, []byte{0xcd, 0xab}},
		{6, []byte{0xef, 0xbe, 0xad, 0xde}},
		{6, []byte{0x01}},
		{7, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}},
		{8, []byte{0x00, 0x00, 0x80, 0x3f}},
		{8, []byte{0x01, 0x00, 0xc0, 0x7f}}, // README noncanonical NaN
		{8, []byte{0x00, 0x00, 0xc0, 0x7f}}, // canonical NaN
		{9, []byte{0x9a, 0x99, 0x99, 0x99, 0x99, 0x99, 0xb9, 0x3f}},
		{9, []byte{0x01, 0, 0, 0, 0, 0, 0xf8, 0x7f}},
		{10, append(u64le(3), 'a', 'b', 'c')},
		{10, append(u64le(2), 0xc0, 0xaf)},
		{10, u64le(5)},
		{11, append(u64le(3), 0x00, 0xff, 0x10)},
		{11, u64le(1 << 63)},
		{12, append(u64le(1), 0x2a)},
		{14, append([]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x3f}, 0, 0, 0, 0, 0, 0, 0, 0x40)},
		{15, []byte{1, 2, 3}},
		{15, []byte{1, 2}},
		{17, append(u64le(1), append(u64le(1), 'x')...)},
		{18, append(u64le(1), append(u64le(2), 1, 0, 0, 0, 2, 0, 0, 0)...)},
		{19, append(u64le(2), 1, 2)},
		{21, append(u64le(2), 1, 0, 0, 0, 0xfe, 0xff, 0xff, 0xff)},
		{21, u64le(3)},
		{22, append([]byte{1, 0, 0, 0}, append(u64le(1), 'x')...)},
		{22, append([]byte{1, 0, 0, 0}, u64le(1)...)},
		{23, append(u64le(2), 1, 2, 3, 4, 5, 6)},
		{23, append(u64le(2), 1, 2, 3, 4, 5)},
		{24, append(u64le(1), append(u64le(1), 'x')...)},
		{25, append(u64le(2), append(u64le(1), 'a')...)},
		{25, u64le(2)},
		{26, []byte{1, 0, 0, 0, 2, 0, 0, 0}},
		{26, []byte{1, 2, 3, 4, 5, 6, 7}},
		{27, []byte{1, 2, 3, 4}},
		{28, []byte{1, 0, 0, 0, 2, 0, 0, 0}},
		{29, append([]byte{5, 0, 0, 0, 6, 0, 0, 0}, append(u64le(1), 'z')...)},
		{29, []byte{1, 2, 3, 4, 5, 6, 7}},
		{30, append([]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0, 0, 0, 0, 0, 0, 0, 0x40}, 9, 0, 0, 0, 0, 0, 0, 0)},
		{31, append(u64le(1), 1, 0, 2, 0, 3, 0, 0, 0, 0, 0, 0, 0)},
		{31, append(u64le(1), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11)},
		{32, []byte{1, 0, 2, 0, 3, 0, 0, 0, 0, 0, 0, 0}},
		{33, append(u64le(2), 1, 2)},
		{33, u64le(3)},
		{34, nil},
		{34, []byte{0xde, 0xad}},
		{35, u64le(3)},
		{35, u64le(1 << 63)},
		{36, []byte{7, 0, 0, 0}},
	}
	for _, seed := range seeds {
		in := append([]byte{byte(seed.idx)}, seed.payload...)
		f.Add(in)
	}
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			data = []byte{0}
		}
		pair := fuzzCodecPairs[int(data[0])%len(fuzzCodecPairs)]
		payload := data[1:]
		v, rest, err := pair.dec(payload)
		if err != nil {
			return
		}
		if len(rest) > len(payload) {
			t.Fatalf("%s: decoder returned a remainder longer than its input (%d > %d)", pair.name, len(rest), len(payload))
		}
		out, err := pair.enc(v)
		if err != nil {
			t.Fatalf("%s: re-encoding a successfully decoded value failed: %v", pair.name, err)
		}
		want := payload[:len(payload)-len(rest)]
		if !bytes.Equal(out, want) {
			t.Fatalf("%s: re-encode = % x, want the consumed prefix % x", pair.name, out, want)
		}
	})
}

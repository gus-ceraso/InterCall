package tool

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	codecfixture "github.com/cerasos/intercall/internal/tool/fixture"
)

// codecs is the compiled fixture's exposed codec surface. The bare
// identifier fixture is already taken by a test helper in
// selector_test.go, so the package is imported as codecfixture.
var codecs = codecfixture.Codecs

// u64le renders one little-endian uint64 length or count prefix.
func u64le(n uint64) []byte {
	return []byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24), byte(n >> 32), byte(n >> 40), byte(n >> 48), byte(n >> 56)}
}

// TestGeneratedCodecVectors exercises the compiled fixture codecs against
// exact wire bytes: little-endian integers, canonical NaN output and
// acceptance, UTF-8 strings, bytes, declaration-order records, lists,
// zero-width fast paths, and payload exhaustion.
func TestGeneratedCodecVectors(t *testing.T) {
	t.Run("integers", func(t *testing.T) {
		tests := []struct {
			name string
			enc  func() ([]byte, error)
			dec  func([]byte) (any, []byte, error)
			want []byte
		}{
			{"int8 min", func() ([]byte, error) { return codecs.EncodeInt8(nil, -128) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt8(b); return v, r, err }, []byte{0x80}},
			{"int8 -1", func() ([]byte, error) { return codecs.EncodeInt8(nil, -1) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt8(b); return v, r, err }, []byte{0xff}},
			{"int8 max", func() ([]byte, error) { return codecs.EncodeInt8(nil, 127) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt8(b); return v, r, err }, []byte{0x7f}},
			{"uint8 max", func() ([]byte, error) { return codecs.EncodeUint8(nil, 255) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint8(b); return v, r, err }, []byte{0xff}},
			{"int16 -2", func() ([]byte, error) { return codecs.EncodeInt16(nil, -2) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt16(b); return v, r, err }, []byte{0xfe, 0xff}},
			{"int16 258", func() ([]byte, error) { return codecs.EncodeInt16(nil, 258) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt16(b); return v, r, err }, []byte{0x02, 0x01}},
			{"int16 min", func() ([]byte, error) { return codecs.EncodeInt16(nil, -32768) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt16(b); return v, r, err }, []byte{0x00, 0x80}},
			{"uint16 0xabcd", func() ([]byte, error) { return codecs.EncodeUint16(nil, 0xabcd) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint16(b); return v, r, err }, []byte{0xcd, 0xab}},
			{"int32 -2", func() ([]byte, error) { return codecs.EncodeInt32(nil, -2) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt32(b); return v, r, err }, []byte{0xfe, 0xff, 0xff, 0xff}},
			{"int32 0x01020304", func() ([]byte, error) { return codecs.EncodeInt32(nil, 0x01020304) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt32(b); return v, r, err }, []byte{0x04, 0x03, 0x02, 0x01}},
			{"int32 min", func() ([]byte, error) { return codecs.EncodeInt32(nil, -2147483648) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt32(b); return v, r, err }, []byte{0x00, 0x00, 0x00, 0x80}},
			{"uint32 0xdeadbeef", func() ([]byte, error) { return codecs.EncodeUint32(nil, 0xdeadbeef) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint32(b); return v, r, err }, []byte{0xef, 0xbe, 0xad, 0xde}},
			{"int64 -2", func() ([]byte, error) { return codecs.EncodeInt64(nil, -2) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt64(b); return v, r, err }, []byte{0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
			{"int64 0x0102030405060708", func() ([]byte, error) { return codecs.EncodeInt64(nil, 0x0102030405060708) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt64(b); return v, r, err }, []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}},
			{"uint64 0x0102030405060708", func() ([]byte, error) { return codecs.EncodeUint64(nil, 0x0102030405060708) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint64(b); return v, r, err }, []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}},
			{"user_id 42", func() ([]byte, error) { return codecs.EncodeUserID(nil, 42) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUserID(b); return v, r, err }, append([]byte{0x2a}, 0, 0, 0, 0, 0, 0, 0)},
			{"customer_id 7", func() ([]byte, error) { return codecs.EncodeCustomerID(nil, 7) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeCustomerID(b); return v, r, err }, append([]byte{0x07}, 0, 0, 0, 0, 0, 0, 0)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := tt.enc()
				if err != nil {
					t.Fatalf("encode: %v", err)
				}
				if !bytes.Equal(got, tt.want) {
					t.Fatalf("encode = % x, want % x", got, tt.want)
				}
				v, rest, err := tt.dec(got)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(rest) != 0 {
					t.Fatalf("decode left %d trailing bytes", len(rest))
				}
				if v == nil {
					t.Fatal("decode returned a nil value")
				}
			})
		}
	})

	t.Run("floats", func(t *testing.T) {
		tests := []struct {
			name string
			v    float64
			want []byte
		}{
			{"float32 1.0", float64(float32(1.0)), []byte{0x00, 0x00, 0x80, 0x3f}},
			{"float32 -2.5", float64(float32(-2.5)), []byte{0x00, 0x00, 0x20, 0xc0}},
			{"float32 0.1", float64(float32(0.1)), []byte{0xcd, 0xcc, 0xcc, 0x3d}},
			{"float32 +inf", math.Inf(1), []byte{0x00, 0x00, 0x80, 0x7f}},
			{"float32 -inf", math.Inf(-1), []byte{0x00, 0x00, 0x80, 0xff}},
			{"float32 -0", math.Copysign(0, -1), []byte{0x00, 0x00, 0x00, 0x80}},
			{"float32 NaN canonical", math.NaN(), []byte{0x00, 0x00, 0xc0, 0x7f}},
			{"float64 1.0", 1.0, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f}},
			{"float64 -2.5", -2.5, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xc0}},
			{"float64 0.1", 0.1, []byte{0x9a, 0x99, 0x99, 0x99, 0x99, 0x99, 0xb9, 0x3f}},
			{"float64 +inf", math.Inf(1), []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x7f}},
			{"float64 -inf", math.Inf(-1), []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xff}},
			{"float64 -0", math.Copysign(0, -1), []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80}},
			{"float64 NaN canonical", math.NaN(), []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var got []byte
				var err error
				if len(tt.want) == 4 {
					got, err = codecs.EncodeFloat32(nil, float32(tt.v))
				} else {
					got, err = codecs.EncodeFloat64(nil, tt.v)
				}
				if err != nil {
					t.Fatalf("encode: %v", err)
				}
				if !bytes.Equal(got, tt.want) {
					t.Fatalf("encode = % x, want % x", got, tt.want)
				}
				// The canonical NaN bit pattern decodes to NaN and
				// re-encodes to the same canonical bytes.
				if len(tt.want) == 4 {
					v, rest, err := codecs.DecodeFloat32(got)
					if err != nil {
						t.Fatalf("decode: %v", err)
					}
					if len(rest) != 0 {
						t.Fatalf("decode left %d trailing bytes", len(rest))
					}
					if math.IsNaN(float64(v)) != math.IsNaN(tt.v) {
						t.Fatalf("decode = %v, NaN-ness want %v", v, math.IsNaN(tt.v))
					}
					out, err := codecs.EncodeFloat32(nil, v)
					if err != nil {
						t.Fatalf("re-encode: %v", err)
					}
					if !bytes.Equal(out, tt.want) {
						t.Fatalf("re-encode = % x, want % x", out, tt.want)
					}
				} else {
					v, rest, err := codecs.DecodeFloat64(got)
					if err != nil {
						t.Fatalf("decode: %v", err)
					}
					if len(rest) != 0 {
						t.Fatalf("decode left %d trailing bytes", len(rest))
					}
					if math.IsNaN(v) != math.IsNaN(tt.v) {
						t.Fatalf("decode = %v, NaN-ness want %v", v, math.IsNaN(tt.v))
					}
					if !math.IsNaN(tt.v) && v != tt.v {
						t.Fatalf("decode = %v, want %v", v, tt.v)
					}
					out, err := codecs.EncodeFloat64(nil, v)
					if err != nil {
						t.Fatalf("re-encode: %v", err)
					}
					if !bytes.Equal(out, tt.want) {
						t.Fatalf("re-encode = % x, want % x", out, tt.want)
					}
				}
			})
		}
	})

	t.Run("strings", func(t *testing.T) {
		tests := []struct {
			name string
			v    string
			want []byte
		}{
			{"empty", "", u64le(0)},
			{"ascii", "a", append(u64le(1), 'a')},
			{"utf8", "héllo", append(u64le(6), []byte("héllo")...)},
			{"name", "ab", append(u64le(2), 'a', 'b')},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var enc func() ([]byte, error)
				var dec func([]byte) (any, []byte, error)
				if tt.name == "name" {
					enc = func() ([]byte, error) { return codecs.EncodeName(nil, codecfixture.Name(tt.v)) }
					dec = func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeName(b); return v, r, err }
				} else {
					enc = func() ([]byte, error) { return codecs.EncodeString(nil, tt.v) }
					dec = func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeString(b); return v, r, err }
				}
				got, err := enc()
				if err != nil {
					t.Fatalf("encode: %v", err)
				}
				if !bytes.Equal(got, tt.want) {
					t.Fatalf("encode = % x, want % x", got, tt.want)
				}
				v, rest, err := dec(got)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(rest) != 0 {
					t.Fatalf("decode left %d trailing bytes", len(rest))
				}
				if tt.name == "name" {
					if v.(codecfixture.Name) != codecfixture.Name(tt.v) {
						t.Fatalf("decode = %q, want %q", v, tt.v)
					}
				} else if v.(string) != tt.v {
					t.Fatalf("decode = %q, want %q", v, tt.v)
				}
			})
		}
		// Encoders reject Go strings that are not valid UTF-8.
		if _, err := codecs.EncodeString(nil, string([]byte{'a', 0xff})); err != codecfixture.ErrUTF8 {
			t.Fatalf("EncodeString(invalid UTF-8) = %v, want %v", err, codecfixture.ErrUTF8)
		}
		if _, err := codecs.EncodeName(nil, codecfixture.Name(string([]byte{0xc3, 0x28}))); err != codecfixture.ErrUTF8 {
			t.Fatalf("EncodeName(invalid UTF-8) = %v, want %v", err, codecfixture.ErrUTF8)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		content := []byte{0x00, 0xff, 0x10}
		got, err := codecs.EncodeBytes(nil, content)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if want := append(u64le(3), content...); !bytes.Equal(got, want) {
			t.Fatalf("encode = % x, want % x", got, want)
		}
		v, rest, err := codecs.DecodeBytes(got)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(rest) != 0 {
			t.Fatalf("decode left %d trailing bytes", len(rest))
		}
		if !bytes.Equal(v, content) {
			t.Fatalf("decode = % x, want % x", v, content)
		}
		// The decoded slice is owned: mutating the input must not change it.
		got[9] = 0x99
		if v[0] != content[0] {
			t.Fatalf("decoded bytes alias the input buffer: got % x", v)
		}
		// Named bytes and the empty value round trip.
		blob, err := codecs.EncodeBlob(nil, codecfixture.Blob(content))
		if err != nil {
			t.Fatalf("EncodeBlob: %v", err)
		}
		bv, rest, err := codecs.DecodeBlob(blob)
		if err != nil || len(rest) != 0 || !bytes.Equal(bv, codecfixture.Blob(content)) {
			t.Fatalf("Blob round trip: v=% x rest=%d err=%v", bv, len(rest), err)
		}
		empty, err := codecs.EncodeBytes(nil, nil)
		if err != nil {
			t.Fatalf("EncodeBytes(nil): %v", err)
		}
		if !bytes.Equal(empty, u64le(0)) {
			t.Fatalf("EncodeBytes(nil) = % x, want % x", empty, u64le(0))
		}
		ev, rest, err := codecs.DecodeBytes(empty)
		if err != nil || len(rest) != 0 || ev == nil || len(ev) != 0 {
			t.Fatalf("empty bytes decode: nonnil=%v len=%d rest=%d err=%v", ev != nil, len(ev), len(rest), err)
		}
	})

	t.Run("records", func(t *testing.T) {
		// Declaration order without padding: x then y.
		p, err := codecs.EncodePoint(nil, codecfixture.Point{X: 1, Y: 2})
		if err != nil {
			t.Fatalf("EncodePoint: %v", err)
		}
		want := append([]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x3f}, 0, 0, 0, 0, 0, 0, 0, 0x40)
		if !bytes.Equal(p, want) {
			t.Fatalf("EncodePoint = % x, want % x", p, want)
		}
		pv, rest, err := codecs.DecodePoint(p)
		if err != nil || len(rest) != 0 || pv != (codecfixture.Point{X: 1, Y: 2}) {
			t.Fatalf("Point round trip: v=%+v rest=%d err=%v", pv, len(rest), err)
		}
		// Field order: red, green, blue.
		px, err := codecs.EncodePixel(nil, codecfixture.Pixel{Red: 1, Green: 2, Blue: 3})
		if err != nil {
			t.Fatalf("EncodePixel: %v", err)
		}
		if !bytes.Equal(px, []byte{1, 2, 3}) {
			t.Fatalf("EncodePixel = % x, want 01 02 03", px)
		}
		// Anonymous records: paint origin and nested locate box.
		xy, err := codecs.EncodeRecordXY(nil, codecfixture.RecordXY{X: 1, Y: 2})
		if err != nil {
			t.Fatalf("EncodeRecordXY: %v", err)
		}
		if !bytes.Equal(xy, []byte{1, 0, 0, 0, 2, 0, 0, 0}) {
			t.Fatalf("EncodeRecordXY = % x, want 01 00 00 00 02 00 00 00", xy)
		}
		xyv, rest, err := codecs.DecodeRecordXY(xy)
		if err != nil || len(rest) != 0 || xyv != (codecfixture.RecordXY{X: 1, Y: 2}) {
			t.Fatalf("RecordXY round trip: v=%+v rest=%d err=%v", xyv, len(rest), err)
		}
		box, err := codecs.EncodeLocateBox(nil, codecfixture.LocateBox{Corner: codecfixture.RecordXY{X: 5, Y: 6}, Label: "z"})
		if err != nil {
			t.Fatalf("EncodeLocateBox: %v", err)
		}
		wantBox := append([]byte{5, 0, 0, 0, 6, 0, 0, 0}, append(u64le(1), 'z')...)
		if !bytes.Equal(box, wantBox) {
			t.Fatalf("EncodeLocateBox = % x, want % x", box, wantBox)
		}
		bv, rest, err := codecs.DecodeLocateBox(box)
		if err != nil || len(rest) != 0 || bv != (codecfixture.LocateBox{Corner: codecfixture.RecordXY{X: 5, Y: 6}, Label: "z"}) {
			t.Fatalf("LocateBox round trip: v=%+v rest=%d err=%v", bv, len(rest), err)
		}
		// A record with a zero-width field encodes only the non-zero fields.
		note, err := codecs.EncodeTinyNote(nil, codecfixture.TinyNote{Tag: codecfixture.Empty{}, Code: 7})
		if err != nil {
			t.Fatalf("EncodeTinyNote: %v", err)
		}
		if !bytes.Equal(note, []byte{7, 0, 0, 0}) {
			t.Fatalf("EncodeTinyNote = % x, want 07 00 00 00", note)
		}
		nv, rest, err := codecs.DecodeTinyNote(note)
		if err != nil || len(rest) != 0 || nv.Code != 7 {
			t.Fatalf("TinyNote round trip: v=%+v rest=%d err=%v", nv, len(rest), err)
		}
		// Exception payload record.
		fp, err := codecs.EncodeFailedPayload(nil, codecfixture.FailedPayload{Code: 1, Message: "x"})
		if err != nil {
			t.Fatalf("EncodeFailedPayload: %v", err)
		}
		wantFP := append([]byte{1, 0, 0, 0}, append(u64le(1), 'x')...)
		if !bytes.Equal(fp, wantFP) {
			t.Fatalf("EncodeFailedPayload = % x, want % x", fp, wantFP)
		}
		fv, rest, err := codecs.DecodeFailedPayload(fp)
		if err != nil || len(rest) != 0 || fv != (codecfixture.FailedPayload{Code: 1, Message: "x"}) {
			t.Fatalf("FailedPayload round trip: v=%+v rest=%d err=%v", fv, len(rest), err)
		}
	})

	t.Run("lists", func(t *testing.T) {
		// A list carries its element count, then consecutive elements.
		li, err := codecs.EncodeListInt32(nil, []int32{1, -2, 3})
		if err != nil {
			t.Fatalf("EncodeListInt32: %v", err)
		}
		want := append(u64le(3), 1, 0, 0, 0, 0xfe, 0xff, 0xff, 0xff, 3, 0, 0, 0)
		if !bytes.Equal(li, want) {
			t.Fatalf("EncodeListInt32 = % x, want % x", li, want)
		}
		lv, rest, err := codecs.DecodeListInt32(li)
		if err != nil || len(rest) != 0 || !reflect.DeepEqual(lv, []int32{1, -2, 3}) {
			t.Fatalf("ListInt32 round trip: v=%v rest=%d err=%v", lv, len(rest), err)
		}
		// List of named records, list of named strings, list of strings.
		lp, err := codecs.EncodeListPixel(nil, []codecfixture.Pixel{{Red: 1, Green: 2, Blue: 3}, {Red: 4, Green: 5, Blue: 6}})
		if err != nil {
			t.Fatalf("EncodeListPixel: %v", err)
		}
		if want := append(u64le(2), 1, 2, 3, 4, 5, 6); !bytes.Equal(lp, want) {
			t.Fatalf("EncodeListPixel = % x, want % x", lp, want)
		}
		ln, err := codecs.EncodeListName(nil, []codecfixture.Name{"x"})
		if err != nil {
			t.Fatalf("EncodeListName: %v", err)
		}
		if want := append(u64le(1), append(u64le(1), 'x')...); !bytes.Equal(ln, want) {
			t.Fatalf("EncodeListName = % x, want % x", ln, want)
		}
		ls, err := codecs.EncodeListString(nil, []string{"a", "bb"})
		if err != nil {
			t.Fatalf("EncodeListString: %v", err)
		}
		wantLS := append(u64le(2), append(u64le(1), 'a')...)
		wantLS = append(wantLS, append(u64le(2), 'b', 'b')...)
		if !bytes.Equal(ls, wantLS) {
			t.Fatalf("EncodeListString = % x, want % x", ls, wantLS)
		}
		sv, rest, err := codecs.DecodeListString(ls)
		if err != nil || len(rest) != 0 || !reflect.DeepEqual(sv, []string{"a", "bb"}) {
			t.Fatalf("ListString round trip: v=%v rest=%d err=%v", sv, len(rest), err)
		}
		// List of anonymous records with fixed-width fields.
		gr, err := codecs.EncodeGridRows(nil, []codecfixture.GridRow{{Row: 1, Col: 2, Value: 3}, {Row: 4, Col: 5, Value: 6}})
		if err != nil {
			t.Fatalf("EncodeGridRows: %v", err)
		}
		wantGR := append(u64le(2),
			1, 0, 2, 0, 3, 0, 0, 0, 0, 0, 0, 0,
			4, 0, 5, 0, 6, 0, 0, 0, 0, 0, 0, 0)
		if !bytes.Equal(gr, wantGR) {
			t.Fatalf("EncodeGridRows = % x, want % x", gr, wantGR)
		}
		gv, rest, err := codecs.DecodeGridRows(gr)
		if err != nil || len(rest) != 0 || !reflect.DeepEqual(gv, []codecfixture.GridRow{{Row: 1, Col: 2, Value: 3}, {Row: 4, Col: 5, Value: 6}}) {
			t.Fatalf("GridRows round trip: v=%+v rest=%d err=%v", gv, len(rest), err)
		}
		// Nested lists: matrix of int32.
		mx, err := codecs.EncodeMatrix(nil, codecfixture.Matrix{{1, 2}, {3}})
		if err != nil {
			t.Fatalf("EncodeMatrix: %v", err)
		}
		wantMX := append(u64le(2),
			byte(2), 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0,
			byte(1), 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0)
		if !bytes.Equal(mx, wantMX) {
			t.Fatalf("EncodeMatrix = % x, want % x", mx, wantMX)
		}
		mv, rest, err := codecs.DecodeMatrix(mx)
		if err != nil || len(rest) != 0 || !reflect.DeepEqual(mv, codecfixture.Matrix{{1, 2}, {3}}) {
			t.Fatalf("Matrix round trip: v=%v rest=%d err=%v", mv, len(rest), err)
		}
		// list uint8 keeps the []uint8 spelling distinct from bytes.
		wu, err := codecs.EncodeListUint8(nil, []uint8{1, 2})
		if err != nil {
			t.Fatalf("EncodeListUint8: %v", err)
		}
		if want := append(u64le(2), 1, 2); !bytes.Equal(wu, want) {
			t.Fatalf("EncodeListUint8 = % x, want % x", wu, want)
		}
		// Nonnil empty decoded slices for every list shape: decoding a
		// zero count produces a nonnil zero-length slice.
		for _, tt := range []struct {
			name string
			in   func() []byte
			dec  func([]byte) error
		}{
			{"int32", func() []byte { b, _ := codecs.EncodeListInt32(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeListInt32(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"pixel", func() []byte { b, _ := codecs.EncodeListPixel(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeListPixel(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"string", func() []byte { b, _ := codecs.EncodeListString(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeListString(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"name", func() []byte { b, _ := codecs.EncodeListName(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeListName(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"names", func() []byte { b, _ := codecs.EncodeNames(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeNames(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"matrix", func() []byte { b, _ := codecs.EncodeMatrix(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeMatrix(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"grid", func() []byte { b, _ := codecs.EncodeGridRows(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeGridRows(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
			{"empty", func() []byte { b, _ := codecs.EncodeListEmpty(nil, nil); return b }, func(b []byte) error {
				v, rest, err := codecs.DecodeListEmpty(b)
				if err == nil && len(rest) == 0 && v == nil {
					t.Fatal("nil slice")
				}
				return err
			}},
		} {
			if err := tt.dec(tt.in()); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
		}
	})

	t.Run("zero-width", func(t *testing.T) {
		// record {} occupies zero bytes in every position.
		re, err := codecs.EncodeRecordEmpty(nil, codecfixture.RecordEmpty{})
		if err != nil {
			t.Fatalf("EncodeRecordEmpty: %v", err)
		}
		if len(re) != 0 {
			t.Fatalf("EncodeRecordEmpty = % x, want empty", re)
		}
		rv, rest, err := codecs.DecodeRecordEmpty([]byte{0xde, 0xad})
		if err != nil || !bytes.Equal(rest, []byte{0xde, 0xad}) {
			t.Fatalf("DecodeRecordEmpty consumes bytes: v=%v rest=% x err=%v", rv, rest, err)
		}
		// The named zero-width type is available and decodes to a value.
		ev, rest, err := codecs.DecodeEmpty(nil)
		if err != nil || len(rest) != 0 || ev != (codecfixture.Empty{}) {
			t.Fatalf("DecodeEmpty: v=%v rest=%d err=%v", ev, len(rest), err)
		}
		// A list of zero-width values carries only its count: encode
		// writes the count with no per-element loop, and decode allocates
		// the native length without per-element work.
		le, err := codecs.EncodeListEmpty(nil, make([]codecfixture.Empty, 3))
		if err != nil {
			t.Fatalf("EncodeListEmpty: %v", err)
		}
		if !bytes.Equal(le, u64le(3)) {
			t.Fatalf("EncodeListEmpty = % x, want count 3 only", le)
		}
		lv, rest, err := codecs.DecodeListEmpty(le)
		if err != nil || len(rest) != 0 || len(lv) != 3 {
			t.Fatalf("DecodeListEmpty: len=%d rest=%d err=%v", len(lv), len(rest), err)
		}
		if lv == nil {
			t.Fatal("DecodeListEmpty returned a nil slice")
		}
		// A zero-width list allocates up to native representability.
		huge := u64le(uint64(^uint(0) >> 1))
		hv, rest, err := codecs.DecodeListEmpty(huge)
		if err != nil || len(rest) != 0 || len(hv) != int(^uint(0)>>1) {
			t.Fatalf("DecodeListEmpty(maxInt): len=%d rest=%d err=%v", len(hv), len(rest), err)
		}
	})

	t.Run("exhaustion", func(t *testing.T) {
		// The paint request: two anonymous record parameters encoded
		// consecutively. Decoding both values must consume the payload
		// exactly, and a trailing value must surface as the remainder.
		origin := codecfixture.RecordXY{X: 1, Y: 2}
		color := codecfixture.RecordRGBA{Red: 1, Green: 2, Blue: 3, Alpha: 4}
		payload, err := codecs.EncodeRecordXY(nil, origin)
		if err != nil {
			t.Fatalf("EncodeRecordXY: %v", err)
		}
		payload, err = codecs.EncodeRecordRGBA(payload, color)
		if err != nil {
			t.Fatalf("EncodeRecordRGBA: %v", err)
		}
		o, rest, err := codecs.DecodeRecordXY(payload)
		if err != nil || o != origin {
			t.Fatalf("first parameter: v=%+v err=%v", o, err)
		}
		if len(rest) == 0 {
			t.Fatal("first parameter consumed the whole payload")
		}
		c, rest, err := codecs.DecodeRecordRGBA(rest)
		if err != nil || c != color {
			t.Fatalf("second parameter: v=%+v err=%v", c, err)
		}
		if len(rest) != 0 {
			t.Fatalf("parameters left %d trailing bytes; a request payload must be consumed exactly", len(rest))
		}
		// A bounded decode never consumes bytes beyond its value.
		v, rest, err := codecs.DecodePoint(append([]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0, 0, 0, 0, 0, 0, 0, 0x40}, 0xaa, 0xbb))
		if err != nil || v != (codecfixture.Point{X: 1, Y: 2}) || !bytes.Equal(rest, []byte{0xaa, 0xbb}) {
			t.Fatalf("trailing bytes: v=%+v rest=% x err=%v", v, rest, err)
		}
	})
}

// TestGeneratedCodecRoundTrips round-trips representative values through
// every generated codec: encode, decode to the same value with no
// remainder, and re-encode to the same bytes.
func TestGeneratedCodecRoundTrips(t *testing.T) {
	type roundTrip struct {
		name string
		v    any
		enc  func(any) ([]byte, error)
		dec  func([]byte) (any, []byte, error)
	}
	tests := []roundTrip{
		{"int8", int8(-5), func(v any) ([]byte, error) { return codecs.EncodeInt8(nil, v.(int8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt8(b); return v, r, err }},
		{"int16", int16(-300), func(v any) ([]byte, error) { return codecs.EncodeInt16(nil, v.(int16)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt16(b); return v, r, err }},
		{"int32", int32(-70000), func(v any) ([]byte, error) { return codecs.EncodeInt32(nil, v.(int32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt32(b); return v, r, err }},
		{"int64", int64(-1 << 40), func(v any) ([]byte, error) { return codecs.EncodeInt64(nil, v.(int64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeInt64(b); return v, r, err }},
		{"uint8", uint8(200), func(v any) ([]byte, error) { return codecs.EncodeUint8(nil, v.(uint8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint8(b); return v, r, err }},
		{"uint16", uint16(60000), func(v any) ([]byte, error) { return codecs.EncodeUint16(nil, v.(uint16)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint16(b); return v, r, err }},
		{"uint32", uint32(4000000000), func(v any) ([]byte, error) { return codecs.EncodeUint32(nil, v.(uint32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint32(b); return v, r, err }},
		{"uint64", uint64(1 << 63), func(v any) ([]byte, error) { return codecs.EncodeUint64(nil, v.(uint64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUint64(b); return v, r, err }},
		{"float32", float32(-0.5), func(v any) ([]byte, error) { return codecs.EncodeFloat32(nil, v.(float32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFloat32(b); return v, r, err }},
		{"float64", 3.14159, func(v any) ([]byte, error) { return codecs.EncodeFloat64(nil, v.(float64)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFloat64(b); return v, r, err }},
		{"string", "InterCall ünïcode", func(v any) ([]byte, error) { return codecs.EncodeString(nil, v.(string)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeString(b); return v, r, err }},
		{"bytes", []byte{0, 1, 2, 0xff}, func(v any) ([]byte, error) { return codecs.EncodeBytes(nil, v.([]byte)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeBytes(b); return v, r, err }},
		{"user_id", codecfixture.UserID(9), func(v any) ([]byte, error) { return codecs.EncodeUserID(nil, v.(codecfixture.UserID)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeUserID(b); return v, r, err }},
		{"name", codecfixture.Name("snow"), func(v any) ([]byte, error) { return codecs.EncodeName(nil, v.(codecfixture.Name)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeName(b); return v, r, err }},
		{"point", codecfixture.Point{X: 1.5, Y: -2.25}, func(v any) ([]byte, error) { return codecs.EncodePoint(nil, v.(codecfixture.Point)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodePoint(b); return v, r, err }},
		{"pixel", codecfixture.Pixel{Red: 10, Green: 20, Blue: 30}, func(v any) ([]byte, error) { return codecs.EncodePixel(nil, v.(codecfixture.Pixel)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodePixel(b); return v, r, err }},
		{"empty", codecfixture.Empty{}, func(v any) ([]byte, error) { return codecs.EncodeEmpty(nil, v.(codecfixture.Empty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeEmpty(b); return v, r, err }},
		{"names", codecfixture.Names{"a", "bb", ""}, func(v any) ([]byte, error) { return codecs.EncodeNames(nil, v.(codecfixture.Names)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeNames(b); return v, r, err }},
		{"matrix", codecfixture.Matrix{{1, -2}, {}, {3}}, func(v any) ([]byte, error) { return codecs.EncodeMatrix(nil, v.(codecfixture.Matrix)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeMatrix(b); return v, r, err }},
		{"blob", codecfixture.Blob{9, 8, 7}, func(v any) ([]byte, error) { return codecs.EncodeBlob(nil, v.(codecfixture.Blob)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeBlob(b); return v, r, err }},
		{"customer_id", codecfixture.CustomerID(11), func(v any) ([]byte, error) { return codecs.EncodeCustomerID(nil, v.(codecfixture.CustomerID)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeCustomerID(b); return v, r, err }},
		{"list_int32", []int32{7, -7, 0}, func(v any) ([]byte, error) { return codecs.EncodeListInt32(nil, v.([]int32)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListInt32(b); return v, r, err }},
		{"list_pixel", []codecfixture.Pixel{{Red: 1, Green: 2, Blue: 3}, {Red: 4, Green: 5, Blue: 6}}, func(v any) ([]byte, error) { return codecs.EncodeListPixel(nil, v.([]codecfixture.Pixel)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListPixel(b); return v, r, err }},
		{"list_name", []codecfixture.Name{"x", "y"}, func(v any) ([]byte, error) { return codecs.EncodeListName(nil, v.([]codecfixture.Name)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListName(b); return v, r, err }},
		{"list_string", []string{}, func(v any) ([]byte, error) { return codecs.EncodeListString(nil, v.([]string)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListString(b); return v, r, err }},
		{"record_xy", codecfixture.RecordXY{X: -1, Y: 1}, func(v any) ([]byte, error) { return codecs.EncodeRecordXY(nil, v.(codecfixture.RecordXY)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordXY(b); return v, r, err }},
		{"record_rgba", codecfixture.RecordRGBA{Red: 1, Green: 2, Blue: 3, Alpha: 4}, func(v any) ([]byte, error) { return codecs.EncodeRecordRGBA(nil, v.(codecfixture.RecordRGBA)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordRGBA(b); return v, r, err }},
		{"record_wh", codecfixture.RecordWH{Width: 640, Height: 480}, func(v any) ([]byte, error) { return codecs.EncodeRecordWH(nil, v.(codecfixture.RecordWH)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordWH(b); return v, r, err }},
		{"locate_box", codecfixture.LocateBox{Corner: codecfixture.RecordXY{X: 1, Y: 2}, Label: "box"}, func(v any) ([]byte, error) { return codecs.EncodeLocateBox(nil, v.(codecfixture.LocateBox)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeLocateBox(b); return v, r, err }},
		{"locate_result", codecfixture.LocateResult{Corner: codecfixture.Point{X: 1, Y: 2}, Area: 4}, func(v any) ([]byte, error) { return codecs.EncodeLocateResult(nil, v.(codecfixture.LocateResult)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeLocateResult(b); return v, r, err }},
		{"grid_rows", []codecfixture.GridRow{{Row: 1, Col: 2, Value: 3}}, func(v any) ([]byte, error) { return codecs.EncodeGridRows(nil, v.([]codecfixture.GridRow)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeGridRows(b); return v, r, err }},
		{"grid_row", codecfixture.GridRow{Row: 9, Col: 8, Value: 7}, func(v any) ([]byte, error) { return codecs.EncodeGridRow(nil, v.(codecfixture.GridRow)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeGridRow(b); return v, r, err }},
		{"list_uint8", []uint8{1, 2, 3}, func(v any) ([]byte, error) { return codecs.EncodeListUint8(nil, v.([]uint8)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListUint8(b); return v, r, err }},
		{"record_empty", codecfixture.RecordEmpty{}, func(v any) ([]byte, error) { return codecs.EncodeRecordEmpty(nil, v.(codecfixture.RecordEmpty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeRecordEmpty(b); return v, r, err }},
		{"list_empty", make([]codecfixture.Empty, 2), func(v any) ([]byte, error) { return codecs.EncodeListEmpty(nil, v.([]codecfixture.Empty)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeListEmpty(b); return v, r, err }},
		{"tiny_note", codecfixture.TinyNote{Tag: codecfixture.Empty{}, Code: 42}, func(v any) ([]byte, error) { return codecs.EncodeTinyNote(nil, v.(codecfixture.TinyNote)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeTinyNote(b); return v, r, err }},
		{"failed_payload", codecfixture.FailedPayload{Code: -1, Message: "boom"}, func(v any) ([]byte, error) { return codecs.EncodeFailedPayload(nil, v.(codecfixture.FailedPayload)) }, func(b []byte) (any, []byte, error) { v, r, err := codecs.DecodeFailedPayload(b); return v, r, err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.enc(tt.v)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, rest, err := tt.dec(out)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(rest) != 0 {
				t.Fatalf("decode left %d trailing bytes", len(rest))
			}
			if !reflect.DeepEqual(got, tt.v) {
				t.Fatalf("decode = %#v, want %#v", got, tt.v)
			}
			again, err := tt.enc(got)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if !bytes.Equal(again, out) {
				t.Fatalf("re-encode = % x, want % x", again, out)
			}
		})
	}
}

// TestGeneratedCodecMalformed is the malformed corpus: every entry must
// be rejected with the exact sentinel, and no decode may panic.
func TestGeneratedCodecMalformed(t *testing.T) {
	tests := []struct {
		name string
		dec  func([]byte) error
		in   []byte
		want error
	}{
		// Truncated fixed-width values.
		{"int8 empty", func(b []byte) error { _, _, err := codecs.DecodeInt8(b); return err }, nil, codecfixture.ErrTruncated},
		{"int16 one byte", func(b []byte) error { _, _, err := codecs.DecodeInt16(b); return err }, []byte{1}, codecfixture.ErrTruncated},
		{"int32 three bytes", func(b []byte) error { _, _, err := codecs.DecodeInt32(b); return err }, []byte{1, 2, 3}, codecfixture.ErrTruncated},
		{"uint32 three bytes", func(b []byte) error { _, _, err := codecs.DecodeUint32(b); return err }, []byte{1, 2, 3}, codecfixture.ErrTruncated},
		{"int64 seven bytes", func(b []byte) error { _, _, err := codecs.DecodeInt64(b); return err }, []byte{1, 2, 3, 4, 5, 6, 7}, codecfixture.ErrTruncated},
		{"float32 three bytes", func(b []byte) error { _, _, err := codecs.DecodeFloat32(b); return err }, []byte{1, 2, 3}, codecfixture.ErrTruncated},
		{"float64 seven bytes", func(b []byte) error { _, _, err := codecs.DecodeFloat64(b); return err }, []byte{1, 2, 3, 4, 5, 6, 7}, codecfixture.ErrTruncated},
		// The README example: 01 00 c0 7f is a noncanonical NaN.
		{"float32 noncanonical NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat32(b); return err }, []byte{0x01, 0x00, 0xc0, 0x7f}, codecfixture.ErrNaN},
		{"float32 signaling NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat32(b); return err }, []byte{0x01, 0x00, 0x80, 0x7f}, codecfixture.ErrNaN},
		{"float32 negative NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat32(b); return err }, []byte{0x00, 0x00, 0xc0, 0xff}, codecfixture.ErrNaN},
		{"float64 noncanonical NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat64(b); return err }, []byte{0x01, 0, 0, 0, 0, 0, 0xf8, 0x7f}, codecfixture.ErrNaN},
		{"float64 signaling NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat64(b); return err }, []byte{0x01, 0, 0, 0, 0, 0, 0xf0, 0x7f}, codecfixture.ErrNaN},
		{"float64 negative NaN", func(b []byte) error { _, _, err := codecs.DecodeFloat64(b); return err }, []byte{0x00, 0, 0, 0, 0, 0, 0xf8, 0xff}, codecfixture.ErrNaN},
		// Hostile lengths.
		{"bytes length exceeds payload", func(b []byte) error { _, _, err := codecs.DecodeBytes(b); return err }, u64le(5), codecfixture.ErrTooLong},
		{"bytes max uint64 length", func(b []byte) error { _, _, err := codecs.DecodeBytes(b); return err }, u64le(math.MaxUint64), codecfixture.ErrTooLong},
		{"bytes length above maxInt", func(b []byte) error { _, _, err := codecs.DecodeBytes(b); return err }, u64le(1 << 63), codecfixture.ErrTooLong},
		{"string length exceeds payload", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, u64le(1), codecfixture.ErrTooLong},
		{"string max uint64 length", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, u64le(math.MaxUint64), codecfixture.ErrTooLong},
		{"list pixel count exceeds payload", func(b []byte) error { _, _, err := codecs.DecodeListPixel(b); return err }, u64le(2), codecfixture.ErrTooLong},
		{"list pixel count max", func(b []byte) error { _, _, err := codecs.DecodeListPixel(b); return err }, u64le(math.MaxUint64), codecfixture.ErrTooLong},
		{"list string count exceeds payload", func(b []byte) error { _, _, err := codecs.DecodeListString(b); return err }, u64le(2), codecfixture.ErrTooLong},
		{"list empty count above maxInt", func(b []byte) error { _, _, err := codecs.DecodeListEmpty(b); return err }, u64le(1 << 63), codecfixture.ErrTooLong},
		{"list empty count max uint64", func(b []byte) error { _, _, err := codecs.DecodeListEmpty(b); return err }, u64le(math.MaxUint64), codecfixture.ErrTooLong},
		// Truncation inside lists and records.
		{"list int32 element truncated", func(b []byte) error { _, _, err := codecs.DecodeListInt32(b); return err }, append(u64le(1), 1, 2, 3), codecfixture.ErrTruncated},
		{"list pixel element truncated", func(b []byte) error { _, _, err := codecs.DecodeListPixel(b); return err }, append(u64le(2), 1, 2, 3, 4, 5), codecfixture.ErrTruncated},
		{"list string element truncated", func(b []byte) error { _, _, err := codecs.DecodeListString(b); return err }, append(u64le(1), append(u64le(2), 'a')...), codecfixture.ErrTooLong},
		{"list string second element missing", func(b []byte) error { _, _, err := codecs.DecodeListString(b); return err }, append(u64le(2), append(u64le(1), 'a')...), codecfixture.ErrTruncated},
		{"matrix inner list truncated", func(b []byte) error { _, _, err := codecs.DecodeMatrix(b); return err }, append(u64le(1), append(u64le(1), 1, 2, 3)...), codecfixture.ErrTruncated},
		{"grid rows element truncated", func(b []byte) error { _, _, err := codecs.DecodeGridRows(b); return err }, append(u64le(1), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11), codecfixture.ErrTruncated},
		{"record xy truncated", func(b []byte) error { _, _, err := codecs.DecodeRecordXY(b); return err }, []byte{1, 2, 3, 4, 5, 6, 7}, codecfixture.ErrTruncated},
		{"point truncated", func(b []byte) error { _, _, err := codecs.DecodePoint(b); return err }, make([]byte, 15), codecfixture.ErrTruncated},
		{"locate box corner truncated", func(b []byte) error { _, _, err := codecs.DecodeLocateBox(b); return err }, []byte{1, 2, 3, 4, 5, 6, 7}, codecfixture.ErrTruncated},
		{"failed payload message too long", func(b []byte) error { _, _, err := codecs.DecodeFailedPayload(b); return err }, append([]byte{0, 0, 0, 0}, u64le(3)...), codecfixture.ErrTooLong},
		// Invalid UTF-8 in strings: overlong, surrogate, truncation, and
		// invalid bytes.
		{"string invalid byte", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, append(u64le(1), 0xff), codecfixture.ErrUTF8},
		{"string overlong", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, append(u64le(2), 0xc0, 0xaf), codecfixture.ErrUTF8},
		{"string surrogate", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, append(u64le(3), 0xed, 0xa0, 0x80), codecfixture.ErrUTF8},
		{"string truncated rune", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, append(u64le(2), 0xe2, 0x82), codecfixture.ErrUTF8},
		{"string truncated length", func(b []byte) error { _, _, err := codecs.DecodeString(b); return err }, u64le(2), codecfixture.ErrTooLong},
		{"name invalid utf8", func(b []byte) error { _, _, err := codecs.DecodeName(b); return err }, append(u64le(1), 0xc3, 0x28), codecfixture.ErrUTF8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.dec(tt.in); err != tt.want {
				t.Fatalf("decode(% x) = %v, want %v", tt.in, err, tt.want)
			}
		})
	}
}

// TestGeneratedCodecOwnedRetention verifies that decoded strings and byte
// slices are owned copies: mutating the input after decoding never
// changes the decoded value.
func TestGeneratedCodecOwnedRetention(t *testing.T) {
	in := append(u64le(4), 'a', 'b', 'c', 'd')
	v, _, err := codecs.DecodeBytes(in)
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}
	in[8] = 0x99
	in[9] = 0x99
	if !bytes.Equal(v, []byte{'a', 'b', 'c', 'd'}) {
		t.Fatalf("decoded bytes changed with the input: % x", v)
	}
	s, _, err := codecs.DecodeString(append(u64le(4), 'a', 'b', 'c', 'd'))
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if s != "abcd" {
		t.Fatalf("decoded string = %q", s)
	}
	// A decoded list of byte slices owns each element.
	payload, err := codecs.EncodeListString(nil, []string{"aa", "bb"})
	if err != nil {
		t.Fatalf("EncodeListString: %v", err)
	}
	got, _, err := codecs.DecodeListString(payload)
	if err != nil {
		t.Fatalf("DecodeListString: %v", err)
	}
	for i := range payload {
		payload[i] ^= 0xff
	}
	if !reflect.DeepEqual(got, []string{"aa", "bb"}) {
		t.Fatalf("decoded strings changed with the input: %v", got)
	}
}

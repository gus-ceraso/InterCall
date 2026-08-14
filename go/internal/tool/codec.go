package tool

import (
	"fmt"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// The generated codec layer implements the exact wire rules of README.md
// "Value Encoding" and SPEC.md "Frame writing and generated codecs":
//
//   - little-endian exact-width two's-complement integers;
//   - canonical quiet-NaN output (0x7fc00000 / 0x7ff8000000000000) and
//     rejection of every other NaN bit pattern;
//   - UTF-8 validation when encoding Go strings and decoding wire strings;
//   - checked lengths and counts before conversion, allocation, slicing,
//     or iteration, with every decode bounded by the remaining payload;
//   - declaration-order records without padding and without field counts;
//   - native-length allocation with no per-element loop for zero-width
//     list elements; and
//   - owned decoded bytes: strings and byte slices are freshly allocated
//     copies that never alias the input buffer.
//
// Every emitted encoder has the form
//
//	func name(buf []byte, v T) ([]byte, error)
//
// appending one complete value to an owned payload, and every bounded
// decoder has the form
//
//	func name(src []byte) (T, []byte, error)
//
// returning the value and the exact unconsumed remainder, so a caller can
// enforce payload exhaustion. Decoders never panic on hostile input and
// never consume bytes beyond one value; encoders fail only on invalid
// UTF-8 in a Go string.
//
// Function names come from IC-08's deterministic private mangling, and
// structurally identical anonymous types share one codec pair through the
// canonical wire text of typeKeyOf.

// codec names are derived from IC-08's deterministic private mangling.
// codecName returns the mangled encoder or decoder function name for one
// codec identity, whose parts distinguish primitives ("int32"), named
// types ("type", "user_id"), and anonymous types (the canonical wire
// text).
func codecName(kind string, parts ...string) string {
	return ManglePrivate(append([]string{kind}, parts...)...)
}

// primitiveParts is the codec identity of one primitive pair.
func primitiveParts(k syntax.TokenKind) []string { return []string{k.String()} }

// namedParts is the codec identity of one named type declaration.
func namedParts(wire string) []string { return []string{"type", wire} }

// codecEmitter emits the generated type declarations and codec pairs of
// one model in deterministic order: primitive pairs in the README
// primitive-table order, named pairs in declaration order, and anonymous
// pairs in first-use order over the same walk.
type codecEmitter struct {
	src   *source
	m     *Model
	anon  map[string]syntax.TypeExpr // type key -> type occurrence
	order []string                   // anonymous type keys in first-use order

	idxSeq   int // fresh loop-index sequence per pair
	countSeq int // fresh count-local sequence per pair
}

func newCodecEmitter(src *source, m *Model) *codecEmitter {
	return &codecEmitter{src: src, m: m, anon: make(map[string]syntax.TypeExpr)}
}

// emit emits the type declarations and every codec pair.
func (e *codecEmitter) emit() {
	e.register()
	e.emitTypeDecls()
	e.emitPrimitivePairs()
	for _, t := range e.m.Types {
		e.emitNamedPair(t)
	}
	for _, key := range e.order {
		e.emitAnonPair(key)
	}
}

// register records every anonymous type occurrence that needs its own
// shared codec pair: anonymous occurrences in procedure parameters,
// results, and exception payloads, and every anonymous occurrence nested
// inside them or inside a named type's underlying type. Named type
// underlyings themselves are inlined into their named pair and never
// registered.
func (e *codecEmitter) register() {
	for _, t := range e.m.Types {
		e.registerUnderlying(t.Decl.Type)
	}
	for _, x := range e.m.Exceptions {
		if x.Payload != nil && isAnonymousType(x.Payload.Type) {
			e.registerType(x.Payload.Type)
		}
	}
	for _, p := range e.m.Procs {
		for _, pr := range p.Params {
			if isAnonymousType(pr.Type.Type) {
				e.registerType(pr.Type.Type)
			}
		}
		if p.Result != nil && isAnonymousType(p.Result.Type) {
			e.registerType(p.Result.Type)
		}
	}
}

// registerUnderlying records the anonymous sub-occurrences of one named
// type's underlying type, which is itself inlined into the named pair.
func (e *codecEmitter) registerUnderlying(t syntax.TypeExpr) {
	switch t := t.(type) {
	case *syntax.ListType:
		e.registerType(t.Elem)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			e.registerType(f.Type)
		}
	}
}

// registerType records one anonymous occurrence and, recursively, every
// anonymous occurrence nested inside it. The first use wins; later
// structurally identical occurrences share the same pair.
func (e *codecEmitter) registerType(t syntax.TypeExpr) {
	if !isAnonymousType(t) {
		return
	}
	key := typeKeyOf(t)
	if _, ok := e.anon[key]; ok {
		return
	}
	e.anon[key] = t
	e.order = append(e.order, key)
	switch t := t.(type) {
	case *syntax.ListType:
		e.registerType(t.Elem)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			e.registerType(f.Type)
		}
	}
}

// emitTypeDecls emits one Go type declaration per named type, with the
// exact @intercall type machine line of SPEC.md "Safe import and re-export
// metadata".
func (e *codecEmitter) emitTypeDecls() {
	for _, t := range e.m.Types {
		e.src.linef("// @intercall type %s", t.Decl.Name.Name)
		e.src.linef("type %s %s", t.GoName, goTypeOf(t.Decl.Type, e.m.names, e.m.types))
		e.src.blank()
	}
}

// emitPrimitivePairs emits the twelve primitive codec pairs in the
// README primitive-table order.
func (e *codecEmitter) emitPrimitivePairs() {
	for _, k := range primitiveKinds {
		emitPrimPair(e.src, k)
	}
}

// emitNamedPair emits the pair of one named type. Its underlying type is
// inlined into the pair bodies, so the pair operates directly on the
// defined Go type.
func (e *codecEmitter) emitNamedPair(t *TypeRec) {
	e.emitPair(namedParts(t.Decl.Name.Name), t.GoName, t.Decl.Type, t.ZeroWidth)
}

// emitAnonPair emits the shared pair of one registered anonymous type.
func (e *codecEmitter) emitAnonPair(key string) {
	t := e.anon[key]
	e.emitPair([]string{key}, goTypeOf(t, e.m.names, e.m.types), t, zeroWidthOf(t, e.m.types))
}

// emitPair emits one encoder/decoder pair over Go type gt for wire type t.
func (e *codecEmitter) emitPair(parts []string, gt string, t syntax.TypeExpr, zero bool) {
	if zero {
		e.emitZeroPair(parts, gt)
		return
	}
	switch t := t.(type) {
	case *syntax.PrimType:
		// A named type over a primitive: convert and delegate.
		e.emitDelegatePair(parts, gt, goTypeOf(t, e.m.names, e.m.types), primitiveParts(t.Kind))
	case *syntax.NamedType:
		// A named type over a named reference: convert and delegate.
		e.emitDelegatePair(parts, gt, goTypeOf(t, e.m.names, e.m.types), namedParts(t.Name.Name))
	case *syntax.ListType, *syntax.RecordType:
		e.emitGeneralPair(parts, gt, t)
	}
}

// emitDelegatePair emits a pair whose bodies convert the value to the
// underlying wire type and delegate to that type's pair.
func (e *codecEmitter) emitDelegatePair(parts []string, gt, conv string, target []string) {
	enc, dec := codecName("enc", parts...), codecName("dec", parts...)
	tenc, tdec := codecName("enc", target...), codecName("dec", target...)
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, gt)
	e.src.open()
	e.src.linef("var err error")
	e.src.linef("buf, err = %s(buf, %s(v))", tenc, conv)
	emitEncErr(e.src)
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, gt)
	e.src.open()
	e.src.linef("v, src, err := %s(src)", tdec)
	e.src.linef("if err != nil {")
	e.src.open()
	e.src.linef("return %s(v), nil, err", gt)
	e.src.close()
	e.src.linef("}")
	e.src.linef("return %s(v), src, nil", gt)
	e.src.close()
	e.src.linef("}")
}

// emitGeneralPair emits a pair whose bodies encode the list or record
// structure of t directly over the value v.
func (e *codecEmitter) emitGeneralPair(parts []string, gt string, t syntax.TypeExpr) {
	enc, dec := codecName("enc", parts...), codecName("dec", parts...)
	e.resetLocals()
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, gt)
	e.src.open()
	e.src.linef("var err error")
	e.emitEncBody(t, "v")
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, gt)
	e.src.open()
	e.src.linef("var v %s", gt)
	e.src.linef("var err error")
	e.emitDecBody(t, "v", "v")
	e.src.linef("return v, src, nil")
	e.src.close()
	e.src.linef("}")
}

// emitZeroPair emits the trivial pair of a zero-width type: encoding
// appends nothing and decoding consumes nothing.
func (e *codecEmitter) emitZeroPair(parts []string, gt string) {
	enc, dec := codecName("enc", parts...), codecName("dec", parts...)
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, gt)
	e.src.open()
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, gt)
	e.src.open()
	e.src.linef("return %s{}, src, nil", gt)
	e.src.close()
	e.src.linef("}")
}

// resetLocals starts a fresh local-name sequence for one pair body.
func (e *codecEmitter) resetLocals() { e.idxSeq, e.countSeq = 0, 0 }

// nextIndex returns the next fresh loop-index name for one pair body.
func (e *codecEmitter) nextIndex() string {
	name := fmt.Sprintf("i%d", e.idxSeq)
	e.idxSeq++
	return name
}

// nextCount returns the next fresh count-local name for one pair body.
func (e *codecEmitter) nextCount() string {
	name := fmt.Sprintf("count%d", e.countSeq)
	e.countSeq++
	return name
}

// emitEncBody emits statements that append-encode the value expression
// val, whose wire type is t, into buf. err is the enclosing encoder's
// declared error variable.
func (e *codecEmitter) emitEncBody(t syntax.TypeExpr, val string) {
	switch t := t.(type) {
	case *syntax.PrimType:
		e.src.linef("buf, err = %s(buf, %s)", codecName("enc", primitiveParts(t.Kind)...), val)
		emitEncErr(e.src)
	case *syntax.NamedType:
		e.src.linef("buf, err = %s(buf, %s)", codecName("enc", namedParts(t.Name.Name)...), val)
		emitEncErr(e.src)
	case *syntax.ListType:
		e.emitEncList(t, val)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			if zeroWidthOf(f.Type, e.m.types) {
				continue // zero-width fields occupy no wire bytes
			}
			e.emitEncBody(f.Type, val+"."+e.m.names.Field[f])
		}
	}
}

// emitEncList emits the count and element loop of one list value.
// Zero-width elements carry only the count, with no per-element loop.
func (e *codecEmitter) emitEncList(t *syntax.ListType, val string) {
	e.src.linef("buf, err = %s(buf, uint64(len(%s)))", codecName("enc", primitiveParts(syntax.TokUint64)...), val)
	emitEncErr(e.src)
	if zeroWidthOf(t.Elem, e.m.types) {
		return
	}
	idx := e.nextIndex()
	e.src.linef("for %s := range %s {", idx, val)
	e.src.open()
	e.emitEncBody(t.Elem, fmt.Sprintf("%s[%s]", val, idx))
	e.src.close()
	e.src.linef("}")
}

// emitEncErr emits the shared encoder error check.
func emitEncErr(src *source) {
	src.linef("if err != nil {")
	src.open()
	src.linef("return nil, err")
	src.close()
	src.linef("}")
}

// emitDecBody emits statements that decode one value of wire type t into
// the assignable expression dst, advancing src and returning errVal with
// a nil remainder on failure. err is the enclosing decoder's declared
// error variable.
func (e *codecEmitter) emitDecBody(t syntax.TypeExpr, dst, errVal string) {
	switch t := t.(type) {
	case *syntax.PrimType:
		e.src.linef("%s, src, err = %s(src)", dst, codecName("dec", primitiveParts(t.Kind)...))
		emitDecErr(e.src, errVal)
	case *syntax.NamedType:
		e.src.linef("%s, src, err = %s(src)", dst, codecName("dec", namedParts(t.Name.Name)...))
		emitDecErr(e.src, errVal)
	case *syntax.ListType:
		e.emitDecList(t, dst, errVal)
	case *syntax.RecordType:
		for _, f := range t.Fields {
			if zeroWidthOf(f.Type, e.m.types) {
				continue // zero-width fields stay at their zero value
			}
			e.emitDecBody(f.Type, dst+"."+e.m.names.Field[f], errVal)
		}
	}
}

// emitDecList emits the checked count and element loop of one list
// decode. The count is checked against native int before conversion and
// against the remaining payload before allocation, so allocation is
// bounded by the payload; a zero-width element list allocates its native
// length without a per-element loop.
func (e *codecEmitter) emitDecList(t *syntax.ListType, dst, errVal string) {
	count := e.nextCount()
	// The count local is declared first and assigned with plain =
	// so that src and err are never shadowed when this statement sits
	// inside a nested block (a list element or record field loop).
	e.src.linef("var %s uint64", count)
	e.src.linef("%s, src, err = %s(src)", count, codecName("dec", primitiveParts(syntax.TokUint64)...))
	emitDecErr(e.src, errVal)
	if zeroWidthOf(t.Elem, e.m.types) {
		e.src.linef("if %s > uint64(%s) {", count, maxIntName)
		e.src.open()
		e.src.linef("return %s, nil, %s", errVal, errLongName)
		e.src.close()
		e.src.linef("}")
		e.src.linef("%s = make(%s, int(%s))", dst, goTypeOf(t, e.m.names, e.m.types), count)
		return
	}
	e.src.linef("if %s > uint64(len(src)) {", count)
	e.src.open()
	e.src.linef("return %s, nil, %s", errVal, errLongName)
	e.src.close()
	e.src.linef("}")
	e.src.linef("%s = make(%s, int(%s))", dst, goTypeOf(t, e.m.names, e.m.types), count)
	idx := e.nextIndex()
	e.src.linef("for %s := range %s {", idx, dst)
	e.src.open()
	e.emitDecBody(t.Elem, fmt.Sprintf("%s[%s]", dst, idx), errVal)
	e.src.close()
	e.src.linef("}")
}

// emitDecErr emits the shared decoder error check.
func emitDecErr(src *source, errVal string) {
	src.linef("if err != nil {")
	src.open()
	src.linef("return %s, nil, err", errVal)
	src.close()
	src.linef("}")
}

// emitPrimPair emits one primitive codec pair with direct byte
// operations: little-endian exact-width integers, canonical quiet NaN
// output, rejection of every other NaN bit pattern, UTF-8 validation on
// strings, and owned byte copies for bytes. The emission is shared by
// the import and export emitters, so both directions render the same
// twelve pairs.
func emitPrimPair(src *source, k syntax.TokenKind) {
	enc, dec := codecName("enc", primitiveParts(k)...), codecName("dec", primitiveParts(k)...)
	gt := "[]byte"
	if k != syntax.TokBytes {
		gt = k.String()
	}
	src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, gt)
	src.open()
	switch k {
	case syntax.TokInt8, syntax.TokInt16, syntax.TokInt32, syntax.TokInt64,
		syntax.TokUint8, syntax.TokUint16, syntax.TokUint32, syntax.TokUint64:
		src.linef("return append(buf, %s), nil", encIntBytes(k))
	case syntax.TokFloat32:
		src.linef("var bits uint32")
		src.linef("if v != v {")
		src.open()
		src.linef("bits = 0x7fc00000")
		src.close()
		src.linef("} else {")
		src.open()
		src.linef("bits = math.Float32bits(v)")
		src.close()
		src.linef("}")
		src.linef("return append(buf, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24)), nil")
	case syntax.TokFloat64:
		src.linef("var bits uint64")
		src.linef("if v != v {")
		src.open()
		src.linef("bits = 0x7ff8000000000000")
		src.close()
		src.linef("} else {")
		src.open()
		src.linef("bits = math.Float64bits(v)")
		src.close()
		src.linef("}")
		src.linef("return append(buf, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24), byte(bits>>32), byte(bits>>40), byte(bits>>48), byte(bits>>56)), nil")
	case syntax.TokString:
		src.linef("if !utf8.ValidString(v) {")
		src.open()
		src.linef("return nil, %s", errUTF8Name)
		src.close()
		src.linef("}")
		src.linef("var err error")
		src.linef("buf, err = %s(buf, uint64(len(v)))", codecName("enc", primitiveParts(syntax.TokUint64)...))
		emitEncErr(src)
		src.linef("return append(buf, v...), nil")
	case syntax.TokBytes:
		src.linef("var err error")
		src.linef("buf, err = %s(buf, uint64(len(v)))", codecName("enc", primitiveParts(syntax.TokUint64)...))
		emitEncErr(src)
		src.linef("return append(buf, v...), nil")
	}
	src.close()
	src.linef("}")
	src.blank()
	src.linef("func %s(src []byte) (%s, []byte, error) {", dec, gt)
	src.open()
	switch k {
	case syntax.TokInt8, syntax.TokInt16, syntax.TokInt32, syntax.TokInt64,
		syntax.TokUint8, syntax.TokUint16, syntax.TokUint32, syntax.TokUint64:
		width := primWidth(k)
		src.linef("if len(src) < %d {", width)
		src.open()
		src.linef("return 0, nil, %s", errShortName)
		src.close()
		src.linef("}")
		src.linef("return %s, src[%d:], nil", decIntExpr(k), width)
	case syntax.TokFloat32:
		src.linef("if len(src) < 4 {")
		src.open()
		src.linef("return 0, nil, %s", errShortName)
		src.close()
		src.linef("}")
		src.linef("bits := uint32(src[0]) | uint32(src[1])<<8 | uint32(src[2])<<16 | uint32(src[3])<<24")
		src.linef("v := math.Float32frombits(bits)")
		src.linef("if v != v && bits != 0x7fc00000 {")
		src.open()
		src.linef("return 0, nil, %s", errNaNName)
		src.close()
		src.linef("}")
		src.linef("return v, src[4:], nil")
	case syntax.TokFloat64:
		src.linef("if len(src) < 8 {")
		src.open()
		src.linef("return 0, nil, %s", errShortName)
		src.close()
		src.linef("}")
		src.linef("bits := uint64(src[0]) | uint64(src[1])<<8 | uint64(src[2])<<16 | uint64(src[3])<<24 |")
		src.linef("	uint64(src[4])<<32 | uint64(src[5])<<40 | uint64(src[6])<<48 | uint64(src[7])<<56")
		src.linef("v := math.Float64frombits(bits)")
		src.linef("if v != v && bits != 0x7ff8000000000000 {")
		src.open()
		src.linef("return 0, nil, %s", errNaNName)
		src.close()
		src.linef("}")
		src.linef("return v, src[8:], nil")
	case syntax.TokString:
		src.linef("n64, src, err := %s(src)", codecName("dec", primitiveParts(syntax.TokUint64)...))
		emitDecErr(src, `""`)
		src.linef("if n64 > uint64(len(src)) {")
		src.open()
		src.linef("return \"\", nil, %s", errLongName)
		src.close()
		src.linef("}")
		src.linef("n := int(n64)")
		src.linef("b := src[:n]")
		src.linef("if !utf8.Valid(b) {")
		src.open()
		src.linef("return \"\", nil, %s", errUTF8Name)
		src.close()
		src.linef("}")
		src.linef("return string(b), src[n:], nil")
	case syntax.TokBytes:
		src.linef("n64, src, err := %s(src)", codecName("dec", primitiveParts(syntax.TokUint64)...))
		emitDecErr(src, "nil")
		src.linef("if n64 > uint64(len(src)) {")
		src.open()
		src.linef("return nil, nil, %s", errLongName)
		src.close()
		src.linef("}")
		src.linef("n := int(n64)")
		src.linef("dst := make([]byte, n)")
		src.linef("copy(dst, src[:n])")
		src.linef("return dst, src[n:], nil")
	}
	src.close()
	src.linef("}")
	src.blank()
}

// encIntBytes renders the little-endian byte list of one integer encoder.
func encIntBytes(k syntax.TokenKind) string {
	var out string
	for i := 0; i < primWidth(k); i++ {
		if i > 0 {
			out += ", "
		}
		if i == 0 {
			out += "byte(v)"
		} else {
			out += fmt.Sprintf("byte(v>>%d)", 8*i)
		}
	}
	return out
}

// decIntExpr renders the little-endian integer expression of one integer
// decoder.
func decIntExpr(k syntax.TokenKind) string {
	var out string
	for i := 0; i < primWidth(k); i++ {
		if i > 0 {
			out += " | "
		}
		if i == 0 {
			out += fmt.Sprintf("%s(src[0])", k.String())
		} else {
			out += fmt.Sprintf("%s(src[%d])<<%d", k.String(), i, 8*i)
		}
	}
	return out
}

// primWidth returns the exact wire width of one primitive kind.
func primWidth(k syntax.TokenKind) int {
	switch k {
	case syntax.TokInt8, syntax.TokUint8:
		return 1
	case syntax.TokInt16, syntax.TokUint16:
		return 2
	case syntax.TokInt32, syntax.TokUint32, syntax.TokFloat32:
		return 4
	case syntax.TokInt64, syntax.TokUint64, syntax.TokFloat64:
		return 8
	}
	return 0
}

// The generated codec support names.
var (
	maxIntName   = ManglePrivate("maxint")
	errShortName = ManglePrivate("error", "short")
	errLongName  = ManglePrivate("error", "long")
	errNaNName   = ManglePrivate("error", "nan")
	errUTF8Name  = ManglePrivate("error", "utf8")
)

package tool

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/cerasos/intercall/go/internal/syntax"
)

// This file implements the export-side generated codec layer: the codec
// pairs of one export binding. The pairs implement the same exact wire
// rules as the import side — little-endian exact-width integers,
// canonical quiet-NaN output and rejection of every other NaN bit
// pattern, UTF-8 validation, checked lengths and counts, declaration-
// order records, and owned decoded bytes — but they operate on the
// provider packages' own Go types instead of types the binding
// declares.
//
// Every type occurrence of the export model (procedure parameters and
// results, exception payloads, and named-type underlyings) carries a Go
// projection: the exact Go type text of the occurrence and the Go field
// names of its records, both recovered from the occurrence's go/types
// structure. Aliases are transparent, so an occurrence whose Go type is
// an alias renders as its resolved wire form, and the exact source
// spelling of an anonymous record — including every struct tag — comes
// from the go/types struct. Primitive pairs are the shared twelve pairs
// of the import emitter; named types get one pair keyed by exact wire
// name whose body operates directly on the provider's defined type; and
// anonymous inline records get one shared pair per distinct (wire
// structure, Go type) pair, because two wire-identical anonymous
// records with different Go spellings need distinct codecs.

// exportNode is the Go projection of one wire type node: the parallel
// tree of the wire structure and the Go facts needed to emit codec
// bodies over provider values.
//
// parts holds the codec identity of a primitive or named leaf (the
// delegate pair it encodes or decodes through), field the Go field name
// of a record child, fields the record children in wire field order,
// elem the list element, and gt the exact Go type text of this subtree
// rendered from the wire structure and the go/types structure.
type exportNode struct {
	wire   syntax.TypeExpr
	parts  []string
	field  string
	fields []*exportNode
	elem   *exportNode
	gt     string
}

// encName returns the delegate encoder name of a leaf node.
func (n *exportNode) encName() string { return codecName("enc", n.parts...) }

// decName returns the delegate decoder name of a leaf node.
func (n *exportNode) decName() string { return codecName("dec", n.parts...) }

// exportPair is one codec pair of an export binding.
//
// parts is the pair's codec identity, gt the Go type of the encoded or
// decoded value, and zero the zero-width fact. A delegate pair (a named
// type over a primitive or another named type) converts through conv to
// the delegate pair target; a general pair encodes or decodes the list
// or record structure of node directly over the value.
type exportPair struct {
	parts  []string
	gt     string
	conv   string
	target []string
	node   *exportNode
	zero   bool
}

// encName returns the encoder name of one pair.
func (p *exportPair) encName() string { return codecName("enc", p.parts...) }

// decName returns the decoder name of one pair.
func (p *exportPair) decName() string { return codecName("dec", p.parts...) }

// exportCodecEmitter emits the codec pairs of one export binding in
// deterministic order: the twelve shared primitive pairs, the named
// pairs in the model's stable topological type order, and the anonymous
// pairs in first-use order over the walk that registered them.
type exportCodecEmitter struct {
	src       *source
	gtName    func(wire string) string // exact wire name -> qualified Go name
	wireTypes map[string]*syntax.TypeDecl

	named []*exportPair          // named pairs in model type order
	anon  map[string]*exportPair // (wire text, Go type) -> pair
	order []*exportPair          // anonymous pairs in first-use order

	idxSeq   int // fresh loop-index sequence per pair
	countSeq int // fresh count-local sequence per pair
}

// newExportCodecEmitter builds the export codec emitter of one binding.
func newExportCodecEmitter(src *source, gtName func(string) string, wireTypes map[string]*syntax.TypeDecl) *exportCodecEmitter {
	return &exportCodecEmitter{
		src:       src,
		gtName:    gtName,
		wireTypes: wireTypes,
		anon:      make(map[string]*exportPair),
	}
}

// primGT renders the Go type text of one primitive wire kind.
func primGT(k syntax.TokenKind) string {
	if k == syntax.TokBytes {
		return "[]byte"
	}
	return k.String()
}

// structOf unwraps one go/types type to its struct, following aliases
// and defined types, or nil when the type is not a struct.
func structOf(t types.Type) *types.Struct {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		t = n.Underlying()
	}
	s, _ := t.(*types.Struct)
	return s
}

// sliceElem unwraps one go/types type to the element of its slice,
// following aliases and defined types, or nil when the type is not a
// slice.
func sliceElem(t types.Type) types.Type {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		t = n.Underlying()
	}
	s, ok := t.(*types.Slice)
	if !ok {
		return nil
	}
	return s.Elem()
}

// buildNode builds the Go projection of one type occurrence: the wire
// tree and the parallel go/types structure, which the mapper already
// correlated field by field in source order. The gt of a record is the
// exact anonymous struct spelling — the field names and every struct
// tag from the go/types struct — so the rendered type is identical to
// the provider's type even when a field carries a non-InterCall tag.
func (e *exportCodecEmitter) buildNode(wire syntax.TypeExpr, t types.Type) *exportNode {
	switch wire := wire.(type) {
	case *syntax.PrimType:
		return &exportNode{wire: wire, parts: primitiveParts(wire.Kind), gt: primGT(wire.Kind)}
	case *syntax.NamedType:
		return &exportNode{wire: wire, parts: namedParts(wire.Name.Name), gt: e.gtName(wire.Name.Name)}
	case *syntax.ListType:
		n := &exportNode{wire: wire}
		n.elem = e.buildNode(wire.Elem, sliceElem(t))
		n.gt = "[]" + n.elem.gt
		return n
	case *syntax.RecordType:
		n := &exportNode{wire: wire}
		st := structOf(t)
		if st == nil || st.NumFields() != len(wire.Fields) {
			panic("tool: internal error: the wire record does not match the Go struct")
		}
		if len(wire.Fields) == 0 {
			n.gt = "struct{}"
			return n
		}
		var b strings.Builder
		b.WriteString("struct {\n")
		for i, f := range wire.Fields {
			child := e.buildNode(f.Type, st.Field(i).Type())
			child.field = st.Field(i).Name()
			n.fields = append(n.fields, child)
			b.WriteByte('\t')
			b.WriteString(child.field)
			b.WriteByte(' ')
			b.WriteString(child.gt)
			if tag := st.Tag(i); tag != "" {
				b.WriteByte(' ')
				b.WriteString(strconv.Quote(tag))
			}
			b.WriteByte('\n')
		}
		b.WriteString("}")
		n.gt = b.String()
		return n
	}
	panic("tool: internal error: unknown wire type in the export codec projection")
}

// registerNamed records the named pair of one reachable ordinary type.
// A named type over a primitive or another named type delegates with a
// conversion; a named type over a list or record inlines its structure
// directly over the provider's defined type.
func (e *exportCodecEmitter) registerNamed(rec *NamedType) {
	node := e.buildNode(rec.Type, rec.TypeName.Type())
	p := &exportPair{
		parts: namedParts(rec.WireName),
		gt:    e.gtName(rec.WireName),
		node:  node,
		zero:  zeroWidthOf(rec.Type, e.wireTypes),
	}
	switch node.wire.(type) {
	case *syntax.PrimType, *syntax.NamedType:
		p.conv = node.gt
		p.target = node.parts
	}
	e.named = append(e.named, p)
}

// registerAnon records one anonymous inline record occurrence and
// returns its pair. Structurally identical occurrences with the same
// Go type text share one pair; occurrences with the same wire
// structure but different Go spellings get distinct pairs. The pair's
// codec identity is the canonical wire text and the Go type text, so
// the generated names are deterministic.
func (e *exportCodecEmitter) registerAnon(wire syntax.TypeExpr, gt string, node *exportNode) *exportPair {
	key := typeKeyOf(wire) + "\x00" + gt
	if p := e.anon[key]; p != nil {
		return p
	}
	p := &exportPair{
		parts: []string{typeKeyOf(wire), gt},
		gt:    gt,
		node:  node,
		zero:  zeroWidthOf(wire, e.wireTypes),
	}
	e.anon[key] = p
	e.order = append(e.order, p)
	return p
}

// emit emits every pair in deterministic order: the twelve shared
// primitive pairs, the named pairs in the model's stable topological
// type order, and the anonymous pairs in first-use order.
func (e *exportCodecEmitter) emit() {
	for _, k := range []syntax.TokenKind{
		syntax.TokInt8, syntax.TokInt16, syntax.TokInt32, syntax.TokInt64,
		syntax.TokUint8, syntax.TokUint16, syntax.TokUint32, syntax.TokUint64,
		syntax.TokFloat32, syntax.TokFloat64, syntax.TokString, syntax.TokBytes,
	} {
		emitPrimPair(e.src, k)
	}
	for _, p := range e.named {
		e.emitPair(p)
	}
	for _, p := range e.order {
		e.emitPair(p)
	}
}

// emitPair emits one registered pair: the zero pair, the delegate
// pair, or the general pair.
func (e *exportCodecEmitter) emitPair(p *exportPair) {
	switch {
	case p.zero:
		e.emitZeroPair(p)
	case p.target != nil:
		e.emitDelegatePair(p)
	default:
		e.emitGeneralPair(p)
	}
}

// emitDelegatePair emits a pair whose bodies convert the value to the
// underlying wire type and delegate to that type's pair.
func (e *exportCodecEmitter) emitDelegatePair(p *exportPair) {
	enc, dec := p.encName(), p.decName()
	tenc, tdec := codecName("enc", p.target...), codecName("dec", p.target...)
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, p.gt)
	e.src.open()
	e.src.linef("var err error")
	e.src.linef("buf, err = %s(buf, %s(v))", tenc, p.conv)
	emitEncErr(e.src)
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, p.gt)
	e.src.open()
	e.src.linef("v, src, err := %s(src)", tdec)
	e.src.linef("if err != nil {")
	e.src.open()
	e.src.linef("return %s(v), nil, err", p.gt)
	e.src.close()
	e.src.linef("}")
	e.src.linef("return %s(v), src, nil", p.gt)
	e.src.close()
	e.src.linef("}")
}

// emitGeneralPair emits a pair whose bodies encode or decode the list
// or record structure of the node directly over the value.
func (e *exportCodecEmitter) emitGeneralPair(p *exportPair) {
	enc, dec := p.encName(), p.decName()
	e.resetLocals()
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, p.gt)
	e.src.open()
	e.src.linef("var err error")
	e.emitEncNode(p.node, "v")
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, p.gt)
	e.src.open()
	e.src.linef("var v %s", p.gt)
	e.src.linef("var err error")
	e.emitDecNode(p.node, "v", "v")
	e.src.linef("return v, src, nil")
	e.src.close()
	e.src.linef("}")
}

// emitZeroPair emits the trivial pair of a zero-width type: encoding
// appends nothing and decoding consumes nothing.
func (e *exportCodecEmitter) emitZeroPair(p *exportPair) {
	enc, dec := p.encName(), p.decName()
	e.src.linef("func %s(buf []byte, v %s) ([]byte, error) {", enc, p.gt)
	e.src.open()
	e.src.linef("return buf, nil")
	e.src.close()
	e.src.linef("}")
	e.src.blank()
	e.src.linef("func %s(src []byte) (%s, []byte, error) {", dec, p.gt)
	e.src.open()
	e.src.linef("return %s{}, src, nil", p.gt)
	e.src.close()
	e.src.linef("}")
}

// resetLocals starts a fresh local-name sequence for one pair body.
func (e *exportCodecEmitter) resetLocals() { e.idxSeq, e.countSeq = 0, 0 }

// nextIndex returns the next fresh loop-index name for one pair body.
func (e *exportCodecEmitter) nextIndex() string {
	name := fmt.Sprintf("i%d", e.idxSeq)
	e.idxSeq++
	return name
}

// nextCount returns the next fresh count-local name for one pair body.
func (e *exportCodecEmitter) nextCount() string {
	name := fmt.Sprintf("count%d", e.countSeq)
	e.countSeq++
	return name
}

// emitEncNode emits statements that append-encode the value expression
// val, whose projection is n, into buf. err is the enclosing encoder's
// declared error variable. Zero-width record fields occupy no wire
// bytes and are skipped.
func (e *exportCodecEmitter) emitEncNode(n *exportNode, val string) {
	switch n.wire.(type) {
	case *syntax.PrimType, *syntax.NamedType:
		e.src.linef("buf, err = %s(buf, %s)", n.encName(), val)
		emitEncErr(e.src)
	case *syntax.ListType:
		e.src.linef("buf, err = %s(buf, uint64(len(%s)))", codecName("enc", primitiveParts(syntax.TokUint64)...), val)
		emitEncErr(e.src)
		if zeroWidthOf(n.wire.(*syntax.ListType).Elem, e.wireTypes) {
			return
		}
		idx := e.nextIndex()
		e.src.linef("for %s := range %s {", idx, val)
		e.src.open()
		e.emitEncNode(n.elem, fmt.Sprintf("%s[%s]", val, idx))
		e.src.close()
		e.src.linef("}")
	case *syntax.RecordType:
		for _, f := range n.fields {
			if zeroWidthOf(f.wire, e.wireTypes) {
				continue
			}
			e.emitEncNode(f, val+"."+f.field)
		}
	}
}

// emitDecNode emits statements that decode one value of the projection
// n into the assignable expression dst, advancing src and returning
// errVal with a nil remainder on failure. err is the enclosing
// decoder's declared error variable.
func (e *exportCodecEmitter) emitDecNode(n *exportNode, dst, errVal string) {
	switch n.wire.(type) {
	case *syntax.PrimType, *syntax.NamedType:
		e.src.linef("%s, src, err = %s(src)", dst, n.decName())
		emitDecErr(e.src, errVal)
	case *syntax.ListType:
		count := e.nextCount()
		// The count local is declared first and assigned with plain =
		// so that src and err are never shadowed when this statement
		// sits inside a nested block (a list element or record field
		// loop).
		e.src.linef("var %s uint64", count)
		e.src.linef("%s, src, err = %s(src)", count, codecName("dec", primitiveParts(syntax.TokUint64)...))
		emitDecErr(e.src, errVal)
		if zeroWidthOf(n.wire.(*syntax.ListType).Elem, e.wireTypes) {
			e.src.linef("if %s > uint64(%s) {", count, maxIntName)
			e.src.open()
			e.src.linef("return %s, nil, %s", errVal, errLongName)
			e.src.close()
			e.src.linef("}")
			e.src.linef("%s = make(%s, int(%s))", dst, n.gt, count)
			return
		}
		e.src.linef("if %s > uint64(len(src)) {", count)
		e.src.open()
		e.src.linef("return %s, nil, %s", errVal, errLongName)
		e.src.close()
		e.src.linef("}")
		e.src.linef("%s = make(%s, int(%s))", dst, n.gt, count)
		idx := e.nextIndex()
		e.src.linef("for %s := range %s {", idx, dst)
		e.src.open()
		e.emitDecNode(n.elem, fmt.Sprintf("%s[%s]", dst, idx), errVal)
		e.src.close()
		e.src.linef("}")
	case *syntax.RecordType:
		for _, f := range n.fields {
			if zeroWidthOf(f.wire, e.wireTypes) {
				continue // zero-width fields stay at their zero value
			}
			e.emitDecNode(f, dst+"."+f.field, errVal)
		}
	}
}

package tool

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cerasos/intercall/go/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// This file tests the source-form-sensitive Go value mapping, the
// reachable named-type graph, and the stable topological order
// (SPEC.md "Procedure signatures and wire values" and "Deterministic
// export order").
//
// The negative cases are small in-memory packages type-checked against
// the standard library, so every rejection is exercised in isolation
// without a go/packages load; the positive cases and the cross-package
// integration use temporary modules loaded through the real discovery
// pipeline, so the mapped records and the importability checks run over
// the actual load graph.

// synthPkg type-checks one in-memory package and builds its explicit
// package record and the provider records of every tagged function.
func synthPkg(t *testing.T, name string, files map[string]string) (*ExplicitPackage, []*Provider) {
	t.Helper()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var afs []*ast.File
	for _, n := range names {
		af, err := parser.ParseFile(fset, n, []byte(files[n]), parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", n, err)
		}
		afs = append(afs, af)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	tpkg, err := (&types.Config{Importer: importer.Default()}).Check(name, fset, afs, info)
	if err != nil {
		t.Fatalf("type-checking fixture: %v", err)
	}
	docs := make(map[string]*Document, len(names))
	for _, n := range names {
		doc, err := ParseGoSource(n, []byte(files[n]))
		if err != nil {
			t.Fatalf("parsing document %s: %v", n, err)
		}
		docs[n] = doc
	}
	exp := &ExplicitPackage{
		Path:  name,
		Name:  tpkg.Name(),
		files: names,
		pkg: &packages.Package{
			ID:              name,
			PkgPath:         name,
			Name:            tpkg.Name(),
			Fset:            fset,
			Syntax:          afs,
			Types:           tpkg,
			TypesInfo:       info,
			CompiledGoFiles: names,
		},
		docs: docs,
	}
	return exp, synthProviders(t, exp)
}

// synthProviders builds the provider records of every tagged function
// of one synthetic package, in source order.
func synthProviders(t *testing.T, exp *ExplicitPackage) []*Provider {
	t.Helper()
	funcs := make(map[string]*ast.FuncDecl)
	for _, af := range exp.pkg.Syntax {
		for _, d := range af.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	var out []*Provider
	for i := range exp.pkg.Syntax {
		doc := exp.docs[exp.files[i]]
		for _, decl := range doc.Decls {
			if decl.Kind != GoFunc || !hasDirective(decl.Doc, ProcedureDir) {
				continue
			}
			fd := funcs[decl.Name]
			if fd == nil {
				t.Fatalf("no function declaration for %q", decl.Name)
			}
			var params []string
			for _, f := range fd.Type.Params.List[1:] {
				for _, n := range f.Names {
					params = append(params, n.Name)
				}
			}
			data := fd.Type.Results != nil && len(fd.Type.Results.List) == 2
			out = append(out, &Provider{Pkg: exp, Name: decl.Name, Func: fd, Doc: decl, Params: params, DataResult: data})
		}
	}
	return out
}

// mapOne runs MapValues over one synthetic package's providers.
func mapOne(t *testing.T, name string, src string) (*TypeMap, error) {
	t.Helper()
	_, providers := synthPkg(t, name, map[string]string{"synth.go": src})
	return MapValues(providers, "")
}

// mapErr fails unless MapValues returns an error containing every
// substring.
func mapErr(t *testing.T, name string, src string, contains ...string) {
	t.Helper()
	_, err := mapOne(t, name, src)
	if err == nil {
		t.Fatal("mapping succeeded, want an error")
	}
	for _, s := range contains {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), s)
		}
	}
}

// wantValue asserts one mapped value's canonical wire structure and
// zero-width fact.
func wantValue(t *testing.T, v *MappedValue, want string, zero bool) {
	t.Helper()
	if got := typeKeyOf(v.Type); got != want {
		t.Errorf("wire structure = %q, want %q", got, want)
	}
	if v.ZeroWidth != zero {
		t.Errorf("ZeroWidth = %v, want %v", v.ZeroWidth, zero)
	}
}

// providerByName returns the mapped provider of one Go function name.
func providerByName(t *testing.T, tm *TypeMap, name string) *MappedProvider {
	t.Helper()
	for _, mp := range tm.Providers {
		if mp.Provider.Name == name {
			return mp
		}
	}
	t.Fatalf("no mapped provider %q", name)
	return nil
}

// paramKeys renders the canonical wire structures of a provider's
// parameters.
func paramKeys(mp *MappedProvider) []string {
	var keys []string
	for _, p := range mp.Params {
		keys = append(keys, typeKeyOf(p.Value.Type))
	}
	return keys
}

// TestTypeMapping covers the exact source-form-sensitive value mapping:
// primitives, []byte versus []uint8 at every source and alias node,
// anonymous records and field tags, named preservation and alias
// flattening, unsupported forms, field and type rejections, codec
// facts, recursion rejection, and the cross-package integration over a
// real load.
func TestTypeMapping(t *testing.T) {
	t.Run("Primitives", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure prims
func Prims(ctx context.Context, a int8, b int16, c int32, d int64, e uint8, f uint16, g uint32, h uint64, i float32, j float64, k string) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Prims")
		want := []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "string"}
		if got := paramKeys(mp); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %v, want %v", got, want)
		}
		for _, p := range mp.Params {
			if p.Value.ZeroWidth {
				t.Errorf("param %q: ZeroWidth = true, want false", p.GoName)
			}
		}
	})

	t.Run("BytesVersusUint8AtEveryNode", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

type B = []byte
type U = []uint8
type B2 = B

// @intercall procedure byte_forms
func Bytes(ctx context.Context, raw []byte, list []uint8, nested [][]byte, aliased B, aliasedList U, chained B2, elem []B) error { return nil }

// @intercall procedure defs
func Defs(ctx context.Context, p Payload, c Codes) error { return nil }

// @intercall type payload
type Payload []byte

// @intercall type
type Codes []uint8
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Bytes")
		want := []string{"bytes", "list uint8", "list bytes", "bytes", "list uint8", "bytes", "list bytes"}
		if got := paramKeys(mp); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %v, want %v", got, want)
		}
		// The defined types preserve their underlying wire forms.
		mp = providerByName(t, tm, "Defs")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"payload", "codes"}) {
			t.Errorf("params = %v, want [payload codes]", got)
		}
		// The aliases are flattened: only the two defined types remain.
		var wires []string
		for _, rec := range tm.Types {
			wires = append(wires, rec.WireName)
		}
		if !reflect.DeepEqual(wires, []string{"codes", "payload"}) {
			t.Errorf("types = %v, want [codes payload]", wires)
		}
		for _, rec := range tm.Types {
			want := "bytes"
			if rec.WireName == "codes" {
				want = "list uint8"
			}
			if got := typeKeyOf(rec.Type); got != want {
				t.Errorf("underlying of %s = %q, want %q", rec.WireName, got, want)
			}
			if rec.ZeroWidth {
				t.Errorf("underlying of %s: ZeroWidth = true, want false", rec.WireName)
			}
		}
	})

	t.Run("AnonymousRecords", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure records
func Records(ctx context.Context, s struct {
	// The display name.
	Name string
	Tags []string
}, empty struct{}) error { return nil }

// @intercall procedure nested
func Nested(ctx context.Context, v struct {
	Meta struct {
		Count uint32
	}
	Items []struct {
		Key []byte
	}
}) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Records")
		wantValue(t, mp.Params[0].Value, "record{name string;tags list string}", false)
		rec := mp.Params[0].Value.Type.(*syntax.RecordType)
		if rec.Fields[0].Doc != "The display name." {
			t.Errorf("field doc = %q, want %q", rec.Fields[0].Doc, "The display name.")
		}
		wantValue(t, mp.Params[1].Value, "record{}", true)
		mp = providerByName(t, tm, "Nested")
		wantValue(t, mp.Params[0].Value, "record{meta record{count uint32};items list record{key bytes}}", false)
	})

	t.Run("FieldTags", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure tagged
func Tagged(ctx context.Context, v struct {
	FieldA string `+"`intercall:\"the_name\"`"+`
	SkipMe int8   `+"`json:\"ignored\"`"+`
	Other  string `+"`intercall:\"other\"`"+`
}) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Tagged")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"record{the_name string;skip_me int8;other string}"}) {
			t.Errorf("params = %v", got)
		}
	})

	t.Run("NamedPreservationAndAliasFlattening", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// User is a tagged defined record.
// @intercall type
type User struct {
	ID string
}

// Aliased is an alias of User.
type Aliased = User

// @intercall procedure use
func Use(ctx context.Context, u User, a Aliased) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Use")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"user", "user"}) {
			t.Errorf("params = %v, want [user user]", got)
		}
		if len(tm.Types) != 1 || tm.Types[0].WireName != "user" {
			t.Fatalf("types = %v, want exactly [user]", typeWires(tm))
		}
		rec := tm.Types[0]
		if rec.GoName != "User" || rec.Doc != "User is a tagged defined record." {
			t.Errorf("record = %+v", rec)
		}
		if got := typeKeyOf(rec.Type); got != "record{id string}" {
			t.Errorf("underlying = %q", got)
		}
		if rec.ZeroWidth {
			t.Error("User: ZeroWidth = true, want false")
		}
	})

	t.Run("ZeroWidthFacts", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall type
type Nothing struct{}

// @intercall type
type Pair struct {
	A int8
}

// @intercall procedure zw
func Zw(ctx context.Context, n Nothing, p Pair, l []Nothing, e struct{}) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Zw")
		wantValue(t, mp.Params[0].Value, "nothing", true)
		wantValue(t, mp.Params[1].Value, "pair", false)
		wantValue(t, mp.Params[2].Value, "list nothing", false)
		wantValue(t, mp.Params[3].Value, "record{}", true)
		for _, rec := range tm.Types {
			want := rec.WireName == "nothing"
			if rec.ZeroWidth != want {
				t.Errorf("ZeroWidth of %s = %v, want %v", rec.WireName, rec.ZeroWidth, want)
			}
		}
	})

	t.Run("ParenthesizedTypes", func(t *testing.T) {
		// Parentheses are transparent to Go type identity and to the
		// mapping: the inner occurrence maps normally, so `([]byte)` is
		// the `[]byte` occurrence `bytes`, an alias with a
		// parenthesized RHS flattens like any other alias, and the
		// byte/uint8 spelling rule applies to the direct element
		// identifier of a slice node.
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure parens
func Parens(ctx context.Context, a ([]byte), b []([]byte), c []((byte)), v struct{ X ([]byte) }, t T, d (T), b2 B) error { return nil }

// @intercall type
type T ([]byte)

type B = ([]byte)
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Parens")
		want := []string{"bytes", "list bytes", "list uint8", "record{x bytes}", "t", "t", "bytes"}
		if got := paramKeys(mp); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %v, want %v", got, want)
		}
		if len(tm.Types) != 1 || tm.Types[0].WireName != "t" || typeKeyOf(tm.Types[0].Type) != "bytes" {
			t.Errorf("types = %v, want exactly [t over bytes]", typeWires(tm))
		}
	})

	t.Run("MultiNameFields", func(t *testing.T) {
		tm, err := mapOne(t, "example.com/synth", `package synth

import "context"

// @intercall procedure multi
func Multi(ctx context.Context, a, b string) error { return nil }
`)
		if err != nil {
			t.Fatalf("mapping: %v", err)
		}
		mp := providerByName(t, tm, "Multi")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"string", "string"}) {
			t.Errorf("params = %v, want [string string]", got)
		}
		mapErr(t, "example.com/synth2", `package synth

import "context"

// @intercall procedure shared
func Shared(ctx context.Context, v struct{ A, B string `+"`intercall:\"x\"`"+` }) error { return nil }
`, `duplicate wire field name "x"`)
	})

	t.Run("MultiNameContext", func(t *testing.T) {
		// The context parameter is not a wire value and must be
		// declared alone: a multi-name first field would silently drop
		// the extra names, which are wire parameters of interface
		// type.
		mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure bad
func Bad(ctx, ctx2 context.Context, x int8) error { return nil }
`, "the context parameter must be declared alone")
	})

	t.Run("UnsupportedForms", func(t *testing.T) {
		forms := []struct{ name, param, want string }{
			{"Bool", "b bool", "bool is not a wire value"},
			{"Int", "i int", "machine-word integer"},
			{"Uint", "u uint", "machine-word integer"},
			{"Uintptr", "u uintptr", "machine-word integer"},
			{"Complex64", "c complex64", "complex numbers are not wire values"},
			{"Complex128", "c complex128", "complex numbers are not wire values"},
			{"Array", "a [3]int8", "array types are not wire values"},
			{"Pointer", "p *int8", "pointer types are not wire values"},
			{"Map", "m map[string]int8", "map types are not wire values"},
			{"Chan", "c chan int8", "channel types are not wire values"},
			{"Func", "f func()", "function types are not wire values"},
			{"ErrorParam", "e error", "interface types are not wire values"},
			{"AnyParam", "a any", "interface types are not wire values"},
			{"GenericInstantiation", "l List[int]", "generic types and instantiations are rejected"},
		}
		for _, f := range forms {
			t.Run(f.name, func(t *testing.T) {
				mapErr(t, "example.com/synth", `package synth

import "context"

type List[T any] struct{ Items []T }

// @intercall procedure bad
func Bad(ctx context.Context, `+f.param+`) error { return nil }
`, f.want)
			})
		}
		t.Run("UnsafePointer", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import (
	"context"
	"unsafe"
)

// @intercall procedure bad
func Bad(ctx context.Context, p unsafe.Pointer) error { return nil }
`, "unsafe.Pointer is not a wire value")
		})
	})

	t.Run("FieldAndTypeRejections", func(t *testing.T) {
		t.Run("Embedded", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

type Inner struct{ X string }

// @intercall procedure use
func Use(ctx context.Context, v struct {
	Inner
	Y string
}) error { return nil }
`, "embedded fields are not wire fields")
		})
		t.Run("UnexportedField", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure use
func Use(ctx context.Context, v struct {
	x string
	Y string
}) error { return nil }
`, `field "x" of parameter "v" of procedure "example.com/synth.Use" must be exported`)
		})
		t.Run("UnexportedType", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

type hidden struct{ X string }

// @intercall procedure use
func Use(ctx context.Context, h hidden) error { return nil }
`, `reachable type "hidden" must be exported`)
		})
		t.Run("MissingDirective", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

type NoDir struct{ X string }

// @intercall procedure use
func Use(ctx context.Context, n NoDir) error { return nil }
`, `reachable type "NoDir" must have exactly one @intercall type directive`)
		})
		t.Run("ReservedTypeName", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type List struct{ X string }

// @intercall procedure use
func Use(ctx context.Context, l List) error { return nil }
`, `projects to reserved wire name "list"`)
		})
		t.Run("ReservedFieldName", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type T struct{ List string }

// @intercall procedure use
func Use(ctx context.Context, v T) error { return nil }
`, `field "List" of type "T" projects to reserved wire name "list"`)
		})
		t.Run("UnderscoreName", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall procedure use
func Use(ctx context.Context, v struct{ User_ID string }) error { return nil }
`, `field "User_ID" of parameter "v" of procedure "example.com/synth.Use"`, "contains an underscore")
		})
		t.Run("GenericType", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

type Gen[T any] struct{ X T }

// @intercall procedure use
func Use(ctx context.Context, g Gen[int]) error { return nil }
`, "generic types and instantiations are rejected")
		})
	})

	t.Run("TagRejections", func(t *testing.T) {
		forms := []struct{ name, tag, want string }{
			{"Empty", `intercall:""`, "empty intercall tag value"},
			{"Dash", `intercall:"-"`, "not a valid wire name"},
			{"Comma", `intercall:"a,omitempty"`, "comma options are not supported"},
			{"Duplicate", `intercall:"a" intercall:"b"`, "duplicate intercall tag keys"},
			{"Unterminated", `intercall:"a`, "unterminated"},
			{"Unquoted", `intercall:a`, "expected a quoted wire name"},
			{"Trailing", `intercall:"a"x`, "unexpected text after the quoted value"},
			{"Reserved", `intercall:"record"`, "not a valid nonreserved InterCall identifier"},
			{"SpaceInValue", `intercall:"a b"`, "malformed intercall tag"},
		}
		for _, f := range forms {
			t.Run(f.name, func(t *testing.T) {
				mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type T struct {
	F string `+"`"+f.tag+"`"+`
}

// @intercall procedure use
func Use(ctx context.Context, v T) error { return nil }
`, f.want)
			})
		}
	})

	t.Run("RecursionRejected", func(t *testing.T) {
		t.Run("ThroughSlice", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type Node struct {
	Children []Node
}

// @intercall procedure use
func Use(ctx context.Context, n Node) error { return nil }
`, "recursive type graph", `type "node"`)
		})
		t.Run("ThroughRecord", func(t *testing.T) {
			// The cycle runs through an anonymous record: Ring contains an
			// inline record that contains a slice of Ring.
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type Ring struct {
	Inner struct {
		Next []Ring
	}
}

// @intercall procedure use
func Use(ctx context.Context, r Ring) error { return nil }
`, "recursive type graph")
		})
		t.Run("Indirect", func(t *testing.T) {
			mapErr(t, "example.com/synth", `package synth

import "context"

// @intercall type
type A struct {
	Bs []B
}

// @intercall type
type B struct {
	As []A
}

// @intercall procedure use
func Use(ctx context.Context, a A) error { return nil }
`, "recursive type graph")
		})
	})

	t.Run("OutputPackageSelfReference", func(t *testing.T) {
		// A reachable type declared in the output package itself would
		// make the generated binding import its own package.
		_, providers := synthPkg(t, "example.com/synth", map[string]string{"synth.go": `package synth

import "context"

// @intercall type
type User struct{ ID string }

// @intercall procedure use
func Use(ctx context.Context, u User) error { return nil }
`})
		_, err := MapValues(providers, "example.com/synth")
		if err == nil || !strings.Contains(err.Error(), "would import its own package") {
			t.Errorf("error = %v, want a self-import error", err)
		}
	})

	t.Run("MainPackageRejected", func(t *testing.T) {
		_, providers := synthPkg(t, "example.com/synth", map[string]string{"synth.go": `package main

import "context"

// @intercall type
type User struct{ ID string }

// @intercall procedure use
func Use(ctx context.Context, u User) error { return nil }
`})
		_, err := MapValues(providers, "example.com/out")
		if err == nil || !strings.Contains(err.Error(), "main package") {
			t.Errorf("error = %v, want a main-package error", err)
		}
	})
}

// typeWires renders the wire names of a TypeMap's types.
func typeWires(tm *TypeMap) []string {
	var wires []string
	for _, rec := range tm.Types {
		wires = append(wires, rec.WireName)
	}
	return wires
}

// mapFixture is the integration module: a provider package over every
// valid mapping form, dependency packages with reachable types, two
// colliding wire names across packages, an internal package, and the
// output packages.
var mapFixture = map[string]string{
	"go.mod": "module example.com/map\n\ngo 1.26.5\n",
	"vals/vals.go": `// Package vals holds the valid value-mapping forms.
package vals

import "context"

// B is an alias of a byte slice.
type B = []byte

// U is an alias of a uint8 slice.
type U = []uint8

// B2 is an alias of B.
type B2 = B

// User is a tagged defined record.
// @intercall type
type User struct {
	ID string
}

// Aliased is an alias of User.
type Aliased = User

// Payload carries raw bytes.
// @intercall type payload
type Payload []byte

// Codes is a list of uint8.
// @intercall type
type Codes []uint8

// @intercall procedure primitives
func Primitives(ctx context.Context, a int8, b uint16, c float32, d float64, e string, f uint64) error { return nil }

// @intercall procedure byte_forms
func ByteForms(ctx context.Context, raw []byte, list []uint8, nested [][]byte, aliased B, aliasedList U, chained B2, elem []B) error { return nil }

// @intercall procedure anonymous
func Anonymous(ctx context.Context, s struct {
	// The display name.
	Name string
	Tags []string
}, empty struct{}) error { return nil }

// @intercall procedure nested
func Nested(ctx context.Context, v struct {
	Meta struct {
		Count uint32
	}
	Items []struct {
		Key []byte
	}
}) error { return nil }

// @intercall procedure tagged
func Tagged(ctx context.Context, v struct {
	FieldA string ` + "`intercall:\"the_name\"`" + `
	SkipMe int8   ` + "`json:\"ignored\"`" + `
}) error { return nil }

// @intercall procedure named_use
func NamedUse(ctx context.Context, u User, p Payload, c Codes) error { return nil }

// @intercall procedure alias_use
func AliasUse(ctx context.Context, u Aliased) error { return nil }

// @intercall procedure multi_name
func MultiName(ctx context.Context, a, b string) error { return nil }

// @intercall procedure with_result
func WithResult(ctx context.Context, n int8) (User, error) { return User{}, nil }
`,
	"dep/dep.go": `// Package dep holds a reachable dependency type.
package dep

// DepType is reachable from vals.
// @intercall type dep_type
type DepType struct {
	Value string
}

// DB is a cross-package alias of a byte slice.
type DB = []byte
`,
	"c1/c1.go": `// Package c1 holds a type whose wire name collides with c2's.
package c1

// @intercall type conflicting
type Conflicting struct {
	A string
}
`,
	"c2/c2.go": `// Package c2 holds a type whose wire name collides with c1's.
package c2

// @intercall type conflicting
type Conflicting struct {
	B string
}
`,
	"sub/internal/hidden/hidden.go": `// Package hidden is internal to example.com/map/sub.
package hidden

// Hidden is reachable from vals.
// @intercall type hidden_kind
type Hidden struct {
	X string
}
`,
	"sub/use/use.go": `// Package use sits inside the internal root and holds the providers
// that reach internal, colliding, and context types.
package use

import (
	"context"

	"example.com/map/c1"
	"example.com/map/c2"
	"example.com/map/dep"
	"example.com/map/sub/internal/hidden"
)

// @intercall procedure cross_package
func CrossPackage(ctx context.Context, t dep.DepType, h hidden.Hidden, db dep.DB) error { return nil }

// @intercall procedure second_ctx
func SecondCtx(ctx context.Context, ctx2 context.Context) error { return nil }

// @intercall procedure collide
func Collide(ctx context.Context, one c1.Conflicting, two c2.Conflicting) error { return nil }
`,
	"out/out.go": `// Package out is the output target outside the internal root.
package out

// Helper is an ordinary function.
func Helper() {}
`,
	"sub/internal/out/out.go": `// Package out is the output target inside the internal root.
package out

// Helper is an ordinary function.
func Helper() {}
`,
}

// TestTypeMappingIntegration runs the complete discovery-to-mapping
// pipeline over a temporary module: every valid form is mapped from a
// real load graph, the reachable graph spans packages, the
// importability of an internal package depends on the output package's
// position, and colliding wire names across packages are rejected.
func TestTypeMappingIntegration(t *testing.T) {
	dir := writeFixture(t, mapFixture)

	t.Run("Positive", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./vals", "./dep", "./sub/use", "./sub/internal/hidden"},
			[]string{
				"example.com/map/vals.Primitives",
				"example.com/map/vals.ByteForms",
				"example.com/map/vals.Anonymous",
				"example.com/map/vals.Nested",
				"example.com/map/vals.Tagged",
				"example.com/map/vals.NamedUse",
				"example.com/map/vals.AliasUse",
				"example.com/map/vals.MultiName",
				"example.com/map/vals.WithResult",
				"example.com/map/sub/use.CrossPackage",
			}, nil, "sub/internal/out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		tm, err := MapValues(res.Providers, "example.com/map/sub/internal/out")
		if err != nil {
			t.Fatalf("MapValues: %v", err)
		}

		mp := providerByName(t, tm, "Primitives")
		want := []string{"int8", "uint16", "float32", "float64", "string", "uint64"}
		if got := paramKeys(mp); !reflect.DeepEqual(got, want) {
			t.Errorf("primitives = %v, want %v", got, want)
		}

		mp = providerByName(t, tm, "ByteForms")
		want = []string{"bytes", "list uint8", "list bytes", "bytes", "list uint8", "bytes", "list bytes"}
		if got := paramKeys(mp); !reflect.DeepEqual(got, want) {
			t.Errorf("byte_forms = %v, want %v", got, want)
		}

		mp = providerByName(t, tm, "Anonymous")
		wantValue(t, mp.Params[0].Value, "record{name string;tags list string}", false)
		rec := mp.Params[0].Value.Type.(*syntax.RecordType)
		if rec.Fields[0].Doc != "The display name." {
			t.Errorf("field doc = %q", rec.Fields[0].Doc)
		}
		wantValue(t, mp.Params[1].Value, "record{}", true)

		mp = providerByName(t, tm, "Nested")
		wantValue(t, mp.Params[0].Value, "record{meta record{count uint32};items list record{key bytes}}", false)

		mp = providerByName(t, tm, "Tagged")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"record{the_name string;skip_me int8}"}) {
			t.Errorf("tagged = %v", got)
		}

		mp = providerByName(t, tm, "NamedUse")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"user", "payload", "codes"}) {
			t.Errorf("named_use = %v", got)
		}

		mp = providerByName(t, tm, "AliasUse")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"user"}) {
			t.Errorf("alias_use = %v", got)
		}

		mp = providerByName(t, tm, "MultiName")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"string", "string"}) {
			t.Errorf("multi_name = %v", got)
		}

		mp = providerByName(t, tm, "WithResult")
		if mp.Result == nil || typeKeyOf(mp.Result.Type) != "user" {
			t.Errorf("result = %+v, want a user reference", mp.Result)
		}

		mp = providerByName(t, tm, "CrossPackage")
		if got := paramKeys(mp); !reflect.DeepEqual(got, []string{"dep_type", "hidden_kind", "bytes"}) {
			t.Errorf("cross_package = %v", got)
		}

		// The reachable named graph in the stable topological order: all
		// types are independent, so the lexicographically smallest wire
		// name wins at every step.
		wantWires := []string{"codes", "dep_type", "hidden_kind", "payload", "user"}
		if got := typeWires(tm); !reflect.DeepEqual(got, wantWires) {
			t.Errorf("types = %v, want %v", got, wantWires)
		}
		underlying := map[string]string{
			"codes":       "list uint8",
			"dep_type":    "record{value string}",
			"hidden_kind": "record{x string}",
			"payload":     "bytes",
			"user":        "record{id string}",
		}
		for _, rec := range tm.Types {
			if got := typeKeyOf(rec.Type); got != underlying[rec.WireName] {
				t.Errorf("underlying of %s = %q, want %q", rec.WireName, got, underlying[rec.WireName])
			}
			if rec.ZeroWidth {
				t.Errorf("ZeroWidth of %s = true, want false", rec.WireName)
			}
		}
	})

	t.Run("InternalInvisible", func(t *testing.T) {
		// The same provider package, but the output package sits outside
		// the internal root: the reachable internal type is not
		// importable.
		res, err := discover(t, dir, []string{"./sub/use"}, []string{"example.com/map/sub/use.CrossPackage"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapValues(res.Providers, "example.com/map/out")
		wantErr(t, err, "reachable type", "example.com/map/sub/internal/hidden", "internal and not visible", "example.com/map/out")
	})

	t.Run("WireNameCollision", func(t *testing.T) {
		res, err := discover(t, dir, []string{"./sub/use"}, []string{"example.com/map/sub/use.Collide"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapValues(res.Providers, "example.com/map/out")
		wantErr(t, err, "wire name collision", "Conflicting", "conflicting")
	})

	t.Run("ContextAsValueRejected", func(t *testing.T) {
		// context.Context is legal only as the first parameter; in a
		// value position it is a reachable named type without an
		// @intercall type directive.
		res, err := discover(t, dir, []string{"./sub/use"}, []string{"example.com/map/sub/use.SecondCtx"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		_, err = MapValues(res.Providers, "example.com/map/out")
		wantErr(t, err, `reachable type "Context"`, "must have exactly one @intercall type directive")
	})
}

// TestNamedTypeOrder covers the stable topological emission order:
// among the types whose named dependencies have already been emitted,
// the lexicographically smallest exact wire name is chosen, independent
// of Go declaration order and package loading order.
func TestNamedTypeOrder(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"go.mod": "module example.com/ord\n\ngo 1.26.5\n",
		"ord.go": `// Package ord holds a dependent type chain.
package ord

import (
	"context"

	"example.com/ord/dep"
)

// @intercall type alpha
type Alpha struct {
	M Middle
}

// @intercall type middle
type Middle struct {
	Z Zeta
}

// @intercall type zeta
type Zeta struct {
	N int8
}

// @intercall type delta
type Delta struct {
	B dep.Base
}

// @intercall procedure use_all
func UseAll(ctx context.Context, a Alpha, d Delta, m Middle, z Zeta) error { return nil }
`,
		"dep/dep.go": `// Package dep holds the cross-package base type.
package dep

// @intercall type base
type Base struct {
	V string
}
`,
		"out/out.go": `// Package out is the output target.
package out

// Helper is an ordinary function.
func Helper() {}
`,
	})

	load := func(t *testing.T) *TypeMap {
		t.Helper()
		res, err := discover(t, dir, []string{".", "./dep"}, []string{"example.com/ord.UseAll"}, nil, "out")
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		tm, err := MapValues(res.Providers, "example.com/ord/out")
		if err != nil {
			t.Fatalf("MapValues: %v", err)
		}
		return tm
	}

	// alpha needs middle, middle needs zeta, and delta needs the
	// cross-package base. The ready set starts as {zeta, base}; base is
	// emitted first, then delta, then zeta, middle, and alpha.
	first := load(t)
	want := []string{"base", "delta", "zeta", "middle", "alpha"}
	if got := typeWires(first); !reflect.DeepEqual(got, want) {
		t.Errorf("types = %v, want %v", got, want)
	}

	// A second complete mapping pass produces the identical order and
	// identical records.
	second := load(t)
	if got := typeWires(second); !reflect.DeepEqual(got, want) {
		t.Errorf("second pass types = %v, want %v", got, want)
	}
	for i := range first.Types {
		a, b := first.Types[i], second.Types[i]
		if a.WireName != b.WireName || a.GoName != b.GoName || a.PkgPath != b.PkgPath || typeKeyOf(a.Type) != typeKeyOf(b.Type) {
			t.Errorf("record %d differs: %+v vs %+v", i, a, b)
		}
	}
}

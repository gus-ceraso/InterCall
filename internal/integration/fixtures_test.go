package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/cerasos/intercall"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eexport"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/internal/integration/fixtures/provider"
)

// TestGeneratedFixtureModules exercises the checked-in generated
// fixture modules as compiled black-box peers: the immutable binding
// handles, the no-connection path of the generated callers, and every
// primitive and value shape, named type, deeply nested anonymous
// record, and exception shape of the fixture interface over one live
// connection. The fixture packages compile as real dependencies of this
// test binary, so a generated source that does not compile fails here
// before any runtime scenario can run.
func TestGeneratedFixtureModules(t *testing.T) {
	// The generated bindings are process singletons with fresh identity.
	if e2eexport.ExportBinding() == (intercall.ExportBinding{}) {
		t.Fatal("export binding handle is the zero value")
	}
	if e2eimport.ImportBinding() == (intercall.ImportBinding{}) {
		t.Fatal("import binding handle is the zero value")
	}

	// A generated caller without a bound connection returns
	// ErrNoConnection without constructing a frame.
	if err := e2eimport.Ping(context.Background()); err != intercall.ErrNoConnection {
		t.Fatalf("Ping without a connection: %v, want ErrNoConnection", err)
	}

	provider.ResetMessages()
	a, b := newPair(t)
	ctx := bind(a)

	if err := e2eimport.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Strings and all eight exact-width integer primitives.
	got, err := e2eimport.Echo(ctx, "hello")
	if err != nil || got != "hello" {
		t.Fatalf("Echo(hello) = %q, %v", got, err)
	}
	wantSum := int64(-100) + int64(1000) + int64(-100000) + int64(-1<<40) +
		int64(200) + int64(60000) + int64(4000000000) + int64(1<<62)
	if got, err := e2eimport.Add(ctx, -100, 1000, -100000, -1<<40, 200, 60000, 4000000000, 1<<62); err != nil || got != wantSum {
		t.Fatalf("Add = %d, %v; want %d", got, err, wantSum)
	}

	// Floats: finite values, signed zero, infinities, and a canonical
	// NaN round trip. (The untyped constant -0.0 converts to positive
	// zero, so negative zero is built with math.Copysign.)
	if got, err := e2eimport.Measure(ctx, 1.5, 2.25); err != nil || got != 3.75 {
		t.Fatalf("Measure(1.5, 2.25) = %v, %v", got, err)
	}
	nz32, nz64 := float32(math.Copysign(0, -1)), math.Copysign(0, -1)
	if got, err := e2eimport.Measure(ctx, nz32, nz64); err != nil || !math.Signbit(got) {
		t.Fatalf("Measure(-0.0, -0.0) = %v, %v; want negative zero", got, err)
	}
	if got, err := e2eimport.Measure(ctx, 0.0, 0.0); err != nil || math.Signbit(got) {
		t.Fatalf("Measure(0.0, 0.0) = %v, %v; want positive zero", got, err)
	}
	if got, err := e2eimport.Measure(ctx, float32(math.Inf(1)), 0); err != nil || !math.IsInf(got, 1) {
		t.Fatalf("Measure(+Inf, 0) = %v, %v", got, err)
	}
	if got, err := e2eimport.Measure(ctx, float32(math.Inf(-1)), 0); err != nil || !math.IsInf(got, -1) {
		t.Fatalf("Measure(-Inf, 0) = %v, %v", got, err)
	}
	if got, err := e2eimport.Measure(ctx, float32(math.NaN()), 2); err != nil || !math.IsNaN(got) {
		t.Fatalf("Measure(NaN, 2) = %v, %v; want NaN", got, err)
	}

	// bytes versus list uint8: nil encodes as empty and decodes to a
	// nonnil zero-length slice.
	if got, err := e2eimport.Sample(ctx, []byte{0, 0xff, 1, 2}, 9); err != nil || !bytes.Equal(got, []byte{0, 0xff, 1, 2}) {
		t.Fatalf("Sample = %v, %v", got, err)
	}
	if got, err := e2eimport.Sample(ctx, nil, 0); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Sample(nil) = %v, %v; want a nonnil empty slice", got, err)
	}
	if got, err := e2eimport.Wave(ctx, []uint8{9, 8, 7}); err != nil || got != 3 {
		t.Fatalf("Wave = %d, %v", got, err)
	}
	if got, err := e2eimport.Wave(ctx, nil); err != nil || got != 0 {
		t.Fatalf("Wave(nil) = %d, %v", got, err)
	}

	// Named scalar, record, list, and bytes types.
	if got, err := e2eimport.Scale(ctx, 2.5, e2eimport.Point{X: 2, Y: 4}); err != nil || got != (e2eimport.Point{X: 5, Y: 10}) {
		t.Fatalf("Scale = %+v, %v", got, err)
	}
	if got, err := e2eimport.Render(ctx, []e2eimport.Pixel{{Red: 1, Green: 2, Blue: 3}, {Red: 255, Green: 0, Blue: 128}}, 5); err != nil || !reflect.DeepEqual(got, []e2eimport.Pixel{{Red: 1, Green: 2, Blue: 3}, {Red: 255, Green: 0, Blue: 128}}) {
		t.Fatalf("Render = %+v, %v", got, err)
	}
	if got, err := e2eimport.Find(ctx, "ab", []e2eimport.Name{"abc", "xab", "abz"}); err != nil || !reflect.DeepEqual(got, e2eimport.Names{"abc", "abz"}) {
		t.Fatalf("Find = %v, %v", got, err)
	}
	if got, err := e2eimport.Account(ctx, e2eimport.CustomerID(42)); err != nil || got != e2eimport.UserID(42) {
		t.Fatalf("Account = %d, %v", got, err)
	}
	if got, err := e2eimport.Fetch(ctx, e2eimport.UserID(9)); err != nil || string(got) != "blob-9" {
		t.Fatalf("Fetch = %q, %v", got, err)
	}

	// Nested lists of named types.
	if got, err := e2eimport.Transform(ctx, e2eimport.Matrix{{1, 2}, {3}}); err != nil || !reflect.DeepEqual(got, e2eimport.Matrix{{1, 2}, {3}}) {
		t.Fatalf("Transform = %v, %v", got, err)
	}

	// Anonymous inline records, including four-level nesting and lists
	// of anonymous records.
	if got, err := e2eimport.Paint(ctx, PaintOrigin{X: 1, Y: 2}, PaintColor{Red: 10, Green: 20, Blue: 30, Alpha: 40}); err != nil || got != (PaintSize{Width: 41, Height: 32}) {
		t.Fatalf("Paint = %+v, %v", got, err)
	}
	if got, err := e2eimport.Locate(ctx, LocateBox{
		Corner: LocateCorner{Inner: LocateInner{X: 5, Y: 6}, Label: "L"},
		Tag:    7,
	}); err != nil || got != (LocateResult{Corner: e2eimport.Point{X: 5, Y: 6}, Area: 70}) {
		t.Fatalf("Locate = %+v, %v", got, err)
	}
	if got, err := e2eimport.Grid(ctx, []GridRow{{Origin: GridOrigin{X: -1, Y: 2}, Value: 10}, {Origin: GridOrigin{X: 3, Y: -4}, Value: 5}}); err != nil || got != 15 {
		t.Fatalf("Grid = %d, %v", got, err)
	}

	// Zero-width values: empty record parameters, returns, and list
	// elements.
	if got, err := e2eimport.Blanks(ctx, 4); err != nil || len(got) != 4 {
		t.Fatalf("Blanks(4) = %d elements, %v", len(got), err)
	}
	if got, err := e2eimport.Blanks(ctx, 0); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Blanks(0) = %v, %v; want a nonnil empty slice", got, err)
	}
	if got, err := e2eimport.Tiny(ctx, e2eimport.Empty{}); err != nil || got != 7 {
		t.Fatalf("Tiny = %d, %v", got, err)
	}
	if got, err := e2eimport.Stamp(ctx); err != nil || got != (e2eimport.Empty{}) {
		t.Fatalf("Stamp = %+v, %v", got, err)
	}

	// Every exception shape: the no-payload sentinel, a record payload,
	// a zero-field record payload, and a payload holding a named type.
	if _, err := e2eimport.Echo(ctx, "denied"); err != e2eimport.Denied {
		t.Fatalf("Echo(denied) err = %v, want the denied sentinel", err)
	}
	var fe *e2eimport.Failed
	if _, err := e2eimport.Echo(ctx, "failed"); !errors.As(err, &fe) {
		t.Fatalf("Echo(failed) err = %v, want *Failed", err)
	} else if fe.Code != 7 || fe.Message != "boom" {
		t.Fatalf("Echo(failed) err = %v, want *Failed{Code:7, Message:boom}", err)
	}
	var bl *e2eimport.Blank
	if _, err := e2eimport.Echo(ctx, "blank"); !errors.As(err, &bl) {
		t.Fatalf("Echo(blank) err = %v, want *Blank", err)
	}
	var pf *e2eimport.PointFailed
	if _, err := e2eimport.Echo(ctx, "point_failed"); !errors.As(err, &pf) {
		t.Fatalf("Echo(point_failed) err = %v, want *PointFailed", err)
	} else if pf.Point != (e2eimport.Point{X: 1.5, Y: -2.5}) {
		t.Fatalf("Echo(point_failed) err = %v, want *PointFailed{Point{1.5,-2.5}}", err)
	}

	// Provider failures map to the fixed internal_exception: a provider
	// panic, an unmatchable wrapped error, a typed-nil payload pointer,
	// and an encoder-rejected success value.
	for _, mode := range []string{"panic", "wrapped", "typed_nil", "bad_utf8"} {
		if _, err := e2eimport.Echo(ctx, mode); err != intercall.ErrInternalException {
			t.Fatalf("Echo(%s) err = %v, want ErrInternalException", mode, err)
		}
	}

	// Notification recording crosses the wire in the call direction.
	if err := e2eimport.Notify(ctx, "smoke-note"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if msgs := provider.Messages(); !reflect.DeepEqual(msgs, []string{"smoke-note"}) {
		t.Fatalf("Messages = %v, want [smoke-note]", msgs)
	}

	// Clean shutdown: the closing side reports the exact ErrClosed cause
	// and the peer terminates on the EOF.
	closeAndWait(t, a)
	if err := b.Wait(); !errors.Is(err, io.EOF) {
		t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
	}
	requireNoLeaks(t)
}

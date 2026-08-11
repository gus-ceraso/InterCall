package intercall

import (
	"context"
	"testing"
	"unsafe"
)

// TestIdentityStateNonZeroSized pins the SPEC.md requirement that identity
// state is non-zero-sized, so pointers to independently constructed states
// are always distinct.
func TestIdentityStateNonZeroSized(t *testing.T) {
	if unsafe.Sizeof(importState{}) == 0 {
		t.Error("importState is zero-sized")
	}
	if unsafe.Sizeof(exportState{}) == 0 {
		t.Error("exportState is zero-sized")
	}
}

// TestZeroValueHasNilState pins that the nil-pointer zero value of a handle
// is invalid.
func TestZeroValueHasNilState(t *testing.T) {
	var e ExportBinding
	if e.state != nil {
		t.Error("zero ExportBinding has non-nil state")
	}
	var i ImportBinding
	if i.state != nil {
		t.Error("zero ImportBinding has non-nil state")
	}
}

// TestCopySharesStatePointer pins that copying a handle copies the pointer
// and retains identity.
func TestCopySharesStatePointer(t *testing.T) {
	i := NewImportBinding()
	if c := i; c.state != i.state {
		t.Error("import copy does not share the state pointer")
	}
	e := newTestExport(t)
	if c := e; c.state != e.state {
		t.Error("export copy does not share the state pointer")
	}
}

// TestConstructedStatesAreDistinct pins that independently constructed
// handles have distinct state addresses.
func TestConstructedStatesAreDistinct(t *testing.T) {
	a := NewImportBinding()
	b := NewImportBinding()
	if a.state == b.state {
		t.Error("independently constructed import states share an address")
	}
	e1 := newTestExport(t)
	e2 := newTestExport(t)
	if e1.state == e2.state {
		t.Error("independently constructed export states share an address")
	}
}

// TestExportStateRetainsDispatch pins that export state carries its dispatch
// function.
func TestExportStateRetainsDispatch(t *testing.T) {
	want := func(context.Context, uint64, []byte) (uint64, []byte) {
		return 7, []byte("payload")
	}
	b, err := NewExportBinding(want)
	if err != nil {
		t.Fatal(err)
	}
	if b.state.dispatch == nil {
		t.Fatal("dispatch function not retained")
	}
	key, payload := b.state.dispatch(context.Background(), 1, nil)
	if key != 7 || string(payload) != "payload" {
		t.Errorf("retained dispatch = (%d, %q), want (7, \"payload\")", key, payload)
	}
}

func newTestExport(t *testing.T) ExportBinding {
	t.Helper()
	b, err := NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) {
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

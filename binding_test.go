package intercall_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	intercall "github.com/cerasos/intercall"
)

// testStream is a minimal transport stream implementing ByteStream.
type testStream struct{}

func (testStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (testStream) Write(p []byte) (int, error) { return len(p), nil }
func (testStream) Close() error                { return nil }

// Compile-time checks that the exported SPI signatures are exactly the
// generated-code contract from SPEC.md.
var (
	_ intercall.ByteStream = testStream{}

	_ intercall.Dispatch = func(context.Context, uint64, []byte) (uint64, []byte) {
		return 0, nil
	}

	_ intercall.RequestEncoder = func() ([]byte, error) { return nil, nil }

	_ intercall.ResponseDecoder = func(uint64, []byte) error { return nil }
)

// dummyDispatch is a fixed dispatch function used to distinguish identity
// tests from behavior tests.
func dummyDispatch(context.Context, uint64, []byte) (uint64, []byte) {
	return 0, nil
}

func TestNewExportBindingRejectsNilDispatch(t *testing.T) {
	b, err := intercall.NewExportBinding(nil)
	if err == nil {
		t.Fatal("NewExportBinding(nil) succeeded")
	}
	if err != intercall.ErrInvalidArgument {
		t.Errorf("err = %v, want ErrInvalidArgument by direct comparison", err)
	}
	if !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Error("errors.Is(err, ErrInvalidArgument) = false")
	}
	if !errors.Is(fmt.Errorf("wrap: %w", err), intercall.ErrInvalidArgument) {
		t.Error("errors.Is through %w wrapping = false")
	}
	if b != (intercall.ExportBinding{}) {
		t.Error("failed construction returned a non-zero handle")
	}
}

func TestImportBindingIdentity(t *testing.T) {
	a := intercall.NewImportBinding()
	if a == (intercall.ImportBinding{}) {
		t.Error("constructed handle compares equal to the zero value")
	}
	b := intercall.NewImportBinding()
	if a == b {
		t.Error("independently constructed handles compare equal")
	}
	c := a
	if c != a {
		t.Error("copied handle lost identity")
	}
	d := b
	if d != b || d == a {
		t.Error("copies of distinct handles disagree on identity")
	}
}

func TestExportBindingIdentity(t *testing.T) {
	a, err := intercall.NewExportBinding(dummyDispatch)
	if err != nil {
		t.Fatal(err)
	}
	if a == (intercall.ExportBinding{}) {
		t.Error("constructed handle compares equal to the zero value")
	}
	// A second construction with the same dispatch still gets fresh state.
	b, err := intercall.NewExportBinding(dummyDispatch)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("independently constructed handles with the same dispatch compare equal")
	}
	c, err := intercall.NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) {
		return 1, []byte("x")
	})
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Error("independently constructed handles with different dispatches compare equal")
	}
	cp := a
	if cp != a {
		t.Error("copied export handle lost identity")
	}
}

func TestZeroValueComparability(t *testing.T) {
	var zeroE intercall.ExportBinding
	var zeroI intercall.ImportBinding
	if zeroE != (intercall.ExportBinding{}) {
		t.Error("zero ExportBinding does not compare equal to itself")
	}
	if zeroI != (intercall.ImportBinding{}) {
		t.Error("zero ImportBinding does not compare equal to itself")
	}
	// The zero value never equals a constructed handle.
	if zeroE == mustExport(t) {
		t.Error("zero ExportBinding equals a constructed handle")
	}
	if zeroI == intercall.NewImportBinding() {
		t.Error("zero ImportBinding equals a constructed handle")
	}
}

// mustExport constructs an export handle, failing the test on error.
func mustExport(t *testing.T) intercall.ExportBinding {
	t.Helper()
	b, err := intercall.NewExportBinding(dummyDispatch)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestConcurrentSharing exercises shared handles and concurrent construction
// from many goroutines; it must hold under -race.
func TestConcurrentSharing(t *testing.T) {
	const goroutines = 32
	const iterations = 200
	shared := intercall.NewImportBinding()
	exported := mustExport(t)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Copies of shared handles retain identity.
				cp := shared
				if cp != shared {
					t.Error("shared import handle identity lost under copy")
					return
				}
				cpE := exported
				if cpE != exported {
					t.Error("shared export handle identity lost under copy")
					return
				}
				// Concurrent construction always yields fresh identity.
				fresh := intercall.NewImportBinding()
				if fresh == shared || fresh == (intercall.ImportBinding{}) {
					t.Error("concurrent import construction collided with a shared handle")
					return
				}
				freshE, err := intercall.NewExportBinding(dummyDispatch)
				if err != nil {
					t.Error(err)
					return
				}
				if freshE == exported || freshE == (intercall.ExportBinding{}) {
					t.Error("concurrent export construction collided with a shared handle")
					return
				}
			}
		}()
	}
	wg.Wait()
}

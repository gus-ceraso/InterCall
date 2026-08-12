package intercall_test

import (
	"errors"
	"fmt"
	"testing"

	intercall "github.com/cerasos/intercall/go"
)

// TestWireSentinels checks the exact text, direct comparison, and errors.Is
// behavior of the three fixed Go runtime wire exceptions.
func TestWireSentinels(t *testing.T) {
	wires := []struct {
		sentinel error
		text     string
	}{
		{intercall.ErrProcedureNotFound, "procedure_not_found"},
		{intercall.ErrInvalidArguments, "invalid_arguments"},
		{intercall.ErrInternalException, "internal_exception"},
	}
	for _, w := range wires {
		if w.sentinel.Error() != w.text {
			t.Errorf("Error() = %q, want %q", w.sentinel.Error(), w.text)
		}
		var err error = w.sentinel
		if err != w.sentinel {
			t.Errorf("%q lost identity through the error interface", w.text)
		}
		if !errors.Is(err, w.sentinel) {
			t.Errorf("errors.Is(%q) = false", w.text)
		}
		if !errors.Is(fmt.Errorf("wrap: %w", w.sentinel), w.sentinel) {
			t.Errorf("errors.Is through %%w wrapping = false for %q", w.text)
		}
	}
	// The three wire sentinels are distinct values.
	if intercall.ErrProcedureNotFound == intercall.ErrInvalidArguments ||
		intercall.ErrProcedureNotFound == intercall.ErrInternalException ||
		intercall.ErrInvalidArguments == intercall.ErrInternalException {
		t.Error("wire sentinels are not pairwise distinct")
	}
}

// TestLocalSentinels checks the local error classifications for nonnil
// nonempty text, self-identity, errors.Is, and mutual distinctness.
func TestLocalSentinels(t *testing.T) {
	locals := []error{
		intercall.ErrInvalidArgument,
		intercall.ErrNoConnection,
		intercall.ErrBindingMismatch,
		intercall.ErrClosed,
		intercall.ErrRequestIDsExhausted,
		intercall.ErrProtocol,
	}
	wires := []error{
		intercall.ErrProcedureNotFound,
		intercall.ErrInvalidArguments,
		intercall.ErrInternalException,
	}
	all := append(append([]error(nil), locals...), wires...)

	for _, e := range all {
		if e == nil {
			t.Error("nil sentinel")
			continue
		}
		if e.Error() == "" {
			t.Error("empty sentinel text")
		}
		if e != e {
			t.Error("sentinel not directly comparable to itself")
		}
		if !errors.Is(e, e) {
			t.Error("errors.Is(e, e) = false")
		}
		if !errors.Is(fmt.Errorf("wrap: %w", e), e) {
			t.Error("errors.Is through %w wrapping = false")
		}
	}

	// Local classifications are distinct from each other, from the wire
	// sentinels, and carry distinct text.
	texts := make(map[string]bool, len(all))
	for i, a := range all {
		if texts[a.Error()] {
			t.Errorf("sentinel text %q is not unique", a.Error())
		}
		texts[a.Error()] = true
		for j, b := range all {
			if i != j && a == b {
				t.Errorf("sentinels %q and %q compare equal", a.Error(), b.Error())
			}
		}
	}
}

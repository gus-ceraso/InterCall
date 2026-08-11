package tool

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// This file tests the interrupted two-target export repair of SPEC.md
// "One-file ownership and safe replacement": different existing stamps,
// or exactly one missing owned target, mean a prior update was
// interrupted, and the next successful invocation deterministically
// repairs both targets without a manifest or deletion. It never
// endangers an unowned file. Every test operates only in temporary
// directories.

func TestArtifactInterruptedExportRepair(t *testing.T) {
	// setup creates a fresh export pair and returns the config of the
	// first and second write.
	setup := func(t *testing.T) (out, intfDir string, cfg1, cfg2 WriteConfig) {
		t.Helper()
		root := t.TempDir()
		out = filepath.Join(root, "out")
		intfDir = filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		cfg1 = WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: canonicalBody(t, testInterfaceSrc1),
		}
		cfg2 = cfg1
		cfg2.InterfaceBody = canonicalBody(t, testInterfaceSrc2)
		return out, intfDir, cfg1, cfg2
	}

	assertPair := func(t *testing.T, out, intfDir string, body []byte) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(out, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if want := composedBinding(ExportMode, ArtifactStamp(body), []byte(testBindingGo)); !bytes.Equal(got, want) {
			t.Errorf("binding = %q, want %q", got, want)
		}
		intf, err := os.ReadFile(filepath.Join(intfDir, "api.intercall"))
		if err != nil {
			t.Fatal(err)
		}
		if want := ownedInterfaceBytes(body); !bytes.Equal(intf, want) {
			t.Errorf("interface = %q, want %q", intf, want)
		}
	}

	t.Run("BindingNewInterfaceOld", func(t *testing.T) {
		// The binding was already replaced when the interface rename
		// failed; the stamps differ and the next run repairs both.
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		body2 := cfg2.InterfaceBody
		if err := os.WriteFile(filepath.Join(out, bindingFile), composedBinding(ExportMode, ArtifactStamp(body2), []byte(testBindingGo)), 0o644); err != nil {
			t.Fatal(err)
		}
		// The pair is binding v2 (new) + interface v1 (old): differing
		// stamps; the repair replaces both.
		writeOnce(t, cfg2)
		assertPair(t, out, intfDir, body2)
	})

	t.Run("InterfaceNewBindingOld", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		body2 := cfg2.InterfaceBody
		intf := filepath.Join(intfDir, "api.intercall")
		// The interface was already replaced when the binding rename
		// failed.
		if err := os.WriteFile(intf, ownedInterfaceBytes(body2), 0o644); err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		assertPair(t, out, intfDir, body2)
	})

	t.Run("MissingInterface", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		body2 := cfg2.InterfaceBody
		if err := os.Remove(filepath.Join(intfDir, "api.intercall")); err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		assertPair(t, out, intfDir, body2)
	})

	t.Run("MissingBinding", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		body2 := cfg2.InterfaceBody
		if err := os.Remove(filepath.Join(out, bindingFile)); err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		assertPair(t, out, intfDir, body2)
	})

	t.Run("RepairReplacesBothEvenWhenOneMatches", func(t *testing.T) {
		// When the existing stamps differ, the repair replaces both
		// targets, including one whose bytes already match the new
		// content.
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		body2 := cfg2.InterfaceBody
		binding := filepath.Join(out, bindingFile)
		// Simulate an interruption right after the binding rename: the
		// binding is already the new one and the interface is old.
		if err := os.WriteFile(binding, composedBinding(ExportMode, ArtifactStamp(body2), []byte(testBindingGo)), 0o644); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		after, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(before, after) {
			t.Error("the already-new binding was not replaced during the repair")
		}
		assertPair(t, out, intfDir, body2)
	})

	t.Run("UnownedInterfaceBlocksRepair", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		binding := filepath.Join(out, bindingFile)
		intf := filepath.Join(intfDir, "api.intercall")
		bindingBefore, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		// A tampered interface is an unowned collision, not recoverable
		// ownership state.
		if err := os.WriteFile(intf, append(ownedInterfaceBytes(canonicalBody(t, testInterfaceSrc1)), ' '), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFails(t, cfg2, "not replaceable")
		bindingAfter, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(bindingBefore, bindingAfter) {
			t.Error("binding was replaced despite the unowned interface")
		}
		if got, _ := os.ReadFile(binding); !bytes.Equal(got, composedBinding(ExportMode, ArtifactStamp(canonicalBody(t, testInterfaceSrc1)), []byte(testBindingGo))) {
			t.Error("binding bytes changed despite the unowned interface")
		}
	})

	t.Run("UnownedBindingBlocksRepair", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		intf := filepath.Join(intfDir, "api.intercall")
		intfBefore, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		// A handwritten binding is an unowned collision.
		if err := os.WriteFile(filepath.Join(out, bindingFile), []byte("package gen\n\nvar x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFails(t, cfg2, "not replaceable")
		intfAfter, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(intfBefore, intfAfter) {
			t.Error("interface was replaced despite the unowned binding")
		}
	})

	t.Run("GoCollisionBlocksRepair", func(t *testing.T) {
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		binding := filepath.Join(out, bindingFile)
		intf := filepath.Join(intfDir, "api.intercall")
		bindingBefore, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		intfBefore, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "other.go"), []byte("package other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFails(t, cfg2, "other.go")
		bindingAfter, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		intfAfter, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(bindingBefore, bindingAfter) || !os.SameFile(intfBefore, intfAfter) {
			t.Error("a target was replaced despite the Go collision")
		}
	})

	t.Run("RepeatedRepairIsStable", func(t *testing.T) {
		// After a repair both stamps agree and the next identical run
		// is a no-op.
		out, intfDir, cfg1, cfg2 := setup(t)
		writeOnce(t, cfg1)
		if err := os.Remove(filepath.Join(intfDir, "api.intercall")); err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		binding := filepath.Join(out, bindingFile)
		intf := filepath.Join(intfDir, "api.intercall")
		bindingBefore, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		intfBefore, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg2)
		bindingAfter, err := os.Stat(binding)
		if err != nil {
			t.Fatal(err)
		}
		intfAfter, err := os.Stat(intf)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(bindingBefore, bindingAfter) || !os.SameFile(intfBefore, intfAfter) {
			t.Error("repeated identical run replaced targets")
		}
	})
}

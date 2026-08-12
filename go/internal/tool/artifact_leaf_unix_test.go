//go:build unix

package tool

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// This file tests the non-following rejection of nonregular target
// leaves on unix hosts: a FIFO and (as root) a character device are
// inspected with non-following file status and are errors exactly like
// symlinks and directories (SPEC.md "One-file ownership and safe
// replacement"). Every test operates only in temporary directories.

// fifoAt creates a named pipe at path.
func fifoAt(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactFIFOLeafRejection(t *testing.T) {
	body := canonicalBody(t, testInterfaceSrc1)
	root := t.TempDir()

	// A FIFO at the binding leaf is an error.
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o777); err != nil {
		t.Fatal(err)
	}
	fifoAt(t, filepath.Join(out, bindingFile))
	writeFails(t, WriteConfig{
		Mode:          ExportMode,
		OutDir:        out,
		Package:       "gen",
		InterfacePath: filepath.Join(root, "api.intercall"),
		GoFile:        []byte(testBindingGo),
		InterfaceBody: body,
	}, "named pipe")

	// A FIFO at the interface leaf is an error.
	out2 := filepath.Join(root, "out2")
	if err := os.MkdirAll(out2, 0o777); err != nil {
		t.Fatal(err)
	}
	intf := filepath.Join(root, "api-fifo.intercall")
	fifoAt(t, intf)
	writeFails(t, WriteConfig{
		Mode:          ExportMode,
		OutDir:        out2,
		Package:       "gen",
		InterfacePath: intf,
		GoFile:        []byte(testBindingGo),
		InterfaceBody: body,
	}, "named pipe")

	// A FIFO with a ".go" name in the output directory is a Go-named
	// nonregular entry.
	out3 := filepath.Join(root, "out3")
	if err := os.MkdirAll(out3, 0o777); err != nil {
		t.Fatal(err)
	}
	fifoAt(t, filepath.Join(out3, "named.go"))
	writeFails(t, WriteConfig{
		Mode:          ImportMode,
		OutDir:        out3,
		Package:       "gen",
		GoFile:        []byte(testBindingGo),
		InterfaceBody: body,
	}, "Go-named nonregular entry")
}

func TestArtifactDeviceLeafRejection(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("creating a device node requires root")
	}
	body := canonicalBody(t, testInterfaceSrc1)
	root := t.TempDir()
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o777); err != nil {
		t.Fatal(err)
	}
	// A character device at the binding leaf is inspected with
	// non-following status and is an error.
	if err := syscall.Mknod(filepath.Join(out, bindingFile), syscall.S_IFCHR|0o644, 0); err != nil {
		t.Skipf("mknod failed: %v", err)
	}
	writeFails(t, WriteConfig{
		Mode:          ExportMode,
		OutDir:        out,
		Package:       "gen",
		InterfacePath: filepath.Join(root, "api.intercall"),
		GoFile:        []byte(testBindingGo),
		InterfaceBody: body,
	}, "device")
}

//go:build unix

package unixsocket

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"

	intercall "github.com/cerasos/intercall/go"
)

func TestListenStreamPermissionsAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "socket")
	if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := listener.Addr().String(); got != path {
		t.Fatalf("Addr() = %q, want %q", got, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %04o, want 0600", got)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket after close: err = %v, want not exist", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
}

func TestListenStreamRejectsExistingPath(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		make func(string) error
	}{
		{"file", func(path string) error { return os.WriteFile(path, []byte{}, 0o600) }},
		{"directory", func(path string) error { return os.Mkdir(path, 0o700) }},
		{"symlink", func(path string) error { return os.Symlink("target", path) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := tc.make(path); err != nil {
				t.Fatal(err)
			}
			if _, err := ListenStream(path, nil); err == nil {
				t.Fatal("ListenStream unexpectedly succeeded")
			}
		})
	}
}

func TestListenStreamAnchorsRelativePath(t *testing.T) {
	first, err := os.MkdirTemp("", "intercall-first-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(first)
	second, err := os.MkdirTemp("", "intercall-second-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(second)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(first, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(first); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	listener, err := ListenStream("./a/../socket", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(first, "a", "..", "socket")); err != nil {
		t.Fatalf("anchored socket missing: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(first, "a", "..", "socket")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("anchored socket after close: %v", err)
	}
}

func TestListenerAccept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := ListenStream(path, &ListenOptions{Mode: 0o640})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := server.Read(buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "x" {
		t.Fatalf("read %q, want x", buf)
	}
}

func TestListenerNilReceiver(t *testing.T) {
	var listener *Listener
	if _, err := listener.AcceptStream(); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("AcceptStream() = %v", err)
	}
	if err := listener.Close(); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("Close() = %v", err)
	}
	if listener.Addr() != nil {
		t.Fatal("Addr() on nil listener is not nil")
	}
}

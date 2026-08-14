//go:build unix

package unixsocket

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestListenerCloseUnblocksAccept(t *testing.T) {
	listener, err := ListenStream(filepath.Join(t.TempDir(), "socket"), nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		_, err := listener.AcceptStream()
		accepted <- err
	}()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-accepted; err == nil {
		t.Fatal("AcceptStream returned nil after close")
	}
}

func TestListenerClosePreservesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "replacement" {
		t.Fatalf("replacement contents = %q", contents)
	}
}

func TestRepeatedUnixServeCycles(t *testing.T) {
	for i := 0; i < 20; i++ {
		path := filepath.Join(t.TempDir(), "socket")
		listener, err := ListenStream(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = listener.Accept()
		}()
		go func() {
			defer wg.Done()
			_ = listener.Close()
		}()
		wg.Wait()
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cycle %d: path after close = %v", i, err)
		}
	}
}

var _ net.Listener = (*Listener)(nil)

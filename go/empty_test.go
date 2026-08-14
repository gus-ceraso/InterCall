package intercall

import (
	"context"
	"crypto/sha256"
	"io"
	"sync"
	"testing"
)

func TestEmptyBindings(t *testing.T) {
	wantID := InterfaceID(sha256.Sum256([]byte(emptyInterfaceCanonicalBody)))
	if emptyInterfaceID != wantID {
		t.Fatalf("empty interface ID = %x, want sha256(canonical body) %x", emptyInterfaceID, wantID)
	}

	export := EmptyExportBinding()
	imp := EmptyImportBinding()
	if export == (ExportBinding{}) || imp == (ImportBinding{}) {
		t.Fatal("empty binding accessor returned a zero handle")
	}
	exportID, exportOK := export.InterfaceID()
	importID, importOK := imp.InterfaceID()
	if !exportOK || !importOK {
		t.Fatal("empty bindings do not carry interface metadata")
	}
	if exportID != wantID || importID != wantID || exportID != importID {
		t.Fatalf("empty binding IDs = (%x, %x), want matching %x", exportID, importID, wantID)
	}
	if EmptyExportBinding() != export || EmptyImportBinding() != imp {
		t.Fatal("empty binding accessors did not retain singleton identity")
	}

	for _, tc := range []struct {
		key     uint64
		payload []byte
	}{
		{0, nil},
		{1, []byte("request")},
		{^uint64(0), []byte{1, 2, 3}},
	} {
		key, payload := export.state.dispatch(context.Background(), tc.key, tc.payload)
		if key != emptyProcedureNotFoundKey || len(payload) != 0 {
			t.Errorf("empty dispatch(%x, %x) = (%x, %x), want procedure_not_found and empty payload", tc.key, tc.payload, key, payload)
		}
	}
}

func TestEmptyBindingsWorkWithRawConnection(t *testing.T) {
	stream := newEmptyTestStream()
	conn, err := NewConnection(context.Background(), stream, EmptyExportBinding(), EmptyImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Wait(); err != ErrClosed {
		t.Fatalf("Wait() = %v, want ErrClosed", err)
	}
	if stream.closeCount() != 1 {
		t.Fatalf("stream close count = %d, want 1", stream.closeCount())
	}
}

type emptyTestStream struct {
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	closes int
}

var _ ByteStream = (*emptyTestStream)(nil)

func newEmptyTestStream() *emptyTestStream { return &emptyTestStream{closed: make(chan struct{})} }

func (s *emptyTestStream) Read([]byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *emptyTestStream) Write(p []byte) (int, error) { return len(p), nil }

func (s *emptyTestStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *emptyTestStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

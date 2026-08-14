package intercall

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type negotiationMemoryStream struct {
	mu        sync.Mutex
	input     *bytes.Reader
	writes    []byte
	maxRead   int
	maxWrite  int
	closes    int
	closeOnce sync.Once
}

var _ ByteStream = (*negotiationMemoryStream)(nil)

func newNegotiationMemoryStream(input []byte) *negotiationMemoryStream {
	return &negotiationMemoryStream{input: bytes.NewReader(input)}
}

func (s *negotiationMemoryStream) Read(p []byte) (int, error) {
	if s.maxRead > 0 && len(p) > s.maxRead {
		p = p[:s.maxRead]
	}
	return s.input.Read(p)
}

func (s *negotiationMemoryStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(p)
	if s.maxWrite > 0 && n > s.maxWrite {
		n = s.maxWrite
	}
	s.writes = append(s.writes, p[:n]...)
	return n, nil
}

func (s *negotiationMemoryStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	s.closeOnce.Do(func() {})
	return nil
}

func (s *negotiationMemoryStream) written() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.writes...)
}

func (s *negotiationMemoryStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

type negotiationWriter func([]byte) (int, error)

func (w negotiationWriter) Write(p []byte) (int, error) { return w(p) }

func negotiatedTestBindings() (ExportBinding, ImportBinding, ExportBinding, ImportBinding) {
	var clientExportID, clientImportID, serverExportID, serverImportID InterfaceID
	clientExportID[0] = 0xa1
	clientImportID[0] = 0xb2
	serverExportID = clientImportID
	serverImportID = clientExportID
	clientExport, _ := NewExportBindingWithInterfaceID(func(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil }, clientExportID)
	clientImport := NewImportBindingWithInterfaceID(clientImportID)
	serverExport, _ := NewExportBindingWithInterfaceID(func(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil }, serverExportID)
	serverImport := NewImportBindingWithInterfaceID(serverImportID)
	return clientExport, clientImport, serverExport, serverImport
}

func TestNegotiatedHandshakeWritesOnlyExpectedPeerIDs(t *testing.T) {
	clientExport, clientImport, serverExport, serverImport := negotiatedTestBindings()
	clientExpected, _ := clientImport.InterfaceID()
	clientExportID, _ := clientExport.InterfaceID()
	serverExpected, _ := serverImport.InterfaceID()
	serverExportID, _ := serverExport.InterfaceID()

	clientStream := newNegotiationMemoryStream(serverImportIDBytes(serverImport))
	client, err := NewNegotiatedClientConnection(context.Background(), clientStream, clientExport, clientImport)
	if err != nil {
		t.Fatalf("client negotiation: %v", err)
	}
	if got := clientStream.written(); !bytes.Equal(got, serverExpectedBytes(clientExpected)) {
		t.Fatalf("client wrote %x, want only import ID %x", got, clientExpected)
	}
	if bytes.Equal(clientStream.written(), serverExpectedBytes(clientExportID)) {
		t.Fatal("client wrote its export ID")
	}
	_ = client.Close()
	_ = client.Wait()

	serverStream := newNegotiationMemoryStream(serverExpectedBytes(serverExportID))
	server, err := NewNegotiatedServerConnection(context.Background(), serverStream, serverExport, serverImport)
	if err != nil {
		t.Fatalf("server negotiation: %v", err)
	}
	if got := serverStream.written(); !bytes.Equal(got, serverExpectedBytes(serverExpected)) {
		t.Fatalf("server wrote %x, want only import ID %x", got, serverExpected)
	}
	if bytes.Equal(serverStream.written(), serverExpectedBytes(serverExportID)) {
		t.Fatal("server wrote its export ID")
	}
	_ = server.Close()
	_ = server.Wait()
}

func serverExpectedBytes(id InterfaceID) []byte        { return id[:] }
func serverImportIDBytes(binding ImportBinding) []byte { id, _ := binding.InterfaceID(); return id[:] }

func TestNegotiatedHandshakeLeavesFirstFrameByteUnread(t *testing.T) {
	clientExport, clientImport, _, serverImport := negotiatedTestBindings()
	clientExportID, _ := clientExport.InterfaceID()
	clientImportID, _ := clientImport.InterfaceID()
	frame := buildFrame(requestFrame, 7, 9, []byte("payload"))
	input := append(serverImportIDBytes(serverImport), frame...)
	stream := newNegotiationMemoryStream(input)
	if err := runNegotiation(stream, clientExportID, clientImportID, negotiationClient); err != nil {
		t.Fatalf("client negotiation: %v", err)
	}
	hdr, payload, err := readFrame(stream.input)
	if err != nil {
		t.Fatalf("read first post-negotiation frame: %v", err)
	}
	if hdr.kind != requestFrame || hdr.requestID != 7 || hdr.key != 9 || string(payload) != "payload" {
		t.Fatalf("post-negotiation frame = (%+v, %q), want request 7/key 9/payload", hdr, payload)
	}
}

func TestNegotiatedHandshakeFragmentationAndMismatch(t *testing.T) {
	clientExport, clientImport, serverExport, serverImport := negotiatedTestBindings()
	serverID, _ := serverExport.InterfaceID()
	clientID, _ := clientExport.InterfaceID()

	stream := newNegotiationMemoryStream(serverImportIDBytes(serverImport))
	stream.maxRead = 1
	stream.maxWrite = 1
	clientImportID, _ := clientImport.InterfaceID()
	if err := runNegotiation(stream, clientID, clientImportID, negotiationClient); err != nil {
		t.Fatalf("fragmented client negotiation: %v", err)
	}
	if got := stream.written(); !bytes.Equal(got, clientImportID[:]) {
		t.Fatalf("fragmented client write = %x, want %x", got, clientImportID)
	}

	mismatch := InterfaceID{}
	mismatch[0] = 0xff
	serverMismatch := newNegotiationMemoryStream(mismatch[:])
	if err := runNegotiation(serverMismatch, serverID, mustInterfaceID(t, serverImport), negotiationServer); !errors.Is(err, ErrInterfaceMismatch) {
		t.Fatalf("server mismatch error = %v, want ErrInterfaceMismatch", err)
	}
	if got := serverMismatch.written(); len(got) != 0 {
		t.Fatalf("server wrote %x after mismatch, want no bytes", got)
	}
}

func mustInterfaceID(t *testing.T, b ImportBinding) InterfaceID {
	t.Helper()
	id, ok := b.InterfaceID()
	if !ok {
		t.Fatal("test binding has no interface ID")
	}
	return id
}

func TestNegotiatedSetupReadDefenses(t *testing.T) {
	id := InterfaceID{1}
	cases := []struct {
		name string
		fn   negotiationReader
		want error
	}{
		{
			name: "invalid count",
			fn:   func([]byte) (int, error) { return 33, nil },
			want: errSetupInvalidReadCount,
		},
		{
			name: "no progress",
			fn:   func([]byte) (int, error) { return 0, nil },
			want: errSetupReadNoProgress,
		},
		{
			name: "short EOF",
			fn:   func(p []byte) (int, error) { p[0] = id[0]; return 1, io.EOF },
			want: io.EOF,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readInterfaceID(tc.fn, "test read interface ID")
			if !errors.Is(err, tc.want) {
				t.Fatalf("readInterfaceID error = %v, want %v", err, tc.want)
			}
		})
	}
}

type negotiationReader func([]byte) (int, error)

func (r negotiationReader) Read(p []byte) (int, error) { return r(p) }

func TestNegotiatedSetupWriteDefenses(t *testing.T) {
	id := InterfaceID{1}
	cases := []struct {
		name string
		fn   negotiationWriter
		want error
	}{
		{
			name: "invalid count",
			fn:   func([]byte) (int, error) { return 33, nil },
			want: errSetupInvalidWriteCount,
		},
		{
			name: "no progress",
			fn:   func([]byte) (int, error) { return 0, nil },
			want: errSetupWriteNoProgress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := writeInterfaceID(tc.fn, id, "test write interface ID"); !errors.Is(err, tc.want) {
				t.Fatalf("writeInterfaceID error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNegotiatedValidationPrecedesOwnership(t *testing.T) {
	stream := newNegotiationMemoryStream(nil)
	export, importBinding, _, _ := negotiatedTestBindings()
	legacyExport, err := NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	legacyImport := NewImportBinding()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name   string
		ctx    context.Context
		stream ByteStream
		export ExportBinding
		imp    ImportBinding
		want   error
	}{
		{"nil context", nil, stream, export, importBinding, ErrInvalidArgument},
		{"nil stream", context.Background(), nil, export, importBinding, ErrInvalidArgument},
		{"zero export", context.Background(), stream, ExportBinding{}, importBinding, ErrInvalidArgument},
		{"zero import", context.Background(), stream, export, ImportBinding{}, ErrInvalidArgument},
		{"legacy export", context.Background(), stream, legacyExport, importBinding, ErrInvalidArgument},
		{"legacy import", context.Background(), stream, export, legacyImport, ErrInvalidArgument},
		{"available context", ctx, stream, export, importBinding, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := NewNegotiatedClientConnection(tc.ctx, tc.stream, tc.export, tc.imp)
			if !errors.Is(got, tc.want) {
				t.Fatalf("error = %v, want %v", got, tc.want)
			}
		})
	}
	if stream.closeCount() != 0 {
		t.Fatalf("validation closed stream %d times, want 0", stream.closeCount())
	}
}

type negotiationBlockingStream struct {
	mu         sync.Mutex
	input      *bytes.Reader
	blockWrite bool
	closed     chan struct{}
	closeOnce  sync.Once
	closes     int
	readStart  chan struct{}
	writeStart chan struct{}
	readOnce   sync.Once
	writeOnce  sync.Once
}

var _ ByteStream = (*negotiationBlockingStream)(nil)

func newNegotiationBlockingStream(input []byte, blockWrite bool) *negotiationBlockingStream {
	return &negotiationBlockingStream{
		input:      bytes.NewReader(input),
		blockWrite: blockWrite,
		closed:     make(chan struct{}),
		readStart:  make(chan struct{}),
		writeStart: make(chan struct{}),
	}
}

func (s *negotiationBlockingStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.input.Len() != 0 {
		n, err := s.input.Read(p)
		s.mu.Unlock()
		return n, err
	}
	s.mu.Unlock()
	s.readOnce.Do(func() { close(s.readStart) })
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *negotiationBlockingStream) Write(p []byte) (int, error) {
	if !s.blockWrite {
		return len(p), nil
	}
	s.writeOnce.Do(func() { close(s.writeStart) })
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *negotiationBlockingStream) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *negotiationBlockingStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func TestNegotiatedSetupCancellationAndTimeout(t *testing.T) {
	clientExport, clientImport, serverExport, serverImport := negotiatedTestBindings()
	clientID, _ := clientExport.InterfaceID()
	serverID, _ := serverExport.InterfaceID()
	clientImportID, _ := clientImport.InterfaceID()
	serverImportID, _ := serverImport.InterfaceID()

	tests := []struct {
		name       string
		role       negotiationRole
		input      []byte
		block      string
		blockWrite bool
	}{
		{"client write", negotiationClient, serverImportID[:], "write", true},
		{"client read", negotiationClient, nil, "read", false},
		{"server read", negotiationServer, nil, "read", false},
		{"server write", negotiationServer, clientImportID[:], "write", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := newNegotiationBlockingStream(tc.input, tc.blockWrite)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if tc.role == negotiationClient {
					_, err = newNegotiatedConnection(ctx, stream, clientExport, clientImport, tc.role, time.Second)
				} else {
					_, err = newNegotiatedConnection(ctx, stream, serverExport, serverImport, tc.role, time.Second)
				}
				done <- err
			}()
			if tc.block == "write" {
				<-stream.writeStart
			} else {
				<-stream.readStart
			}
			cancel()
			if err := <-done; err != context.Canceled {
				t.Fatalf("setup error = %v, want context.Canceled", err)
			}
			if stream.closeCount() != 1 {
				t.Fatalf("stream close count = %d, want 1", stream.closeCount())
			}
		})
	}

	t.Run("short internal timeout", func(t *testing.T) {
		stream := newNegotiationBlockingStream(nil, true)
		_, err := newNegotiatedConnection(context.Background(), stream, clientExport, clientImport, negotiationClient, 10*time.Millisecond)
		if err != context.DeadlineExceeded {
			t.Fatalf("setup error = %v, want context.DeadlineExceeded", err)
		}
		if stream.closeCount() != 1 {
			t.Fatalf("stream close count = %d, want 1", stream.closeCount())
		}
	})

	// The client/server IDs above are deliberately asymmetric; keep the
	// assignments live in this test so a future simplification cannot make
	// the cancellation cases accidentally symmetric.
	_ = clientID
	_ = serverID
}

func TestNegotiatedConnectionUsesOriginalContextAfterHandoff(t *testing.T) {
	clientExport, clientImport, _, serverImport := negotiatedTestBindings()
	stream := newNegotiationBlockingStream(serverImportIDBytes(serverImport), false)
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := NewNegotiatedClientConnection(ctx, stream, clientExport, clientImport)
	if err != nil {
		t.Fatal(err)
	}
	if conn == nil {
		t.Fatal("successful negotiation returned nil connection")
	}
	cancel()
	if got := conn.Wait(); got != context.Canceled {
		t.Fatalf("Wait after original-context cancellation = %v, want context.Canceled", got)
	}
	if stream.closeCount() != 1 {
		t.Fatalf("stream close count = %d, want 1", stream.closeCount())
	}
}

func TestNegotiatedNetPipeAsymmetricExchange(t *testing.T) {
	clientExport, clientImport, serverExport, serverImport := negotiatedTestBindings()
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	results := make(chan error, 2)
	go func() {
		conn, err := NewNegotiatedClientConnection(context.Background(), clientSide, clientExport, clientImport)
		if err == nil {
			_ = conn.Close()
			_ = conn.Wait()
		}
		results <- err
	}()
	go func() {
		conn, err := NewNegotiatedServerConnection(context.Background(), serverSide, serverExport, serverImport)
		if err == nil {
			_ = conn.Close()
			_ = conn.Wait()
		}
		results <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("pipe negotiation: %v", err)
		}
	}
}

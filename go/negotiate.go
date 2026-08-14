package intercall

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

const defaultNegotiationTimeout = 10 * time.Second

var (
	// Setup-only write classifications. They intentionally do not reuse the
	// frame-writing errors: a failed interface record is still before the
	// InterCall frame protocol starts.
	errSetupInvalidWriteCount sentinel = "intercall: invalid setup write count"
	errSetupWriteNoProgress   sentinel = "intercall: setup write made no progress"
	errSetupInvalidReadCount  sentinel = "intercall: invalid setup read count"
	errSetupReadNoProgress    sentinel = "intercall: setup read made no progress"
)

type negotiationRole uint8

const (
	negotiationClient negotiationRole = iota
	negotiationServer
)

// closeOnceStream gives negotiated setup and the eventual Connection one
// shared ownership wrapper. The underlying stream is closed at most once,
// including when setup cancellation races a setup failure or successful
// handoff.
type closeOnceStream struct {
	stream ByteStream
	once   sync.Once
	err    error
}

var _ ByteStream = (*closeOnceStream)(nil)

func (s *closeOnceStream) Read(p []byte) (int, error)  { return s.stream.Read(p) }
func (s *closeOnceStream) Write(p []byte) (int, error) { return s.stream.Write(p) }

func (s *closeOnceStream) Close() error {
	s.once.Do(func() { s.err = s.stream.Close() })
	return s.err
}

// setupOutcome arbitrates all setup failures and the successful ownership
// transition. The first selected error is permanent; handoff is possible only
// while no setup error has been selected.
type setupOutcome struct {
	mu        sync.Mutex
	err       error
	handedOff bool
}

func (o *setupOutcome) selectError(err error) {
	if err == nil {
		panic("intercall: nil negotiated setup error")
	}
	o.mu.Lock()
	if o.err == nil && !o.handedOff {
		o.err = err
	}
	o.mu.Unlock()
}

func (o *setupOutcome) error() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func (o *setupOutcome) handoff() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.handedOff = true
	return nil
}

// negotiationSetup owns the temporary setup context, callback, outcome, and
// close-once stream until a successful NewConnection call transfers the
// wrapper to the connection.
type negotiationSetup struct {
	owner        *closeOnceStream
	outcome      setupOutcome
	cancel       context.CancelFunc
	stop         func() bool
	callbackDone chan struct{}
}

func (s *negotiationSetup) stopCallback() {
	if s.stop == nil {
		return
	}
	stop := s.stop
	s.stop = nil
	if !stop() {
		<-s.callbackDone
	}
}

func (s *negotiationSetup) fail(err error) error {
	s.outcome.selectError(err)
	s.stopCallback()
	_ = s.owner.Close()
	s.cancel()
	if selected := s.outcome.error(); selected != nil {
		return selected
	}
	return err
}

// NewNegotiatedClientConnection agrees on the interface expected by each
// endpoint, then constructs an ordinary symmetric Connection. The client
// writes its import binding's ID first; it never writes its own export ID.
// The caller's context owns the returned connection, while a temporary
// ten-second setup phase bounds interface agreement.
func NewNegotiatedClientConnection(ctx context.Context, stream ByteStream, export ExportBinding, imp ImportBinding) (*Connection, error) {
	return newNegotiatedConnection(ctx, stream, export, imp, negotiationClient, defaultNegotiationTimeout)
}

// NewNegotiatedServerConnection agrees on the interface expected by each
// endpoint, then constructs an ordinary symmetric Connection. The server
// reads the client's expected-server ID first and writes its import binding's
// ID only after that ID matches its export binding.
func NewNegotiatedServerConnection(ctx context.Context, stream ByteStream, export ExportBinding, imp ImportBinding) (*Connection, error) {
	return newNegotiatedConnection(ctx, stream, export, imp, negotiationServer, defaultNegotiationTimeout)
}

// newNegotiatedConnection is the timeout-parameterized setup core. The
// timeout is unexported so tests can exercise setup deadlines without a
// ten-second sleep; production callers use defaultNegotiationTimeout.
func newNegotiatedConnection(ctx context.Context, stream ByteStream, export ExportBinding, imp ImportBinding, role negotiationRole, timeout time.Duration) (*Connection, error) {
	exportID, importID, err := validateNegotiatedArguments(ctx, stream, export, imp)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	owner := &closeOnceStream{stream: stream}
	setupCtx, cancel := context.WithTimeout(ctx, timeout)
	setup := &negotiationSetup{
		owner:        owner,
		cancel:       cancel,
		callbackDone: make(chan struct{}),
	}
	setup.stop = context.AfterFunc(setupCtx, func() {
		err := setupCtx.Err()
		if err == nil {
			err = context.Canceled
		}
		setup.outcome.selectError(err)
		_ = owner.Close()
		close(setup.callbackDone)
	})

	if err := runNegotiation(setup.owner, exportID, importID, role); err != nil {
		return nil, setup.fail(err)
	}

	// Stop and join the cancellation callback before making handoff one
	// lock-protected transition. If the callback won, its exact context error
	// and cleanup are already complete here.
	setup.stopCallback()
	if err := setup.outcome.handoff(); err != nil {
		_ = owner.Close()
		cancel()
		return nil, err
	}
	cancel()

	conn, err := NewConnection(ctx, owner, export, imp)
	if err != nil {
		// NewConnection rejected ownership, normally because the original
		// context became canceled between setup and construction.
		_ = owner.Close()
		return nil, err
	}
	return conn, nil
}

func validateNegotiatedArguments(ctx context.Context, stream ByteStream, export ExportBinding, imp ImportBinding) (InterfaceID, InterfaceID, error) {
	if ctx == nil {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection context is nil: %w", ErrInvalidArgument)
	}
	if stream == nil {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection stream is nil: %w", ErrInvalidArgument)
	}
	if export == (ExportBinding{}) {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection export binding is zero: %w", ErrInvalidArgument)
	}
	if imp == (ImportBinding{}) {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection import binding is zero: %w", ErrInvalidArgument)
	}
	exportID, ok := export.InterfaceID()
	if !ok {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection export binding has no interface ID: %w", ErrInvalidArgument)
	}
	importID, ok := imp.InterfaceID()
	if !ok {
		return InterfaceID{}, InterfaceID{}, fmt.Errorf("intercall: negotiated connection import binding has no interface ID: %w", ErrInvalidArgument)
	}
	return exportID, importID, nil
}

func runNegotiation(stream ByteStream, exportID, importID InterfaceID, role negotiationRole) error {
	if role == negotiationClient {
		if err := writeInterfaceID(stream, importID, "client write interface ID"); err != nil {
			return err
		}
		serverID, err := readInterfaceID(stream, "client read interface ID")
		if err != nil {
			return err
		}
		if serverID != exportID {
			return interfaceMismatch("client", exportID, serverID)
		}
		return nil
	}

	clientID, err := readInterfaceID(stream, "server read interface ID")
	if err != nil {
		return err
	}
	if clientID != exportID {
		return interfaceMismatch("server", exportID, clientID)
	}
	return writeInterfaceID(stream, importID, "server write interface ID")
}

func interfaceMismatch(role string, expected, received InterfaceID) error {
	return fmt.Errorf("intercall: %s interface ID mismatch: expected %x, received %x: %w", role, expected, received, ErrInterfaceMismatch)
}

func writeInterfaceID(w io.Writer, id InterfaceID, operation string) error {
	remaining := id[:]
	written := 0
	for len(remaining) != 0 {
		n, err := w.Write(remaining)
		if n < 0 || n > len(remaining) {
			return fmt.Errorf("intercall: %s: invalid byte count %d for %d-byte remainder: %w", operation, n, len(remaining), errSetupInvalidWriteCount)
		}
		if err != nil {
			return fmt.Errorf("intercall: %s: write failed after %d of %d bytes: %w", operation, written+n, len(id), err)
		}
		if n == 0 {
			return fmt.Errorf("intercall: %s: no progress: %w", operation, errSetupWriteNoProgress)
		}
		written += n
		remaining = remaining[n:]
	}
	return nil
}

func readInterfaceID(r io.Reader, operation string) (InterfaceID, error) {
	var id InterfaceID
	read := 0
	for read < len(id) {
		n, err := r.Read(id[read:])
		remaining := len(id) - read
		if n < 0 || n > remaining {
			return InterfaceID{}, fmt.Errorf("intercall: %s: invalid byte count %d for %d-byte remainder: %w", operation, n, remaining, errSetupInvalidReadCount)
		}
		read += n
		if read == len(id) {
			return id, nil
		}
		if err != nil {
			return InterfaceID{}, fmt.Errorf("intercall: %s: short read after %d of %d bytes: %w", operation, read, len(id), err)
		}
		if n == 0 {
			return InterfaceID{}, fmt.Errorf("intercall: %s: no progress: %w", operation, errSetupReadNoProgress)
		}
	}
	return id, nil
}

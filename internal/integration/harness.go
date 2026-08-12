package integration

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cerasos/intercall"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eexport"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eimport"
)

// newConnection constructs one connection over the given stream with
// the fixture bindings: the checked-in export binding of the shared
// provider package and the checked-in import binding of the owned
// interface. Both ends of every test connection share these two
// process singletons, which the runtime allows any number of
// connections to share concurrently.
func newConnection(t *testing.T, ctx context.Context, stream intercall.ByteStream) *intercall.Connection {
	t.Helper()
	conn, err := intercall.NewConnection(ctx, stream, e2eexport.ExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	return conn
}

// newPair constructs two connected connections over one duplex stream:
// both peers run the same generated export and import bindings, so a
// call from either side is served by the other. Cleanup closes both
// ends, so a failed test cannot leak connections into later iterations.
func newPair(t *testing.T) (a, b *intercall.Connection) {
	t.Helper()
	ea, eb := newDuplex()
	a = newConnection(t, context.Background(), ea)
	b = newConnection(t, context.Background(), eb)
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return a, b
}

// newRawPeer constructs one connection whose stream's other end stays
// in the test's hands: the test writes raw request and response frames
// as the opposing peer and reads the frames the connection writes.
func newRawPeer(t *testing.T) (conn *intercall.Connection, peer *duplex) {
	t.Helper()
	ea, eb := newDuplex()
	conn = newConnection(t, context.Background(), ea)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, eb
}

// bind returns a context carrying the connection, as the generated
// import callers require.
func bind(conn *intercall.Connection) context.Context {
	return intercall.WithConnection(context.Background(), conn)
}

// closeAndWait closes the connection and asserts the exact ErrClosed
// terminal cause, reaping the receive loop, observer, and teardown.
func closeAndWait(t *testing.T, conn *intercall.Connection) {
	t.Helper()
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := conn.Wait(); err != intercall.ErrClosed {
		t.Fatalf("Wait after Close: %v, want ErrClosed", err)
	}
}

// eventually polls cond until it reports true or a failsafe deadline
// passes. It synchronizes the tests with cross-goroutine state such as
// a handler registering in the wait registry; the deadline is a pure
// failsafe and is never the assertion mechanism.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Goroutine-leak tracking. The baseline is captured once per test
// process at the first check, after the first test has reaped its
// connections, so every -count=20 iteration compares against the same
// process state and a leak in one iteration is caught by the next.
var (
	goroutineBaselineOnce sync.Once
	goroutineBaseline     int
)

// requireNoLeaks fails the test if the goroutine count does not return
// to the process baseline within a failsafe deadline. Wait already
// reaps the receive loop, terminal teardown, and context observer, and
// every provider honors its handler context cancellation, so a healthy
// test returns to the baseline immediately; the poll is a pure
// failsafe.
func requireNoLeaks(t *testing.T) {
	t.Helper()
	goroutineBaselineOnce.Do(func() {
		goroutineBaseline = runtime.NumGoroutine()
	})
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > goroutineBaseline+2 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: %d goroutines running, baseline %d", runtime.NumGoroutine(), goroutineBaseline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Anonymous record aliases of the fixture interface. The generated
// import callers spell these records inline with their exact intercall
// tags; the aliases are identical types, so the tests can construct
// values without spelling the generated signatures.
type (
	// PaintOrigin is record { x int32; y int32; }.
	PaintOrigin = struct {
		X int32 `intercall:"x"`
		Y int32 `intercall:"y"`
	}
	// PaintColor is record { red uint8; green uint8; blue uint8; alpha
	// uint8; }.
	PaintColor = struct {
		Red   uint8 `intercall:"red"`
		Green uint8 `intercall:"green"`
		Blue  uint8 `intercall:"blue"`
		Alpha uint8 `intercall:"alpha"`
	}
	// PaintSize is record { width uint32; height uint32; }.
	PaintSize = struct {
		Width  uint32 `intercall:"width"`
		Height uint32 `intercall:"height"`
	}
	// LocateInner is record { x int32; y int32; }, the innermost corner.
	LocateInner = struct {
		X int32 `intercall:"x"`
		Y int32 `intercall:"y"`
	}
	// LocateCorner is record { inner record { x int32; y int32; };
	// label string; }.
	LocateCorner = struct {
		Inner LocateInner `intercall:"inner"`
		Label string      `intercall:"label"`
	}
	// LocateBox is record { corner record { inner record { x int32; y
	// int32; }; label string; }; tag uint16; }.
	LocateBox = struct {
		Corner LocateCorner `intercall:"corner"`
		Tag    uint16       `intercall:"tag"`
	}
	// LocateResult is record { corner point; area uint64; }.
	LocateResult = struct {
		Corner e2eimport.Point `intercall:"corner"`
		Area   uint64          `intercall:"area"`
	}
	// GridOrigin is record { x int16; y int16; }, one grid row origin.
	GridOrigin = struct {
		X int16 `intercall:"x"`
		Y int16 `intercall:"y"`
	}
	// GridRow is record { origin record { x int16; y int16; }; value
	// int64; }, one grid list element.
	GridRow = struct {
		Origin GridOrigin `intercall:"origin"`
		Value  int64      `intercall:"value"`
	}
)

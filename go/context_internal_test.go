package intercall

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mustPanic asserts that f panics.
func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	f()
}

// TestConnectionContextBindAndLookup pins that WithConnection binds a
// nonnil connection and ConnectionFromContext returns exactly that
// connection.
func TestConnectionContextBindAndLookup(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})
	ctx := WithConnection(context.Background(), c)

	got, err := ConnectionFromContext(ctx)
	if err != nil {
		t.Fatalf("ConnectionFromContext: %v", err)
	}
	if got != c {
		t.Errorf("lookup returned %p, want %p", got, c)
	}
	finish(t, c, ErrClosed)
}

// TestConnectionContextReplacement pins that WithConnection replaces any
// earlier binding under the private key.
func TestConnectionContextReplacement(t *testing.T) {
	a := newTestConn(t, context.Background(), &countingStream{})
	b := newTestConn(t, context.Background(), &countingStream{})
	ctx := WithConnection(context.Background(), a)
	ctx = WithConnection(ctx, b)

	got, err := ConnectionFromContext(ctx)
	if err != nil {
		t.Fatalf("ConnectionFromContext: %v", err)
	}
	if got != b {
		t.Errorf("lookup returned %p, want the replacement %p", got, b)
	}
	if got == a {
		t.Error("lookup returned the replaced binding")
	}
	finish(t, a, ErrClosed)
	finish(t, b, ErrClosed)
}

// TestConnectionContextNoBinding pins that ConnectionFromContext returns
// ErrNoConnection when no nonnil connection is bound, regardless of other
// context values or cancellation.
func TestConnectionContextNoBinding(t *testing.T) {
	if _, err := ConnectionFromContext(context.Background()); err != ErrNoConnection {
		t.Errorf("empty context: err = %v, want ErrNoConnection", err)
	}
	if _, err := ConnectionFromContext(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Error("errors.Is(empty context, ErrNoConnection) = false")
	}
	ctx := context.WithValue(context.Background(), struct{}{}, "unrelated")
	if _, err := ConnectionFromContext(ctx); err != ErrNoConnection {
		t.Errorf("unrelated value: err = %v, want ErrNoConnection", err)
	}
	ctx2, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ConnectionFromContext(ctx2); err != ErrNoConnection {
		t.Errorf("canceled context: err = %v, want ErrNoConnection", err)
	}
}

// TestConnectionContextNilContext pins that ConnectionFromContext returns
// ErrInvalidArgument for a nil context.
func TestConnectionContextNilContext(t *testing.T) {
	if _, err := ConnectionFromContext(nil); err != ErrInvalidArgument {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
	if _, err := ConnectionFromContext(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Error("errors.Is(nil context, ErrInvalidArgument) = false")
	}
}

// TestConnectionContextNilBindingIsNoConnection pins that only a nonnil
// connection counts as bound: a forged nil binding under the private key
// still yields ErrNoConnection.
func TestConnectionContextNilBindingIsNoConnection(t *testing.T) {
	ctx := context.WithValue(context.Background(), connectionContextKey{}, (*Connection)(nil))
	if _, err := ConnectionFromContext(ctx); err != ErrNoConnection {
		t.Errorf("err = %v, want ErrNoConnection for a nil binding", err)
	}
}

// TestConnectionContextNilParentPanics pins the WithConnection panic
// contract for a nil parent.
func TestConnectionContextNilParentPanics(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})
	mustPanic(t, "WithConnection(nil, conn)", func() { WithConnection(nil, c) })
	finish(t, c, ErrClosed)
}

// TestConnectionContextNilConnectionPanics pins the WithConnection panic
// contract for a nil connection.
func TestConnectionContextNilConnectionPanics(t *testing.T) {
	mustPanic(t, "WithConnection(ctx, nil)", func() { WithConnection(context.Background(), nil) })
}

// TestConnectionContextInheritsCancellation pins that the derived context
// keeps the parent's cancellation while the binding survives it.
func TestConnectionContextInheritsCancellation(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})
	parent, cancel := context.WithCancel(context.Background())
	ctx := WithConnection(parent, c)

	if got, err := ConnectionFromContext(ctx); err != nil || got != c {
		t.Fatalf("lookup = (%p, %v), want (%p, nil)", got, err, c)
	}
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("derived context did not inherit parent cancellation")
	}
	if got, err := ConnectionFromContext(ctx); err != nil || got != c {
		t.Errorf("lookup after cancellation = (%p, %v), want (%p, nil)", got, err, c)
	}
	finish(t, c, ErrClosed)
}

// TestConnectionContextBindingSurvivesTerminal pins that a bound context
// keeps returning the connection after the connection terminates.
func TestConnectionContextBindingSurvivesTerminal(t *testing.T) {
	c := newTestConn(t, context.Background(), &countingStream{})
	ctx := WithConnection(context.Background(), c)

	finish(t, c, ErrClosed)
	if got, err := ConnectionFromContext(ctx); err != nil || got != c {
		t.Errorf("lookup after terminal = (%p, %v), want (%p, nil)", got, err, c)
	}
}

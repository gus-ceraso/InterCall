package integration

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cerasos/intercall"
	"github.com/cerasos/intercall/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/internal/integration/fixtures/provider"
)

// TestShutdown covers the exact terminal causes and the races between
// the terminal events: explicit Close, construction-context
// cancellation, EOF and half-close, and write failure. Every cause is
// exact — the first event to win the common selection lock decides —
// and every path reaps the receive loop, teardown, and observer.
func TestShutdown(t *testing.T) {
	// Explicit Close selects the exact ErrClosed cause; the peer
	// terminates on the EOF its stream reports.
	t.Run("ExactCloseCause", func(t *testing.T) {
		a, b := newPair(t)
		if err := a.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := a.Wait(); err != intercall.ErrClosed {
			t.Fatalf("Wait = %v, want ErrClosed", err)
		}
		if err := b.Wait(); !errors.Is(err, io.EOF) {
			t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
		}
	})

	// Construction-context cancellation selects the exact context error.
	t.Run("ContextCancellationCause", func(t *testing.T) {
		ea, _ := newDuplex()
		ctx, cancel := context.WithCancel(context.Background())
		conn := newConnection(t, ctx, ea)
		cancel()
		if err := conn.Wait(); err != context.Canceled {
			t.Fatalf("Wait = %v, want context.Canceled", err)
		}
	})

	// A deadline construction context selects the exact deadline error.
	t.Run("DeadlineCause", func(t *testing.T) {
		ea, _ := newDuplex()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		conn := newConnection(t, ctx, ea)
		if err := conn.Wait(); err != context.DeadlineExceeded {
			t.Fatalf("Wait = %v, want context.DeadlineExceeded", err)
		}
	})

	// Close and context cancellation race for the same selection lock:
	// whichever wins, the cause is exactly one of the two.
	t.Run("CloseVsContextRace", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			ea, _ := newDuplex()
			ctx, cancel := context.WithCancel(context.Background())
			conn := newConnection(t, ctx, ea)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = conn.Close()
			}()
			go func() {
				defer wg.Done()
				cancel()
			}()
			wg.Wait()
			if err := conn.Wait(); err != intercall.ErrClosed && err != context.Canceled {
				t.Fatalf("close/cancel race cause = %v, want ErrClosed or context.Canceled", err)
			}
		}
	})

	// A half-close from the peer is a clean EOF cause.
	t.Run("EOFHalfClose", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		if err := peer.Close(); err != nil {
			t.Fatalf("closing the test side: %v", err)
		}
		if err := conn.Wait(); !errors.Is(err, io.EOF) {
			t.Fatalf("Wait = %v, want an io.EOF cause", err)
		}
	})

	// A write failure during a handler response is terminal with the
	// writer's exact error; the caller's pending call is claimed by the
	// EOF its connection observes when the peer goes away.
	t.Run("WriteFailure", func(t *testing.T) {
		ea, eb := newDuplex()
		var armed atomic.Bool
		injected := errors.New("injected write failure")
		a := newConnection(t, context.Background(), ea)
		b := newConnection(t, context.Background(), &failStream{ByteStream: eb, armed: &armed, err: injected})
		ctxA := bind(a)

		done := make(chan error, 1)
		go func() {
			_, err := e2eimport.Wait(ctxA, 9)
			done <- err
		}()
		eventually(t, "wait 9 to register", func() bool { return provider.IsWaiting(9) })
		armed.Store(true)
		provider.ReleaseWait(9)

		// B's response write fails with the injected error, which is
		// preserved through the terminal cause.
		if err := b.Wait(); !errors.Is(err, injected) {
			t.Fatalf("peer Wait = %v, want the injected write error", err)
		}
		// A's call completes when its connection observes the EOF.
		if err := <-done; !errors.Is(err, io.EOF) {
			t.Fatalf("call error = %v, want an io.EOF cause", err)
		}
		if err := a.Wait(); !errors.Is(err, io.EOF) {
			t.Fatalf("caller Wait = %v, want an io.EOF cause", err)
		}
	})

	// Close and EOF race for the same selection lock: whichever wins,
	// the cause is exactly the local close or the EOF.
	t.Run("CloseVsEOF", func(t *testing.T) {
		conn, peer := newRawPeer(t)
		go func() {
			_ = peer.Close()
		}()
		_ = conn.Close()
		if err := conn.Wait(); err != intercall.ErrClosed && !errors.Is(err, io.EOF) {
			t.Fatalf("close/EOF race cause = %v, want ErrClosed or an io.EOF cause", err)
		}
	})

	requireNoLeaks(t)
}

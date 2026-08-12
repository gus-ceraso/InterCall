package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/provider"
)

// TestCancellation covers per-call cancellation over generated callers:
// a canceled call returns its exact context error while the handler is
// blocked, the late response then arriving is consumed as an opaque
// unmatched frame without terminating the connection; a deadline
// returns the exact deadline error; and a pre-canceled call context
// never reaches the wire.
func TestCancellation(t *testing.T) {
	a, b := newPair(t)
	ctxA, ctxB := bind(a), bind(b)

	// Cancel while the handler is blocked at the peer. The call returns
	// the exact context error; the pending entry is retired, so the
	// handler's late response is unmatched and opaque.
	callCtx, cancel := context.WithCancel(ctxA)
	done := make(chan error, 1)
	go func() {
		got, err := e2eimport.Wait(callCtx, 7)
		if got != 0 {
			done <- fmt.Errorf("Wait(7) returned %d", got)
			return
		}
		done <- err
	}()
	eventually(t, "wait 7 to register", func() bool { return provider.IsWaiting(7) })
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("canceled Wait(7) = %v, want context.Canceled", err)
	}
	// The late response arrives after the retirement and must not
	// terminate the connection.
	provider.ReleaseWait(7)
	if err := e2eimport.Ping(ctxA); err != nil {
		t.Fatalf("A ping after the late response: %v", err)
	}
	if err := e2eimport.Ping(ctxB); err != nil {
		t.Fatalf("B ping after the late response: %v", err)
	}

	// A deadline cancellation returns the exact deadline error and its
	// late response is likewise opaque.
	deadlineCtx, cancelDeadline := context.WithTimeout(ctxA, 50*time.Millisecond)
	done2 := make(chan error, 1)
	go func() {
		_, err := e2eimport.Wait(deadlineCtx, 8)
		done2 <- err
	}()
	eventually(t, "wait 8 to register", func() bool { return provider.IsWaiting(8) })
	if err := <-done2; err != context.DeadlineExceeded {
		t.Fatalf("deadline Wait(8) = %v, want context.DeadlineExceeded", err)
	}
	cancelDeadline()
	provider.ReleaseWait(8)
	if err := e2eimport.Ping(ctxA); err != nil {
		t.Fatalf("A ping after the deadline response: %v", err)
	}

	// A pre-canceled call context returns its exact error without
	// constructing a frame; the connection is unaffected.
	preCtx, cancelPre := context.WithCancel(ctxA)
	cancelPre()
	if _, err := e2eimport.Echo(preCtx, "x"); err != context.Canceled {
		t.Fatalf("Echo on a pre-canceled context = %v, want context.Canceled", err)
	}
	if err := e2eimport.Ping(ctxA); err != nil {
		t.Fatalf("A ping after the pre-canceled call: %v", err)
	}

	closeAndWait(t, a)
	if err := b.Wait(); !errors.Is(err, io.EOF) {
		t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
	}
	requireNoLeaks(t)
}

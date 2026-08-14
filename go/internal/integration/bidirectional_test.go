package integration

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/provider"
)

// TestBidirectional joins both generated directions over one duplex
// stream: each peer calls the other with the same fixture bindings,
// including concurrent cross-direction traffic and recorded
// notifications, and both connections survive to a clean close. Opposing
// request ID spaces are independent, so equal IDs in both directions are
// legal and must never cross-match.
func TestBidirectional(t *testing.T) {
	provider.ResetMessages()
	a, b := newPair(t)
	ctxA, ctxB := bind(a), bind(b)

	// A -> B and B -> A echo and notify concurrently.
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("a-%d", i)
			got, err := e2eimport.Echo(ctxA, want)
			if err != nil {
				errs <- fmt.Errorf("A->B Echo(%s): %v", want, err)
				return
			}
			if got != want {
				errs <- fmt.Errorf("A->B Echo(%s) = %q", want, got)
			}
			errs <- e2eimport.Notify(ctxA, "note-a-"+want)
		}(i)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("b-%d", i)
			got, err := e2eimport.Echo(ctxB, want)
			if err != nil {
				errs <- fmt.Errorf("B->A Echo(%s): %v", want, err)
				return
			}
			if got != want {
				errs <- fmt.Errorf("B->A Echo(%s) = %q", want, got)
			}
			errs <- e2eimport.Notify(ctxB, "note-b-"+want)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	// Both directions carry value calls with different shapes.
	if got, err := e2eimport.Add(ctxA, 1, 2, 3, 4, 5, 6, 7, 8); err != nil || got != 36 {
		t.Fatalf("A->B Add = %d, %v", got, err)
	}
	if got, err := e2eimport.Measure(ctxB, 0.5, 1.25); err != nil || got != 1.75 {
		t.Fatalf("B->A Measure = %v, %v", got, err)
	}

	// The shared recorder observed both directions.
	want := []string{"note-a-a-0", "note-a-a-1", "note-a-a-2", "note-a-a-3",
		"note-b-b-0", "note-b-b-1", "note-b-b-2", "note-b-b-3"}
	msgs := provider.Messages()
	for _, w := range want {
		found := false
		for _, m := range msgs {
			if m == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("notification %q missing from %v", w, msgs)
		}
	}
	if len(msgs) != len(want) {
		t.Errorf("recorded %d notifications, want %d: %v", len(msgs), len(want), msgs)
	}

	// Clean shutdown: the closing side reports the exact ErrClosed cause
	// and the peer terminates on the EOF.
	closeAndWait(t, a)
	if err := b.Wait(); !errors.Is(err, io.EOF) {
		t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
	}
	requireNoLeaks(t)
}

// TestNested exercises calls made from inside handlers: a provider on
// one peer calls the generated import binding of the opposing peer with
// its bound handler context, so each nesting level hops the request
// between the peers. Outer(0) is 1 and Outer(n) is Inner(n-1) + 1, and
// symmetrically for Inner, so Outer(n) and Inner(n) both return n + 1.
func TestNested(t *testing.T) {
	a, b := newPair(t)
	ctxA, ctxB := bind(a), bind(b)

	for n := uint64(0); n <= 6; n++ {
		if got, err := e2eimport.Outer(ctxA, n); err != nil || got != n+1 {
			t.Fatalf("A->B Outer(%d) = %d, %v; want %d", n, got, err, n+1)
		}
		if got, err := e2eimport.Inner(ctxB, n); err != nil || got != n+1 {
			t.Fatalf("B->A Inner(%d) = %d, %v; want %d", n, got, err, n+1)
		}
	}

	// Concurrent nesting in both directions: eight chains of four hops
	// each run simultaneously across the two peers.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if got, err := e2eimport.Outer(ctxA, 4); err != nil || got != 5 {
				t.Errorf("concurrent A->B Outer(4) = %d, %v", got, err)
			}
		}()
		go func() {
			defer wg.Done()
			if got, err := e2eimport.Inner(ctxB, 4); err != nil || got != 5 {
				t.Errorf("concurrent B->A Inner(4) = %d, %v", got, err)
			}
		}()
	}
	wg.Wait()

	closeAndWait(t, a)
	if err := b.Wait(); !errors.Is(err, io.EOF) {
		t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
	}
	requireNoLeaks(t)
}

package integration

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/cerasos/intercall/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/internal/integration/fixtures/provider"
)

// callResult is one completed Wait call outcome.
type callResult struct {
	id  uint32
	got uint32
	err error
}

// TestConcurrent fires many calls in both directions at once: the
// opposing peers allocate their request IDs from the same range at the
// same time, responses arrive out of order under controlled release,
// and every call still observes its own result.
func TestConcurrent(t *testing.T) {
	a, b := newPair(t)
	ctxA, ctxB := bind(a), bind(b)

	// Equal opposing IDs: both peers fire pings concurrently, each
	// allocating IDs from 0 while the opposing peer does the same.
	const pings = 64
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < pings; i++ {
			if err := e2eimport.Ping(ctxA); err != nil {
				errs <- fmt.Errorf("A ping %d: %v", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < pings; i++ {
			if err := e2eimport.Ping(ctxB); err != nil {
				errs <- fmt.Errorf("B ping %d: %v", i, err)
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Concurrent echo calls in both directions with distinct values.
	const echos = 32
	echoErrs := make(chan error, echos*2)
	for i := 0; i < echos; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("ea-%d", i)
			got, err := e2eimport.Echo(ctxA, want)
			if err != nil || got != want {
				echoErrs <- fmt.Errorf("A echo %d = %q, %v", i, got, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("eb-%d", i)
			got, err := e2eimport.Echo(ctxB, want)
			if err != nil || got != want {
				echoErrs <- fmt.Errorf("B echo %d = %q, %v", i, got, err)
			}
		}(i)
	}
	wg.Wait()
	close(echoErrs)
	for err := range echoErrs {
		if err != nil {
			t.Error(err)
		}
	}

	// Controlled out-of-order responses: A waits on ids 1..8 at B while
	// B waits on ids 101..108 at A; every handler registers before the
	// first release, and the releases run in reverse order so the
	// responses arrive out of order.
	const n = 8
	var idsA, idsB []uint32
	for i := 1; i <= n; i++ {
		idsA = append(idsA, uint32(i))
		idsB = append(idsB, uint32(100+i))
	}
	results := make(chan callResult, 2*n)
	for _, id := range idsA {
		go func(id uint32) {
			got, err := e2eimport.Wait(ctxA, id)
			results <- callResult{id: id, got: got, err: err}
		}(id)
	}
	for _, id := range idsB {
		go func(id uint32) {
			got, err := e2eimport.Wait(ctxB, id)
			results <- callResult{id: id, got: got, err: err}
		}(id)
	}
	eventually(t, "all wait handlers to register", func() bool {
		for _, id := range idsA {
			if !provider.IsWaiting(id) {
				return false
			}
		}
		for _, id := range idsB {
			if !provider.IsWaiting(id) {
				return false
			}
		}
		return true
	})
	for i := n - 1; i >= 0; i-- {
		provider.ReleaseWait(idsA[i])
		provider.ReleaseWait(idsB[i])
	}
	for i := 0; i < 2*n; i++ {
		r := <-results
		if r.err != nil || r.got != r.id {
			t.Errorf("Wait(%d) = %d, %v", r.id, r.got, r.err)
		}
	}

	closeAndWait(t, a)
	if err := b.Wait(); !errors.Is(err, io.EOF) {
		t.Fatalf("peer Wait = %v, want an io.EOF cause", err)
	}
	requireNoLeaks(t)
}

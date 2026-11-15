package atlas

import "testing"

// What DiscardDeferred drops is not run by the flush that follows, and is not kept.
func TestDiscardDeferredDropsWithoutRunning(t *testing.T) {
	// What other tests deferred is theirs to run: set it aside, and put it back.
	deferredM.Lock()
	others := deferred
	deferred = nil
	deferredM.Unlock()
	defer func() {
		deferredM.Lock()
		deferred = append(others, deferred...)
		deferredM.Unlock()
	}()

	ran := false
	appendDeferred(func() { ran = true })
	DiscardDeferred()
	if n := DeferredFuncCountForTesting(); n != 0 {
		t.Fatalf("%d deferred calls kept after DiscardDeferred, want 0", n)
	}
	flushDeferred()
	if ran {
		t.Fatal("a discarded call ran at the next flush")
	}
}

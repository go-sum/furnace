package worker

import (
	"testing"
	"time"
)

func TestBackoff_InitiallyReady(t *testing.T) {
	var b backoff
	if !b.ready() {
		t.Fatal("zero-value backoff must be ready")
	}
}

func TestBackoff_RecordSetsDelay(t *testing.T) {
	var b backoff
	b.record(time.Minute)
	if b.ready() {
		t.Fatal("expected not ready immediately after record")
	}
}

func TestBackoff_ExponentialGrowth(t *testing.T) {
	// Each record call should at least double the previous delay.
	// We check multipliers by inspecting nextPoll relative to now.
	base := time.Hour
	var b backoff

	before := time.Now()
	b.record(base) // failures=1 → 1× base
	after := time.Now()
	checkWindow(t, "1×", b.nextPoll, before.Add(base), after.Add(base))

	before = time.Now()
	b.record(base) // failures=2 → 2× base
	after = time.Now()
	checkWindow(t, "2×", b.nextPoll, before.Add(2*base), after.Add(2*base))

	before = time.Now()
	b.record(base) // failures=3 → 4× base
	after = time.Now()
	checkWindow(t, "4×", b.nextPoll, before.Add(4*base), after.Add(4*base))

	before = time.Now()
	b.record(base) // failures=4 → 8× base
	after = time.Now()
	checkWindow(t, "8×", b.nextPoll, before.Add(8*base), after.Add(8*base))
}

func TestBackoff_CapsAt10x(t *testing.T) {
	base := time.Hour
	var b backoff
	// Drive failures to the point where the cap kicks in (failures≥5 → multiplier=10).
	for i := 0; i < 5; i++ {
		b.record(base)
	}
	// Bracket the final record to get a tight time window.
	before := time.Now()
	b.record(base)
	after := time.Now()
	checkWindow(t, "10× cap", b.nextPoll, before.Add(10*base), after.Add(10*base))
}

func TestBackoff_ResetClearsState(t *testing.T) {
	var b backoff
	b.record(time.Minute)
	b.reset()
	if b.failures != 0 {
		t.Fatalf("expected failures=0 after reset, got %d", b.failures)
	}
	if !b.ready() {
		t.Fatal("expected ready after reset")
	}
}

func TestBackoff_ReadyAfterDelay(t *testing.T) {
	var b backoff
	b.record(time.Minute)
	// Simulate time passing by moving nextPoll into the past.
	b.nextPoll = time.Now().Add(-time.Second)
	if !b.ready() {
		t.Fatal("expected ready once nextPoll is in the past")
	}
}

// checkWindow asserts that got falls within [lo, hi] with a small tolerance.
func checkWindow(t *testing.T, label string, got, lo, hi time.Time) {
	t.Helper()
	tol := 100 * time.Millisecond
	if got.Before(lo.Add(-tol)) || got.After(hi.Add(tol)) {
		t.Fatalf("%s: nextPoll %v not in [%v, %v]", label, got, lo, hi)
	}
}

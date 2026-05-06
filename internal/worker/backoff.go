package worker

import "time"

type backoff struct {
	failures int
	nextPoll time.Time
}

// record increments the failure count and sets the next allowed poll time
// using exponential backoff capped at 10× base.
func (b *backoff) record(base time.Duration) {
	b.failures++
	multiplier := 1 << (b.failures - 1) // 1, 2, 4, 8, 16, ...
	if multiplier > 10 {
		multiplier = 10
	}
	b.nextPoll = time.Now().Add(time.Duration(multiplier) * base)
}

// reset clears all backoff state, making the app immediately eligible for polling.
func (b *backoff) reset() {
	b.failures = 0
	b.nextPoll = time.Time{}
}

// ready reports whether the app may be polled now.
func (b *backoff) ready() bool {
	return b.failures == 0 || !time.Now().Before(b.nextPoll)
}

// Package clock holds the [models.Clock] implementations mimir-server and the
// tests use: the system clock, and a Fake that tests drive so polling,
// re-registration and expiry happen without sleeping.
//
// Like objectstore it is shared infrastructure, not a Layer.
package clock

import (
	"context"
	"sync"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

var _ models.Clock = (*Fake)(nil)

// Real returns the system clock.
func Real() models.Clock {
	return realClock{}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Fake is a Clock that moves only when Advance is called. It is safe for
// concurrent use.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	// changed is closed and replaced whenever a waiter is added.
	changed chan struct{}
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

// NewFake returns a Fake whose time is start.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start, changed: make(chan struct{})}
}

// Now returns the Fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

// After returns a channel that receives once Advance has moved the time d
// past now. A d of zero or less fires at once.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, waiter{at: f.now.Add(d), ch: ch})
	close(f.changed)
	f.changed = make(chan struct{})

	return ch
}

// Advance moves the time forward by d and fires every waiter that is due.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = f.now.Add(d)
	pending := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at.After(f.now) {
			pending = append(pending, w)
			continue
		}
		w.ch <- f.now
	}
	f.waiters = pending
}

// WaitForWaiters blocks until at least n calls to After are waiting, or ctx
// is done. Call it before Advance so the goroutines being driven are parked.
func (f *Fake) WaitForWaiters(ctx context.Context, n int) error {
	for {
		f.mu.Lock()
		count, changed := len(f.waiters), f.changed
		f.mu.Unlock()

		if count >= n {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

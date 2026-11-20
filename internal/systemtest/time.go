package systemtest

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/internal/server"
)

// Day is how far Day moves the clock: the interval of the clients' daily
// re-Registration.
const Day = client.DefaultRegistrationInterval

// waiters is how many calls to the fake clock's After are parked once
// everything the harness runs is waiting for its next step: each running
// Deployable's client parks one for its poll and one for its next
// Registration, and each server parks one for its reconcile and one for its
// expiry. Anything new that
// waits on the clock must be added here. The caller holds h.mu.
func (h *Harness) waiters() int {
	const perDeployable = 2 // poll, re-Registration
	const perServer = 2     // reconcile, expiry

	return perDeployable*len(h.deployables) + perServer*len(h.replicas)
}

// Advance moves the clock d on, once every client and server is waiting for
// its next step. Events nobody awaited are discarded first, so what a test
// awaits afterwards belongs to this step. It doesn't wait for the work the
// step sets off: await it with RunningDeployable.AwaitRegistration, or use
// NextPoll, Reconcile or Day, which do.
func (h *Harness) Advance(t *testing.T, d time.Duration) {
	t.Helper()

	h.mu.Lock()
	deployables := slices.Clone(h.deployables)
	replicas := slices.Clone(h.replicas)
	waiters := h.waiters()
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(t.Context(), awaitTimeout)
	defer cancel()
	if err := h.Clock.WaitForWaiters(ctx, waiters); err != nil {
		t.Fatalf("clients and servers never waited for their next step: %v", err)
	}

	// Everything is parked, so no event is still on its way.
	for _, dep := range deployables {
		drain(dep.registered)
		drain(dep.polled)
	}
	for _, r := range replicas {
		for len(r.reconciled) > 0 {
			<-r.reconciled
		}
		for len(r.expired) > 0 {
			<-r.expired
		}
	}

	h.Clock.Advance(d)
}

// Elapse moves the clock d on, firing every timer that falls due, and returns
// when every client has polled and every server has reconciled and run its
// expiry. d must be at least server.DefaultExpiryInterval, so that all of them
// are due. Clients that have a re-Registration due run it too: await it with
// RunningDeployable.AwaitRegistration.
func (h *Harness) Elapse(t *testing.T, d time.Duration) {
	t.Helper()

	if d < server.DefaultExpiryInterval {
		t.Fatalf("Elapse(%v): shorter than the expiry interval %v, so not every timer would fire",
			d, server.DefaultExpiryInterval)
	}

	h.Advance(t, d)

	h.mu.Lock()
	deployables := slices.Clone(h.deployables)
	replicas := slices.Clone(h.replicas)
	h.mu.Unlock()

	for _, dep := range deployables {
		if err := await(t, dep.polled, "poll"); err != nil {
			t.Logf("poll by %s finished with: %v", dep.Name, err)
		}
	}
	for _, r := range replicas {
		r.awaitReconcile(t)
		r.awaitExpiry(t)
	}
}

// Day is Elapse for a day, the interval of the clients' re-Registration.
func (h *Harness) Day(t *testing.T) {
	t.Helper()

	h.Elapse(t, Day)
}

// RejectRegistrations makes the server answer every Registration with a 503,
// or stops that. Everything else it serves is unaffected.
func (h *Harness) RejectRegistrations(reject bool) { h.rejectRegistrations.Store(reject) }

// gateRegistrations is next, except that it rejects Registrations while
// RejectRegistrations says so.
func (h *Harness) gateRegistrations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.rejectRegistrations.Load() && r.URL.Path == "/v1/registrations" {
			http.Error(w, "registrations are rejected", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (r *Replica) awaitExpiry(t *testing.T) {
	t.Helper()

	select {
	case <-r.expired:
	case <-time.After(awaitTimeout):
		t.Fatal("timed out waiting for a server to run its expiry")
	}
}

func (r *Replica) awaitReconcile(t *testing.T) {
	t.Helper()

	select {
	case <-r.reconciled:
	case <-time.After(awaitTimeout):
		t.Fatal("timed out waiting for a server to reconcile")
	}
}

// AwaitExpiry waits for every server to finish a run of its expiry.
func (h *Harness) AwaitExpiry(t *testing.T) {
	t.Helper()

	h.mu.Lock()
	replicas := slices.Clone(h.replicas)
	h.mu.Unlock()

	for _, r := range replicas {
		r.awaitExpiry(t)
	}
}

// AwaitReconcile waits for every server to finish a reconcile.
func (h *Harness) AwaitReconcile(t *testing.T) {
	t.Helper()

	h.mu.Lock()
	replicas := slices.Clone(h.replicas)
	h.mu.Unlock()

	for _, r := range replicas {
		r.awaitReconcile(t)
	}
}

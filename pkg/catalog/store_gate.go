// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package catalog

import (
	"sync"
	"sync/atomic"
)

// storeGate coordinates the operations that read or write a PebbleDB instance
// with the close of that instance.
//
// PebbleDB panics rather than returning an error when a closed database is
// used, so a plain "is it closed?" flag is not enough: the flag can read open
// and the database can close before the operation issues its first call. A
// background scan that loses that race takes the whole process down.
//
// Callers wrap every database access in enter/leave. close then waits for the
// operations that entered before it and rejects the ones that arrive after it,
// which turns closing the store into a join rather than a signal. Callers that
// arrive late get an ordinary catalog-closed error and can treat it as an
// end-of-life condition.
//
// enter/leave pairs may nest on the same goroutine: the gate counts operations
// instead of excluding them, so an operation that calls another one cannot
// deadlock against a concurrent close.
type storeGate struct {
	// inFlight counts the operations currently permitted to use the database.
	inFlight atomic.Int64

	// closed is set once, by close, before it waits for inFlight to drain.
	closed atomic.Bool

	// drained is closed once the last operation leaves a closed gate.
	drained   chan struct{}
	drainOnce sync.Once
}

// newStoreGate returns an open gate.
func newStoreGate() *storeGate {
	return &storeGate{drained: make(chan struct{})}
}

// enter registers an operation against the database. On success the caller
// owns one in-flight slot and must release it with leave, typically deferred.
// Once close has been called it returns a catalog-closed error and the caller
// must not touch the database.
func (g *storeGate) enter() error {
	g.inFlight.Add(1)
	if g.closed.Load() {
		g.leave()
		return NewCatalogClosedError()
	}
	return nil
}

// leave releases the slot taken by a successful enter.
func (g *storeGate) leave() {
	if g.inFlight.Add(-1) == 0 && g.closed.Load() {
		g.signalDrained()
	}
}

// isClosed reports whether the gate has been closed.
func (g *storeGate) isClosed() bool {
	return g.closed.Load()
}

// close shuts the gate, waits for the operations already inside it to finish,
// and then runs closeFn, which performs the real close of the database.
// Subsequent calls are no-ops that report success, matching the idempotent
// Close contract of the storage layer.
func (g *storeGate) close(closeFn func() error) error {
	if g.closed.Swap(true) {
		return nil
	}

	// Operations that entered before the flag was set still hold the database
	// open; wait for them. Operations that enter afterwards observe the flag
	// and leave without touching it, because the increment they made is
	// ordered before the load of the flag that rejects them.
	if g.inFlight.Load() > 0 {
		<-g.drained
	} else {
		g.signalDrained()
	}

	return closeFn()
}

// signalDrained releases everyone waiting on the drain, exactly once.
func (g *storeGate) signalDrained() {
	g.drainOnce.Do(func() { close(g.drained) })
}

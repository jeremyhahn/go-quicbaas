// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package registrytest provides an in-memory registry.Backend for tests of
// the layers above pkg/registry (the service layer and every transport). It
// models the store properties the registry depends on — leases that delete
// their keys, a revision per write, watches that replay history from a start
// revision and then follow live changes, and compaction — so a test can drive
// the real registry, service and SDK client end to end without a qrdb node.
package registrytest

import (
	"context"
	"strconv"
	"strings"
	"sync"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// LeaseNotFoundError reports an operation on a lease the backend does not hold.
type LeaseNotFoundError struct {
	// LeaseID is the missing lease.
	LeaseID int64
}

// Error returns a human-readable description.
func (e *LeaseNotFoundError) Error() string {
	return "registrytest: lease " + strconv.FormatInt(e.LeaseID, 10) + " not found"
}

// CompactedError reports a watch whose start revision precedes the retained
// history. Its text carries go-qrdb's compaction reason, so
// qrdbtransport.ClassifyWatchError recognises it exactly as it does a real
// store's.
type CompactedError struct {
	// StartRevision is the revision the watch asked for.
	StartRevision int64
}

// Error returns a human-readable description.
func (e *CompactedError) Error() string {
	return "registrytest: required start revision has been compacted (" + strconv.FormatInt(e.StartRevision, 10) + ")"
}

type entry struct {
	value  []byte
	lease  int64
	modRev int64
}

type watcher struct {
	prefix string
	ch     chan qrdbtransport.WatchResponse
	ctx    context.Context
}

// MemoryBackend is an in-memory registry.Backend. The zero value is not
// usable; construct it with NewMemoryBackend.
type MemoryBackend struct {
	mu         sync.Mutex
	revision   int64
	compacted  int64
	nextLease  int64
	leases     map[int64]int64
	store      map[string]entry
	history    []*qrdbtransport.WatchEvent
	historyRev []int64
	watchers   []*watcher
}

// NewMemoryBackend returns an empty backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		leases: make(map[int64]int64),
		store:  make(map[string]entry),
	}
}

// Grant creates a lease.
func (m *MemoryBackend) Grant(_ context.Context, ttlSeconds int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextLease++
	m.leases[m.nextLease] = ttlSeconds
	return m.nextLease, nil
}

// KeepAlive reports the lease's TTL, or LeaseNotFoundError.
func (m *MemoryBackend) KeepAlive(_ context.Context, leaseID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl, ok := m.leases[leaseID]
	if !ok {
		return 0, &LeaseNotFoundError{LeaseID: leaseID}
	}
	return ttl, nil
}

// Revoke deletes a lease and every key attached to it, emitting a delete per
// key, as a lease expiry does.
func (m *MemoryBackend) Revoke(_ context.Context, leaseID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.leases[leaseID]; !ok {
		return &LeaseNotFoundError{LeaseID: leaseID}
	}
	delete(m.leases, leaseID)
	for key, e := range m.store {
		if e.lease != leaseID {
			continue
		}
		delete(m.store, key)
		m.revision++
		m.record(&qrdbtransport.WatchEvent{
			Type:   qrdbtransport.EventDelete,
			Kv:     &qrdbtransport.MVCCKeyValue{Key: []byte(key), ModRevision: m.revision},
			PrevKv: &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: e.value, ModRevision: e.modRev, LeaseID: e.lease},
		})
	}
	return nil
}

// Expire is Revoke under the name a test reads naturally when it simulates
// a dead host's lease lapsing.
func (m *MemoryBackend) Expire(leaseID int64) error {
	return m.Revoke(context.Background(), leaseID)
}

// PutWithLease writes a key attached to a lease.
func (m *MemoryBackend) PutWithLease(_ context.Context, key string, value []byte, leaseID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.leases[leaseID]; !ok {
		return &LeaseNotFoundError{LeaseID: leaseID}
	}
	m.revision++
	m.store[key] = entry{value: append([]byte(nil), value...), lease: leaseID, modRev: m.revision}
	m.record(&qrdbtransport.WatchEvent{
		Type: qrdbtransport.EventPut,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: append([]byte(nil), value...), ModRevision: m.revision, LeaseID: leaseID},
	})
	return nil
}

// Exists reports whether a key is present.
func (m *MemoryBackend) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.store[key]
	return ok, nil
}

// Range returns the keys under prefix and the current revision.
func (m *MemoryBackend) Range(_ context.Context, prefix string) (map[string][]byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte)
	for key, e := range m.store {
		if strings.HasPrefix(key, prefix) {
			out[key] = append([]byte(nil), e.value...)
		}
	}
	return out, m.revision, nil
}

// WatchPrefix replays the retained history from startRevision (0 = none),
// then delivers live changes until ctx ends. A start revision at or below
// the compaction point is refused with CompactedError.
func (m *MemoryBackend) WatchPrefix(ctx context.Context, prefix string, startRevision int64) (<-chan qrdbtransport.WatchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if startRevision > 0 && startRevision <= m.compacted {
		return nil, &CompactedError{StartRevision: startRevision}
	}
	w := &watcher{prefix: prefix, ch: make(chan qrdbtransport.WatchResponse, 1024), ctx: ctx}
	if startRevision > 0 {
		for i, ev := range m.history {
			if m.historyRev[i] >= startRevision && strings.HasPrefix(eventKey(ev), prefix) {
				w.ch <- qrdbtransport.WatchResponse{Events: []*qrdbtransport.WatchEvent{ev}, Revision: m.historyRev[i]}
			}
		}
	}
	m.watchers = append(m.watchers, w)
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, candidate := range m.watchers {
			if candidate == w {
				m.watchers = append(m.watchers[:i], m.watchers[i+1:]...)
				close(w.ch)
				return
			}
		}
	}()
	return w.ch, nil
}

// Compact discards the history at and below revision, so a watch from there
// is refused.
func (m *MemoryBackend) Compact(revision int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compacted = revision
	kept := 0
	for i, rev := range m.historyRev {
		if rev > revision {
			m.history[kept], m.historyRev[kept] = m.history[i], rev
			kept++
		}
	}
	m.history, m.historyRev = m.history[:kept], m.historyRev[:kept]
}

// CancelWatches ends every open watch with a store cancellation carrying
// reason, as a store evicting its watchers does.
func (m *MemoryBackend) CancelWatches(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.watchers {
		select {
		case w.ch <- qrdbtransport.WatchResponse{Canceled: true, CancelReason: reason, Revision: m.revision}:
		case <-w.ctx.Done():
		}
	}
}

// Revision returns the current store revision.
func (m *MemoryBackend) Revision() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revision
}

// record appends an event to the history and fans it out to live watchers.
// The caller holds m.mu.
func (m *MemoryBackend) record(ev *qrdbtransport.WatchEvent) {
	m.history = append(m.history, ev)
	m.historyRev = append(m.historyRev, m.revision)
	for _, w := range m.watchers {
		if !strings.HasPrefix(eventKey(ev), w.prefix) {
			continue
		}
		select {
		case w.ch <- qrdbtransport.WatchResponse{Events: []*qrdbtransport.WatchEvent{ev}, Revision: m.revision}:
		case <-w.ctx.Done():
		}
	}
}

// eventKey returns the key an event concerns.
func eventKey(ev *qrdbtransport.WatchEvent) string {
	if ev.Kv != nil {
		return string(ev.Kv.Key)
	}
	if ev.PrevKv != nil {
		return string(ev.PrevKv.Key)
	}
	return ""
}

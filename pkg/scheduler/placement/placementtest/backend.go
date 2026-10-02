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

// Package placementtest provides an in-memory go-qrdb stand-in for unit tests
// of the placement engine's callers.
package placementtest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// ErrInjected is the error a failing operation returns.
var ErrInjected = errors.New("placementtest: injected failure")

// Backend is an in-memory placement.Backend with go-qrdb's transaction
// semantics — a create-revision-zero compare holds only for an absent key, a
// value compare only for identical bytes — plus leases that delete their keys
// on revoke or expiry. It is for unit tests of placement's callers; the
// arbitration itself is proven against a real go-qrdb node in
// pkg/scheduler/placement.
//
// FailOp names one operation ("grant", "keepalive", "revoke", "putlease",
// "get", "put", "delete", "scan", "commit") that then fails with ErrInjected.
// CommitHook, when set, runs once before the next Commit, outside the lock,
// so a test can interleave a rival write.
type Backend struct {
	mu        sync.Mutex
	kv        map[string][]byte
	leases    map[int64][]string
	nextLease int64

	FailOp     string
	CommitHook func()
}

// New returns an empty Backend.
func New() *Backend {
	return &Backend{kv: map[string][]byte{}, leases: map[int64][]string{}}
}

func (m *Backend) fail(op string) error {
	if m.FailOp == op {
		return ErrInjected
	}
	return nil
}

func (m *Backend) Grant(_ context.Context, _ int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("grant"); err != nil {
		return 0, err
	}
	m.nextLease++
	m.leases[m.nextLease] = nil
	return m.nextLease, nil
}

func (m *Backend) KeepAlive(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("keepalive"); err != nil {
		return err
	}
	if _, ok := m.leases[id]; !ok {
		return &transport.LeaseNotFoundError{LeaseID: id}
	}
	return nil
}

func (m *Backend) Revoke(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("revoke"); err != nil {
		return err
	}
	keys, ok := m.leases[id]
	if !ok {
		return errors.New("lease not found")
	}
	for _, k := range keys {
		delete(m.kv, k)
	}
	delete(m.leases, id)
	return nil
}

// Expire simulates a lease running out.
func (m *Backend) Expire(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.leases[id] {
		delete(m.kv, k)
	}
	delete(m.leases, id)
}

func (m *Backend) PutWithLease(_ context.Context, k string, v []byte, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("putlease"); err != nil {
		return err
	}
	if _, ok := m.leases[id]; !ok {
		return errors.New("lease not found")
	}
	m.kv[k] = bytes.Clone(v)
	m.leases[id] = append(m.leases[id], k)
	return nil
}

func (m *Backend) Get(_ context.Context, k string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("get"); err != nil {
		return nil, err
	}
	v, ok := m.kv[k]
	if !ok {
		return nil, nil
	}
	return bytes.Clone(v), nil
}

func (m *Backend) Put(_ context.Context, k string, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("put"); err != nil {
		return err
	}
	m.kv[k] = bytes.Clone(v)
	return nil
}

func (m *Backend) Delete(_ context.Context, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("delete"); err != nil {
		return err
	}
	delete(m.kv, k)
	return nil
}

func (m *Backend) Scan(_ context.Context, p string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("scan"); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for k, v := range m.kv {
		if strings.HasPrefix(k, p) {
			out[k] = bytes.Clone(v)
		}
	}
	return out, nil
}

func (m *Backend) Commit(_ context.Context, req *transport.TransactionRequest) (bool, error) {
	if m.CommitHook != nil {
		hook := m.CommitHook
		m.CommitHook = nil
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("commit"); err != nil {
		return false, err
	}
	for _, c := range req.Compares {
		v, ok := m.kv[string(c.Key)]
		switch c.Target {
		case transport.CompareCreateRevision:
			if ok {
				return false, nil
			}
		case transport.CompareValue:
			if !ok || !bytes.Equal(v, c.Value) {
				return false, nil
			}
		default:
			return false, errors.New("unsupported compare")
		}
	}
	for _, op := range req.SuccessOps {
		switch op.Type {
		case transport.OpPut:
			m.kv[string(op.Key)] = bytes.Clone(op.Value)
		case transport.OpDelete:
			delete(m.kv, string(op.Key))
		default:
			return false, errors.New("unsupported op")
		}
	}
	return true, nil
}

// HasLease reports whether a lease is live.
func (m *Backend) HasLease(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.leases[id]
	return ok
}

// Set writes a raw value, bypassing every guard, so a test can plant a
// corrupt record.
func (m *Backend) Set(k string, v []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kv[k] = bytes.Clone(v)
}

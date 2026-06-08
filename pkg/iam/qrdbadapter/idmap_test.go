//go:build iam

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

package qrdbadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// -----------------------------------------------------------------------------
// Routing KVStore test double — mirrors pkg/iam's tenant-routing test double:
// it routes by the context tenant, isolates barrier tenants, and fails closed
// for a sealed tenant (NEVER falling back to the shared store).
// -----------------------------------------------------------------------------

type sealedError struct{}

func (sealedError) Error() string { return "tenant sealed" }

var errTenantSealedTest = sealedError{}

type memStore struct{ data map[string][]byte }

func newMemStore() *memStore { return &memStore{data: make(map[string][]byte)} }

func (m *memStore) Put(_ context.Context, key string, value []byte) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "memstore.get"}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memStore) Delete(_ context.Context, key string) error { delete(m.data, key); return nil }

func (m *memStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *memStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *memStore) keyCount() int { return len(m.data) }

type routingTestKV struct {
	shared *memStore
	stores map[string]*memStore
	sealed map[string]bool
}

func newRoutingTestKV() *routingTestKV {
	return &routingTestKV{shared: newMemStore(), stores: make(map[string]*memStore), sealed: make(map[string]bool)}
}

func (r *routingTestKV) barrierTenant(tenantID string) *memStore {
	st := newMemStore()
	r.stores[tenantID] = st
	return st
}

func (r *routingTestKV) sealTenant(tenantID string) { r.sealed[tenantID] = true }

func (r *routingTestKV) resolve(ctx context.Context) (*memStore, error) {
	tenantID := iam.TenantFromContext(ctx)
	if r.sealed[tenantID] {
		return nil, errTenantSealedTest
	}
	if tenantID == "" {
		return r.shared, nil
	}
	if st, ok := r.stores[tenantID]; ok {
		return st, nil
	}
	return r.shared, nil
}

func (r *routingTestKV) Put(ctx context.Context, key string, value []byte) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Put(ctx, key, value)
}

func (r *routingTestKV) Get(ctx context.Context, key string) ([]byte, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, key)
}

func (r *routingTestKV) Delete(ctx context.Context, key string) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}

func (r *routingTestKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Scan(ctx, prefix, fn)
}

func (r *routingTestKV) List(ctx context.Context, prefix string) ([]string, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, prefix)
}

func (r *routingTestKV) Exists(ctx context.Context, key string) (bool, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return false, err
	}
	return st.Exists(ctx, key)
}

var _ interfaces.KVStore = (*routingTestKV)(nil)

// -----------------------------------------------------------------------------
// IDMap tests
// -----------------------------------------------------------------------------

// Link persists both directions; ToQRDB and ToBroker resolve them, and the
// records land in the barrier tenant's store only.
func TestBackendIDMap_BidirectionalRoundTrip(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	const uuid = "11111111-2222-3333-4444-555555555555"
	const qid = uint64(987654321)

	if err := m.Link(ctx, "acme", uuid, qid); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if tenantStore.keyCount() != 2 {
		t.Fatalf("expected 2 index entries, got %d", tenantStore.keyCount())
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("idmap leaked into the shared store")
	}

	gotID, err := m.ToQRDB(ctx, "acme", uuid)
	if err != nil || gotID != qid {
		t.Fatalf("ToQRDB = %d, %v; want %d, nil", gotID, err, qid)
	}
	gotUUID, err := m.ToBroker(ctx, "acme", qid)
	if err != nil || gotUUID != uuid {
		t.Fatalf("ToBroker = %q, %v; want %q, nil", gotUUID, err, uuid)
	}
}

// Link is idempotent for an identical pair.
func TestBackendIDMap_LinkIdempotent(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if err := m.Link(ctx, "acme", "u", 1); err != nil {
		t.Fatalf("first Link: %v", err)
	}
	if err := m.Link(ctx, "acme", "u", 1); err != nil {
		t.Fatalf("idempotent Link: %v", err)
	}
}

// Link rejects a conflicting remap in either direction.
func TestBackendIDMap_LinkConflict(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if err := m.Link(ctx, "acme", "u", 1); err != nil {
		t.Fatalf("Link: %v", err)
	}
	// Same uuid -> different qrdb id.
	if err := m.Link(ctx, "acme", "u", 2); !errors.Is(err, ErrIDMapConflict) {
		t.Fatalf("forward remap = %v, want ErrIDMapConflict", err)
	}
	// Same qrdb id -> different uuid.
	if err := m.Link(ctx, "acme", "v", 1); !errors.Is(err, ErrIDMapConflict) {
		t.Fatalf("reverse remap = %v, want ErrIDMapConflict", err)
	}
}

// Lookups for an unknown id report not-found in both directions.
func TestBackendIDMap_NotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if _, err := m.ToQRDB(ctx, "acme", "nope"); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("ToQRDB(missing) = %v, want ErrIDMapNotFound", err)
	}
	if _, err := m.ToBroker(ctx, "acme", 404); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("ToBroker(missing) = %v, want ErrIDMapNotFound", err)
	}
}

// Unlink removes both directions; a subsequent lookup is not-found and a
// re-unlink is a no-op.
func TestBackendIDMap_Unlink(t *testing.T) {
	kv := newRoutingTestKV()
	store := kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if err := m.Link(ctx, "acme", "u", 1); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := m.Unlink(ctx, "acme", "u"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if store.keyCount() != 0 {
		t.Fatalf("expected 0 entries after unlink, got %d", store.keyCount())
	}
	if _, err := m.ToQRDB(ctx, "acme", "u"); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("ToQRDB after unlink = %v, want ErrIDMapNotFound", err)
	}
	if err := m.Unlink(ctx, "acme", "u"); err != nil {
		t.Fatalf("re-Unlink = %v, want nil", err)
	}
}

// Tenants are isolated: a correlation in one tenant is invisible in another.
func TestBackendIDMap_TenantIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if err := m.Link(ctx, "acme", "u", 1); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if _, err := m.ToQRDB(ctx, "globex", "u"); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("cross-tenant ToQRDB = %v, want ErrIDMapNotFound", err)
	}
}

// Invalid inputs are rejected with ErrIDMapInvalid on every verb.
func TestBackendIDMap_InvalidInput(t *testing.T) {
	m := NewBackendIDMap(newRoutingTestKV())
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"link empty tenant", func() error { return m.Link(ctx, "", "u", 1) }},
		{"link empty uuid", func() error { return m.Link(ctx, "acme", "", 1) }},
		{"link zero id", func() error { return m.Link(ctx, "acme", "u", 0) }},
		{"toqrdb empty tenant", func() error { _, e := m.ToQRDB(ctx, "", "u"); return e }},
		{"toqrdb empty uuid", func() error { _, e := m.ToQRDB(ctx, "acme", ""); return e }},
		{"tobroker empty tenant", func() error { _, e := m.ToBroker(ctx, "", 1); return e }},
		{"tobroker zero id", func() error { _, e := m.ToBroker(ctx, "acme", 0); return e }},
		{"unlink empty tenant", func() error { return m.Unlink(ctx, "", "u") }},
		{"unlink empty uuid", func() error { return m.Unlink(ctx, "acme", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, ErrIDMapInvalid) {
				t.Fatalf("%s = %v, want ErrIDMapInvalid", tc.name, err)
			}
		})
	}
}

// A sealed barrier tenant fails closed across Link and both lookups, never
// falling back to the shared store.
func TestBackendIDMap_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	if err := m.Link(ctx, "acme", "u", 1); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("Link on sealed = %v, want sealed error", err)
	}
	if _, err := m.ToQRDB(ctx, "acme", "u"); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("ToQRDB on sealed = %v, want sealed error", err)
	}
	if _, err := m.ToBroker(ctx, "acme", 1); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("ToBroker on sealed = %v, want sealed error", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("sealed tenant fell back to the shared store")
	}
}

// The IDMap sentinels expose human-readable messages via their typed payload.
func TestIDMapErrorMessages(t *testing.T) {
	if ErrIDMapNotFound.Error() == "" || ErrIDMapInvalid.Error() == "" || ErrIDMapConflict.Error() == "" {
		t.Fatal("idmap sentinels must carry non-empty messages")
	}
	var msg idMapMessageError
	if (&msg).Error() != "" {
		t.Fatalf("empty payload Error() = %q, want empty string", (&msg).Error())
	}
	const want = "boom"
	e := &idMapMessageError{message: want}
	if e.Error() != want {
		t.Fatalf("payload Error() = %q, want %q", e.Error(), want)
	}
}

// A corrupted forward index value surfaces as an internal error from ToQRDB
// rather than panicking or silently returning a wrong id.
func TestBackendIDMap_CorruptForwardValue(t *testing.T) {
	kv := newRoutingTestKV()
	st := kv.barrierTenant("acme")
	m := NewBackendIDMap(kv)
	ctx := context.Background()

	// Write a non-numeric value at the forward key directly.
	st.data[idMapForwardKey("acme", "u")] = []byte("not-a-number")
	if _, err := m.ToQRDB(ctx, "acme", "u"); err == nil {
		t.Fatal("ToQRDB on a corrupt value should error")
	} else if errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("corrupt value reported as not-found: %v", err)
	}
}

// faultyKV wraps routingTestKV and injects a failure on the Nth Put or Delete
// (1-based). It exercises the error-propagation branches inside Link/Unlink
// deterministically without any real I/O or sleeping.
type faultyKV struct {
	*routingTestKV
	failPutOn    int // fail on the Nth Put (0 = never)
	failDeleteOn int // fail on the Nth Delete (0 = never)
	puts         int
	deletes      int
}

var errInjected = errors.New("injected kv failure")

func (f *faultyKV) Put(ctx context.Context, key string, value []byte) error {
	f.puts++
	if f.failPutOn != 0 && f.puts == f.failPutOn {
		return errInjected
	}
	return f.routingTestKV.Put(ctx, key, value)
}

func (f *faultyKV) Delete(ctx context.Context, key string) error {
	f.deletes++
	if f.failDeleteOn != 0 && f.deletes == f.failDeleteOn {
		return errInjected
	}
	return f.routingTestKV.Delete(ctx, key)
}

func newFaultyKV() *faultyKV {
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	return &faultyKV{routingTestKV: base}
}

// Link surfaces a failure writing the forward index (first Put).
func TestBackendIDMap_LinkForwardWriteError(t *testing.T) {
	kv := newFaultyKV()
	kv.failPutOn = 1
	if err := NewBackendIDMap(kv).Link(context.Background(), "acme", "u", 1); !errors.Is(err, errInjected) {
		t.Fatalf("Link forward-write error = %v, want injected failure", err)
	}
}

// Link surfaces a failure writing the reverse index (second Put).
func TestBackendIDMap_LinkReverseWriteError(t *testing.T) {
	kv := newFaultyKV()
	kv.failPutOn = 2
	if err := NewBackendIDMap(kv).Link(context.Background(), "acme", "u", 1); !errors.Is(err, errInjected) {
		t.Fatalf("Link reverse-write error = %v, want injected failure", err)
	}
}

// Unlink surfaces a failure deleting the forward index (first Delete).
func TestBackendIDMap_UnlinkForwardDeleteError(t *testing.T) {
	kv := newFaultyKV()
	m := NewBackendIDMap(kv)
	if err := m.Link(context.Background(), "acme", "u", 1); err != nil {
		t.Fatalf("Link: %v", err)
	}
	kv.failDeleteOn = 1
	if err := m.Unlink(context.Background(), "acme", "u"); !errors.Is(err, errInjected) {
		t.Fatalf("Unlink forward-delete error = %v, want injected failure", err)
	}
}

// Unlink surfaces a failure deleting the reverse index (second Delete).
func TestBackendIDMap_UnlinkReverseDeleteError(t *testing.T) {
	kv := newFaultyKV()
	m := NewBackendIDMap(kv)
	if err := m.Link(context.Background(), "acme", "u", 1); err != nil {
		t.Fatalf("Link: %v", err)
	}
	kv.failDeleteOn = 2
	if err := m.Unlink(context.Background(), "acme", "u"); !errors.Is(err, errInjected) {
		t.Fatalf("Unlink reverse-delete error = %v, want injected failure", err)
	}
}

// withTenant leaves the context unchanged for an empty tenant (defensive path).
func TestWithTenant_EmptyTenant(t *testing.T) {
	ctx := context.Background()
	if got := withTenant(ctx, ""); got != ctx {
		t.Fatal("withTenant with empty tenant must return the original context")
	}
}

// failGetKV injects a non-not-found error on Get so Link's conflict-check read
// path propagates it (rather than treating it as "no existing binding").
type failGetKV struct{ *routingTestKV }

func (f *failGetKV) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errInjected
}

// Link propagates an unexpected error from the forward conflict-check Get.
func TestBackendIDMap_LinkForwardGetError(t *testing.T) {
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failGetKV{routingTestKV: base}
	if err := NewBackendIDMap(kv).Link(context.Background(), "acme", "u", 1); !errors.Is(err, errInjected) {
		t.Fatalf("Link forward-get error = %v, want injected failure", err)
	}
}

// failReverseGetKV returns not-found for the forward key and an injected error
// for the reverse key, so Link's reverse conflict-check read path propagates.
type failReverseGetKV struct{ *routingTestKV }

func (f *failReverseGetKV) Get(ctx context.Context, key string) ([]byte, error) {
	if len(key) >= len(keyPrefixIDMapReverse) && key[:len(keyPrefixIDMapReverse)] == keyPrefixIDMapReverse {
		return nil, errInjected
	}
	return f.routingTestKV.Get(ctx, key)
}

// Link propagates an unexpected error from the reverse conflict-check Get.
func TestBackendIDMap_LinkReverseGetError(t *testing.T) {
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failReverseGetKV{routingTestKV: base}
	if err := NewBackendIDMap(kv).Link(context.Background(), "acme", "u", 1); !errors.Is(err, errInjected) {
		t.Fatalf("Link reverse-get error = %v, want injected failure", err)
	}
}

// Unlink propagates an unexpected error from its ToQRDB read (non-not-found).
func TestBackendIDMap_UnlinkGetError(t *testing.T) {
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failGetKV{routingTestKV: base}
	if err := NewBackendIDMap(kv).Unlink(context.Background(), "acme", "u"); !errors.Is(err, errInjected) {
		t.Fatalf("Unlink get error = %v, want injected failure", err)
	}
}

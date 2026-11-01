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

package registry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeBackend is an in-memory Backend for tests. It models the two store
// properties the registry relies on: keys attached to a lease disappear when
// the lease is revoked, and every write advances a store revision.
type fakeBackend struct {
	mu        sync.Mutex
	nextLease int64
	revision  int64
	store     map[string][]byte
	leaseOf   map[string]int64
	keepAlive []int64
	revoked   []int64
	puts      []string
	watchCh   chan qrdbtransport.WatchResponse

	watchPrefix   string
	watchStartRev int64

	grantErr     error
	putErr       error
	putErrOnKey  string
	rangeErr     error
	existsErr    error
	watchErr     error
	keepAliveErr error
	revokeErr    error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		store:   make(map[string][]byte),
		leaseOf: make(map[string]int64),
		watchCh: make(chan qrdbtransport.WatchResponse, 8),
	}
}

func (f *fakeBackend) Grant(_ context.Context, _ int64) (int64, error) {
	if f.grantErr != nil {
		return 0, f.grantErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextLease++
	return f.nextLease, nil
}

func (f *fakeBackend) KeepAlive(_ context.Context, leaseID int64) (int64, error) {
	if f.keepAliveErr != nil {
		return 0, f.keepAliveErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keepAlive = append(f.keepAlive, leaseID)
	return 30, nil
}

func (f *fakeBackend) Revoke(_ context.Context, leaseID int64) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, leaseID)
	for k, id := range f.leaseOf {
		if id == leaseID {
			delete(f.store, k)
			delete(f.leaseOf, k)
			f.revision++
		}
	}
	return nil
}

func (f *fakeBackend) PutWithLease(_ context.Context, key string, value []byte, leaseID int64) error {
	if f.putErr != nil && (f.putErrOnKey == "" || strings.HasPrefix(key, f.putErrOnKey)) {
		return f.putErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[key] = value
	f.leaseOf[key] = leaseID
	f.puts = append(f.puts, key)
	f.revision++
	return nil
}

func (f *fakeBackend) Exists(_ context.Context, key string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.store[key]
	return ok, nil
}

func (f *fakeBackend) Range(_ context.Context, prefix string) (map[string][]byte, int64, error) {
	if f.rangeErr != nil {
		return nil, 0, f.rangeErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte)
	for k, v := range f.store {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, f.revision, nil
}

func (f *fakeBackend) WatchPrefix(_ context.Context, prefix string, startRevision int64) (<-chan qrdbtransport.WatchResponse, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	f.mu.Lock()
	f.watchPrefix = prefix
	f.watchStartRev = startRevision
	f.mu.Unlock()
	return f.watchCh, nil
}

func sampleInstance() ServiceInstance {
	return ServiceInstance{
		ID:       "inst-1",
		Service:  "payments",
		Address:  "10.0.0.5:8080",
		Metadata: map[string]string{"zone": "a"},
	}
}

func TestRegistry_RegisterAndResolve(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	inst := sampleInstance()

	leaseID, err := r.Register(sysCtx(), inst, 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if leaseID == 0 {
		t.Fatal("expected non-zero lease ID")
	}

	instances, revision, err := r.Resolve(sysCtx(), "payments")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != "inst-1" || instances[0].Address != "10.0.0.5:8080" {
		t.Fatalf("Resolve = %+v, want [inst-1@10.0.0.5:8080]", instances)
	}
	if revision != b.revision {
		t.Fatalf("Resolve revision = %d, want the store revision %d", revision, b.revision)
	}
}

// TestRegistry_RegisterWritesOwnershipMarkFirst proves the lease ownership
// mark is written before the instance, both under the registration's lease, so
// an instance is never visible under a lease its tenant cannot renew.
func TestRegistry_RegisterWritesOwnershipMarkFirst(t *testing.T) {
	b := newFakeBackend()
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	leaseID, err := New(b).Register(ctx, sampleInstance(), 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	wantMark := leaseKey("tenant-a", leaseID)
	wantInstance := instanceKey("tenant-a", "payments", "inst-1")
	if len(b.puts) != 2 || b.puts[0] != wantMark || b.puts[1] != wantInstance {
		t.Fatalf("puts = %v, want [%s %s]", b.puts, wantMark, wantInstance)
	}
	if b.leaseOf[wantMark] != leaseID || b.leaseOf[wantInstance] != leaseID {
		t.Fatalf("mark and instance must share lease %d, got %d and %d", leaseID, b.leaseOf[wantMark], b.leaseOf[wantInstance])
	}
}

func TestRegistry_ResolveOrdersByInstanceID(t *testing.T) {
	r := New(newFakeBackend())
	for _, id := range []string{"c", "a", "b"} {
		if _, err := r.Register(sysCtx(), ServiceInstance{ID: id, Service: "s", Address: "x:1"}, time.Second); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	instances, _, err := r.Resolve(sysCtx(), "s")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 3 || instances[0].ID != "a" || instances[1].ID != "b" || instances[2].ID != "c" {
		t.Fatalf("Resolve order = %+v, want a,b,c", instances)
	}
}

func TestRegistry_RegisterInvalid(t *testing.T) {
	r := New(newFakeBackend())
	cases := map[string]ServiceInstance{
		"missing id":       {Service: "x", Address: "a:1"},
		"missing address":  {Service: "x", ID: "i"},
		"oversize service": {Service: strings.Repeat("s", maxNameLen+1), ID: "i", Address: "a:1"},
		"oversize id":      {Service: "s", ID: strings.Repeat("i", maxNameLen+1), Address: "a:1"},
	}
	for name, inst := range cases {
		_, err := r.Register(sysCtx(), inst, time.Second)
		var ie *InvalidInstanceError
		if !errors.As(err, &ie) {
			t.Fatalf("%s: error = %T (%v), want *InvalidInstanceError", name, err, err)
		}
	}
}

func TestRegistry_TenantIsolation(t *testing.T) {
	r := New(newFakeBackend())
	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")

	// Same service+instance ID under two tenants must not collide; the tenant
	// comes from the request context (platform-standard pattern).
	if _, err := r.Register(ctxA, ServiceInstance{ID: "i", Service: "payments", Address: "addr-a"}, 10*time.Second); err != nil {
		t.Fatalf("tenant-a Register: %v", err)
	}
	if _, err := r.Register(ctxB, ServiceInstance{ID: "i", Service: "payments", Address: "addr-b"}, 10*time.Second); err != nil {
		t.Fatalf("tenant-b Register: %v", err)
	}

	a, _, err := r.Resolve(ctxA, "payments")
	if err != nil {
		t.Fatalf("tenant-a Resolve: %v", err)
	}
	if len(a) != 1 || a[0].Address != "addr-a" {
		t.Fatalf("tenant-a resolve = %+v, want only addr-a", a)
	}
	bb, _, err := r.Resolve(ctxB, "payments")
	if err != nil {
		t.Fatalf("tenant-b Resolve: %v", err)
	}
	if len(bb) != 1 || bb[0].Address != "addr-b" {
		t.Fatalf("tenant-b resolve = %+v, want only addr-b", bb)
	}

	// A single-tenant (no tenant in context) caller sees neither tenant's data.
	none, _, err := r.Resolve(sysCtx(), "payments")
	if err != nil {
		t.Fatalf("single-tenant Resolve: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("single-tenant resolve = %+v, want empty (isolated from tenants)", none)
	}
}

// TestRegistry_LeaseOwnershipIsTenantScoped proves one tenant can neither
// renew nor revoke another tenant's lease, and that the refusal does not say
// whether the lease exists.
func TestRegistry_LeaseOwnershipIsTenantScoped(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")

	leaseID, err := r.Register(ctxA, sampleInstance(), 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	var notFound *LeaseNotFoundError
	if _, err := r.Renew(ctxB, leaseID); !errors.As(err, &notFound) || notFound.LeaseID != leaseID {
		t.Fatalf("tenant-b Renew = %v, want *LeaseNotFoundError{%d}", err, leaseID)
	}
	if err := r.Deregister(ctxB, leaseID); !errors.As(err, &notFound) {
		t.Fatalf("tenant-b Deregister = %v, want *LeaseNotFoundError", err)
	}
	if err := r.Deregister(sysCtx(), leaseID); !errors.As(err, &notFound) {
		t.Fatalf("single-tenant Deregister = %v, want *LeaseNotFoundError", err)
	}
	if len(b.keepAlive) != 0 || len(b.revoked) != 0 {
		t.Fatalf("a refused caller reached the store: keepalives %v revokes %v", b.keepAlive, b.revoked)
	}

	// The owner still can.
	if _, err := r.Renew(ctxA, leaseID); err != nil {
		t.Fatalf("owner Renew: %v", err)
	}
	if err := r.Deregister(ctxA, leaseID); err != nil {
		t.Fatalf("owner Deregister: %v", err)
	}
}

func TestRegistry_InvalidTenantRejected(t *testing.T) {
	r := New(newFakeBackend())
	ctx := tenantctx.WithTenant(context.Background(), "bad/tenant")
	_, err := r.Register(ctx, sampleInstance(), time.Second)
	var ie *InvalidInstanceError
	if !errors.As(err, &ie) {
		t.Fatalf("error = %T (%v), want *InvalidInstanceError for invalid tenant", err, err)
	}
	if _, err := r.Renew(ctx, 1); !errors.As(err, &ie) {
		t.Fatalf("Renew error = %T (%v), want *InvalidInstanceError for invalid tenant", err, err)
	}
}

func TestRegistry_RegisterRejectsReservedNames(t *testing.T) {
	r := New(newFakeBackend())
	cases := []ServiceInstance{
		{Service: "foo/bar", ID: "x", Address: "a:1"}, // service with separator
		{Service: "svc", ID: "a/b", Address: "a:1"},   // id with separator
		{Service: "", ID: "x", Address: "a:1"},        // empty service
		{Service: "svc", ID: "", Address: "a:1"},      // empty id
	}
	for _, inst := range cases {
		_, err := r.Register(sysCtx(), inst, time.Second)
		var ie *InvalidInstanceError
		if !errors.As(err, &ie) {
			t.Fatalf("Register(%+v) error = %T (%v), want *InvalidInstanceError", inst, err, err)
		}
	}
}

func TestRegistry_ResolveAndWatchRejectReservedNames(t *testing.T) {
	r := New(newFakeBackend())
	if _, _, err := r.Resolve(sysCtx(), "foo/bar"); err == nil {
		t.Fatal("Resolve should reject a service name containing '/'")
	}
	if _, err := r.Watch(sysCtx(), "foo/bar", 0); err == nil {
		t.Fatal("Watch should reject a service name containing '/'")
	}
	var ie *InvalidInstanceError
	if _, err := r.Watch(sysCtx(), "svc", -1); !errors.As(err, &ie) {
		t.Fatalf("Watch(-1) = %v, want *InvalidInstanceError for a negative revision", err)
	}
}

func TestRegistry_RegisterPutFailureRevokesLease(t *testing.T) {
	for name, failingKey := range map[string]string{
		"ownership mark": leasePrefix,
		"instance":       keyPrefix,
	} {
		t.Run(name, func(t *testing.T) {
			b := newFakeBackend()
			b.putErr = errors.New("put boom")
			b.putErrOnKey = failingKey
			_, err := New(b).Register(sysCtx(), sampleInstance(), time.Second)
			var re *RegistryError
			if !errors.As(err, &re) || re.Op != "put" || !errors.Is(err, b.putErr) {
				t.Fatalf("error = %v, want RegistryError{Op:put} wrapping the put failure", err)
			}
			if len(b.revoked) != 1 {
				t.Fatalf("expected orphaned lease revoke, got %d revokes", len(b.revoked))
			}
		})
	}
}

// TestRegistry_RegisterReportsAFailedOrphanRevoke proves a cleanup failure is
// reported with the write failure rather than swallowed.
func TestRegistry_RegisterReportsAFailedOrphanRevoke(t *testing.T) {
	b := newFakeBackend()
	b.putErr = errors.New("put boom")
	b.revokeErr = errors.New("revoke boom")
	_, err := New(b).Register(sysCtx(), sampleInstance(), time.Second)
	if !errors.Is(err, b.putErr) || !errors.Is(err, b.revokeErr) {
		t.Fatalf("error = %v, want both the put and the revoke failure", err)
	}
}

func TestRegistry_RenewAndDeregister(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	leaseID, err := r.Register(sysCtx(), sampleInstance(), 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ttl, err := r.Renew(sysCtx(), leaseID)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if ttl != 30 {
		t.Fatalf("Renew ttl = %d, want the backend's 30", ttl)
	}
	if len(b.keepAlive) != 1 || b.keepAlive[0] != leaseID {
		t.Fatalf("keepAlive = %v, want [%d]", b.keepAlive, leaseID)
	}

	if err := r.Deregister(sysCtx(), leaseID); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	instances, _, err := r.Resolve(sysCtx(), "payments")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("after deregister Resolve = %+v, want empty", instances)
	}

	// The ownership mark went with the lease, so the lease is no longer
	// renewable.
	var notFound *LeaseNotFoundError
	if _, err := r.Renew(sysCtx(), leaseID); !errors.As(err, &notFound) {
		t.Fatalf("Renew after Deregister = %v, want *LeaseNotFoundError", err)
	}
}

func TestRegistry_RenewRejectsNonPositiveLease(t *testing.T) {
	r := New(newFakeBackend())
	var ie *InvalidInstanceError
	for _, id := range []int64{0, -3} {
		if _, err := r.Renew(sysCtx(), id); !errors.As(err, &ie) {
			t.Fatalf("Renew(%d) = %v, want *InvalidInstanceError", id, err)
		}
	}
}

func TestRegistry_ResolveSkipsMalformed(t *testing.T) {
	b := newFakeBackend()
	b.store[servicePrefix("", "payments")+"good"] = mustJSON(t, sampleInstance())
	b.store[servicePrefix("", "payments")+"bad"] = []byte("{not json")
	r := New(b)

	instances, _, err := r.Resolve(sysCtx(), "payments")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("Resolve returned %d, want 1 (malformed skipped)", len(instances))
	}
}

func TestRegistry_WatchPutAndDelete(t *testing.T) {
	b := newFakeBackend()
	r := New(b)

	ctx, cancel := context.WithCancel(sysCtx())
	defer cancel()
	ch, err := r.Watch(ctx, "payments", 12)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if b.watchStartRev != 12 || b.watchPrefix != servicePrefix("", "payments") {
		t.Fatalf("watch opened at %q rev %d, want %q rev 12", b.watchPrefix, b.watchStartRev, servicePrefix("", "payments"))
	}

	inst := sampleInstance()
	data := mustJSON(t, inst)
	key := instanceKey("", inst.Service, inst.ID)

	// PUT event carrying its own modification revision.
	b.watchCh <- qrdbtransport.WatchResponse{Revision: 99, Events: []*qrdbtransport.WatchEvent{{
		Type: qrdbtransport.EventPut,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: data, ModRevision: 13},
	}}}
	ev := recvEvent(t, ch)
	if ev.Type != Put || ev.Instance.ID != "inst-1" || ev.Revision != 13 || ev.Err != nil {
		t.Fatalf("PUT event = %+v, want Put inst-1 at revision 13", ev)
	}

	// DELETE event with PrevKv (lease expiry path) and no Kv revision: the
	// response header revision stands in.
	b.watchCh <- qrdbtransport.WatchResponse{Revision: 14, Events: []*qrdbtransport.WatchEvent{{
		Type:   qrdbtransport.EventDelete,
		PrevKv: &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: data},
	}}}
	ev = recvEvent(t, ch)
	if ev.Type != Delete || ev.Instance.ID != "inst-1" || ev.Instance.Address != "10.0.0.5:8080" || ev.Revision != 14 {
		t.Fatalf("DELETE event = %+v, want Delete inst-1 with address at revision 14", ev)
	}
}

func TestRegistry_WatchDeleteWithoutPrevKV(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	ctx, cancel := context.WithCancel(sysCtx())
	defer cancel()
	ch, err := r.Watch(ctx, "payments", 0)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	key := instanceKey("", "payments", "inst-9")
	b.watchCh <- qrdbtransport.WatchResponse{Events: []*qrdbtransport.WatchEvent{{
		Type: qrdbtransport.EventDelete,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte(key)},
	}}}
	ev := recvEvent(t, ch)
	if ev.Type != Delete || ev.Instance.ID != "inst-9" || ev.Instance.Service != "payments" {
		t.Fatalf("DELETE event = %+v, want Delete inst-9/payments derived from key", ev)
	}
}

// TestRegistry_WatchEndsWithTypedError proves a store-cancelled watch ends
// with one final event naming why, then a closed channel.
func TestRegistry_WatchEndsWithTypedError(t *testing.T) {
	cases := map[string]struct {
		reason string
		check  func(error) bool
	}{
		"compacted": {
			reason: "required start revision has been compacted",
			check: func(err error) bool {
				var c *WatchCompactedError
				return errors.As(err, &c) && c.FromRevision == 5 && c.Service == "payments"
			},
		},
		"evicted": {
			reason: "watcher evicted: slow consumer",
			check: func(err error) bool {
				var e *WatchEndedError
				return errors.As(err, &e) && e.Service == "payments"
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newFakeBackend()
			ch, err := New(b).Watch(sysCtx(), "payments", 5)
			if err != nil {
				t.Fatalf("Watch: %v", err)
			}
			b.watchCh <- qrdbtransport.WatchResponse{Canceled: true, CancelReason: tc.reason}
			ev := recvEvent(t, ch)
			if !tc.check(ev.Err) {
				t.Fatalf("final event Err = %T (%v), unexpected", ev.Err, ev.Err)
			}
			select {
			case _, open := <-ch:
				if open {
					t.Fatal("channel delivered after the final error event")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("channel not closed after the final error event")
			}
		})
	}
}

func TestRegistry_WatchOpenErrors(t *testing.T) {
	b := newFakeBackend()
	b.watchErr = errors.New("watch boom")
	_, err := New(b).Watch(sysCtx(), "payments", 0)
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "watch" {
		t.Fatalf("error = %v, want RegistryError{Op:watch}", err)
	}

	b.watchErr = errors.New("rpc error: required start revision has been compacted")
	_, err = New(b).Watch(sysCtx(), "payments", 3)
	var compacted *WatchCompactedError
	if !errors.As(err, &compacted) || compacted.FromRevision != 3 {
		t.Fatalf("error = %v, want *WatchCompactedError from revision 3", err)
	}
}

// TestRegistry_BackendErrors injects one backend failure per verb and checks
// that each verb wraps it in a RegistryError naming the failed operation.
func TestRegistry_BackendErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name   string
		inject func(b *fakeBackend)
		call   func(r *Registry, leaseID int64) error
		wantOp string
	}{
		{
			name:   "register grant",
			inject: func(b *fakeBackend) { b.grantErr = boom },
			call: func(r *Registry, _ int64) error {
				_, err := r.Register(sysCtx(), sampleInstance(), time.Second)
				return err
			},
			wantOp: "grant",
		},
		{
			name:   "renew keepalive",
			inject: func(b *fakeBackend) { b.keepAliveErr = boom },
			call: func(r *Registry, leaseID int64) error {
				_, err := r.Renew(sysCtx(), leaseID)
				return err
			},
			wantOp: "keepalive",
		},
		{
			name:   "renew ownership check",
			inject: func(b *fakeBackend) { b.existsErr = boom },
			call: func(r *Registry, leaseID int64) error {
				_, err := r.Renew(sysCtx(), leaseID)
				return err
			},
			wantOp: "exists",
		},
		{
			name:   "deregister revoke",
			inject: func(b *fakeBackend) { b.revokeErr = boom },
			call: func(r *Registry, leaseID int64) error {
				return r.Deregister(sysCtx(), leaseID)
			},
			wantOp: "revoke",
		},
		{
			name:   "resolve range",
			inject: func(b *fakeBackend) { b.rangeErr = boom },
			call: func(r *Registry, _ int64) error {
				_, _, err := r.Resolve(sysCtx(), "payments")
				return err
			},
			wantOp: "range",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newFakeBackend()
			r := New(b)
			leaseID, err := r.Register(sysCtx(), sampleInstance(), time.Second)
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			tc.inject(b)
			err = tc.call(r, leaseID)
			var re *RegistryError
			if !errors.As(err, &re) || re.Op != tc.wantOp {
				t.Fatalf("error = %v, want RegistryError{Op:%s}", err, tc.wantOp)
			}
		})
	}
}

func TestRegistry_RegisterSubSecondTTL(t *testing.T) {
	// A sub-second TTL is raised to MinTTL, not truncated to 0.
	b := &ttlRecordingBackend{fakeBackend: newFakeBackend()}
	if _, err := New(b).Register(sysCtx(), sampleInstance(), 100*time.Millisecond); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if b.granted != int64(MinTTL/time.Second) {
		t.Fatalf("granted TTL = %d, want %d", b.granted, int64(MinTTL/time.Second))
	}
}

// ttlRecordingBackend records the TTL a Grant asked for.
type ttlRecordingBackend struct {
	*fakeBackend
	granted int64
}

func (b *ttlRecordingBackend) Grant(ctx context.Context, ttlSeconds int64) (int64, error) {
	b.granted = ttlSeconds
	return b.fakeBackend.Grant(ctx, ttlSeconds)
}

func TestTranslateEvent_EdgeCases(t *testing.T) {
	// A nil event is skipped.
	if _, ok := translateEvent("", "svc", nil, 0); ok {
		t.Fatal("nil event should be skipped")
	}
	// PUT with nil Kv is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{Type: qrdbtransport.EventPut}, 0); ok {
		t.Fatal("PUT with nil Kv should be skipped")
	}
	// PUT with malformed value is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{
		Type: qrdbtransport.EventPut,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte("k"), Value: []byte("{bad")},
	}, 0); ok {
		t.Fatal("PUT with malformed value should be skipped")
	}
	// DELETE with malformed PrevKv falls back to key-derived ID.
	ev, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{
		Type:   qrdbtransport.EventDelete,
		PrevKv: &qrdbtransport.MVCCKeyValue{Key: []byte(instanceKey("", "svc", "id7")), Value: []byte("{bad")},
	}, 8)
	if !ok || ev.Type != Delete || ev.Instance.ID != "id7" || ev.Revision != 8 {
		t.Fatalf("DELETE with bad PrevKv = %+v ok=%v, want Delete id7 from key at revision 8", ev, ok)
	}
	// Unknown event type is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{Type: qrdbtransport.EventType(99)}, 0); ok {
		t.Fatal("unknown event type should be skipped")
	}
}

func TestEventType_String(t *testing.T) {
	if Put.String() != "PUT" || Delete.String() != "DELETE" || EventType(9).String() != "UNKNOWN" {
		t.Fatalf("EventType names = %s/%s/%s", Put, Delete, EventType(9))
	}
}

func TestRegistryErrors_Messages(t *testing.T) {
	re := &RegistryError{Op: "put", Err: errors.New("boom")}
	if re.Error() != "registry: put: boom" {
		t.Fatalf("RegistryError.Error() = %q", re.Error())
	}
	if !errors.Is(re, re.Unwrap()) {
		t.Fatal("RegistryError.Unwrap should return the wrapped error")
	}
	ie := &InvalidInstanceError{Reason: "service is required"}
	if ie.Error() != "registry: invalid service instance: service is required" {
		t.Fatalf("InvalidInstanceError.Error() = %q", ie.Error())
	}
	ln := &LeaseNotFoundError{LeaseID: 42}
	if !strings.Contains(ln.Error(), "42") {
		t.Fatalf("LeaseNotFoundError.Error() = %q, want the lease ID", ln.Error())
	}
	cause := errors.New("cause")
	wc := &WatchCompactedError{Service: "s", FromRevision: 7, Err: cause}
	if !strings.Contains(wc.Error(), "revision 7") || !errors.Is(wc, cause) {
		t.Fatalf("WatchCompactedError = %q, want the revision and an unwrappable cause", wc.Error())
	}
	we := &WatchEndedError{Service: "s", Err: cause}
	if !strings.Contains(we.Error(), "cause") || !errors.Is(we, cause) {
		t.Fatalf("WatchEndedError = %q, want an unwrappable cause", we.Error())
	}
}

func mustJSON(t *testing.T, inst ServiceInstance) []byte {
	t.Helper()
	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for registry event")
		return Event{}
	}
}

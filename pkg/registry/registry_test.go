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
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// fakeBackend is an in-memory Backend for tests.
type fakeBackend struct {
	mu        sync.Mutex
	nextLease int64
	store     map[string][]byte
	leaseOf   map[string]int64
	keepAlive []int64
	revoked   []int64
	watchCh   chan qrdbtransport.WatchResponse

	grantErr     error
	putErr       error
	scanErr      error
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

func (f *fakeBackend) KeepAlive(_ context.Context, leaseID int64) error {
	if f.keepAliveErr != nil {
		return f.keepAliveErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keepAlive = append(f.keepAlive, leaseID)
	return nil
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
		}
	}
	return nil
}

func (f *fakeBackend) PutWithLease(_ context.Context, key string, value []byte, leaseID int64) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[key] = value
	f.leaseOf[key] = leaseID
	return nil
}

func (f *fakeBackend) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte)
	for k, v := range f.store {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out[k] = v
		}
	}
	return out, nil
}

func (f *fakeBackend) WatchPrefix(_ context.Context, _ string) (<-chan qrdbtransport.WatchResponse, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
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

	leaseID, err := r.Register(context.Background(), inst, 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if leaseID == 0 {
		t.Fatal("expected non-zero lease ID")
	}

	instances, err := r.Resolve(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != "inst-1" || instances[0].Address != "10.0.0.5:8080" {
		t.Fatalf("Resolve = %+v, want [inst-1@10.0.0.5:8080]", instances)
	}
}

func TestRegistry_RegisterInvalid(t *testing.T) {
	r := New(newFakeBackend())
	_, err := r.Register(context.Background(), ServiceInstance{Service: "x"}, time.Second)
	var ie *InvalidInstanceError
	if !errors.As(err, &ie) {
		t.Fatalf("error = %T (%v), want *InvalidInstanceError", err, err)
	}
}

func TestRegistry_TenantIsolation(t *testing.T) {
	r := New(newFakeBackend())
	ctxA := iam.ContextWithTenant(context.Background(), "tenant-a")
	ctxB := iam.ContextWithTenant(context.Background(), "tenant-b")

	// Same service+instance ID under two tenants must not collide; the tenant
	// comes from the request context (platform-standard pattern).
	if _, err := r.Register(ctxA, ServiceInstance{ID: "i", Service: "payments", Address: "addr-a"}, 10*time.Second); err != nil {
		t.Fatalf("tenant-a Register: %v", err)
	}
	if _, err := r.Register(ctxB, ServiceInstance{ID: "i", Service: "payments", Address: "addr-b"}, 10*time.Second); err != nil {
		t.Fatalf("tenant-b Register: %v", err)
	}

	a, err := r.Resolve(ctxA, "payments")
	if err != nil {
		t.Fatalf("tenant-a Resolve: %v", err)
	}
	if len(a) != 1 || a[0].Address != "addr-a" {
		t.Fatalf("tenant-a resolve = %+v, want only addr-a", a)
	}
	bb, err := r.Resolve(ctxB, "payments")
	if err != nil {
		t.Fatalf("tenant-b Resolve: %v", err)
	}
	if len(bb) != 1 || bb[0].Address != "addr-b" {
		t.Fatalf("tenant-b resolve = %+v, want only addr-b", bb)
	}

	// A single-tenant (no tenant in context) caller sees neither tenant's data.
	none, err := r.Resolve(context.Background(), "payments")
	if err != nil {
		t.Fatalf("single-tenant Resolve: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("single-tenant resolve = %+v, want empty (isolated from tenants)", none)
	}
}

func TestRegistry_InvalidTenantRejected(t *testing.T) {
	r := New(newFakeBackend())
	ctx := iam.ContextWithTenant(context.Background(), "bad/tenant")
	_, err := r.Register(ctx, sampleInstance(), time.Second)
	var ie *InvalidInstanceError
	if !errors.As(err, &ie) {
		t.Fatalf("error = %T (%v), want *InvalidInstanceError for invalid tenant", err, err)
	}
}

func TestRegistry_RegisterRejectsReservedNames(t *testing.T) {
	r := New(newFakeBackend())
	cases := []ServiceInstance{
		{Service: "foo/bar", ID: "x"}, // service with separator
		{Service: "svc", ID: "a/b"},   // id with separator
		{Service: "", ID: "x"},        // empty service
		{Service: "svc", ID: ""},      // empty id
	}
	for _, inst := range cases {
		_, err := r.Register(context.Background(), inst, time.Second)
		var ie *InvalidInstanceError
		if !errors.As(err, &ie) {
			t.Fatalf("Register(%+v) error = %T (%v), want *InvalidInstanceError", inst, err, err)
		}
	}
}

func TestRegistry_ResolveAndWatchRejectReservedNames(t *testing.T) {
	r := New(newFakeBackend())
	if _, err := r.Resolve(context.Background(), "foo/bar"); err == nil {
		t.Fatal("Resolve should reject a service name containing '/'")
	}
	if _, err := r.Watch(context.Background(), "foo/bar"); err == nil {
		t.Fatal("Watch should reject a service name containing '/'")
	}
}

func TestRegistry_RegisterPutFailureRevokesLease(t *testing.T) {
	b := newFakeBackend()
	b.putErr = errors.New("put boom")
	r := New(b)

	_, err := r.Register(context.Background(), sampleInstance(), time.Second)
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "put" {
		t.Fatalf("error = %v, want RegistryError{Op:put}", err)
	}
	if len(b.revoked) != 1 {
		t.Fatalf("expected orphaned lease revoke, got %d revokes", len(b.revoked))
	}
}

func TestRegistry_RenewAndDeregister(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	leaseID, err := r.Register(context.Background(), sampleInstance(), 10*time.Second)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Renew(context.Background(), leaseID); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if len(b.keepAlive) != 1 || b.keepAlive[0] != leaseID {
		t.Fatalf("keepAlive = %v, want [%d]", b.keepAlive, leaseID)
	}

	if err := r.Deregister(context.Background(), leaseID); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	instances, _ := r.Resolve(context.Background(), "payments")
	if len(instances) != 0 {
		t.Fatalf("after deregister Resolve = %+v, want empty", instances)
	}
}

func TestRegistry_ResolveSkipsMalformed(t *testing.T) {
	b := newFakeBackend()
	b.store[servicePrefix("", "payments")+"good"] = mustJSON(t, sampleInstance())
	b.store[servicePrefix("", "payments")+"bad"] = []byte("{not json")
	r := New(b)

	instances, err := r.Resolve(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("Resolve returned %d, want 1 (malformed skipped)", len(instances))
	}
}

func TestRegistry_ResolveError(t *testing.T) {
	b := newFakeBackend()
	b.scanErr = errors.New("scan boom")
	_, err := New(b).Resolve(context.Background(), "payments")
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "scan" {
		t.Fatalf("error = %v, want RegistryError{Op:scan}", err)
	}
}

func TestRegistry_WatchPutAndDelete(t *testing.T) {
	b := newFakeBackend()
	r := New(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Watch(ctx, "payments")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	inst := sampleInstance()
	data := mustJSON(t, inst)
	key := instanceKey("", inst.Service, inst.ID)

	// PUT event.
	b.watchCh <- qrdbtransport.WatchResponse{Events: []*qrdbtransport.WatchEvent{{
		Type: qrdbtransport.EventPut,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: data},
	}}}
	ev := recvEvent(t, ch)
	if ev.Type != Put || ev.Instance.ID != "inst-1" {
		t.Fatalf("PUT event = %+v, want Put inst-1", ev)
	}

	// DELETE event with PrevKv (lease expiry path).
	b.watchCh <- qrdbtransport.WatchResponse{Events: []*qrdbtransport.WatchEvent{{
		Type:   qrdbtransport.EventDelete,
		PrevKv: &qrdbtransport.MVCCKeyValue{Key: []byte(key), Value: data},
	}}}
	ev = recvEvent(t, ch)
	if ev.Type != Delete || ev.Instance.ID != "inst-1" || ev.Instance.Address != "10.0.0.5:8080" {
		t.Fatalf("DELETE event = %+v, want Delete inst-1 with address", ev)
	}
}

func TestRegistry_WatchDeleteWithoutPrevKV(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := r.Watch(ctx, "payments")
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

func TestRegistry_WatchError(t *testing.T) {
	b := newFakeBackend()
	b.watchErr = errors.New("watch boom")
	_, err := New(b).Watch(context.Background(), "payments")
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "watch" {
		t.Fatalf("error = %v, want RegistryError{Op:watch}", err)
	}
}

func TestRegistry_RegisterGrantError(t *testing.T) {
	b := newFakeBackend()
	b.grantErr = errors.New("grant boom")
	_, err := New(b).Register(context.Background(), sampleInstance(), time.Second)
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "grant" {
		t.Fatalf("error = %v, want RegistryError{Op:grant}", err)
	}
}

func TestRegistry_RenewError(t *testing.T) {
	b := newFakeBackend()
	b.keepAliveErr = errors.New("keepalive boom")
	err := New(b).Renew(context.Background(), 1)
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "keepalive" {
		t.Fatalf("error = %v, want RegistryError{Op:keepalive}", err)
	}
}

func TestRegistry_DeregisterError(t *testing.T) {
	b := newFakeBackend()
	b.revokeErr = errors.New("revoke boom")
	err := New(b).Deregister(context.Background(), 1)
	var re *RegistryError
	if !errors.As(err, &re) || re.Op != "revoke" {
		t.Fatalf("error = %v, want RegistryError{Op:revoke}", err)
	}
}

func TestRegistry_RegisterSubSecondTTL(t *testing.T) {
	// A sub-second TTL is floored to 1 second, not 0.
	b := newFakeBackend()
	if _, err := New(b).Register(context.Background(), sampleInstance(), 100*time.Millisecond); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestTranslateEvent_EdgeCases(t *testing.T) {
	// PUT with nil Kv is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{Type: qrdbtransport.EventPut}); ok {
		t.Fatal("PUT with nil Kv should be skipped")
	}
	// PUT with malformed value is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{
		Type: qrdbtransport.EventPut,
		Kv:   &qrdbtransport.MVCCKeyValue{Key: []byte("k"), Value: []byte("{bad")},
	}); ok {
		t.Fatal("PUT with malformed value should be skipped")
	}
	// DELETE with malformed PrevKv falls back to key-derived ID.
	ev, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{
		Type:   qrdbtransport.EventDelete,
		PrevKv: &qrdbtransport.MVCCKeyValue{Key: []byte(instanceKey("", "svc", "id7")), Value: []byte("{bad")},
	})
	if !ok || ev.Type != Delete || ev.Instance.ID != "id7" {
		t.Fatalf("DELETE with bad PrevKv = %+v ok=%v, want Delete id7 from key", ev, ok)
	}
	// Unknown event type is skipped.
	if _, ok := translateEvent("", "svc", &qrdbtransport.WatchEvent{Type: qrdbtransport.EventType(99)}); ok {
		t.Fatal("unknown event type should be skipped")
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
	ie := &InvalidInstanceError{Reason: "service and id are required"}
	if ie.Error() != "registry: invalid service instance: service and id are required" {
		t.Fatalf("InvalidInstanceError.Error() = %q", ie.Error())
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

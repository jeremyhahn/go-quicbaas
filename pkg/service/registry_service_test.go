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

package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry/registrytest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// memRegistryBackend wraps the shared in-memory backend with the two hooks
// these tests need: an injectable range failure and the start revision the
// last watch asked for.
type memRegistryBackend struct {
	*registrytest.MemoryBackend
	fromRev  int64
	rangeErr error
}

func newMemRegistryBackend() *memRegistryBackend {
	return &memRegistryBackend{MemoryBackend: registrytest.NewMemoryBackend()}
}

func (m *memRegistryBackend) Range(ctx context.Context, prefix string) (map[string][]byte, int64, error) {
	if m.rangeErr != nil {
		return nil, 0, m.rangeErr
	}
	return m.MemoryBackend.Range(ctx, prefix)
}

func (m *memRegistryBackend) WatchPrefix(ctx context.Context, prefix string, startRevision int64) (<-chan qrdbtransport.WatchResponse, error) {
	m.fromRev = startRevision
	return m.MemoryBackend.WatchPrefix(ctx, prefix, startRevision)
}

func registryInstance() RegistryServiceInstance {
	return RegistryServiceInstance{ID: "i-1", Service: "api", Address: "10.1.0.4:443", Metadata: map[string]string{"zone": "z1"}}
}

func codeOf(t *testing.T, err error) errors.ErrorCode {
	t.Helper()
	var qb *errors.QBError
	if !stderrors.As(err, &qb) {
		t.Fatalf("error %T (%v) is not a *QBError", err, err)
	}
	return qb.Code
}

func TestRegistryService_RegisterResolveRenewDeregister(t *testing.T) {
	backend := newMemRegistryBackend()
	svc := NewRegistryServiceFromBackend(backend)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	reg, err := svc.Register(ctx, &RegisterServiceInstanceRequest{Instance: registryInstance(), TTLSeconds: 15})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.LeaseID == 0 || reg.TTLSeconds != 15 {
		t.Fatalf("Register = %+v, want a lease with TTL 15", reg)
	}

	res, err := svc.Resolve(ctx, &ResolveServiceRequest{Service: "api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Instances) != 1 || res.Instances[0].Address != "10.1.0.4:443" || res.Revision != backend.Revision() {
		t.Fatalf("Resolve = %+v, want the instance at revision %d", res, backend.Revision())
	}

	renewed, err := svc.Renew(ctx, &RenewServiceLeaseRequest{LeaseID: reg.LeaseID})
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if renewed.LeaseID != reg.LeaseID || renewed.TTLSeconds != 15 {
		t.Fatalf("Renew = %+v, want lease %d reset to its 15s", renewed, reg.LeaseID)
	}

	if err := svc.Deregister(ctx, &DeregisterServiceInstanceRequest{LeaseID: reg.LeaseID}); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	res, err = svc.Resolve(ctx, &ResolveServiceRequest{Service: "api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Instances) != 0 {
		t.Fatalf("Resolve after Deregister = %+v, want empty", res.Instances)
	}
}

func TestRegistryService_NilRequests(t *testing.T) {
	svc := NewRegistryServiceFromBackend(newMemRegistryBackend())
	ctx := tenantctx.WithSystem(context.Background())
	if _, err := svc.Register(ctx, nil); err != ErrRegistryNilRequest {
		t.Fatalf("Register(nil) = %v", err)
	}
	if _, err := svc.Renew(ctx, nil); err != ErrRegistryNilRequest {
		t.Fatalf("Renew(nil) = %v", err)
	}
	if err := svc.Deregister(ctx, nil); err != ErrRegistryNilRequest {
		t.Fatalf("Deregister(nil) = %v", err)
	}
	if _, err := svc.Resolve(ctx, nil); err != ErrRegistryNilRequest {
		t.Fatalf("Resolve(nil) = %v", err)
	}
	if _, err := svc.Watch(ctx, nil); err != ErrRegistryNilRequest {
		t.Fatalf("Watch(nil) = %v", err)
	}
}

func TestRegistryService_RegisterTTLBounds(t *testing.T) {
	svc := NewRegistryServiceFromBackend(newMemRegistryBackend())
	for _, ttl := range []int64{0, RegistryMinTTLSeconds - 1, RegistryMaxTTLSeconds + 1} {
		_, err := svc.Register(tenantctx.WithSystem(context.Background()), &RegisterServiceInstanceRequest{Instance: registryInstance(), TTLSeconds: ttl})
		if err != ErrRegistryTTLRange {
			t.Fatalf("Register(ttl=%d) = %v, want ErrRegistryTTLRange", ttl, err)
		}
	}
	for _, ttl := range []int64{RegistryMinTTLSeconds, RegistryMaxTTLSeconds} {
		if _, err := svc.Register(tenantctx.WithSystem(context.Background()), &RegisterServiceInstanceRequest{Instance: registryInstance(), TTLSeconds: ttl}); err != nil {
			t.Fatalf("Register(ttl=%d): %v", ttl, err)
		}
	}
}

// TestRegistryService_ErrorCodes proves each registry failure reaches the
// transports with the status code they map, so a missing lease is a 404 on
// REST and NotFound on gRPC rather than a generic failure.
func TestRegistryService_ErrorCodes(t *testing.T) {
	backend := newMemRegistryBackend()
	svc := NewRegistryServiceFromBackend(backend)
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.Register(ctx, &RegisterServiceInstanceRequest{Instance: RegistryServiceInstance{Service: "api"}, TTLSeconds: 5})
	if got := codeOf(t, err); got != errors.ErrInvalidInput {
		t.Fatalf("invalid instance code = %v, want ErrInvalidInput", got)
	}
	var invalid *registry.InvalidInstanceError
	if !stderrors.As(err, &invalid) {
		t.Fatalf("invalid instance error %v does not unwrap to *registry.InvalidInstanceError", err)
	}

	_, err = svc.Renew(ctx, &RenewServiceLeaseRequest{LeaseID: 999})
	if got := codeOf(t, err); got != errors.ErrNotFound {
		t.Fatalf("unknown lease code = %v, want ErrNotFound", got)
	}
	if err := svc.Deregister(ctx, &DeregisterServiceInstanceRequest{LeaseID: 999}); codeOf(t, err) != errors.ErrNotFound {
		t.Fatalf("Deregister unknown lease = %v, want ErrNotFound", err)
	}

	backend.rangeErr = stderrors.New("store down")
	_, err = svc.Resolve(ctx, &ResolveServiceRequest{Service: "api"})
	if got := codeOf(t, err); got != errors.ErrUnavailable {
		t.Fatalf("store failure code = %v, want ErrUnavailable", got)
	}

	if got := registryErrorCode(&registry.WatchCompactedError{Service: "api", Err: stderrors.New("x")}); got != errors.ErrOutOfRange {
		t.Fatalf("compacted code = %v, want ErrOutOfRange", got)
	}
}

func TestRegistryService_UnavailableStore(t *testing.T) {
	down := stderrors.New("no qrdb client")
	svc := newRegistryService(func() (*registry.Registry, error) { return nil, down })
	ctx := tenantctx.WithSystem(context.Background())

	checks := map[string]error{}
	_, checks["register"] = svc.Register(ctx, &RegisterServiceInstanceRequest{Instance: registryInstance(), TTLSeconds: 5})
	_, checks["renew"] = svc.Renew(ctx, &RenewServiceLeaseRequest{LeaseID: 1})
	checks["deregister"] = svc.Deregister(ctx, &DeregisterServiceInstanceRequest{LeaseID: 1})
	_, checks["resolve"] = svc.Resolve(ctx, &ResolveServiceRequest{Service: "api"})
	_, checks["watch"] = svc.Watch(ctx, &WatchServiceRequest{Service: "api"})
	for name, err := range checks {
		if codeOf(t, err) != errors.ErrUnavailable || !stderrors.Is(err, down) {
			t.Fatalf("%s = %v, want ErrUnavailable wrapping the store failure", name, err)
		}
	}
}

func TestRegistryService_Watch(t *testing.T) {
	backend := newMemRegistryBackend()
	svc := NewRegistryServiceFromBackend(backend)
	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	defer cancel()

	reg, err := svc.Register(ctx, &RegisterServiceInstanceRequest{Instance: registryInstance(), TTLSeconds: 5})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Watching from the first revision replays the registration's own write
	// (its ownership mark is outside the service prefix and is not seen).
	events, err := svc.Watch(ctx, &WatchServiceRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if backend.fromRev != 1 {
		t.Fatalf("watch opened from %d, want 1", backend.fromRev)
	}
	select {
	case ev := <-events:
		if ev.Type != RegistryEventPut || ev.Instance.ID != "i-1" || ev.Revision != 2 {
			t.Fatalf("event = %+v, want Put i-1 at revision 2", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event delivered")
	}

	// The lease lapsing deletes the instance and every watcher sees it.
	if err := backend.Expire(reg.LeaseID); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Type != RegistryEventDelete || ev.Instance.Address != "10.1.0.4:443" {
			t.Fatalf("event = %+v, want Delete of the expired instance", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delete delivered")
	}

	if _, err := svc.Watch(ctx, &WatchServiceRequest{Service: "a/b"}); codeOf(t, err) != errors.ErrInvalidInput {
		t.Fatalf("Watch invalid service = %v, want ErrInvalidInput", err)
	}

	backend.Compact(backend.Revision())
	if _, err := svc.Watch(ctx, &WatchServiceRequest{Service: "api", FromRevision: 1}); codeOf(t, err) != errors.ErrOutOfRange {
		t.Fatalf("Watch from a compacted revision = %v, want ErrOutOfRange", err)
	}
}

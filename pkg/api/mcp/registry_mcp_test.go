//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeRegistryService records requests and replays canned answers.
type fakeRegistryService struct {
	gotRegister   *service.RegisterServiceInstanceRequest
	gotRenew      *service.RenewServiceLeaseRequest
	gotDeregister *service.DeregisterServiceInstanceRequest
	gotResolve    *service.ResolveServiceRequest
	gotWatch      *service.WatchServiceRequest

	events   chan service.RegistryEvent
	err      error
	watchErr error
}

func (f *fakeRegistryService) Register(_ context.Context, req *service.RegisterServiceInstanceRequest) (*service.RegisterServiceInstanceResponse, error) {
	f.gotRegister = req
	if f.err != nil {
		return nil, f.err
	}
	return &service.RegisterServiceInstanceResponse{LeaseID: 9007199254740993, TTLSeconds: req.TTLSeconds}, nil
}

func (f *fakeRegistryService) Renew(_ context.Context, req *service.RenewServiceLeaseRequest) (*service.RenewServiceLeaseResponse, error) {
	f.gotRenew = req
	if f.err != nil {
		return nil, f.err
	}
	return &service.RenewServiceLeaseResponse{LeaseID: req.LeaseID, TTLSeconds: 8}, nil
}

func (f *fakeRegistryService) Deregister(_ context.Context, req *service.DeregisterServiceInstanceRequest) error {
	f.gotDeregister = req
	return f.err
}

func (f *fakeRegistryService) Resolve(_ context.Context, req *service.ResolveServiceRequest) (*service.ResolveServiceResponse, error) {
	f.gotResolve = req
	if f.err != nil {
		return nil, f.err
	}
	return &service.ResolveServiceResponse{
		Instances: []service.RegistryServiceInstance{{ID: "a", Service: req.Service, Address: "10.0.0.1:80", Metadata: map[string]string{"k": "v"}}},
		Revision:  21,
	}, nil
}

func (f *fakeRegistryService) Watch(_ context.Context, req *service.WatchServiceRequest) (<-chan service.RegistryEvent, error) {
	f.gotWatch = req
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	return f.events, nil
}

func newRegistryToolRegistry(svc service.RegistryService) *ToolRegistry {
	return &ToolRegistry{service: &mockService{registrySvc: svc}}
}

func TestRegistryTools_RegisterRenewDeregisterResolve(t *testing.T) {
	fake := &fakeRegistryService{}
	reg := newRegistryToolRegistry(fake)
	ctx := context.Background()

	out, err := reg.executeRegistryTool(ctx, "quicbaas_registry_register", map[string]interface{}{
		"service": "api", "id": "i", "address": "10.0.0.2:443",
		"metadata":    map[string]interface{}{"zone": "z"},
		"ttl_seconds": float64(30),
	})
	require.NoError(t, err)
	result := out.(map[string]interface{})
	assert.Equal(t, "9007199254740993", result["lease_id"], "a lease ID beyond 2^53 survives as a string")
	assert.Equal(t, service.RegistryServiceInstance{ID: "i", Service: "api", Address: "10.0.0.2:443", Metadata: map[string]string{"zone": "z"}}, fake.gotRegister.Instance)
	assert.Equal(t, int64(30), fake.gotRegister.TTLSeconds)

	out, err = reg.executeRegistryTool(ctx, "quicbaas_registry_renew", map[string]interface{}{"lease_id": "9007199254740993"})
	require.NoError(t, err)
	assert.Equal(t, int64(9007199254740993), fake.gotRenew.LeaseID)
	assert.Equal(t, int64(8), out.(map[string]interface{})["ttl_seconds"])

	out, err = reg.executeRegistryTool(ctx, "quicbaas_registry_deregister", map[string]interface{}{"lease_id": float64(12)})
	require.NoError(t, err)
	assert.Equal(t, int64(12), fake.gotDeregister.LeaseID)
	assert.Equal(t, true, out.(map[string]interface{})["deregistered"])

	out, err = reg.executeRegistryTool(ctx, "quicbaas_registry_resolve", map[string]interface{}{"service": "api"})
	require.NoError(t, err)
	resolved := out.(map[string]interface{})
	assert.Equal(t, int64(21), resolved["revision"])
	instances := resolved["instances"].([]map[string]interface{})
	require.Len(t, instances, 1)
	assert.Equal(t, "10.0.0.1:80", instances[0]["address"])
	assert.Equal(t, map[string]string{"k": "v"}, instances[0]["metadata"])
}

func TestRegistryTools_ArgumentErrors(t *testing.T) {
	reg := newRegistryToolRegistry(&fakeRegistryService{})
	ctx := context.Background()
	cases := map[string]map[string]interface{}{
		"quicbaas_registry_register":   {"service": "api", "id": "i"},
		"quicbaas_registry_renew":      {"lease_id": "x"},
		"quicbaas_registry_deregister": {},
		"quicbaas_registry_resolve":    {},
		"quicbaas_registry_watch":      {"service": "api", "timeout_seconds": float64(99)},
	}
	for tool, args := range cases {
		_, err := reg.executeRegistryTool(ctx, tool, args)
		var qb *qberrors.QBError
		require.Error(t, err, tool)
		require.True(t, errors.As(err, &qb), tool)
		assert.Equal(t, qberrors.ErrInvalidInput, qb.Code, tool)
	}
	for _, lease := range []interface{}{"0", "-3", float64(-1)} {
		_, err := reg.executeRegistryTool(ctx, "quicbaas_registry_renew", map[string]interface{}{"lease_id": lease})
		assert.Error(t, err, "lease %v", lease)
	}
	_, err := reg.executeRegistryTool(ctx, "quicbaas_registry_watch", map[string]interface{}{"service": "api", "from_revision": float64(-2)})
	assert.Error(t, err)
	_, err = reg.executeRegistryTool(ctx, "quicbaas_registry_nope", nil)
	assert.Error(t, err)
}

func TestRegistryTools_ServiceErrorsKeepTheirCode(t *testing.T) {
	notFound := &qberrors.QBError{Code: qberrors.ErrNotFound, Op: "x", Err: &registry.LeaseNotFoundError{LeaseID: 3}}
	reg := newRegistryToolRegistry(&fakeRegistryService{err: notFound})
	_, err := reg.executeRegistryTool(context.Background(), "quicbaas_registry_renew", map[string]interface{}{"lease_id": "3"})
	var qb *qberrors.QBError
	require.True(t, errors.As(err, &qb))
	assert.Equal(t, qberrors.ErrNotFound, qb.Code)

	_, err = newRegistryToolRegistry(nil).executeRegistryTool(context.Background(), "quicbaas_registry_resolve", map[string]interface{}{"service": "api"})
	assert.Error(t, err, "no registry configured")
}

// TestRegistryTools_WatchCursorAdvancesOnlyOnDelivery pins the loss-free
// long-poll contract: the returned revision is the last DELIVERED change, and
// a poll that delivers nothing hands back from_revision-1.
func TestRegistryTools_WatchCursorAdvancesOnlyOnDelivery(t *testing.T) {
	events := make(chan service.RegistryEvent, 2)
	events <- service.RegistryEvent{Type: service.RegistryEventPut, Instance: service.RegistryServiceInstance{ID: "a", Service: "api"}, Revision: 10}
	events <- service.RegistryEvent{Type: service.RegistryEventDelete, Instance: service.RegistryServiceInstance{ID: "a", Service: "api"}, Revision: 11}
	fake := &fakeRegistryService{events: events}
	reg := newRegistryToolRegistry(fake)

	out, err := reg.executeRegistryTool(context.Background(), "quicbaas_registry_watch",
		map[string]interface{}{"service": "api", "from_revision": float64(9), "timeout_seconds": float64(1)})
	require.NoError(t, err)
	result := out.(map[string]interface{})
	assert.Equal(t, int64(9), fake.gotWatch.FromRevision)
	assert.Equal(t, int64(11), result["revision"])
	delivered := result["events"].([]map[string]interface{})
	require.Len(t, delivered, 2)
	assert.Equal(t, "PUT", delivered[0]["type"])
	assert.Equal(t, "DELETE", delivered[1]["type"])
	assert.Equal(t, false, result["compacted"])

	idle := newRegistryToolRegistry(&fakeRegistryService{events: make(chan service.RegistryEvent)})
	start := time.Now()
	out, err = idle.executeRegistryTool(context.Background(), "quicbaas_registry_watch",
		map[string]interface{}{"service": "api", "from_revision": float64(40), "timeout_seconds": float64(1)})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond, "an idle poll waits out its timeout")
	assert.Equal(t, int64(39), out.(map[string]interface{})["revision"])
	assert.Empty(t, out.(map[string]interface{})["events"])
}

func TestRegistryTools_WatchReportsCompactionAndEnding(t *testing.T) {
	compacted := &qberrors.QBError{Code: qberrors.ErrOutOfRange, Op: "x", Err: &registry.WatchCompactedError{Service: "api", FromRevision: 2, Err: errors.New("c")}}
	reg := newRegistryToolRegistry(&fakeRegistryService{watchErr: compacted})
	out, err := reg.executeRegistryTool(context.Background(), "quicbaas_registry_watch", map[string]interface{}{"service": "api", "from_revision": float64(2)})
	require.NoError(t, err, "compaction is a result, not a tool failure")
	assert.Equal(t, true, out.(map[string]interface{})["compacted"])

	ended := make(chan service.RegistryEvent, 2)
	ended <- service.RegistryEvent{Type: service.RegistryEventPut, Revision: 5}
	ended <- service.RegistryEvent{Err: &registry.WatchEndedError{Service: "api", Err: errors.New("evicted")}}
	reg = newRegistryToolRegistry(&fakeRegistryService{events: ended})
	out, err = reg.executeRegistryTool(context.Background(), "quicbaas_registry_watch", map[string]interface{}{"service": "api", "from_revision": float64(5)})
	require.NoError(t, err)
	result := out.(map[string]interface{})
	assert.Equal(t, true, result["ended"])
	assert.Equal(t, false, result["compacted"])
	assert.Equal(t, int64(5), result["revision"], "the change delivered before the ending still counts")

	reg = newRegistryToolRegistry(&fakeRegistryService{watchErr: &qberrors.QBError{Code: qberrors.ErrUnavailable, Op: "x", Err: errors.New("down")}})
	_, err = reg.executeRegistryTool(context.Background(), "quicbaas_registry_watch", map[string]interface{}{"service": "api"})
	assert.Error(t, err)
}

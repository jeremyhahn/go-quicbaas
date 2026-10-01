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

package embedded

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry/registrytest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
)

// registryMock embeds the base mock and serves a real RegistryService.
type registryMock struct {
	*mockQuicBaaS
	registry service.RegistryService
}

func (m *registryMock) Registry() service.RegistryService { return m.registry }

// newRegistryHarness wires the real registry client, over the embedded
// transport, over the real service, over an in-memory store.
func newRegistryHarness(t *testing.T) (registryclient.Client, *registrytest.MemoryBackend) {
	t.Helper()
	backend := registrytest.NewMemoryBackend()
	tr, err := New(&registryMock{mockQuicBaaS: &mockQuicBaaS{}, registry: service.NewRegistryServiceFromBackend(backend)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return registryclient.NewClient(tr), backend
}

func nextEvent(t *testing.T, ch <-chan registryclient.Event) registryclient.Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		require.True(t, ok, "watch channel closed early")
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a registry event")
		return registryclient.Event{}
	}
}

func TestEmbeddedRegistry_EndToEnd(t *testing.T) {
	client, backend := newRegistryHarness(t)
	ctx, cancel := context.WithCancel(iam.ContextWithTenant(context.Background(), "acme"))
	defer cancel()

	events, err := client.Watch(ctx, &registryclient.WatchRequest{Service: "api"})
	require.NoError(t, err)

	lease, err := client.Register(ctx, &registryclient.RegisterRequest{
		Instance:   registryclient.ServiceInstance{ID: "node-a", Service: "api", Address: "10.0.4.7:8443", Metadata: map[string]string{"zone": "z1"}},
		TTLSeconds: 15,
	})
	require.NoError(t, err)
	assert.Positive(t, lease.LeaseID)

	put := nextEvent(t, events)
	assert.Equal(t, registryclient.EventPut, put.Type)
	assert.Equal(t, "10.0.4.7:8443", put.Instance.Address)
	assert.Equal(t, "z1", put.Instance.Metadata["zone"])

	resolved, err := client.Resolve(ctx, "api")
	require.NoError(t, err)
	require.Len(t, resolved.Instances, 1)
	assert.Equal(t, backend.Revision(), resolved.Revision)

	renewed, err := client.Renew(ctx, lease.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, int64(15), renewed.TTLSeconds)

	// The host dies: its lease lapses and the watcher sees the instance go.
	require.NoError(t, backend.Expire(lease.LeaseID))
	gone := nextEvent(t, events)
	assert.Equal(t, registryclient.EventDelete, gone.Type)
	assert.Equal(t, "node-a", gone.Instance.ID)
	assert.Greater(t, gone.Revision, put.Revision)
}

// TestEmbeddedRegistry_TenantsAreIsolated proves the tenant the embedder puts
// in the context scopes every verb.
func TestEmbeddedRegistry_TenantsAreIsolated(t *testing.T) {
	client, _ := newRegistryHarness(t)
	acme := iam.ContextWithTenant(context.Background(), "acme")
	globex := iam.ContextWithTenant(context.Background(), "globex")

	lease, err := client.Register(acme, &registryclient.RegisterRequest{
		Instance: registryclient.ServiceInstance{ID: "a", Service: "api", Address: "x:1"}, TTLSeconds: 5,
	})
	require.NoError(t, err)

	other, err := client.Resolve(globex, "api")
	require.NoError(t, err)
	assert.Empty(t, other.Instances, "another tenant sees nothing")
	assert.Error(t, client.Deregister(globex, lease.LeaseID), "another tenant cannot revoke the lease")
	_, err = client.Renew(globex, lease.LeaseID)
	assert.Error(t, err, "another tenant cannot renew the lease")
	require.NoError(t, client.Deregister(acme, lease.LeaseID))
}

func TestEmbeddedRegistry_WatchEndings(t *testing.T) {
	client, backend := newRegistryHarness(t)
	ctx := context.Background()
	lease, err := client.Register(ctx, &registryclient.RegisterRequest{
		Instance: registryclient.ServiceInstance{ID: "a", Service: "api", Address: "x:1"}, TTLSeconds: 5,
	})
	require.NoError(t, err)

	backend.Compact(backend.Revision())
	refused, err := client.Watch(ctx, &registryclient.WatchRequest{Service: "api", FromRevision: 1})
	require.NoError(t, err)
	var compacted *registryclient.WatchCompactedError
	first := nextEvent(t, refused)
	require.True(t, stderrors.As(first.Err, &compacted), "compacted start = %+v", first)
	assert.Equal(t, "api", compacted.Service)

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := client.Watch(watchCtx, &registryclient.WatchRequest{Service: "api"})
	require.NoError(t, err)
	// The watch is open once it has delivered a change.
	second, err := client.Register(ctx, &registryclient.RegisterRequest{
		Instance: registryclient.ServiceInstance{ID: "b", Service: "api", Address: "x:2"}, TTLSeconds: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, "b", nextEvent(t, events).Instance.ID)
	backend.CancelWatches("watcher evicted")
	final := nextEvent(t, events)
	var ended *registryclient.WatchEndedError
	require.True(t, stderrors.As(final.Err, &ended), "final = %+v", final)
	require.NoError(t, client.Deregister(ctx, lease.LeaseID))
	require.NoError(t, client.Deregister(ctx, second.LeaseID))
}

func TestEmbeddedRegistry_HandlerErrors(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	ctx := context.Background()
	for _, method := range []string{"Register", "Renew", "Deregister", "Resolve"} {
		_, err := tr.Request(ctx, registryclient.ServiceName, method, nil)
		assert.Error(t, err, "%s without a registry", method)
	}

	reg, err := New(&registryMock{mockQuicBaaS: &mockQuicBaaS{}, registry: service.NewRegistryServiceFromBackend(registrytest.NewMemoryBackend())})
	require.NoError(t, err)
	for _, method := range []string{"Register", "Renew", "Deregister", "Resolve"} {
		_, err := reg.Request(ctx, registryclient.ServiceName, method, "wrong")
		assert.Error(t, err, "%s with a wrong request type", method)
	}
	stream, err := reg.RequestStream(ctx, registryclient.ServiceName, "Watch", "wrong")
	require.NoError(t, err, "the embedded stream reports handler failures as a message")
	msg, err := stream.Recv()
	if err == nil {
		_, isErr := msg.(error)
		assert.True(t, isErr, "a wrong request type ends the stream with an error message, got %T", msg)
	}
}

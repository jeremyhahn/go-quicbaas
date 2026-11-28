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
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// servicesMock embeds the base mock and serves a real ServicesService.
type servicesMock struct {
	*mockQuicBaaS
	services service.ServicesService
}

func (m *servicesMock) Services() service.ServicesService { return m.services }

// newServicesHarness wires the real services client, over the embedded
// transport, over the real service, over a real plane.
func newServicesHarness(t *testing.T) (servicesclient.Client, *serviceplanetest.Runtime) {
	t.Helper()
	rt := serviceplanetest.NewRuntime(t)
	tr, err := New(&servicesMock{mockQuicBaaS: &mockQuicBaaS{}, services: service.NewServicesService(rt.Resolver(t))})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return servicesclient.NewClient(tr), rt
}

func sdkSigned(t *testing.T, rt *serviceplanetest.Runtime, tenant, name string) *servicesclient.SignedManifest {
	sm := rt.Sign(t, tenant, name)
	return &servicesclient.SignedManifest{Payload: sm.Payload, KeyID: sm.KeyID, Signature: sm.Signature}
}

func TestEmbeddedServices_TheSevenVerbs(t *testing.T) {
	client, rt := newServicesHarness(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := client.Deploy(ctx, sdkSigned(t, rt, "acme", "web"))
	require.NoError(t, err)
	assert.Equal(t, "running", rec.State)
	assert.Equal(t, "rbac:acme/web", rec.Grants["rbac"])
	var m struct{ Name string }
	require.NoError(t, json.Unmarshal(rec.Manifest, &m))
	assert.Equal(t, "web", m.Name)

	for _, step := range []struct {
		verb  func(context.Context, string) (*servicesclient.Record, error)
		state string
	}{{client.Stop, "stopped"}, {client.Start, "running"}, {client.Restart, "running"}, {client.Get, "running"}} {
		rec, err = step.verb(ctx, "web")
		require.NoError(t, err)
		assert.Equal(t, step.state, rec.State)
	}
	rec, err = client.Redeploy(ctx, sdkSigned(t, rt, "acme", "web"))
	require.NoError(t, err)
	assert.EqualValues(t, 2, rec.Revision)
	list, err := client.List(ctx)
	require.NoError(t, err)
	require.Len(t, list.Services, 1)
	require.NoError(t, client.Undeploy(ctx, "web"))
	_, err = client.Get(ctx, "web")
	assert.Error(t, err)
}

func TestEmbeddedServices_ResumeAndTenantScope(t *testing.T) {
	client, rt := newServicesHarness(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	rt.Launcher.Fail(stderrors.New("image pull failed"))
	_, err := client.Deploy(acme, sdkSigned(t, rt, "acme", "web"))
	require.Error(t, err)
	rt.Launcher.Fail(nil)
	rec, err := client.Resume(acme, "web")
	require.NoError(t, err)
	assert.Equal(t, "running", rec.State)

	globex := tenantctx.WithTenant(context.Background(), "globex")
	_, err = client.Get(globex, "web")
	assert.Error(t, err, "another tenant cannot read the service")
	_, err = client.Deploy(globex, sdkSigned(t, rt, "acme", "api"))
	assert.Error(t, err, "a tenant cannot deploy another tenant's manifest")
	_, err = client.Deploy(acme, nil)
	assert.Error(t, err)
}

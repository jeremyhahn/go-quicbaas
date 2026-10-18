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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

var errFederationStore = errors.New("federation store unavailable")

// iamFederationStore stores providers in memory. The embedded
// iam.FederationManager covers the login and linking verbs these handlers
// never call.
type iamFederationStore struct {
	iam.FederationManager
	providers map[string]*iam.ExternalProvider
	err       error
	calls     int
}

func newIAMFederationStore() *iamFederationStore {
	return &iamFederationStore{providers: map[string]*iam.ExternalProvider{}}
}

func (f *iamFederationStore) RegisterProvider(_ context.Context, p *iam.ExternalProvider) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	stored := *p
	f.providers[p.TenantID+"/"+p.ID] = &stored
	return nil
}

func (f *iamFederationStore) GetProvider(_ context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.providers[tenantID+"/"+providerID]
	if !ok {
		return nil, iam.ErrProviderNotFound
	}
	stored := *p
	return &stored, nil
}

func (f *iamFederationStore) UpdateProvider(_ context.Context, p *iam.ExternalProvider) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	stored := *p
	f.providers[p.TenantID+"/"+p.ID] = &stored
	return nil
}

func (f *iamFederationStore) DeleteProvider(_ context.Context, tenantID, providerID string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	delete(f.providers, tenantID+"/"+providerID)
	return nil
}

func (f *iamFederationStore) ListProviders(_ context.Context, tenantID string, _ iam.ListOptions) ([]*iam.ExternalProvider, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []*iam.ExternalProvider
	for _, p := range f.providers {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

// federationMock adds the federation-manager accessor to the base mock.
type federationMock struct {
	*mockQuicBaaS
	mgr iam.FederationManager
}

func (m *federationMock) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return m.mgr, nil
}

func newFederationClient(t *testing.T, mgr iam.FederationManager) (iamsdk.Client, *Transport) {
	t.Helper()
	tr, err := New(&federationMock{mockQuicBaaS: &mockQuicBaaS{}, mgr: mgr})
	require.NoError(t, err)
	return iamsdk.NewClient(tr), tr
}

func tenantA() context.Context {
	return transport.ContextWithTenant(context.Background(), "tenant-a")
}

func TestEmbeddedFederationDispatchRegistered(t *testing.T) {
	for _, method := range []string{
		"CreateProvider", "GetProvider", "UpdateProvider", "DeleteProvider", "ListProviders",
	} {
		_, ok := unaryHandlers["quicbaas.IAM.Federation/"+method]
		assert.True(t, ok, "quicbaas.IAM.Federation/%s is not registered", method)
	}
}

func TestEmbeddedFederationProviderLifecycle(t *testing.T) {
	mgr := newIAMFederationStore()
	client, _ := newFederationClient(t, mgr)
	ctx := tenantA()

	created, err := client.CreateProvider(ctx, &cli.CreateProviderOptions{
		TenantID: "tenant-a", ProviderID: "okta", Name: "Okta", Type: "oidc",
		ClientID: "cid", ClientSecret: "secret", Scopes: []string{"openid"},
	})
	require.NoError(t, err)
	assert.Equal(t, "okta", created.ID)
	assert.Equal(t, "tenant-a", created.TenantID)
	assert.Equal(t, "oidc", created.Type)
	assert.Equal(t, "secret", mgr.providers["tenant-a/okta"].ClientSecret)

	got, err := client.GetProvider(ctx, "tenant-a", "okta")
	require.NoError(t, err)
	assert.Equal(t, "Okta", got.Name)

	name, enabled := "Okta SSO", true
	updated, err := client.UpdateProvider(ctx, &cli.UpdateProviderOptions{
		TenantID: "tenant-a", ProviderID: "okta", Name: &name, Enabled: &enabled,
	})
	require.NoError(t, err)
	assert.Equal(t, "Okta SSO", updated.Name)
	assert.True(t, updated.Enabled)
	assert.Equal(t, "cid", updated.ClientID, "an unset field keeps its stored value")

	list, err := client.ListProviders(ctx, "tenant-a")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "okta", list[0].ID)

	require.NoError(t, client.DeleteProvider(ctx, "tenant-a", "okta"))
	assert.Empty(t, mgr.providers)
}

func TestEmbeddedFederationCrossTenantRefused(t *testing.T) {
	mgr := newIAMFederationStore()
	client, _ := newFederationClient(t, mgr)
	ctx := tenantA()

	calls := map[string]func() error{
		"CreateProvider": func() error {
			_, err := client.CreateProvider(ctx, &cli.CreateProviderOptions{TenantID: "tenant-b", ProviderID: "p"})
			return err
		},
		"GetProvider": func() error {
			_, err := client.GetProvider(ctx, "tenant-b", "p")
			return err
		},
		"UpdateProvider": func() error {
			_, err := client.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "tenant-b", ProviderID: "p"})
			return err
		},
		"DeleteProvider": func() error { return client.DeleteProvider(ctx, "tenant-b", "p") },
		"ListProviders": func() error {
			_, err := client.ListProviders(ctx, "tenant-b")
			return err
		},
	}
	for method, call := range calls {
		t.Run(method, func(t *testing.T) {
			require.Error(t, call())
		})
	}
	assert.Zero(t, mgr.calls, "a refused call must not reach the federation manager")
}

func TestEmbeddedFederationServiceError(t *testing.T) {
	mgr := newIAMFederationStore()
	mgr.err = errFederationStore
	client, _ := newFederationClient(t, mgr)
	ctx := tenantA()

	_, err := client.CreateProvider(ctx, &cli.CreateProviderOptions{TenantID: "tenant-a", ProviderID: "p"})
	require.ErrorIs(t, err, errFederationStore)
	_, err = client.GetProvider(ctx, "tenant-a", "p")
	require.ErrorIs(t, err, errFederationStore)
	_, err = client.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "tenant-a", ProviderID: "p"})
	require.ErrorIs(t, err, errFederationStore)
	require.ErrorIs(t, client.DeleteProvider(ctx, "tenant-a", "p"), errFederationStore)
	_, err = client.ListProviders(ctx, "tenant-a")
	require.ErrorIs(t, err, errFederationStore)
}

func TestEmbeddedFederationNotConfigured(t *testing.T) {
	ctx := tenantA()
	for name, tr := range map[string]*Transport{
		"no accessor": mustNewTransport(t, &mockQuicBaaS{}),
		"nil manager": mustNewTransport(t, &federationMock{mockQuicBaaS: &mockQuicBaaS{}}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := iamsdk.NewClient(tr).ListProviders(ctx, "tenant-a")
			var notConfigured *ServiceNotConfiguredError
			require.ErrorAs(t, err, &notConfigured)
		})
	}
}

func TestEmbeddedFederationInvalidRequest(t *testing.T) {
	_, tr := newFederationClient(t, newIAMFederationStore())
	ctx := tenantA()

	for method, req := range map[string]interface{}{
		"CreateProvider": (*cli.CreateProviderOptions)(nil),
		"UpdateProvider": (*cli.UpdateProviderOptions)(nil),
		"GetProvider":    "not-a-request",
		"DeleteProvider": 42,
		"ListProviders":  map[string]string{"tenant_id": "tenant-a"},
	} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(ctx, "quicbaas.IAM.Federation", method, req)
			var invalid *InvalidRequestTypeError
			require.ErrorAs(t, err, &invalid)
		})
	}
}

func mustNewTransport(t *testing.T, svc service.QuicBaaS) *Transport {
	t.Helper()
	tr, err := New(svc)
	require.NoError(t, err)
	return tr
}

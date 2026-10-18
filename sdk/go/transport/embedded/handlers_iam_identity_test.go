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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// federationManagerDouble records the calls the identity handlers make.
type federationManagerDouble struct {
	iam.FederationManager
	err       error
	links     []*iam.FederatedIdentity
	userID    uint64
	tenantID  string
	provider  string
	token     string
	unlinked  bool
	listCalls int
}

func (f *federationManagerDouble) LinkAccount(_ context.Context, userID uint64, tenantID, providerID, externalToken string) error {
	f.userID, f.tenantID, f.provider, f.token = userID, tenantID, providerID, externalToken
	return f.err
}

func (f *federationManagerDouble) UnlinkAccount(_ context.Context, userID uint64, tenantID, providerID string) error {
	f.userID, f.tenantID, f.provider = userID, tenantID, providerID
	f.unlinked = f.err == nil
	return f.err
}

func (f *federationManagerDouble) GetUserLinks(_ context.Context, userID uint64, tenantID string) ([]*iam.FederatedIdentity, error) {
	f.userID, f.tenantID = userID, tenantID
	f.listCalls++
	return f.links, f.err
}

// federationQuicBaaS serves a federation manager the way quicBaaS does.
type federationQuicBaaS struct {
	service.QuicBaaS
	mgr iam.FederationManager
}

func (q *federationQuicBaaS) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return q.mgr, nil
}

// acmeContext is a caller of tenant acme, the tenant the tests act for.
func acmeContext() context.Context {
	return transport.ContextWithTenant(context.Background(), "acme")
}

func newIdentityClient(t *testing.T, svc service.QuicBaaS) iamsdk.Client {
	t.Helper()
	tr, err := New(svc)
	require.NoError(t, err)
	return iamsdk.NewClient(tr)
}

func TestIAMIdentityLink_ThroughClient(t *testing.T) {
	mgr := &federationManagerDouble{}
	client := newIdentityClient(t, &federationQuicBaaS{mgr: mgr})

	got, err := client.LinkIdentity(acmeContext(), &cli.LinkIdentityOptions{
		TenantID: "acme", UserID: "42", ProviderID: "okta", AuthCode: "code-1",
	})
	require.NoError(t, err)
	assert.Equal(t, &cli.FederatedIdentity{UserID: "42", TenantID: "acme", ProviderID: "okta", Created: got.Created}, got)
	assert.Equal(t, uint64(42), mgr.userID)
	assert.Equal(t, "acme", mgr.tenantID)
	assert.Equal(t, "okta", mgr.provider)
	assert.Equal(t, "code-1", mgr.token)
}

func TestIAMIdentityLink_ServiceError(t *testing.T) {
	want := stderrors.New("link refused")
	client := newIdentityClient(t, &federationQuicBaaS{mgr: &federationManagerDouble{err: want}})

	_, err := client.LinkIdentity(acmeContext(), &cli.LinkIdentityOptions{
		TenantID: "acme", UserID: "42", ProviderID: "okta", AuthCode: "code-1",
	})
	require.ErrorIs(t, err, want)
}

func TestIAMIdentityLink_OtherTenantRefused(t *testing.T) {
	mgr := &federationManagerDouble{}
	client := newIdentityClient(t, &federationQuicBaaS{mgr: mgr})

	_, err := client.LinkIdentity(acmeContext(), &cli.LinkIdentityOptions{
		TenantID: "globex", UserID: "42", ProviderID: "okta", AuthCode: "code-1",
	})
	require.Error(t, err)
	assert.Empty(t, mgr.tenantID, "the manager must not be reached for another tenant")
}

func TestIAMIdentityLink_WrongAndTypedNilRequest(t *testing.T) {
	svc := &federationQuicBaaS{mgr: &federationManagerDouble{}}
	for name, req := range map[string]interface{}{
		"wrong type": struct{}{},
		"typed nil":  (*cli.LinkIdentityOptions)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := handleIAMIdentityLink(context.Background(), svc, req)
			var typeErr *InvalidRequestTypeError
			require.ErrorAs(t, err, &typeErr)
		})
	}
}

func TestIAMIdentityUnlink_ThroughClient(t *testing.T) {
	mgr := &federationManagerDouble{}
	client := newIdentityClient(t, &federationQuicBaaS{mgr: mgr})

	require.NoError(t, client.UnlinkIdentity(acmeContext(), "acme", "42", "okta"))
	assert.True(t, mgr.unlinked)
	assert.Equal(t, uint64(42), mgr.userID)
	assert.Equal(t, "acme", mgr.tenantID)
	assert.Equal(t, "okta", mgr.provider)
}

func TestIAMIdentityUnlink_NotConfigured(t *testing.T) {
	client := newIdentityClient(t, &federationQuicBaaS{})

	err := client.UnlinkIdentity(acmeContext(), "acme", "42", "okta")
	var notConfigured *ServiceNotConfiguredError
	require.ErrorAs(t, err, &notConfigured)
}

func TestIAMIdentityUnlink_InvalidUserID(t *testing.T) {
	mgr := &federationManagerDouble{}
	client := newIdentityClient(t, &federationQuicBaaS{mgr: mgr})

	err := client.UnlinkIdentity(acmeContext(), "acme", "not-a-number", "okta")
	var idErr *InvalidUserIDError
	require.ErrorAs(t, err, &idErr)
	assert.False(t, mgr.unlinked)
}

func TestIAMIdentityListUserIdentities_ThroughClient(t *testing.T) {
	mgr := &federationManagerDouble{links: []*iam.FederatedIdentity{{
		ID: "link-1", UserID: 42, TenantID: "acme", ProviderID: "okta",
		ExternalID: "ext-9", ExternalEmail: "user@example.com",
	}}}
	client := newIdentityClient(t, &federationQuicBaaS{mgr: mgr})

	got, err := client.ListUserIdentities(acmeContext(), "acme", "42")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "link-1", got[0].ID)
	assert.Equal(t, "42", got[0].UserID)
	assert.Equal(t, "okta", got[0].ProviderID)
	assert.Equal(t, "ext-9", got[0].ExternalID)
	assert.Equal(t, "user@example.com", got[0].ExternalEmail)
	assert.Equal(t, "acme", mgr.tenantID)
}

func TestIAMIdentityListUserIdentities_ServiceError(t *testing.T) {
	want := stderrors.New("store down")
	client := newIdentityClient(t, &federationQuicBaaS{mgr: &federationManagerDouble{err: want}})

	_, err := client.ListUserIdentities(acmeContext(), "acme", "42")
	require.ErrorIs(t, err, want)
}

func TestIAMIdentityListUserIdentities_WrongRequest(t *testing.T) {
	mgr := &federationManagerDouble{}
	_, err := handleIAMIdentityListUserIdentities(context.Background(), &federationQuicBaaS{mgr: mgr}, struct{}{})
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Zero(t, mgr.listCalls)
}

func TestIAMIdentityDispatchRegistered(t *testing.T) {
	for _, method := range []string{"LinkIdentity", "UnlinkIdentity", "ListUserIdentities"} {
		_, ok := unaryHandlers[iamIdentityServiceName+"/"+method]
		assert.True(t, ok, method)
	}
}

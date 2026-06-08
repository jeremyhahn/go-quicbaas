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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth/qrdbauthz"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeRBACClient permits a single (tenant, resource, action) tuple and denies
// the rest, recording the identity it was asked about. It lets the bridge tests
// exercise the qrdb PDP seam without a real authorizer.
type fakeRBACClient struct {
	allowTenant   string
	allowResource string
	allowAction   string
	lastIdentity  *qrdbtransport.Identity
}

func (c *fakeRBACClient) Authorize(_ context.Context, id *qrdbtransport.Identity, resource, action string) error {
	c.lastIdentity = id
	if id.TenantID == c.allowTenant && resource == c.allowResource && action == c.allowAction {
		return nil
	}
	return errors.New("permission denied")
}

func (c *fakeRBACClient) ListPermissions(context.Context, *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	return nil, nil
}

var _ qrdbtransport.RBACClient = (*fakeRBACClient)(nil)

func newBridge(t *testing.T, rbac qrdbtransport.RBACClient) *qrdbAuthzService {
	t.Helper()
	adapter, err := qrdbauthz.New(func() qrdbtransport.RBACClient { return rbac }, nil)
	require.NoError(t, err)
	return newQRDBAuthzService(adapter)
}

func TestQRDBAuthzService_Allow(t *testing.T) {
	rbac := &fakeRBACClient{allowTenant: "system", allowResource: "kv", allowAction: "put"}
	bridge := newBridge(t, rbac)

	dec, err := bridge.Authorize(context.Background(), &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "u1", TenantID: "system", Roles: []string{"admin"}},
		Resource: "kv",
		Action:   "put",
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "qrdb", dec.Provider)
}

func TestQRDBAuthzService_Deny(t *testing.T) {
	rbac := &fakeRBACClient{allowTenant: "system", allowResource: "kv", allowAction: "get"}
	bridge := newBridge(t, rbac)

	dec, err := bridge.Authorize(context.Background(), &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "u1", TenantID: "system", Roles: []string{"reader"}},
		Resource: "kv",
		Action:   "put", // not permitted
	})
	require.NoError(t, err)
	assert.False(t, dec.Allowed)
	assert.NotEmpty(t, dec.Reason)
}

func TestQRDBAuthzService_TenantFallbackFromContext(t *testing.T) {
	rbac := &fakeRBACClient{allowTenant: "acme", allowResource: "kv", allowAction: "put"}
	bridge := newBridge(t, rbac)

	// Subject carries no tenant; the bridge must fall back to the context tenant.
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	dec, err := bridge.Authorize(ctx, &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "u1", TenantID: "", Roles: []string{"admin"}},
		Resource: "kv",
		Action:   "put",
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	require.NotNil(t, rbac.lastIdentity)
	assert.Equal(t, "acme", rbac.lastIdentity.TenantID)
}

func TestQRDBAuthzService_BatchAuthorize(t *testing.T) {
	rbac := &fakeRBACClient{allowTenant: "system", allowResource: "kv", allowAction: "get"}
	bridge := newBridge(t, rbac)

	reqs := []*interfaces.AuthorizationRequest{
		{Subject: interfaces.Subject{ID: "u1", TenantID: "system"}, Resource: "kv", Action: "get"},
		{Subject: interfaces.Subject{ID: "u1", TenantID: "system"}, Resource: "kv", Action: "put"},
	}
	decs, err := bridge.BatchAuthorize(context.Background(), reqs)
	require.NoError(t, err)
	require.Len(t, decs, 2)
	assert.True(t, decs[0].Allowed)
	assert.False(t, decs[1].Allowed)
}

func TestQRDBAuthzService_Accessors(t *testing.T) {
	bridge := newBridge(t, &fakeRBACClient{})
	assert.Nil(t, bridge.GetProvider())
	assert.Equal(t, "qrdb", bridge.GetProviderType())
	assert.NoError(t, bridge.Close())
}

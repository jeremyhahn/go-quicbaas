//go:build !integration

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
)

// nodeRolesDouble stands in for the embedded node's role service.
type nodeRolesDouble struct{}

func (nodeRolesDouble) CreateRole(context.Context, *qrdbsdk.CreateRoleRequest) (*qrdbsdk.Role, error) {
	return &qrdbsdk.Role{}, nil
}

func (nodeRolesDouble) GetRoleByName(context.Context, string, string) (*qrdbsdk.Role, error) {
	return &qrdbsdk.Role{}, nil
}

// With an embedded node, first-boot seeds roles through the node's own role
// service, never through the authenticated client the IdentityStore uses.
func TestFirstBootRoles_PrefersTheNodeRoleService(t *testing.T) {
	node := &nodeRolesDouble{}

	got := firstBootRoles(node, buildTestIAMComponents(t))

	assert.Same(t, node, got)
}

// Without an embedded node there is no role service to bind to, so the
// IdentityStore adapter carries the role calls.
func TestFirstBootRoles_FallsBackToTheIdentityStoreAdapter(t *testing.T) {
	got := firstBootRoles(nil, buildTestIAMComponents(t))

	assert.IsType(t, &qrdbadapter.RoleAdapter{}, got)
}

func TestNodeRoleService_NoNodeIsNil(t *testing.T) {
	srv := withTempServer(t)
	srv.setQRDBNode(nil)

	assert.Nil(t, srv.nodeRoleService())
}

// One role surface answers both the create and the name lookup, so a re-run
// resolves the roles where the first run created them.
func TestFirstBootDependencies_RolesAndResolverAreOneSurface(t *testing.T) {
	srv := withTempServer(t)
	srv.setQRDBNode(nil)

	deps, _, err := srv.firstBootDependencies(buildTestIAMComponents(t))

	require.NoError(t, err)
	require.NotNil(t, deps.Roles)
	assert.Same(t, deps.Roles, deps.RoleResolver)
}

// Without an embedded node there is no principal to bind, so the caller's
// context passes through unchanged.
func TestServiceContext_NoNodeKeepsTheContext(t *testing.T) {
	srv := withTempServer(t)
	srv.setQRDBNode(nil)
	ctx := context.WithValue(context.Background(), nodeRolesDouble{}, "marker")

	got, err := srv.ServiceContext(ctx)

	require.NoError(t, err)
	assert.Equal(t, ctx, got)
}

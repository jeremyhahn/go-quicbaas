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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// iamServiceWrapper backend-resolution error branches.
//
// Each IAM method first resolves its backing manager via the provider and
// short-circuits when the provider errors or returns a nil manager. The
// existing method tests cover only the happy path, leaving both guard branches
// uncovered. These tests pin the two failure modes — a propagated provider
// error and the "<manager> not available" guard — for every method whose
// guards were previously untested.
// =============================================================================

// invokeWrapper drives a single wrapper method with the supplied provider and
// returns the resulting error. The request is irrelevant because the guard
// fires before any request field is read.
type iamMethodCase struct {
	name   string
	invoke func(ctx context.Context, w *iamServiceWrapper) (any, error)
	// unavailableMsg is the substring expected when the manager is nil.
	unavailableMsg string
	// providerErr installs the corresponding *Err field on the provider.
	withProviderErr func(p *mockIAMServiceProvider, err error)
}

func TestIAMServiceWrapper_BackendResolutionErrors(t *testing.T) {
	cases := []iamMethodCase{
		{
			name: "ListUsers",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ListUsers(ctx, &IAMListUsersRequest{TenantID: "t1"})
			},
			unavailableMsg:  "identity store not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.identityStoreErr = err },
		},
		{
			name: "CreateGroup",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.CreateGroup(ctx, &CreateGroupRequest{TenantID: "t1", Name: "g"})
			},
			unavailableMsg:  "identity store not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.identityStoreErr = err },
		},
		{
			name: "ListGroups",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ListGroups(ctx, &ListGroupsRequest{TenantID: "t1"})
			},
			unavailableMsg:  "identity store not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.identityStoreErr = err },
		},
		{
			name: "CreateRole",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.CreateRole(ctx, &IAMCreateRoleRequest{TenantID: "t1", Name: "r"})
			},
			unavailableMsg:  "identity store not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.identityStoreErr = err },
		},
		{
			name: "ListRoles",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ListRoles(ctx, &IAMListRolesRequest{TenantID: "t1"})
			},
			unavailableMsg:  "identity store not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.identityStoreErr = err },
		},
		{
			name: "RegisterApp",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.RegisterApp(ctx, &RegisterAppRequest{TenantID: "t1", Name: "app"})
			},
			unavailableMsg:  "app manager not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.appMgrErr = err },
		},
		{
			name: "ListApps",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ListApps(ctx, &ListAppsRequest{TenantID: "t1"})
			},
			unavailableMsg:  "app manager not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.appMgrErr = err },
		},
		{
			name: "ListPolicies",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ListPolicies(ctx, &ListPoliciesRequest{TenantID: "t1"})
			},
			unavailableMsg:  "policy engine not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.policyEngineErr = err },
		},
		{
			name: "ValidatePolicy",
			invoke: func(ctx context.Context, w *iamServiceWrapper) (any, error) {
				return w.ValidatePolicy(ctx, &ValidatePolicyRequest{})
			},
			unavailableMsg:  "policy engine not available",
			withProviderErr: func(p *mockIAMServiceProvider, err error) { p.policyEngineErr = err },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_ManagerUnavailable", func(t *testing.T) {
			// Provider returns a nil manager and no error: the wrapper must
			// surface the typed "not available" guard.
			provider := &mockIAMServiceProvider{}
			wrapper := &iamServiceWrapper{provider: provider}

			resp, err := tc.invoke(context.Background(), wrapper)
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tc.unavailableMsg)
		})

		t.Run(tc.name+"_ProviderError", func(t *testing.T) {
			// Provider returns an error while resolving the manager: the
			// wrapper must propagate it verbatim.
			backendErr := &testIAMError{msg: "backend resolution failed"}
			provider := &mockIAMServiceProvider{}
			tc.withProviderErr(provider, backendErr)
			wrapper := &iamServiceWrapper{provider: provider}

			resp, err := tc.invoke(context.Background(), wrapper)
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), "backend resolution failed")
		})
	}
}

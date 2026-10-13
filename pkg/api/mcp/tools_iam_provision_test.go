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
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestExecuteIAMTenantProvision_CarriesEveryArgument(t *testing.T) {
	var got *service.IAMProvisionTenantRequest
	svc := &mockIAMService{provisionFunc: func(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
		got = req
		return &service.IAMProvisionTenantResponse{TenantID: req.TenantID, AdminPending: true, InviteToken: "tok"}, nil
	}}
	reg := newIAMTestRegistry(svc)

	result, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_tenant_provision", map[string]interface{}{
		"tenant_id":           "acme",
		"name":                "Acme",
		"isolation_type":      "prefix",
		"admin_username":      "root",
		"admin_enrollment":    "invite",
		"admin_csr_pem":       "pem",
		"invite_expiry_hours": float64(12), // JSON numbers decode as float64
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, got)
	assert.Equal(t, &service.IAMProvisionTenantRequest{
		TenantID:          "acme",
		Name:              "Acme",
		IsolationType:     qrdbsdk.TenantIsolationType("prefix"),
		AdminUsername:     "root",
		AdminEnrollment:   service.IAMTenantAdminEnrollInvite,
		AdminCSRPEM:       "pem",
		InviteExpiryHours: 12,
	}, got)
}

func TestExecuteIAMTenantProvision_Errors(t *testing.T) {
	ctx := context.Background()
	args := map[string]interface{}{"tenant_id": "acme"}

	t.Run("service error is returned", func(t *testing.T) {
		want := stderrors.New("provision failed")
		reg := newIAMTestRegistry(&mockIAMService{provisionFunc: func(context.Context, *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
			return nil, want
		}})
		_, err := reg.executeIAMTool(ctx, "quicbaas_iam_tenant_provision", args)
		assert.ErrorIs(t, err, want)
	})

	t.Run("no IAM service", func(t *testing.T) {
		_, err := newIAMTestRegistry(nil).executeIAMTool(ctx, "quicbaas_iam_tenant_provision", args)
		assert.Error(t, err)
	})
}

func TestTenantProvisionToolRequiresTenantCreate(t *testing.T) {
	perm, ok := qrdbToolExactMapping["quicbaas_iam_tenant_provision"]
	require.True(t, ok, "an unmapped tool is denied by the MCP authorizer")
	assert.Equal(t, qrdbToolResource{"tenants", "create"}, perm, "must match REST and gRPC")
}

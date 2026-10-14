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

package grpc

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newProvisionIAMServer(t *testing.T, svc IAMService) *IAMServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewIAMServer(svc, log)
}

func TestIAMServer_ProvisionTenant_InviteMode(t *testing.T) {
	svc := &mockIAMService{}
	expires := time.Now().Add(time.Hour)
	svc.On("ProvisionTenant", mock.Anything, mock.MatchedBy(func(r *service.IAMProvisionTenantRequest) bool {
		return r.TenantID == "acme" && r.AdminEnrollment == service.IAMTenantAdminEnrollInvite && r.InviteExpiryHours == 12
	})).Return(&service.IAMProvisionTenantResponse{
		TenantID: "acme", AdminUserID: 7, AdminPending: true,
		InviteToken: "tok", InviteExpiresAt: &expires, Timestamp: time.Now(),
	}, nil)
	srv := newProvisionIAMServer(t, svc)

	resp, err := srv.ProvisionTenant(context.Background(), &iampb.ProvisionTenantRequest{
		TenantId: "acme", AdminEnrollment: "invite", InviteExpiryHours: 12,
	})
	require.NoError(t, err)
	assert.Equal(t, "tok", resp.InviteToken)
	assert.Equal(t, "7", resp.AdminUserId)
	assert.True(t, resp.AdminPending)
	require.NotNil(t, resp.InviteExpiresAt)
	assert.True(t, resp.InviteExpiresAt.AsTime().Equal(expires))
	svc.AssertExpectations(t)
}

func TestIAMServer_ProvisionTenant_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("missing tenant id never reaches the service", func(t *testing.T) {
		svc := &mockIAMService{}
		_, err := newProvisionIAMServer(t, svc).ProvisionTenant(ctx, &iampb.ProvisionTenantRequest{})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		svc.AssertNotCalled(t, "ProvisionTenant", mock.Anything, mock.Anything)
	})

	t.Run("service errors keep their class", func(t *testing.T) {
		svc := &mockIAMService{}
		svc.On("ProvisionTenant", mock.Anything, mock.Anything).
			Return(nil, errors.New(errors.ErrInvalidInput, "iam.ProvisionTenant", "admin CSR required"))
		_, err := newProvisionIAMServer(t, svc).ProvisionTenant(ctx, &iampb.ProvisionTenantRequest{TenantId: "acme"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

type recordingTenantProvisioner struct {
	got *service.IAMProvisionTenantRequest
	err error
}

func (p *recordingTenantProvisioner) Provision(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	p.got = req
	if p.err != nil {
		return nil, p.err
	}
	return &service.IAMProvisionTenantResponse{TenantID: req.TenantID}, nil
}

func TestIAMServiceAdapter_ProvisionTenant(t *testing.T) {
	ctx := context.Background()
	req := &service.IAMProvisionTenantRequest{TenantID: "acme"}

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	_, err := adapter.ProvisionTenant(ctx, req)
	require.Error(t, err, "no provisioner wired")

	prov := &recordingTenantProvisioner{}
	adapter.SetTenantProvisioner(prov)
	resp, err := adapter.ProvisionTenant(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "acme", resp.TenantID)
	assert.Same(t, req, prov.got)

	want := stderrors.New("provision failed")
	adapter.SetTenantProvisioner(&recordingTenantProvisioner{err: want})
	_, err = adapter.ProvisionTenant(ctx, req)
	assert.ErrorIs(t, err, want)
}

func TestAuthzMapping_ProvisionTenantRequiresTenantCreate(t *testing.T) {
	perm, ok := grpcMethodPermissions["/quicbaas.iam.v1.IAMService/ProvisionTenant"]
	require.True(t, ok, "ProvisionTenant must be mapped or RequireAuthz denies it")
	assert.Equal(t, "tenants", perm.resource)
	assert.Equal(t, "create", perm.action)
}

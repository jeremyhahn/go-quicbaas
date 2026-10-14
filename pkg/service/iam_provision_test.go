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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

type recordingProvisioner struct {
	got  *IAMProvisionTenantRequest
	resp *IAMProvisionTenantResponse
	err  error
}

func (p *recordingProvisioner) Provision(_ context.Context, req *IAMProvisionTenantRequest) (*IAMProvisionTenantResponse, error) {
	p.got = req
	return p.resp, p.err
}

func TestIAMProvisionTenantRequest_ValidateAcceptsBothModes(t *testing.T) {
	cert := &IAMProvisionTenantRequest{TenantID: "acme", IsolationType: qrdbsdk.TenantIsolationPrefix, AdminCSRPEM: testCSR}
	require.NoError(t, cert.Validate())
	assert.Equal(t, IAMTenantAdminEnrollCert, cert.Enrollment(), "an empty mode defaults to cert")

	invite := &IAMProvisionTenantRequest{TenantID: "acme", IsolationType: qrdbsdk.TenantIsolationPrefix, AdminEnrollment: IAMTenantAdminEnrollInvite, InviteExpiryHours: 48}
	require.NoError(t, invite.Validate())
	assert.Equal(t, IAMTenantAdminEnrollInvite, invite.Enrollment())
}

func TestIAMProvisionTenantRequest_ValidateRefusesBadRequests(t *testing.T) {
	tests := []struct {
		name string
		req  IAMProvisionTenantRequest
		want error
	}{
		{
			name: "missing tenant id would mean the system tenant",
			req:  IAMProvisionTenantRequest{AdminCSRPEM: testCSR},
			want: &ProvisionTenantIDRequiredError{},
		},
		{
			name: "cert mode without a CSR",
			req:  IAMProvisionTenantRequest{TenantID: "acme", AdminEnrollment: IAMTenantAdminEnrollCert},
			want: &ProvisionAdminCSRRequiredError{},
		},
		{
			name: "invite mode with a CSR",
			req:  IAMProvisionTenantRequest{TenantID: "acme", AdminEnrollment: IAMTenantAdminEnrollInvite, AdminCSRPEM: testCSR},
			want: &ProvisionConflictingEnrollmentError{},
		},
		{
			name: "invite mode for the system tenant",
			req:  IAMProvisionTenantRequest{TenantID: qrdbsdk.SystemTenantID, AdminEnrollment: IAMTenantAdminEnrollInvite},
			want: &ProvisionSystemTenantInviteError{},
		},
		{
			name: "unknown mode",
			req:  IAMProvisionTenantRequest{TenantID: "acme", AdminEnrollment: "password"},
			want: &ProvisionEnrollmentModeError{Mode: "password"},
		},
		{
			name: "negative invite lifetime",
			req:  IAMProvisionTenantRequest{TenantID: "acme", AdminEnrollment: IAMTenantAdminEnrollInvite, InviteExpiryHours: -1},
			want: &ProvisionInviteExpiryError{Hours: -1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			require.Error(t, err)
			assert.Equal(t, tt.want, err)
			assert.NotEmpty(t, err.Error())
		})
	}
}

func TestIAMServiceWrapper_ProvisionTenant_Delegates(t *testing.T) {
	want := &IAMProvisionTenantResponse{TenantID: "acme", AdminUserID: 42, AdminPending: true, InviteToken: "tok"}
	prov := &recordingProvisioner{resp: want}
	w := &iamServiceWrapper{provider: &mockIAMServiceProvider{provisioner: prov}}

	req := &IAMProvisionTenantRequest{TenantID: "acme", IsolationType: qrdbsdk.TenantIsolationPrefix, AdminEnrollment: IAMTenantAdminEnrollInvite}
	got, err := w.ProvisionTenant(context.Background(), req)
	require.NoError(t, err)
	assert.Same(t, want, got)
	assert.Same(t, req, prov.got)
}

func TestIAMServiceWrapper_ProvisionTenant_RefusesBeforeProvisioning(t *testing.T) {
	prov := &recordingProvisioner{}
	w := &iamServiceWrapper{provider: &mockIAMServiceProvider{provisioner: prov}}
	ctx := context.Background()

	_, err := w.ProvisionTenant(ctx, nil)
	assert.True(t, errors.IsInvalidInput(err), "nil request: %v", err)

	_, err = w.ProvisionTenant(ctx, &IAMProvisionTenantRequest{TenantID: "acme"})
	assert.True(t, errors.IsInvalidInput(err), "validation failures are input errors: %v", err)
	var csrErr *ProvisionAdminCSRRequiredError
	assert.True(t, stderrors.As(err, &csrErr), "the typed cause stays in the chain")

	assert.Nil(t, prov.got, "an invalid request never reaches the provisioner")
}

func TestIAMServiceWrapper_ProvisionTenant_ProvisionerMissingOrFailing(t *testing.T) {
	ctx := context.Background()
	req := &IAMProvisionTenantRequest{TenantID: "acme", IsolationType: qrdbsdk.TenantIsolationPrefix, AdminCSRPEM: testCSR}

	w := &iamServiceWrapper{provider: &mockIAMServiceProvider{}}
	_, err := w.ProvisionTenant(ctx, req)
	assert.True(t, errors.Is(err, errors.ErrUnavailable), "no provisioner wired: %v", err)

	lookupErr := stderrors.New("provider down")
	w = &iamServiceWrapper{provider: &mockIAMServiceProvider{provisionerErr: lookupErr}}
	_, err = w.ProvisionTenant(ctx, req)
	assert.ErrorIs(t, err, lookupErr)

	provErr := stderrors.New("provision failed")
	w = &iamServiceWrapper{provider: &mockIAMServiceProvider{provisioner: &recordingProvisioner{err: provErr}}}
	_, err = w.ProvisionTenant(ctx, req)
	assert.ErrorIs(t, err, provErr)
}

func TestIAMServiceAdapter_GetTenantProvisioner(t *testing.T) {
	ctx := context.Background()

	got, err := (&iamServiceAdapter{}).GetTenantProvisioner(ctx)
	require.NoError(t, err)
	assert.Nil(t, got, "no components means no provisioner")

	prov := &recordingProvisioner{}
	got, err = (&iamServiceAdapter{components: &mockIAMComponents{provisioner: prov}}).GetTenantProvisioner(ctx)
	require.NoError(t, err)
	assert.Same(t, prov, got)
}

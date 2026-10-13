//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// provisionIAMService records the provisioning request it is handed.
type provisionIAMService struct {
	service.IAMService
	got  *service.IAMProvisionTenantRequest
	resp *service.IAMProvisionTenantResponse
	err  error
}

func (s *provisionIAMService) ProvisionTenant(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	s.got = req
	return s.resp, s.err
}

type provisionQuicBaaS struct {
	service.QuicBaaS
	iam service.IAMService
}

func (q *provisionQuicBaaS) IAM() service.IAMService { return q.iam }

func provisionRequest(t *testing.T, iamSvc service.IAMService, tenantID, body string) *httptest.ResponseRecorder {
	t.Helper()
	h, cleanup := setupDeepHandler(t, &provisionQuicBaaS{QuicBaaS: testutil.NewMockQuicBaaS(), iam: iamSvc})
	t.Cleanup(cleanup)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+tenantID+"/provision", strings.NewReader(body))
	r = withChiParams(r, map[string]string{"tenant_id": tenantID})
	w := httptest.NewRecorder()
	h.ProvisionTenant(w, r)
	return w
}

func TestProvisionTenant_InviteModeReturnsTheToken(t *testing.T) {
	svc := &provisionIAMService{resp: &service.IAMProvisionTenantResponse{
		TenantID: "acme", AdminUserID: "7", AdminRoleID: "role-1", AdminPending: true, InviteToken: "tok",
	}}

	w := provisionRequest(t, svc, "acme", `{"name":"Acme","admin_enrollment":"invite","invite_expiry_hours":12}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	require.NotNil(t, svc.got)
	assert.Equal(t, "acme", svc.got.TenantID, "the path names the tenant")
	assert.Equal(t, service.IAMTenantAdminEnrollInvite, svc.got.AdminEnrollment)
	assert.Equal(t, 12, svc.got.InviteExpiryHours)

	var resp service.IAMProvisionTenantResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "tok", resp.InviteToken)
	assert.Equal(t, "7", resp.AdminUserID)
	assert.True(t, resp.AdminPending)
}

// An invite-mode request that omits invite_expiry_hours reaches the service
// as zero, which the provisioner turns into the default lifetime, and the
// response carries the token and that expiry back to the caller.
func TestProvisionTenant_InviteModeWithoutExpiryReturnsTheDefault(t *testing.T) {
	expiresAt := time.Now().Add(iam.DefaultInviteTTL).UTC()
	svc := &provisionIAMService{resp: &service.IAMProvisionTenantResponse{
		TenantID: "acme", AdminUserID: "7", AdminRoleID: "role-1", AdminPending: true,
		InviteToken: "tok", InviteExpiresAt: &expiresAt,
	}}

	w := provisionRequest(t, svc, "acme", `{"admin_enrollment":"invite"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	require.NotNil(t, svc.got)
	assert.Zero(t, svc.got.InviteExpiryHours, "an omitted expiry is left to the provisioner's default")

	var resp service.IAMProvisionTenantResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "tok", resp.InviteToken)
	require.NotNil(t, resp.InviteExpiresAt)
	assert.WithinDuration(t, expiresAt, *resp.InviteExpiresAt, time.Second)
}

func TestProvisionTenant_RefusesMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: `{`},
		{name: "unknown field", body: `{"admin_key_pem":"x"}`},
		{name: "body names another tenant", body: `{"tenant_id":"other"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &provisionIAMService{}
			w := provisionRequest(t, svc, "acme", tt.body)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Nil(t, svc.got, "a malformed request never reaches the service")
		})
	}
}

func TestProvisionTenant_MapsServiceErrors(t *testing.T) {
	invalid := dberrors.Wrap(&service.ProvisionAdminCSRRequiredError{}, dberrors.ErrInvalidInput, "iam.ProvisionTenant")
	w := provisionRequest(t, &provisionIAMService{err: invalid}, "acme", `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	exists := dberrors.New(dberrors.ErrAlreadyExists, "iam.ProvisionTenant", "tenant exists")
	w = provisionRequest(t, &provisionIAMService{err: exists}, "acme", `{}`)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestProvisionTenant_IAMServiceUnavailable(t *testing.T) {
	w := provisionRequest(t, nil, "acme", `{}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

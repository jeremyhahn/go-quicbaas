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
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var provisionInviteExpiry = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// ProvisionTenant echoes the request into the response so a test can see
// every field cross the wire.
func (f *fakeIAMServer) ProvisionTenant(_ context.Context, req *iampb.ProvisionTenantRequest) (*iampb.ProvisionTenantResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	if req.AdminEnrollment == "cert" {
		return &iampb.ProvisionTenantResponse{
			TenantId:            req.TenantId,
			AdminUserId:         "7",
			AdminUsername:       req.AdminUsername,
			AdminRoleId:         "tenant-admin",
			AdminCertPem:        "issued for " + req.AdminCsrPem,
			AdminCertThumbprint: "spki-sha256",
			Timestamp:           timestamppb.New(provisionInviteExpiry),
		}, nil
	}
	return &iampb.ProvisionTenantResponse{
		TenantId:        req.TenantId,
		AdminUserId:     "7",
		AdminUsername:   req.AdminUsername,
		AdminPending:    req.AdminEnrollment == "invite",
		InviteToken:     req.Name + "|" + req.IsolationType + "|" + req.AdminCsrPem,
		InviteExpiresAt: timestamppb.New(provisionInviteExpiry.Add(time.Duration(req.InviteExpiryHours) * time.Hour)),
		Timestamp:       timestamppb.New(provisionInviteExpiry),
	}, nil
}

func TestIAMHandler_ProvisionTenant_CarriesEveryField(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)

	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.Tenant", "Provision", &iam.ProvisionTenantRequest{
		TenantID:          "acme",
		Name:              "Acme",
		IsolationType:     "prefix",
		AdminUsername:     "root",
		AdminEnrollment:   iam.TenantAdminEnrollInvite,
		InviteExpiryHours: 2,
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	resp, ok := out.(*iam.ProvisionTenantResponse)
	if !ok {
		t.Fatalf("expected *iam.ProvisionTenantResponse, got %T", out)
	}
	if resp.TenantID != "acme" || resp.AdminUserID != "7" || resp.AdminUsername != "root" || !resp.AdminPending {
		t.Errorf("identity fields not carried: %+v", resp)
	}
	if resp.InviteToken != "Acme|prefix|" {
		t.Errorf("name/isolation/csr not carried, got %q", resp.InviteToken)
	}
	if resp.InviteExpiresAt == nil || !resp.InviteExpiresAt.Equal(provisionInviteExpiry.Add(2*time.Hour)) {
		t.Errorf("invite expiry hours not carried, got %v", resp.InviteExpiresAt)
	}
	if !resp.Timestamp.Equal(provisionInviteExpiry) {
		t.Errorf("timestamp not carried, got %v", resp.Timestamp)
	}
}

// TestIAMHandler_ProvisionTenant_CertMode proves the cert enrollment path:
// the admin CSR reaches the server, and the issued certificate and its bound
// thumbprint come back with no pending admin and no invite.
func TestIAMHandler_ProvisionTenant_CertMode(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)

	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.Tenant", "Provision", &iam.ProvisionTenantRequest{
		TenantID:        "acme",
		AdminUsername:   "root",
		AdminEnrollment: iam.TenantAdminEnrollCert,
		AdminCSRPEM:     "csr-pem",
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	resp, ok := out.(*iam.ProvisionTenantResponse)
	if !ok {
		t.Fatalf("expected *iam.ProvisionTenantResponse, got %T", out)
	}
	if resp.AdminPending || resp.InviteToken != "" || resp.InviteExpiresAt != nil {
		t.Errorf("cert mode returned an invite: %+v", resp)
	}
	if resp.AdminCertPEM != "issued for csr-pem" {
		t.Errorf("admin CSR or issued certificate not carried, got %q", resp.AdminCertPEM)
	}
	if resp.AdminCertThumbprint != "spki-sha256" || resp.AdminRoleID != "tenant-admin" {
		t.Errorf("cert binding fields not carried: %+v", resp)
	}
}

func TestIAMHandler_ProvisionTenant_RequiresTenantID(t *testing.T) {
	h := newIAMHandlerWithServer(t, false)

	_, err := h.handleRequest(context.Background(), "quicbaas.IAM.Tenant", "Provision",
		map[string]interface{}{"admin_enrollment": "invite"})
	if err == nil {
		t.Fatal("expected an error for a request without tenant_id")
	}
}

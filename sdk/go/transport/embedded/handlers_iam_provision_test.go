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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// provisioningIAMService records the provisioning request it is handed.
type provisioningIAMService struct {
	service.IAMService
	got  *service.IAMProvisionTenantRequest
	resp *service.IAMProvisionTenantResponse
	err  error
}

func (s *provisioningIAMService) ProvisionTenant(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	s.got = req
	return s.resp, s.err
}

type provisioningQuicBaaS struct {
	service.QuicBaaS
	iam service.IAMService
}

func (q *provisioningQuicBaaS) IAM() service.IAMService { return q.iam }

func TestHandleIAMTenantProvision_InviteMode(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	iamSvc := &provisioningIAMService{resp: &service.IAMProvisionTenantResponse{
		TenantID: "acme", AdminUserID: "7", AdminPending: true,
		InviteToken: "tok", InviteExpiresAt: &expires,
	}}
	svc := &provisioningQuicBaaS{iam: iamSvc}

	out, err := handleIAMTenantProvision(context.Background(), svc, &iamsdk.ProvisionTenantRequest{
		TenantID:          "acme",
		AdminEnrollment:   iamsdk.TenantAdminEnrollInvite,
		InviteExpiryHours: 12,
	})
	if err != nil {
		t.Fatalf("handleIAMTenantProvision failed: %v", err)
	}

	if iamSvc.got == nil || iamSvc.got.TenantID != "acme" ||
		iamSvc.got.AdminEnrollment != service.IAMTenantAdminEnrollInvite || iamSvc.got.InviteExpiryHours != 12 {
		t.Fatalf("service received %+v, want the SDK request carried through", iamSvc.got)
	}
	resp, ok := out.(*iamsdk.ProvisionTenantResponse)
	if !ok {
		t.Fatalf("expected *iamsdk.ProvisionTenantResponse, got %T", out)
	}
	if resp.InviteToken != "tok" || resp.AdminUserID != "7" || !resp.AdminPending || resp.InviteExpiresAt != &expires {
		t.Errorf("response %+v does not carry the service result", resp)
	}
}

func TestHandleIAMTenantProvision_Refusals(t *testing.T) {
	ctx := context.Background()
	req := &iamsdk.ProvisionTenantRequest{TenantID: "acme"}

	t.Run("no IAM service", func(t *testing.T) {
		if _, err := handleIAMTenantProvision(ctx, &mockQuicBaaSForIAM{}, req); err == nil {
			t.Fatal("expected an error when the server exposes no IAM service")
		}
		if _, err := handleIAMTenantProvision(ctx, &provisioningQuicBaaS{}, req); err == nil {
			t.Fatal("expected an error for a nil IAM service")
		}
	})

	t.Run("wrong request type", func(t *testing.T) {
		iamSvc := &provisioningIAMService{}
		svc := &provisioningQuicBaaS{iam: iamSvc}
		if _, err := handleIAMTenantProvision(ctx, svc, &iamsdk.CreateTenantRequest{ID: "acme"}); err == nil {
			t.Fatal("expected an error for a non-provision request")
		}
		if iamSvc.got != nil {
			t.Error("a mistyped request must not reach the service")
		}
	})

	t.Run("service failure", func(t *testing.T) {
		want := errors.New("provision failed")
		svc := &provisioningQuicBaaS{iam: &provisioningIAMService{err: want}}
		if _, err := handleIAMTenantProvision(ctx, svc, req); !errors.Is(err, want) {
			t.Fatalf("expected the service error, got %v", err)
		}
	})
}

func TestIAMTenantProvisionDispatchRegistered(t *testing.T) {
	if _, ok := iamUnaryHandlers["quicbaas.IAM.Tenant/Provision"]; !ok {
		t.Fatal("expected quicbaas.IAM.Tenant/Provision in the IAM dispatch table")
	}
	if _, ok := unaryHandlers["quicbaas.IAM.Tenant/Provision"]; !ok {
		t.Fatal("expected quicbaas.IAM.Tenant/Provision in the global dispatch table")
	}
}

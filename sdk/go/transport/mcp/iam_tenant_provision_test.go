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
	"reflect"
	"strings"
	"testing"
	"time"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const tenantProvisionTool = "quicbaas_iam_tenant_provision"

// TestTenantProvisionEndToEnd drives TenantClient.Provision through a real
// in-process MCP server in both enrollment modes. The server tool reads each
// argument by its snake_case name, so the test pins the exact argument set,
// and checks every result field comes back typed.
func TestTenantProvisionEndToEnd(t *testing.T) {
	stamp := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	expires := stamp.Add(24 * time.Hour)

	cases := []struct {
		name     string
		req      *iam.ProvisionTenantRequest
		wantArgs map[string]interface{}
		result   map[string]interface{}
		want     *iam.ProvisionTenantResponse
	}{
		{
			name: "cert",
			req: &iam.ProvisionTenantRequest{
				TenantID:        "acme",
				Name:            "Acme",
				IsolationType:   iam.TenantIsolationBarrier,
				AdminUsername:   "root",
				AdminEnrollment: iam.TenantAdminEnrollCert,
				AdminCSRPEM:     "CSR-PEM",
			},
			wantArgs: map[string]interface{}{
				"tenant_id":        "acme",
				"name":             "Acme",
				"isolation_type":   "barrier",
				"admin_username":   "root",
				"admin_enrollment": "cert",
				"admin_csr_pem":    "CSR-PEM",
			},
			result: map[string]interface{}{
				"tenant_id":             "acme",
				"admin_user_id":         "7",
				"admin_username":        "root",
				"admin_role_id":         "role-admin",
				"admin_cert_pem":        "CERT-PEM",
				"admin_cert_thumbprint": "spki-sha256",
				"timestamp":             stamp.Format(time.RFC3339),
			},
			want: &iam.ProvisionTenantResponse{
				TenantID:            "acme",
				AdminUserID:         "7",
				AdminUsername:       "root",
				AdminRoleID:         "role-admin",
				AdminCertPEM:        "CERT-PEM",
				AdminCertThumbprint: "spki-sha256",
				Timestamp:           stamp,
			},
		},
		{
			name: "invite",
			req: &iam.ProvisionTenantRequest{
				TenantID:          "acme",
				Name:              "Acme",
				IsolationType:     iam.TenantIsolationPrefix,
				AdminUsername:     "root",
				AdminEnrollment:   iam.TenantAdminEnrollInvite,
				InviteExpiryHours: 24,
			},
			wantArgs: map[string]interface{}{
				"tenant_id":           "acme",
				"name":                "Acme",
				"isolation_type":      "prefix",
				"admin_username":      "root",
				"admin_enrollment":    "invite",
				"invite_expiry_hours": float64(24),
			},
			result: map[string]interface{}{
				"tenant_id":         "acme",
				"admin_user_id":     "7",
				"admin_username":    "root",
				"admin_role_id":     "role-admin",
				"admin_pending":     true,
				"invite_token":      "one-time-token",
				"invite_expires_at": expires.Format(time.RFC3339),
				"timestamp":         stamp.Format(time.RFC3339),
			},
			want: &iam.ProvisionTenantResponse{
				TenantID:        "acme",
				AdminUserID:     "7",
				AdminUsername:   "root",
				AdminRoleID:     "role-admin",
				AdminPending:    true,
				InviteToken:     "one-time-token",
				InviteExpiresAt: &expires,
				Timestamp:       stamp,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: tenantProvisionTool,
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.wantArgs) {
						t.Errorf("arguments = %v, want %v", args, tc.wantArgs)
					}
				},
				result: tc.result,
			})
			defer cleanup()

			resp, err := iam.NewTenantClient(tr).Provision(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("response = %+v, want %+v", resp, tc.want)
			}
		})
	}
}

// TestTenantProvisionToolRefusal verifies a tool-level refusal comes back as
// a typed SDK error carrying the server's message.
func TestTenantProvisionToolRefusal(t *testing.T) {
	cases := []struct {
		name string
		code int
		want errors.ErrorCode
	}{
		{"already exists", MCPAlreadyExists, errors.CodeAlreadyExists},
		{"forbidden", MCPPermissionDenied, errors.CodeForbidden},
		{"no code", 0, errors.CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:      tenantProvisionTool,
				toolError:     true,
				toolErrorCode: tc.code,
				rawText:       "tenant acme already exists",
			})
			defer cleanup()

			_, err := iam.NewTenantClient(tr).Provision(context.Background(),
				&iam.ProvisionTenantRequest{TenantID: "acme", AdminEnrollment: iam.TenantAdminEnrollInvite})
			if errors.Code(err) != tc.want || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("expected a %v tool error carrying the server message, got %v", tc.want, err)
			}
		})
	}
}

// TestTenantProvisionBadRequestType verifies a request that cannot be encoded
// is refused as invalid input instead of reaching the tool with no arguments.
func TestTenantProvisionBadRequestType(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: tenantProvisionTool,
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("an unencodable request reached the tool")
		},
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), "quicbaas.IAM.Tenant", "Provision", make(chan int))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected an invalid-input error, got %v", err)
	}
}

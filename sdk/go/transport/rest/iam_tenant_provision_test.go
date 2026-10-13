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
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// capturedRequest is what the provision test server saw.
type capturedRequest struct {
	method string
	path   string
	body   map[string]interface{}
}

// TestTenantProvisionRoundTrip drives TenantClient.Provision through the REST
// transport in both enrollment modes. The server decodes the body strictly,
// so the test pins the exact key set the SDK sends, and checks every
// response field comes back typed.
func TestTenantProvisionRoundTrip(t *testing.T) {
	expires := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	stamp := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		req      *iam.ProvisionTenantRequest
		wantBody map[string]interface{}
		reply    map[string]interface{}
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
			wantBody: map[string]interface{}{
				"tenant_id":        "acme",
				"name":             "Acme",
				"isolation_type":   "barrier",
				"admin_username":   "root",
				"admin_enrollment": "cert",
				"admin_csr_pem":    "CSR-PEM",
			},
			reply: map[string]interface{}{
				"tenant_id":             "acme",
				"admin_user_id":         "7",
				"admin_username":        "root",
				"admin_role_id":         "role-admin",
				"admin_pending":         false,
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
			wantBody: map[string]interface{}{
				"tenant_id":           "acme",
				"name":                "Acme",
				"isolation_type":      "prefix",
				"admin_username":      "root",
				"admin_enrollment":    "invite",
				"invite_expiry_hours": float64(24),
			},
			reply: map[string]interface{}{
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
			// The channel orders the handler's writes before the test's reads.
			captured := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := capturedRequest{method: r.Method, path: r.URL.Path}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read body: %v", err)
				}
				if err := json.Unmarshal(raw, &got.body); err != nil {
					t.Errorf("decode body: %v", err)
				}
				captured <- got
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tc.reply); err != nil {
					t.Errorf("encode reply: %v", err)
				}
			}))
			defer server.Close()

			resp, err := iam.NewTenantClient(newTestTransport(t, server)).Provision(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}

			got := <-captured
			if got.method != http.MethodPost || got.path != "/api/v1/tenants/acme/provision" {
				t.Errorf("request = %s %s, want POST /api/v1/tenants/acme/provision", got.method, got.path)
			}
			if !reflect.DeepEqual(got.body, tc.wantBody) {
				t.Errorf("body = %v, want %v", got.body, tc.wantBody)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("response = %+v, want %+v", resp, tc.want)
			}
		})
	}
}

// TestTenantProvisionRoundTrip_ServerRefusal verifies a server refusal comes
// back as the typed SDK error for its status.
func TestTenantProvisionRoundTrip_ServerRefusal(t *testing.T) {
	cases := []struct {
		name   string
		status int
		is     func(error) bool
	}{
		{"forbidden", http.StatusForbidden, errors.IsForbidden},
		{"conflict", http.StatusConflict, errors.IsAlreadyExists},
		{"bad-request", http.StatusBadRequest, errors.IsInvalidInput},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(`{"error": "refused"}`)); err != nil {
					t.Errorf("write reply: %v", err)
				}
			}))
			defer server.Close()

			_, err := iam.NewTenantClient(newTestTransport(t, server)).Provision(context.Background(),
				&iam.ProvisionTenantRequest{TenantID: "acme", AdminEnrollment: iam.TenantAdminEnrollInvite})
			if !tc.is(err) {
				t.Fatalf("expected a typed %s error, got %v", tc.name, err)
			}
		})
	}
}

// TestTenantProvision_BadRequestType verifies a request that cannot be
// encoded is refused as invalid input before any I/O.
func TestTenantProvision_BadRequestType(t *testing.T) {
	tr := newMappingTransport(t)

	_, err := tr.Request(context.Background(), iamTenantServicePrefix, "Provision", make(chan int))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected an invalid-input error, got %v", err)
	}
}

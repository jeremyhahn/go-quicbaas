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

package quic

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// provisionServer answers POST /api/v1/tenants/{tenant_id}/provision over
// HTTP/3, sending the decoded body on bodies and replying with reply. The
// channel orders the handler's write before the test's read.
func provisionServer(t *testing.T, bodies chan<- map[string]interface{}, reply map[string]interface{}) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tenants/acme/provision" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	})
}

// TestTenantProvisionOverHTTP3 drives TenantClient.Provision over a real
// HTTP/3 listener in both enrollment modes: QUIC carries the REST dialect, so
// the route, the body keys and the typed response match REST exactly.
func TestTenantProvisionOverHTTP3(t *testing.T) {
	stamp := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	expires := stamp.Add(24 * time.Hour)

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
				AdminEnrollment:   iam.TenantAdminEnrollInvite,
				InviteExpiryHours: 24,
			},
			wantBody: map[string]interface{}{
				"tenant_id":           "acme",
				"admin_enrollment":    "invite",
				"invite_expiry_hours": float64(24),
			},
			reply: map[string]interface{}{
				"tenant_id":         "acme",
				"admin_user_id":     "7",
				"admin_username":    "admin",
				"admin_role_id":     "role-admin",
				"admin_pending":     true,
				"invite_token":      "one-time-token",
				"invite_expires_at": expires.Format(time.RFC3339),
				"timestamp":         stamp.Format(time.RFC3339),
			},
			want: &iam.ProvisionTenantResponse{
				TenantID:        "acme",
				AdminUserID:     "7",
				AdminUsername:   "admin",
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
			bodies := make(chan map[string]interface{}, 1)
			addr, shutdown := startTestHTTP3Server(t, provisionServer(t, bodies, tc.reply))
			defer shutdown()

			tr, err := New(testTransportConfig(t, addr))
			require.NoError(t, err)
			defer tr.Close()

			resp, err := iam.NewTenantClient(tr).Provision(context.Background(), tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantBody, <-bodies)
			assert.Equal(t, tc.want, resp)
		})
	}
}

// TestTenantProvisionOverHTTP3_ServerRefusal verifies a refusal served over
// HTTP/3 comes back as the typed SDK error.
func TestTenantProvisionOverHTTP3_ServerRefusal(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		if _, err := w.Write([]byte(`{"error": "insufficient permissions"}`)); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = iam.NewTenantClient(tr).Provision(context.Background(),
		&iam.ProvisionTenantRequest{TenantID: "acme", AdminEnrollment: iam.TenantAdminEnrollInvite})
	assert.True(t, errors.IsForbidden(err), "expected a typed forbidden error, got %v", err)
}

// TestTenantProvisionOverHTTP3_BadRequestType verifies a request that cannot
// be encoded is refused as invalid input before any I/O.
func TestTenantProvisionOverHTTP3_BadRequestType(t *testing.T) {
	tr, err := New(testTransportConfig(t, "127.0.0.1:1"))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.IAM.Tenant", "Provision", make(chan int))
	assert.True(t, errors.IsInvalidInput(err), "expected an invalid-input error, got %v", err)
}

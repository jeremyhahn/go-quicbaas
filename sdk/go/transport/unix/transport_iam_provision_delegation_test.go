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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestTransport_TenantProvision_Delegates verifies tenant provisioning in both
// enrollment modes reaches the gRPC IAM dispatch over AF_UNIX: with no server
// listening the only acceptable failure is the connection itself. Field
// mapping and refusal codes are pinned by the gRPC transport's tests, which
// this transport delegates to unchanged.
func TestTransport_TenantProvision_Delegates(t *testing.T) {
	cases := []struct {
		name string
		req  *iam.ProvisionTenantRequest
	}{
		{"cert", &iam.ProvisionTenantRequest{
			TenantID:        "acme",
			Name:            "Acme",
			IsolationType:   iam.TenantIsolationBarrier,
			AdminUsername:   "root",
			AdminEnrollment: iam.TenantAdminEnrollCert,
			AdminCSRPEM:     "CSR-PEM",
		}},
		{"invite", &iam.ProvisionTenantRequest{
			TenantID:          "acme",
			Name:              "Acme",
			IsolationType:     iam.TenantIsolationPrefix,
			AdminUsername:     "root",
			AdminEnrollment:   iam.TenantAdminEnrollInvite,
			InviteExpiryHours: 24,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.IAM.Tenant", "Provision", tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("provision was rejected as unsupported, dispatch regression: %v", err)
			}
			if errors.IsInvalidInput(err) {
				t.Fatalf("a well-formed provision request was refused as invalid: %v", err)
			}
		})
	}
}

// TestTransport_TenantProvision_BadRequestType verifies a request that cannot
// be encoded is refused as invalid input.
func TestTransport_TenantProvision_BadRequestType(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.IAM.Tenant", "Provision", make(chan int))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected an invalid-input error, got %v", err)
	}
}

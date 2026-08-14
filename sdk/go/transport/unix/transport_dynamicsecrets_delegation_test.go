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

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// dynamicSecretsService is the SDK service identifier the dynamic secrets
// client routes through.
const dynamicSecretsService = "quicbaas.DynamicSecrets"

// TestTransport_DynamicSecrets_Delegates proves the Unix transport routes the
// DynamicSecrets surface through the gRPC dynamic secrets handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE
// DynamicSecretsService for both. With no server listening and a short deadline
// every call fails on connection — but crucially NOT with "unimplemented" or
// "invalid request type", which would mean the dispatch never saw the request
// or rejected the SDK's own types. The gRPC handler tests assert the proto
// conversion; this pins the delegation contract.
func TestTransport_DynamicSecrets_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"create-role", "CreateRole", &dsclient.CreateRoleRequest{
			Name: "db-readonly", GeneratorType: "postgres",
			DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour,
		}},
		{"get-role", "GetRole", &dsclient.GetRoleRequest{Name: "db-readonly"}},
		{"list-roles", "ListRoles", &dsclient.ListRolesRequest{}},
		{"delete-role", "DeleteRole", &dsclient.DeleteRoleRequest{Name: "db-readonly"}},
		{"generate", "GenerateCredential", &dsclient.GenerateCredentialRequest{RoleName: "db-readonly"}},
		{"renew-lease", "RenewLease", &dsclient.RenewLeaseRequest{LeaseID: "lease-1"}},
		{"revoke-lease", "RevokeLease", &dsclient.RevokeLeaseRequest{LeaseID: "lease-1"}},
		{"list-leases", "ListLeases", &dsclient.ListLeasesRequest{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, dynamicSecretsService, tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("dynamic secrets request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "not exposed by DynamicSecretsService") {
				t.Fatalf("method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
			if strings.Contains(msg, "invalid request type") {
				t.Fatalf("method %q rejected its own SDK request type: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_DynamicSecrets_ClosedTransport proves a closed Unix transport
// rejects dynamic secrets calls instead of dialing a dead socket.
func TestTransport_DynamicSecrets_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := tr.Request(context.Background(), dynamicSecretsService, "ListRoles",
		&dsclient.ListRolesRequest{})
	if err == nil {
		t.Fatal("expected a closed-transport error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed-transport error, got: %v", err)
	}
}

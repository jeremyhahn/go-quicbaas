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

	accountclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestTransport_Account_Delegates proves the Unix transport routes the pooled
// provider account surface through the gRPC account handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE AccountService
// for both. With no server listening and a short deadline every call fails on
// connection — but crucially NOT with "not supported" / "not yet supported by
// SDK", which is the Phase 8 regression this guards. The gRPC handler tests
// assert the full proto round-trip; this pins the delegation contract.
func TestTransport_Account_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"create", "Create", &accountclient.CreateAccountRequest{
			TenantID: "t1", Provider: accountclient.ProviderClaude,
			Credential: &accountclient.Credential{AccessToken: "SECRET-ACCESS-TOKEN"},
		}},
		{"get", "Get", &accountclient.GetAccountRequest{TenantID: "t1", AccountID: "a1"}},
		{"list", "List", &accountclient.ListAccountsRequest{TenantID: "t1"}},
		{"update", "Update", &accountclient.UpdateAccountRequest{TenantID: "t1", AccountID: "a1"}},
		{"delete", "Delete", &accountclient.DeleteAccountRequest{TenantID: "t1", AccountID: "a1"}},
		{"rotate", "RotateCredential", &accountclient.RotateCredentialRequest{
			TenantID: "t1", AccountID: "a1",
			Credential: &accountclient.Credential{AccessToken: "SECRET-ACCESS-TOKEN"},
		}},
		{"revoke", "Revoke", &accountclient.RevokeAccountRequest{TenantID: "t1", AccountID: "a1"}},
		{"set-default", "SetDefault", &accountclient.SetDefaultAccountRequest{TenantID: "t1", AccountID: "a1"}},
		{"usage", "Usage", &accountclient.GetAccountUsageRequest{TenantID: "t1", AccountID: "a1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.Account", tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("account request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "unknown account method") {
				t.Fatalf("account method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_Account_ClosedTransport proves a closed Unix transport rejects
// account calls instead of dialing a dead socket.
func TestTransport_Account_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := tr.Request(context.Background(), "quicbaas.Account", "List",
		&accountclient.ListAccountsRequest{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected an error from a closed transport")
	}
}

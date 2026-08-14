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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// =============================================================================
// Harness
// =============================================================================

// mockQuicBaaSForAccount exposes an AccountService to the embedded transport.
type mockQuicBaaSForAccount struct {
	service.QuicBaaS
	accountSvc service.AccountService
}

func (m *mockQuicBaaSForAccount) Account() service.AccountService { return m.accountSvc }

func embeddedSampleAccount() *orchestration.Account {
	return &orchestration.Account{
		ID:            "acct-1",
		TenantID:      "tenant-1",
		ProjectID:     "proj-1",
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "alice-work",
		Default:       true,
		Status:        orchestration.AccountStatusActive,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func embeddedSampleCredential() *accountSDK.Credential {
	return &accountSDK.Credential{
		AccessToken:  "SECRET-ACCESS-TOKEN",
		RefreshToken: "SECRET-REFRESH-TOKEN",
		Env:          map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
		ExpiresAt:    time.Now().Add(time.Hour).UTC(),
	}
}

// newAccountTransport builds an embedded transport wired to accountSvc.
func newAccountTransport(t *testing.T, accountSvc service.AccountService) *Transport {
	t.Helper()
	tr, err := New(&mockQuicBaaSForAccount{accountSvc: accountSvc})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

// =============================================================================
// Dispatch
// =============================================================================

// TestAccountHandlersAreRegistered proves every account method is routable
// in-process; a missing entry would surface to callers as UNIMPLEMENTED.
func TestAccountHandlersAreRegistered(t *testing.T) {
	for _, method := range []string{
		"Create", "Get", "List", "Update", "Delete",
		"RotateCredential", "Revoke", "SetDefault", "Usage",
	} {
		if _, ok := unaryHandlers[accountServiceName+"/"+method]; !ok {
			t.Errorf("quicbaas.Account/%s is not registered in the embedded transport", method)
		}
	}
}

// TestAccountHandlers_HappyPath drives every verb through the transport and
// asserts the request/response conversions are lossless.
func TestAccountHandlers_HappyPath(t *testing.T) {
	var createReq *service.CreateAccountRequest
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			createReq = req
			return &service.CreateAccountResponse{Account: embeddedSampleAccount()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: embeddedSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			if req.Provider != orchestration.AccountProviderClaude || req.ProjectID != "proj-1" {
				t.Errorf("filters lost in conversion: %+v", req)
			}
			return &service.ListAccountsResponse{
				Accounts: []*orchestration.Account{embeddedSampleAccount()}, Total: 1,
			}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			if !req.ClearProjectID {
				t.Error("clear_project_id lost in conversion")
			}
			return &service.UpdateAccountResponse{Account: embeddedSampleAccount()}, nil
		},
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			return &service.DeleteAccountResponse{AccountID: req.AccountID, Deleted: true}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			if req.Credential == nil || req.Credential.RefreshToken != "SECRET-REFRESH-TOKEN" {
				t.Errorf("rotate credential lost in conversion: %+v", req.Credential)
			}
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID}, nil
		},
		RevokeAccountFunc: func(ctx context.Context, req *service.RevokeAccountRequest) (*service.RevokeAccountResponse, error) {
			if req.Reason != "compromised" {
				t.Errorf("reason lost in conversion: %q", req.Reason)
			}
			revoked := embeddedSampleAccount()
			revoked.Status = orchestration.AccountStatusRevoked
			return &service.RevokeAccountResponse{Account: revoked}, nil
		},
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			return &service.SetDefaultAccountResponse{AccountID: req.AccountID}, nil
		},
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage: &orchestration.UsageReport{TotalTokens: 4242, ByAgent: map[string]int64{"a": 42}},
			}, nil
		},
	}
	tr := newAccountTransport(t, svc)
	ctx := context.Background()

	created, err := tr.Request(ctx, accountServiceName, "Create", &accountSDK.CreateAccountRequest{
		TenantID: "tenant-1", ProjectID: "proj-1", Provider: accountSDK.ProviderClaude,
		DisplayName: "alice-work", Default: true, RefreshPolicy: accountSDK.RefreshOAuth2,
		Credential: embeddedSampleCredential(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, ok := created.(*accountSDK.AccountInfo)
	if !ok || info.ID != "acct-1" {
		t.Fatalf("unexpected create response: %#v", created)
	}
	if createReq == nil || createReq.Credential == nil ||
		createReq.Credential.AccessToken != "SECRET-ACCESS-TOKEN" ||
		createReq.Credential.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "SECRET-ENV-VALUE" ||
		createReq.Credential.ExpiresAt.IsZero() {
		t.Fatalf("credential lost in conversion: %+v", createReq)
	}
	if createReq.Provider != orchestration.AccountProviderClaude || !createReq.Default {
		t.Errorf("metadata lost in conversion: %+v", createReq)
	}

	if _, err := tr.Request(ctx, accountServiceName, "Get",
		&accountSDK.GetAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"}); err != nil {
		t.Errorf("Get: %v", err)
	}

	listed, err := tr.Request(ctx, accountServiceName, "List",
		&accountSDK.ListAccountsRequest{TenantID: "tenant-1", Provider: accountSDK.ProviderClaude, ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	list, ok := listed.(*accountSDK.ListAccountsResponse)
	if !ok || list.Total != 1 || len(list.Accounts) != 1 {
		t.Fatalf("unexpected list response: %#v", listed)
	}

	if _, err := tr.Request(ctx, accountServiceName, "Update",
		&accountSDK.UpdateAccountRequest{TenantID: "tenant-1", AccountID: "acct-1", ClearProjectID: true}); err != nil {
		t.Errorf("Update: %v", err)
	}

	deleted, err := tr.Request(ctx, accountServiceName, "Delete",
		&accountSDK.DeleteAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if del, ok := deleted.(*accountSDK.DeleteAccountResponse); !ok || !del.Deleted {
		t.Fatalf("unexpected delete response: %#v", deleted)
	}

	if _, err := tr.Request(ctx, accountServiceName, "RotateCredential",
		&accountSDK.RotateCredentialRequest{TenantID: "tenant-1", AccountID: "acct-1", Credential: embeddedSampleCredential()}); err != nil {
		t.Errorf("RotateCredential: %v", err)
	}

	revoked, err := tr.Request(ctx, accountServiceName, "Revoke",
		&accountSDK.RevokeAccountRequest{TenantID: "tenant-1", AccountID: "acct-1", Reason: "compromised"})
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if rev, ok := revoked.(*accountSDK.AccountInfo); !ok || rev.Status != accountSDK.StatusRevoked {
		t.Fatalf("unexpected revoke response: %#v", revoked)
	}

	if _, err := tr.Request(ctx, accountServiceName, "SetDefault",
		&accountSDK.SetDefaultAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"}); err != nil {
		t.Errorf("SetDefault: %v", err)
	}

	usage, err := tr.Request(ctx, accountServiceName, "Usage",
		&accountSDK.GetAccountUsageRequest{TenantID: "tenant-1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if report, ok := usage.(*accountSDK.UsageReport); !ok || report.TotalTokens != 4242 {
		t.Fatalf("unexpected usage response: %#v", usage)
	}
}

// =============================================================================
// Error paths
// =============================================================================

// TestAccountHandlers_WrongRequestType proves each handler rejects a request
// of the wrong Go type instead of panicking on the assertion.
func TestAccountHandlers_WrongRequestType(t *testing.T) {
	tr := newAccountTransport(t, &mocks.MockAccountService{})
	for _, method := range []string{
		"Create", "Get", "List", "Update", "Delete",
		"RotateCredential", "Revoke", "SetDefault", "Usage",
	} {
		if _, err := tr.Request(context.Background(), accountServiceName, method, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}

// TestAccountHandlers_ServiceUnavailable proves every verb fails closed when
// the composed service exposes no AccountService.
func TestAccountHandlers_ServiceUnavailable(t *testing.T) {
	tr := newAccountTransport(t, nil)
	calls := map[string]interface{}{
		"Create":           &accountSDK.CreateAccountRequest{TenantID: "t", Provider: accountSDK.ProviderClaude, Credential: embeddedSampleCredential()},
		"Get":              &accountSDK.GetAccountRequest{TenantID: "t", AccountID: "a"},
		"List":             &accountSDK.ListAccountsRequest{TenantID: "t"},
		"Update":           &accountSDK.UpdateAccountRequest{TenantID: "t", AccountID: "a"},
		"Delete":           &accountSDK.DeleteAccountRequest{TenantID: "t", AccountID: "a"},
		"RotateCredential": &accountSDK.RotateCredentialRequest{TenantID: "t", AccountID: "a", Credential: embeddedSampleCredential()},
		"Revoke":           &accountSDK.RevokeAccountRequest{TenantID: "t", AccountID: "a"},
		"SetDefault":       &accountSDK.SetDefaultAccountRequest{TenantID: "t", AccountID: "a"},
		"Usage":            &accountSDK.GetAccountUsageRequest{TenantID: "t", AccountID: "a"},
	}
	for method, req := range calls {
		if _, err := tr.Request(context.Background(), accountServiceName, method, req); err == nil {
			t.Errorf("%s succeeded with no account service wired", method)
		}
	}
}

// TestAccountCredentialConverterIsInboundOnly documents and pins the one-way
// credential conversion: nil in, nil out, and no reverse converter exists.
func TestAccountCredentialConverterIsInboundOnly(t *testing.T) {
	if accountCredentialToOrchestration(nil) != nil {
		t.Error("nil credential must convert to nil")
	}
	if accountInfoFromOrchestration(nil) != nil {
		t.Error("nil account must convert to nil")
	}
	converted := accountCredentialToOrchestration(embeddedSampleCredential())
	if converted == nil || converted.AccessToken != "SECRET-ACCESS-TOKEN" {
		t.Fatalf("credential lost in conversion: %+v", converted)
	}
}

// =============================================================================
// Credential leak guard
// =============================================================================

// TestAccountEmbeddedResponsesCarryNoCredential asserts no embedded response
// serializes credential material.
func TestAccountEmbeddedResponsesCarryNoCredential(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			if req.Credential == nil || req.Credential.AccessToken == "" {
				t.Fatal("test setup: credential did not reach the service")
			}
			return &service.CreateAccountResponse{Account: embeddedSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{embeddedSampleAccount()}, Total: 1}, nil
		},
	}
	tr := newAccountTransport(t, svc)
	ctx := context.Background()

	created, err := tr.Request(ctx, accountServiceName, "Create", &accountSDK.CreateAccountRequest{
		TenantID: "tenant-1", Provider: accountSDK.ProviderClaude, Credential: embeddedSampleCredential(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	listed, err := tr.Request(ctx, accountServiceName, "List", &accountSDK.ListAccountsRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	markers := []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-ENV-VALUE", "access_token"}
	for i, resp := range []interface{}{created, listed} {
		encoded, marshalErr := json.Marshal(resp)
		if marshalErr != nil {
			t.Fatalf("marshal response %d: %v", i, marshalErr)
		}
		for _, marker := range markers {
			if strings.Contains(string(encoded), marker) {
				t.Errorf("response %d leaked credential marker %q: %s", i, marker, encoded)
			}
		}
	}
}

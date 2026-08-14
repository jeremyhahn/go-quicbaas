//go:build quic

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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Harness
// =============================================================================

// mockServiceWithAccount exposes an AccountService to the QUIC handler.
type mockServiceWithAccount struct {
	mockService
	accountSvc service.AccountService
}

func (m *mockServiceWithAccount) Account() service.AccountService { return m.accountSvc }

// createAccountHandler builds a QUIC handler wired to accountSvc.
func createAccountHandler(accountSvc service.AccountService) *Handler {
	return NewHandler(&mockServiceWithAccount{accountSvc: accountSvc}, nil, nil, nil, createOrchestratorTestLogger())
}

// accountRequest builds an OpOrchestrator request carrying an account sub-op.
func accountRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()
	payload, err := EncodePayload(body)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	full := make([]byte, 1+len(payload))
	full[0] = subOp
	copy(full[1:], payload)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

// quicSampleAccount is the metadata record the fakes return.
func quicSampleAccount() *orchestration.Account {
	return &orchestration.Account{
		ID:            "acct-1",
		TenantID:      "tenant-1",
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "alice-work",
		Status:        orchestration.AccountStatusActive,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

// =============================================================================
// Dispatch
// =============================================================================

// TestAccountSubOpRangeRoutesToAccountHandler proves the account sub-op range
// carried on OpOrchestrator reaches the account dispatch table rather than
// falling into the orchestrator switch (which would answer
// UnknownSubOpcodeError).
func TestAccountSubOpRangeRoutesToAccountHandler(t *testing.T) {
	called := map[byte]bool{}
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			called[SubOpAccountCreate] = true
			return &service.CreateAccountResponse{Account: quicSampleAccount()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			called[SubOpAccountGet] = true
			return &service.GetAccountResponse{Account: quicSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			called[SubOpAccountList] = true
			return &service.ListAccountsResponse{}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			called[SubOpAccountUpdate] = true
			return &service.UpdateAccountResponse{Account: quicSampleAccount()}, nil
		},
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			called[SubOpAccountDelete] = true
			return &service.DeleteAccountResponse{AccountID: req.AccountID, Deleted: true}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			called[SubOpAccountRotate] = true
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID}, nil
		},
		RevokeAccountFunc: func(ctx context.Context, req *service.RevokeAccountRequest) (*service.RevokeAccountResponse, error) {
			called[SubOpAccountRevoke] = true
			return &service.RevokeAccountResponse{Account: quicSampleAccount()}, nil
		},
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			called[SubOpAccountSetDefault] = true
			return &service.SetDefaultAccountResponse{AccountID: req.AccountID}, nil
		},
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			called[SubOpAccountUsage] = true
			return &service.GetAccountUsageResponse{Usage: &orchestration.UsageReport{TotalTokens: 7}}, nil
		},
	}
	handler := createAccountHandler(svc)

	bodies := map[byte]interface{}{
		SubOpAccountCreate: &service.CreateAccountRequest{
			TenantID: "tenant-1", Provider: orchestration.AccountProviderClaude,
			Credential: &orchestration.AccountCredential{AccessToken: "SECRET"},
		},
		SubOpAccountGet:        &service.GetAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"},
		SubOpAccountList:       &service.ListAccountsRequest{TenantID: "tenant-1"},
		SubOpAccountUpdate:     &service.UpdateAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"},
		SubOpAccountDelete:     &service.DeleteAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"},
		SubOpAccountRotate:     &service.RotateAccountCredentialRequest{TenantID: "tenant-1", AccountID: "acct-1", Credential: &orchestration.AccountCredential{AccessToken: "SECRET"}},
		SubOpAccountRevoke:     &service.RevokeAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"},
		SubOpAccountSetDefault: &service.SetDefaultAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"},
		SubOpAccountUsage:      &service.GetAccountUsageRequest{TenantID: "tenant-1", AccountID: "acct-1"},
	}
	for subOp, body := range bodies {
		resp := handler.HandleRequest(context.Background(), accountRequest(t, subOp, body))
		if resp.Status != StatusOK {
			t.Errorf("sub-op 0x%02X: status = %d: %s", subOp, resp.Status, string(resp.Payload))
		}
		if !called[subOp] {
			t.Errorf("sub-op 0x%02X did not reach the account service", subOp)
		}
	}
}

// TestAccountDispatch_UnknownSubOp covers the error path: a byte inside the
// account range with no registered handler must fail, not silently succeed.
func TestAccountDispatch_UnknownSubOp(t *testing.T) {
	handler := createAccountHandler(&mocks.MockAccountService{})
	// isAccountSubOp admits the whole 0x30-0x38 range; every byte in it is
	// currently mapped, so drive the negative case through the dispatcher
	// directly with an out-of-table byte.
	resp := handler.handleAccountDispatch(context.Background(), 0x3F, &Request{})
	if resp.Status == StatusOK {
		t.Fatal("expected an error for an unregistered account sub-op")
	}
}

// TestAccountDispatch_ServiceUnavailable proves every account sub-op fails
// closed when no AccountService is wired.
func TestAccountDispatch_ServiceUnavailable(t *testing.T) {
	handler := createAccountHandler(nil)
	for subOp := SubOpAccountCreate; subOp <= SubOpAccountUsage; subOp++ {
		resp := handler.HandleRequest(context.Background(),
			accountRequest(t, subOp, map[string]string{"tenant_id": "t1", "account_id": "a1"}))
		if resp.Status == StatusOK {
			t.Errorf("sub-op 0x%02X succeeded with no account service wired", subOp)
		}
	}
}

// TestAccountDispatch_InvalidPayload covers the decode error path.
func TestAccountDispatch_InvalidPayload(t *testing.T) {
	handler := createAccountHandler(&mocks.MockAccountService{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpOrchestrator,
		Payload: append([]byte{SubOpAccountGet}, []byte("not-json")...),
	})
	if resp.Status == StatusOK {
		t.Fatal("expected an error for an undecodable account payload")
	}
}

// =============================================================================
// Authorization
// =============================================================================

// TestAccountSubOpsAuthorizeAsCredentials is the authz guard: the account
// sub-ops share the OpOrchestrator dispatch byte, so without sub-op-aware
// resolution they would authorize under the materially weaker
// orchestrator:status the rest of that opcode uses.
func TestAccountSubOpsAuthorizeAsCredentials(t *testing.T) {
	cases := []struct {
		subOp  byte
		action string
	}{
		{SubOpAccountCreate, "store"},
		{SubOpAccountGet, "retrieve"},
		{SubOpAccountList, "list"},
		{SubOpAccountUpdate, "store"},
		{SubOpAccountDelete, "delete"},
		{SubOpAccountRotate, "rotate"},
		{SubOpAccountRevoke, "delete"},
		{SubOpAccountSetDefault, "store"},
		{SubOpAccountUsage, "retrieve"},
	}
	for _, tc := range cases {
		resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tc.subOp})
		if !ok {
			t.Errorf("sub-op 0x%02X is unmapped (default-denied)", tc.subOp)
			continue
		}
		if resource != "credentials" || action != tc.action {
			t.Errorf("sub-op 0x%02X -> %s:%s, want credentials:%s", tc.subOp, resource, action, tc.action)
		}
	}
}

// TestNonAccountOrchestratorSubOpsKeepOrchestratorPermission proves the
// account branch did not capture the rest of the OpOrchestrator surface.
func TestNonAccountOrchestratorSubOpsKeepOrchestratorPermission(t *testing.T) {
	resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{SubOpOrchestratorSpawn})
	if !ok {
		t.Fatal("orchestrator spawn lost its permission mapping")
	}
	if resource != "orchestrator" || action != "status" {
		t.Fatalf("orchestrator spawn -> %s:%s, want orchestrator:status", resource, action)
	}
}

// =============================================================================
// Credential leak guard
// =============================================================================

// TestAccountQUICResponsesCarryNoCredential drives every response-producing
// account sub-op and asserts no credential marker reaches the wire.
func TestAccountQUICResponsesCarryNoCredential(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			if req.Credential == nil || req.Credential.AccessToken == "" {
				t.Fatal("test setup: credential did not reach the service")
			}
			return &service.CreateAccountResponse{Account: quicSampleAccount()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: quicSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{quicSampleAccount()}, Total: 1}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID}, nil
		},
	}
	handler := createAccountHandler(svc)

	cred := &orchestration.AccountCredential{
		AccessToken:  "SECRET-ACCESS-TOKEN",
		RefreshToken: "SECRET-REFRESH-TOKEN",
		Env:          map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
	}
	requests := []*Request{
		accountRequest(t, SubOpAccountCreate, &service.CreateAccountRequest{
			TenantID: "tenant-1", Provider: orchestration.AccountProviderClaude, Credential: cred,
		}),
		accountRequest(t, SubOpAccountGet, &service.GetAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"}),
		accountRequest(t, SubOpAccountList, &service.ListAccountsRequest{TenantID: "tenant-1"}),
		accountRequest(t, SubOpAccountRotate, &service.RotateAccountCredentialRequest{
			TenantID: "tenant-1", AccountID: "acct-1", Credential: cred,
		}),
	}
	markers := []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-ENV-VALUE", "access_token", "refresh_token"}
	for i, req := range requests {
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Fatalf("request %d failed: %s", i, string(resp.Payload))
		}
		body := string(resp.Payload)
		for _, marker := range markers {
			if strings.Contains(body, marker) {
				t.Errorf("response %d leaked credential marker %q: %s", i, marker, body)
			}
		}
	}
}

// =============================================================================
// Spawn account-field propagation
// =============================================================================

// TestQUICSpawn_PinsAccountID asserts an explicit account_id survives the QUIC
// wire into the service request.
func TestQUICSpawn_PinsAccountID(t *testing.T) {
	var seen *orchestration.SpawnSessionRequest
	agentSvc := &testAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.SpawnSessionRequest{Backend: "claude", AccountID: "acct-pinned"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp := handler.HandleRequest(context.Background(), buildOrchestratorRequest(SubOpOrchestratorSpawn, payload))
	if resp.Status != StatusOK {
		t.Fatalf("spawn failed: %s", string(resp.Payload))
	}
	if seen == nil || seen.AccountID != "acct-pinned" {
		t.Fatalf("QUIC spawn dropped account_id: %+v", seen)
	}
	if seen.AccountAuto {
		t.Error("QUIC spawn invented account_auto")
	}
}

// TestQUICSpawn_CarriesAccountAuto asserts account_auto and the named strategy
// survive the QUIC wire — without them the least-usage strategy is unreachable.
func TestQUICSpawn_CarriesAccountAuto(t *testing.T) {
	var seen *orchestration.SpawnSessionRequest
	agentSvc := &testAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.SpawnSessionRequest{
		Backend: "claude", AccountAuto: true, AccountStrategy: "least_usage",
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp := handler.HandleRequest(context.Background(), buildOrchestratorRequest(SubOpOrchestratorSpawn, payload))
	if resp.Status != StatusOK {
		t.Fatalf("spawn failed: %s", string(resp.Payload))
	}
	if seen == nil || !seen.AccountAuto || seen.AccountStrategy != "least_usage" {
		t.Fatalf("QUIC spawn dropped account_auto/account_strategy: %+v", seen)
	}
}

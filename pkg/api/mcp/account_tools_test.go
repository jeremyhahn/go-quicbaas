//go:build mcp

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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Harness
// =============================================================================

// accountMockQuicBaaS exposes an AccountService to the MCP tool registry.
type accountMockQuicBaaS struct {
	mockService
	accountSvc service.AccountService
}

func (m *accountMockQuicBaaS) Account() service.AccountService { return m.accountSvc }

func newAccountRegistry(t *testing.T, accountSvc service.AccountService) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&accountMockQuicBaaS{accountSvc: accountSvc}, log)
}

func mcpSampleAccount() *orchestration.Account {
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
// Registration + dispatch
// =============================================================================

// TestAccountToolsAreRegistered proves every account tool is discoverable, so
// an MCP client can actually call it.
func TestAccountToolsAreRegistered(t *testing.T) {
	reg := newAccountRegistry(t, &mocks.MockAccountService{})
	registered := make(map[string]bool)
	for _, tool := range reg.ListTools() {
		registered[tool.Name] = true
	}
	for name := range accountToolHandlers {
		if !registered[name] {
			t.Errorf("account tool %q is not registered and is therefore unreachable", name)
		}
		if !isAccountTool(name) {
			t.Errorf("isAccountTool(%q) = false", name)
		}
	}
	if isAccountTool("project_create") {
		t.Error("isAccountTool matched a non-account tool")
	}
}

// TestExecuteAccountTools_HappyPath drives every account tool through the
// registry's public entry point and asserts each reaches its service verb.
func TestExecuteAccountTools_HappyPath(t *testing.T) {
	called := map[string]bool{}
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			called["create"] = true
			if req.Credential == nil || req.Credential.AccessToken != "SECRET-ACCESS-TOKEN" {
				t.Errorf("credential not forwarded: %+v", req.Credential)
			}
			if req.Provider != orchestration.AccountProviderClaude {
				t.Errorf("provider not forwarded: %q", req.Provider)
			}
			return &service.CreateAccountResponse{Account: mcpSampleAccount()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			called["get"] = true
			return &service.GetAccountResponse{Account: mcpSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			called["list"] = true
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{mcpSampleAccount()}, Total: 1}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			called["update"] = true
			if !req.ClearProjectID {
				t.Error("clear_project_id not forwarded")
			}
			return &service.UpdateAccountResponse{Account: mcpSampleAccount()}, nil
		},
		DeleteAccountFunc: func(ctx context.Context, req *service.DeleteAccountRequest) (*service.DeleteAccountResponse, error) {
			called["delete"] = true
			return &service.DeleteAccountResponse{AccountID: req.AccountID, Deleted: true}, nil
		},
		RotateAccountCredentialFunc: func(ctx context.Context, req *service.RotateAccountCredentialRequest) (*service.RotateAccountCredentialResponse, error) {
			called["rotate"] = true
			if req.Credential == nil || req.Credential.RefreshToken != "SECRET-REFRESH-TOKEN" {
				t.Errorf("rotate credential not forwarded: %+v", req.Credential)
			}
			return &service.RotateAccountCredentialResponse{AccountID: req.AccountID}, nil
		},
		RevokeAccountFunc: func(ctx context.Context, req *service.RevokeAccountRequest) (*service.RevokeAccountResponse, error) {
			called["revoke"] = true
			if req.Reason != "compromised" {
				t.Errorf("reason not forwarded: %q", req.Reason)
			}
			return &service.RevokeAccountResponse{Account: mcpSampleAccount()}, nil
		},
		SetDefaultAccountFunc: func(ctx context.Context, req *service.SetDefaultAccountRequest) (*service.SetDefaultAccountResponse, error) {
			called["default"] = true
			return &service.SetDefaultAccountResponse{AccountID: req.AccountID}, nil
		},
		GetAccountUsageFunc: func(ctx context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			called["usage"] = true
			return &service.GetAccountUsageResponse{Usage: &orchestration.UsageReport{TotalTokens: 11}}, nil
		},
	}
	reg := newAccountRegistry(t, svc)

	credential := map[string]interface{}{
		"access_token":  "SECRET-ACCESS-TOKEN",
		"refresh_token": "SECRET-REFRESH-TOKEN",
		"env":           map[string]interface{}{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
	}
	calls := []struct {
		tool string
		args map[string]interface{}
		key  string
	}{
		{"quicbaas_agent_account_create", map[string]interface{}{"tenant_id": "tenant-1", "provider": "claude", "credential": credential}, "create"},
		{"quicbaas_agent_account_get", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1"}, "get"},
		{"quicbaas_agent_account_list", map[string]interface{}{"tenant_id": "tenant-1"}, "list"},
		{"quicbaas_agent_account_update", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1", "clear_project_id": true}, "update"},
		{"quicbaas_agent_account_delete", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1"}, "delete"},
		{"quicbaas_agent_account_rotate", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1", "credential": credential}, "rotate"},
		{"quicbaas_agent_account_revoke", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1", "reason": "compromised"}, "revoke"},
		{"quicbaas_agent_account_set_default", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1"}, "default"},
		{"quicbaas_agent_account_usage", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1"}, "usage"},
	}
	for _, c := range calls {
		if _, err := reg.ExecuteTool(context.Background(), c.tool, c.args); err != nil {
			t.Errorf("%s: %v", c.tool, err)
		}
		if !called[c.key] {
			t.Errorf("%s did not reach the account service", c.tool)
		}
	}
}

// TestExecuteAccountTool_ServiceUnavailable proves every account tool fails
// closed when no AccountService is wired, rather than returning a fake success.
func TestExecuteAccountTool_ServiceUnavailable(t *testing.T) {
	reg := newAccountRegistry(t, nil)
	for name := range accountToolHandlers {
		if _, err := reg.ExecuteTool(context.Background(), name, map[string]interface{}{"tenant_id": "t1"}); err == nil {
			t.Errorf("%s succeeded with no account service wired", name)
		}
	}
}

// TestExecuteAccountTool_UnknownTool covers the dispatcher's error path.
func TestExecuteAccountTool_UnknownTool(t *testing.T) {
	reg := newAccountRegistry(t, &mocks.MockAccountService{})
	if _, err := reg.executeAccountTool(context.Background(), "quicbaas_agent_account_bogus", nil); err == nil {
		t.Fatal("expected an error for an unknown account tool")
	}
}

// TestAccountCredentialFromArgs_MissingCredential proves a call with no
// credential produces nil (which the service layer rejects) rather than an
// empty envelope that would silently overwrite a real credential.
func TestAccountCredentialFromArgs_MissingCredential(t *testing.T) {
	if cred := accountCredentialFromArgs(map[string]interface{}{"tenant_id": "t1"}); cred != nil {
		t.Fatalf("expected nil credential, got %+v", cred)
	}
	if cred := accountCredentialFromArgs(map[string]interface{}{"credential": "not-an-object"}); cred != nil {
		t.Fatalf("expected nil credential for a non-object, got %+v", cred)
	}
}

// =============================================================================
// Authorization
// =============================================================================

// TestAccountToolsHaveQRDBPermissionMapping is the default-deny guard. Under
// RequireAuthz a tool with no qrdb permission mapping is denied AND filtered
// out of tools/list, so an unmapped account tool would ship dead rather than
// working. It also pins the exact permission so REST, gRPC, QUIC and MCP agree.
func TestAccountToolsHaveQRDBPermissionMapping(t *testing.T) {
	want := map[string][2]string{
		"quicbaas_agent_account_create":      {"credentials", "store"},
		"quicbaas_agent_account_get":         {"agent_accounts", "read"},
		"quicbaas_agent_account_list":        {"agent_accounts", "list"},
		"quicbaas_agent_account_update":      {"credentials", "store"},
		"quicbaas_agent_account_delete":      {"credentials", "delete"},
		"quicbaas_agent_account_rotate":      {"credentials", "rotate"},
		"quicbaas_agent_account_revoke":      {"credentials", "delete"},
		"quicbaas_agent_account_set_default": {"credentials", "store"},
		"quicbaas_agent_account_usage":       {"agent_accounts", "read"},
		// The release verb is deliberately NOT a credentials:* grant: that
		// family is registered ScopeCross (system tenant only) and would deny
		// the tenant-scoped agent daemon the tool exists for.
		"quicbaas_agent_account_materialize": {"agent_accounts", "materialize"},
	}
	for name := range accountToolHandlers {
		expected, declared := want[name]
		if !declared {
			t.Errorf("account tool %q has no expected permission in this test; add one", name)
			continue
		}
		resource, action, ok := getToolQRDBPermission(name)
		if !ok {
			t.Errorf("account tool %q is unmapped and is therefore denied + hidden from tools/list", name)
			continue
		}
		if resource != expected[0] || action != expected[1] {
			t.Errorf("%s -> %s:%s, want %s:%s", name, resource, action, expected[0], expected[1])
		}
	}
}

// =============================================================================
// Credential leak guard
// =============================================================================

// TestAccountMCPResultsCarryNoCredential asserts no account tool result
// serializes credential material.
func TestAccountMCPResultsCarryNoCredential(t *testing.T) {
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			if req.Credential == nil || req.Credential.AccessToken == "" {
				t.Fatal("test setup: credential did not reach the service")
			}
			return &service.CreateAccountResponse{Account: mcpSampleAccount()}, nil
		},
		GetAccountFunc: func(ctx context.Context, req *service.GetAccountRequest) (*service.GetAccountResponse, error) {
			return &service.GetAccountResponse{Account: mcpSampleAccount()}, nil
		},
		ListAccountsFunc: func(ctx context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{mcpSampleAccount()}, Total: 1}, nil
		},
	}
	reg := newAccountRegistry(t, svc)

	credential := map[string]interface{}{
		"access_token":  "SECRET-ACCESS-TOKEN",
		"refresh_token": "SECRET-REFRESH-TOKEN",
		"env":           map[string]interface{}{"CLAUDE_CODE_OAUTH_TOKEN": "SECRET-ENV-VALUE"},
	}
	results := []struct {
		tool string
		args map[string]interface{}
	}{
		{"quicbaas_agent_account_create", map[string]interface{}{"tenant_id": "tenant-1", "provider": "claude", "credential": credential}},
		{"quicbaas_agent_account_get", map[string]interface{}{"tenant_id": "tenant-1", "account_id": "acct-1"}},
		{"quicbaas_agent_account_list", map[string]interface{}{"tenant_id": "tenant-1"}},
	}
	markers := []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN", "SECRET-ENV-VALUE", "access_token", "refresh_token"}
	for _, c := range results {
		out, err := reg.ExecuteTool(context.Background(), c.tool, c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("%s: marshal result: %v", c.tool, err)
		}
		for _, marker := range markers {
			if strings.Contains(string(encoded), marker) {
				t.Errorf("%s leaked credential marker %q: %s", c.tool, marker, encoded)
			}
		}
	}
}

// =============================================================================
// Spawn account-field propagation
// =============================================================================

// TestMCPSpawn_PinsAccountID asserts account_id survives the MCP tool
// arguments into the service request.
func TestMCPSpawn_PinsAccountID(t *testing.T) {
	var seen *orchestration.SpawnSessionRequest
	agentSvc := &mockAgentService{
		spawnFn: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":  "tenant-1",
		"project_id": "proj-1",
		"backend":    "claude",
		"account_id": "acct-pinned",
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if seen == nil || seen.AccountID != "acct-pinned" {
		t.Fatalf("MCP spawn dropped account_id: %+v", seen)
	}
	if seen.AccountAuto {
		t.Error("MCP spawn invented account_auto")
	}
}

// TestMCPSpawn_CarriesAccountAuto asserts account_auto and the named strategy
// reach the service layer — without them the least-usage strategy is
// unreachable over MCP.
func TestMCPSpawn_CarriesAccountAuto(t *testing.T) {
	var seen *orchestration.SpawnSessionRequest
	agentSvc := &mockAgentService{
		spawnFn: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":        "tenant-1",
		"project_id":       "proj-1",
		"backend":          "claude",
		"account_auto":     true,
		"account_strategy": "least_usage",
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if seen == nil || !seen.AccountAuto || seen.AccountStrategy != "least_usage" {
		t.Fatalf("MCP spawn dropped account_auto/account_strategy: %+v", seen)
	}
}

// =============================================================================
// Credential release (go-quicbaas#734)
//
// The MCP tool is the one account tool whose result is secret material. Its
// distinguishing property is that TENANCY IS NOT AN ARGUMENT: it comes from the
// authenticated request context, so an MCP client cannot ask for another
// tenant's pool by editing a field.
// =============================================================================

// TestAccountMCP_MaterializeBindsTenancyToTheCaller is the happy path plus the
// property that makes the tool safe.
func TestAccountMCP_MaterializeBindsTenancyToTheCaller(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			_ context.Context, req *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			captured = req
			return &service.MaterializeAccountCredentialResponse{
				AccountID: req.AccountID,
				Provider:  orchestration.AccountProviderClaude,
				Credential: &orchestration.AccountCredential{
					AccessToken:       "RELEASED",
					AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
				},
			}, nil
		},
	})

	ctx := iam.ContextWithTenant(context.Background(), "tenant-1")
	// A tenant_id argument is deliberately supplied AND deliberately ignored:
	// the tool must take tenancy from the caller, not the arguments.
	result, err := reg.executeAccountTool(ctx, "quicbaas_agent_account_materialize", map[string]interface{}{
		"tenant_id":  "tenant-2",
		"account_id": "acct-1",
		"project_id": "project-x",
	})
	if err != nil {
		t.Fatalf("materialize tool error = %v", err)
	}
	if result == nil {
		t.Fatal("materialize tool returned no result")
	}
	if captured == nil {
		t.Fatal("the service was not called")
	}
	if captured.TenantID != "tenant-1" {
		t.Errorf("tool asked for tenant %q, want the caller's tenant-1", captured.TenantID)
	}
	if captured.AccountID != "acct-1" || captured.ProjectID != "project-x" {
		t.Errorf("tool asked for %q/%q, want acct-1/project-x", captured.AccountID, captured.ProjectID)
	}
}

// TestAccountMCP_MaterializeRefusesATenantlessCaller covers the bad-input path:
// an unauthenticated (or tenant-less) call is refused rather than defaulted.
func TestAccountMCP_MaterializeRefusesATenantlessCaller(t *testing.T) {
	var called bool
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			_ context.Context, _ *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			called = true
			return &service.MaterializeAccountCredentialResponse{}, nil
		},
	})

	_, err := reg.executeAccountTool(context.Background(), "quicbaas_agent_account_materialize",
		map[string]interface{}{"account_id": "acct-1"})
	if err == nil {
		t.Fatal("a tenant-less call released a credential")
	}
	if called {
		t.Fatal("a tenant-less call reached the service")
	}
	if !strings.Contains(err.Error(), "tenant") {
		t.Errorf("refusal %q does not name the missing binding", err.Error())
	}
}

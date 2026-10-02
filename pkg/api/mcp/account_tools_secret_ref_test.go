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
	"strings"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestAccountMCP_SecretRefReachesService proves the create and update tools
// carry credential_secret_ref / credential_secret_file to the service, and
// that create no longer demands an inline credential in its schema.
func TestAccountMCP_SecretRefReachesService(t *testing.T) {
	var createReq *service.CreateAccountRequest
	var updateReq *service.UpdateAccountRequest
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			createReq = req
			return &service.CreateAccountResponse{}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updateReq = req
			return &service.UpdateAccountResponse{}, nil
		},
	})
	ctx := context.Background()

	if _, err := reg.executeAccountTool(ctx, "quicbaas_agent_account_create", map[string]interface{}{
		"tenant_id": "t1", "provider": "codex",
		"credential_secret_ref": "agents/codex/reviewer/auth_json", "credential_secret_file": ".codex/auth.json",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if createReq == nil || createReq.Credential != nil ||
		createReq.CredentialSecretRef != "agents/codex/reviewer/auth_json" ||
		createReq.CredentialSecretFile != ".codex/auth.json" {
		t.Fatalf("create reference lost: %+v", createReq)
	}

	if _, err := reg.executeAccountTool(ctx, "quicbaas_agent_account_update", map[string]interface{}{
		"tenant_id": "t1", "account_id": "a1", "credential_secret_ref": "agents/claude/claude1/oauth_token",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if updateReq == nil || updateReq.CredentialSecretRef != "agents/claude/claude1/oauth_token" {
		t.Fatalf("update reference lost: %+v", updateReq)
	}

	for _, tool := range reg.tools {
		if tool.Name != "quicbaas_agent_account_create" {
			continue
		}
		required, ok := tool.InputSchema["required"].([]string)
		if !ok {
			t.Fatal("create's schema has no required list")
		}
		for _, field := range required {
			if field == "credential" {
				t.Fatal("create's schema still requires an inline credential; a secret reference is an equal alternative")
			}
		}
	}
}

// TestAccountMCP_UnresolvedSecretRefDoesNotLeakBackendDiagnostics proves the
// error an MCP tool call reports — its Error(), which the MCP surface returns
// as the tool result — carries nothing of the secrets backend's QBError cause.
func TestAccountMCP_UnresolvedSecretRefDoesNotLeakBackendDiagnostics(t *testing.T) {
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			context.Context, *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			return nil, &orchestration.AccountSecretRefUnresolvedError{
				TenantID: "t1", AccountID: "a1", Ref: "agents/claude/claude1/oauth_token",
				Reason: "the secret could not be read",
				Cause: qberrors.New(qberrors.ErrUnavailable, "secrets.GetSecret", "vault diag sk-fake-marker").
					WithContext("backend_diagnostic", "sk-fake-marker"),
			}
		},
	})

	_, err := reg.executeAccountTool(tenantctx.WithTenant(context.Background(), "t1"),
		"quicbaas_agent_account_materialize", map[string]interface{}{"account_id": "a1"})
	if err == nil {
		t.Fatal("materialize succeeded against a refusing service")
	}
	if strings.Contains(err.Error(), "sk-fake-marker") {
		t.Fatalf("tool error leaks the secrets backend's diagnostics: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "credential_secret_ref") {
		t.Errorf("tool error lost the sanitized refusal: %s", err.Error())
	}
}

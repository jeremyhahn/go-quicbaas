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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Pooled provider account MCP tools.
//
// SECURITY INVARIANT: the tool results are the service-layer response types,
// none of which carries an orchestration.AccountCredential. Credential
// material is accepted inbound on agent_account_create and
// agent_account_rotate only.
//
// ONE TOOL IS THE DELIBERATE EXCEPTION: agent_account_materialize RELEASES an
// envelope, because a pooled credential has to reach the worker host that runs
// the agent (go-quicbaas#734). It carries its own permission rather than the
// credentials:* grant the rest of this family takes, and it binds tenancy to
// the AUTHENTICATED CALLER rather than to a tenant_id argument.
// =============================================================================

// registerAccountTools registers the pooled provider account MCP tools.
func (r *ToolRegistry) registerAccountTools() {
	credentialSchema := map[string]interface{}{
		"type":        "object",
		"description": "Secret envelope for the account. Write-only: no tool ever returns it.",
		"properties": map[string]interface{}{
			"access_token":         map[string]interface{}{"type": "string", "description": "OAuth2 access token"},
			"refresh_token":        map[string]interface{}{"type": "string", "description": "OAuth2 refresh token"},
			"token_endpoint":       map[string]interface{}{"type": "string", "description": "RFC 6749 token endpoint used for refresh"},
			"client_id":            map[string]interface{}{"type": "string", "description": "OAuth2 client identifier"},
			"access_token_env_key": map[string]interface{}{"type": "string", "description": "Environment variable the access token is projected into (e.g. CLAUDE_CODE_OAUTH_TOKEN)"},
			"env":                  map[string]interface{}{"type": "object", "description": "Static environment variables injected into the spawned subprocess"},
			"scopes":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "OAuth2 scopes"},
		},
	}

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_create",
		Description: "Create a pooled provider account (one Anthropic/Claude, OpenAI/Codex, or OpenCode login) an agent session can be spawned against.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":      map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"project_id":     map[string]interface{}{"type": "string", "description": "Optional project scope; empty means tenant-wide"},
				"provider":       map[string]interface{}{"type": "string", "description": "Provider: claude, codex, or opencode"},
				"display_name":   map[string]interface{}{"type": "string", "description": "Operator-chosen label"},
				"default":        map[string]interface{}{"type": "boolean", "description": "Mark this account the tenant+provider default"},
				"refresh_policy": map[string]interface{}{"type": "string", "description": "Credential refresh policy: none or oauth2"},
				"credential":     credentialSchema,
			},
			"required": []string{"tenant_id", "provider", "credential"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_get",
		Description: "Retrieve pooled provider account metadata by ID. Never returns credential material.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_list",
		Description: "List pooled provider accounts for a tenant, optionally filtered by provider and/or project.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant whose accounts to list"},
				"provider":   map[string]interface{}{"type": "string", "description": "Optional provider filter"},
				"project_id": map[string]interface{}{"type": "string", "description": "Optional project filter"},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_update",
		Description: "Update a pooled provider account's non-secret metadata (display name, project scope, refresh policy).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":        map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id":       map[string]interface{}{"type": "string", "description": "Account ID"},
				"display_name":     map[string]interface{}{"type": "string", "description": "New display name"},
				"project_id":       map[string]interface{}{"type": "string", "description": "New project scope"},
				"clear_project_id": map[string]interface{}{"type": "boolean", "description": "Remove the project scope, making the account tenant-wide"},
				"refresh_policy":   map[string]interface{}{"type": "string", "description": "New refresh policy: none or oauth2"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_delete",
		Description: "Permanently delete a pooled provider account and its stored credential envelope.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_rotate",
		Description: "Replace a pooled provider account's stored credential envelope. Metadata is unchanged.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
				"credential": credentialSchema,
			},
			"required": []string{"tenant_id", "account_id", "credential"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_agent_account_materialize",
		Description: "Release one pooled provider account's credential envelope to the caller, for the caller's own tenant. " +
			"The result carries live secret material; it is the only account tool that does.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID to release"},
				"project_id": map[string]interface{}{"type": "string", "description": "Project the requesting spawn belongs to; an account fenced to a project is released only to a request naming it"},
				"job_id":     map[string]interface{}{"type": "string", "description": "Job the requesting spawn works for; it scopes the delivery grant this release takes out"},
				"lease_id":   map[string]interface{}{"type": "string", "description": "Delivery grant the caller already holds. Empty asks for a first release; set it to refresh a copy already held, and expect a refusal once the grant has been revoked"},
			},
			"required": []string{"account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_revoke",
		Description: "Revoke a pooled provider account, excluding it from every selection strategy.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
				"reason":     map[string]interface{}{"type": "string", "description": "Operator-supplied revocation reason"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_set_default",
		Description: "Mark a pooled provider account as the tenant (or tenant+project) default for its provider.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_usage",
		Description: "Report cumulative token usage recorded against a pooled provider account.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID"},
			},
			"required": []string{"tenant_id", "account_id"},
		},
	})
}

// =============================================================================
// Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeAccountCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.CreateAccount(ctx, &service.CreateAccountRequest{
		TenantID:      getString(args, "tenant_id"),
		ProjectID:     getString(args, "project_id"),
		Provider:      orchestration.AccountProvider(getString(args, "provider")),
		DisplayName:   getString(args, "display_name"),
		Default:       getBool(args, "default"),
		RefreshPolicy: orchestration.AccountRefreshPolicy(getString(args, "refresh_policy")),
		Credential:    accountCredentialFromArgs(args),
	})
}

func (r *ToolRegistry) executeAccountGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.GetAccount(ctx, &service.GetAccountRequest{
		TenantID:  getString(args, "tenant_id"),
		AccountID: getString(args, "account_id"),
	})
}

func (r *ToolRegistry) executeAccountList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  getString(args, "tenant_id"),
		Provider:  orchestration.AccountProvider(getString(args, "provider")),
		ProjectID: getString(args, "project_id"),
	})
}

func (r *ToolRegistry) executeAccountUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.UpdateAccount(ctx, &service.UpdateAccountRequest{
		TenantID:       getString(args, "tenant_id"),
		AccountID:      getString(args, "account_id"),
		DisplayName:    getString(args, "display_name"),
		ProjectID:      getString(args, "project_id"),
		ClearProjectID: getBool(args, "clear_project_id"),
		RefreshPolicy:  orchestration.AccountRefreshPolicy(getString(args, "refresh_policy")),
	})
}

func (r *ToolRegistry) executeAccountDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.DeleteAccount(ctx, &service.DeleteAccountRequest{
		TenantID:  getString(args, "tenant_id"),
		AccountID: getString(args, "account_id"),
	})
}

func (r *ToolRegistry) executeAccountRotate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.RotateAccountCredential(ctx, &service.RotateAccountCredentialRequest{
		TenantID:   getString(args, "tenant_id"),
		AccountID:  getString(args, "account_id"),
		Credential: accountCredentialFromArgs(args),
	})
}

// executeAccountMaterialize releases one account's credential envelope.
//
// Tenancy is the CALLER'S OWN and is deliberately NOT read from a tenant_id
// argument the way every other tool in this file reads it: this result is
// secret material, so the tenant must come from the identity the transport
// authenticated. An unauthenticated (or tenant-less) caller is refused rather
// than defaulted.
func (r *ToolRegistry) executeAccountMaterialize(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		return nil, api.NewValidationError("tenant_id", "the caller carries no tenant binding")
	}
	return accountSvc.MaterializeAccountCredential(ctx, &service.MaterializeAccountCredentialRequest{
		TenantID:  tenantID,
		AccountID: getString(args, "account_id"),
		ProjectID: getString(args, "project_id"),
		JobID:     getString(args, "job_id"),
		LeaseID:   getString(args, "lease_id"),
		AgentID:   mcpAccountReleaseSubject(ctx),
	})
}

// mcpAccountReleaseSubject names the principal a release is audited against.
// An MCP caller with no resolvable subject is recorded as unattributed by the
// service layer rather than being silently attributed to the tenant.
func mcpAccountReleaseSubject(ctx context.Context) string {
	if subject, ok := auth.SubjectFromContext(ctx); ok && subject.Authenticated {
		return subject.ID
	}
	return ""
}

func (r *ToolRegistry) executeAccountRevoke(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.RevokeAccount(ctx, &service.RevokeAccountRequest{
		TenantID:  getString(args, "tenant_id"),
		AccountID: getString(args, "account_id"),
		Reason:    getString(args, "reason"),
	})
}

func (r *ToolRegistry) executeAccountSetDefault(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.SetDefaultAccount(ctx, &service.SetDefaultAccountRequest{
		TenantID:  getString(args, "tenant_id"),
		AccountID: getString(args, "account_id"),
	})
}

func (r *ToolRegistry) executeAccountUsage(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	return accountSvc.GetAccountUsage(ctx, &service.GetAccountUsageRequest{
		TenantID:  getString(args, "tenant_id"),
		AccountID: getString(args, "account_id"),
	})
}

// =============================================================================
// Helpers
// =============================================================================

// accountCredentialFromArgs extracts the inbound credential envelope from the
// tool arguments. Returns nil when absent, which the service layer rejects
// with an AccountInvalidError for the verbs that require one.
func accountCredentialFromArgs(args map[string]interface{}) *orchestration.AccountCredential {
	raw, ok := args["credential"].(map[string]interface{})
	if !ok {
		return nil
	}
	cred := &orchestration.AccountCredential{
		AccessToken:       getString(raw, "access_token"),
		RefreshToken:      getString(raw, "refresh_token"),
		TokenEndpoint:     getString(raw, "token_endpoint"),
		ClientID:          getString(raw, "client_id"),
		AccessTokenEnvKey: getString(raw, "access_token_env_key"),
		Scopes:            getStringSlice(raw, "scopes"),
	}
	if env := getStringMap(raw, "env"); len(env) > 0 {
		cred.Env = env
	}
	return cred
}

// getAccountService retrieves the AccountService from the QuicBaaS service.
func (r *ToolRegistry) getAccountService() (service.AccountService, error) {
	accountSvc := r.service.Account()
	if accountSvc == nil {
		return nil, api.NewServiceUnavailableError("account")
	}
	return accountSvc, nil
}

// accountToolHandlers is the O(1) dispatch table for the account tools.
var accountToolHandlers = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"quicbaas_agent_account_create":      (*ToolRegistry).executeAccountCreate,
	"quicbaas_agent_account_get":         (*ToolRegistry).executeAccountGet,
	"quicbaas_agent_account_list":        (*ToolRegistry).executeAccountList,
	"quicbaas_agent_account_update":      (*ToolRegistry).executeAccountUpdate,
	"quicbaas_agent_account_delete":      (*ToolRegistry).executeAccountDelete,
	"quicbaas_agent_account_rotate":      (*ToolRegistry).executeAccountRotate,
	"quicbaas_agent_account_materialize": (*ToolRegistry).executeAccountMaterialize,
	"quicbaas_agent_account_revoke":      (*ToolRegistry).executeAccountRevoke,
	"quicbaas_agent_account_set_default": (*ToolRegistry).executeAccountSetDefault,
	"quicbaas_agent_account_usage":       (*ToolRegistry).executeAccountUsage,
}

// isAccountTool reports whether name is a pooled provider account tool.
func isAccountTool(name string) bool {
	_, ok := accountToolHandlers[name]
	return ok
}

// executeAccountTool dispatches an account tool call to its handler.
func (r *ToolRegistry) executeAccountTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handler, ok := accountToolHandlers[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return handler(r, ctx, args)
}

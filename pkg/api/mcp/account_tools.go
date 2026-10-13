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
	"math"
	"strconv"
	"strings"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
			"files": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string", "contentEncoding": "base64"},
				"description": "Provider config files written into the session home, keyed by relative path (e.g. .codex/auth.json); values are standard base64"},
			"scopes":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "OAuth2 scopes"},
			"expires_at": map[string]interface{}{"type": "string", "format": "date-time", "description": "RFC 3339 time the access token stops working; omit when unknown"},
		},
	}

	billingSchema := map[string]interface{}{
		"type":        "object",
		"description": "How the account is billed. mode: subscription (a turn adds nothing to the bill) or api (a turn bills its list price); monthly_fee_micros: a subscription's flat monthly fee in millionths of a US dollar",
		"properties": map[string]interface{}{
			"mode":               map[string]interface{}{"type": "string", "enum": []string{"subscription", "api"}},
			"monthly_fee_micros": map[string]interface{}{"type": "integer", "minimum": 0},
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
				"billing":        billingSchema,
				"spend":          accountSpendSchema("What the account may spend (sliding token and/or list-price dollar windows) and how many failures take it out of service"),
				"credential":     credentialSchema,
				"credential_secret_ref": map[string]interface{}{"type": "string",
					"description": "Short name of the tenant secret the credential is read from on every spawn (e.g. agents/claude/claude1/oauth_token); instead of credential"},
				"credential_secret_file": map[string]interface{}{"type": "string",
					"description": "With credential_secret_ref: materialize the secret as this file relative to the session home (e.g. .codex/auth.json) instead of as the access token"},
				"max_concurrent_sessions": map[string]interface{}{"type": "integer", "minimum": 0,
					"description": "How many live sessions the account may serve at once; 0 (the default) is unmetered"},
			},
			"required": []string{"tenant_id", "provider"},
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
		Description: "List one page of a tenant's pooled provider accounts, newest first, optionally filtered by provider and/or project. Answers with the accounts plus total, limit, offset and, when more remain, next_offset; limit defaults to 25 (capped at 100) and q searches the account ID, display name and provider.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id":  map[string]interface{}{"type": "string", "description": "Tenant whose accounts to list"},
				"provider":   map[string]interface{}{"type": "string", "description": "Optional provider filter"},
				"project_id": map[string]interface{}{"type": "string", "description": "Optional project filter"},
			}, strings.Join(orchestration.AccountPageSpec().SortFields(), ", ")),
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_update",
		Description: "Update a pooled provider account's non-secret metadata (display name, project scope, refresh policy, session cap, billing, spend policy), or point its credential at a tenant secret.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":        map[string]interface{}{"type": "string", "description": "Tenant that owns the account"},
				"account_id":       map[string]interface{}{"type": "string", "description": "Account ID"},
				"display_name":     map[string]interface{}{"type": "string", "description": "New display name"},
				"project_id":       map[string]interface{}{"type": "string", "description": "New project scope"},
				"clear_project_id": map[string]interface{}{"type": "boolean", "description": "Remove the project scope, making the account tenant-wide"},
				"refresh_policy":   map[string]interface{}{"type": "string", "description": "New refresh policy: none or oauth2"},
				"billing":          billingSchema,
				"spend":            accountSpendSchema("Replaces the account's spend policy; an empty object makes it unmetered"),
				"credential_secret_ref": map[string]interface{}{"type": "string",
					"description": "Switch the credential to (or re-point it at) this tenant secret; re-applying the current one returns an account whose secret was missing to service"},
				"credential_secret_file": map[string]interface{}{"type": "string",
					"description": "With credential_secret_ref: materialize the secret as this file relative to the session home"},
				"max_concurrent_sessions": map[string]interface{}{"type": "integer", "minimum": 0,
					"description": "New cap on live sessions; 0 makes the account unmetered. Omit to leave it unchanged"},
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
		Name: "quicbaas_agent_account_reserve_session",
		Description: "Reserve one session slot on a pooled provider account for the caller's own tenant. " +
			"Refused when the account is at its max_concurrent_sessions cap. The session record naming the returned reservation satisfies it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"account_id": map[string]interface{}{"type": "string", "description": "Account ID to reserve a session slot on"},
				"expected_live": map[string]interface{}{"type": "integer", "minimum": 0,
					"description": "Claim only if the account's live-session count is still this; omit for an unconditional claim"},
			},
			"required": []string{"account_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_account_release_session",
		Description: "Give back a session reservation for a spawn that failed.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"account_id":     map[string]interface{}{"type": "string", "description": "Account the reservation is on"},
				"reservation_id": map[string]interface{}{"type": "string", "description": "Reservation to release"},
			},
			"required": []string{"account_id", "reservation_id"},
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
	billing, err := accountBillingFromArgs(args["billing"])
	if err != nil {
		return nil, err
	}
	spend, _, err := getAccountSpendPolicy(args, "spend")
	if err != nil {
		return nil, err
	}
	credential, err := accountCredentialFromArgs(args)
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
		Billing:       billing,
		Spend:         spend,
		Credential:    credential,

		CredentialSecretRef:   getString(args, "credential_secret_ref"),
		CredentialSecretFile:  getString(args, "credential_secret_file"),
		MaxConcurrentSessions: getInt(args, "max_concurrent_sessions"),
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
	resp, err := accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  getString(args, "tenant_id"),
		Provider:  orchestration.AccountProvider(getString(args, "provider")),
		ProjectID: getString(args, "project_id"),
		Page:      pageFromArgs(args),
	})
	if err != nil {
		return nil, err
	}
	envelope := servicePageEnvelope("accounts", resp.Accounts,
		resp.Total, resp.Limit, resp.Offset, resp.NextOffset, resp.Facets)
	envelope["timestamp"] = resp.Timestamp
	return marshalResult(envelope)
}

func (r *ToolRegistry) executeAccountUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	billing, err := optionalAccountBillingFromArgs(args)
	if err != nil {
		return nil, err
	}
	spend, err := optionalAccountSpendPolicy(args, "spend")
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
		Billing:        billing,
		Spend:          spend,

		CredentialSecretRef:   getString(args, "credential_secret_ref"),
		CredentialSecretFile:  getString(args, "credential_secret_file"),
		MaxConcurrentSessions: getOptionalInt(args, "max_concurrent_sessions"),
	})
}

// AccountBillingArgumentError reports a billing tool argument that is not the
// documented shape. A malformed billing is refused rather than read as "not
// configured": on an update, a zero billing would replace — and so erase — the
// account's saved billing (go-quicbaas#1058 review).
type AccountBillingArgumentError struct {
	// Field is the offending argument: "billing", "billing.mode" or
	// "billing.monthly_fee_micros".
	Field string
}

// Error implements the error interface.
func (e *AccountBillingArgumentError) Error() string {
	switch e.Field {
	case "billing.mode":
		return "mcp: billing.mode must be a string"
	case "billing.monthly_fee_micros":
		return "mcp: billing.monthly_fee_micros must be a whole number"
	default:
		return "mcp: billing must be an object with mode and monthly_fee_micros"
	}
}

// accountBillingFromArgs reads a billing object. An absent billing is the
// zero value; one of the wrong shape is an AccountBillingArgumentError.
func accountBillingFromArgs(raw interface{}) (orchestration.AccountBilling, error) {
	if raw == nil {
		return orchestration.AccountBilling{}, nil
	}
	fields, ok := raw.(map[string]interface{})
	if !ok {
		return orchestration.AccountBilling{}, &AccountBillingArgumentError{Field: "billing"}
	}
	var billing orchestration.AccountBilling
	if mode, present := fields["mode"]; present && mode != nil {
		text, ok := mode.(string)
		if !ok {
			return orchestration.AccountBilling{}, &AccountBillingArgumentError{Field: "billing.mode"}
		}
		billing.Mode = orchestration.BillingMode(text)
	}
	if fee, present := fields["monthly_fee_micros"]; present && fee != nil {
		micros, ok := wholeNumber(fee)
		if !ok {
			return orchestration.AccountBilling{}, &AccountBillingArgumentError{Field: "billing.monthly_fee_micros"}
		}
		billing.MonthlyFeeMicros = micros
	}
	return billing, nil
}

// wholeNumber reads a JSON number that must be integral. JSON decodes every
// number to float64, so a fractional or out-of-range value is refused here
// rather than silently truncated.
func wholeNumber(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != math.Trunc(n) || n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// optionalAccountBillingFromArgs is an update's billing: nil when the caller
// did not send one, which leaves the account's billing unchanged.
func optionalAccountBillingFromArgs(args map[string]interface{}) (*orchestration.AccountBilling, error) {
	raw, present := args["billing"]
	if !present || raw == nil {
		return nil, nil
	}
	billing, err := accountBillingFromArgs(raw)
	if err != nil {
		return nil, err
	}
	return &billing, nil
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
	credential, err := accountCredentialFromArgs(args)
	if err != nil {
		return nil, err
	}
	return accountSvc.RotateAccountCredential(ctx, &service.RotateAccountCredentialRequest{
		TenantID:   getString(args, "tenant_id"),
		AccountID:  getString(args, "account_id"),
		Credential: credential,
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
	tenantID, err := tenantctx.FromContext(ctx, "mcp.account.materialize")
	if err != nil {
		return nil, err
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

// reservationTenant binds a reservation tool to the caller's tenant and
// REFUSES a tenant_id argument naming another one: a principal of tenant A
// must not reach tenant B's accounts, and a silently re-pointed request would
// look to the caller like a claim on the account it named.
func reservationTenant(ctx context.Context, args map[string]interface{}) (string, error) {
	tenantID, err := tenantctx.FromContext(ctx, "mcp.account.reservation")
	if err != nil {
		return "", err
	}
	if requested := getString(args, "tenant_id"); requested != "" && requested != tenantID {
		return "", api.NewValidationError("tenant_id", "must be the caller's own tenant")
	}
	return tenantID, nil
}

// executeAccountReserveSession claims a session slot for the caller's tenant.
func (r *ToolRegistry) executeAccountReserveSession(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	tenantID, err := reservationTenant(ctx, args)
	if err != nil {
		return nil, err
	}
	return accountSvc.ReserveAccountSession(ctx, &service.ReserveAccountSessionRequest{
		TenantID:     tenantID,
		AccountID:    getString(args, "account_id"),
		ExpectedLive: getOptionalInt(args, "expected_live"),
	})
}

// executeAccountReleaseSession gives a session reservation back.
func (r *ToolRegistry) executeAccountReleaseSession(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accountSvc, err := r.getAccountService()
	if err != nil {
		return nil, err
	}
	tenantID, err := reservationTenant(ctx, args)
	if err != nil {
		return nil, err
	}
	return accountSvc.ReleaseAccountSession(ctx, &service.ReleaseAccountSessionRequest{
		TenantID:      tenantID,
		AccountID:     getString(args, "account_id"),
		ReservationID: getString(args, "reservation_id"),
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
func accountCredentialFromArgs(args map[string]interface{}) (*orchestration.AccountCredential, error) {
	raw, ok := args["credential"].(map[string]interface{})
	if !ok {
		return nil, nil
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
	files, err := accountCredentialFilesFromArgs(raw["files"])
	if err != nil {
		return nil, err
	}
	cred.Files = files
	expiresAt, err := accountCredentialExpiryFromArgs(raw["expires_at"])
	if err != nil {
		return nil, err
	}
	cred.ExpiresAt = expiresAt
	return cred, nil
}

// accountCredentialFilesFromArgs decodes a credential.files argument: an object
// mapping a path relative to the session home to standard base64 file bytes.
// It decodes through encoding/json into map[string][]byte, the same field type
// and therefore the same rules as the REST credential body, so a payload one
// transport accepts the other accepts too. An absent or empty object yields nil.
func accountCredentialFilesFromArgs(raw interface{}) (map[string][]byte, error) {
	if raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, &AccountCredentialFilesError{Err: err}
	}
	var files map[string][]byte
	if err := json.Unmarshal(encoded, &files); err != nil {
		return nil, &AccountCredentialFilesError{Err: err}
	}
	if len(files) == 0 {
		return nil, nil
	}
	return files, nil
}

// accountCredentialExpiryFromArgs reads credential.expires_at, an RFC 3339
// timestamp. Absent (or null) is the zero time: an unknown expiry. A value that
// is not an RFC 3339 string is refused rather than read as "never expires",
// which account balancing would otherwise trust.
func accountCredentialExpiryFromArgs(raw interface{}) (time.Time, error) {
	if raw == nil {
		return time.Time{}, nil
	}
	text, ok := raw.(string)
	if !ok {
		return time.Time{}, &AccountCredentialExpiryError{}
	}
	expiresAt, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, &AccountCredentialExpiryError{Value: text, Err: err}
	}
	return expiresAt, nil
}

// AccountCredentialExpiryError reports a credential.expires_at argument that is
// not an RFC 3339 timestamp string.
type AccountCredentialExpiryError struct {
	// Value is the offending text; empty when the argument was not a string.
	Value string
	// Err is the parse failure, nil when the argument was not a string.
	Err error
}

// Error implements the error interface.
func (e *AccountCredentialExpiryError) Error() string {
	if e.Err == nil {
		return "mcp: credential.expires_at must be an RFC 3339 timestamp string"
	}
	return "mcp: credential.expires_at " + strconv.Quote(e.Value) + " is not an RFC 3339 timestamp: " + e.Err.Error()
}

// Unwrap returns the parse failure.
func (e *AccountCredentialExpiryError) Unwrap() error {
	return e.Err
}

// AccountCredentialFilesError reports a credential.files argument that is not
// an object of relative path to base64-encoded file bytes.
type AccountCredentialFilesError struct {
	// Err is the decode failure.
	Err error
}

// Error implements the error interface.
func (e *AccountCredentialFilesError) Error() string {
	return "mcp: credential.files must map relative paths to base64-encoded file bytes: " + e.Err.Error()
}

// Unwrap returns the decode failure.
func (e *AccountCredentialFilesError) Unwrap() error {
	return e.Err
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
	"quicbaas_agent_account_create":          (*ToolRegistry).executeAccountCreate,
	"quicbaas_agent_account_get":             (*ToolRegistry).executeAccountGet,
	"quicbaas_agent_account_list":            (*ToolRegistry).executeAccountList,
	"quicbaas_agent_account_update":          (*ToolRegistry).executeAccountUpdate,
	"quicbaas_agent_account_delete":          (*ToolRegistry).executeAccountDelete,
	"quicbaas_agent_account_rotate":          (*ToolRegistry).executeAccountRotate,
	"quicbaas_agent_account_materialize":     (*ToolRegistry).executeAccountMaterialize,
	"quicbaas_agent_account_reserve_session": (*ToolRegistry).executeAccountReserveSession,
	"quicbaas_agent_account_release_session": (*ToolRegistry).executeAccountReleaseSession,
	"quicbaas_agent_account_revoke":          (*ToolRegistry).executeAccountRevoke,
	"quicbaas_agent_account_set_default":     (*ToolRegistry).executeAccountSetDefault,
	"quicbaas_agent_account_usage":           (*ToolRegistry).executeAccountUsage,
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

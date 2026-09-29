// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package orchestration

import (
	"errors"
	"regexp"
)

// Sentinel errors for agent identity operations.
var (
	// ErrAgentIdentityRequired indicates a required field is missing from the agent identity.
	ErrAgentIdentityRequired = errors.New("orchestration: agent identity required field missing")

	// ErrAgentIdentityInvalidScope indicates an invalid scope format in the agent identity.
	ErrAgentIdentityInvalidScope = errors.New("orchestration: agent identity invalid scope format")
)

// Agent runtime scope constants define the OAuth2 scopes that a remote fleet node
// daemon (pkg/fleet) is authorized to exercise against the central
// orchestrator's FleetRuntime gRPC service. They follow the three-segment
// "resource:subresource:action" naming convention so they slot into the
// same HasScope / wildcard rules as existing scopes.
const (
	// AgentScopeRuntimeSpawn authorizes the caller to start a sandboxed agent
	// session (ACP/Ollama/container spawn) on a remote fleet node host.
	AgentScopeRuntimeSpawn = "agent:runtime:spawn"

	// AgentScopeRuntimePrompt authorizes the caller to send a prompt to an
	// already-spawned session on a remote fleet node host.
	AgentScopeRuntimePrompt = "agent:runtime:prompt"

	// AgentScopeRuntimeStatus authorizes the caller to poll session status
	// and retrieve heartbeat/telemetry from a remote fleet node host.
	AgentScopeRuntimeStatus = "agent:runtime:status"

	// AgentScopeRuntimeInfer authorizes the caller to run a stateless
	// inference call against a backend that does not require a prior Spawn
	// (e.g., Ollama). Separate from AgentScopeRuntimePrompt so operators can
	// grant fine-grained access to the sessionless inference path.
	AgentScopeRuntimeInfer = "agent:runtime:infer"
)

// FleetRuntimeScopes returns the full set of scopes a freshly enrolled agent
// daemon is expected to receive. Callers can compare against this list when
// validating token issuance or documenting bootstrap expectations.
func FleetRuntimeScopes() []string {
	return []string{
		AgentScopeRuntimeSpawn,
		AgentScopeRuntimePrompt,
		AgentScopeRuntimeStatus,
		AgentScopeRuntimeInfer,
	}
}

// FleetRuntimeRole is the role name carried by an agent runtime identity
// (embedded or cert-authenticated). It exists for audit/display purposes;
// authorization decisions are made from the scope set, not the role name.
const FleetRuntimeRole = "agent-runtime"

// Agent MCP scope constants define the least-privilege, qrdb-canonical
// "resource:action" permissions a cert-authenticated agent may exercise over
// the QuicBaaS MCP transport. The set is intentionally minimal: an agent can
// run sessionless inference and read/write key-value workspace data inside its
// OWN tenant (tenant isolation is enforced by the tenant injected from the
// agent certificate's O= field, row-level security downstream). It explicitly
// excludes secrets, KMS, IAM, barrier, cluster and orchestrator control-plane
// verbs — an agent must not be able to administer the platform that runs it.
const (
	// AgentScopeMCPInference authorizes the sessionless inference tool
	// (quicbaas_orchestrator_inference -> orchestrator:inference).
	AgentScopeMCPInference = "orchestrator:inference"

	// AgentScopeMCPKVGet authorizes single-key reads (qrdb_get -> kv:get).
	AgentScopeMCPKVGet = "kv:get"

	// AgentScopeMCPKVPut authorizes key writes (qrdb_put -> kv:put).
	AgentScopeMCPKVPut = "kv:put"

	// AgentScopeMCPKVList authorizes key listing (qrdb_list -> kv:list).
	AgentScopeMCPKVList = "kv:list"
)

// AgentMCPScopes returns the bounded scope set granted to an agent that
// authenticates to the MCP transport with its mTLS agent certificate
// (OU=agents). Operators who need a broader or narrower agent surface manage
// that through token-based identities; this set is the fixed floor for the
// certificate path and must stay least-privilege.
//
// This is the DECLARATION. The copy that is actually written into a tenant's
// agent-session role is iam.AgentSessionGrant, which restates the same literals
// because pkg/iam must not depend on this package; pkg/server
// TestAgentSessionGrant_MatchesTheOrchestrationDeclaration fails the build if
// the two ever disagree. Change both or neither.
func AgentMCPScopes() []string {
	return []string{
		AgentScopeMCPInference,
		AgentScopeMCPKVGet,
		AgentScopeMCPKVPut,
		AgentScopeMCPKVList,
	}
}

// scopeFormatRegex validates that a scope follows the resource:action pattern.
// Accepts formats like "kms:read", "secrets:write", "orchestrator:*", or "a:b:c".
var scopeFormatRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*:[a-zA-Z*][a-zA-Z0-9_*-]*(?::[a-zA-Z*][a-zA-Z0-9_*-]*)?$`)

// AgentIdentityRequiredError indicates a required field is missing from the agent identity.
type AgentIdentityRequiredError struct {
	Field string
}

// Error implements the error interface.
func (e *AgentIdentityRequiredError) Error() string {
	return ErrAgentIdentityRequired.Error() + ": " + e.Field
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentIdentityRequiredError) Unwrap() error {
	return ErrAgentIdentityRequired
}

// AgentIdentityInvalidScopeError indicates an invalid scope format in the agent identity.
type AgentIdentityInvalidScopeError struct {
	Scope string
}

// Error implements the error interface.
func (e *AgentIdentityInvalidScopeError) Error() string {
	return ErrAgentIdentityInvalidScope.Error() + ": " + e.Scope
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentIdentityInvalidScopeError) Unwrap() error {
	return ErrAgentIdentityInvalidScope
}

// AgentIdentity is an agent DAEMON's enrollment result: the OAuth2 application
// the control plane registered for a remote fleet node host, plus the grant that
// application carries. pkg/fleet ProductionEnroller builds it from the response
// to POST /api/v1/fleet/enroll and the token exchange that follows, and the
// daemon's own gRPC spawn server authorizes the central's inbound dispatch
// against its Scopes (GRPCSpawnServer.authorize). That is where it is a
// security control, and the only place.
//
// In the control loop it is NOT one. The loop keeps a role-keyed AgentIdentity
// per job purely so a turn's token spend can be attributed to a stable agent ID
// (turnAttribution); the record it builds carries no ServiceAccountID, ClientID
// or ClientSecret and no authorization check consults it. A SPAWNED SESSION's
// authority does not live here at all -- it lives in the qrdb user record
// iam.EnrollAgentSessionIdentity writes for the session's certificate, because
// go-qrdb resolves a principal's roles from the store and never from the
// credential or from anything the orchestrator holds in memory.
type AgentIdentity struct {
	// AgentID is the orchestrator-assigned agent identifier.
	AgentID string

	// ServiceAccountID is the IAM service account ID provisioned for this agent.
	ServiceAccountID string

	// TenantID is the tenant this agent belongs to.
	TenantID string

	// Role is the agent's role from AgentRoleSpec.Role (e.g. "lead", "worker", "reviewer").
	Role string

	// Scopes are the allowed MCP scopes from AgentRoleSpec.MCPScopes
	// (e.g. ["kms:read", "secrets:read", "orchestrator:read"]).
	Scopes []string

	// OIDCSubject is the OIDC subject claim for this agent's service account.
	OIDCSubject string

	// ClientID is the OAuth2 client ID for this agent's service account.
	ClientID string

	// ClientSecret is the plaintext client secret, short-lived, passed to the
	// agent process via environment variable.
	ClientSecret string
}

// Validate checks that all required fields are present and scopes are well-formed.
func (ai *AgentIdentity) Validate() error {
	if ai.AgentID == "" {
		return &AgentIdentityRequiredError{Field: "agent_id"}
	}
	if ai.ServiceAccountID == "" {
		return &AgentIdentityRequiredError{Field: "service_account_id"}
	}
	if ai.TenantID == "" {
		return &AgentIdentityRequiredError{Field: "tenant_id"}
	}
	if ai.Role == "" {
		return &AgentIdentityRequiredError{Field: "role"}
	}
	if len(ai.Scopes) == 0 {
		return &AgentIdentityRequiredError{Field: "scopes"}
	}
	for _, scope := range ai.Scopes {
		if !IsValidScopeFormat(scope) {
			return &AgentIdentityInvalidScopeError{Scope: scope}
		}
	}
	if ai.ClientID == "" {
		return &AgentIdentityRequiredError{Field: "client_id"}
	}
	return nil
}

// HasScope returns true if the agent identity includes the given scope.
func (ai *AgentIdentity) HasScope(scope string) bool {
	for _, s := range ai.Scopes {
		if s == scope {
			return true
		}
		// Support wildcard matching: "kms:*" matches "kms:read"
		if len(s) > 2 && s[len(s)-1] == '*' {
			prefix := s[:len(s)-1]
			if len(scope) >= len(prefix) && scope[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// HasAnyScope returns true if the agent identity includes any of the given scopes.
func (ai *AgentIdentity) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if ai.HasScope(scope) {
			return true
		}
	}
	return false
}

// IsValidScopeFormat validates that a scope string follows the resource:action
// pattern. Accepts two-segment (e.g. "kms:read") or three-segment (e.g.
// "kms:keys:read") formats. Each segment must start with a letter or wildcard
// and contain only alphanumeric characters, underscores, hyphens, or wildcards.
func IsValidScopeFormat(scope string) bool {
	return scopeFormatRegex.MatchString(scope)
}

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

	// ErrAgentSecurityContextRequired indicates a required field is missing from the security context.
	ErrAgentSecurityContextRequired = errors.New("orchestration: agent security context required field missing")

	// ErrAgentIdentityInvalidScope indicates an invalid scope format in the agent identity.
	ErrAgentIdentityInvalidScope = errors.New("orchestration: agent identity invalid scope format")
)

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

// AgentSecurityContextRequiredError indicates a required field is missing from the security context.
type AgentSecurityContextRequiredError struct {
	Field string
}

// Error implements the error interface.
func (e *AgentSecurityContextRequiredError) Error() string {
	return ErrAgentSecurityContextRequired.Error() + ": " + e.Field
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentSecurityContextRequiredError) Unwrap() error {
	return ErrAgentSecurityContextRequired
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

// AgentIdentity binds an agent to an IAM service account. It carries the
// agent's IAM principal information including scopes derived from the
// workflow's AgentRoleSpec.MCPScopes. The actual provisioning (calling
// OrchestratorAuthService.CreateServiceAccount) happens in the control
// loop's handleInitialize and handleAssign states.
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

// AgentSecurityContext carries security state for a running agent. It is
// populated during the handleAssign control loop state after the agent's
// service account is provisioned and a short-lived token is issued.
type AgentSecurityContext struct {
	// Identity is the agent's IAM identity binding.
	Identity *AgentIdentity

	// AccessToken is the current short-lived OIDC access token.
	AccessToken string

	// TokenExpiry is the Unix timestamp (seconds) when the access token expires.
	TokenExpiry int64

	// CorrelationID is the unique identifier linking all audit events for this agent's job.
	CorrelationID string

	// PolicyIDs are the IAM policy IDs created for this agent's session.
	PolicyIDs []string
}

// Validate checks that all required fields are present in the security context.
func (sc *AgentSecurityContext) Validate() error {
	if sc.Identity == nil {
		return &AgentSecurityContextRequiredError{Field: "identity"}
	}
	if err := sc.Identity.Validate(); err != nil {
		return err
	}
	if sc.AccessToken == "" {
		return &AgentSecurityContextRequiredError{Field: "access_token"}
	}
	if sc.TokenExpiry <= 0 {
		return &AgentSecurityContextRequiredError{Field: "token_expiry"}
	}
	if sc.CorrelationID == "" {
		return &AgentSecurityContextRequiredError{Field: "correlation_id"}
	}
	return nil
}

// IsTokenExpired returns true if the access token has expired based on the
// given current Unix timestamp in seconds.
func (sc *AgentSecurityContext) IsTokenExpired(nowUnix int64) bool {
	return nowUnix >= sc.TokenExpiry
}

// IsValidScopeFormat validates that a scope string follows the resource:action
// pattern. Accepts two-segment (e.g. "kms:read") or three-segment (e.g.
// "kms:keys:read") formats. Each segment must start with a letter or wildcard
// and contain only alphanumeric characters, underscores, hyphens, or wildcards.
func IsValidScopeFormat(scope string) bool {
	return scopeFormatRegex.MatchString(scope)
}

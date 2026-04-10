//go:build iam

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

// Package iam provides Identity and Access Management functionality for QuicBaaS.
// It implements multi-tenant authentication, authorization, and access control with
// support for ABAC policies, OAuth2/OIDC, and WebAuthn.
package iam

import (
	"time"
)

// Tenant represents a logical isolation boundary in QuicBaaS IAM.
// All users, groups, roles, and policies belong to a specific tenant.
type Tenant struct {
	// ID is the unique tenant identifier (URL-safe, lowercase)
	ID string `json:"id" yaml:"id"`

	// Name is the human-readable tenant name
	Name string `json:"name" yaml:"name"`

	// Settings contains tenant-specific configuration
	Settings TenantSettings `json:"settings" yaml:"settings"`

	// Quotas defines resource limits for this tenant
	Quotas TenantQuotas `json:"quotas,omitempty" yaml:"quotas,omitempty"`

	// Created is the tenant creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// TenantSettings contains tenant-specific configuration options.
type TenantSettings struct {
	// MaxUsers is the maximum number of users allowed in this tenant
	MaxUsers int `json:"max_users,omitempty" yaml:"max_users,omitempty"`

	// MFARequired indicates whether multi-factor authentication is mandatory
	// Deprecated: Use MFAPolicy.Required instead
	MFARequired bool `json:"mfa_required" yaml:"mfa_required"`

	// MFAPolicy defines MFA requirements and configuration for this tenant
	MFAPolicy *MFAPolicy `json:"mfa_policy,omitempty" yaml:"mfa_policy,omitempty"`

	// NamedNetworks maps network names to CIDR blocks for policy evaluation
	NamedNetworks map[string][]string `json:"named_networks,omitempty" yaml:"named_networks,omitempty"`

	// SessionTimeout is the default session timeout duration
	SessionTimeout time.Duration `json:"session_timeout,omitempty" yaml:"session_timeout,omitempty"`

	// SessionIdleTimeout is the idle session timeout
	SessionIdleTimeout time.Duration `json:"session_idle_timeout,omitempty" yaml:"session_idle_timeout,omitempty"`

	// TokenTTL is the default access token lifetime
	TokenTTL time.Duration `json:"token_ttl,omitempty" yaml:"token_ttl,omitempty"`

	// RefreshTokenTTL is the refresh token lifetime
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl,omitempty" yaml:"refresh_token_ttl,omitempty"`

	// AuditPolicy defines audit logging configuration for this tenant
	// AuditPolicy (disabled - requires iam build tag) *AuditPolicy `json:"audit_policy,omitempty" yaml:"audit_policy,omitempty"`
}

// TenantQuotas defines resource limits for a tenant.
type TenantQuotas struct {
	// MaxUsers is the maximum number of users
	MaxUsers int `json:"max_users,omitempty" yaml:"max_users,omitempty"`

	// MaxGroups is the maximum number of groups
	MaxGroups int `json:"max_groups,omitempty" yaml:"max_groups,omitempty"`

	// MaxRoles is the maximum number of roles
	MaxRoles int `json:"max_roles,omitempty" yaml:"max_roles,omitempty"`

	// MaxPolicies is the maximum number of policies
	MaxPolicies int `json:"max_policies,omitempty" yaml:"max_policies,omitempty"`

	// MaxApps is the maximum number of OAuth applications
	MaxApps int `json:"max_apps,omitempty" yaml:"max_apps,omitempty"`
}

// User represents an authenticated principal in the IAM system.
type User struct {
	// ID is the unique user identifier
	ID string `json:"id" yaml:"id"`

	// Email is the user's email address (unique within tenant)
	Email string `json:"email" yaml:"email"`

	// Name is the user's display name
	Name string `json:"name" yaml:"name"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles is the list of role IDs assigned to this user
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups is the list of group IDs this user belongs to
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Attributes contains custom user attributes for policy evaluation
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Created is the user creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`

	// LastAuth is the last successful authentication timestamp
	LastAuth *time.Time `json:"last_auth,omitempty" yaml:"last_auth,omitempty"`

	// MFAEnabled indicates whether MFA is enabled for this user
	MFAEnabled bool `json:"mfa_enabled" yaml:"mfa_enabled"`

	// MFAMethod is the MFA method being used (totp, webauthn)
	MFAMethod MFAMethod `json:"mfa_method,omitempty" yaml:"mfa_method,omitempty"`

	// MFAEnrolledAt is when MFA was enrolled
	MFAEnrolledAt *time.Time `json:"mfa_enrolled_at,omitempty" yaml:"mfa_enrolled_at,omitempty"`

	// MFAVerified indicates whether MFA was verified in the current session
	// Deprecated: Use session-level MFA verification instead
	MFAVerified bool `json:"mfa_verified" yaml:"mfa_verified"`

	// MFATimestamp is the last MFA verification time
	MFATimestamp *time.Time `json:"mfa_timestamp,omitempty" yaml:"mfa_timestamp,omitempty"`

	// Suspended indicates whether the user account is suspended
	Suspended bool `json:"suspended,omitempty" yaml:"suspended,omitempty"`

	// Locked indicates whether the user account is locked
	Locked bool `json:"locked,omitempty" yaml:"locked,omitempty"`
}

// Group represents a collection of users for policy targeting.
type Group struct {
	// ID is the unique group identifier within the tenant
	ID string `json:"id" yaml:"id"`

	// Name is the group display name
	Name string `json:"name" yaml:"name"`

	// Description explains the group's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Members is the list of user IDs in this group
	Members []string `json:"members,omitempty" yaml:"members,omitempty"`

	// Created is the group creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// Role represents a named set of permissions.
type Role struct {
	// ID is the unique role identifier within the tenant
	ID string `json:"id" yaml:"id"`

	// Name is the role display name
	Name string `json:"name" yaml:"name"`

	// Description explains the role's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Permissions is the list of permission strings
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// Created is the role creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// Application represents an OAuth2/OIDC client application.
type Application struct {
	// ClientID is the unique OAuth client identifier
	ClientID string `json:"client_id" yaml:"client_id"`

	// ClientSecret is the OAuth client secret (hashed)
	ClientSecret string `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`

	// Name is the application display name
	Name string `json:"name" yaml:"name"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// RedirectURIs is the list of allowed OAuth redirect URIs
	RedirectURIs []string `json:"redirect_uris" yaml:"redirect_uris"`

	// ResourcePrefix is the resource namespace for this application
	ResourcePrefix string `json:"resource_prefix,omitempty" yaml:"resource_prefix,omitempty"`

	// GrantTypes is the list of allowed OAuth grant types
	GrantTypes []string `json:"grant_types" yaml:"grant_types"`

	// ResponseTypes is the list of allowed OAuth response types
	ResponseTypes []string `json:"response_types" yaml:"response_types"`

	// Created is the application creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// Session represents an authenticated user session.
type Session struct {
	// ID is the unique session identifier
	ID string `json:"id" yaml:"id"`

	// UserID is the authenticated user
	UserID string `json:"user_id" yaml:"user_id"`

	// TenantID is the session tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Created is the session creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Expires is the absolute session expiration time
	Expires time.Time `json:"expires" yaml:"expires"`

	// IdleTimeout is the session idle timeout duration
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// LastActivity is the last activity timestamp
	LastActivity time.Time `json:"last_activity" yaml:"last_activity"`

	// MFAVerified indicates whether MFA was verified for this session
	MFAVerified bool `json:"mfa_verified" yaml:"mfa_verified"`

	// AuthMethod is the authentication method used (webauthn, oauth, etc.)
	AuthMethod string `json:"auth_method" yaml:"auth_method"`

	// IPAddress is the client IP address
	IPAddress string `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`

	// UserAgent is the client user agent
	UserAgent string `json:"user_agent,omitempty" yaml:"user_agent,omitempty"`
}

// Token represents an OAuth2 access or refresh token.
type Token struct {
	// JTI is the unique token identifier (JWT ID)
	JTI string `json:"jti" yaml:"jti"`

	// Subject is the user or service ID this token represents
	Subject string `json:"subject" yaml:"subject"`

	// TenantID is the token tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Scopes is the list of granted scopes
	Scopes []string `json:"scopes" yaml:"scopes"`

	// Expires is the token expiration timestamp
	Expires time.Time `json:"expires" yaml:"expires"`

	// Issued is the token issuance timestamp
	Issued time.Time `json:"issued" yaml:"issued"`

	// TokenType is the type of token (access, refresh, id)
	TokenType string `json:"token_type" yaml:"token_type"`

	// Revoked indicates whether the token has been revoked
	Revoked bool `json:"revoked,omitempty" yaml:"revoked,omitempty"`

	// ClientID is the OAuth client that requested this token
	ClientID string `json:"client_id,omitempty" yaml:"client_id,omitempty"`
}

// Policy represents an ABAC policy document.
type Policy struct {
	// APIVersion is the policy schema version
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`

	// Kind is the resource type (always "Policy")
	Kind string `json:"kind" yaml:"kind"`

	// Metadata contains policy metadata
	Metadata PolicyMetadata `json:"metadata" yaml:"metadata"`

	// Spec contains the policy specification
	Spec PolicySpec `json:"spec" yaml:"spec"`
}

// PolicyMetadata contains policy identification and description.
type PolicyMetadata struct {
	// Name is the unique policy name within the tenant
	Name string `json:"name" yaml:"name"`

	// Tenant is the owning tenant ID
	Tenant string `json:"tenant" yaml:"tenant"`

	// Description explains the policy's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Labels are arbitrary key-value labels
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Priority determines evaluation order (higher = first)
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Created is the policy creation timestamp
	Created time.Time `json:"created,omitempty" yaml:"created,omitempty"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated,omitempty" yaml:"updated,omitempty"`
}

// PolicySpec contains the policy specification.
type PolicySpec struct {
	// Subjects defines who this policy applies to
	Subjects SubjectSelector `json:"subjects" yaml:"subjects"`

	// Variables defines reusable expressions
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`

	// Rules defines the authorization rules
	Rules []Rule `json:"rules" yaml:"rules"`
}

// SubjectSelector defines criteria for matching subjects.
type SubjectSelector struct {
	// Users is the list of specific user IDs
	Users []string `json:"users,omitempty" yaml:"users,omitempty"`

	// Groups is the list of group IDs
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Roles is the list of role IDs
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Services is the list of service account IDs
	Services []string `json:"services,omitempty" yaml:"services,omitempty"`

	// Match contains attribute-based matching criteria
	Match map[string]interface{} `json:"match,omitempty" yaml:"match,omitempty"`

	// Authenticated matches all authenticated users
	Authenticated bool `json:"authenticated,omitempty" yaml:"authenticated,omitempty"`

	// Any matches all subjects including anonymous
	Any bool `json:"any,omitempty" yaml:"any,omitempty"`
}

// Rule represents a single authorization rule within a policy.
type Rule struct {
	// Name is the unique rule identifier within the policy
	Name string `json:"name" yaml:"name"`

	// Effect is the authorization decision (allow or deny)
	Effect Effect `json:"effect" yaml:"effect"`

	// Priority determines rule evaluation order (higher = first)
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Actions is the list of operations this rule applies to
	Actions []string `json:"actions" yaml:"actions"`

	// Resources is the list of resource patterns this rule applies to
	Resources []string `json:"resources" yaml:"resources"`

	// When contains conditions that must all be true
	When []Condition `json:"when,omitempty" yaml:"when,omitempty"`

	// Unless contains conditions that must all be false
	Unless []Condition `json:"unless,omitempty" yaml:"unless,omitempty"`
}

// Effect represents the authorization decision.
type Effect string

const (
	// EffectAllow grants access.
	EffectAllow Effect = "allow"

	// EffectDeny denies access.
	EffectDeny Effect = "deny"
)

// Condition represents a policy evaluation condition.
type Condition struct {
	// Expression is the condition expression string
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`

	// And contains sub-conditions that must all be true
	And []Condition `json:"and,omitempty" yaml:"and,omitempty"`

	// Or contains sub-conditions where at least one must be true
	Or []Condition `json:"or,omitempty" yaml:"or,omitempty"`

	// Not negates the condition
	Not *Condition `json:"not,omitempty" yaml:"not,omitempty"`
}

// CheckRequest represents an authorization check request.
type CheckRequest struct {
	// Subject is the principal requesting access
	Subject Subject `json:"subject" yaml:"subject"`

	// Action is the operation being performed
	Action string `json:"action" yaml:"action"`

	// Resource is the target resource identifier
	Resource string `json:"resource" yaml:"resource"`

	// Context contains additional request context
	Context map[string]interface{} `json:"context,omitempty" yaml:"context,omitempty"`
}

// Subject represents a principal in an authorization check.
type Subject struct {
	// ID is the subject identifier
	ID string `json:"id" yaml:"id"`

	// Name is the subject's display name
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Email is the subject's email address
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Tenant is the subject's tenant identifier (alias for TenantID for compatibility)
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty"`

	// TenantID is the subject's tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles is the list of role IDs
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups is the list of group IDs
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Attributes contains custom attributes
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Attrs is an alias for Attributes for backward compatibility
	Attrs map[string]interface{} `json:"attrs,omitempty" yaml:"attrs,omitempty"`

	// Authenticated indicates whether the subject is authenticated
	Authenticated bool `json:"authenticated" yaml:"authenticated"`

	// MFAVerified indicates whether MFA was verified
	MFAVerified bool `json:"mfa_verified,omitempty" yaml:"mfa_verified,omitempty"`

	// MFATimestamp is the last MFA verification time
	MFATimestamp *time.Time `json:"mfa_timestamp,omitempty" yaml:"mfa_timestamp,omitempty"`

	// LastLogin is the last login timestamp
	LastLogin *time.Time `json:"last_login,omitempty" yaml:"last_login,omitempty"`

	// PasswordChange is the last password change timestamp
	PasswordChange *time.Time `json:"password_change,omitempty" yaml:"password_change,omitempty"`
}

// CheckResponse represents an authorization check response.
type CheckResponse struct {
	// Allowed indicates whether access is granted
	Allowed bool `json:"allowed" yaml:"allowed"`

	// MatchedPolicy is the name of the policy that matched
	MatchedPolicy string `json:"matched_policy,omitempty" yaml:"matched_policy,omitempty"`

	// MatchedRule is the name of the rule that matched
	MatchedRule string `json:"matched_rule,omitempty" yaml:"matched_rule,omitempty"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`

	// Reason provides additional context for the decision
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// PolicySimulationRequest represents a request to simulate policy evaluation.
type PolicySimulationRequest struct {
	// Policy is the policy to simulate (may be unsaved draft)
	Policy *Policy `json:"policy" yaml:"policy"`

	// CheckRequest is the authorization check to simulate
	CheckRequest *CheckRequest `json:"check_request" yaml:"check_request"`

	// IncludeTrace enables detailed evaluation tracing
	IncludeTrace bool `json:"include_trace,omitempty" yaml:"include_trace,omitempty"`
}

// PolicySimulationResult represents the result of a policy simulation.
type PolicySimulationResult struct {
	// Allowed indicates whether access would be granted
	Allowed bool `json:"allowed" yaml:"allowed"`

	// MatchedPolicy is the name of the policy that matched
	MatchedPolicy string `json:"matched_policy,omitempty" yaml:"matched_policy,omitempty"`

	// MatchedRule is the name of the rule that matched
	MatchedRule string `json:"matched_rule,omitempty" yaml:"matched_rule,omitempty"`

	// EvaluationSteps provides a trace of evaluation steps (when IncludeTrace=true)
	EvaluationSteps []EvaluationStep `json:"evaluation_steps,omitempty" yaml:"evaluation_steps,omitempty"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`

	// Reason provides additional context for the decision
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// EvaluationStep represents a single step in policy evaluation.
type EvaluationStep struct {
	// StepNumber is the ordinal step number
	StepNumber int `json:"step_number" yaml:"step_number"`

	// PolicyName is the policy being evaluated
	PolicyName string `json:"policy_name" yaml:"policy_name"`

	// RuleName is the rule being evaluated (if applicable)
	RuleName string `json:"rule_name,omitempty" yaml:"rule_name,omitempty"`

	// Action describes the evaluation action
	Action string `json:"action" yaml:"action"`

	// Result is the outcome of this step
	Result bool `json:"result" yaml:"result"`

	// Details provides additional context
	Details string `json:"details,omitempty" yaml:"details,omitempty"`
}

// EffectivePermissionsRequest represents a request to calculate effective permissions.
type EffectivePermissionsRequest struct {
	// Subject is the subject to calculate permissions for
	Subject *Subject `json:"subject" yaml:"subject"`

	// ResourcePatterns are resource patterns to check (e.g., "orders://*")
	ResourcePatterns []string `json:"resource_patterns" yaml:"resource_patterns"`

	// Actions are specific actions to check (if empty, all actions are checked)
	Actions []string `json:"actions,omitempty" yaml:"actions,omitempty"`

	// IncludeDenyDetails includes details about denied permissions
	IncludeDenyDetails bool `json:"include_deny_details,omitempty" yaml:"include_deny_details,omitempty"`
}

// EffectivePermissionsResult represents the calculated effective permissions.
type EffectivePermissionsResult struct {
	// Subject is the subject these permissions are for
	Subject *Subject `json:"subject" yaml:"subject"`

	// Permissions maps resource patterns to their permission sets
	Permissions map[string]*PermissionSet `json:"permissions" yaml:"permissions"`

	// TotalPoliciesEvaluated is the number of policies checked
	TotalPoliciesEvaluated int `json:"total_policies_evaluated" yaml:"total_policies_evaluated"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`
}

// PermissionSet represents allowed and denied actions for a resource.
type PermissionSet struct {
	// Allowed are actions that are permitted
	Allowed []string `json:"allowed" yaml:"allowed"`

	// Denied are actions that are explicitly denied
	Denied []string `json:"denied,omitempty" yaml:"denied,omitempty"`

	// Sources maps actions to the policies that grant/deny them
	Sources map[string][]string `json:"sources,omitempty" yaml:"sources,omitempty"`
}

// Credential represents a WebAuthn credential.
type Credential struct {
	// ID is the credential identifier
	ID string `json:"id" yaml:"id"`

	// UserID is the owning user
	UserID string `json:"user_id" yaml:"user_id"`

	// TenantID is the credential tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// PublicKey is the credential public key
	PublicKey []byte `json:"public_key" yaml:"public_key"`

	// AttestationType is the attestation type
	AttestationType string `json:"attestation_type" yaml:"attestation_type"`

	// AAGUID is the authenticator AAGUID
	AAGUID []byte `json:"aaguid,omitempty" yaml:"aaguid,omitempty"`

	// SignCount is the signature counter
	SignCount uint32 `json:"sign_count" yaml:"sign_count"`

	// Created is the credential creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// LastUsed is the last usage timestamp
	LastUsed *time.Time `json:"last_used,omitempty" yaml:"last_used,omitempty"`
}

// Validate validates a Tenant for correctness.
func (t *Tenant) Validate() error {
	if t.ID == "" {
		return NewIAMError(ErrTenantInvalid, "iam.tenant.validate", "tenant ID is required", nil)
	}
	if t.Name == "" {
		return NewIAMError(ErrTenantInvalid, "iam.tenant.validate", "tenant name is required", nil)
	}
	return nil
}

// Validate validates a User for correctness.
func (u *User) Validate() error {
	if u.ID == "" {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user ID is required", nil)
	}
	if u.Email == "" {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user email is required", nil)
	}
	if u.TenantID == "" {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user tenant ID is required", nil)
	}
	return nil
}

// Validate validates a Group for correctness.
func (g *Group) Validate() error {
	if g.ID == "" {
		return NewIAMError(ErrGroupInvalid, "iam.group.validate", "group ID is required", nil)
	}
	if g.Name == "" {
		return NewIAMError(ErrGroupInvalid, "iam.group.validate", "group name is required", nil)
	}
	if g.TenantID == "" {
		return NewIAMError(ErrGroupInvalid, "iam.group.validate", "group tenant ID is required", nil)
	}
	return nil
}

// Validate validates a Role for correctness.
func (r *Role) Validate() error {
	if r.ID == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role ID is required", nil)
	}
	if r.Name == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role name is required", nil)
	}
	if r.TenantID == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role tenant ID is required", nil)
	}
	return nil
}

// Validate validates a Policy for correctness.
func (p *Policy) Validate() error {
	if p.APIVersion == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy apiVersion is required", nil)
	}
	if p.Kind != "Policy" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy kind must be 'Policy'", nil)
	}
	if p.Metadata.Name == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy name is required", nil)
	}
	if p.Metadata.Tenant == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy tenant is required", nil)
	}
	if len(p.Spec.Rules) == 0 {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy must have at least one rule", nil)
	}
	for i, rule := range p.Spec.Rules {
		if err := rule.Validate(); err != nil {
			return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "invalid rule at index", err).
				WithContext("index", i).
				WithContext("rule_name", rule.Name)
		}
	}
	return nil
}

// Validate validates a Rule for correctness.
func (r *Rule) Validate() error {
	if r.Name == "" {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule name is required", nil)
	}
	if r.Effect != EffectAllow && r.Effect != EffectDeny {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule effect must be 'allow' or 'deny'", nil)
	}
	if len(r.Actions) == 0 {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule must have at least one action", nil)
	}
	if len(r.Resources) == 0 {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule must have at least one resource", nil)
	}
	return nil
}

// IsExpired checks if a session has expired.
func (s *Session) IsExpired() bool {
	now := time.Now()
	if now.After(s.Expires) {
		return true
	}
	if s.IdleTimeout > 0 && now.Sub(s.LastActivity) > s.IdleTimeout {
		return true
	}
	return false
}

// IsExpired checks if a token has expired.
func (t *Token) IsExpired() bool {
	return time.Now().After(t.Expires)
}

// ========== Compiled Policy Types for ABAC Engine ==========

// CompiledPolicy is the compiled form of a policy for fast evaluation.
type CompiledPolicy struct {
	Name           string
	Priority       int
	SubjectMatcher *SubjectMatcher
	Rules          []*CompiledRule
	Variables      map[string]interface{}
}

// CompiledRule represents a compiled authorization rule.
type CompiledRule struct {
	Name         string
	Effect       Effect
	Priority     int
	Actions      []string
	ResourceTrie *ResourceTrie
	Conditions   []CompiledCondition
	Unless       []CompiledCondition
}

// SubjectMatcher matches subjects against policy selectors.
type SubjectMatcher struct {
	Users         map[string]bool
	Groups        map[string]bool
	Roles         map[string]bool
	Services      map[string]bool
	Matchers      []AttributeMatcher
	Authenticated bool
	Any           bool
}

// AttributeMatcher matches subject attributes.
type AttributeMatcher interface {
	Match(subject *Subject) bool
}

// CompiledCondition represents a compiled condition expression.
type CompiledCondition interface {
	Evaluate(ctx *EvalContext) bool
}

// EvalContext provides context for condition evaluation.
type EvalContext struct {
	Subject   *Subject
	Action    string
	Resource  string
	Request   map[string]interface{}
	Time      time.Time
	Variables map[string]interface{}
}

// ResourceTrie is a prefix tree for efficient resource pattern matching.
type ResourceTrie struct {
	root *trieNode
}

type trieNode struct {
	children     map[string]*trieNode
	isWildcard   bool
	isMultiLevel bool
	isVariable   bool
	variableName string
	value        interface{}
	isTerminal   bool
}

// PolicyEngineOptions configures the policy engine.
type PolicyEngineOptions struct {
	Storage       PolicyStorage
	Cache         *PolicyCache
	CacheTTL      time.Duration
	MaxCacheSize  int
	EnableMetrics bool
}

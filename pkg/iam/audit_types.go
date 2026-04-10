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

package iam

import (
	"time"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	// ID is the unique event identifier
	ID string `json:"id"`

	// TenantID is the tenant this event belongs to
	TenantID string `json:"tenant_id"`

	// Timestamp is when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// EventType categorizes the event
	EventType AuditEventType `json:"event_type"`

	// Category is the high-level category
	Category AuditCategory `json:"category"`

	// Actor contains information about who performed the action
	Actor *AuditActor `json:"actor,omitempty"`

	// Resource contains information about the affected resource
	Resource *AuditResource `json:"resource,omitempty"`

	// Action is the specific action performed
	Action string `json:"action"`

	// Outcome indicates success/failure/denied
	Outcome AuditOutcome `json:"outcome"`

	// Details contains additional context-specific information
	Details map[string]interface{} `json:"details,omitempty"`

	// IPAddress is the client IP address
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the client user agent
	UserAgent string `json:"user_agent,omitempty"`

	// ImpersonationSessionID links this event to an impersonation session.
	// When set, this event occurred while an admin was impersonating another user.
	ImpersonationSessionID string `json:"impersonation_session_id,omitempty"`

	// CorrelationID groups related audit events together.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// AuditEventType represents the type of audit event.
type AuditEventType string

const (
	// AuditEventAuthentication represents authentication attempts
	AuditEventAuthentication AuditEventType = "authentication"

	// AuditEventAuthorization represents authorization checks
	AuditEventAuthorization AuditEventType = "authorization"

	// AuditEventUserManagement represents user CRUD operations
	AuditEventUserManagement AuditEventType = "user_management"

	// AuditEventGroupManagement represents group CRUD operations
	AuditEventGroupManagement AuditEventType = "group_management"

	// AuditEventRoleManagement represents role CRUD operations
	AuditEventRoleManagement AuditEventType = "role_management"

	// AuditEventPolicyChange represents policy modifications
	AuditEventPolicyChange AuditEventType = "policy_change"

	// AuditEventMFAEvent represents MFA-related events
	AuditEventMFAEvent AuditEventType = "mfa"

	// AuditEventSessionEvent represents session lifecycle events
	AuditEventSessionEvent AuditEventType = "session"

	// AuditEventTenantEvent represents tenant operations
	AuditEventTenantEvent AuditEventType = "tenant"

	// AuditEventAppEvent represents application/OAuth operations
	AuditEventAppEvent AuditEventType = "application"
)

// AuditCategory represents high-level event categories.
type AuditCategory string

const (
	// AuditCategoryAuthentication for authentication events
	AuditCategoryAuthentication AuditCategory = "authentication"

	// AuditCategoryAuthorization for authorization events
	AuditCategoryAuthorization AuditCategory = "authorization"

	// AuditCategoryAdministration for administrative changes
	AuditCategoryAdministration AuditCategory = "administration"

	// AuditCategorySecurity for security-related events
	AuditCategorySecurity AuditCategory = "security"
)

// AuditOutcome represents the result of an audited action.
type AuditOutcome string

const (
	// AuditOutcomeSuccess indicates the action succeeded
	AuditOutcomeSuccess AuditOutcome = "success"

	// AuditOutcomeFailure indicates the action failed
	AuditOutcomeFailure AuditOutcome = "failure"

	// AuditOutcomeDenied indicates the action was denied by policy
	AuditOutcomeDenied AuditOutcome = "denied"
)

// AuditActor represents the entity that performed an action.
type AuditActor struct {
	// UserID is the user identifier
	UserID string `json:"user_id,omitempty"`

	// Username is the username
	Username string `json:"username,omitempty"`

	// Email is the user's email
	Email string `json:"email,omitempty"`

	// SessionID is the session identifier
	SessionID string `json:"session_id,omitempty"`

	// ClientID is the OAuth client ID for service actors
	ClientID string `json:"client_id,omitempty"`

	// ImpersonatingUserID is set when this action is performed by an admin
	// impersonating another user. This is the admin's actual user ID.
	ImpersonatingUserID string `json:"impersonating_user_id,omitempty"`

	// ImpersonatingUsername is the admin's username when impersonating
	ImpersonatingUsername string `json:"impersonating_username,omitempty"`
}

// AuditResource represents a resource affected by an action.
type AuditResource struct {
	// Type is the resource type (user, group, role, policy, tenant, app)
	Type string `json:"type"`

	// ID is the resource identifier
	ID string `json:"id"`

	// Name is the resource name
	Name string `json:"name,omitempty"`
}

// AuditFilter contains criteria for querying audit events.
type AuditFilter struct {
	// TenantID filters by tenant
	TenantID string `json:"tenant_id"`

	// EventTypes filters by event types
	EventTypes []AuditEventType `json:"event_types,omitempty"`

	// Categories filters by categories
	Categories []AuditCategory `json:"categories,omitempty"`

	// Outcomes filters by outcomes
	Outcomes []AuditOutcome `json:"outcomes,omitempty"`

	// ActorID filters by actor user ID
	ActorID string `json:"actor_id,omitempty"`

	// ResourceID filters by resource ID
	ResourceID string `json:"resource_id,omitempty"`

	// StartTime filters events after this time
	StartTime *time.Time `json:"start_time,omitempty"`

	// EndTime filters events before this time
	EndTime *time.Time `json:"end_time,omitempty"`

	// Limit limits the number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the pagination offset
	Offset int `json:"offset,omitempty"`
}

// Validate validates the audit event for correctness.
func (e *AuditEvent) Validate() error {
	if e.ID == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "event ID is required", nil)
	}
	if e.TenantID == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "tenant ID is required", nil)
	}
	if e.EventType == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "event type is required", nil)
	}
	if e.Category == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "category is required", nil)
	}
	if e.Action == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "action is required", nil)
	}
	if e.Outcome == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "outcome is required", nil)
	}
	if e.Timestamp.IsZero() {
		return NewIAMError(ErrAuditInvalid, "iam.audit.validate", "timestamp is required", nil)
	}
	return nil
}

// Validate validates the audit filter for correctness.
func (f *AuditFilter) Validate() error {
	if f.TenantID == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.filter", "tenant ID is required", nil)
	}
	if f.Limit < 0 {
		return NewIAMError(ErrAuditInvalid, "iam.audit.filter", "limit cannot be negative", nil)
	}
	if f.Offset < 0 {
		return NewIAMError(ErrAuditInvalid, "iam.audit.filter", "offset cannot be negative", nil)
	}
	if f.StartTime != nil && f.EndTime != nil && f.EndTime.Before(*f.StartTime) {
		return NewIAMError(ErrAuditInvalid, "iam.audit.filter", "end time must be after start time", nil)
	}
	return nil
}

// ApplyDefaults applies default values to the filter.
func (f *AuditFilter) ApplyDefaults() {
	if f.Limit == 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}
}

// AuditPolicy defines tenant-level audit logging requirements.
type AuditPolicy struct {
	// Enabled indicates whether audit logging is enabled for this tenant
	Enabled bool `json:"enabled" yaml:"enabled"`

	// RetentionDays is the number of days to retain audit logs
	RetentionDays int `json:"retention_days" yaml:"retention_days"`

	// LogAuthEvents enables logging of authentication events
	LogAuthEvents bool `json:"log_auth_events" yaml:"log_auth_events"`

	// LogAdminEvents enables logging of administrative actions
	LogAdminEvents bool `json:"log_admin_events" yaml:"log_admin_events"`

	// LogPolicyChecks enables logging of policy evaluation checks
	LogPolicyChecks bool `json:"log_policy_checks" yaml:"log_policy_checks"`
}

// ApplyDefaults applies default values to the audit policy.
func (p *AuditPolicy) ApplyDefaults() {
	if p.RetentionDays == 0 {
		p.RetentionDays = 90 // 90 days default
	}
	// If no specific settings, enable all logging by default when enabled
	if !p.LogAuthEvents && !p.LogAdminEvents && !p.LogPolicyChecks {
		p.LogAuthEvents = true
		p.LogAdminEvents = true
		p.LogPolicyChecks = true
	}
}

// Validate validates the audit policy for correctness.
func (p *AuditPolicy) Validate() error {
	if p.RetentionDays < 0 {
		return NewIAMError(ErrAuditInvalid, "iam.audit_policy.validate", "retention_days cannot be negative", nil)
	}
	if p.RetentionDays > 3650 { // 10 years max
		return NewIAMError(ErrAuditInvalid, "iam.audit_policy.validate", "retention_days cannot exceed 3650 days (10 years)", nil)
	}
	return nil
}

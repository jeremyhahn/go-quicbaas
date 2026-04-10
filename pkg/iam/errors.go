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
	"strconv"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// IAMMessageError is a simple typed error for IAM sentinel error inner messages.
type IAMMessageError struct {
	Message string
}

// Error implements the error interface.
func (e *IAMMessageError) Error() string {
	return e.Message
}

// IAMContextError is a typed error for IAM constructor functions that include context.
type IAMContextError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *IAMContextError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *IAMContextError) Unwrap() error {
	return e.Cause
}

// IAM-specific error types following QuicBaaS error conventions.
var (
	// ErrTenantNotFound is returned when a tenant is not found.
	ErrTenantNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant not found"},
	}

	// ErrTenantAlreadyExists is returned when attempting to create a duplicate tenant.
	ErrTenantAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant already exists"},
	}

	// ErrInvalidTenantID is returned when a tenant ID is invalid.
	ErrInvalidTenantID = &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant ID"},
	}

	// ErrInvalidTenantName is returned when a tenant name is invalid.
	ErrInvalidTenantName = &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant name"},
	}

	// ErrQuotaExceeded is returned when a quota limit is exceeded.
	ErrQuotaExceeded = &dberrors.DragonError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.quota",
		Err:  &IAMMessageError{Message: "quota exceeded"},
	}

	// ErrTenantSuspended is returned when operating on a suspended tenant.
	ErrTenantSuspended = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant is suspended"},
	}

	// ErrTenantDeleted is returned when operating on a deleted tenant.
	ErrTenantDeleted = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant is deleted"},
	}

	// ErrTenantInvalid is returned when tenant validation fails.
	ErrTenantInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant"},
	}

	// ErrUserInvalid is returned when user validation fails.
	ErrUserInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "invalid user"},
	}

	// ErrGroupInvalid is returned when group validation fails.
	ErrGroupInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.group",
		Err:  &IAMMessageError{Message: "invalid group"},
	}

	// ErrRoleInvalid is returned when role validation fails.
	ErrRoleInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.role",
		Err:  &IAMMessageError{Message: "invalid role"},
	}

	// ErrPolicyInvalid is returned when policy validation fails.
	ErrPolicyInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.policy",
		Err:  &IAMMessageError{Message: "invalid policy"},
	}

	// ErrRuleInvalid is returned when rule validation fails.
	ErrRuleInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.rule",
		Err:  &IAMMessageError{Message: "invalid rule"},
	}

	// ErrPolicyNotFound is returned when a policy is not found.
	ErrPolicyNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.policy",
		Err:  &IAMMessageError{Message: "policy not found"},
	}

	// ErrPolicyCompilationFailed is returned when policy compilation fails.
	ErrPolicyCompilationFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.compile",
		Err:  &IAMMessageError{Message: "policy compilation failed"},
	}

	// ErrPolicyEvaluationFailed is returned when policy evaluation fails.
	ErrPolicyEvaluationFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.evaluate",
		Err:  &IAMMessageError{Message: "policy evaluation failed"},
	}

	// ErrPolicySyncFailed is returned when policy sync fails.
	ErrPolicySyncFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.sync",
		Err:  &IAMMessageError{Message: "policy sync failed"},
	}

	// ErrPEPClosed is returned when operating on a closed PEP.
	ErrPEPClosed = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.pep",
		Err:  &IAMMessageError{Message: "PEP is closed"},
	}

	// ErrInvalidCheckRequest is returned when a check request is invalid.
	ErrInvalidCheckRequest = &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.pep.check",
		Err:  &IAMMessageError{Message: "invalid check request"},
	}

	// ErrUserNotFound is returned when a user is not found.
	ErrUserNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "user not found"},
	}

	// ErrUserAlreadyExists is returned when attempting to create a user that already exists.
	ErrUserAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "user already exists"},
	}

	// ErrEmailAlreadyExists is returned when attempting to create a user with a duplicate email.
	ErrEmailAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "email already exists"},
	}

	// ErrGroupNotFound is returned when a group is not found.
	ErrGroupNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.group",
		Err:  &IAMMessageError{Message: "group not found"},
	}

	// ErrGroupAlreadyExists is returned when attempting to create a group that already exists.
	ErrGroupAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.group",
		Err:  &IAMMessageError{Message: "group already exists"},
	}

	// ErrRoleAlreadyExists is returned when attempting to create a role that already exists.
	ErrRoleAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.role",
		Err:  &IAMMessageError{Message: "role already exists"},
	}

	// ErrUserAlreadyInGroup is returned when adding a user to a group they're already in.
	ErrUserAlreadyInGroup = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.group.membership",
		Err:  &IAMMessageError{Message: "user already in group"},
	}

	// ErrUserNotInGroup is returned when removing a user from a group they're not in.
	ErrUserNotInGroup = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.group.membership",
		Err:  &IAMMessageError{Message: "user not in group"},
	}

	// ErrRoleNotFound is returned when a role is not found.
	ErrRoleNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.role",
		Err:  &IAMMessageError{Message: "role not found"},
	}

	// ErrRoleAlreadyAssigned is returned when assigning a role that's already assigned.
	ErrRoleAlreadyAssigned = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.role.assignment",
		Err:  &IAMMessageError{Message: "role already assigned"},
	}

	// ErrRoleNotAssigned is returned when revoking a role that's not assigned.
	ErrRoleNotAssigned = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.role.assignment",
		Err:  &IAMMessageError{Message: "role not assigned"},
	}

	// ErrCredentialInvalid is returned when credential validation fails.
	ErrCredentialInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.webauthn.credential",
		Err:  &IAMMessageError{Message: "invalid credential"},
	}

	// ErrCredentialNotFound is returned when a credential is not found.
	ErrCredentialNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.credential",
		Err:  &IAMMessageError{Message: "credential not found"},
	}

	// ErrChallengeInvalid is returned when challenge validation fails.
	ErrChallengeInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "invalid challenge"},
	}

	// ErrChallengeNotFound is returned when a challenge is not found.
	ErrChallengeNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "challenge not found"},
	}

	// ErrChallengeExpired is returned when a challenge has expired.
	ErrChallengeExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "challenge expired"},
	}

	// ErrWebAuthnRegistrationFailed is returned when WebAuthn registration fails.
	ErrWebAuthnRegistrationFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.webauthn.registration",
		Err:  &IAMMessageError{Message: "WebAuthn registration failed"},
	}

	// ErrWebAuthnAuthenticationFailed is returned when WebAuthn authentication fails.
	ErrWebAuthnAuthenticationFailed = &dberrors.DragonError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.webauthn.authentication",
		Err:  &IAMMessageError{Message: "WebAuthn authentication failed"},
	}

	// MFA-specific errors.

	// ErrMFAInvalid is returned when MFA validation fails.
	ErrMFAInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "invalid MFA configuration"},
	}

	// ErrMFANotEnrolled is returned when MFA is not enrolled.
	ErrMFANotEnrolled = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA not enrolled"},
	}

	// ErrMFAAlreadyEnrolled is returned when MFA is already enrolled.
	ErrMFAAlreadyEnrolled = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA already enrolled"},
	}

	// ErrMFAEnrollmentNotFound is returned when an enrollment is not found.
	ErrMFAEnrollmentNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa.enrollment",
		Err:  &IAMMessageError{Message: "MFA enrollment not found"},
	}

	// ErrMFAEnrollmentExpired is returned when an enrollment has expired.
	ErrMFAEnrollmentExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.enrollment",
		Err:  &IAMMessageError{Message: "MFA enrollment expired"},
	}

	// ErrMFAChallengeNotFound is returned when an MFA challenge is not found.
	ErrMFAChallengeNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa.challenge",
		Err:  &IAMMessageError{Message: "MFA challenge not found"},
	}

	// ErrMFAChallengeExpired is returned when an MFA challenge has expired.
	ErrMFAChallengeExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.challenge",
		Err:  &IAMMessageError{Message: "MFA challenge expired"},
	}

	// ErrMFACodeInvalid is returned when an MFA code is invalid.
	ErrMFACodeInvalid = &dberrors.DragonError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.mfa.verify",
		Err:  &IAMMessageError{Message: "invalid MFA code"},
	}

	// ErrMFAMaxAttemptsExceeded is returned when max verification attempts are exceeded.
	ErrMFAMaxAttemptsExceeded = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.verify",
		Err:  &IAMMessageError{Message: "maximum MFA verification attempts exceeded"},
	}

	// ErrMFARequired is returned when MFA is required but not verified.
	ErrMFARequired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA verification required"},
	}

	// ErrMFABackupCodeInvalid is returned when a backup code is invalid.
	ErrMFABackupCodeInvalid = &dberrors.DragonError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.mfa.backup",
		Err:  &IAMMessageError{Message: "invalid backup code"},
	}

	// ErrMFABackupCodesExhausted is returned when all backup codes are used.
	ErrMFABackupCodesExhausted = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.backup",
		Err:  &IAMMessageError{Message: "all backup codes have been used"},
	}

	// ErrMFAMethodNotAllowed is returned when an MFA method is not allowed.
	ErrMFAMethodNotAllowed = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA method not allowed"},
	}

	// Session-specific errors.

	// ErrSessionInvalid is returned when session validation fails.
	ErrSessionInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "invalid session"},
	}

	// ErrSessionNotFound is returned when a session is not found.
	ErrSessionNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session not found"},
	}

	// ErrSessionExpired is returned when a session has expired.
	ErrSessionExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session expired"},
	}

	// ErrSessionLimitExceeded is returned when max concurrent sessions exceeded.
	ErrSessionLimitExceeded = &dberrors.DragonError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session limit exceeded"},
	}

	// ErrSessionRevoked is returned when a session has been revoked.
	ErrSessionRevoked = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session revoked"},
	}
	// Audit-specific errors.

	// ErrAuditInvalid is returned when audit validation fails.
	ErrAuditInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "invalid audit event"},
	}

	// ErrAuditEventNotFound is returned when an audit event is not found.
	ErrAuditEventNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "audit event not found"},
	}

	// ErrAuditStorageClosed is returned when operating on closed audit storage.
	ErrAuditStorageClosed = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "audit storage is closed"},
	}

	// Delegated Admin errors.

	// ErrAdminAssignmentNotFound is returned when an admin assignment is not found.
	ErrAdminAssignmentNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin assignment not found"},
	}

	// ErrAdminAssignmentInvalid is returned when admin assignment validation fails.
	ErrAdminAssignmentInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "invalid admin assignment"},
	}

	// ErrAdminAssignmentExpired is returned when an admin assignment has expired.
	ErrAdminAssignmentExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin assignment expired"},
	}

	// ErrAdminPermissionDenied is returned when admin permission is denied.
	ErrAdminPermissionDenied = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin permission denied"},
	}

	// ErrImpersonationNotAllowed is returned when impersonation is not allowed.
	ErrImpersonationNotAllowed = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation not allowed"},
	}

	// ErrImpersonationSessionNotFound is returned when an impersonation session is not found.
	ErrImpersonationSessionNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation session not found"},
	}

	// ErrImpersonationSessionExpired is returned when an impersonation session has expired.
	ErrImpersonationSessionExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation session expired"},
	}

	// GDPR Compliance errors.

	// ErrConsentNotFound is returned when a consent record is not found.
	ErrConsentNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "consent not found"},
	}

	// ErrConsentInvalid is returned when consent validation fails.
	ErrConsentInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "invalid consent"},
	}

	// ErrConsentExpired is returned when a consent has expired.
	ErrConsentExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "consent expired"},
	}

	// ErrDataExportNotFound is returned when a data export request is not found.
	ErrDataExportNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export request not found"},
	}

	// ErrDataExportInvalid is returned when data export validation fails.
	ErrDataExportInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "invalid data export request"},
	}

	// ErrDataExportPending is returned when trying to re-request a pending export.
	ErrDataExportPending = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export already pending"},
	}

	// ErrDataExportExpired is returned when an export has expired.
	ErrDataExportExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export expired"},
	}

	// ErrDeletionNotFound is returned when a deletion request is not found.
	ErrDeletionNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion request not found"},
	}

	// ErrDeletionInvalid is returned when deletion validation fails.
	ErrDeletionInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "invalid deletion request"},
	}

	// ErrDeletionPending is returned when trying to re-request a pending deletion.
	ErrDeletionPending = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion request already pending"},
	}

	// ErrDeletionInProgress is returned when deletion is already in progress.
	ErrDeletionInProgress = &dberrors.DragonError{
		Code: dberrors.ErrConflict,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion already in progress"},
	}

	// ErrDeletionCancelled is returned when deletion has been cancelled.
	ErrDeletionCancelled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion was cancelled"},
	}

	// ErrGracePeriodActive is returned when trying to process during grace period.
	ErrGracePeriodActive = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "grace period still active"},
	}
)

// CompilationFailedError is returned when policy compilation fails.
type CompilationFailedError struct {
	Policy string
	Reason string
}

// Error implements the error interface.
func (e *CompilationFailedError) Error() string {
	return "compilation failed for policy '" + e.Policy + "': " + e.Reason
}

// InvalidPolicyError is returned when a policy is invalid.
type InvalidPolicyError struct {
	TenantID string
	Name     string
	Reason   string
}

// Error implements the error interface.
func (e *InvalidPolicyError) Error() string {
	if e.Name != "" {
		return "invalid policy '" + e.Name + "': " + e.Reason
	}
	return "invalid policy: " + e.Reason
}

// PolicyExistsError is returned when a policy already exists.
type PolicyExistsError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *PolicyExistsError) Error() string {
	return "policy '" + e.Name + "' already exists in tenant '" + e.TenantID + "'"
}

// PolicyNotFoundTypeError is returned when a policy is not found.
type PolicyNotFoundTypeError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *PolicyNotFoundTypeError) Error() string {
	return "policy '" + e.Name + "' not found in tenant '" + e.TenantID + "'"
}

// ConditionParsingError represents a condition parsing error.
type ConditionParsingError struct {
	Expression string
	Reason     string
}

// Error implements the error interface.
func (e *ConditionParsingError) Error() string {
	return "condition parsing failed for '" + e.Expression + "': " + e.Reason
}

// TenantNotFoundError is a typed error for tenant not found with context.
type TenantNotFoundError struct {
	TenantID string
}

// Error implements the error interface.
func (e *TenantNotFoundError) Error() string {
	return "tenant not found: " + e.TenantID
}

// NewTenantNotFoundError creates a new tenant not found error with context.
func NewTenantNotFoundError(tenantID string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.tenant",
		Err:  &TenantNotFoundError{TenantID: tenantID},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
		},
	}
}

// InvalidTenantIDError is a typed error for invalid tenant ID with context.
type InvalidTenantIDError struct {
	TenantID string
	Reason   string
}

// Error implements the error interface.
func (e *InvalidTenantIDError) Error() string {
	return "invalid tenant ID " + e.TenantID + ": " + e.Reason
}

// NewInvalidTenantIDError creates a new invalid tenant ID error with context.
func NewInvalidTenantIDError(tenantID string, reason string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant",
		Err:  &InvalidTenantIDError{TenantID: tenantID, Reason: reason},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
			"reason":    reason,
		},
	}
}

// QuotaExceededError is a typed error for quota exceeded with context.
type QuotaExceededError struct {
	Resource string
	Limit    int64
	Current  int64
}

// Error implements the error interface.
func (e *QuotaExceededError) Error() string {
	return "quota exceeded for " + e.Resource + ": limit=" + strconv.FormatInt(e.Limit, 10) + ", current=" + strconv.FormatInt(e.Current, 10)
}

// NewQuotaExceededError creates a new quota exceeded error with context.
func NewQuotaExceededError(tenantID string, resource string, limit int64, current int64) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.quota",
		Err:  &QuotaExceededError{Resource: resource, Limit: limit, Current: current},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
			"resource":  resource,
			"limit":     limit,
			"current":   current,
		},
	}
}

// NewIAMError creates a new IAM error with the specified code, operation, and message.
func NewIAMError(base *dberrors.DragonError, op string, message string, cause error) *dberrors.DragonError {
	return &dberrors.DragonError{
		Code: base.Code,
		Op:   op,
		Err:  &IAMContextError{Message: message, Cause: cause},
	}
}

// IAMPolicyNotFoundError is a typed error for policy not found with context.
type IAMPolicyNotFoundError struct {
	PolicyName string
}

// Error implements the error interface.
func (e *IAMPolicyNotFoundError) Error() string {
	return "policy not found: " + e.PolicyName
}

// NewPolicyNotFoundError creates a new policy not found error with context.
func NewPolicyNotFoundError(tenantID, policyName string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.policy",
		Err:  &IAMPolicyNotFoundError{PolicyName: policyName},
		Context: map[string]interface{}{
			"tenant_id":   tenantID,
			"policy_name": policyName,
		},
	}
}

// PolicyCompilationError is a typed error for policy compilation failure.
type PolicyCompilationError struct {
	PolicyName string
	Cause      error
}

// Error implements the error interface.
func (e *PolicyCompilationError) Error() string {
	return "failed to compile policy " + e.PolicyName + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicyCompilationError) Unwrap() error {
	return e.Cause
}

// NewPolicyCompilationError creates a new policy compilation error with context.
func NewPolicyCompilationError(tenantID, policyName string, cause error) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.compile",
		Err:  &PolicyCompilationError{PolicyName: policyName, Cause: cause},
		Context: map[string]interface{}{
			"tenant_id":   tenantID,
			"policy_name": policyName,
		},
	}
}

// PolicyEvaluationError is a typed error for policy evaluation failure.
type PolicyEvaluationError struct {
	PolicyName string
	Cause      error
}

// Error implements the error interface.
func (e *PolicyEvaluationError) Error() string {
	return "failed to evaluate policy " + e.PolicyName + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicyEvaluationError) Unwrap() error {
	return e.Cause
}

// NewPolicyEvaluationError creates a new policy evaluation error with context.
func NewPolicyEvaluationError(tenantID, policyName string, cause error) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.evaluate",
		Err:  &PolicyEvaluationError{PolicyName: policyName, Cause: cause},
		Context: map[string]interface{}{
			"tenant_id":   tenantID,
			"policy_name": policyName,
		},
	}
}

// PolicySyncError is a typed error for policy sync failure.
type PolicySyncError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *PolicySyncError) Error() string {
	return "failed to sync policies for tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicySyncError) Unwrap() error {
	return e.Cause
}

// NewPolicySyncError creates a new policy sync error with context.
func NewPolicySyncError(tenantID string, cause error) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.sync",
		Err:  &PolicySyncError{TenantID: tenantID, Cause: cause},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
		},
	}
}

// InvalidCheckRequestError is a typed error for invalid check request.
type InvalidCheckRequestError struct {
	Reason string
}

// Error implements the error interface.
func (e *InvalidCheckRequestError) Error() string {
	return "invalid check request: " + e.Reason
}

// NewInvalidCheckRequestError creates a new invalid check request error with context.
func NewInvalidCheckRequestError(reason string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.pep.check",
		Err:  &InvalidCheckRequestError{Reason: reason},
		Context: map[string]interface{}{
			"reason": reason,
		},
	}
}

// CredentialNotFoundError is a typed error for credential not found.
type CredentialNotFoundError struct {
	CredentialID string
}

// Error implements the error interface.
func (e *CredentialNotFoundError) Error() string {
	return "credential not found: " + e.CredentialID
}

// NewCredentialNotFoundError creates a new credential not found error with context.
func NewCredentialNotFoundError(tenantID, credentialID string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.credential",
		Err:  &CredentialNotFoundError{CredentialID: credentialID},
		Context: map[string]interface{}{
			"tenant_id":     tenantID,
			"credential_id": credentialID,
		},
	}
}

// ChallengeNotFoundError is a typed error for challenge not found.
type ChallengeNotFoundError struct {
	ChallengeID string
}

// Error implements the error interface.
func (e *ChallengeNotFoundError) Error() string {
	return "challenge not found: " + e.ChallengeID
}

// NewChallengeNotFoundError creates a new challenge not found error with context.
func NewChallengeNotFoundError(challengeID string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.challenge",
		Err:  &ChallengeNotFoundError{ChallengeID: challengeID},
		Context: map[string]interface{}{
			"challenge_id": challengeID,
		},
	}
}

// ChallengeExpiredError is a typed error for challenge expired.
type ChallengeExpiredError struct {
	ChallengeID string
}

// Error implements the error interface.
func (e *ChallengeExpiredError) Error() string {
	return "challenge expired: " + e.ChallengeID
}

// NewChallengeExpiredError creates a new challenge expired error with context.
func NewChallengeExpiredError(challengeID string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.challenge",
		Err:  &ChallengeExpiredError{ChallengeID: challengeID},
		Context: map[string]interface{}{
			"challenge_id": challengeID,
		},
	}
}

// WebAuthnRegistrationError is a typed error for WebAuthn registration failure.
type WebAuthnRegistrationError struct {
	UserID string
	Cause  error
}

// Error implements the error interface.
func (e *WebAuthnRegistrationError) Error() string {
	return "WebAuthn registration failed for user " + e.UserID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *WebAuthnRegistrationError) Unwrap() error {
	return e.Cause
}

// NewWebAuthnRegistrationError creates a new WebAuthn registration error with context.
func NewWebAuthnRegistrationError(tenantID, userID string, cause error) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.webauthn.registration",
		Err:  &WebAuthnRegistrationError{UserID: userID, Cause: cause},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
			"user_id":   userID,
		},
	}
}

// WebAuthnAuthenticationError is a typed error for WebAuthn authentication failure.
type WebAuthnAuthenticationError struct {
	Email string
	Cause error
}

// Error implements the error interface.
func (e *WebAuthnAuthenticationError) Error() string {
	return "WebAuthn authentication failed for email " + e.Email + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *WebAuthnAuthenticationError) Unwrap() error {
	return e.Cause
}

// NewWebAuthnAuthenticationError creates a new WebAuthn authentication error with context.
func NewWebAuthnAuthenticationError(tenantID, email string, cause error) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.webauthn.authentication",
		Err:  &WebAuthnAuthenticationError{Email: email, Cause: cause},
		Context: map[string]interface{}{
			"tenant_id": tenantID,
			"email":     email,
		},
	}
}

// OAuth2 error codes as defined in RFC 6749.
const (
	ErrOAuthInvalidRequest          = "invalid_request"
	ErrOAuthInvalidClient           = "invalid_client"
	ErrOAuthInvalidGrant            = "invalid_grant"
	ErrOAuthUnauthorizedClient      = "unauthorized_client"
	ErrOAuthUnsupportedGrantType    = "unsupported_grant_type"
	ErrOAuthInvalidScope            = "invalid_scope"
	ErrOAuthAccessDenied            = "access_denied"
	ErrOAuthUnsupportedResponseType = "unsupported_response_type"
	ErrOAuthServerError             = "server_error"
	ErrOAuthTemporarilyUnavailable  = "temporarily_unavailable"
)

// OAuth Error represents an OAuth2 error response.
type OAuthError struct {
	// ErrorCode is the OAuth2 error code
	ErrorCode string `json:"error"`

	// ErrorDescription provides additional human-readable information
	ErrorDescription string `json:"error_description,omitempty"`

	// ErrorURI is a URI identifying a human-readable web page with information about the error
	ErrorURI string `json:"error_uri,omitempty"`

	// State preserves the state parameter from the request
	State string `json:"state,omitempty"`
}

// NewOAuthError creates a new OAuth2 error.
func NewOAuthError(code, description string) *OAuthError {
	return &OAuthError{
		ErrorCode:        code,
		ErrorDescription: description,
	}
}

// WithState adds state to an OAuth error.
func (e *OAuthError) WithState(state string) *OAuthError {
	e.State = state
	return e
}

// WithURI adds an error URI to an OAuth error.
func (e *OAuthError) WithURI(uri string) *OAuthError {
	e.ErrorURI = uri
	return e
}

// Implement the error interface for OAuthError.
func (e *OAuthError) Error() string {
	if e.ErrorDescription != "" {
		return e.ErrorCode + ": " + e.ErrorDescription
	}
	return e.ErrorCode
}

// ErrPKCEPlainMethodNotAllowed is returned when the plain PKCE method is used.
// OAuth 2.1 prohibits the plain method; only S256 is allowed.
var ErrPKCEPlainMethodNotAllowed = &OAuthError{
	ErrorCode:        ErrOAuthInvalidRequest,
	ErrorDescription: "code_challenge_method 'plain' is not allowed per OAuth 2.1; use S256",
}

// ValidationError represents a field validation error.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface for ValidationError.
func (e *ValidationError) Error() string {
	return "validation error: " + e.Field + ": " + e.Message
}

// InvalidSyncConfigError represents a sync configuration validation error.
type InvalidSyncConfigError struct {
	Reason string
}

// Error implements the error interface for InvalidSyncConfigError.
func (e *InvalidSyncConfigError) Error() string {
	return "invalid sync configuration: " + e.Reason
}

// UnexpectedResultTypeError is returned when a Raft read returns an unexpected type.
type UnexpectedResultTypeError struct {
	ExpectedType string
	ActualType   string
}

// Error implements the error interface.
func (e *UnexpectedResultTypeError) Error() string {
	return "unexpected result type: expected " + e.ExpectedType + ", got " + e.ActualType
}

// NilSnapshotError is returned when a snapshot is nil.
type NilSnapshotError struct{}

// Error implements the error interface.
func (e *NilSnapshotError) Error() string {
	return "snapshot is nil"
}

// MissingPolicyDataError is returned when policy data is missing for a change event.
type MissingPolicyDataError struct {
	EventType string
}

// Error implements the error interface.
func (e *MissingPolicyDataError) Error() string {
	return "missing policy data for " + e.EventType + " event"
}

// ResourceAlreadyExistsError is returned when a generic resource already exists.
type ResourceAlreadyExistsError struct{}

// Error implements the error interface.
func (e *ResourceAlreadyExistsError) Error() string {
	return "resource already exists"
}

// InvalidIndexEntryError is returned when an index entry has an invalid format.
type InvalidIndexEntryError struct{}

// Error implements the error interface.
func (e *InvalidIndexEntryError) Error() string {
	return "invalid index entry"
}

// SyncInProgressError is returned when a sync is already in progress.
type SyncInProgressError struct{}

// Error implements the error interface.
func (e *SyncInProgressError) Error() string {
	return "sync already in progress"
}

// IncrementalSyncDisabledError is returned when incremental sync is not enabled.
type IncrementalSyncDisabledError struct{}

// Error implements the error interface.
func (e *IncrementalSyncDisabledError) Error() string {
	return "incremental sync is not enabled"
}

// NilEventError is returned when a nil event is passed.
type NilEventError struct{}

// Error implements the error interface.
func (e *NilEventError) Error() string {
	return "event is nil"
}

// VersionConflictError is returned when a version conflict occurs during sync.
type VersionConflictError struct {
	Current  int64
	Incoming int64
}

// Error implements the error interface.
func (e *VersionConflictError) Error() string {
	return "version conflict: current=" + strconv.FormatInt(e.Current, 10) + ", incoming=" + strconv.FormatInt(e.Incoming, 10)
}

// PolicyCompileFailedError is returned when a policy fails to compile during sync.
type PolicyCompileFailedError struct {
	Cause error
}

// Error implements the error interface.
func (e *PolicyCompileFailedError) Error() string {
	return "failed to compile policy: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicyCompileFailedError) Unwrap() error {
	return e.Cause
}

// RaftProposerRequiredError is returned when a raft proposer is not provided.
type RaftProposerRequiredError struct{}

// Error implements the error interface.
func (e *RaftProposerRequiredError) Error() string {
	return "raft proposer is required for distributed PEP"
}

// SyncerCreationError is returned when a syncer fails to create.
type SyncerCreationError struct {
	Cause error
}

// Error implements the error interface.
func (e *SyncerCreationError) Error() string {
	return "failed to create syncer: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *SyncerCreationError) Unwrap() error {
	return e.Cause
}

// SubscribeFailedError is returned when subscribing to cluster changes fails.
type SubscribeFailedError struct {
	Cause error
}

// Error implements the error interface.
func (e *SubscribeFailedError) Error() string {
	return "failed to subscribe to cluster changes: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *SubscribeFailedError) Unwrap() error {
	return e.Cause
}

// PolicyUnmarshalError is returned when a policy fails to unmarshal.
type PolicyUnmarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *PolicyUnmarshalError) Error() string {
	return "failed to unmarshal policy: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicyUnmarshalError) Unwrap() error {
	return e.Cause
}

// PolicyMarshalError is returned when a policy fails to marshal.
type PolicyMarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *PolicyMarshalError) Error() string {
	return "failed to marshal policy: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PolicyMarshalError) Unwrap() error {
	return e.Cause
}

// BaseSyncerCreationError is returned when a base syncer fails to create.
type BaseSyncerCreationError struct {
	Cause error
}

// Error implements the error interface.
func (e *BaseSyncerCreationError) Error() string {
	return "failed to create base syncer: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *BaseSyncerCreationError) Unwrap() error {
	return e.Cause
}

// BatchProcessorCreationError is returned when a batch processor fails to create.
type BatchProcessorCreationError struct {
	Cause error
}

// Error implements the error interface.
func (e *BatchProcessorCreationError) Error() string {
	return "failed to create batch processor: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *BatchProcessorCreationError) Unwrap() error {
	return e.Cause
}

// DeltaLogAppendError is returned when appending to the delta log fails.
type DeltaLogAppendError struct {
	Cause error
}

// Error implements the error interface.
func (e *DeltaLogAppendError) Error() string {
	return "failed to append to delta log: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *DeltaLogAppendError) Unwrap() error {
	return e.Cause
}

// ConnectionCreationError is returned when creating a pool connection fails.
type ConnectionCreationError struct {
	Index int
	Total int
	Cause error
}

// Error implements the error interface.
func (e *ConnectionCreationError) Error() string {
	return "failed to create initial connection " + strconv.Itoa(e.Index) + "/" + strconv.Itoa(e.Total) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ConnectionCreationError) Unwrap() error {
	return e.Cause
}

// InvalidConnectionTypeError is returned when an invalid connection type is passed to pool.
type InvalidConnectionTypeError struct{}

// Error implements the error interface.
func (e *InvalidConnectionTypeError) Error() string {
	return "invalid connection type"
}

// FactoryFailedError is returned when a connection factory fails.
type FactoryFailedError struct {
	Cause error
}

// Error implements the error interface.
func (e *FactoryFailedError) Error() string {
	return "factory failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *FactoryFailedError) Unwrap() error {
	return e.Cause
}

// DeltaLogVersionError is returned when a requested version is too old.
type DeltaLogVersionError struct {
	RequestedVersion int64
	MinimumVersion   int64
}

// Error implements the error interface.
func (e *DeltaLogVersionError) Error() string {
	return "requested version " + strconv.FormatInt(e.RequestedVersion, 10) + " is older than available minimum version " + strconv.FormatInt(e.MinimumVersion, 10)
}

// CompressorCreationError is returned when a compressor fails to create.
type CompressorCreationError struct {
	Cause error
}

// Error implements the error interface.
func (e *CompressorCreationError) Error() string {
	return "failed to create compressor: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *CompressorCreationError) Unwrap() error {
	return e.Cause
}

// BatchMarshalError is returned when marshaling a batch fails.
type BatchMarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *BatchMarshalError) Error() string {
	return "failed to marshal batch: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *BatchMarshalError) Unwrap() error {
	return e.Cause
}

// BatchCompressError is returned when compressing a batch fails.
type BatchCompressError struct {
	Cause error
}

// Error implements the error interface.
func (e *BatchCompressError) Error() string {
	return "failed to compress batch: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *BatchCompressError) Unwrap() error {
	return e.Cause
}

// BatchNotifyError is returned when notifying a batch fails.
type BatchNotifyError struct {
	Cause error
}

// Error implements the error interface.
func (e *BatchNotifyError) Error() string {
	return "failed to notify batch: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *BatchNotifyError) Unwrap() error {
	return e.Cause
}

// ResourceURIError is returned when a resource URI is invalid.
type ResourceURIError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *ResourceURIError) Error() string {
	if e.Cause != nil {
		return e.Detail + ": " + e.Cause.Error()
	}
	return e.Detail
}

// Unwrap returns the underlying cause.
func (e *ResourceURIError) Unwrap() error {
	return e.Cause
}

// AttributeMatcherError is returned when an attribute matcher is unsupported.
type AttributeMatcherError struct {
	Key string
}

// Error implements the error interface.
func (e *AttributeMatcherError) Error() string {
	return "unsupported attribute matcher operators for key " + e.Key
}

// AttributeMatcherCompilationError is returned when compiling an attribute matcher fails.
type AttributeMatcherCompilationError struct {
	Key   string
	Cause error
}

// Error implements the error interface.
func (e *AttributeMatcherCompilationError) Error() string {
	return "failed to compile attribute matcher for " + e.Key + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AttributeMatcherCompilationError) Unwrap() error {
	return e.Cause
}

// ConditionParseError is returned when parsing a policy condition expression fails.
type ConditionParseError struct {
	Expression string
	Cause      error
}

// Error implements the error interface.
func (e *ConditionParseError) Error() string {
	return "failed to parse condition '" + e.Expression + "': " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ConditionParseError) Unwrap() error {
	return e.Cause
}

// UnlessConditionParseError is returned when parsing an unless condition expression fails.
type UnlessConditionParseError struct {
	Expression string
	Cause      error
}

// Error implements the error interface.
func (e *UnlessConditionParseError) Error() string {
	return "failed to parse unless condition '" + e.Expression + "': " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *UnlessConditionParseError) Unwrap() error {
	return e.Cause
}

// TimeParseError is returned when time parsing fails.
type TimeParseError struct {
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *TimeParseError) Error() string {
	if e.Cause != nil {
		return e.Detail + ": " + e.Cause.Error()
	}
	return e.Detail
}

// Unwrap returns the underlying cause.
func (e *TimeParseError) Unwrap() error {
	return e.Cause
}

// UnixTimeFormatError is returned when a UnixTime value has an invalid format.
type UnixTimeFormatError struct{}

// Error implements the error interface.
func (e *UnixTimeFormatError) Error() string {
	return "UnixTime: invalid format, expected number or RFC3339 string"
}

// IDTokenFormatError is returned when an ID token has an invalid format.
type IDTokenFormatError struct{}

// Error implements the error interface.
func (e *IDTokenFormatError) Error() string {
	return "invalid ID token format"
}

// IDTokenDecodeError is returned when an ID token payload fails to decode.
type IDTokenDecodeError struct {
	Cause error
}

// Error implements the error interface.
func (e *IDTokenDecodeError) Error() string {
	return "failed to decode ID token payload: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *IDTokenDecodeError) Unwrap() error {
	return e.Cause
}

// IDTokenClaimsError is returned when ID token claims fail to parse.
type IDTokenClaimsError struct {
	Cause error
}

// Error implements the error interface.
func (e *IDTokenClaimsError) Error() string {
	return "failed to parse ID token claims: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *IDTokenClaimsError) Unwrap() error {
	return e.Cause
}

// OIDCConfigRequiredError is returned when a required OIDC config field is missing.
type OIDCConfigRequiredError struct {
	Field string
}

// Error implements the error interface.
func (e *OIDCConfigRequiredError) Error() string {
	return e.Field + " is required"
}

// JWKSGenerationError is returned when JWKS generation fails.
type JWKSGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *JWKSGenerationError) Error() string {
	return "failed to generate JWKS: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *JWKSGenerationError) Unwrap() error {
	return e.Cause
}

// TokenRevokedError is returned when a token has been revoked.
type TokenRevokedError struct{}

// Error implements the error interface.
func (e *TokenRevokedError) Error() string {
	return "token is revoked"
}

// ConsentIDGenerationError is returned when consent ID generation fails.
type ConsentIDGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *ConsentIDGenerationError) Error() string {
	return "failed to generate consent ID: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ConsentIDGenerationError) Unwrap() error {
	return e.Cause
}

// ExportIDGenerationError is returned when export ID generation fails.
type ExportIDGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *ExportIDGenerationError) Error() string {
	return "failed to generate export ID: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ExportIDGenerationError) Unwrap() error {
	return e.Cause
}

// DeletionIDGenerationError is returned when deletion ID generation fails.
type DeletionIDGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *DeletionIDGenerationError) Error() string {
	return "failed to generate deletion ID: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *DeletionIDGenerationError) Unwrap() error {
	return e.Cause
}

// ConsentExistsError is returned when a consent already exists for a purpose.
type ConsentExistsError struct {
	Purpose   string
	ConsentID string
}

// Error implements the error interface.
func (e *ConsentExistsError) Error() string {
	return "consent already exists for purpose " + e.Purpose + ": " + e.ConsentID
}

// ProviderNameExistsError is returned when a provider name already exists.
type ProviderNameExistsError struct {
	Name string
}

// Error implements the error interface.
func (e *ProviderNameExistsError) Error() string {
	return "provider with name '" + e.Name + "' already exists"
}

// LinkExternalExistsError is returned when a link for an external ID already exists.
type LinkExternalExistsError struct {
	ExternalID string
}

// Error implements the error interface.
func (e *LinkExternalExistsError) Error() string {
	return "link for external ID '" + e.ExternalID + "' already exists"
}

// FederatedLinkNotFoundError is returned when a federated identity link is not found.
type FederatedLinkNotFoundError struct {
	LinkID string
}

// Error implements the error interface.
func (e *FederatedLinkNotFoundError) Error() string {
	return "federated identity link not found: " + e.LinkID
}

// StateExpiredDetailError is returned when a callback state has expired.
type StateExpiredDetailError struct {
	Token string
}

// Error implements the error interface.
func (e *StateExpiredDetailError) Error() string {
	return "callback state expired: " + e.Token
}

// DiscoveryStatusError is returned when OIDC discovery returns a non-200 status.
type DiscoveryStatusError struct {
	StatusCode int
}

// Error implements the error interface.
func (e *DiscoveryStatusError) Error() string {
	return "discovery returned status " + strconv.Itoa(e.StatusCode)
}

// UserInfoStatusError is returned when the userinfo endpoint returns a non-200 status.
type UserInfoStatusError struct {
	StatusCode int
}

// Error implements the error interface.
func (e *UserInfoStatusError) Error() string {
	return "userinfo returned status " + strconv.Itoa(e.StatusCode)
}

// BatchUnsupportedOperationError indicates an unsupported batch operation type.
type BatchUnsupportedOperationError struct{}

// Error implements the error interface.
func (e *BatchUnsupportedOperationError) Error() string {
	return "unsupported operation type"
}

// BatchCSVHeaderError indicates the CSV must have a header and at least one data row.
type BatchCSVHeaderError struct{}

// Error implements the error interface.
func (e *BatchCSVHeaderError) Error() string {
	return "CSV must have header and at least one data row"
}

// BatchCSVNotSupportedError indicates CSV import is not supported for the given operation type.
type BatchCSVNotSupportedError struct {
	OperationType string
}

// Error implements the error interface.
func (e *BatchCSVNotSupportedError) Error() string {
	return "CSV import not supported for " + e.OperationType
}

// BatchCannotCancelError indicates the operation cannot be cancelled.
type BatchCannotCancelError struct{}

// Error implements the error interface.
func (e *BatchCannotCancelError) Error() string {
	return "operation cannot be cancelled"
}

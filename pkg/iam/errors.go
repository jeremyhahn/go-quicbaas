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
	"strings"

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

// AppFieldNotStoredError is returned when an application update carries
// fields that Application has no place for. The update is refused whole:
// a field the service cannot apply is never silently dropped.
type AppFieldNotStoredError struct {
	Fields []string
}

// Error implements the error interface.
func (e *AppFieldNotStoredError) Error() string {
	return "application does not store field(s): " + strings.Join(e.Fields, ", ")
}

// Unwrap returns ErrAppFieldNotStored so callers can match the coded sentinel.
func (e *AppFieldNotStoredError) Unwrap() error {
	return ErrAppFieldNotStored
}

// AppMetadataError marks an application register or update refused for its
// metadata (redirect URIs, grant types, auth method, audience, fields), as
// opposed to a failure of the store or the platform. Transports map it to a
// client error; every other register or update error is a server error.
type AppMetadataError struct {
	Cause error
}

// Error implements the error interface with the refusal's own message.
func (e *AppMetadataError) Error() string {
	return e.Cause.Error()
}

// Unwrap returns the refusal so callers can still match its type or sentinel.
func (e *AppMetadataError) Unwrap() error {
	return e.Cause
}

// TenantFieldNotStoredError is returned when a tenant update sets a field
// the tenant does not store yet: description, metadata or status (#1518).
// The update is refused whole, so a caller is never told a dropped field
// was stored.
type TenantFieldNotStoredError struct {
	Fields []string
}

// Error implements the error interface.
func (e *TenantFieldNotStoredError) Error() string {
	return "tenant does not store field(s): " + strings.Join(e.Fields, ", ")
}

// Unwrap returns ErrTenantFieldNotStored so callers can match the coded sentinel.
func (e *TenantFieldNotStoredError) Unwrap() error {
	return ErrTenantFieldNotStored
}

// CheckTenantFieldsStored refuses a tenant update that sets description,
// metadata or status. It returns nil when none is set, and otherwise a
// TenantFieldNotStoredError naming the set fields in that fixed order.
func CheckTenantFieldsStored(hasDescription, hasMetadata, hasStatus bool) error {
	var fields []string
	if hasDescription {
		fields = append(fields, "description")
	}
	if hasMetadata {
		fields = append(fields, "metadata")
	}
	if hasStatus {
		fields = append(fields, "status")
	}
	if len(fields) == 0 {
		return nil
	}
	return &TenantFieldNotStoredError{Fields: fields}
}

// IAM-specific error types following QuicBaaS error conventions.
var (
	// ErrTenantNotFound is returned when a tenant is not found.
	ErrTenantNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant not found"},
	}

	// ErrTenantAlreadyExists is returned when attempting to create a duplicate tenant.
	ErrTenantAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant already exists"},
	}

	// ErrInvalidTenantID is returned when a tenant ID is invalid.
	ErrInvalidTenantID = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant ID"},
	}

	// ErrInvalidTenantName is returned when a tenant name is invalid.
	ErrInvalidTenantName = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant name"},
	}

	// ErrQuotaExceeded is returned when a quota limit is exceeded.
	ErrQuotaExceeded = &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.quota",
		Err:  &IAMMessageError{Message: "quota exceeded"},
	}

	// ErrTenantSuspended is returned when operating on a suspended tenant.
	ErrTenantSuspended = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant is suspended"},
	}

	// ErrTenantDeleted is returned when operating on a deleted tenant.
	ErrTenantDeleted = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "tenant is deleted"},
	}

	// ErrTenantInvalid is returned when tenant validation fails.
	ErrTenantInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.tenant",
		Err:  &IAMMessageError{Message: "invalid tenant"},
	}

	// ErrUserInvalid is returned when user validation fails.
	ErrUserInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "invalid user"},
	}

	// ErrRoleInvalid is returned when role validation fails.
	ErrRoleInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.role",
		Err:  &IAMMessageError{Message: "invalid role"},
	}

	// ErrPolicyInvalid is returned when policy validation fails.
	ErrPolicyInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.policy",
		Err:  &IAMMessageError{Message: "invalid policy"},
	}

	// ErrPolicyRecordInvalid is returned when a stored policy record does
	// not decode strictly. The record is never applied.
	ErrPolicyRecordInvalid = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.load",
		Err:  &IAMMessageError{Message: "stored policy record does not decode"},
	}

	// ErrRuleInvalid is returned when rule validation fails.
	ErrRuleInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.rule",
		Err:  &IAMMessageError{Message: "invalid rule"},
	}

	// ErrConditionNested is returned when a rule condition nests And, Or or
	// Not. go-qrdb's ABAC condition carries only a flat Expression, so a
	// nested condition cannot be enforced and is refused rather than dropped.
	ErrConditionNested = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.condition",
		Err:  &IAMMessageError{Message: "nested conditions are not supported"},
	}

	// ErrPolicyNotFound is returned when a policy is not found.
	ErrPolicyNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.policy",
		Err:  &IAMMessageError{Message: "policy not found"},
	}

	// ErrPolicyCompilationFailed is returned when policy compilation fails.
	ErrPolicyCompilationFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.compile",
		Err:  &IAMMessageError{Message: "policy compilation failed"},
	}

	// ErrPolicyEvaluationFailed is returned when policy evaluation fails.
	ErrPolicyEvaluationFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.evaluate",
		Err:  &IAMMessageError{Message: "policy evaluation failed"},
	}

	// ErrPolicySyncFailed is returned when policy sync fails.
	ErrPolicySyncFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.policy.sync",
		Err:  &IAMMessageError{Message: "policy sync failed"},
	}

	// ErrPEPClosed is returned when operating on a closed PEP.
	ErrPEPClosed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.pep",
		Err:  &IAMMessageError{Message: "PEP is closed"},
	}

	// ErrInvalidCheckRequest is returned when a check request is invalid.
	ErrInvalidCheckRequest = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.pep.check",
		Err:  &IAMMessageError{Message: "invalid check request"},
	}

	// ErrUserNotFound is returned when a user is not found.
	ErrUserNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.user",
		Err:  &UserNotFoundError{},
	}

	// ErrUserAlreadyExists is returned when attempting to create a user that already exists.
	ErrUserAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "user already exists"},
	}

	// ErrEmailAlreadyExists is returned when attempting to create a user with a duplicate email.
	ErrEmailAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.user",
		Err:  &IAMMessageError{Message: "email already exists"},
	}

	// ErrRoleAlreadyExists is returned when attempting to create a role that already exists.
	ErrRoleAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.role",
		Err:  &RoleAlreadyExistsError{},
	}

	// ErrRoleIDAlreadyExists is returned when a create supplies a role ID that
	// is already taken.
	//
	// It shares ErrAlreadyExists with the duplicate-NAME case because both are
	// genuine conflicts and both belong at the same status, but it is a
	// separate sentinel because the two are different conditions with different
	// remedies: a duplicate name means choose another name, a duplicate id
	// means the caller either collided with an existing role or is retrying a
	// create it already made. Collapsing them would tell an operator "role
	// already exists" when the name they chose is free.
	//
	// This mirrors how the user store already distinguishes
	// ErrEmailAlreadyExists from ErrUserAlreadyExists — same code, distinct
	// message — rather than inventing a status for it. Because QBError.Is
	// compares the code alone, callers tell the two apart with
	// IsRoleIDAlreadyExists / IsRoleAlreadyExists, not errors.Is.
	ErrRoleIDAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.role",
		Err:  &RoleIDAlreadyExistsError{},
	}

	// ErrSystemRoleImmutable is returned when an operation would rename,
	// re-permission or delete a built-in system role.
	//
	// Forbidden rather than conflict: a conflict invites the caller to resolve
	// it and retry, and there is nothing to resolve — no caller may perform
	// this operation on this object at any time. Reporting it as a conflict
	// would send an operator looking for the state to fix.
	ErrSystemRoleImmutable = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.role",
		Err:  &SystemRoleImmutableError{},
	}

	// ErrReservedRoleName is returned when a create would take a name reserved
	// for a built-in system role.
	//
	// Invalid input rather than already-exists: the name is refused by policy,
	// not because a row currently holds it, and the caller's remedy is to
	// choose a different name. It sits with the other value-level refusals
	// (RoleValidationError, RoleInvalidPermissionError) that this package
	// already maps to invalid input.
	ErrReservedRoleName = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.role",
		Err:  &ReservedRoleNameError{},
	}

	// ErrRoleReconcileUnsupported is returned when built-in role
	// reconciliation is requested of a role storage that does not implement
	// BuiltInRoleReconciler.
	//
	// Configuration rather than not-implemented: the capability exists and is
	// wired in every deployment whose roles live in qrdb (the only canonical
	// role store). Meeting a storage without it means the process was
	// assembled with roles somewhere else, which an operator fixes in the
	// wiring, not by waiting for a feature.
	ErrRoleReconcileUnsupported = &dberrors.QBError{
		Code: dberrors.ErrConfiguration,
		Op:   "iam.role.reconcile",
		Err:  &RoleReconcileUnsupportedError{},
	}

	// ErrRoleNotFound is returned when a role is not found.
	ErrRoleNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.role",
		Err:  &RoleNotFoundError{},
	}

	// ErrRoleAlreadyAssigned is returned when assigning a role that's already assigned.
	ErrRoleAlreadyAssigned = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.role.assignment",
		Err:  &IAMMessageError{Message: "role already assigned"},
	}

	// ErrRoleNotAssigned is returned when revoking a role that's not assigned.
	ErrRoleNotAssigned = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.role.assignment",
		Err:  &IAMMessageError{Message: "role not assigned"},
	}

	// ErrCredentialInvalid is returned when credential validation fails.
	ErrCredentialInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.webauthn.credential",
		Err:  &IAMMessageError{Message: "invalid credential"},
	}

	// ErrCredentialNotFound is returned when a credential is not found.
	ErrCredentialNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.credential",
		Err:  &IAMMessageError{Message: "credential not found"},
	}

	// ErrChallengeInvalid is returned when challenge validation fails.
	ErrChallengeInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "invalid challenge"},
	}

	// ErrChallengeNotFound is returned when a challenge is not found.
	ErrChallengeNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "challenge not found"},
	}

	// ErrChallengeExpired is returned when a challenge has expired.
	ErrChallengeExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.challenge",
		Err:  &IAMMessageError{Message: "challenge expired"},
	}

	// ErrWebAuthnRegistrationFailed is returned when WebAuthn registration fails.
	ErrWebAuthnRegistrationFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.webauthn.registration",
		Err:  &IAMMessageError{Message: "WebAuthn registration failed"},
	}

	// ErrWebAuthnAuthenticationFailed is returned when WebAuthn authentication fails.
	ErrWebAuthnAuthenticationFailed = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.webauthn.authentication",
		Err:  &IAMMessageError{Message: "WebAuthn authentication failed"},
	}

	// MFA-specific errors.

	// ErrMFAInvalid is returned when MFA validation fails.
	ErrMFAInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "invalid MFA configuration"},
	}

	// ErrMFANotEnrolled is returned when MFA is not enrolled.
	ErrMFANotEnrolled = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa",
		Err:  &MFANotEnrolledError{},
	}

	// ErrMFAAlreadyEnrolled is returned when MFA is already enrolled.
	ErrMFAAlreadyEnrolled = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA already enrolled"},
	}

	// ErrMFAEnrollmentNotFound is returned when an enrollment is not found.
	ErrMFAEnrollmentNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa.enrollment",
		Err:  &IAMMessageError{Message: "MFA enrollment not found"},
	}

	// ErrMFAEnrollmentExpired is returned when an enrollment has expired.
	ErrMFAEnrollmentExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.enrollment",
		Err:  &IAMMessageError{Message: "MFA enrollment expired"},
	}

	// ErrMFAChallengeNotFound is returned when an MFA challenge is not found.
	ErrMFAChallengeNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.mfa.challenge",
		Err:  &IAMMessageError{Message: "MFA challenge not found"},
	}

	// ErrMFAChallengeExpired is returned when an MFA challenge has expired.
	ErrMFAChallengeExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.challenge",
		Err:  &IAMMessageError{Message: "MFA challenge expired"},
	}

	// ErrMFACodeInvalid is returned when an MFA code is invalid.
	ErrMFACodeInvalid = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.mfa.verify",
		Err:  &MFACodeInvalidError{},
	}

	// ErrMFAMaxAttemptsExceeded is returned when max verification attempts are exceeded.
	ErrMFAMaxAttemptsExceeded = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.verify",
		Err:  &IAMMessageError{Message: "maximum MFA verification attempts exceeded"},
	}

	// ErrMFARequired is returned when MFA is required but not verified.
	ErrMFARequired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA verification required"},
	}

	// ErrMFABackupCodeInvalid is returned when a backup code is invalid.
	ErrMFABackupCodeInvalid = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.mfa.backup",
		Err:  &IAMMessageError{Message: "invalid backup code"},
	}

	// ErrMFABackupCodesExhausted is returned when all backup codes are used.
	ErrMFABackupCodesExhausted = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa.backup",
		Err:  &IAMMessageError{Message: "all backup codes have been used"},
	}

	// ErrMFAMethodNotAllowed is returned when an MFA method is not allowed.
	ErrMFAMethodNotAllowed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.mfa",
		Err:  &IAMMessageError{Message: "MFA method not allowed"},
	}

	// Session-specific errors.

	// ErrSessionInvalid is returned when session validation fails.
	ErrSessionInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "invalid session"},
	}

	// ErrSessionNotFound is returned when a session is not found.
	ErrSessionNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session not found"},
	}

	// ErrSessionExpired is returned when a session has expired.
	ErrSessionExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session expired"},
	}

	// ErrSessionLimitExceeded is returned when max concurrent sessions exceeded.
	ErrSessionLimitExceeded = &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session limit exceeded"},
	}

	// ErrSessionRevoked is returned when a session has been revoked.
	ErrSessionRevoked = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.session",
		Err:  &IAMMessageError{Message: "session revoked"},
	}
	// Audit-specific errors.

	// ErrAuditInvalid is returned when audit validation fails.
	ErrAuditInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "invalid audit event"},
	}

	// ErrAuditEventNotFound is returned when an audit event is not found.
	ErrAuditEventNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "audit event not found"},
	}

	// ErrAuditStorageClosed is returned when operating on closed audit storage.
	ErrAuditStorageClosed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "audit storage is closed"},
	}

	// ErrAuditEventExists is returned when an audit event is saved under an
	// ID the store already holds. Overwriting it would silently erase an
	// audit record, so the store refuses the second write.
	ErrAuditEventExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.audit",
		Err:  &IAMMessageError{Message: "audit event ID already exists"},
	}

	// Delegated Admin errors.

	// ErrAdminAssignmentNotFound is returned when an admin assignment is not found.
	ErrAdminAssignmentNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin assignment not found"},
	}

	// ErrAdminAssignmentInvalid is returned when admin assignment validation fails.
	ErrAdminAssignmentInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "invalid admin assignment"},
	}

	// ErrAdminAssignmentExpired is returned when an admin assignment has expired.
	ErrAdminAssignmentExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin assignment expired"},
	}

	// ErrAdminPermissionDenied is returned when admin permission is denied.
	ErrAdminPermissionDenied = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin",
		Err:  &IAMMessageError{Message: "admin permission denied"},
	}

	// ErrImpersonationNotAllowed is returned when impersonation is not allowed.
	ErrImpersonationNotAllowed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation not allowed"},
	}

	// ErrImpersonationActorRequired is returned when StartImpersonation is
	// invoked without an authenticated principal in context. The acting
	// admin is always the authenticated caller (go-quicbaas#383); with no
	// verified identity there is no actor to authorize, so the call fails
	// closed rather than minting a session for an anonymous caller.
	ErrImpersonationActorRequired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation requires an authenticated caller"},
	}

	// ErrImpersonationTargetNotFound is returned when the impersonation
	// target cannot be resolved as a user of the target tenant — no
	// session is minted for an unverified principal (#357).
	ErrImpersonationTargetNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation target not found in target tenant"},
	}

	// ErrImpersonationTargetResolverRequired is returned when
	// StartImpersonation is invoked on a manager with no
	// ImpersonationTargetResolver wired: without a way to verify the
	// target, impersonation fails closed (#357).
	ErrImpersonationTargetResolverRequired = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation target resolver not configured"},
	}

	// ErrImpersonationSessionNotFound is returned when an impersonation session is not found.
	ErrImpersonationSessionNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation session not found"},
	}

	// ErrImpersonationSessionExpired is returned when an impersonation session has expired.
	ErrImpersonationSessionExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.admin.impersonate",
		Err:  &IAMMessageError{Message: "impersonation session expired"},
	}

	// GDPR Compliance errors.

	// ErrConsentNotFound is returned when a consent record is not found.
	ErrConsentNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "consent not found"},
	}

	// ErrConsentInvalid is returned when consent validation fails.
	ErrConsentInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "invalid consent"},
	}

	// ErrConsentExpired is returned when a consent has expired.
	ErrConsentExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.consent",
		Err:  &IAMMessageError{Message: "consent expired"},
	}

	// ErrDataExportNotFound is returned when a data export request is not found.
	ErrDataExportNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export request not found"},
	}

	// ErrDataExportInvalid is returned when data export validation fails.
	ErrDataExportInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "invalid data export request"},
	}

	// ErrDataExportPending is returned when trying to re-request a pending export.
	ErrDataExportPending = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export already pending"},
	}

	// ErrDataExportExpired is returned when an export has expired.
	ErrDataExportExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.export",
		Err:  &IAMMessageError{Message: "data export expired"},
	}

	// ErrDeletionNotFound is returned when a deletion request is not found.
	ErrDeletionNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion request not found"},
	}

	// ErrDeletionInvalid is returned when deletion validation fails.
	ErrDeletionInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "invalid deletion request"},
	}

	// ErrDeletionPending is returned when trying to re-request a pending deletion.
	ErrDeletionPending = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion request already pending"},
	}

	// ErrDeletionInProgress is returned when deletion is already in progress.
	ErrDeletionInProgress = &dberrors.QBError{
		Code: dberrors.ErrConflict,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion already in progress"},
	}

	// ErrDeletionCancelled is returned when deletion has been cancelled.
	ErrDeletionCancelled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "deletion was cancelled"},
	}

	// ErrGracePeriodActive is returned when trying to process during grace period.
	ErrGracePeriodActive = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.gdpr.deletion",
		Err:  &IAMMessageError{Message: "grace period still active"},
	}

	// Invite / passwordless activation errors.

	// ErrInviteNotFound is returned when an invite token cannot be found.
	ErrInviteNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.invite",
		Err:  &InviteUnusableError{Reason: InviteAbsent},
	}

	// ErrInviteExpired is returned when an invite token has passed its expiry time.
	ErrInviteExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.invite",
		Err:  &InviteUnusableError{Reason: InviteExpired},
	}

	// ErrInviteUsed is returned when an invite token has already been consumed.
	ErrInviteUsed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.invite",
		Err:  &InviteUnusableError{Reason: InviteRedeemed},
	}

	// ErrUserNotPending is returned when an activation is attempted on a user that
	// is not in the pending state.
	ErrUserNotPending = &dberrors.QBError{
		Code: dberrors.ErrConflict,
		Op:   "iam.invite.activate",
		Err:  &UserNotPendingError{},
	}

	// ErrPIVCertRejected is returned when the presented PIV certificate fails
	// chain verification or does not satisfy the required key usage.
	ErrPIVCertRejected = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.invite.activate.piv",
		Err:  &PIVCertRejectedError{},
	}

	// ErrAAGUIDNotAllowedSentinel is the base typed sentinel for AAGUID rejections.
	ErrAAGUIDNotAllowedSentinel = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.aaguid",
		Err:  &IAMMessageError{Message: "authenticator AAGUID not in allowlist"},
	}

	// ErrPasswordRejected is returned when a request body contains a password field,
	// which is not permitted in the passwordless IAM flow.
	ErrPasswordRejected = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.user.create",
		Err:  &IAMMessageError{Message: "password-based authentication is not allowed; use webauthn or piv"},
	}

	// mTLS / OAuth2 RFC 8705 errors.

	// ErrMTLSNoCertificate is returned when no client certificate was presented.
	ErrMTLSNoCertificate = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.oauth2.mtls",
		Err:  &IAMMessageError{Message: "client certificate required"},
	}

	// ErrMTLSClientNotFound is returned when the client_id is not registered.
	ErrMTLSClientNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.oauth2.mtls",
		Err:  &IAMMessageError{Message: "oauth2 client not found"},
	}

	// ErrMTLSAuthMethodMismatch is returned when the client's registered auth
	// method does not match the mTLS path being used.
	ErrMTLSAuthMethodMismatch = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.oauth2.mtls",
		Err:  &IAMMessageError{Message: "token_endpoint_auth_method mismatch"},
	}

	// ErrMTLSCertMismatch is returned when the presented certificate does not
	// match the client's registered Subject DN or SAN dNSName.
	ErrMTLSCertMismatch = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.oauth2.mtls.pki",
		Err:  &IAMMessageError{Message: "certificate subject/SAN does not match registered client"},
	}

	// ErrMTLSThumbprintNotPinned is returned when the client has no thumbprint pinned.
	ErrMTLSThumbprintNotPinned = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.oauth2.mtls.self_signed",
		Err:  &IAMMessageError{Message: "no mtls certificate thumbprint registered for client"},
	}

	// ErrMTLSThumbprintMismatch is returned when the cert thumbprint does not
	// match the pinned value.
	ErrMTLSThumbprintMismatch = &dberrors.QBError{
		Code: dberrors.ErrUnauthorized,
		Op:   "iam.oauth2.mtls.self_signed",
		Err:  &IAMMessageError{Message: "certificate thumbprint does not match registered client"},
	}

	// ErrMTLSInvalidCertPEM is returned when the provided PEM cannot be decoded.
	ErrMTLSInvalidCertPEM = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.oauth2.mtls",
		Err:  &IAMMessageError{Message: "invalid certificate PEM encoding"},
	}

	// ErrMTLSInvalidThumbprint is returned when a certificate thumbprint is
	// not the base64url (unpadded) SHA-256 digest CertThumbprintS256 produces.
	ErrMTLSInvalidThumbprint = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.oauth2.mtls",
		Err:  &IAMMessageError{Message: "certificate thumbprint must be a base64url SHA-256 digest"},
	}

	// ErrMTLSThumbprintRequired is returned when an update would leave a
	// self_signed_tls_client_auth client without a pinned certificate.
	ErrMTLSThumbprintRequired = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.oauth2.mtls.self_signed",
		Err:  &IAMMessageError{Message: "self_signed_tls_client_auth requires a pinned certificate thumbprint"},
	}

	// ErrAppFieldNotStored is the coded sentinel AppFieldNotStoredError unwraps to.
	ErrAppFieldNotStored = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.app.update",
		Err:  &IAMMessageError{Message: "application update carries a field the application does not store"},
	}

	// ErrTenantFieldNotStored is the coded sentinel TenantFieldNotStoredError unwraps to.
	ErrTenantFieldNotStored = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.tenant.update",
		Err:  &IAMMessageError{Message: "tenant update carries a field the tenant does not store"},
	}

	// ErrInvalidTokenEndpointAuthMethod is returned when a token endpoint
	// auth method is not one of the RFC 8705 / RFC 6749 methods supported.
	ErrInvalidTokenEndpointAuthMethod = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.app",
		Err:  &IAMMessageError{Message: "unsupported token endpoint auth method"},
	}

	// ErrInvalidResponseType is returned when an application's response types
	// are not all in response_types_supported, or name a response type whose
	// grant type the application does not hold (RFC 7591 §2.1).
	ErrInvalidResponseType = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.app",
		Err:  &IAMMessageError{Message: "response_types unsupported or inconsistent with grant_types"},
	}

	// ErrMTLSMissingCNFClaim is returned when the access token has no cnf claim.
	ErrMTLSMissingCNFClaim = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.oauth2.mtls.verify",
		Err:  &IAMMessageError{Message: "token missing cnf/x5t#S256 binding claim"},
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

// NestedConditionError identifies a rule condition that nests And, Or or
// Not. Field is the rule's condition list ("when" or "unless") and Index the
// position of the offending condition in it.
type NestedConditionError struct {
	Field string
	Index int
}

// Error implements the error interface.
func (e *NestedConditionError) Error() string {
	return "condition " + e.Field + "[" + strconv.Itoa(e.Index) +
		"] nests and/or/not; only a flat expression is supported"
}

// StoredPolicyDecodeError reports a stored policy record that does not
// decode strictly: it has keys no policy field takes, or it is not a policy
// document at all. The record is never applied, so none of its rules take
// effect. UnknownKeys names the refused keys as dotted paths
// ("spec.rules[0].subjects"); it is empty when the record does not decode
// for another reason, which Reason gives.
type StoredPolicyDecodeError struct {
	TenantID    string
	Name        string
	UnknownKeys []string
	Reason      string

	// record is the record's original stored bytes. A write that restores
	// the flagged record (a rolled-back repair or delete) stores them
	// unchanged, never the rule-less placeholder ListPolicies returns.
	record []byte
}

// Error implements the error interface.
func (e *StoredPolicyDecodeError) Error() string {
	msg := "stored policy '" + e.Name + "' in tenant '" + e.TenantID + "' is not applied: "
	if len(e.UnknownKeys) > 0 {
		return msg + "unknown keys " + strings.Join(e.UnknownKeys, ", ")
	}
	return msg + e.Reason
}

// Unwrap returns ErrPolicyRecordInvalid so callers can match the class.
func (e *StoredPolicyDecodeError) Unwrap() error {
	return ErrPolicyRecordInvalid
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

// InvalidRedirectURIError is returned when an app's redirect URI is refused.
// It describes the request, not the server, so transports answer it as the
// caller's mistake (go-quicbaas#1463). Cause carries the parse error, if any.
type InvalidRedirectURIError struct {
	URI    string
	Reason string
	Cause  error
}

// Error implements the error interface.
func (e *InvalidRedirectURIError) Error() string {
	if e.URI == "" {
		return e.Reason
	}
	return "invalid redirect URI " + e.URI + ": " + e.Reason
}

// Unwrap returns the underlying parse error, if any.
func (e *InvalidRedirectURIError) Unwrap() error {
	return e.Cause
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
func NewIAMError(base *dberrors.QBError, op string, message string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	return &dberrors.QBError{
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
	// ErrOAuthInvalidDPoPProof is returned when a presented DPoP proof fails
	// verification at the token endpoint (RFC 9449 §5).
	ErrOAuthInvalidDPoPProof = "invalid_dpop_proof"
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

// AuthorizeDirectError is an authorization-endpoint error raised while the
// client_id or redirect_uri is missing, unknown or unregistered. Per
// draft-ietf-oauth-v2-1-16 §4.1.2.1 the server must not redirect the user
// agent to an unvalidated redirect URI, so the caller reports it directly.
// Every other error from OIDCProvider.Authorize is raised after the client
// and its redirect URI are validated and goes back to the client through
// that redirect URI.
type AuthorizeDirectError struct {
	*OAuthError
}

// newAuthorizeDirectError creates an AuthorizeDirectError.
func newAuthorizeDirectError(code, description string) *AuthorizeDirectError {
	return &AuthorizeDirectError{OAuthError: NewOAuthError(code, description)}
}

// Unwrap returns the underlying OAuth error.
func (e *AuthorizeDirectError) Unwrap() error {
	return e.OAuthError
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

// HRDInvalidError is returned when a home-realm directory operation receives
// invalid arguments (empty tenant/domain/host, nil store).
type HRDInvalidError struct {
	Reason string
}

// Error implements the error interface.
func (e *HRDInvalidError) Error() string {
	return "home-realm directory: invalid request: " + e.Reason
}

// HRDNotFoundError is returned when no home-realm routing entry matches the
// presented hints (fail-closed: the login must be rejected).
type HRDNotFoundError struct{}

// Error implements the error interface.
func (e *HRDNotFoundError) Error() string {
	return "home-realm directory: no tenant routing entry matched"
}

// HRDConflictError is returned when a domain/host is already registered to a
// different tenant, or an unregister is attempted by a non-owner.
type HRDConflictError struct {
	Owner string
}

// Error implements the error interface.
func (e *HRDConflictError) Error() string {
	return "home-realm directory: routing entry owned by another tenant"
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

// PolicyVersionUnsupportedError is returned when a policy create or update
// carries a non-zero version. Policies are not versioned and nothing
// compares and sets on one, so the key is refused rather than accepted and
// ignored (go-quicbaas#1409).
type PolicyVersionUnsupportedError struct {
	// Version is the version the caller sent, as text.
	Version string
}

// Error implements the error interface.
func (e *PolicyVersionUnsupportedError) Error() string {
	return "policy version is not supported: got " + e.Version + ", send 0 or omit it"
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

// TokenRevokedError is returned when a token has been revoked.
type TokenRevokedError struct{}

// TokenRevocationCheckError is returned when the revocation store cannot
// answer whether a token is revoked. The token is refused: a store that cannot
// answer is not evidence that the token is fine.
type TokenRevocationCheckError struct {
	Cause error
}

// Error implements the error interface.
func (e *TokenRevocationCheckError) Error() string {
	return "token revocation check failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *TokenRevocationCheckError) Unwrap() error {
	return e.Cause
}

// Error implements the error interface.
func (e *TokenRevokedError) Error() string {
	return "token is revoked"
}

// AuditLogIDGenerationError is returned when the CSPRNG fails while minting
// an audit event ID. The ID is the key the audit store files the event under,
// so a guessable fallback could collide and overwrite an existing record; the
// operation that needed the record is refused instead.
type AuditLogIDGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *AuditLogIDGenerationError) Error() string {
	return "failed to generate audit log ID: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AuditLogIDGenerationError) Unwrap() error {
	return e.Cause
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

// UnsupportedDataExportFormatError reports a GDPR data export requested in a
// format other than JSON, the only format the export generator writes.
type UnsupportedDataExportFormatError struct {
	Format DataExportFormat
}

func (e *UnsupportedDataExportFormatError) Error() string {
	return "unsupported data export format " + strconv.Quote(string(e.Format)) + ": only json is supported"
}

// BatchCannotCancelError indicates the operation cannot be cancelled.
type BatchCannotCancelError struct{}

// Error implements the error interface.
func (e *BatchCannotCancelError) Error() string {
	return "operation cannot be cancelled"
}

// MTLSCertVerificationError is returned when a client cert fails chain verification.
type MTLSCertVerificationError struct {
	Cause error
}

// Error implements the error interface.
func (e *MTLSCertVerificationError) Error() string {
	return "mtls certificate chain verification failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MTLSCertVerificationError) Unwrap() error {
	return e.Cause
}

// MTLSCertParseError is returned when a PEM certificate cannot be parsed.
type MTLSCertParseError struct {
	Cause error
}

// Error implements the error interface.
func (e *MTLSCertParseError) Error() string {
	return "failed to parse mtls certificate: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MTLSCertParseError) Unwrap() error {
	return e.Cause
}

// PIVCertIssuerRejectedError is returned when the PIV certificate's issuer DN is
// not in the operator-configured TrustedIssuers list. This is a second-gate check
// that fires after successful chain verification.
type PIVCertIssuerRejectedError struct {
	// Issuer is the Subject DN of the certificate's issuing CA.
	Issuer string

	// AllowedIssuers is the operator-configured allowlist of permitted DNs.
	AllowedIssuers []string
}

// Error implements the error interface.
func (e *PIVCertIssuerRejectedError) Error() string {
	return "piv certificate issuer DN \"" + e.Issuer + "\" is not in the trusted issuers list"
}

// NewPIVCertIssuerRejectedError creates a QBError wrapping PIVCertIssuerRejectedError.
func NewPIVCertIssuerRejectedError(issuer string, allowed []string) error {
	return &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.piv.issuer",
		Err:  &PIVCertIssuerRejectedError{Issuer: issuer, AllowedIssuers: allowed},
		Context: map[string]interface{}{
			"issuer":          issuer,
			"allowed_issuers": allowed,
		},
	}
}

// ErrAAGUIDNotAllowedError is returned when a FIDO2 authenticator AAGUID is not
// in the operator-configured allowlist and the policy is strict enforcement.
type ErrAAGUIDNotAllowedError struct {
	// AAGUID is the hex-encoded AAGUID that was rejected.
	AAGUID string

	// AuthenticatorName is the human-readable name when known from MDS3.
	// Empty when the AAGUID is unknown.
	AuthenticatorName string
}

// Error implements the error interface.
func (e *ErrAAGUIDNotAllowedError) Error() string {
	if e.AuthenticatorName != "" {
		return "authenticator AAGUID " + e.AAGUID + " (" + e.AuthenticatorName + ") is not in the allowlist"
	}
	return "authenticator AAGUID " + e.AAGUID + " is not in the allowlist"
}

// NewErrAAGUIDNotAllowed creates a QBError wrapping ErrAAGUIDNotAllowedError.
func NewErrAAGUIDNotAllowed(aaguid, authenticatorName string) error {
	return &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.aaguid",
		Err:  &ErrAAGUIDNotAllowedError{AAGUID: aaguid, AuthenticatorName: authenticatorName},
		Context: map[string]interface{}{
			"aaguid": aaguid,
		},
	}
}

// ErrAAGUIDGateUnconfiguredError is returned when an AAGUID enforcement
// decision was required and no allowlist is configured at all.
//
// It is a REFUSAL, not a pass-through. An absent allowlist used to mean "skip
// the check", so a deployment that never configured the gate accepted every
// authenticator — including a virtual one — as an administrator credential,
// and did so silently. The refusal names the configuration that is missing so
// the operator can fix it rather than guess.
type ErrAAGUIDGateUnconfiguredError struct{}

// Error implements the error interface.
func (e *ErrAAGUIDGateUnconfiguredError) Error() string {
	return "no FIDO2 AAGUID allowlist is configured, so this authenticator's model cannot be verified; " +
		"set iam.aaguid.allowlist_file to the authenticator models this deployment trusts " +
		"(an unconfigured gate denies rather than accepting any authenticator)"
}

// NewErrAAGUIDGateUnconfigured creates a QBError wrapping
// ErrAAGUIDGateUnconfiguredError.
func NewErrAAGUIDGateUnconfigured() error {
	return &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.aaguid",
		Err:  &ErrAAGUIDGateUnconfiguredError{},
	}
}

// PrincipalWithoutLoginKeyError is returned when a user record carries neither
// an email login key nor a certificate binding, so no login ceremony could ever
// resolve it.
type PrincipalWithoutLoginKeyError struct {
	TenantID string
	Username string
}

// Error implements the error interface.
func (e *PrincipalWithoutLoginKeyError) Error() string {
	return "user " + strconv.Quote(e.Username) + " in tenant " + strconv.Quote(e.TenantID) +
		" has neither an email login key nor a certificate binding, so it could never authenticate"
}

// NewPrincipalWithoutLoginKeyError creates a QBError wrapping
// PrincipalWithoutLoginKeyError.
func NewPrincipalWithoutLoginKeyError(tenantID, username string) error {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.user.create",
		Err:  &PrincipalWithoutLoginKeyError{TenantID: tenantID, Username: username},
	}
}

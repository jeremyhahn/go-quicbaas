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
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestNewPolicyNotFoundError tests the constructor for policy not found.
func TestNewPolicyNotFoundError(t *testing.T) {
	err := NewPolicyNotFoundError("tenant-1", "missing-policy")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	var de *dberrors.QBError
	if !errors.As(err, &de) {
		t.Fatal("expected QBError")
	}
	if de.Code != dberrors.ErrNotFound {
		t.Errorf("expected ErrNotFound code, got %v", de.Code)
	}
	if !strings.Contains(de.Error(), "missing-policy") {
		t.Errorf("expected error to contain policy name, got %q", de.Error())
	}
}

// TestNewPolicyCompilationError tests the constructor for policy compilation.
func TestNewPolicyCompilationError(t *testing.T) {
	cause := errors.New("syntax error")
	err := NewPolicyCompilationError("tenant-1", "broken-policy", cause)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	var de *dberrors.QBError
	if !errors.As(err, &de) {
		t.Fatal("expected QBError")
	}
	if !strings.Contains(de.Error(), "broken-policy") {
		t.Errorf("expected error to contain policy name, got %q", de.Error())
	}
}

// TestNewPolicyEvaluationError tests the constructor for policy evaluation.
func TestNewPolicyEvaluationError(t *testing.T) {
	cause := errors.New("runtime error")
	err := NewPolicyEvaluationError("tenant-1", "eval-policy", cause)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "eval-policy") {
		t.Errorf("expected error to contain policy name, got %q", err.Error())
	}
}

// TestNewInvalidCheckRequestError tests the constructor for invalid check request.
func TestNewInvalidCheckRequestError(t *testing.T) {
	err := NewInvalidCheckRequestError("missing subject")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "missing subject") {
		t.Errorf("expected reason in error, got %q", err.Error())
	}
}

// TestErrorMessages checks that each error type's message carries its
// identifying fields and, for types that wrap a cause, that errors.Is finds
// the cause through Unwrap.
func TestErrorMessages(t *testing.T) {
	cause := errors.New("underlying cause")

	tests := []struct {
		name  string
		err   error
		want  []string
		cause error
	}{
		{"CompilationFailedError", &CompilationFailedError{Policy: "test-policy", Reason: "syntax error at line 5"}, []string{"compilation failed for policy 'test-policy': syntax error at line 5"}, nil},
		{"InvalidPolicyError without name", &InvalidPolicyError{Reason: "actions field is required"}, []string{"invalid policy: actions field is required"}, nil},
		{"InvalidPolicyError with name", &InvalidPolicyError{Name: "admin-policy", Reason: "missing actions"}, []string{"invalid policy 'admin-policy': missing actions"}, nil},
		{"PolicyExistsError", &PolicyExistsError{Name: "admin-access"}, []string{"policy 'admin-access' already exists in tenant ''"}, nil},
		{"PolicyNotFoundTypeError with tenant", &PolicyNotFoundTypeError{TenantID: "tenant-1", Name: "viewer-policy"}, []string{"policy 'viewer-policy' not found in tenant 'tenant-1'"}, nil},
		{"PolicyNotFoundTypeError without tenant", &PolicyNotFoundTypeError{Name: "missing-policy"}, []string{"policy 'missing-policy' not found in tenant ''"}, nil},
		{"IAMPolicyNotFoundError", &IAMPolicyNotFoundError{PolicyName: "viewer-policy"}, []string{"policy not found: viewer-policy"}, nil},
		{"PolicyCompilationError", &PolicyCompilationError{PolicyName: "broken-policy", Cause: cause}, []string{"failed to compile policy broken-policy: underlying cause"}, cause},
		{"PolicyEvaluationError", &PolicyEvaluationError{PolicyName: "eval-policy", Cause: cause}, []string{"failed to evaluate policy eval-policy: underlying cause"}, cause},
		{"ConditionParsingError", &ConditionParsingError{Expression: "time > now()", Reason: "invalid function"}, []string{"condition parsing failed for 'time > now()': invalid function"}, nil},
		{"IAMMessageError", &IAMMessageError{Message: "test message"}, []string{"test message"}, nil},
		{"IAMContextError with cause", &IAMContextError{Message: "outer", Cause: cause}, []string{"outer", "underlying cause"}, cause},
		{"IAMContextError without cause", &IAMContextError{Message: "standalone"}, []string{"standalone"}, nil},
		{"TenantNotFoundError", &TenantNotFoundError{TenantID: "tenant-abc"}, []string{"tenant not found: tenant-abc"}, nil},
		{"InvalidTenantIDError", &InvalidTenantIDError{TenantID: "bad!", Reason: "contains special chars"}, []string{"invalid tenant ID bad!: contains special chars"}, nil},
		{"QuotaExceededError", &QuotaExceededError{Resource: "users", Limit: 100, Current: 101}, []string{"users", "100"}, nil},
		{"PolicySyncError", &PolicySyncError{TenantID: "tenant-1", Cause: cause}, []string{"failed to sync policies for tenant tenant-1: underlying cause"}, cause},
		{"CredentialNotFoundError", &CredentialNotFoundError{CredentialID: "cred-123"}, []string{"credential not found: cred-123"}, nil},
		{"ChallengeNotFoundError", &ChallengeNotFoundError{ChallengeID: "ch-456"}, []string{"challenge not found: ch-456"}, nil},
		{"ChallengeExpiredError", &ChallengeExpiredError{ChallengeID: "ch-789"}, []string{"challenge expired: ch-789"}, nil},
		{"WebAuthnRegistrationError", &WebAuthnRegistrationError{UserID: "user-1", Cause: cause}, []string{"WebAuthn registration failed for user user-1: underlying cause"}, cause},
		{"WebAuthnAuthenticationError", &WebAuthnAuthenticationError{Email: "test@example.com", Cause: cause}, []string{"test@example.com"}, cause},
		{"UnexpectedResultTypeError", &UnexpectedResultTypeError{ExpectedType: "[]byte", ActualType: "string"}, []string{"[]byte", "string"}, nil},
		{"NilSnapshotError", &NilSnapshotError{}, []string{"snapshot is nil"}, nil},
		{"MissingPolicyDataError", &MissingPolicyDataError{EventType: "create"}, []string{"create"}, nil},
		{"ResourceAlreadyExistsError", &ResourceAlreadyExistsError{}, []string{"resource already exists"}, nil},
		{"InvalidIndexEntryError", &InvalidIndexEntryError{}, []string{"invalid index entry"}, nil},
		{"SyncInProgressError", &SyncInProgressError{}, []string{"sync already in progress"}, nil},
		{"IncrementalSyncDisabledError", &IncrementalSyncDisabledError{}, []string{"incremental sync is not enabled"}, nil},
		{"VersionConflictError", &VersionConflictError{Current: 5, Incoming: 3}, []string{"5", "3"}, nil},
		{"PolicyCompileFailedError", &PolicyCompileFailedError{Cause: cause}, []string{"underlying cause"}, cause},
		{"RaftProposerRequiredError", &RaftProposerRequiredError{}, []string{"raft proposer is required for distributed PEP"}, nil},
		{"SyncerCreationError", &SyncerCreationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"SubscribeFailedError", &SubscribeFailedError{Cause: cause}, []string{"underlying cause"}, cause},
		{"PolicyUnmarshalError", &PolicyUnmarshalError{Cause: cause}, []string{"underlying cause"}, cause},
		{"PolicyMarshalError", &PolicyMarshalError{Cause: cause}, []string{"underlying cause"}, cause},
		{"BaseSyncerCreationError", &BaseSyncerCreationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"BatchProcessorCreationError", &BatchProcessorCreationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"DeltaLogAppendError", &DeltaLogAppendError{Cause: cause}, []string{"underlying cause"}, cause},
		{"ConnectionCreationError", &ConnectionCreationError{Index: 2, Total: 5, Cause: cause}, []string{"2/5"}, cause},
		{"InvalidConnectionTypeError", &InvalidConnectionTypeError{}, []string{"invalid connection type"}, nil},
		{"FactoryFailedError", &FactoryFailedError{Cause: cause}, []string{"underlying cause"}, cause},
		{"DeltaLogVersionError", &DeltaLogVersionError{RequestedVersion: 3, MinimumVersion: 10}, []string{"3", "10"}, nil},
		{"CompressorCreationError", &CompressorCreationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"BatchMarshalError", &BatchMarshalError{Cause: cause}, []string{"underlying cause"}, cause},
		{"BatchCompressError", &BatchCompressError{Cause: cause}, []string{"underlying cause"}, cause},
		{"BatchNotifyError", &BatchNotifyError{Cause: cause}, []string{"underlying cause"}, cause},
		{"ResourceURIError with cause", &ResourceURIError{Detail: "invalid URI", Cause: cause}, []string{"invalid URI", "underlying cause"}, cause},
		{"ResourceURIError without cause", &ResourceURIError{Detail: "just detail"}, []string{"just detail"}, nil},
		{"AttributeMatcherError", &AttributeMatcherError{Key: "department"}, []string{"department"}, nil},
		{"AttributeMatcherCompilationError", &AttributeMatcherCompilationError{Key: "role", Cause: cause}, []string{"role", "underlying cause"}, cause},
		{"ConditionParseError", &ConditionParseError{Expression: "x > 5", Cause: cause}, []string{"x > 5"}, cause},
		{"UnlessConditionParseError", &UnlessConditionParseError{Expression: "role != admin", Cause: cause}, []string{"role != admin"}, cause},
		{"TimeParseError with cause", &TimeParseError{Detail: "invalid time", Cause: cause}, []string{"invalid time", "underlying cause"}, cause},
		{"TimeParseError without cause", &TimeParseError{Detail: "just detail"}, []string{"just detail"}, nil},
		{"UnixTimeFormatError", &UnixTimeFormatError{}, []string{"UnixTime"}, nil},
		{"IDTokenFormatError", &IDTokenFormatError{}, []string{"invalid ID token format"}, nil},
		{"IDTokenDecodeError", &IDTokenDecodeError{Cause: cause}, []string{"underlying cause"}, cause},
		{"IDTokenClaimsError", &IDTokenClaimsError{Cause: cause}, []string{"underlying cause"}, cause},
		{"OIDCConfigRequiredError", &OIDCConfigRequiredError{Field: "issuer"}, []string{"issuer is required"}, nil},
		{"TokenRevokedError", &TokenRevokedError{}, []string{"token is revoked"}, nil},
		{"ConsentIDGenerationError", &ConsentIDGenerationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"ExportIDGenerationError", &ExportIDGenerationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"DeletionIDGenerationError", &DeletionIDGenerationError{Cause: cause}, []string{"underlying cause"}, cause},
		{"ConsentExistsError", &ConsentExistsError{Purpose: "marketing", ConsentID: "c-123"}, []string{"marketing", "c-123"}, nil},
		{"ProviderNameExistsError", &ProviderNameExistsError{Name: "google"}, []string{"google"}, nil},
		{"LinkExternalExistsError", &LinkExternalExistsError{ExternalID: "ext-123"}, []string{"ext-123"}, nil},
		{"FederatedLinkNotFoundError", &FederatedLinkNotFoundError{LinkID: "link-abc"}, []string{"link-abc"}, nil},
		{"StateExpiredDetailError", &StateExpiredDetailError{Token: "tok-xyz"}, []string{"tok-xyz"}, nil},
		{"DiscoveryStatusError", &DiscoveryStatusError{StatusCode: 503}, []string{"503"}, nil},
		{"UserInfoStatusError", &UserInfoStatusError{StatusCode: 401}, []string{"401"}, nil},
		{"NilEventError", &NilEventError{}, []string{"event is nil"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, want := range tt.want {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q, want it to contain %q", msg, want)
				}
			}
			if tt.cause != nil && !errors.Is(tt.err, tt.cause) {
				t.Errorf("errors.Is(%T, cause) = false, want true", tt.err)
			}
		})
	}
}

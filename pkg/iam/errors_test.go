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

// TestErrCompilationFailed_Error tests the Error method for ErrCompilationFailed.
func TestErrCompilationFailed_Error(t *testing.T) {
	tests := []struct {
		name           string
		err            *CompilationFailedError
		expectedOutput string
	}{
		{
			name: "basic compilation error",
			err: &CompilationFailedError{
				Policy: "test-policy",
				Reason: "syntax error at line 5",
			},
			expectedOutput: "compilation failed for policy 'test-policy': syntax error at line 5",
		},
		{
			name: "empty policy name",
			err: &CompilationFailedError{
				Policy: "",
				Reason: "unknown error",
			},
			expectedOutput: "compilation failed for policy '': unknown error",
		},
		{
			name: "complex reason",
			err: &CompilationFailedError{
				Policy: "admin-policy",
				Reason: "invalid action: 'delete:*' is not a valid action pattern for resource 'urn:quicbaas:kv:*'",
			},
			expectedOutput: "compilation failed for policy 'admin-policy': invalid action: 'delete:*' is not a valid action pattern for resource 'urn:quicbaas:kv:*'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expectedOutput {
				t.Errorf("expected %q, got %q", tc.expectedOutput, result)
			}
		})
	}
}

// TestErrInvalidPolicy_Error tests the Error method for InvalidPolicyError.
func TestErrInvalidPolicy_Error(t *testing.T) {
	tests := []struct {
		name           string
		err            *InvalidPolicyError
		expectedOutput string
	}{
		{
			name: "missing actions without name",
			err: &InvalidPolicyError{
				Reason: "actions field is required",
			},
			expectedOutput: "invalid policy: actions field is required",
		},
		{
			name: "empty reason without name",
			err: &InvalidPolicyError{
				Reason: "",
			},
			expectedOutput: "invalid policy: ",
		},
		{
			name: "with policy name",
			err: &InvalidPolicyError{
				Name:   "admin-policy",
				Reason: "missing actions",
			},
			expectedOutput: "invalid policy 'admin-policy': missing actions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expectedOutput {
				t.Errorf("expected %q, got %q", tc.expectedOutput, result)
			}
		})
	}
}

// TestErrPolicyExists_Error tests the Error method for PolicyExistsError.
func TestErrPolicyExists_Error(t *testing.T) {
	err := &PolicyExistsError{Name: "admin-access"}
	expected := "policy 'admin-access' already exists in tenant ''"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestPolicyNotFoundTypeError_Error tests the Error method for PolicyNotFoundTypeError.
func TestPolicyNotFoundTypeError_Error(t *testing.T) {
	t.Run("with tenant and name", func(t *testing.T) {
		err := &PolicyNotFoundTypeError{TenantID: "tenant-1", Name: "viewer-policy"}
		expected := "policy 'viewer-policy' not found in tenant 'tenant-1'"
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("with empty tenant", func(t *testing.T) {
		err := &PolicyNotFoundTypeError{Name: "missing-policy"}
		expected := "policy 'missing-policy' not found in tenant ''"
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})
}

// TestErrPolicyNotFoundType_Error tests the Error method for IAMPolicyNotFoundError.
func TestErrPolicyNotFoundType_Error(t *testing.T) {
	err := &IAMPolicyNotFoundError{PolicyName: "viewer-policy"}
	expected := "policy not found: viewer-policy"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

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

// TestPolicyCompilationError_ErrorAndUnwrap tests Error and Unwrap for PolicyCompilationError.
func TestPolicyCompilationError_ErrorAndUnwrap(t *testing.T) {
	t.Run("error includes policy name and cause", func(t *testing.T) {
		cause := errors.New("syntax error")
		err := &PolicyCompilationError{PolicyName: "broken-policy", Cause: cause}
		expected := "failed to compile policy broken-policy: syntax error"
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("unwrap returns cause", func(t *testing.T) {
		cause := errors.New("inner cause")
		err := &PolicyCompilationError{PolicyName: "test", Cause: cause}
		if err.Unwrap() != cause {
			t.Errorf("expected cause, got %v", err.Unwrap())
		}
		if !errors.Is(err, cause) {
			t.Error("errors.Is should match unwrapped cause")
		}
	})
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

// TestPolicyEvaluationError_ErrorAndUnwrap tests Error and Unwrap for PolicyEvaluationError.
func TestPolicyEvaluationError_ErrorAndUnwrap(t *testing.T) {
	t.Run("error includes policy name and cause", func(t *testing.T) {
		cause := errors.New("runtime error")
		err := &PolicyEvaluationError{PolicyName: "eval-policy", Cause: cause}
		expected := "failed to evaluate policy eval-policy: runtime error"
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("unwrap returns cause", func(t *testing.T) {
		cause := errors.New("eval failure")
		err := &PolicyEvaluationError{PolicyName: "test", Cause: cause}
		if err.Unwrap() != cause {
			t.Errorf("expected cause, got %v", err.Unwrap())
		}
		if !errors.Is(err, cause) {
			t.Error("errors.Is should match unwrapped cause")
		}
	})
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

// TestErrConditionParsing_Error tests the Error method for ConditionParsingError.
func TestErrConditionParsing_Error(t *testing.T) {
	err := &ConditionParsingError{Expression: "time > now()", Reason: "invalid function"}
	expected := "condition parsing failed for 'time > now()': invalid function"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestErrorTypeFields tests basic error type fields.
func TestErrorTypeFields(t *testing.T) {
	t.Run("IAMMessageError", func(t *testing.T) {
		err := &IAMMessageError{Message: "test message"}
		if err.Error() != "test message" {
			t.Errorf("unexpected error: %v", err.Error())
		}
	})

	t.Run("IAMContextError with cause", func(t *testing.T) {
		cause := errors.New("inner error")
		err := &IAMContextError{Message: "outer", Cause: cause}
		if !strings.Contains(err.Error(), "inner error") {
			t.Errorf("expected cause in error, got %q", err.Error())
		}
	})

	t.Run("IAMContextError without cause", func(t *testing.T) {
		err := &IAMContextError{Message: "standalone"}
		if err.Error() != "standalone" {
			t.Errorf("unexpected error: %v", err.Error())
		}
	})
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

// =============================================================================
// IAMContextError.Unwrap
// =============================================================================

func TestIAMContextError_Unwrap(t *testing.T) {
	t.Run("returns cause when set", func(t *testing.T) {
		cause := errors.New("root cause")
		err := &IAMContextError{Message: "context", Cause: cause}
		if err.Unwrap() != cause {
			t.Errorf("expected cause, got %v", err.Unwrap())
		}
	})

	t.Run("returns nil when no cause", func(t *testing.T) {
		err := &IAMContextError{Message: "no cause"}
		if err.Unwrap() != nil {
			t.Errorf("expected nil, got %v", err.Unwrap())
		}
	})
}

// =============================================================================
// Tenant and Quota Error Types
// =============================================================================

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

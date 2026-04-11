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

func TestTenantNotFoundError_Error(t *testing.T) {
	err := &TenantNotFoundError{TenantID: "tenant-abc"}
	if result := err.Error(); result != "tenant not found: tenant-abc" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestInvalidTenantIDError_Error(t *testing.T) {
	err := &InvalidTenantIDError{TenantID: "bad!", Reason: "contains special chars"}
	expected := "invalid tenant ID bad!: contains special chars"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestQuotaExceededError_Error(t *testing.T) {
	err := &QuotaExceededError{Resource: "users", Limit: 100, Current: 101}
	if !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "100") {
		t.Errorf("unexpected error: %q", err.Error())
	}
}

// =============================================================================
// Policy Sync Error
// =============================================================================

func TestPolicySyncError_Error(t *testing.T) {
	cause := errors.New("network timeout")
	err := &PolicySyncError{TenantID: "tenant-1", Cause: cause}
	expected := "failed to sync policies for tenant tenant-1: network timeout"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestPolicySyncError_Unwrap(t *testing.T) {
	cause := errors.New("net error")
	err := &PolicySyncError{TenantID: "t1", Cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

// =============================================================================
// WebAuthn Error Types
// =============================================================================

func TestCredentialNotFoundError_Error(t *testing.T) {
	err := &CredentialNotFoundError{CredentialID: "cred-123"}
	if result := err.Error(); result != "credential not found: cred-123" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestChallengeNotFoundError_Error(t *testing.T) {
	err := &ChallengeNotFoundError{ChallengeID: "ch-456"}
	if result := err.Error(); result != "challenge not found: ch-456" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestChallengeExpiredError_Error(t *testing.T) {
	err := &ChallengeExpiredError{ChallengeID: "ch-789"}
	if result := err.Error(); result != "challenge expired: ch-789" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestWebAuthnRegistrationError_Error(t *testing.T) {
	cause := errors.New("attestation failed")
	err := &WebAuthnRegistrationError{UserID: "user-1", Cause: cause}
	expected := "WebAuthn registration failed for user user-1: attestation failed"
	if result := err.Error(); result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestWebAuthnRegistrationError_Unwrap(t *testing.T) {
	cause := errors.New("inner")
	err := &WebAuthnRegistrationError{UserID: "u1", Cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestWebAuthnAuthenticationError_Error(t *testing.T) {
	cause := errors.New("invalid signature")
	err := &WebAuthnAuthenticationError{Email: "test@example.com", Cause: cause}
	if !strings.Contains(err.Error(), "test@example.com") {
		t.Errorf("expected email in error, got %q", err.Error())
	}
}

func TestWebAuthnAuthenticationError_Unwrap(t *testing.T) {
	cause := errors.New("sig error")
	err := &WebAuthnAuthenticationError{Email: "e@e.com", Cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

// =============================================================================
// Sync/PEP Error Types (lines 1090-1400)
// =============================================================================

func TestUnexpectedResultTypeError_Error(t *testing.T) {
	err := &UnexpectedResultTypeError{ExpectedType: "[]byte", ActualType: "string"}
	if result := err.Error(); !strings.Contains(result, "[]byte") || !strings.Contains(result, "string") {
		t.Errorf("unexpected: %q", result)
	}
}

func TestNilSnapshotError_Error(t *testing.T) {
	err := &NilSnapshotError{}
	if err.Error() != "snapshot is nil" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestMissingPolicyDataError_Error(t *testing.T) {
	err := &MissingPolicyDataError{EventType: "create"}
	if !strings.Contains(err.Error(), "create") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestResourceAlreadyExistsError_Error(t *testing.T) {
	err := &ResourceAlreadyExistsError{}
	if err.Error() != "resource already exists" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestInvalidIndexEntryError_Error(t *testing.T) {
	err := &InvalidIndexEntryError{}
	if err.Error() != "invalid index entry" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestSyncInProgressError_Error(t *testing.T) {
	err := &SyncInProgressError{}
	if err.Error() != "sync already in progress" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestIncrementalSyncDisabledError_Error(t *testing.T) {
	err := &IncrementalSyncDisabledError{}
	if err.Error() != "incremental sync is not enabled" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestVersionConflictError_Error(t *testing.T) {
	err := &VersionConflictError{Current: 5, Incoming: 3}
	if !strings.Contains(err.Error(), "5") || !strings.Contains(err.Error(), "3") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestPolicyCompileFailedError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("bad syntax")
	err := &PolicyCompileFailedError{Cause: cause}
	if !strings.Contains(err.Error(), "bad syntax") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestRaftProposerRequiredError_Error(t *testing.T) {
	err := &RaftProposerRequiredError{}
	if err.Error() != "raft proposer is required for distributed PEP" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestSyncerCreationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("config error")
	err := &SyncerCreationError{Cause: cause}
	if !strings.Contains(err.Error(), "config error") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestSubscribeFailedError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("connection refused")
	err := &SubscribeFailedError{Cause: cause}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestPolicyUnmarshalError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("invalid json")
	err := &PolicyUnmarshalError{Cause: cause}
	if !strings.Contains(err.Error(), "invalid json") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestPolicyMarshalError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("encoding error")
	err := &PolicyMarshalError{Cause: cause}
	if !strings.Contains(err.Error(), "encoding error") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestBaseSyncerCreationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("init failed")
	err := &BaseSyncerCreationError{Cause: cause}
	if !strings.Contains(err.Error(), "init failed") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestBatchProcessorCreationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("bad config")
	err := &BatchProcessorCreationError{Cause: cause}
	if !strings.Contains(err.Error(), "bad config") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestDeltaLogAppendError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("disk full")
	err := &DeltaLogAppendError{Cause: cause}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestConnectionCreationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("refused")
	err := &ConnectionCreationError{Index: 2, Total: 5, Cause: cause}
	if !strings.Contains(err.Error(), "2/5") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestInvalidConnectionTypeError_Error(t *testing.T) {
	err := &InvalidConnectionTypeError{}
	if err.Error() != "invalid connection type" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestFactoryFailedError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("factory err")
	err := &FactoryFailedError{Cause: cause}
	if !strings.Contains(err.Error(), "factory err") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestDeltaLogVersionError_Error(t *testing.T) {
	err := &DeltaLogVersionError{RequestedVersion: 3, MinimumVersion: 10}
	if !strings.Contains(err.Error(), "3") || !strings.Contains(err.Error(), "10") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestCompressorCreationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("no codec")
	err := &CompressorCreationError{Cause: cause}
	if !strings.Contains(err.Error(), "no codec") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestBatchMarshalError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("marshal fail")
	err := &BatchMarshalError{Cause: cause}
	if !strings.Contains(err.Error(), "marshal fail") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestBatchCompressError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("compress fail")
	err := &BatchCompressError{Cause: cause}
	if !strings.Contains(err.Error(), "compress fail") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestBatchNotifyError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("notify fail")
	err := &BatchNotifyError{Cause: cause}
	if !strings.Contains(err.Error(), "notify fail") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

// =============================================================================
// ResourceURI and Compiler Error Types
// =============================================================================

func TestResourceURIError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("parse failed")
		err := &ResourceURIError{Detail: "invalid URI", Cause: cause}
		if !strings.Contains(err.Error(), "invalid URI") || !strings.Contains(err.Error(), "parse failed") {
			t.Errorf("unexpected: %q", err.Error())
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := &ResourceURIError{Detail: "just detail"}
		if err.Error() != "just detail" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestResourceURIError_Unwrap(t *testing.T) {
	cause := errors.New("inner")
	err := &ResourceURIError{Detail: "d", Cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestAttributeMatcherError_Error(t *testing.T) {
	err := &AttributeMatcherError{Key: "department"}
	if !strings.Contains(err.Error(), "department") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestAttributeMatcherCompilationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("bad regex")
	err := &AttributeMatcherCompilationError{Key: "role", Cause: cause}
	if !strings.Contains(err.Error(), "role") || !strings.Contains(err.Error(), "bad regex") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestConditionParseError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("syntax")
	err := &ConditionParseError{Expression: "x > 5", Cause: cause}
	if !strings.Contains(err.Error(), "x > 5") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestUnlessConditionParseError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("invalid op")
	err := &UnlessConditionParseError{Expression: "role != admin", Cause: cause}
	if !strings.Contains(err.Error(), "role != admin") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

// =============================================================================
// Time and Token Error Types
// =============================================================================

func TestTimeParseError_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("bad format")
		err := &TimeParseError{Detail: "invalid time", Cause: cause}
		if !strings.Contains(err.Error(), "invalid time") || !strings.Contains(err.Error(), "bad format") {
			t.Errorf("unexpected: %q", err.Error())
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := &TimeParseError{Detail: "just detail"}
		if err.Error() != "just detail" {
			t.Errorf("unexpected: %q", err.Error())
		}
	})
}

func TestTimeParseError_Unwrap(t *testing.T) {
	cause := errors.New("parse error")
	err := &TimeParseError{Detail: "d", Cause: cause}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestUnixTimeFormatError_Error(t *testing.T) {
	err := &UnixTimeFormatError{}
	if !strings.Contains(err.Error(), "UnixTime") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestIDTokenFormatError_Error(t *testing.T) {
	err := &IDTokenFormatError{}
	if err.Error() != "invalid ID token format" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestIDTokenDecodeError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("base64 error")
	err := &IDTokenDecodeError{Cause: cause}
	if !strings.Contains(err.Error(), "base64 error") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestIDTokenClaimsError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("json error")
	err := &IDTokenClaimsError{Cause: cause}
	if !strings.Contains(err.Error(), "json error") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

// =============================================================================
// OIDC Error Types
// =============================================================================

func TestOIDCConfigRequiredError_Error(t *testing.T) {
	err := &OIDCConfigRequiredError{Field: "issuer"}
	if err.Error() != "issuer is required" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestJWKSGenerationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("key gen failed")
	err := &JWKSGenerationError{Cause: cause}
	if !strings.Contains(err.Error(), "key gen failed") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestTokenRevokedError_Error(t *testing.T) {
	err := &TokenRevokedError{}
	if err.Error() != "token is revoked" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

// =============================================================================
// GDPR/Consent Error Types
// =============================================================================

func TestConsentIDGenerationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("rand error")
	err := &ConsentIDGenerationError{Cause: cause}
	if !strings.Contains(err.Error(), "rand error") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestExportIDGenerationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("gen fail")
	err := &ExportIDGenerationError{Cause: cause}
	if !strings.Contains(err.Error(), "gen fail") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestDeletionIDGenerationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("id gen fail")
	err := &DeletionIDGenerationError{Cause: cause}
	if !strings.Contains(err.Error(), "id gen fail") {
		t.Errorf("unexpected: %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Errorf("unwrap mismatch")
	}
}

func TestConsentExistsError_Error(t *testing.T) {
	err := &ConsentExistsError{Purpose: "marketing", ConsentID: "c-123"}
	if !strings.Contains(err.Error(), "marketing") || !strings.Contains(err.Error(), "c-123") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

// =============================================================================
// Federation Error Types
// =============================================================================

func TestProviderNameExistsError_Error(t *testing.T) {
	err := &ProviderNameExistsError{Name: "google"}
	if !strings.Contains(err.Error(), "google") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestLinkExternalExistsError_Error(t *testing.T) {
	err := &LinkExternalExistsError{ExternalID: "ext-123"}
	if !strings.Contains(err.Error(), "ext-123") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestFederatedLinkNotFoundError_Error(t *testing.T) {
	err := &FederatedLinkNotFoundError{LinkID: "link-abc"}
	if !strings.Contains(err.Error(), "link-abc") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestStateExpiredDetailError_Error(t *testing.T) {
	err := &StateExpiredDetailError{Token: "tok-xyz"}
	if !strings.Contains(err.Error(), "tok-xyz") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestDiscoveryStatusError_Error(t *testing.T) {
	err := &DiscoveryStatusError{StatusCode: 503}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

func TestUserInfoStatusError_Error(t *testing.T) {
	err := &UserInfoStatusError{StatusCode: 401}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("unexpected: %q", err.Error())
	}
}

// =============================================================================
// NilEventError
// =============================================================================

func TestNilEventError_Error(t *testing.T) {
	err := &NilEventError{}
	if err.Error() != "event is nil" {
		t.Errorf("unexpected: %q", err.Error())
	}
}

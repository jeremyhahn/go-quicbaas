// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package errors

import (
	"testing"
)

func TestErrorCode_String(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected string
	}{
		{CodeUnknown, "UNKNOWN"},
		{CodeInvalidInput, "INVALID_INPUT"},
		{CodeNotFound, "NOT_FOUND"},
		{CodeAlreadyExists, "ALREADY_EXISTS"},
		{CodeUnauthorized, "UNAUTHORIZED"},
		{CodeForbidden, "FORBIDDEN"},
		{CodeConflict, "CONFLICT"},
		{CodePreconditionFailed, "PRECONDITION_FAILED"},
		{CodeRateLimited, "RATE_LIMITED"},
		{CodeInternal, "INTERNAL"},
		{CodeUnavailable, "UNAVAILABLE"},
		{CodeTimeout, "TIMEOUT"},
		{CodeCanceled, "CANCELED"},
		{CodeUnimplemented, "UNIMPLEMENTED"},
		{CodeResourceExhausted, "RESOURCE_EXHAUSTED"},
		{CodeSealed, "SEALED"},
		{CodeNotInitialized, "NOT_INITIALIZED"},
		{CodeConfiguration, "CONFIGURATION"},
		{CodeConnection, "CONNECTION"},
		{CodeTransport, "TRANSPORT"},
		{ErrorCode(999), "UNKNOWN"}, // Unknown code
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.code.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestErrorCode_IsRetryable(t *testing.T) {
	retryableCodes := []ErrorCode{
		CodeUnavailable,
		CodeTimeout,
		CodeResourceExhausted,
	}

	nonRetryableCodes := []ErrorCode{
		CodeUnknown,
		CodeInvalidInput,
		CodeNotFound,
		CodeAlreadyExists,
		CodeUnauthorized,
		CodeForbidden,
		CodeConflict,
		CodePreconditionFailed,
		CodeRateLimited,
		CodeInternal,
		CodeCanceled,
		CodeUnimplemented,
		CodeSealed,
		CodeNotInitialized,
		CodeConfiguration,
		CodeConnection,
		CodeTransport,
	}

	for _, code := range retryableCodes {
		t.Run(code.String()+"_retryable", func(t *testing.T) {
			if !code.IsRetryable() {
				t.Errorf("expected %v to be retryable", code)
			}
		})
	}

	for _, code := range nonRetryableCodes {
		t.Run(code.String()+"_not_retryable", func(t *testing.T) {
			if code.IsRetryable() {
				t.Errorf("expected %v to not be retryable", code)
			}
		})
	}
}

func TestErrorCode_IsClientError(t *testing.T) {
	clientErrorCodes := []ErrorCode{
		CodeInvalidInput,
		CodeNotFound,
		CodeAlreadyExists,
		CodeUnauthorized,
		CodeForbidden,
		CodeConflict,
		CodePreconditionFailed,
		CodeRateLimited,
	}

	nonClientErrorCodes := []ErrorCode{
		CodeUnknown,
		CodeInternal,
		CodeUnavailable,
		CodeTimeout,
		CodeCanceled,
		CodeUnimplemented,
		CodeResourceExhausted,
		CodeSealed,
		CodeNotInitialized,
		CodeConfiguration,
		CodeConnection,
		CodeTransport,
	}

	for _, code := range clientErrorCodes {
		t.Run(code.String()+"_is_client", func(t *testing.T) {
			if !code.IsClientError() {
				t.Errorf("expected %v to be client error", code)
			}
		})
	}

	for _, code := range nonClientErrorCodes {
		t.Run(code.String()+"_not_client", func(t *testing.T) {
			if code.IsClientError() {
				t.Errorf("expected %v to not be client error", code)
			}
		})
	}
}

func TestErrorCode_IsServerError(t *testing.T) {
	serverErrorCodes := []ErrorCode{
		CodeInternal,
		CodeUnavailable,
		CodeTimeout,
		CodeUnimplemented,
		CodeResourceExhausted,
	}

	nonServerErrorCodes := []ErrorCode{
		CodeUnknown,
		CodeInvalidInput,
		CodeNotFound,
		CodeAlreadyExists,
		CodeUnauthorized,
		CodeForbidden,
		CodeConflict,
		CodePreconditionFailed,
		CodeRateLimited,
		CodeCanceled,
		CodeSealed,
		CodeNotInitialized,
		CodeConfiguration,
		CodeConnection,
		CodeTransport,
	}

	for _, code := range serverErrorCodes {
		t.Run(code.String()+"_is_server", func(t *testing.T) {
			if !code.IsServerError() {
				t.Errorf("expected %v to be server error", code)
			}
		})
	}

	for _, code := range nonServerErrorCodes {
		t.Run(code.String()+"_not_server", func(t *testing.T) {
			if code.IsServerError() {
				t.Errorf("expected %v to not be server error", code)
			}
		})
	}
}

func TestErrorCodeValues(t *testing.T) {
	// Verify codes are defined in expected order (iota)
	if CodeUnknown != 0 {
		t.Errorf("expected CodeUnknown=0, got %d", CodeUnknown)
	}
	if CodeInvalidInput != 1 {
		t.Errorf("expected CodeInvalidInput=1, got %d", CodeInvalidInput)
	}
	if CodeNotFound != 2 {
		t.Errorf("expected CodeNotFound=2, got %d", CodeNotFound)
	}

	// Verify all codes have unique values
	codes := []ErrorCode{
		CodeUnknown, CodeInvalidInput, CodeNotFound, CodeAlreadyExists,
		CodeUnauthorized, CodeForbidden, CodeConflict, CodePreconditionFailed,
		CodeRateLimited, CodeInternal, CodeUnavailable, CodeTimeout,
		CodeCanceled, CodeUnimplemented, CodeResourceExhausted,
		CodeSealed, CodeNotInitialized, CodeConfiguration,
		CodeConnection, CodeTransport,
	}

	seen := make(map[ErrorCode]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("duplicate error code value: %d (%s)", code, code.String())
		}
		seen[code] = true
	}
}

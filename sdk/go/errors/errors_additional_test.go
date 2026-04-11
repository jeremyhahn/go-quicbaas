// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package errors

import (
	"errors"
	"testing"
)

func TestQBError_Is_NonQBError(t *testing.T) {
	de := New(CodeNotFound, "op", "message")
	regularErr := errors.New("regular error")

	if de.Is(regularErr) {
		t.Error("expected Is to return false for non-QBError")
	}
}

func TestIsRetryable_NonQBError(t *testing.T) {
	regularErr := errors.New("regular error")
	if IsRetryable(regularErr) {
		t.Error("expected IsRetryable to return false for non-QBError")
	}
}

func TestIsRetryable_RetryableQBError(t *testing.T) {
	// Test retryable error
	err := NewUnavailable("op", "server down")
	if !IsRetryable(err) {
		t.Error("expected IsRetryable to return true for Unavailable error")
	}
}

func TestQBError_WithContext_Chaining(t *testing.T) {
	err := New(CodeNotFound, "op", "message").
		WithContext("key1", "value1").
		WithContext("key2", 42).
		WithContext("key3", true)

	if err.Context["key1"] != "value1" {
		t.Error("key1 not set correctly")
	}
	if err.Context["key2"] != 42 {
		t.Error("key2 not set correctly")
	}
	if err.Context["key3"] != true {
		t.Error("key3 not set correctly")
	}
}

func TestQBError_WrappedErrorChain(t *testing.T) {
	innerErr := errors.New("inner error")
	middleErr := Wrap(innerErr, CodeInternal, "middle", "middle message")
	outerErr := Wrap(middleErr, CodeUnavailable, "outer", "outer message")

	// Should be able to unwrap through chain
	if !errors.Is(outerErr, middleErr) {
		t.Error("expected outerErr to wrap middleErr")
	}
	if !errors.Is(outerErr, innerErr) {
		t.Error("expected outerErr to wrap innerErr")
	}
}

func TestNewPreconditionFailed(t *testing.T) {
	err := New(CodePreconditionFailed, "op", "precondition not met")
	if err.Code != CodePreconditionFailed {
		t.Errorf("expected CodePreconditionFailed, got %v", err.Code)
	}
}

func TestNewRateLimited(t *testing.T) {
	err := New(CodeRateLimited, "op", "too many requests")
	if err.Code != CodeRateLimited {
		t.Errorf("expected CodeRateLimited, got %v", err.Code)
	}
}

func TestNewUnimplemented(t *testing.T) {
	err := New(CodeUnimplemented, "op", "not implemented")
	if err.Code != CodeUnimplemented {
		t.Errorf("expected CodeUnimplemented, got %v", err.Code)
	}
}

func TestNewTransport(t *testing.T) {
	err := New(CodeTransport, "op", "transport error")
	if err.Code != CodeTransport {
		t.Errorf("expected CodeTransport, got %v", err.Code)
	}
}

func TestCode_WithWrappedError(t *testing.T) {
	innerErr := NewNotFound("inner", "not found")
	outerErr := Wrap(innerErr, CodeInternal, "outer", "wrapped")

	// Code should return the outer error's code
	if Code(outerErr) != CodeInternal {
		t.Errorf("expected CodeInternal, got %v", Code(outerErr))
	}

	// But inner error is still accessible
	if Code(innerErr) != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", Code(innerErr))
	}
}

func TestOp_WithWrappedError(t *testing.T) {
	innerErr := New(CodeNotFound, "innerOp", "not found")
	outerErr := Wrap(innerErr, CodeInternal, "outerOp", "wrapped")

	if Op(outerErr) != "outerOp" {
		t.Errorf("expected 'outerOp', got %q", Op(outerErr))
	}
}

func TestMessage_WithWrappedError(t *testing.T) {
	innerErr := New(CodeNotFound, "op", "inner message")
	outerErr := Wrap(innerErr, CodeInternal, "op", "outer message")

	if Message(outerErr) != "outer message" {
		t.Errorf("expected 'outer message', got %q", Message(outerErr))
	}
}

func TestIsNotInitialized(t *testing.T) {
	err := NewNotInitialized("op", "server not initialized")
	if Code(err) != CodeNotInitialized {
		t.Errorf("expected CodeNotInitialized, got %v", Code(err))
	}
}

func TestIsConfiguration(t *testing.T) {
	err := NewConfiguration("op", "invalid config")
	if Code(err) != CodeConfiguration {
		t.Errorf("expected CodeConfiguration, got %v", Code(err))
	}
}

func TestQBError_Error_Formats(t *testing.T) {
	// Test error string without underlying error
	err1 := New(CodeNotFound, "Get", "key not found")
	expected1 := "NOT_FOUND: Get: key not found"
	if err1.Error() != expected1 {
		t.Errorf("expected %q, got %q", expected1, err1.Error())
	}

	// Test error string with underlying error
	underlying := errors.New("connection reset")
	err2 := Wrap(underlying, CodeConnection, "Dial", "failed to connect")
	expected2 := "CONNECTION: Dial: failed to connect: connection reset"
	if err2.Error() != expected2 {
		t.Errorf("expected %q, got %q", expected2, err2.Error())
	}
}

func TestErrorCode_DefaultSwitch(t *testing.T) {
	// Test that unknown codes fall through to default case
	unknownCode := ErrorCode(9999)
	if unknownCode.String() != "UNKNOWN" {
		t.Errorf("expected 'UNKNOWN' for unknown code, got %q", unknownCode.String())
	}
}

func TestIsChecks_AllCodes(t *testing.T) {
	tests := []struct {
		name    string
		err     *QBError
		isFn    func(error) bool
		fnName  string
		isMatch bool
	}{
		{"NotFound matches IsNotFound", NewNotFound("op", "msg"), IsNotFound, "IsNotFound", true},
		{"AlreadyExists matches IsAlreadyExists", NewAlreadyExists("op", "msg"), IsAlreadyExists, "IsAlreadyExists", true},
		{"InvalidInput matches IsInvalidInput", NewInvalidInput("op", "msg"), IsInvalidInput, "IsInvalidInput", true},
		{"Unauthorized matches IsUnauthorized", NewUnauthorized("op", "msg"), IsUnauthorized, "IsUnauthorized", true},
		{"Forbidden matches IsForbidden", NewForbidden("op", "msg"), IsForbidden, "IsForbidden", true},
		{"Conflict matches IsConflict", NewConflict("op", "msg"), IsConflict, "IsConflict", true},
		{"Timeout matches IsTimeout", NewTimeout("op", "msg"), IsTimeout, "IsTimeout", true},
		{"Unavailable matches IsUnavailable", NewUnavailable("op", "msg"), IsUnavailable, "IsUnavailable", true},
		{"Canceled matches IsCanceled", NewCanceled("op", "msg"), IsCanceled, "IsCanceled", true},
		{"Sealed matches IsSealed", NewSealed("op", "msg"), IsSealed, "IsSealed", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := tt.isFn(tt.err); result != tt.isMatch {
				t.Errorf("%s(%v) = %v, expected %v", tt.fnName, tt.err, result, tt.isMatch)
			}
		})
	}
}

func TestIsClientError_AllClientCodes(t *testing.T) {
	clientErrors := []*QBError{
		NewInvalidInput("op", "msg"),
		NewNotFound("op", "msg"),
		NewAlreadyExists("op", "msg"),
		NewUnauthorized("op", "msg"),
		NewForbidden("op", "msg"),
		NewConflict("op", "msg"),
		New(CodePreconditionFailed, "op", "msg"),
		New(CodeRateLimited, "op", "msg"),
	}

	for _, err := range clientErrors {
		if !IsClientError(err) {
			t.Errorf("expected %v to be client error", err)
		}
	}
}

func TestIsServerError_AllServerCodes(t *testing.T) {
	serverErrors := []*QBError{
		NewInternal("op", "msg"),
		NewUnavailable("op", "msg"),
		NewTimeout("op", "msg"),
		New(CodeUnimplemented, "op", "msg"),
		NewResourceExhausted("op", "msg"),
	}

	for _, err := range serverErrors {
		if !IsServerError(err) {
			t.Errorf("expected %v to be server error", err)
		}
	}
}

func TestIsRetryable_AllRetryableCodes(t *testing.T) {
	retryableErrors := []*QBError{
		NewUnavailable("op", "msg"),
		NewTimeout("op", "msg"),
		NewResourceExhausted("op", "msg"),
	}

	for _, err := range retryableErrors {
		if !IsRetryable(err) {
			t.Errorf("expected %v to be retryable", err)
		}
	}
}

func TestErrorsAs_QBError(t *testing.T) {
	err := NewNotFound("op", "not found")
	var de *QBError
	if !errors.As(err, &de) {
		t.Error("expected errors.As to match QBError")
	}
	if de.Code != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", de.Code)
	}
}

func TestErrorsAs_WrappedQBError(t *testing.T) {
	inner := errors.New("inner")
	outer := Wrap(inner, CodeInternal, "op", "wrapped")

	var de *QBError
	if !errors.As(outer, &de) {
		t.Error("expected errors.As to match QBError")
	}
	if de.Code != CodeInternal {
		t.Errorf("expected CodeInternal, got %v", de.Code)
	}
}

func TestQBError_NilContext(t *testing.T) {
	err := New(CodeNotFound, "op", "message")
	if err.Context != nil {
		t.Error("expected nil context for new error")
	}

	// WithContext should create context map
	err.WithContext("key", "value")
	if err.Context == nil {
		t.Error("expected non-nil context after WithContext")
	}
}

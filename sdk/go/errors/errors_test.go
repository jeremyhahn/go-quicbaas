// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package errors

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(CodeNotFound, "TestOp", "resource not found")

	if err.Code != CodeNotFound {
		t.Errorf("expected code %v, got %v", CodeNotFound, err.Code)
	}
	if err.Op != "TestOp" {
		t.Errorf("expected op 'TestOp', got %q", err.Op)
	}
	if err.Message != "resource not found" {
		t.Errorf("expected message 'resource not found', got %q", err.Message)
	}
}

func TestWrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := Wrap(underlying, CodeInternal, "WrapOp", "wrapped message")

	if err.Code != CodeInternal {
		t.Errorf("expected code %v, got %v", CodeInternal, err.Code)
	}
	if err.Err != underlying {
		t.Error("expected wrapped error to be preserved")
	}
	if !errors.Is(err, underlying) {
		t.Error("expected errors.Is to find underlying error")
	}
}

func TestQBError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *QBError
		expected string
	}{
		{
			name:     "without underlying error",
			err:      New(CodeNotFound, "Get", "key not found"),
			expected: "NOT_FOUND: Get: key not found",
		},
		{
			name:     "with underlying error",
			err:      Wrap(errors.New("io error"), CodeInternal, "Read", "failed to read"),
			expected: "INTERNAL: Read: failed to read: io error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestQBError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying")
	err := Wrap(underlying, CodeInternal, "Op", "message")

	if unwrapped := err.Unwrap(); unwrapped != underlying {
		t.Error("expected Unwrap to return underlying error")
	}
}

func TestQBError_Is(t *testing.T) {
	err1 := New(CodeNotFound, "Op1", "message1")
	err2 := New(CodeNotFound, "Op2", "message2")
	err3 := New(CodeInternal, "Op3", "message3")

	if !errors.Is(err1, err2) {
		t.Error("expected errors with same code to match")
	}
	if errors.Is(err1, err3) {
		t.Error("expected errors with different codes to not match")
	}
}

func TestQBError_IsRetryable(t *testing.T) {
	tests := []struct {
		code      ErrorCode
		retryable bool
	}{
		{CodeNotFound, false},
		{CodeInvalidInput, false},
		{CodeUnavailable, true},
		{CodeTimeout, true},
		{CodeResourceExhausted, true},
		{CodeInternal, false},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			err := New(tt.code, "Op", "message")
			if got := err.IsRetryable(); got != tt.retryable {
				t.Errorf("expected IsRetryable=%v for %v, got %v", tt.retryable, tt.code, got)
			}
		})
	}
}

func TestQBError_WithContext(t *testing.T) {
	err := New(CodeNotFound, "Op", "message")
	err.WithContext("key", "value")
	err.WithContext("count", 42)

	if err.Context["key"] != "value" {
		t.Error("expected context key to be set")
	}
	if err.Context["count"] != 42 {
		t.Error("expected context count to be set")
	}
}

func TestHelperConstructors(t *testing.T) {
	tests := []struct {
		name     string
		err      *QBError
		expected ErrorCode
	}{
		{"NewInvalidInput", NewInvalidInput("Op", "msg"), CodeInvalidInput},
		{"NewNotFound", NewNotFound("Op", "msg"), CodeNotFound},
		{"NewAlreadyExists", NewAlreadyExists("Op", "msg"), CodeAlreadyExists},
		{"NewUnauthorized", NewUnauthorized("Op", "msg"), CodeUnauthorized},
		{"NewForbidden", NewForbidden("Op", "msg"), CodeForbidden},
		{"NewConflict", NewConflict("Op", "msg"), CodeConflict},
		{"NewInternal", NewInternal("Op", "msg"), CodeInternal},
		{"NewUnavailable", NewUnavailable("Op", "msg"), CodeUnavailable},
		{"NewTimeout", NewTimeout("Op", "msg"), CodeTimeout},
		{"NewCanceled", NewCanceled("Op", "msg"), CodeCanceled},
		{"NewConnection", NewConnection("Op", "msg"), CodeConnection},
		{"NewSealed", NewSealed("Op", "msg"), CodeSealed},
		{"NewNotInitialized", NewNotInitialized("Op", "msg"), CodeNotInitialized},
		{"NewConfiguration", NewConfiguration("Op", "msg"), CodeConfiguration},
		{"NewResourceExhausted", NewResourceExhausted("Op", "msg"), CodeResourceExhausted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.expected {
				t.Errorf("expected code %v, got %v", tt.expected, tt.err.Code)
			}
		})
	}
}

func TestCodeHelpers(t *testing.T) {
	tests := []struct {
		err      error
		fn       func(error) bool
		expected bool
	}{
		{NewNotFound("Op", "msg"), IsNotFound, true},
		{NewInvalidInput("Op", "msg"), IsNotFound, false},
		{NewAlreadyExists("Op", "msg"), IsAlreadyExists, true},
		{NewInvalidInput("Op", "msg"), IsInvalidInput, true},
		{NewUnauthorized("Op", "msg"), IsUnauthorized, true},
		{NewForbidden("Op", "msg"), IsForbidden, true},
		{NewConflict("Op", "msg"), IsConflict, true},
		{NewTimeout("Op", "msg"), IsTimeout, true},
		{NewUnavailable("Op", "msg"), IsUnavailable, true},
		{NewCanceled("Op", "msg"), IsCanceled, true},
		{NewSealed("Op", "msg"), IsSealed, true},
		{NewUnavailable("Op", "msg"), IsRetryable, true},
		{NewNotFound("Op", "msg"), IsRetryable, false},
		{NewInvalidInput("Op", "msg"), IsClientError, true},
		{NewInternal("Op", "msg"), IsClientError, false},
		{NewInternal("Op", "msg"), IsServerError, true},
		{NewNotFound("Op", "msg"), IsServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if got := tt.fn(tt.err); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestCode(t *testing.T) {
	if Code(NewNotFound("Op", "msg")) != CodeNotFound {
		t.Error("expected Code to return NotFound")
	}
	if Code(errors.New("regular error")) != CodeUnknown {
		t.Error("expected Code to return Unknown for non-QBError")
	}
}

func TestOp(t *testing.T) {
	if Op(New(CodeNotFound, "MyOp", "msg")) != "MyOp" {
		t.Error("expected Op to return operation name")
	}
	if Op(errors.New("regular error")) != "" {
		t.Error("expected Op to return empty for non-QBError")
	}
}

func TestMessage(t *testing.T) {
	if Message(New(CodeNotFound, "Op", "my message")) != "my message" {
		t.Error("expected Message to return message")
	}
	if Message(errors.New("regular error")) != "regular error" {
		t.Error("expected Message to return error string for non-QBError")
	}
}

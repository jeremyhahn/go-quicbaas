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

package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQBError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *QBError
		want string
	}{
		{
			name: "with operation and underlying error",
			err: &QBError{
				Code: ErrNotFound,
				Op:   "storage.Get",
				Err:  errors.New("key not found"),
			},
			want: "storage.Get: not_found: key not found",
		},
		{
			name: "with operation only",
			err: &QBError{
				Code: ErrTimeout,
				Op:   "raft.Propose",
			},
			want: "raft.Propose: timeout",
		},
		{
			name: "with underlying error only",
			err: &QBError{
				Code: ErrInternal,
				Err:  errors.New("unexpected error"),
			},
			want: "internal: unexpected error",
		},
		{
			name: "code only",
			err: &QBError{
				Code: ErrConflict,
			},
			want: "conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("QBError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQBError_Is(t *testing.T) {
	baseErr := errors.New("base error")
	qbErr := &QBError{
		Code: ErrNotFound,
		Op:   "test",
		Err:  baseErr,
	}

	tests := []struct {
		name   string
		err    *QBError
		target error
		want   bool
	}{
		{
			name:   "matches same code",
			err:    qbErr,
			target: &QBError{Code: ErrNotFound},
			want:   true,
		},
		{
			name:   "does not match different code",
			err:    qbErr,
			target: &QBError{Code: ErrTimeout},
			want:   false,
		},
		{
			name:   "matches underlying error",
			err:    qbErr,
			target: baseErr,
			want:   true,
		},
		{
			name:   "nil target",
			err:    qbErr,
			target: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Is(tt.target); got != tt.want {
				t.Errorf("QBError.Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQBError_Unwrap(t *testing.T) {
	baseErr := errors.New("base error")
	qbErr := &QBError{
		Code: ErrInternal,
		Err:  baseErr,
	}

	if got := qbErr.Unwrap(); !errors.Is(got, baseErr) {
		t.Errorf("QBError.Unwrap() = %v, want %v", got, baseErr)
	}
}

func TestQBError_WithContext(t *testing.T) {
	err := &QBError{Code: ErrNotFound}
	err = err.WithContext("key", "test-key")
	err = err.WithContext("table", "users")

	if val, ok := err.GetContext("key"); !ok || val != "test-key" {
		t.Errorf("GetContext(key) = %v, %v; want test-key, true", val, ok)
	}

	if val, ok := err.GetContext("table"); !ok || val != "users" {
		t.Errorf("GetContext(table) = %v, %v; want users, true", val, ok)
	}

	if _, ok := err.GetContext("nonexistent"); ok {
		t.Error("GetContext(nonexistent) should return false")
	}
}

func TestQBError_IsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  *QBError
		want bool
	}{
		{
			name: "timeout is retryable",
			err:  &QBError{Code: ErrTimeout},
			want: true,
		},
		{
			name: "unavailable is retryable",
			err:  &QBError{Code: ErrUnavailable},
			want: true,
		},
		{
			name: "not found is not retryable",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "explicit retry flag",
			err:  &QBError{Code: ErrNotFound, Retry: true},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.IsRetryable(); got != tt.want {
				t.Errorf("QBError.IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	err := New(ErrNotFound, "storage.Get", "key not found")

	if err.Code != ErrNotFound {
		t.Errorf("New().Code = %v, want %v", err.Code, ErrNotFound)
	}

	if err.Op != "storage.Get" {
		t.Errorf("New().Op = %v, want %v", err.Op, "storage.Get")
	}

	if err.Error() != "storage.Get: not_found: key not found" {
		t.Errorf("New().Error() = %v, want %v", err.Error(), "storage.Get: not_found: key not found")
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrInvalidInput, "validate", "field %s is required", "username")

	if err.Code != ErrInvalidInput {
		t.Errorf("Newf().Code = %v, want %v", err.Code, ErrInvalidInput)
	}

	wantMsg := "validate: invalid_input: field username is required"
	if err.Error() != wantMsg {
		t.Errorf("Newf().Error() = %v, want %v", err.Error(), wantMsg)
	}
}

func TestWrap(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name    string
		err     error
		code    ErrorCode
		op      string
		wantNil bool
	}{
		{
			name:    "wrap nil returns nil",
			err:     nil,
			code:    ErrInternal,
			op:      "test",
			wantNil: true,
		},
		{
			name:    "wrap standard error",
			err:     baseErr,
			code:    ErrStorage,
			op:      "storage.Get",
			wantNil: false,
		},
		{
			name:    "wrap QBError",
			err:     &QBError{Code: ErrNotFound, Op: "inner"},
			code:    ErrStorage,
			op:      "outer",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrap(tt.err, tt.code, tt.op)

			if tt.wantNil {
				if got != nil {
					t.Errorf("Wrap() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Error("Wrap() = nil, want error")
				return
			}

			var de *QBError
			if !errors.As(got, &de) {
				t.Error("Wrap() did not return QBError")
				return
			}

			if de.Op != tt.op {
				t.Errorf("Wrap().Op = %v, want %v", de.Op, tt.op)
			}
		})
	}
}

func TestIs(t *testing.T) {
	notFoundError := New(ErrNotFound, "test", "not found")
	timeoutErr := New(ErrTimeout, "test", "timeout")

	tests := []struct {
		name string
		err  error
		code ErrorCode
		want bool
	}{
		{
			name: "matches code",
			err:  notFoundError,
			code: ErrNotFound,
			want: true,
		},
		{
			name: "does not match code",
			err:  notFoundError,
			code: ErrTimeout,
			want: false,
		},
		{
			name: "wrapped error preserves original code",
			err:  Wrap(notFoundError, ErrStorage, "outer"),
			code: ErrNotFound, // Wrapping preserves original error code
			want: true,
		},
		{
			name: "nil error",
			err:  nil,
			code: ErrNotFound,
			want: false,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			code: ErrNotFound,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Is(tt.err, tt.code); got != tt.want {
				t.Errorf("Is() = %v, want %v", got, tt.want)
			}
		})
	}

	// Test specific timeout error
	if !Is(timeoutErr, ErrTimeout) {
		t.Error("Is() should return true for timeout error")
	}
}

func TestCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorCode
	}{
		{
			name: "QBError",
			err:  New(ErrNotFound, "test", "not found"),
			want: ErrNotFound,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			want: ErrUnknown,
		},
		{
			name: "nil error",
			err:  nil,
			want: ErrUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Code(tt.err); got != tt.want {
				t.Errorf("Code() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "QBError with op",
			err:  New(ErrNotFound, "storage.Get", "not found"),
			want: "storage.Get",
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			want: "",
		},
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Operation(tt.err); got != tt.want {
				t.Errorf("Operation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "retryable error",
			err:  New(ErrTimeout, "test", "timeout"),
			want: true,
		},
		{
			name: "non-retryable error",
			err:  New(ErrNotFound, "test", "not found"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommonConstructors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		err := NotFound("storage.Get", "key")
		if !Is(err, ErrNotFound) {
			t.Error("NotFound() should create ErrNotFound")
		}
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		err := AlreadyExists("storage.Set", "key")
		if !Is(err, ErrAlreadyExists) {
			t.Error("AlreadyExists() should create ErrAlreadyExists")
		}
	})

	t.Run("InvalidInput", func(t *testing.T) {
		err := InvalidInput("validate", "missing field")
		if !Is(err, ErrInvalidInput) {
			t.Error("InvalidInput() should create ErrInvalidInput")
		}
	})

	t.Run("InvalidInputf", func(t *testing.T) {
		err := InvalidInputf("validate", "field %s is invalid", "username")
		if !Is(err, ErrInvalidInput) {
			t.Error("InvalidInputf() should create ErrInvalidInput")
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		err := Timeout("raft.Propose", "5s")
		if !Is(err, ErrTimeout) {
			t.Error("Timeout() should create ErrTimeout")
		}
		if !IsRetryable(err) {
			t.Error("Timeout() should be retryable")
		}
	})

	t.Run("Internal", func(t *testing.T) {
		err := Internal("process", "unexpected state")
		if !Is(err, ErrInternal) {
			t.Error("Internal() should create ErrInternal")
		}
	})

	t.Run("Unavailable", func(t *testing.T) {
		err := Unavailable("cluster", "no leader")
		if !Is(err, ErrUnavailable) {
			t.Error("Unavailable() should create ErrUnavailable")
		}
		if !IsRetryable(err) {
			t.Error("Unavailable() should be retryable")
		}
	})
}

func TestErrorCode_String(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{ErrNotFound, "not_found"},
		{ErrTimeout, "timeout"},
		{ErrConflict, "conflict"},
		{ErrInternal, "internal"},
		{ErrorCode(9999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("ErrorCode.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorCode_HTTPStatus(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want int
	}{
		{ErrNotFound, 404},
		{ErrConflict, 409},
		{ErrTimeout, 408},
		{ErrInvalidInput, 400},
		{ErrUnauthorized, 401},
		{ErrForbidden, 403},
		{ErrUnavailable, 503},
		{ErrInternal, 500},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			if got := tt.code.HTTPStatus(); got != tt.want {
				t.Errorf("ErrorCode.HTTPStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorCode_IsRetryable(t *testing.T) {
	retryable := []ErrorCode{
		ErrTimeout,
		ErrUnavailable,
		ErrLeaderNotAvailable,
		ErrClusterNotReady,
		ErrDeadlineExceeded,
		ErrResourceExhausted,
		ErrAborted,
	}

	for _, code := range retryable {
		t.Run(code.String(), func(t *testing.T) {
			if !code.IsRetryable() {
				t.Errorf("%s should be retryable", code.String())
			}
		})
	}

	nonRetryable := []ErrorCode{
		ErrNotFound,
		ErrConflict,
		ErrInvalidInput,
		ErrUnauthorized,
		ErrForbidden,
	}

	for _, code := range nonRetryable {
		t.Run(code.String(), func(t *testing.T) {
			if code.IsRetryable() {
				t.Errorf("%s should not be retryable", code.String())
			}
		})
	}
}

func TestQuotaExceededError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *QuotaExceededError
		contains []string
	}{
		{
			name: "with tenant ID",
			err: &QuotaExceededError{
				Resource:  "vectors",
				Namespace: "test-ns",
				TenantID:  "tenant-123",
				Limit:     1000,
				Current:   900,
				Requested: 200,
			},
			contains: []string{"tenant-123", "test-ns", "vectors", "1000", "900", "200"},
		},
		{
			name: "without tenant ID",
			err: &QuotaExceededError{
				Resource:  "storage_bytes",
				Namespace: "prod-ns",
				TenantID:  "",
				Limit:     1000000,
				Current:   950000,
				Requested: 100000,
			},
			contains: []string{"prod-ns", "storage_bytes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			for _, s := range tt.contains {
				if !contains(errStr, s) {
					t.Errorf("Error() = %q, want to contain %q", errStr, s)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewQuotaExceededError(t *testing.T) {
	err := NewQuotaExceededError("vectors", "test-ns", "tenant-abc", 100, 95, 10)

	if err.Code != ErrResourceExhausted {
		t.Errorf("Code = %v, want %v", err.Code, ErrResourceExhausted)
	}

	if err.Op != "quota.check" {
		t.Errorf("Op = %q, want %q", err.Op, "quota.check")
	}

	var qe *QuotaExceededError
	if !errors.As(err.Err, &qe) {
		t.Error("Err should be *QuotaExceededError")
	}

	if qe.Resource != "vectors" {
		t.Errorf("Resource = %q, want %q", qe.Resource, "vectors")
	}
	if qe.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", qe.Namespace, "test-ns")
	}
	if qe.TenantID != "tenant-abc" {
		t.Errorf("TenantID = %q, want %q", qe.TenantID, "tenant-abc")
	}
	if qe.Limit != 100 {
		t.Errorf("Limit = %d, want %d", qe.Limit, 100)
	}
	if qe.Current != 95 {
		t.Errorf("Current = %d, want %d", qe.Current, 95)
	}
	if qe.Requested != 10 {
		t.Errorf("Requested = %d, want %d", qe.Requested, 10)
	}

	// Check context values
	if err.Context["resource"] != "vectors" {
		t.Errorf("Context[resource] = %v, want %q", err.Context["resource"], "vectors")
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "quota exceeded error directly",
			err:  &QuotaExceededError{Resource: "vectors"},
			want: true,
		},
		{
			name: "qb error with resource exhausted code",
			err:  NewQuotaExceededError("storage", "ns", "tenant", 100, 90, 20),
			want: true,
		},
		{
			name: "different error",
			err:  &QBError{Code: ErrNotFound, Op: "test"},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsQuotaExceeded(tt.err); got != tt.want {
				t.Errorf("IsQuotaExceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsResourceExhausted(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "resource exhausted error",
			err:  &QBError{Code: ErrResourceExhausted},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsResourceExhausted(tt.err); got != tt.want {
				t.Errorf("IsResourceExhausted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMissingJustification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "missing justification error",
			err:  NewMissingJustificationError("SwitchTenantContext"),
			want: true,
		},
		{
			name: "qb error with missing justification code",
			err:  &QBError{Code: ErrMissingJustification},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMissingJustification(tt.err); got != tt.want {
				t.Errorf("IsMissingJustification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMissingUserContext(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "missing user context error",
			err:  NewMissingUserContextError("SwitchTenantContext"),
			want: true,
		},
		{
			name: "qb error with missing user context code",
			err:  &QBError{Code: ErrMissingUserContext},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMissingUserContext(tt.err); got != tt.want {
				t.Errorf("IsMissingUserContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewMissingJustificationError(t *testing.T) {
	err := NewMissingJustificationError("SwitchTenantContext")

	if err.Code != ErrMissingJustification {
		t.Errorf("Code = %v, want %v", err.Code, ErrMissingJustification)
	}
	if err.Op != "SwitchTenantContext" {
		t.Errorf("Op = %v, want %v", err.Op, "SwitchTenantContext")
	}
	if err.Err == nil {
		t.Error("Err should not be nil")
	}

	// Test error message format
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return non-empty message")
	}
}

func TestNewMissingUserContextError(t *testing.T) {
	err := NewMissingUserContextError("AuthHandler")

	if err.Code != ErrMissingUserContext {
		t.Errorf("Code = %v, want %v", err.Code, ErrMissingUserContext)
	}
	if err.Op != "AuthHandler" {
		t.Errorf("Op = %v, want %v", err.Op, "AuthHandler")
	}
	if err.Err == nil {
		t.Error("Err should not be nil")
	}

	// Test error message format
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return non-empty message")
	}
}

// =============================================================================
// Consolidated from coverage_gaps_test.go
// =============================================================================

func TestNewSystemKeyspaceViolationError(t *testing.T) {
	t.Run("creates error with correct code and fields", func(t *testing.T) {
		err := NewSystemKeyspaceViolationError("kv.Put", "__system/config")
		require.NotNil(t, err)
		assert.Equal(t, ErrSystemKeyspaceViolation, err.Code)
		assert.Equal(t, "kv.Put", err.Op)
		assert.Contains(t, err.Error(), "__system/config")
		assert.Contains(t, err.Error(), "system keyspace")

		val, ok := err.Context["key"]
		assert.True(t, ok)
		assert.Equal(t, "__system/config", val)
	})

	t.Run("empty key still creates error", func(t *testing.T) {
		err := NewSystemKeyspaceViolationError("op", "")
		require.NotNil(t, err)
		assert.Equal(t, ErrSystemKeyspaceViolation, err.Code)
	})
}

func TestNewCrossTenantAccessError(t *testing.T) {
	t.Run("creates error with correct code and context", func(t *testing.T) {
		err := NewCrossTenantAccessError("kv.Get", "tenant-A", "tenant-B")
		require.NotNil(t, err)
		assert.Equal(t, ErrCrossTenantAccess, err.Code)
		assert.Equal(t, "kv.Get", err.Op)
		assert.Contains(t, err.Error(), "tenant-A")
		assert.Contains(t, err.Error(), "tenant-B")
		assert.Contains(t, err.Error(), "cross-tenant access denied")

		val, ok := err.Context["requesting_tenant"]
		assert.True(t, ok)
		assert.Equal(t, "tenant-A", val)

		val, ok = err.Context["target_tenant"]
		assert.True(t, ok)
		assert.Equal(t, "tenant-B", val)
	})

	t.Run("empty tenants still creates error", func(t *testing.T) {
		err := NewCrossTenantAccessError("op", "", "")
		require.NotNil(t, err)
		assert.Equal(t, ErrCrossTenantAccess, err.Code)
	})
}

func TestNewMissingTenantContextError(t *testing.T) {
	t.Run("creates error with correct code", func(t *testing.T) {
		err := NewMissingTenantContextError("kv.List")
		require.NotNil(t, err)
		assert.Equal(t, ErrMissingTenantContext, err.Code)
		assert.Equal(t, "kv.List", err.Op)
		assert.Contains(t, err.Error(), "tenant context is required")
	})

	t.Run("empty op still creates error", func(t *testing.T) {
		err := NewMissingTenantContextError("")
		require.NotNil(t, err)
		assert.Equal(t, ErrMissingTenantContext, err.Code)
	})
}

func TestIsSystemKeyspaceViolation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "system keyspace violation error",
			err:  NewSystemKeyspaceViolationError("op", "key"),
			want: true,
		},
		{
			name: "qb error with correct code",
			err:  &QBError{Code: ErrSystemKeyspaceViolation},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsSystemKeyspaceViolation(tt.err))
		})
	}
}

func TestIsCrossTenantAccess(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "cross tenant access error",
			err:  NewCrossTenantAccessError("op", "a", "b"),
			want: true,
		},
		{
			name: "qb error with correct code",
			err:  &QBError{Code: ErrCrossTenantAccess},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCrossTenantAccess(tt.err))
		})
	}
}

func TestIsMissingTenantContext(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "missing tenant context error",
			err:  NewMissingTenantContextError("op"),
			want: true,
		},
		{
			name: "qb error with correct code",
			err:  &QBError{Code: ErrMissingTenantContext},
			want: true,
		},
		{
			name: "different error code",
			err:  &QBError{Code: ErrNotFound},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsMissingTenantContext(tt.err))
		})
	}
}

func TestNotImplementedHelper(t *testing.T) {
	t.Run("creates error with correct code", func(t *testing.T) {
		err := NotImplemented("feature.X", "not yet available")
		require.Error(t, err)

		var de *QBError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, ErrNotImplemented, de.Code)
		assert.Equal(t, "feature.X", de.Op)
		assert.Contains(t, err.Error(), "not yet available")
	})

	t.Run("is not retryable", func(t *testing.T) {
		err := NotImplemented("op", "reason")
		assert.False(t, IsRetryable(err))
	})
}

func TestErrorCode_String_RemainingCodes(t *testing.T) {
	// Test codes that were not covered in existing tests
	codes := []struct {
		code ErrorCode
		want string
	}{
		{ErrQuorumLost, "quorum_lost"},
		{ErrSplitBrain, "split_brain"},
		{ErrRateLimited, "rate_limited"},
		{ErrNotImplemented, "not_implemented"},
		{ErrNotAllowed, "not_allowed"},
		{ErrSystemKeyspaceViolation, "system_keyspace_violation"},
		{ErrCrossTenantAccess, "cross_tenant_access"},
		{ErrMissingTenantContext, "missing_tenant_context"},
		{ErrMissingJustification, "missing_justification"},
		{ErrMissingUserContext, "missing_user_context"},
	}

	for _, tc := range codes {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.code.String())
		})
	}
}

func TestErrorCode_HTTPStatus_RemainingCodes(t *testing.T) {
	// Test codes that were not covered in existing tests
	codes := []struct {
		code   ErrorCode
		status int
	}{
		{ErrNotAllowed, 403},
		{ErrSystemKeyspaceViolation, 403},
		{ErrCrossTenantAccess, 403},
		{ErrMissingTenantContext, 400},
		{ErrMissingJustification, 400},
		{ErrMissingUserContext, 400},
		{ErrQuorumLost, 503},
		{ErrNotImplemented, 501},
		{ErrRateLimited, 429},
		{ErrSplitBrain, 500},
	}

	for _, tc := range codes {
		t.Run(tc.code.String(), func(t *testing.T) {
			assert.Equal(t, tc.status, tc.code.HTTPStatus())
		})
	}
}

func TestErrorCode_IsRetryable_RateLimited(t *testing.T) {
	assert.True(t, ErrRateLimited.IsRetryable())
}

// =============================================================================
// Consolidated from errors_additional_test.go
// =============================================================================

func TestWrapf(t *testing.T) {
	t.Run("wrapf with standard error", func(t *testing.T) {
		baseErr := errors.New("base error")
		err := Wrapf(baseErr, ErrStorage, "storage.Get", "failed to get key %s", "test-key")

		require.Error(t, err)

		var de *QBError
		require.ErrorAs(t, err, &de)
		assert.Equal(t, ErrStorage, de.Code)
		assert.Equal(t, "storage.Get", de.Op)
		assert.Contains(t, err.Error(), "failed to get key test-key")
		assert.Contains(t, err.Error(), "base error")
	})

	t.Run("wrapf with nil error", func(t *testing.T) {
		err := Wrapf(nil, ErrStorage, "storage.Get", "some message")
		assert.Nil(t, err)
	})

	t.Run("wrapf preserves retryable", func(t *testing.T) {
		baseErr := errors.New("timeout")
		err := Wrapf(baseErr, ErrTimeout, "api.call", "request timed out")

		require.Error(t, err)
		assert.True(t, IsRetryable(err))
	})
}

func TestQBError_WithRetry(t *testing.T) {
	t.Run("set retry to true", func(t *testing.T) {
		err := &QBError{Code: ErrNotFound}
		assert.False(t, err.IsRetryable())

		err = err.WithRetry(true)
		assert.True(t, err.IsRetryable())
	})

	t.Run("set retry to false", func(t *testing.T) {
		err := &QBError{Code: ErrTimeout, Retry: true}
		assert.True(t, err.IsRetryable())

		err = err.WithRetry(false)
		// Still retryable because code is retryable
		assert.True(t, err.IsRetryable())
	})

	t.Run("explicit retry takes precedence", func(t *testing.T) {
		err := &QBError{Code: ErrNotFound, Retry: false}
		assert.False(t, err.IsRetryable())

		err.Retry = true
		assert.True(t, err.IsRetryable())
	})
}

func TestQBError_GetContext_Nil(t *testing.T) {
	err := &QBError{Code: ErrNotFound, Context: nil}
	val, ok := err.GetContext("key")
	assert.Nil(t, val)
	assert.False(t, ok)
}

func TestAs(t *testing.T) {
	t.Run("successful As", func(t *testing.T) {
		err := New(ErrNotFound, "test", "not found")
		var de *QBError
		ok := As(err, &de)
		assert.True(t, ok)
		assert.Equal(t, ErrNotFound, de.Code)
	})

	t.Run("As with non-QBError", func(t *testing.T) {
		err := errors.New("standard error")
		var de *QBError
		ok := As(err, &de)
		assert.False(t, ok)
	})

	t.Run("As with nil", func(t *testing.T) {
		var de *QBError
		ok := As(nil, &de)
		assert.False(t, ok)
	})
}

func TestGetContext(t *testing.T) {
	t.Run("get context from QBError", func(t *testing.T) {
		err := New(ErrNotFound, "test", "not found")
		err.WithContext("key", "value")
		err.WithContext("number", 42)

		val, ok := GetContext(err, "key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)

		val, ok = GetContext(err, "number")
		assert.True(t, ok)
		assert.Equal(t, 42, val)
	})

	t.Run("get context from non-QBError", func(t *testing.T) {
		err := errors.New("standard error")
		val, ok := GetContext(err, "key")
		assert.False(t, ok)
		assert.Nil(t, val)
	})

	t.Run("get context from nil error", func(t *testing.T) {
		val, ok := GetContext(nil, "key")
		assert.False(t, ok)
		assert.Nil(t, val)
	})

	t.Run("get non-existent context key", func(t *testing.T) {
		err := New(ErrNotFound, "test", "not found")
		val, ok := GetContext(err, "nonexistent")
		assert.False(t, ok)
		assert.Nil(t, val)
	})
}

func TestInternalf(t *testing.T) {
	err := Internalf("process", "unexpected state: %s", "invalid")
	require.Error(t, err)

	var de *QBError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, ErrInternal, de.Code)
	assert.Equal(t, "process", de.Op)
	assert.Contains(t, err.Error(), "unexpected state: invalid")
}

func TestUnavailablef(t *testing.T) {
	err := Unavailablef("cluster", "no leader available in %d nodes", 3)
	require.Error(t, err)

	var de *QBError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, ErrUnavailable, de.Code)
	assert.Equal(t, "cluster", de.Op)
	assert.Contains(t, err.Error(), "no leader available in 3 nodes")
	assert.True(t, IsRetryable(err))
}

func TestErrorCode_String_AllCodes(t *testing.T) {
	codes := []struct {
		code ErrorCode
		want string
	}{
		{ErrUnknown, "unknown"},
		{ErrNotFound, "not_found"},
		{ErrConflict, "conflict"},
		{ErrTimeout, "timeout"},
		{ErrInternal, "internal"},
		{ErrInvalidInput, "invalid_input"},
		{ErrUnauthorized, "unauthorized"},
		{ErrForbidden, "forbidden"},
		{ErrUnavailable, "unavailable"},
		{ErrStorage, "storage"},
		{ErrRaft, "raft"},
		{ErrEncryption, "encryption"},
		{ErrDecryption, "decryption"},
		{ErrValidation, "validation"},
		{ErrConfiguration, "configuration"},
		{ErrCanceled, "canceled"},
		{ErrDeadlineExceeded, "deadline_exceeded"},
		{ErrAlreadyExists, "already_exists"},
		{ErrResourceExhausted, "resource_exhausted"},
		{ErrPreconditionFailed, "precondition_failed"},
		{ErrAborted, "aborted"},
		{ErrOutOfRange, "out_of_range"},
		{ErrUnimplemented, "unimplemented"},
		{ErrDataLoss, "data_loss"},
		{ErrSnapshot, "snapshot"},
		{ErrReplication, "replication"},
		{ErrLeaderNotAvailable, "leader_not_available"},
		{ErrClusterNotReady, "cluster_not_ready"},
	}

	for _, tc := range codes {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.code.String())
		})
	}
}

func TestErrorCode_HTTPStatus_AllCodes(t *testing.T) {
	codes := []struct {
		code   ErrorCode
		status int
	}{
		{ErrNotFound, 404},
		{ErrConflict, 409},
		{ErrAlreadyExists, 409},
		{ErrTimeout, 408},
		{ErrDeadlineExceeded, 408},
		{ErrInvalidInput, 400},
		{ErrValidation, 400},
		{ErrOutOfRange, 400},
		{ErrUnauthorized, 401},
		{ErrForbidden, 403},
		{ErrUnavailable, 503},
		{ErrLeaderNotAvailable, 503},
		{ErrClusterNotReady, 503},
		{ErrUnimplemented, 501},
		{ErrResourceExhausted, 429},
		{ErrPreconditionFailed, 412},
		{ErrInternal, 500},
		{ErrUnknown, 500},
		{ErrStorage, 503},
		{ErrRaft, 503},
		{ErrEncryption, 500},
		{ErrDecryption, 500},
		{ErrConfiguration, 500},
		{ErrCanceled, 500},
		{ErrAborted, 500},
		{ErrDataLoss, 500},
		{ErrSnapshot, 500},
		{ErrReplication, 500},
	}

	for _, tc := range codes {
		t.Run(tc.code.String(), func(t *testing.T) {
			assert.Equal(t, tc.status, tc.code.HTTPStatus())
		})
	}
}

func TestWrap_PreservesContext(t *testing.T) {
	originalErr := New(ErrNotFound, "inner", "not found")
	originalErr.WithContext("key", "value")
	originalErr.WithContext("id", 123)

	wrappedErr := Wrap(originalErr, ErrStorage, "outer")
	require.Error(t, wrappedErr)

	var de *QBError
	require.ErrorAs(t, wrappedErr, &de)
	assert.Equal(t, "outer", de.Op)

	// Context should be preserved
	val, ok := GetContext(wrappedErr, "key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)

	val, ok = GetContext(wrappedErr, "id")
	assert.True(t, ok)
	assert.Equal(t, 123, val)
}

func TestWrap_PreservesRetry(t *testing.T) {
	originalErr := New(ErrNotFound, "inner", "not found")
	originalErr.WithRetry(true)

	wrappedErr := Wrap(originalErr, ErrStorage, "outer")
	require.Error(t, wrappedErr)

	assert.True(t, IsRetryable(wrappedErr))
}

func TestQBError_IsWithNilUnderlying(t *testing.T) {
	err := &QBError{Code: ErrNotFound, Err: nil}
	standardErr := errors.New("standard")

	assert.False(t, err.Is(standardErr))
	assert.True(t, err.Is(&QBError{Code: ErrNotFound}))
}

// =============================================================================
// Consolidated from helpers_coverage_test.go
// =============================================================================

func TestIsNotFound_Helper(t *testing.T) {
	// Test with NotFound error
	err := NotFound("test.Op", "resource")
	assert.True(t, IsNotFound(err))

	// Test with nil
	assert.False(t, IsNotFound(nil))

	// Test with different error code
	err2 := New(ErrStorage, "test.Op", "storage error")
	assert.False(t, IsNotFound(err2))
}

func TestIsCanceled_Helper(t *testing.T) {
	// Test with Canceled error
	err := New(ErrCanceled, "test.Op", "canceled")
	assert.True(t, IsCanceled(err))

	// Test with nil
	assert.False(t, IsCanceled(nil))

	// Test with different error code
	err2 := New(ErrTimeout, "test.Op", "timeout")
	assert.False(t, IsCanceled(err2))
}

func TestIsInvalidInput_Helper(t *testing.T) {
	// Test with InvalidInput error
	err := InvalidInput("test.Op", "invalid input")
	assert.True(t, IsInvalidInput(err))

	// Test with nil
	assert.False(t, IsInvalidInput(nil))

	// Test with different error code
	err2 := New(ErrStorage, "test.Op", "storage error")
	assert.False(t, IsInvalidInput(err2))
}

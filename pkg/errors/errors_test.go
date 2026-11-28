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
			// Wrap RECLASSIFIES: the result reports the caller's code, and the
			// inner classification stays reachable only through Unwrap
			// (go-qrdb#187, owner-ruled 2026-09-02).
			name: "wrapped error reports the caller's code",
			err:  Wrap(notFoundError, ErrStorage, "outer"),
			code: ErrStorage,
			want: true,
		},
		{
			name: "wrapped error no longer reports the inner code",
			err:  Wrap(notFoundError, ErrStorage, "outer"),
			code: ErrNotFound,
			want: false,
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
	tests := []struct {
		name    string
		err     error
		code    ErrorCode
		op      string
		message string
	}{
		{"NotFound", NotFound("storage.Get", "key"), ErrNotFound, "storage.Get", "key"},
		{"AlreadyExists", AlreadyExists("storage.Set", "key"), ErrAlreadyExists, "storage.Set", "key"},
		{"InvalidInput", InvalidInput("validate", "missing field"), ErrInvalidInput, "validate", "missing field"},
		{"InvalidInputf", InvalidInputf("validate", "field %s is invalid", "username"), ErrInvalidInput, "validate", "field username is invalid"},
		{"Timeout", Timeout("raft.Propose", "5s"), ErrTimeout, "raft.Propose", "5s"},
		{"Internal", Internal("process", "unexpected state"), ErrInternal, "process", "unexpected state"},
		{"Internalf", Internalf("process", "unexpected state: %s", "invalid"), ErrInternal, "process", "unexpected state: invalid"},
		{"Unavailable", Unavailable("cluster", "no leader"), ErrUnavailable, "cluster", "no leader"},
		{"Unavailablef", Unavailablef("cluster", "no leader available in %d nodes", 3), ErrUnavailable, "cluster", "no leader available in 3 nodes"},
		{"NotImplemented", NotImplemented("feature.X", "not yet available"), ErrNotImplemented, "feature.X", "not yet available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var de *QBError
			require.ErrorAs(t, tt.err, &de)
			assert.Equal(t, tt.code, de.Code)
			assert.Equal(t, tt.op, de.Op)
			assert.Contains(t, tt.err.Error(), tt.message)
		})
	}
}

func TestErrorCode_String(t *testing.T) {
	tests := []struct {
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
		{ErrorCode(9999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.String())
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
		{ErrAlreadyExists, 409},
		{ErrTimeout, 408},
		{ErrDeadlineExceeded, 408},
		{ErrInvalidInput, 400},
		{ErrValidation, 400},
		{ErrOutOfRange, 400},
		{ErrMissingTenantContext, 400},
		{ErrMissingJustification, 400},
		{ErrMissingUserContext, 400},
		{ErrUnauthorized, 401},
		{ErrForbidden, 403},
		{ErrNotAllowed, 403},
		{ErrSystemKeyspaceViolation, 403},
		{ErrCrossTenantAccess, 403},
		{ErrPreconditionFailed, 412},
		{ErrResourceExhausted, 429},
		{ErrRateLimited, 429},
		{ErrInternal, 500},
		{ErrUnknown, 500},
		{ErrEncryption, 500},
		{ErrDecryption, 500},
		{ErrConfiguration, 500},
		{ErrCanceled, 500},
		{ErrAborted, 500},
		{ErrDataLoss, 500},
		{ErrSnapshot, 500},
		{ErrReplication, 500},
		{ErrSplitBrain, 500},
		{ErrUnimplemented, 501},
		{ErrNotImplemented, 501},
		{ErrUnavailable, 503},
		{ErrLeaderNotAvailable, 503},
		{ErrClusterNotReady, 503},
		{ErrStorage, 503},
		{ErrRaft, 503},
		{ErrQuorumLost, 503},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.HTTPStatus())
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
		ErrRateLimited,
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
				assert.Contains(t, errStr, s)
			}
		})
	}
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

// TestIsPredicates proves each category predicate is bound: it matches its
// own error and rejects a different code and nil.
func TestIsPredicates(t *testing.T) {
	tests := []struct {
		name  string
		is    func(error) bool
		match error
		other error
	}{
		{"IsNotFound", IsNotFound, NotFound("test.Op", "resource"), New(ErrStorage, "test.Op", "storage error")},
		{"IsCanceled", IsCanceled, New(ErrCanceled, "test.Op", "canceled"), New(ErrTimeout, "test.Op", "timeout")},
		{"IsInvalidInput", IsInvalidInput, InvalidInput("test.Op", "invalid input"), New(ErrStorage, "test.Op", "storage error")},
		{"IsResourceExhausted", IsResourceExhausted, &QBError{Code: ErrResourceExhausted}, &QBError{Code: ErrNotFound}},
		{"IsQuotaExceeded", IsQuotaExceeded, NewQuotaExceededError("storage", "ns", "tenant", 100, 90, 20), &QBError{Code: ErrNotFound}},
		{"IsQuotaExceeded direct", IsQuotaExceeded, &QuotaExceededError{Resource: "vectors"}, errors.New("some error")},
		{"IsMissingJustification", IsMissingJustification, NewMissingJustificationError("SwitchTenantContext"), &QBError{Code: ErrNotFound}},
		{"IsMissingUserContext", IsMissingUserContext, NewMissingUserContextError("SwitchTenantContext"), &QBError{Code: ErrNotFound}},
		{"IsSystemKeyspaceViolation", IsSystemKeyspaceViolation, NewSystemKeyspaceViolationError("op", "key"), &QBError{Code: ErrNotFound}},
		{"IsCrossTenantAccess", IsCrossTenantAccess, NewCrossTenantAccessError("op", "a", "b"), &QBError{Code: ErrNotFound}},
		{"IsMissingTenantContext", IsMissingTenantContext, NewMissingTenantContextError("op"), &QBError{Code: ErrNotFound}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.is(tt.match))
			assert.False(t, tt.is(tt.other))
			assert.False(t, tt.is(nil))
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
}

func TestNewMissingTenantContextError(t *testing.T) {
	t.Run("creates error with correct code", func(t *testing.T) {
		err := NewMissingTenantContextError("kv.List")
		require.NotNil(t, err)
		assert.Equal(t, ErrMissingTenantContext, err.Code)
		assert.Equal(t, "kv.List", err.Op)
		assert.Contains(t, err.Error(), "tenant context is required")
	})
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
}

func TestQBError_WithRetry(t *testing.T) {
	err := &QBError{Code: ErrNotFound}
	assert.False(t, err.IsRetryable())

	err = err.WithRetry(true)
	assert.True(t, err.IsRetryable())
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
}

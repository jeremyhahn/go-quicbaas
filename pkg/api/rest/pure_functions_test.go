//go:build rest

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

package rest

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Validation Helper Function Tests (from transaction_types.go)
// =============================================================================

func TestIsValidCompareTargetExhaustive(t *testing.T) {
	tests := []struct {
		name     string
		target   CompareTarget
		expected bool
	}{
		// Valid targets
		{
			name:     "valid target version",
			target:   CompareTargetVersion,
			expected: true,
		},
		{
			name:     "valid target value",
			target:   CompareTargetValue,
			expected: true,
		},
		{
			name:     "valid target exists",
			target:   CompareTargetExists,
			expected: true,
		},
		// Invalid targets
		{
			name:     "invalid empty string",
			target:   CompareTarget(""),
			expected: false,
		},
		{
			name:     "invalid random string",
			target:   CompareTarget("invalid"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity VERSION",
			target:   CompareTarget("VERSION"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity Value",
			target:   CompareTarget("Value"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity EXISTS",
			target:   CompareTarget("EXISTS"),
			expected: false,
		},
		{
			name:     "invalid partial match ver",
			target:   CompareTarget("ver"),
			expected: false,
		},
		{
			name:     "invalid with whitespace",
			target:   CompareTarget(" version "),
			expected: false,
		},
		{
			name:     "invalid special characters",
			target:   CompareTarget("version!"),
			expected: false,
		},
		{
			name:     "valid unicode normalized",
			target:   CompareTarget("versio\u006E"),
			expected: true, // Unicode normalization should work for 'n'
		},
		{
			name:     "invalid null-like",
			target:   CompareTarget("null"),
			expected: false,
		},
		{
			name:     "invalid with newline",
			target:   CompareTarget("version\n"),
			expected: false,
		},
		{
			name:     "invalid with tab",
			target:   CompareTarget("version\t"),
			expected: false,
		},
		{
			name:     "invalid mixed case Version",
			target:   CompareTarget("Version"),
			expected: false,
		},
		{
			name:     "invalid similar typo versoin", //nolint:misspell // the misspelling is the case being described
			target:   CompareTarget("versoin"),       //nolint:misspell // the misspelling is the test input
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCompareTarget(tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidCompareOperatorExhaustive(t *testing.T) {
	tests := []struct {
		name     string
		operator CompareOperator
		expected bool
	}{
		// Valid operators
		{
			name:     "valid operator equal",
			operator: CompareOpEqual,
			expected: true,
		},
		{
			name:     "valid operator not_equal",
			operator: CompareOpNotEqual,
			expected: true,
		},
		{
			name:     "valid operator greater",
			operator: CompareOpGreater,
			expected: true,
		},
		{
			name:     "valid operator less",
			operator: CompareOpLess,
			expected: true,
		},
		{
			name:     "valid operator greater_equal",
			operator: CompareOpGreaterEq,
			expected: true,
		},
		{
			name:     "valid operator less_equal",
			operator: CompareOpLessEq,
			expected: true,
		},
		// Invalid operators
		{
			name:     "invalid empty string",
			operator: CompareOperator(""),
			expected: false,
		},
		{
			name:     "invalid random string",
			operator: CompareOperator("invalid"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity EQUAL",
			operator: CompareOperator("EQUAL"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity Equal",
			operator: CompareOperator("Equal"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator eq",
			operator: CompareOperator("eq"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator ne",
			operator: CompareOperator("ne"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator gt",
			operator: CompareOperator("gt"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator lt",
			operator: CompareOperator("lt"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator gte",
			operator: CompareOperator("gte"),
			expected: false,
		},
		{
			name:     "invalid SQL-like operator lte",
			operator: CompareOperator("lte"),
			expected: false,
		},
		{
			name:     "invalid symbol ==",
			operator: CompareOperator("=="),
			expected: false,
		},
		{
			name:     "invalid symbol !=",
			operator: CompareOperator("!="),
			expected: false,
		},
		{
			name:     "invalid symbol >",
			operator: CompareOperator(">"),
			expected: false,
		},
		{
			name:     "invalid symbol <",
			operator: CompareOperator("<"),
			expected: false,
		},
		{
			name:     "invalid symbol >=",
			operator: CompareOperator(">="),
			expected: false,
		},
		{
			name:     "invalid symbol <=",
			operator: CompareOperator("<="),
			expected: false,
		},
		{
			name:     "invalid with whitespace",
			operator: CompareOperator(" equal "),
			expected: false,
		},
		{
			name:     "invalid underscore typo not-equal",
			operator: CompareOperator("not-equal"),
			expected: false,
		},
		{
			name:     "invalid camelCase notEqual",
			operator: CompareOperator("notEqual"),
			expected: false,
		},
		{
			name:     "invalid with newline",
			operator: CompareOperator("equal\n"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCompareOperator(tt.operator)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidTransactionOpTypeExhaustive(t *testing.T) {
	tests := []struct {
		name     string
		opType   TransactionOpType
		expected bool
	}{
		// Valid operation types
		{
			name:     "valid type put",
			opType:   TxnOpPut,
			expected: true,
		},
		{
			name:     "valid type delete",
			opType:   TxnOpDelete,
			expected: true,
		},
		{
			name:     "valid type get",
			opType:   TxnOpGet,
			expected: true,
		},
		// Invalid operation types
		{
			name:     "invalid empty string",
			opType:   TransactionOpType(""),
			expected: false,
		},
		{
			name:     "invalid random string",
			opType:   TransactionOpType("invalid"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity PUT",
			opType:   TransactionOpType("PUT"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity Put",
			opType:   TransactionOpType("Put"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity DELETE",
			opType:   TransactionOpType("DELETE"),
			expected: false,
		},
		{
			name:     "invalid case sensitivity GET",
			opType:   TransactionOpType("GET"),
			expected: false,
		},
		{
			name:     "invalid CRUD update",
			opType:   TransactionOpType("update"),
			expected: false,
		},
		{
			name:     "invalid CRUD create",
			opType:   TransactionOpType("create"),
			expected: false,
		},
		{
			name:     "invalid CRUD patch",
			opType:   TransactionOpType("patch"),
			expected: false,
		},
		{
			name:     "invalid HTTP method POST",
			opType:   TransactionOpType("POST"),
			expected: false,
		},
		{
			name:     "invalid with whitespace",
			opType:   TransactionOpType(" put "),
			expected: false,
		},
		{
			name:     "invalid partial match p",
			opType:   TransactionOpType("p"),
			expected: false,
		},
		{
			name:     "invalid insert alias",
			opType:   TransactionOpType("insert"),
			expected: false,
		},
		{
			name:     "invalid remove alias",
			opType:   TransactionOpType("remove"),
			expected: false,
		},
		{
			name:     "invalid read alias",
			opType:   TransactionOpType("read"),
			expected: false,
		},
		{
			name:     "invalid write alias",
			opType:   TransactionOpType("write"),
			expected: false,
		},
		{
			name:     "invalid with newline",
			opType:   TransactionOpType("put\n"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidTransactionOpType(tt.opType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// IAM Error Tests (from iam_errors.go)
// =============================================================================

func TestOAuthErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		err      *OAuthError
		expected string
	}{
		{
			name: "error with description",
			err: &OAuthError{
				Code:             ErrInvalidRequest,
				ErrorDescription: "missing required parameter",
			},
			expected: "invalid_request: missing required parameter",
		},
		{
			name: "error without description",
			err: &OAuthError{
				Code: ErrInvalidClient,
			},
			expected: "invalid_client",
		},
		{
			name: "error with empty description",
			err: &OAuthError{
				Code:             ErrInvalidGrant,
				ErrorDescription: "",
			},
			expected: "invalid_grant",
		},
		{
			name: "error with all fields",
			err: &OAuthError{
				Code:             ErrUnauthorizedClient,
				ErrorDescription: "client not authorized",
				ErrorURI:         "https://example.com/errors",
				State:            "abc123",
			},
			expected: "unauthorized_client: client not authorized",
		},
		{
			name: "unsupported_grant_type error",
			err: &OAuthError{
				Code:             ErrUnsupportedGrantType,
				ErrorDescription: "authorization_code not supported",
			},
			expected: "unsupported_grant_type: authorization_code not supported",
		},
		{
			name: "invalid_scope error",
			err: &OAuthError{
				Code:             ErrInvalidScope,
				ErrorDescription: "requested scope exceeds granted scope",
			},
			expected: "invalid_scope: requested scope exceeds granted scope",
		},
		{
			name: "access_denied error",
			err: &OAuthError{
				Code:             ErrAccessDenied,
				ErrorDescription: "user denied consent",
			},
			expected: "access_denied: user denied consent",
		},
		{
			name: "unsupported_response_type error",
			err: &OAuthError{
				Code:             ErrUnsupportedResponseType,
				ErrorDescription: "token response type not supported",
			},
			expected: "unsupported_response_type: token response type not supported",
		},
		{
			name: "server_error",
			err: &OAuthError{
				Code:             ErrServerError,
				ErrorDescription: "internal server failure",
			},
			expected: "server_error: internal server failure",
		},
		{
			name: "temporarily_unavailable error",
			err: &OAuthError{
				Code:             ErrTemporarilyUnavailable,
				ErrorDescription: "service maintenance",
			},
			expected: "temporarily_unavailable: service maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewOAuthErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		description string
	}{
		{
			name:        "basic error",
			code:        ErrInvalidRequest,
			description: "missing parameter",
		},
		{
			name:        "empty description",
			code:        ErrInvalidClient,
			description: "",
		},
		{
			name:        "long description",
			code:        ErrInvalidGrant,
			description: "this is a very long description that explains in detail what went wrong with the grant process and why it failed",
		},
		{
			name:        "description with special characters",
			code:        ErrAccessDenied,
			description: "access denied: user@example.com (reason: 403)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewOAuthError(tt.code, tt.description)

			require.NotNil(t, err)
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
			assert.Contains(t, err.ErrorURI, "https://docs.quicbaas.io/iam/errors#")
			assert.Contains(t, err.ErrorURI, tt.code)
		})
	}
}

func TestNewInvalidRequestErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "missing parameter",
			description: "missing required parameter 'client_id'",
		},
		{
			name:        "malformed request",
			description: "request body is not valid JSON",
		},
		{
			name:        "empty description",
			description: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInvalidRequestError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrInvalidRequest, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewInvalidClientErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "unknown client",
			description: "client not found",
		},
		{
			name:        "invalid credentials",
			description: "client authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInvalidClientError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrInvalidClient, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewInvalidGrantErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "expired code",
			description: "authorization code has expired",
		},
		{
			name:        "revoked token",
			description: "refresh token has been revoked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInvalidGrantError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrInvalidGrant, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewUnauthorizedClientErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "grant type not allowed",
			description: "client is not authorized for this grant type",
		},
		{
			name:        "redirect URI mismatch",
			description: "redirect_uri does not match registered URIs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewUnauthorizedClientError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrUnauthorizedClient, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewUnsupportedGrantTypeErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "unknown grant type",
			description: "grant_type 'implicit' is not supported",
		},
		{
			name:        "disabled grant type",
			description: "password grant is disabled on this server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewUnsupportedGrantTypeError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrUnsupportedGrantType, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewInvalidScopeErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "unknown scope",
			description: "scope 'admin' is not recognized",
		},
		{
			name:        "exceeds allowed",
			description: "requested scope exceeds granted scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInvalidScopeError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrInvalidScope, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewAccessDeniedErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "user denied",
			description: "user denied the authorization request",
		},
		{
			name:        "policy violation",
			description: "request violates security policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAccessDeniedError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrAccessDenied, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewUnsupportedResponseTypeErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "unknown response type",
			description: "response_type 'token' is not supported",
		},
		{
			name:        "combination not allowed",
			description: "response_type combination not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewUnsupportedResponseTypeError(tt.description)

			require.NotNil(t, err)
			assert.Equal(t, ErrUnsupportedResponseType, err.Code)
			assert.Equal(t, tt.description, err.ErrorDescription)
		})
	}
}

func TestNewIAMAPIErrorFactory(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		description string
	}{
		{
			name:        "bad request",
			code:        ErrBadRequest,
			description: "invalid request format",
		},
		{
			name:        "unauthorized",
			code:        ErrUnauthorized,
			description: "authentication required",
		},
		{
			name:        "forbidden",
			code:        ErrForbidden,
			description: "insufficient permissions",
		},
		{
			name:        "not found",
			code:        ErrNotFound,
			description: "resource not found",
		},
		{
			name:        "conflict",
			code:        ErrConflict,
			description: "resource already exists",
		},
		{
			name:        "rate limited",
			code:        ErrRateLimited,
			description: "too many requests",
		},
		{
			name:        "internal error",
			code:        ErrInternalError,
			description: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewIAMAPIError(tt.code, tt.description)

			require.NotNil(t, err)
			assert.Equal(t, tt.code, err.Error)
			assert.Equal(t, tt.description, err.ErrorDescription)
			assert.Contains(t, err.ErrorURI, "https://docs.quicbaas.io/iam/errors#")
			assert.Contains(t, err.ErrorURI, tt.code)
		})
	}
}

// =============================================================================
// Middleware Error Type Tests (from middleware.go)
// =============================================================================

func TestInternalServerErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "basic message",
			message:  "internal server error",
			expected: "internal server error",
		},
		{
			name:     "empty message",
			message:  "",
			expected: "",
		},
		{
			name:     "detailed message",
			message:  "database connection failed: timeout after 30s",
			expected: "database connection failed: timeout after 30s",
		},
		{
			name:     "message with special characters",
			message:  "error: connection refused (127.0.0.1:5432)",
			expected: "error: connection refused (127.0.0.1:5432)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &InternalServerError{Message: tt.message}
			result := err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRequestTimeoutErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected string
	}{
		{
			name:     "30 second timeout",
			timeout:  30 * time.Second,
			expected: "request timeout after 30s",
		},
		{
			name:     "1 minute timeout",
			timeout:  time.Minute,
			expected: "request timeout after 1m0s",
		},
		{
			name:     "500ms timeout",
			timeout:  500 * time.Millisecond,
			expected: "request timeout after 500ms",
		},
		{
			name:     "zero timeout",
			timeout:  0,
			expected: "request timeout after 0s",
		},
		{
			name:     "complex duration",
			timeout:  2*time.Minute + 30*time.Second,
			expected: "request timeout after 2m30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &RequestTimeoutError{Timeout: tt.timeout}
			result := err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRateLimitErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		period   string
		expected string
	}{
		{
			name:     "100 requests per second",
			limit:    100,
			period:   "1s",
			expected: "rate limit exceeded: 100 requests per 1s",
		},
		{
			name:     "1000 requests per minute",
			limit:    1000,
			period:   "1m",
			expected: "rate limit exceeded: 1000 requests per 1m",
		},
		{
			name:     "1 request per hour",
			limit:    1,
			period:   "1h",
			expected: "rate limit exceeded: 1 requests per 1h",
		},
		{
			name:     "zero limit",
			limit:    0,
			period:   "1s",
			expected: "rate limit exceeded: 0 requests per 1s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &RateLimitError{
				Limit:  tt.limit,
				Period: tt.period,
			}
			result := err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidationErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		message  string
		expected string
	}{
		{
			name:     "with field",
			field:    "email",
			message:  "invalid format",
			expected: "validation error for email: invalid format",
		},
		{
			name:     "without field",
			field:    "",
			message:  "request body is required",
			expected: "validation error: request body is required",
		},
		{
			name:     "nested field",
			field:    "user.address.zip_code",
			message:  "must be 5 digits",
			expected: "validation error for user.address.zip_code: must be 5 digits",
		},
		{
			name:     "array index field",
			field:    "items[0].quantity",
			message:  "must be positive",
			expected: "validation error for items[0].quantity: must be positive",
		},
		{
			name:     "empty message with field",
			field:    "name",
			message:  "",
			expected: "validation error for name: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ValidationError{
				Field:   tt.field,
				Message: tt.message,
			}
			result := err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// getClientIP Function Tests (from middleware.go)
// =============================================================================

func TestGetClientIPExhaustive(t *testing.T) {
	tests := []struct {
		name          string
		xForwardedFor string
		xRealIP       string
		remoteAddr    string
		expectedIP    string
	}{
		// X-Forwarded-For tests
		{
			name:          "X-Forwarded-For single IP",
			xForwardedFor: "192.168.1.100",
			remoteAddr:    "10.0.0.1:12345",
			expectedIP:    "192.168.1.100",
		},
		{
			name:          "X-Forwarded-For multiple IPs",
			xForwardedFor: "203.0.113.195, 70.41.3.18, 150.172.238.178",
			remoteAddr:    "10.0.0.1:12345",
			expectedIP:    "203.0.113.195",
		},
		{
			name:          "X-Forwarded-For with spaces",
			xForwardedFor: "  192.168.1.100  , 10.0.0.1",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    "192.168.1.100",
		},
		{
			name:          "X-Forwarded-For IPv6",
			xForwardedFor: "2001:db8:85a3::8a2e:370:7334",
			remoteAddr:    "10.0.0.1:12345",
			expectedIP:    "2001:db8:85a3::8a2e:370:7334",
		},
		// X-Real-IP tests
		{
			name:       "X-Real-IP only",
			xRealIP:    "192.168.1.100",
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Real-IP with whitespace",
			xRealIP:    "  192.168.1.100  ",
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Real-IP IPv6",
			xRealIP:    "::1",
			remoteAddr: "127.0.0.1:8080",
			expectedIP: "::1",
		},
		// X-Forwarded-For takes precedence over X-Real-IP
		{
			name:          "X-Forwarded-For precedence over X-Real-IP",
			xForwardedFor: "10.0.0.50",
			xRealIP:       "192.168.1.100",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    "10.0.0.50",
		},
		// RemoteAddr fallback tests
		{
			name:       "RemoteAddr with port",
			remoteAddr: "192.168.1.100:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "192.168.1.100",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "RemoteAddr IPv6 with port in brackets",
			remoteAddr: "[2001:db8::1]:12345",
			expectedIP: "[2001:db8::1]", // LastIndex finds : after bracket, strips port correctly
		},
		{
			name:       "RemoteAddr localhost",
			remoteAddr: "127.0.0.1:8080",
			expectedIP: "127.0.0.1",
		},
		{
			name:       "RemoteAddr empty",
			remoteAddr: "",
			expectedIP: "",
		},
		// Edge cases
		{
			name:          "empty X-Forwarded-For falls back to X-Real-IP",
			xForwardedFor: "",
			xRealIP:       "10.0.0.1",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    "10.0.0.1",
		},
		{
			name:          "empty X-Forwarded-For and X-Real-IP",
			xForwardedFor: "",
			xRealIP:       "",
			remoteAddr:    "192.168.1.1:443",
			expectedIP:    "192.168.1.1",
		},
		{
			name:          "X-Forwarded-For with only comma returns trimmed comma",
			xForwardedFor: ",",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    ",", // Index returns 0, condition idx > 0 false, returns TrimSpace(",") = ","
		},
		{
			name:          "X-Forwarded-For with multiple commas",
			xForwardedFor: "192.168.1.1, , 10.0.0.1",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    "192.168.1.1",
		},
		{
			name:          "X-Forwarded-For single IP with trailing comma",
			xForwardedFor: "192.168.1.1,",
			remoteAddr:    "127.0.0.1:8080",
			expectedIP:    "192.168.1.1",
		},
		{
			name:       "RemoteAddr only colon returns colon",
			remoteAddr: ":",
			expectedIP: ":", // LastIndex returns 0, condition idx > 0 false, returns r.RemoteAddr
		},
		{
			name:       "RemoteAddr with multiple colons no brackets",
			remoteAddr: "::1:8080",
			expectedIP: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}
			req.RemoteAddr = tt.remoteAddr

			result := getClientIP(req)
			assert.Equal(t, tt.expectedIP, result)
		})
	}
}

// =============================================================================
// Error Interface Compliance Tests
// =============================================================================

func TestPureFunctionErrorInterfaceCompliance(t *testing.T) {
	// Ensure all error types implement the error interface
	var _ error = &OAuthError{}
	var _ error = &InternalServerError{}
	var _ error = &RequestTimeoutError{}
	var _ error = &RateLimitError{}
	var _ error = &ValidationError{}

	t.Run("OAuthError implements error", func(t *testing.T) {
		var err error = &OAuthError{Code: "test"}
		assert.NotEmpty(t, err.Error())
	})

	t.Run("InternalServerError implements error", func(t *testing.T) {
		var err error = &InternalServerError{Message: "test"}
		assert.NotEmpty(t, err.Error())
	})

	t.Run("RequestTimeoutError implements error", func(t *testing.T) {
		var err error = &RequestTimeoutError{Timeout: time.Second}
		assert.NotEmpty(t, err.Error())
	})

	t.Run("RateLimitError implements error", func(t *testing.T) {
		var err error = &RateLimitError{Limit: 100, Period: "1s"}
		assert.NotEmpty(t, err.Error())
	})

	t.Run("ValidationError implements error", func(t *testing.T) {
		var err error = &ValidationError{Message: "test"}
		assert.NotEmpty(t, err.Error())
	})
}

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

package secrets

import (
	stderrors "errors"
	"reflect"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestErrorTypes_Error checks the message of each error type, one row per
// formatting branch of its Error method.
func TestErrorTypes_Error(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "resolution with scheme, message and cause",
			err:  NewSecretResolutionError("kms", "secret not found", stderrors.New("underlying error")),
			want: "kms: secret not found: underlying error",
		},
		{
			name: "resolution with scheme and message",
			err:  NewSecretResolutionError("vault", "connection timeout", nil),
			want: "vault: connection timeout",
		},
		{
			name: "resolution with message only",
			err:  NewSecretResolutionError("", "generic error", nil),
			want: "generic error",
		},
		{
			name: "not found with provider",
			err:  &SecretNotFoundError{Path: "/secrets/db/password", Provider: "vault"},
			want: `secret not found: path "/secrets/db/password" in provider "vault"`,
		},
		{
			name: "not found without provider",
			err:  &SecretNotFoundError{Path: "/secrets/api/key"},
			want: `secret not found: path "/secrets/api/key"`,
		},
		{
			name: "resolution failed with reason",
			err:  &ResolutionFailedError{URI: "kms://secret/path", Reason: "connection refused"},
			want: `failed to resolve secret URI "kms://secret/path": connection refused`,
		},
		{
			name: "resolution failed without reason",
			err:  &ResolutionFailedError{URI: "vault://secret/path"},
			want: `failed to resolve secret URI "vault://secret/path"`,
		},
		{
			name: "remote connection with provider, endpoint and cause",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com:8200",
				Provider: "vault",
				Cause:    stderrors.New("connection refused"),
			},
			want: "failed to connect to vault KMS at https://vault.example.com:8200: connection refused",
		},
		{
			name: "remote connection with provider and cause",
			err:  &RemoteConnectionFailedError{Provider: "azure", Cause: stderrors.New("auth failed")},
			want: "failed to connect to azure KMS: auth failed",
		},
		{
			name: "remote connection with endpoint only",
			err:  &RemoteConnectionFailedError{Endpoint: "https://remote.host:443"},
			want: "failed to connect to remote KMS at https://remote.host:443",
		},
		{
			name: "remote connection with no fields",
			err:  &RemoteConnectionFailedError{},
			want: "failed to connect to remote KMS",
		},
		{
			name: "invalid URI with reason",
			err:  &InvalidSecretURIError{URI: "invalid://bad/uri", Reason: "scheme not recognized"},
			want: `invalid secret URI "invalid://bad/uri": scheme not recognized`,
		},
		{
			name: "invalid URI without reason",
			err:  &InvalidSecretURIError{URI: "malformed-uri"},
			want: `invalid secret URI "malformed-uri"`,
		},
		{
			name: "entropy with bytes and cause",
			err:  &EntropyGenerationFailedError{RequestedBytes: 32, Cause: stderrors.New("entropy pool exhausted")},
			want: "failed to generate 32 bytes of entropy: entropy pool exhausted",
		},
		{
			name: "entropy with cause only",
			err:  &EntropyGenerationFailedError{Cause: stderrors.New("random device unavailable")},
			want: "failed to generate entropy: random device unavailable",
		},
		{
			name: "entropy with negative bytes and cause",
			err:  &EntropyGenerationFailedError{RequestedBytes: -1, Cause: stderrors.New("invalid request")},
			want: "failed to generate entropy: invalid request",
		},
		{
			name: "entropy with bytes only",
			err:  &EntropyGenerationFailedError{RequestedBytes: 64},
			want: "failed to generate 64 bytes of entropy",
		},
		{
			name: "entropy with no bytes and no cause",
			err:  &EntropyGenerationFailedError{},
			want: "failed to generate entropy",
		},
		{
			name: "entropy from NewEntropyGenerationError with cause",
			err:  NewEntropyGenerationError(stderrors.New("entropy source error")),
			want: "failed to generate entropy: entropy source error",
		},
		{
			name: "entropy from NewEntropyGenerationError with nil cause",
			err:  NewEntropyGenerationError(nil),
			want: "failed to generate entropy",
		},
		{
			name: "unsupported scheme with supported schemes",
			err: &UnsupportedSchemeError{
				Scheme:           "unknown",
				URI:              "unknown://secret/path",
				SupportedSchemes: []string{"kms", "vault", "env"},
			},
			want: `unsupported scheme "unknown" in URI "unknown://secret/path": supported schemes are [kms vault env]`,
		},
		{
			name: "unsupported scheme with nil supported schemes",
			err:  &UnsupportedSchemeError{Scheme: "custom", URI: "custom://path"},
			want: `unsupported scheme "custom" in URI "custom://path"`,
		},
		{
			name: "unsupported scheme with empty supported schemes",
			err:  &UnsupportedSchemeError{Scheme: "test", URI: "test://uri", SupportedSchemes: []string{}},
			want: `unsupported scheme "test" in URI "test://uri"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestErrorTypes_Unwrap checks that each error type carrying a cause exposes
// it to errors.Is, and unwraps to nil without one.
func TestErrorTypes_Unwrap(t *testing.T) {
	sentinel := stderrors.New("sentinel error")
	tests := []struct {
		name      string
		withCause interface{ Unwrap() error }
		noCause   interface{ Unwrap() error }
	}{
		{
			name:      "SecretResolutionError",
			withCause: NewSecretResolutionError("kms", "wrapped sentinel", sentinel),
			noCause:   NewSecretResolutionError("kms", "error", nil),
		},
		{
			name:      "RemoteConnectionFailedError",
			withCause: &RemoteConnectionFailedError{Endpoint: "https://vault.example.com", Provider: "vault", Cause: sentinel},
			noCause:   &RemoteConnectionFailedError{Endpoint: "https://vault.example.com", Provider: "vault"},
		},
		{
			name:      "EntropyGenerationFailedError",
			withCause: &EntropyGenerationFailedError{RequestedBytes: 16, Cause: sentinel},
			noCause:   &EntropyGenerationFailedError{RequestedBytes: 16},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.withCause.Unwrap(); got != sentinel {
				t.Errorf("Unwrap() = %v, want the sentinel cause", got)
			}
			if !stderrors.Is(tc.withCause.(error), sentinel) {
				t.Error("errors.Is did not find the sentinel cause")
			}
			if got := tc.noCause.Unwrap(); got != nil {
				t.Errorf("Unwrap() without cause = %v, want nil", got)
			}
		})
	}
}

// wrapped hides err one level down a chain, as callers that add context do.
func wrapped(err error) error {
	return &dberrors.WrappedMessageError{Message: "outer", Cause: err}
}

// TestIsPredicates checks each Is* predicate against its own error type,
// directly and wrapped, the QBError code it also accepts, and errors it must
// reject.
func TestIsPredicates(t *testing.T) {
	unrelated := stderrors.New("some error")
	tests := []struct {
		name string
		is   func(error) bool
		err  error
		want bool
	}{
		{"IsSecretNotFound own type", IsSecretNotFound, &SecretNotFoundError{Path: "/secrets/test", Provider: "vault"}, true},
		{"IsSecretNotFound wrapped own type", IsSecretNotFound, wrapped(&SecretNotFoundError{Path: "/secrets/test"}), true},
		{"IsSecretNotFound QBError ErrNotFound", IsSecretNotFound, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}, true},
		{"IsSecretNotFound QBError other code", IsSecretNotFound, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}, false},
		{"IsSecretNotFound unrelated", IsSecretNotFound, unrelated, false},
		{"IsSecretNotFound nil", IsSecretNotFound, nil, false},

		{"IsResolutionFailed own type", IsResolutionFailed, &ResolutionFailedError{URI: "kms://test", Reason: "timeout"}, true},
		{"IsResolutionFailed wrapped own type", IsResolutionFailed, wrapped(&ResolutionFailedError{URI: "kms://test"}), true},
		{"IsResolutionFailed QBError without own type", IsResolutionFailed, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}, false},
		{"IsResolutionFailed unrelated", IsResolutionFailed, unrelated, false},
		{"IsResolutionFailed nil", IsResolutionFailed, nil, false},

		{"IsRemoteConnectionFailed own type", IsRemoteConnectionFailed, &RemoteConnectionFailedError{Endpoint: "https://vault.example.com", Provider: "vault"}, true},
		{"IsRemoteConnectionFailed wrapped own type", IsRemoteConnectionFailed, wrapped(&RemoteConnectionFailedError{Provider: "vault"}), true},
		{"IsRemoteConnectionFailed QBError ErrUnavailable", IsRemoteConnectionFailed, &dberrors.QBError{Code: dberrors.ErrUnavailable, Op: "test"}, true},
		{"IsRemoteConnectionFailed QBError other code", IsRemoteConnectionFailed, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}, false},
		{"IsRemoteConnectionFailed unrelated", IsRemoteConnectionFailed, unrelated, false},
		{"IsRemoteConnectionFailed nil", IsRemoteConnectionFailed, nil, false},

		{"IsInvalidSecretURI own type", IsInvalidSecretURI, &InvalidSecretURIError{URI: "bad-uri", Reason: "missing scheme"}, true},
		{"IsInvalidSecretURI wrapped own type", IsInvalidSecretURI, wrapped(&InvalidSecretURIError{URI: "bad-uri"}), true},
		{"IsInvalidSecretURI unrelated", IsInvalidSecretURI, unrelated, false},
		{"IsInvalidSecretURI nil", IsInvalidSecretURI, nil, false},

		{"IsEntropyGenerationFailed own type", IsEntropyGenerationFailed, &EntropyGenerationFailedError{RequestedBytes: 32}, true},
		{"IsEntropyGenerationFailed wrapped own type", IsEntropyGenerationFailed, wrapped(&EntropyGenerationFailedError{RequestedBytes: 32, Cause: stderrors.New("inner")}), true},
		{"IsEntropyGenerationFailed QBError ErrEncryption", IsEntropyGenerationFailed, &dberrors.QBError{Code: dberrors.ErrEncryption, Op: "test"}, true},
		{"IsEntropyGenerationFailed QBError other code", IsEntropyGenerationFailed, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}, false},
		{"IsEntropyGenerationFailed unrelated", IsEntropyGenerationFailed, unrelated, false},
		{"IsEntropyGenerationFailed nil", IsEntropyGenerationFailed, nil, false},

		{"IsUnsupportedScheme own type", IsUnsupportedScheme, &UnsupportedSchemeError{Scheme: "custom", URI: "custom://path"}, true},
		{"IsUnsupportedScheme wrapped own type", IsUnsupportedScheme, wrapped(&UnsupportedSchemeError{Scheme: "custom"}), true},
		{"IsUnsupportedScheme QBError without own type", IsUnsupportedScheme, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Err: unrelated}, false},
		{"IsUnsupportedScheme unrelated", IsUnsupportedScheme, unrelated, false},
		{"IsUnsupportedScheme nil", IsUnsupportedScheme, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.is(tc.err); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewQBErrorConstructors checks that each QBError constructor sets the
// code, op, retry flag and context callers read, wraps the typed error with
// the given fields, and is matched by its predicate.
func TestNewQBErrorConstructors(t *testing.T) {
	cause := stderrors.New("connection refused")
	tests := []struct {
		name        string
		err         *dberrors.QBError
		cause       error
		is          func(error) bool
		wantCode    dberrors.ErrorCode
		wantOp      string
		wantRetry   bool
		wantContext map[string]interface{}
		wantInner   string
	}{
		{
			name:        "NewSecretNotFoundError",
			err:         NewSecretNotFoundError("secrets.get", "/secrets/db/password", "vault"),
			is:          IsSecretNotFound,
			wantCode:    dberrors.ErrNotFound,
			wantOp:      "secrets.get",
			wantContext: map[string]interface{}{"path": "/secrets/db/password", "provider": "vault"},
			wantInner:   `secret not found: path "/secrets/db/password" in provider "vault"`,
		},
		{
			name:        "NewResolutionFailedError with cause",
			err:         NewResolutionFailedError("secrets.resolve", "kms://secret/path", "connection failed", cause),
			is:          IsResolutionFailed,
			wantCode:    dberrors.ErrInternal,
			wantOp:      "secrets.resolve",
			wantContext: map[string]interface{}{"uri": "kms://secret/path", "reason": "connection failed", "cause": "connection refused"},
			wantInner:   `failed to resolve secret URI "kms://secret/path": connection failed`,
		},
		{
			name:        "NewResolutionFailedError without cause",
			err:         NewResolutionFailedError("secrets.resolve", "vault://secret/path", "not found", nil),
			is:          IsResolutionFailed,
			wantCode:    dberrors.ErrInternal,
			wantOp:      "secrets.resolve",
			wantContext: map[string]interface{}{"uri": "vault://secret/path", "reason": "not found"},
			wantInner:   `failed to resolve secret URI "vault://secret/path": not found`,
		},
		{
			name:        "NewRemoteConnectionFailedError",
			err:         NewRemoteConnectionFailedError("secrets.connect", "https://vault.example.com:8200", "vault", cause),
			cause:       cause,
			is:          IsRemoteConnectionFailed,
			wantCode:    dberrors.ErrUnavailable,
			wantOp:      "secrets.connect",
			wantRetry:   true,
			wantContext: map[string]interface{}{"endpoint": "https://vault.example.com:8200", "provider": "vault"},
			wantInner:   "failed to connect to vault KMS at https://vault.example.com:8200: connection refused",
		},
		{
			name:        "NewInvalidSecretURIError",
			err:         NewInvalidSecretURIError("secrets.parse", "bad-uri://test", "missing path"),
			is:          IsInvalidSecretURI,
			wantCode:    dberrors.ErrInvalidInput,
			wantOp:      "secrets.parse",
			wantContext: map[string]interface{}{"uri": "bad-uri://test", "reason": "missing path"},
			wantInner:   `invalid secret URI "bad-uri://test": missing path`,
		},
		{
			name:        "NewEntropyGenerationFailedError",
			err:         NewEntropyGenerationFailedError("secrets.generateRandom", 64, cause),
			cause:       cause,
			is:          IsEntropyGenerationFailed,
			wantCode:    dberrors.ErrEncryption,
			wantOp:      "secrets.generateRandom",
			wantContext: map[string]interface{}{"requested_bytes": 64},
			wantInner:   "failed to generate 64 bytes of entropy: connection refused",
		},
		{
			name:     "NewUnsupportedSchemeError with supported schemes",
			err:      NewUnsupportedSchemeError("secrets.resolve", "custom", "custom://path", []string{"kms", "vault", "env"}),
			is:       IsUnsupportedScheme,
			wantCode: dberrors.ErrInvalidInput,
			wantOp:   "secrets.resolve",
			wantContext: map[string]interface{}{
				"scheme":            "custom",
				"uri":               "custom://path",
				"supported_schemes": []string{"kms", "vault", "env"},
			},
			wantInner: `unsupported scheme "custom" in URI "custom://path": supported schemes are [kms vault env]`,
		},
		{
			name:     "NewUnsupportedSchemeError with nil supported schemes",
			err:      NewUnsupportedSchemeError("secrets.resolve", "unknown", "unknown://path", nil),
			is:       IsUnsupportedScheme,
			wantCode: dberrors.ErrInvalidInput,
			wantOp:   "secrets.resolve",
			wantContext: map[string]interface{}{
				"scheme":            "unknown",
				"uri":               "unknown://path",
				"supported_schemes": []string(nil),
			},
			wantInner: `unsupported scheme "unknown" in URI "unknown://path"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.wantCode {
				t.Errorf("Code = %v, want %v", tc.err.Code, tc.wantCode)
			}
			if tc.err.Op != tc.wantOp {
				t.Errorf("Op = %q, want %q", tc.err.Op, tc.wantOp)
			}
			if tc.err.Retry != tc.wantRetry {
				t.Errorf("Retry = %v, want %v", tc.err.Retry, tc.wantRetry)
			}
			if !reflect.DeepEqual(tc.err.Context, tc.wantContext) {
				t.Errorf("Context = %v, want %v", tc.err.Context, tc.wantContext)
			}
			if got := tc.err.Err.Error(); got != tc.wantInner {
				t.Errorf("inner Error() = %q, want %q", got, tc.wantInner)
			}
			if tc.cause != nil && !stderrors.Is(tc.err, tc.cause) {
				t.Error("errors.Is did not find the cause through the QBError")
			}
			if !tc.is(tc.err) {
				t.Error("predicate did not match the constructed error")
			}
		})
	}
}

// TestErrNilContext_Sentinel tests the sentinel error
func TestErrNilContext_Sentinel(t *testing.T) {
	if ErrNilContext == nil {
		t.Fatal("Expected non-nil ErrNilContext")
	}
	if ErrNilContext.Error() != "context cannot be nil" {
		t.Errorf("Expected 'context cannot be nil', got %s", ErrNilContext.Error())
	}

	// Test errors.Is
	wrappedErr := &dberrors.WrappedMessageError{Message: "wrapped", Cause: ErrNilContext}
	if !stderrors.Is(wrappedErr, ErrNilContext) {
		t.Error("Expected errors.Is to find ErrNilContext")
	}
}

// TestErrorChaining tests complex error wrapping chains
func TestErrorChaining(t *testing.T) {
	// Create a multi-level error chain
	innerErr := stderrors.New("network timeout")
	entropyErr := &EntropyGenerationFailedError{
		RequestedBytes: 32,
		Cause:          innerErr,
	}
	qbErr := &dberrors.QBError{
		Code: dberrors.ErrEncryption,
		Op:   "secrets.generate",
		Err:  entropyErr,
	}
	outerErr := &dberrors.WrappedMessageError{Message: "operation failed", Cause: qbErr}

	// Test that errors.Is can find innerErr through the chain
	if !stderrors.Is(outerErr, innerErr) {
		t.Error("Expected errors.Is to find innerErr through chain")
	}

	// Test errors.As can find EntropyGenerationFailedError
	var egf *EntropyGenerationFailedError
	if !stderrors.As(outerErr, &egf) {
		t.Error("Expected errors.As to find EntropyGenerationFailedError")
	}
	if egf.RequestedBytes != 32 {
		t.Errorf("Expected RequestedBytes 32, got %d", egf.RequestedBytes)
	}

	// Test errors.As can find QBError
	var de *dberrors.QBError
	if !stderrors.As(outerErr, &de) {
		t.Error("Expected errors.As to find QBError")
	}
	if de.Code != dberrors.ErrEncryption {
		t.Errorf("Expected ErrEncryption, got %v", de.Code)
	}

	// Test IsEntropyGenerationFailed works through chain
	if !IsEntropyGenerationFailed(outerErr) {
		t.Error("Expected IsEntropyGenerationFailed to return true")
	}
}

// asTarget reports whether errors.As finds a T in err, and the T it found.
func asTarget[T error](err error) (error, bool) {
	var target T
	ok := stderrors.As(err, &target)
	return target, ok
}

// TestErrorsAs_AllTypes checks that errors.As finds each error type, the very
// value that was wrapped, through a wrapping chain.
func TestErrorsAs_AllTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		find func(error) (error, bool)
	}{
		{"SecretResolutionError", &SecretResolutionError{Message: "test"}, asTarget[*SecretResolutionError]},
		{"SecretNotFoundError", &SecretNotFoundError{Path: "test"}, asTarget[*SecretNotFoundError]},
		{"ResolutionFailedError", &ResolutionFailedError{URI: "test"}, asTarget[*ResolutionFailedError]},
		{"RemoteConnectionFailedError", &RemoteConnectionFailedError{Endpoint: "test"}, asTarget[*RemoteConnectionFailedError]},
		{"InvalidSecretURIError", &InvalidSecretURIError{URI: "test"}, asTarget[*InvalidSecretURIError]},
		{"EntropyGenerationFailedError", NewEntropyGenerationError(stderrors.New("entropy source error")), asTarget[*EntropyGenerationFailedError]},
		{"UnsupportedSchemeError", &UnsupportedSchemeError{Scheme: "test"}, asTarget[*UnsupportedSchemeError]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, ok := tc.find(wrapped(tc.err))
			if !ok {
				t.Fatal("errors.As did not find the type")
			}
			if found != tc.err {
				t.Errorf("errors.As found %v, want the wrapped value", found)
			}
		})
	}
}

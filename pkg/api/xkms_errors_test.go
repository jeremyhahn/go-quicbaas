//go:build !integration && hashicorpmetrics && local

package api

import (
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestXKMSNotInitializedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &XKMSNotInitializedError{}
		if got := e.Error(); got != "quicbaas xkms not initialized" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &XKMSNotInitializedError{}
		if !e.Is(&XKMSNotInitializedError{}) {
			t.Error("Is should match same type")
		}
		if e.Is(errors.New("other")) {
			t.Error("Is should not match different type")
		}
	})
	t.Run("ErrorsIs", func(t *testing.T) {
		e := &XKMSNotInitializedError{}
		if !errors.Is(e, &XKMSNotInitializedError{}) {
			t.Error("errors.Is should match")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewXKMSNotInitializedError("xkms.encrypt")
		if de.Code != dberrors.ErrPreconditionFailed {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestXKMSAlreadyInitializedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &XKMSAlreadyInitializedError{}
		if got := e.Error(); got != "quicbaas xkms already initialized" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &XKMSAlreadyInitializedError{}
		if !e.Is(&XKMSAlreadyInitializedError{}) {
			t.Error("Is should match")
		}
		if e.Is(errors.New("other")) {
			t.Error("Is should not match different type")
		}
	})
}

func TestXKMSInvalidConfigError(t *testing.T) {
	t.Run("WithDetail", func(t *testing.T) {
		e := &XKMSInvalidConfigError{Detail: "missing key path"}
		want := "invalid xkms configuration: missing key path"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("WithoutDetail", func(t *testing.T) {
		e := &XKMSInvalidConfigError{}
		if got := e.Error(); got != "invalid xkms configuration" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &XKMSInvalidConfigError{Detail: "test"}
		if !e.Is(&XKMSInvalidConfigError{}) {
			t.Error("Is should match")
		}
		if e.Is(errors.New("other")) {
			t.Error("Is should not match different type")
		}
	})
}

func TestXKMSInitializationError(t *testing.T) {
	cause := errors.New("lib error")
	t.Run("Error", func(t *testing.T) {
		e := &XKMSInitializationError{Cause: cause}
		want := "failed to initialize go-xkms: lib error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &XKMSInitializationError{Cause: cause}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewXKMSInitializationError(cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
		if de.Op != "xkms.initialize" {
			t.Errorf("op = %q", de.Op)
		}
	})
}

func TestXKMSConditionalErrors(t *testing.T) {
	cause := errors.New("underlying")

	type conditionalTest struct {
		name        string
		withCause   error
		withoutMsg  string
		withCauseMs string
	}

	tests := []struct {
		name        string
		withCauseFn func(error) error
		noCauseFn   func() error
		withMsg     string
		withoutMsg  string
	}{
		{
			"SealingError",
			func(c error) error { return &SealingError{Cause: c} },
			func() error { return &SealingError{} },
			"sealing operation failed: underlying",
			"sealing operation failed",
		},
		{
			"XKMSEncryptionError",
			func(c error) error { return &XKMSEncryptionError{Cause: c} },
			func() error { return &XKMSEncryptionError{} },
			"encryption operation failed: underlying",
			"encryption operation failed",
		},
		{
			"XKMSKeyGenerationError",
			func(c error) error { return &XKMSKeyGenerationError{Cause: c} },
			func() error { return &XKMSKeyGenerationError{} },
			"key generation failed: underlying",
			"key generation failed",
		},
		{
			"XKMSKeyNotFoundError",
			func(c error) error { return &XKMSKeyNotFoundError{Cause: c} },
			func() error { return &XKMSKeyNotFoundError{} },
			"key not found: underlying",
			"key not found",
		},
		{
			"SigningError",
			func(c error) error { return &SigningError{Cause: c} },
			func() error { return &SigningError{} },
			"signing operation failed: underlying",
			"signing operation failed",
		},
		{
			"VerificationError",
			func(c error) error { return &VerificationError{Cause: c} },
			func() error { return &VerificationError{} },
			"verification operation failed: underlying",
			"verification operation failed",
		},
		{
			"CertificateOperationError",
			func(c error) error { return &CertificateOperationError{Cause: c} },
			func() error { return &CertificateOperationError{} },
			"certificate operation failed: underlying",
			"certificate operation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/WithCause", func(t *testing.T) {
			e := tt.withCauseFn(cause)
			if got := e.Error(); got != tt.withMsg {
				t.Errorf("Error() = %q, want %q", got, tt.withMsg)
			}
			type unwrapper interface{ Unwrap() error }
			if uw, ok := e.(unwrapper); ok {
				if uw.Unwrap() != cause {
					t.Error("Unwrap failed")
				}
			}
		})
		t.Run(tt.name+"/WithoutCause", func(t *testing.T) {
			e := tt.noCauseFn()
			if got := e.Error(); got != tt.withoutMsg {
				t.Errorf("Error() = %q, want %q", got, tt.withoutMsg)
			}
		})
	}
}

func TestXKMSIsMethodMatching(t *testing.T) {
	tests := []struct {
		name  string
		errFn func() error
	}{
		{"SealingError", func() error { return &SealingError{} }},
		{"XKMSEncryptionError", func() error { return &XKMSEncryptionError{} }},
		{"XKMSKeyGenerationError", func() error { return &XKMSKeyGenerationError{} }},
		{"XKMSKeyNotFoundError", func() error { return &XKMSKeyNotFoundError{} }},
		{"SigningError", func() error { return &SigningError{} }},
		{"VerificationError", func() error { return &VerificationError{} }},
		{"CertificateOperationError", func() error { return &CertificateOperationError{} }},
		{"InvalidKeyIDError", func() error { return &InvalidKeyIDError{} }},
		{"RNGNotInitializedError", func() error { return &RNGNotInitializedError{} }},
		{"RNGError", func() error { return &RNGError{} }},
		{"JWTSigningError", func() error { return &JWTSigningError{} }},
		{"JWTVerificationError", func() error { return &JWTVerificationError{} }},
		{"JWTParsingError", func() error { return &JWTParsingError{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.errFn()
			if !errors.Is(e, tt.errFn()) {
				t.Errorf("errors.Is should match same type for %s", tt.name)
			}
			if errors.Is(e, errors.New("unrelated")) {
				t.Errorf("errors.Is should not match unrelated error for %s", tt.name)
			}
		})
	}
}

func TestUnsealingError(t *testing.T) {
	cause := errors.New("bad shares")
	t.Run("WithDetail", func(t *testing.T) {
		e := &UnsealingError{Detail: "insufficient shares"}
		want := "unsealing operation failed: insufficient shares"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("WithCause", func(t *testing.T) {
		e := &UnsealingError{Cause: cause}
		want := "unsealing operation failed: bad shares"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("WithoutBoth", func(t *testing.T) {
		e := &UnsealingError{}
		if got := e.Error(); got != "unsealing operation failed" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &UnsealingError{Detail: "test"}
		if !e.Is(&UnsealingError{}) {
			t.Error("Is should match")
		}
	})
}

func TestXKMSDecryptionError(t *testing.T) {
	cause := errors.New("bad key")
	t.Run("WithDetail", func(t *testing.T) {
		e := &XKMSDecryptionError{Detail: "key expired"}
		want := "decryption operation failed: key expired"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("WithCause", func(t *testing.T) {
		e := &XKMSDecryptionError{Cause: cause}
		want := "decryption operation failed: bad key"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("WithoutBoth", func(t *testing.T) {
		e := &XKMSDecryptionError{}
		if got := e.Error(); got != "decryption operation failed" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &XKMSDecryptionError{}
		if !e.Is(&XKMSDecryptionError{}) {
			t.Error("Is should match")
		}
	})
}

func TestKeyImportError(t *testing.T) {
	cause := errors.New("parse error")
	t.Run("DetailAndCause", func(t *testing.T) {
		e := &KeyImportError{Detail: "RSA", Cause: cause}
		want := "key import failed: RSA: parse error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("DetailOnly", func(t *testing.T) {
		e := &KeyImportError{Detail: "EC"}
		want := "key import failed: EC"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("CauseOnly", func(t *testing.T) {
		e := &KeyImportError{Cause: cause}
		want := "key import failed: parse error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Neither", func(t *testing.T) {
		e := &KeyImportError{}
		if got := e.Error(); got != "key import failed" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &KeyImportError{Detail: "test"}
		if !e.Is(&KeyImportError{}) {
			t.Error("Is should match")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewKeyImportError("xkms.import", "ECDSA", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInvalidKeyIDError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &InvalidKeyIDError{}
		if got := e.Error(); got != "invalid key ID" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &InvalidKeyIDError{}
		if !e.Is(&InvalidKeyIDError{}) {
			t.Error("Is should match")
		}
	})
}

func TestRNGNotInitializedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &RNGNotInitializedError{}
		if got := e.Error(); got != "quicbaas RNG not initialized" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &RNGNotInitializedError{}
		if !e.Is(&RNGNotInitializedError{}) {
			t.Error("Is should match")
		}
	})
}

func TestRNGError(t *testing.T) {
	cause := errors.New("entropy failure")
	t.Run("WithDetail", func(t *testing.T) {
		e := &RNGError{Detail: "insufficient entropy"}
		want := "random number generation failed: insufficient entropy"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("WithCause", func(t *testing.T) {
		e := &RNGError{Cause: cause}
		want := "random number generation failed: entropy failure"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("WithoutBoth", func(t *testing.T) {
		e := &RNGError{}
		if got := e.Error(); got != "random number generation failed" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Is", func(t *testing.T) {
		e := &RNGError{Detail: "test"}
		if !e.Is(&RNGError{}) {
			t.Error("Is should match")
		}
	})
}

func TestRNGInitializationError(t *testing.T) {
	cause := errors.New("device error")
	t.Run("WithDetail", func(t *testing.T) {
		e := &RNGInitializationError{Detail: "failed to open /dev/random", Cause: cause}
		want := "failed to open /dev/random: device error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("WithoutDetail", func(t *testing.T) {
		e := &RNGInitializationError{Cause: cause}
		want := "failed to initialize RNG: device error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &RNGInitializationError{Cause: cause}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
}

func TestJWTErrors(t *testing.T) {
	cause := errors.New("token error")

	jwtTests := []struct {
		name       string
		withDetail func() error
		withCause  func() error
		noBoth     func() error
		detailMsg  string
		causeMsg   string
		noBothMsg  string
	}{
		{
			"JWTSigningError",
			func() error { return &JWTSigningError{Detail: "bad key"} },
			func() error { return &JWTSigningError{Cause: cause} },
			func() error { return &JWTSigningError{} },
			"jwt signing failed: bad key",
			"jwt signing failed: token error",
			"jwt signing failed",
		},
		{
			"JWTVerificationError",
			func() error { return &JWTVerificationError{Detail: "expired"} },
			func() error { return &JWTVerificationError{Cause: cause} },
			func() error { return &JWTVerificationError{} },
			"jwt verification failed: expired",
			"jwt verification failed: token error",
			"jwt verification failed",
		},
		{
			"JWTParsingError",
			func() error { return &JWTParsingError{Detail: "malformed"} },
			func() error { return &JWTParsingError{Cause: cause} },
			func() error { return &JWTParsingError{} },
			"jwt parsing failed: malformed",
			"jwt parsing failed: token error",
			"jwt parsing failed",
		},
	}

	for _, tt := range jwtTests {
		t.Run(tt.name+"/WithDetail", func(t *testing.T) {
			if got := tt.withDetail().Error(); got != tt.detailMsg {
				t.Errorf("got %q, want %q", got, tt.detailMsg)
			}
		})
		t.Run(tt.name+"/WithCause", func(t *testing.T) {
			e := tt.withCause()
			if got := e.Error(); got != tt.causeMsg {
				t.Errorf("got %q, want %q", got, tt.causeMsg)
			}
			type unwrapper interface{ Unwrap() error }
			if uw, ok := e.(unwrapper); ok {
				if uw.Unwrap() != cause {
					t.Error("Unwrap failed")
				}
			}
		})
		t.Run(tt.name+"/NoBoth", func(t *testing.T) {
			if got := tt.noBoth().Error(); got != tt.noBothMsg {
				t.Errorf("got %q, want %q", got, tt.noBothMsg)
			}
		})
	}
}

func TestUnsupportedBackendOperationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnsupportedBackendOperationError{Backend: "file", Operation: "rotate"}
		want := "backend file does not support rotate"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestUnexpectedKeyTypeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnexpectedKeyTypeError{Expected: "ECDSA", Actual: "RSA"}
		want := "ECDSA requires matching key type, got RSA"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestCloseError(t *testing.T) {
	err1 := errors.New("err1")
	err2 := errors.New("err2")

	t.Run("Error", func(t *testing.T) {
		e := &CloseError{Errors: []error{err1, err2}}
		want := "close errors: err1; err2"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("MultiUnwrap", func(t *testing.T) {
		e := &CloseError{Errors: []error{err1, err2}}
		errs := e.Unwrap()
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d", len(errs))
		}
		if errs[0] != err1 || errs[1] != err2 {
			t.Error("Unwrap returned wrong errors")
		}
	})
	t.Run("ErrorsIs", func(t *testing.T) {
		e := &CloseError{Errors: []error{err1, err2}}
		if !errors.Is(e, err1) {
			t.Error("errors.Is should match err1")
		}
		if !errors.Is(e, err2) {
			t.Error("errors.Is should match err2")
		}
	})
}

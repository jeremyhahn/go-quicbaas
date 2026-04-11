//go:build !integration && hashicorpmetrics && local

package api

import (
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestSentinelWrapError(t *testing.T) {
	sentinel := errors.New("forbidden")
	cause := errors.New("bad key")

	t.Run("Error", func(t *testing.T) {
		e := &SentinelWrapError{Sentinel: sentinel, Cause: cause}
		want := "forbidden: bad key"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("MultiUnwrap", func(t *testing.T) {
		e := &SentinelWrapError{Sentinel: sentinel, Cause: cause}
		errs := e.Unwrap()
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d", len(errs))
		}
		if errs[0] != sentinel {
			t.Error("first error should be sentinel")
		}
		if errs[1] != cause {
			t.Error("second error should be cause")
		}
	})
	t.Run("ErrorsIs", func(t *testing.T) {
		e := &SentinelWrapError{Sentinel: sentinel, Cause: cause}
		if !errors.Is(e, sentinel) {
			t.Error("errors.Is should match sentinel")
		}
		if !errors.Is(e, cause) {
			t.Error("errors.Is should match cause")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewSentinelWrapError("secrets.put", dberrors.ErrEncryption, sentinel, cause)
		if de.Code != dberrors.ErrEncryption {
			t.Errorf("code = %v", de.Code)
		}
		if de.Op != "secrets.put" {
			t.Errorf("op = %q", de.Op)
		}
	})
}

func TestCryptoOperationError(t *testing.T) {
	cause := errors.New("bad nonce")
	t.Run("Error", func(t *testing.T) {
		e := &CryptoOperationError{Operation: "encrypt", Cause: cause}
		want := "encrypt: bad nonce"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &CryptoOperationError{Operation: "encrypt", Cause: cause}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewCryptoOperationError("secrets.encrypt", "seal", cause)
		if de.Code != dberrors.ErrEncryption {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestDEKSizeMismatchError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &DEKSizeMismatchError{Expected: 32, Algorithm: "AES-256"}
		want := "DEK size mismatch: expected 32 bytes for AES-256"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewDEKSizeMismatchError("dek.generate", 16, "AES-128")
		if de.Code != dberrors.ErrEncryption {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestDEKNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &DEKNotFoundError{DEKID: "dek-abc"}
		if got := e.Error(); got != "DEK not found: dek-abc" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewDEKNotFoundError("dek.get", "dek-xyz")
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestDEKVersionNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &DEKVersionNotFoundError{DEKID: "dek-1", Version: 3}
		want := "DEK version not found: dek-1 version 3"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewDEKVersionNotFoundError("dek.get", "dek-2", 5)
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestDEKOperationError(t *testing.T) {
	cause := errors.New("io error")
	t.Run("Error", func(t *testing.T) {
		e := &DEKOperationError{Operation: "load DEK", Cause: cause}
		want := "load DEK: io error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &DEKOperationError{Operation: "store", Cause: cause}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewDEKOperationError("dek.store", "marshal", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestNilDEKError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NilDEKError{}
		if got := e.Error(); got != "DEK cannot be nil" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewNilDEKError("dek.rotate")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestSecretStoreOperationError(t *testing.T) {
	cause := errors.New("pebble error")
	t.Run("Error", func(t *testing.T) {
		e := &SecretStoreOperationError{Operation: "put secret", Cause: cause}
		want := "put secret: pebble error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &SecretStoreOperationError{Operation: "get", Cause: cause}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewSecretStoreOperationError("secrets.put", "write", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestPathTraversalError(t *testing.T) {
	sentinel := errors.New("path traversal")
	t.Run("Error", func(t *testing.T) {
		e := &PathTraversalError{Sentinel: sentinel, Detail: "key contains ../"}
		want := "path traversal: key contains ../"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &PathTraversalError{Sentinel: sentinel, Detail: "test"}
		if e.Unwrap() != sentinel {
			t.Error("Unwrap should return sentinel")
		}
	})
	t.Run("ErrorsIs", func(t *testing.T) {
		e := &PathTraversalError{Sentinel: sentinel, Detail: "test"}
		if !errors.Is(e, sentinel) {
			t.Error("errors.Is should match sentinel")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewPathTraversalError("secrets.put", sentinel, "../etc/passwd")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestVersionPositiveError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &VersionPositiveError{}
		if got := e.Error(); got != "version must be positive" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewVersionPositiveError("secrets.get")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestSecretsCauseErrors(t *testing.T) {
	cause := errors.New("underlying")

	tests := []struct {
		name     string
		errFn    func() error
		newFn    func() *dberrors.QBError
		wantMsg  string
		wantCode dberrors.ErrorCode
	}{
		{
			"BackendClose",
			func() error { return &BackendCloseError{Cause: cause} },
			func() *dberrors.QBError { return NewBackendCloseError("secrets.close", cause) },
			"failed to close backend: underlying",
			dberrors.ErrInternal,
		},
		{
			"InvalidBlob",
			func() error { return &InvalidBlobError{Cause: cause} },
			func() *dberrors.QBError { return NewInvalidBlobError("secrets.decrypt", cause) },
			"invalid blob: underlying",
			dberrors.ErrInvalidInput,
		},
		{
			"DEKRetrieval",
			func() error { return &DEKRetrievalError{Cause: cause} },
			func() *dberrors.QBError { return NewDEKRetrievalError("secrets.get", cause) },
			"failed to get DEK: underlying",
			dberrors.ErrInternal,
		},
		{
			"AuthenticationFailed",
			func() error { return &AuthenticationFailedError{Cause: cause} },
			func() *dberrors.QBError { return NewAuthenticationFailedError("secrets.decrypt", cause) },
			"authentication failed: underlying",
			dberrors.ErrDecryption,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.errFn()
			if got := e.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			type unwrapper interface{ Unwrap() error }
			if uw, ok := e.(unwrapper); ok {
				if uw.Unwrap() != cause {
					t.Error("Unwrap failed")
				}
			}
			de := tt.newFn()
			if de.Code != tt.wantCode {
				t.Errorf("code = %v, want %v", de.Code, tt.wantCode)
			}
		})
	}
}

//go:build !integration && hashicorpmetrics && local

package api

import (
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestKMSSimpleFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		errFn   func() error
		wantMsg string
	}{
		{"InvalidAlgorithm", func() error { return &InvalidAlgorithmError{Algorithm: "ROT13"} }, "unsupported algorithm: ROT13"},
		{"KeyDisabled", func() error { return &KeyDisabledError{KeyID: "k1"} }, "key k1 is disabled"},
		{"KeyDeleted", func() error { return &KeyDeletedError{KeyID: "k2"} }, "key k2 is deleted"},
		{"KeyPendingDeletion", func() error { return &KeyPendingDeletionError{KeyID: "k3"} }, "key k3 is pending deletion"},
		{"AlgorithmOperationUnsupported", func() error { return &AlgorithmOperationUnsupportedError{Algorithm: "RSA", Operation: "encrypt"} }, "algorithm RSA does not support operation encrypt"},
		{"UnknownAlgorithm", func() error { return &UnknownAlgorithmError{Name: "XYZ"} }, "unknown algorithm: XYZ"},
		{"AlgorithmSigningUnsupported", func() error { return &AlgorithmSigningUnsupportedError{Algorithm: "AES"} }, "algorithm AES does not support signing"},
		{"NoBackendsAvailable", func() error { return &NoBackendsAvailableError{Source: "registry"} }, "no backends available in registry"},
		{"NoBackendsRegistered", func() error { return &NoBackendsRegisteredError{} }, "no backends were successfully registered"},
		{"UnsupportedPublicKeyType", func() error { return &UnsupportedPublicKeyTypeError{TypeName: "DSA"} }, "unsupported public key type: DSA"},
		{"NoPublicComponent", func() error { return &NoPublicComponentError{} }, "key has no public component"},
		{"UnsupportedHashAlgorithm", func() error { return &UnsupportedHashAlgorithmError{Algorithm: "MD5"} }, "unsupported hash algorithm: MD5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.errFn()
			if got := e.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestKMSCauseErrors(t *testing.T) {
	cause := errors.New("underlying")

	tests := []struct {
		name    string
		errFn   func() error
		newFn   func() *dberrors.DragonError
		wantMsg string
	}{
		{"BackendRouting", func() error { return &BackendRoutingError{Cause: cause} }, func() *dberrors.DragonError { return NewBackendRoutingError("kms", cause) }, "failed to route to backend: underlying"},
		{"FailoverExhausted", func() error { return &FailoverExhaustedError{Attempts: 3, Cause: cause} }, func() *dberrors.DragonError { return NewFailoverExhaustedError("kms", 3, cause) }, "all failover attempts failed after 3 tries: underlying"},
		{"BackendRegistration", func() error { return &BackendRegistrationError{Name: "aws", Cause: cause} }, func() *dberrors.DragonError { return NewBackendRegistrationError("kms", "aws", cause) }, "failed to register backend aws: underlying"},
		{"RegistryClose", func() error { return &RegistryCloseError{Cause: cause} }, func() *dberrors.DragonError { return NewRegistryCloseError("kms", cause) }, "failed to close registry: underlying"},
		{"SymmetricKeyGeneration", func() error { return &SymmetricKeyGenerationError{Cause: cause} }, func() *dberrors.DragonError { return NewSymmetricKeyGenerationError("kms", cause) }, "failed to generate symmetric key: underlying"},
		{"AsymmetricKeyGeneration", func() error { return &AsymmetricKeyGenerationError{Cause: cause} }, func() *dberrors.DragonError { return NewAsymmetricKeyGenerationError("kms", cause) }, "failed to generate asymmetric key: underlying"},
		{"KeyVersionGeneration", func() error { return &KeyVersionGenerationError{Cause: cause} }, func() *dberrors.DragonError { return NewKeyVersionGenerationError("kms", cause) }, "failed to generate new key version: underlying"},
		{"KeyDeletion", func() error { return &KeyDeletionError{Cause: cause} }, func() *dberrors.DragonError { return NewKeyDeletionError("kms", cause) }, "failed to delete key: underlying"},
		{"KeyMetadataRetrieval", func() error { return &KeyMetadataRetrievalError{Cause: cause} }, func() *dberrors.DragonError { return NewKeyMetadataRetrievalError("kms", cause) }, "failed to get key metadata: underlying"},
		{"EncryptionOperation", func() error { return &EncryptionOperationError{Cause: cause} }, func() *dberrors.DragonError { return NewEncryptionOperationError("kms", cause) }, "encryption failed: underlying"},
		{"DecryptionOperation", func() error { return &DecryptionOperationError{Cause: cause} }, func() *dberrors.DragonError { return NewDecryptionOperationError("kms", cause) }, "decryption failed: underlying"},
		{"CiphertextDeserialization", func() error { return &CiphertextDeserializationError{Cause: cause} }, func() *dberrors.DragonError { return NewCiphertextDeserializationError("kms", cause) }, "failed to deserialize ciphertext: underlying"},
		{"SignerRetrieval", func() error { return &SignerRetrievalError{Cause: cause} }, func() *dberrors.DragonError { return NewSignerRetrievalError("kms", cause) }, "failed to get signer: underlying"},
		{"SigningOperation", func() error { return &SigningOperationError{Cause: cause} }, func() *dberrors.DragonError { return NewSigningOperationError("kms", cause) }, "signing failed: underlying"},
		{"RandomGeneration", func() error { return &RandomGenerationError{Cause: cause} }, func() *dberrors.DragonError { return NewRandomGenerationError("kms", cause) }, "failed to generate random bytes: underlying"},
		{"DEKWrapping", func() error { return &DEKWrappingError{Cause: cause} }, func() *dberrors.DragonError { return NewDEKWrappingError("kms", cause) }, "failed to wrap DEK: underlying"},
		{"PublicKeyMarshal", func() error { return &PublicKeyMarshalError{Cause: cause} }, func() *dberrors.DragonError { return NewPublicKeyMarshalError("kms", cause) }, "failed to marshal public key: underlying"},
		{"CacheClose", func() error { return &CacheCloseError{Cause: cause} }, func() *dberrors.DragonError { return NewCacheCloseError("kms", cause) }, "failed to close key cache: underlying"},
		{"DEKDecryption", func() error { return &DEKDecryptionError{Cause: cause} }, func() *dberrors.DragonError { return NewDEKDecryptionError("kms", cause) }, "failed to decrypt DEK: underlying"},
		{"KeyRotation", func() error { return &KeyRotationError{Cause: cause} }, func() *dberrors.DragonError { return NewKeyRotationError("kms", cause) }, "key rotation failed: underlying"},
		{"RandomKeyGeneration", func() error { return &RandomKeyGenerationError{Cause: cause} }, func() *dberrors.DragonError { return NewRandomKeyGenerationError("kms", cause) }, "failed to generate random key: underlying"},
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
			if de.Err.Error() != tt.wantMsg {
				t.Errorf("constructor msg = %q, want %q", de.Err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestCacheLimitExceededError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &CacheLimitExceededError{LimitType: "max_keys", Value: "5001", Limit: "5000"}
		want := "max_keys: 5001 exceeds 5000"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewCacheLimitExceededError("cache", nil, "size", "100MB", "50MB")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInitializationError(t *testing.T) {
	cause := errors.New("fail")
	t.Run("Error", func(t *testing.T) {
		e := &InitializationError{Component: "cache", Cause: cause}
		want := "failed to initialize cache: fail"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewInitializationError("kms", "backend", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestComponentCreationError(t *testing.T) {
	cause := errors.New("fail")
	t.Run("Error", func(t *testing.T) {
		e := &ComponentCreationError{Component: "signer", Cause: cause}
		want := "failed to create signer: fail"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewComponentCreationError("kms", "encryptor", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

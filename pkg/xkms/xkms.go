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

// Package xkms provides the integration layer between QuicBaaS and go-xkms.
// ALL cryptographic operations in QuicBaaS MUST go through this package.
// This ensures consistent key management across the entire system.
//
// Key Reference Patterns:
//   - KeyAttributes: Preferred in Go code for type-safe key operations
//   - Key ID (kid): Use in configs, format: "backend:type:algo:keyname" or just "keyname"
//
// Example kid formats:
//   - "my-key"                           - Uses default backend
//   - ":::my-key"                         - Explicit form of above
//   - "pkcs11:::my-key"                   - Specify backend only
//   - "pkcs11:signing:ecdsa-p256:my-key"  - Full specification
package xkms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// PublicKey returns the public key for the given key ID.
// The key ID can be in format "backend:keyname" or just "keyname".
func PublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	// Get the signer, which has access to the public key
	signer, err := SignerByID(ctx, kid)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "xkms.publicKey",
			Err:  &api.OperationFailedError{Operation: "get signer for public key", Cause: err},
		}
	}

	return signer.Public(), nil
}

// -----------------------------------------------------------------------------
// Error Types
// -----------------------------------------------------------------------------

// ErrNotInitialized is returned when xkms operations are attempted before initialization.
var ErrNotInitialized = &api.XKMSNotInitializedError{}

// ErrAlreadyInitialized is returned when Initialize is called more than once.
var ErrAlreadyInitialized = &api.XKMSAlreadyInitializedError{}

// ErrInvalidConfig is returned when the configuration is invalid.
var ErrInvalidConfig = &api.XKMSInvalidConfigError{}

// ErrSealingFailed is returned when a seal operation fails.
var ErrSealingFailed = &api.SealingError{}

// ErrUnsealingFailed is returned when an unseal operation fails.
var ErrUnsealingFailed = &api.UnsealingError{}

// ErrEncryptionFailed is returned when an encryption operation fails.
var ErrEncryptionFailed = &api.XKMSEncryptionError{}

// ErrDecryptionFailed is returned when a decryption operation fails.
var ErrDecryptionFailed = &api.XKMSDecryptionError{}

// ErrKeyGenerationFailed is returned when key generation fails.
var ErrKeyGenerationFailed = &api.XKMSKeyGenerationError{}

// ErrKeyNotFound is returned when a key cannot be found.
var ErrKeyNotFound = &api.XKMSKeyNotFoundError{}

// ErrKeyImportFailed is returned when a key import operation fails.
var ErrKeyImportFailed = &api.KeyImportError{}

// ErrSigningFailed is returned when a signing operation fails.
var ErrSigningFailed = &api.SigningError{}

// ErrVerificationFailed is returned when a verification operation fails.
var ErrVerificationFailed = &api.VerificationError{}

// ErrCertificateOperationFailed is returned when a certificate operation fails.
var ErrCertificateOperationFailed = &api.CertificateOperationError{}

// ErrInvalidKeyID is returned when a key ID is invalid.
var ErrInvalidKeyID = &api.InvalidKeyIDError{}

// -----------------------------------------------------------------------------
// Package State
// -----------------------------------------------------------------------------

var (
	initialized bool
	initMu      sync.RWMutex
	logger      interfaces.Logger

	// defaultBackend is the backend a key with no StoreType lives in; it
	// names the version record CurrentSigner reads.
	defaultBackend string
)

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// Config contains the configuration for the QuicBaaS xkms integration.
type Config struct {
	// DefaultBackend is the name of the default backend to use (e.g., "software", "tpm2", "pkcs11")
	DefaultBackend string

	// Backends is a map of backend configurations
	Backends map[string]xkmssdk.ServiceBackend

	// KeyVersions is the store of key-version records go-xkms needs to
	// rotate a key: a rotation creates the next version as its own backend
	// key and records it as current. Without one, every key is its own
	// version 1 and RotateKey returns ErrKeyVersionsNotConfigured.
	KeyVersions *xkmssdk.KeyVersionStore

	// Logger for structured logging
	Logger interfaces.Logger
}

// -----------------------------------------------------------------------------
// Initialization
// -----------------------------------------------------------------------------

// Initialize sets up the QuicBaaS xkms integration.
// This MUST be called at application startup before any crypto operations.
func Initialize(cfg *Config) error {
	initMu.Lock()
	defer initMu.Unlock()

	if initialized {
		return ErrAlreadyInitialized
	}

	if cfg == nil {
		return &api.XKMSInvalidConfigError{Detail: "config cannot be nil"}
	}

	if len(cfg.Backends) == 0 {
		return &api.XKMSInvalidConfigError{Detail: "at least one backend must be configured"}
	}

	if cfg.Logger == nil {
		return &api.XKMSInvalidConfigError{Detail: "logger cannot be nil"}
	}

	logger = cfg.Logger
	defaultBackend = cfg.DefaultBackend

	// Initialize go-xkms service
	serviceConfig := &xkmssdk.ServiceConfig{
		Backends:       cfg.Backends,
		DefaultBackend: cfg.DefaultBackend,
	}

	if err := xkmssdk.ServiceInitialize(serviceConfig); err != nil {
		return api.NewXKMSInitializationError(err)
	}

	if cfg.KeyVersions != nil {
		svc, err := xkmssdk.Get()
		if err != nil {
			return api.NewXKMSInitializationError(err)
		}
		svc.SetKeyVersionStore(cfg.KeyVersions)
	}

	initialized = true

	logger.WithFields(map[string]interface{}{
		"default_backend": cfg.DefaultBackend,
		"num_backends":    len(cfg.Backends),
	}).Info("QuicBaaS xkms initialized")

	return nil
}

// IsInitialized returns whether the xkms has been initialized.
func IsInitialized() bool {
	initMu.RLock()
	defer initMu.RUnlock()
	return initialized
}

// Reset clears the xkms state (for testing).
func Reset() {
	initMu.Lock()
	defer initMu.Unlock()

	xkmssdk.ServiceReset()
	initialized = false
	logger = nil
	defaultBackend = ""
}

// checkInitialized is a helper to check initialization status.
func checkInitialized() error {
	initMu.RLock()
	defer initMu.RUnlock()
	if !initialized {
		return ErrNotInitialized
	}
	return nil
}

// -----------------------------------------------------------------------------
// Key ID Parsing (for config references)
// -----------------------------------------------------------------------------

// ParseKeyID parses a key ID string into KeyAttributes.
// This uses the unified 4-part Key ID format: backend:type:algo:keyname
// All segments except keyname are optional.
//
// Examples:
//   - "my-key"                           - shorthand for just keyname (uses defaults)
//   - ":::my-key"                         - explicit form of above
//   - "pkcs11:::my-key"                   - specify backend only
//   - "pkcs11:signing:ecdsa-p256:my-key"  - full specification
func ParseKeyID(kid string) (*xkmssdk.KeyAttributes, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}
	return xkmssdk.ServiceParseKeyIDToAttributes(kid)
}

// ParseCertificateID parses a certificate ID string into KeyAttributes.
// Same format as ParseKeyID.
func ParseCertificateID(kid string) (*xkmssdk.KeyAttributes, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}
	return xkmssdk.ServiceParseCertificateID(kid)
}

// -----------------------------------------------------------------------------
// Key Generation (Primary API using KeyAttributes)
// -----------------------------------------------------------------------------

// GenerateKey generates a new asymmetric key with the given attributes.
// Uses BackendFor(attrs) to resolve the correct backend based on StoreType.
//
// This is the preferred method for key generation in Go code.
func GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceGenerateKey(ctx, attrs)
	if err != nil {
		return nil, &api.XKMSKeyGenerationError{Cause: err}
	}

	return key, nil
}

// GenerateKeyWithBackend generates a new key on a specific backend.
func GenerateKeyWithBackend(ctx context.Context, backendName string, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceGenerateKeyWithBackend(ctx, backendName, attrs)
	if err != nil {
		return nil, &api.XKMSKeyGenerationError{Cause: err}
	}

	return key, nil
}

// -----------------------------------------------------------------------------
// Key Retrieval (Primary API using KeyAttributes)
// -----------------------------------------------------------------------------

// Key retrieves a key by its attributes.
// Uses BackendFor(attrs) to resolve the correct backend based on StoreType.
//
// This is the preferred method for key retrieval in Go code.
func Key(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceKey(ctx, attrs)
	if err != nil {
		return nil, &api.XKMSKeyNotFoundError{Cause: err}
	}

	return key, nil
}

// KeyByID retrieves a key by its key ID (kid).
// Format: "backend:type:algo:keyname" with optional segments, or just "keyname".
//
// Use this for config-driven key lookups.
func KeyByID(ctx context.Context, kid string) (crypto.PrivateKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceKeyByID(ctx, kid)
	if err != nil {
		return nil, &api.XKMSKeyNotFoundError{Cause: err}
	}

	return key, nil
}

// DeleteKey deletes a key by its attributes.
func DeleteKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceDeleteKey(ctx, attrs)
}

// DeleteKeyByID deletes a key by its key ID (kid).
func DeleteKeyByID(ctx context.Context, kid string) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceDeleteKeyByID(ctx, kid)
}

// RotateKey creates the next version of an existing key and makes it
// current. go-xkms keeps every earlier version (the new version is its own
// backend key, "<name>--v<n>"), and records the versions in the store wired
// through Config.KeyVersions; without one it returns
// xkmssdk.ErrKeyVersionsNotConfigured.
func RotateKey(ctx context.Context, kid string) (crypto.PrivateKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceRotateKey(ctx, kid)
}

// ListKeys lists all keys across all backends or from a specific backend.
func ListKeys(ctx context.Context, backendName ...string) ([]*xkmssdk.KeyAttributes, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceListKeys(ctx, backendName...)
}

// -----------------------------------------------------------------------------
// Signer/Decrypter (Primary API using KeyAttributes)
// -----------------------------------------------------------------------------

// Signer returns a crypto.Signer for the specified key attributes.
// This is the primary method - use SignerByID for string-based lookups.
func Signer(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceSigner(ctx, attrs)
}

// CurrentSigner returns a crypto.Signer for the CURRENT version of the key
// attrs names. go-xkms rotates a key by creating its next version as its own
// backend key ("<CN>--v<n>"), so Signer(attrs) keeps answering with version
// 1; a caller that signs with whatever the key currently is (an OIDC token
// signer) resolves it here, from the version record go-xkms keeps. A key
// that was never rotated, or a service with no version store, is its own
// current version.
func CurrentSigner(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}
	current, err := currentVersionAttrs(ctx, attrs)
	if err != nil {
		return nil, err
	}
	return xkmssdk.ServiceSigner(ctx, current)
}

// currentVersionAttrs returns attrs re-addressed to the key's current
// version, read from the service's key-version store.
func currentVersionAttrs(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*xkmssdk.KeyAttributes, error) {
	if attrs == nil {
		return nil, &api.XKMSInvalidConfigError{Detail: "key attributes cannot be nil"}
	}
	svc, err := xkmssdk.Get()
	if err != nil {
		return nil, err
	}
	store := svc.KeyVersionStore()
	if store == nil {
		return attrs, nil
	}

	backend := string(attrs.StoreType)
	if backend == "" {
		backend = defaultBackend
	}
	tenant := attrs.TenantID
	if qrdbsdk.IsSystemTenant(tenant) {
		tenant = ""
	}
	rec, found, err := store.Load(ctx, tenant, backend, attrs.CN)
	if err != nil {
		return nil, err
	}
	if !found || rec.Current <= 1 {
		return attrs, nil
	}

	current := *attrs
	current.CN = xkmssdk.KeyVersionName(attrs.CN, rec.Current)
	return &current, nil
}

// SignerByID returns a crypto.Signer for the specified key ID (kid).
// Format: "backend:type:algo:keyname" with optional segments, or just "keyname".
func SignerByID(ctx context.Context, kid string) (crypto.Signer, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceSignerByID(ctx, kid)
}

// Decrypter returns a crypto.Decrypter for the specified key attributes.
// This is the primary method - use DecrypterByID for string-based lookups.
func Decrypter(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceDecrypter(ctx, attrs)
}

// DecrypterByID returns a crypto.Decrypter for the specified key ID (kid).
func DecrypterByID(ctx context.Context, kid string) (crypto.Decrypter, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceDecrypterByID(ctx, kid)
}

// -----------------------------------------------------------------------------
// Signing and Verification
// -----------------------------------------------------------------------------

// SignOptions contains options for signing operations.
type SignOptions = xkmssdk.SignOptions

// Sign signs data using the specified key (by kid).
// The kid format is "backend:type:algo:keyname" with optional segments.
func Sign(ctx context.Context, kid string, data []byte, opts *SignOptions) ([]byte, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	sig, err := xkmssdk.ServiceSign(ctx, kid, data, opts)
	if err != nil {
		return nil, &api.SigningError{Cause: err}
	}

	return sig, nil
}

// Verify verifies a signature against data using the specified key (by kid).
func Verify(ctx context.Context, kid string, data, signature []byte, opts *xkmssdk.VerifyOpts) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	err := xkmssdk.ServiceVerify(ctx, kid, data, signature, opts)
	if err != nil {
		return &api.VerificationError{Cause: err}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Symmetric Encryption/Decryption (Primary API using KeyAttributes)
// -----------------------------------------------------------------------------

// Encrypt encrypts data using a symmetric key by kid.
// The kid supports format: "backend:key-id" or just "key-id" (uses default backend).
func Encrypt(ctx context.Context, kid string, data []byte, opts *xkmssdk.EncryptOptions) (*xkmssdk.EncryptedData, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	encrypted, err := xkmssdk.ServiceEncrypt(ctx, kid, data, opts)
	if err != nil {
		return nil, &api.XKMSEncryptionError{Cause: err}
	}

	return encrypted, nil
}

// Decrypt decrypts data using a symmetric key by kid.
func Decrypt(ctx context.Context, kid string, data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	if data == nil {
		return nil, &api.XKMSDecryptionError{Detail: "encrypted data cannot be nil"}
	}

	plaintext, err := xkmssdk.ServiceDecrypt(ctx, kid, data, opts)
	if err != nil {
		return nil, &api.XKMSDecryptionError{Cause: err}
	}

	return plaintext, nil
}

// -----------------------------------------------------------------------------
// Symmetric Key Operations
// -----------------------------------------------------------------------------

// GenerateSymmetricKey generates a new symmetric key.
func GenerateSymmetricKey(ctx context.Context, backendName string, attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceGenerateSymmetricKey(ctx, backendName, attrs)
	if err != nil {
		return nil, &api.XKMSKeyGenerationError{Cause: err}
	}

	return key, nil
}

// GetSymmetricKey retrieves an existing symmetric key.
// The keyRef supports format: "backend:key-id" or just "key-id" (uses default backend).
func GetSymmetricKey(ctx context.Context, keyRef string) (xkmssdk.SymmetricKey, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	key, err := xkmssdk.ServiceGetSymmetricKey(ctx, keyRef)
	if err != nil {
		return nil, &api.XKMSKeyNotFoundError{Cause: err}
	}

	return key, nil
}

// Errors reporting material that does not match the algorithm it is offered
// under. Storing a key beneath a false description is worse than refusing it:
// the description is what every later operation reasons from.
var (
	// ErrKeyMaterialNotPKCS8 indicates asymmetric material that is not PKCS#8 DER.
	ErrKeyMaterialNotPKCS8 = stderrors.New("xkms: asymmetric key material must be PKCS#8 DER")

	// ErrKeyMaterialAlgorithmMismatch indicates material whose actual algorithm
	// — INCLUDING its curve — contradicts the algorithm declared for it.
	ErrKeyMaterialAlgorithmMismatch = stderrors.New("xkms: key material contradicts the declared algorithm")

	// ErrUnsupportedAsymmetricKeyType indicates a parsed key of a type no
	// backend here handles.
	ErrUnsupportedAsymmetricKeyType = stderrors.New("xkms: unsupported asymmetric private key type")
)

// verifyMaterialMatchesAlgorithm refuses asymmetric material whose real
// identity contradicts the attributes it would be stored under.
//
// THE CURVE IS PART OF THE IDENTITY, not a detail of it. Comparing only the
// algorithm family accepts a P-384 key declared as P-256 — and since a caller
// that names no curve is defaulted to P-256 downstream, that is the likely
// shape of the mistake rather than an exotic one.
//
// go-xkms validates this pairing for RSA only; ECDSA and Ed25519 fall through
// an empty default branch, which is exactly the two algorithms the signing
// policy uses (upstream: go-xkms#53).
func verifyMaterialMatchesAlgorithm(material []byte, attrs *xkmssdk.KeyAttributes) error {
	key, err := x509.ParsePKCS8PrivateKey(material)
	if err != nil {
		return ErrKeyMaterialNotPKCS8
	}

	switch parsed := key.(type) {
	case ed25519.PrivateKey:
		return requireDeclaredAlgorithm(x509.Ed25519, attrs)
	case *ed25519.PrivateKey:
		return requireDeclaredAlgorithm(x509.Ed25519, attrs)
	case *ecdsa.PrivateKey:
		if err := requireDeclaredAlgorithm(x509.ECDSA, attrs); err != nil {
			return err
		}
		if attrs.ECCAttributes != nil && attrs.ECCAttributes.Curve != nil &&
			attrs.ECCAttributes.Curve != parsed.Curve {
			return ErrKeyMaterialAlgorithmMismatch
		}
		return nil
	case *rsa.PrivateKey:
		return requireDeclaredAlgorithm(x509.RSA, attrs)
	default:
		return ErrUnsupportedAsymmetricKeyType
	}
}

// requireDeclaredAlgorithm compares a parsed key's algorithm to the declared one.
func requireDeclaredAlgorithm(actual x509.PublicKeyAlgorithm, attrs *xkmssdk.KeyAttributes) error {
	if actual != attrs.KeyAlgorithm {
		return ErrKeyMaterialAlgorithmMismatch
	}
	return nil
}

// ImportKeyMaterial imports raw key material into a backend using the import
// token flow, which wraps the material in transit.
//
// The algorithm rides on attrs and is what decides how the backend interprets
// material: raw bytes for a symmetric key, PKCS#8 DER for an asymmetric one.
// Nothing in this function inspects the algorithm, so the two are one
// operation rather than two — it was named for symmetric keys only because
// that was the first caller.
func ImportKeyMaterial(ctx context.Context, backendName string, attrs *xkmssdk.KeyAttributes, material []byte) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	// ASYMMETRIC MATERIAL MUST DESCRIBE ITSELF TRUTHFULLY, and the check lives
	// HERE because this is the chokepoint every caller passes through. It was
	// first written a layer up in pkg/kms, which left this exported function
	// reachable with no validation at all — one implementation beside the
	// contract it interrogates, rather than a copy per caller.
	if !attrs.IsSymmetric() {
		if err := verifyMaterialMatchesAlgorithm(material, attrs); err != nil {
			return err
		}
	}

	// Get the keystore for this backend
	keyStore, err := xkmssdk.ServiceGetBackend(backendName)
	if err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "xkms.importKeyMaterial",
			Err:  &api.OperationFailedError{Operation: "get backend", Cause: err},
		}
	}

	// Get the underlying backend from the keystore
	underlyingBackend := keyStore.KeyProvider()

	// THE IMPORT-TOKEN FLOW IS THE ONLY PATH. There is no raw-import
	// shortcut, and the wrapping below is not a fallback from one.
	//
	// This used to type-assert backends to a locally-declared RawKeyImporter
	// and call ImportRawKey "preferred for internal use — no token flow
	// required". No go-xkms backend has ever had that method, so the assertion
	// never matched and every import took the wrapping path anyway. A dead
	// preferred path is worse than no path: the next reader optimises or
	// debugs the branch that never runs.
	//
	// It is not reinstated as a real capability because that would be a
	// go-xkms decision, not ours (docs/00 L8), and because the token flow is
	// what makes the backend STORE wrapped material: raw bytes still reach
	// WrapKey below, but ImportKey — the call that persists the key — only
	// ever receives the wrapped form. If go-xkms ever adds a method by that
	// name, it must be adopted deliberately; nothing here will start calling
	// it silently, because nothing here asks for it.
	importBackend, ok := underlyingBackend.(xkmssdk.ImportExportBackend)
	if !ok {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "xkms.importKeyMaterial",
			Err:  &api.UnsupportedBackendOperationError{Backend: backendName, Operation: "key import"},
		}
	}

	// Step 1: get import parameters, which generate the RSA wrapping key pair.
	// RSA-OAEP-SHA256 is the wrapping algorithm.
	importParams, err := importBackend.GetImportParameters(ctx, attrs, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	if err != nil {
		return &api.KeyImportError{Detail: "failed to get import parameters", Cause: err}
	}

	// Step 2: Wrap the raw key material using the import parameters
	wrappedMaterial, err := importBackend.WrapKey(ctx, material, importParams)
	if err != nil {
		return &api.KeyImportError{Detail: "failed to wrap key material", Cause: err}
	}

	// Step 3: Import the wrapped key (the backend will unwrap it internally)
	if err := importBackend.ImportKey(ctx, attrs, wrappedMaterial); err != nil {
		return &api.KeyImportError{Cause: err}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Sealing Operations (for protecting sensitive data)
// -----------------------------------------------------------------------------

// Seal encrypts/protects data using the default backend's sealing mechanism.
// This is the primary method for protecting sensitive data like DEKs.
//
// Different backends provide different security guarantees:
//   - TPM2: PCR-bound hardware sealing (strongest)
//   - PKCS#11: HSM-backed AES-GCM encryption
//   - AWS/Azure/GCP KMS: Cloud-managed envelope encryption
//   - PKCS#8: Software-based HKDF + AES-GCM (portable)
func Seal(ctx context.Context, data []byte, opts *xkmssdk.SealOptions) (*xkmssdk.SealedData, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	sealed, err := xkmssdk.ServiceSeal(ctx, data, opts)
	if err != nil {
		return nil, &api.SealingError{Cause: err}
	}

	return sealed, nil
}

// SealWithBackend seals data using a specific backend.
func SealWithBackend(ctx context.Context, backendName string, data []byte, opts *xkmssdk.SealOptions) (*xkmssdk.SealedData, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	sealed, err := xkmssdk.ServiceSealWithBackend(ctx, backendName, data, opts)
	if err != nil {
		return nil, &api.SealingError{Cause: err}
	}

	return sealed, nil
}

// Unseal decrypts/recovers data that was previously sealed.
// The sealed data's backend type determines which backend is used.
func Unseal(ctx context.Context, sealed *xkmssdk.SealedData, opts *xkmssdk.UnsealOptions) ([]byte, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	if sealed == nil {
		return nil, &api.UnsealingError{Detail: "sealed data cannot be nil"}
	}

	plaintext, err := xkmssdk.ServiceUnseal(ctx, sealed, opts)
	if err != nil {
		return nil, &api.UnsealingError{Cause: err}
	}

	return plaintext, nil
}

// UnsealWithBackend unseals data using a specific backend.
func UnsealWithBackend(ctx context.Context, backendName string, sealed *xkmssdk.SealedData, opts *xkmssdk.UnsealOptions) ([]byte, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	if sealed == nil {
		return nil, &api.UnsealingError{Detail: "sealed data cannot be nil"}
	}

	plaintext, err := xkmssdk.ServiceUnsealWithBackend(ctx, backendName, sealed, opts)
	if err != nil {
		return nil, &api.UnsealingError{Cause: err}
	}

	return plaintext, nil
}

// CanSeal returns true if the specified backend supports sealing operations.
func CanSeal(backendName ...string) bool {
	if err := checkInitialized(); err != nil {
		return false
	}

	return xkmssdk.ServiceCanSeal(backendName...)
}

// -----------------------------------------------------------------------------
// Certificate Operations (Primary API using KeyAttributes)
// -----------------------------------------------------------------------------

// Certificate retrieves a certificate by key attributes.
// This is the preferred method for certificate retrieval in Go code.
func Certificate(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*x509.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	cert, err := xkmssdk.ServiceCertificate(ctx, attrs)
	if err != nil {
		return nil, &api.CertificateOperationError{Cause: err}
	}

	return cert, nil
}

// CertificateByID retrieves a certificate by key ID (kid).
// Use this for config-driven certificate lookups.
func CertificateByID(ctx context.Context, kid string) (*x509.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	cert, err := xkmssdk.ServiceCertificateByID(ctx, kid)
	if err != nil {
		return nil, &api.CertificateOperationError{Cause: err}
	}

	return cert, nil
}

// SaveCertificate saves a certificate using key attributes.
func SaveCertificate(ctx context.Context, attrs *xkmssdk.KeyAttributes, cert *x509.Certificate) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceSaveCertificate(ctx, attrs, cert)
}

// SaveCertificateByID saves a certificate by key ID.
func SaveCertificateByID(ctx context.Context, kid string, cert *x509.Certificate) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceSaveCertificateByID(ctx, kid, cert)
}

// DeleteCertificate deletes a certificate by key attributes.
func DeleteCertificate(ctx context.Context, attrs *xkmssdk.KeyAttributes) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceDeleteCertificate(ctx, attrs)
}

// DeleteCertificateByID deletes a certificate by key ID.
func DeleteCertificateByID(ctx context.Context, kid string) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceDeleteCertificateByID(ctx, kid)
}

// CertificateExists checks if a certificate exists for the given key attributes.
func CertificateExists(ctx context.Context, attrs *xkmssdk.KeyAttributes) (bool, error) {
	if err := checkInitialized(); err != nil {
		return false, err
	}

	return xkmssdk.ServiceCertificateExists(ctx, attrs)
}

// CertificateExistsByID checks if a certificate exists for the given key ID.
func CertificateExistsByID(ctx context.Context, kid string) (bool, error) {
	if err := checkInitialized(); err != nil {
		return false, err
	}

	return xkmssdk.ServiceCertificateExistsByID(ctx, kid)
}

// ListCertificates lists all certificate IDs across all backends or from a specific backend.
func ListCertificates(ctx context.Context, backendName ...string) ([]string, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceListCertificates(ctx, backendName...)
}

// -----------------------------------------------------------------------------
// Certificate Chain Operations
// -----------------------------------------------------------------------------

// CertificateChain retrieves a certificate chain by key attributes.
func CertificateChain(ctx context.Context, attrs *xkmssdk.KeyAttributes) ([]*x509.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	chain, err := xkmssdk.ServiceCertificateChain(ctx, attrs)
	if err != nil {
		return nil, &api.CertificateOperationError{Cause: err}
	}

	return chain, nil
}

// CertificateChainByID retrieves a certificate chain by key ID.
func CertificateChainByID(ctx context.Context, kid string) ([]*x509.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	chain, err := xkmssdk.ServiceCertificateChainByID(ctx, kid)
	if err != nil {
		return nil, &api.CertificateOperationError{Cause: err}
	}

	return chain, nil
}

// SaveCertificateChain saves a certificate chain using key attributes.
func SaveCertificateChain(ctx context.Context, attrs *xkmssdk.KeyAttributes, chain []*x509.Certificate) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceSaveCertificateChain(ctx, attrs, chain)
}

// SaveCertificateChainByID saves a certificate chain by key ID.
func SaveCertificateChainByID(ctx context.Context, kid string, chain []*x509.Certificate) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceSaveCertificateChainByID(ctx, kid, chain)
}

// -----------------------------------------------------------------------------
// TLS Operations
// -----------------------------------------------------------------------------

// TLSCertificate returns a complete tls.Certificate for the given key attributes.
func TLSCertificate(ctx context.Context, attrs *xkmssdk.KeyAttributes) (tls.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return tls.Certificate{}, err
	}

	return xkmssdk.ServiceTLSCertificate(ctx, attrs)
}

// TLSCertificateByID returns a complete tls.Certificate by key ID.
func TLSCertificateByID(ctx context.Context, kid string) (tls.Certificate, error) {
	if err := checkInitialized(); err != nil {
		return tls.Certificate{}, err
	}

	return xkmssdk.ServiceTLSCertificateByID(ctx, kid)
}

// -----------------------------------------------------------------------------
// Backend Access
// -----------------------------------------------------------------------------

// BackendInfo contains information about a backend.
type BackendInfo = xkmssdk.ServiceBackendInfo

// GetBackend returns a specific backend by name.
func GetBackend(name string) (xkmssdk.ServiceBackend, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceGetBackend(name)
}

// DefaultBackend returns the default backend.
func DefaultBackend() (xkmssdk.ServiceBackend, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceDefaultBackend()
}

// DefaultBackendName returns the registered name (StoreType) of the default
// backend. The underlying go-xkms service does not export the default backend's
// name directly, so this resolves it by matching the default backend instance
// against the named registrations. This name is the StoreType callers must set
// on KeyAttributes for backends (e.g. the software keystore) that require an
// explicit store type during key generation.
func DefaultBackendName() (string, error) {
	if err := checkInitialized(); err != nil {
		return "", err
	}

	def, err := xkmssdk.ServiceDefaultBackend()
	if err != nil {
		return "", err
	}

	names := xkmssdk.ServiceBackends()
	// Single-backend setups are unambiguous.
	if len(names) == 1 {
		return names[0], nil
	}
	for _, name := range names {
		b, getErr := xkmssdk.ServiceGetBackend(name)
		if getErr != nil {
			continue
		}
		if b == def {
			return name, nil
		}
	}
	return "", &api.ConfigurationError{Action: "resolve default xkms backend name"}
}

// RegisterBackend registers a named backend with the already-initialized
// global service. It is used at boot when initializeCA has already
// initialized the singleton from the CA key universe and startKMS resolves an
// additional backend (e.g. tpm2, pkcs11, a cloud KMS) that must participate
// in key operations. Registering a name that already exists REPLACES the
// previous instance, so callers that do not intend replacement must check
// Backends() first.
func RegisterBackend(name string, backend xkmssdk.ServiceBackend) error {
	if err := checkInitialized(); err != nil {
		return err
	}
	if err := xkmssdk.RegisterServiceBackend(name, backend); err != nil {
		return &api.XKMSBackendRegistrationError{Backend: name, Cause: err}
	}
	return nil
}

// BackendFor returns the appropriate backend for the given key attributes.
func BackendFor(attrs *xkmssdk.KeyAttributes) (xkmssdk.ServiceBackend, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceBackendFor(attrs)
}

// Backends returns the names of all registered backends.
func Backends() []string {
	if err := checkInitialized(); err != nil {
		return nil
	}

	return xkmssdk.ServiceBackends()
}

// GetBackendInfo returns information about a specific backend.
func GetBackendInfo(name string) (*BackendInfo, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceGetBackendInfo(name)
}

// GetBackendCapabilities returns the capabilities of a specific backend.
func GetBackendCapabilities(name string) (xkmssdk.Capabilities, error) {
	if err := checkInitialized(); err != nil {
		return xkmssdk.Capabilities{}, err
	}

	return xkmssdk.ServiceGetBackendCapabilities(name)
}

// -----------------------------------------------------------------------------
// Key Import
// -----------------------------------------------------------------------------

// GetImportParameters retrieves parameters needed to import a key into a backend.
func GetImportParameters(ctx context.Context, backendName string, attrs *xkmssdk.KeyAttributes, algorithm xkmssdk.WrappingAlgorithm) (*xkmssdk.ImportParameters, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceGetImportParameters(ctx, backendName, attrs, algorithm)
}

// WrapKey wraps key material for import into a backend.
func WrapKey(ctx context.Context, backendName string, keyMaterial []byte, params *xkmssdk.ImportParameters) (*xkmssdk.WrappedKeyMaterial, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	return xkmssdk.ServiceWrapKey(ctx, backendName, keyMaterial, params)
}

// ECDHESWrappedData represents the result of ECDH-ES key wrapping.
// It contains the wrapped key in JWE compact serialization format.
type ECDHESWrappedData struct {
	// JWE is the JWE compact serialization of the wrapped key
	JWE string

	// Algorithm is the key encryption algorithm used (e.g., "ECDH-ES+A256KW")
	Algorithm string

	// ContentEncryption is the content encryption algorithm used (e.g., "A256GCM")
	ContentEncryption string
}

// WrapKeyWithECDHES wraps key material using ECDH-ES key agreement.
// This is used for secure key transport to EC key holders.
//
// Parameters:
//   - wrappingKeyID: The key ID of the EC public key to wrap to
//   - keyMaterial: The key material to wrap
//   - algorithm: The key encryption algorithm (e.g., "ECDH-ES+A256KW")
//
// Returns:
//   - ECDHESWrappedData containing the JWE-encoded wrapped key
//   - error if wrapping fails
func WrapKeyWithECDHES(ctx context.Context, wrappingKeyID string, keyMaterial []byte, algorithm string) (*ECDHESWrappedData, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	// Get the wrapping key's public key
	publicKey, err := PublicKey(ctx, wrappingKeyID)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "xkms.wrapKeyWithECDHES",
			Err:  &api.OperationFailedError{Operation: "get wrapping public key", Cause: err},
		}
	}

	// Ensure it's an EC public key
	ecPublicKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "xkms.wrapKeyWithECDHES",
			Err:  &api.UnexpectedKeyTypeError{Expected: "ECDH-ES EC public key", Actual: fmt.Sprintf("%T", publicKey)},
		}
	}

	// Map algorithm to content encryption
	contentEnc := "A256GCM" // Default content encryption
	switch algorithm {
	case "ECDH-ES+A128KW":
		contentEnc = "A128GCM"
	case "ECDH-ES+A192KW":
		contentEnc = "A192GCM"
	case "ECDH-ES+A256KW":
		contentEnc = "A256GCM"
	case "ECDH-ES":
		// Direct key agreement, content encryption matches key size
		contentEnc = "A256GCM"
	}

	// Create JWE encrypter
	encrypter, err := xkmssdk.NewJWEEncrypter(algorithm, contentEnc, ecPublicKey)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "xkms.wrapKeyWithECDHES",
			Err:  &api.OperationFailedError{Operation: "create JWE encrypter", Cause: err},
		}
	}

	// Encrypt the key material
	jweStr, err := encrypter.Encrypt(keyMaterial)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrEncryption,
			Op:   "xkms.wrapKeyWithECDHES",
			Err:  &api.OperationFailedError{Operation: "encrypt key material", Cause: err},
		}
	}

	return &ECDHESWrappedData{
		JWE:               jweStr,
		Algorithm:         algorithm,
		ContentEncryption: contentEnc,
	}, nil
}

// UnwrapKeyWithECDHES unwraps key material that was wrapped using ECDH-ES.
//
// Parameters:
//   - privateKeyID: The key ID of the EC private key to unwrap with
//   - jweData: The JWE compact serialization string
//
// Returns:
//   - The unwrapped key material
//   - error if unwrapping fails
func UnwrapKeyWithECDHES(ctx context.Context, privateKeyID string, jweData string) ([]byte, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	// Try to get a decrypter first (for hardware-backed keys)
	decrypter, err := DecrypterByID(ctx, privateKeyID)
	if err == nil {
		// Use xkms decrypter via callback
		xkmsDec := xkmssdk.NewJWEXKMSDecrypter(func(kid string) (crypto.Decrypter, error) {
			return DecrypterByID(ctx, privateKeyID)
		})

		plaintext, err := xkmsDec.DecryptWithKeyID(jweData, privateKeyID)
		if err != nil {
			return nil, &dberrors.QBError{
				Code: dberrors.ErrDecryption,
				Op:   "xkms.unwrapKeyWithECDHES",
				Err:  &api.OperationFailedError{Operation: "decrypt JWE with xkms", Cause: err},
			}
		}

		return plaintext, nil
	}

	// Fall back to getting the private key directly (for software keys)
	privateKey, err := KeyByID(ctx, privateKeyID)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "xkms.unwrapKeyWithECDHES",
			Err:  &api.OperationFailedError{Operation: "get private key", Cause: err},
		}
	}

	// Use the private key directly
	ecPrivateKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "xkms.unwrapKeyWithECDHES",
			Err:  &api.UnexpectedKeyTypeError{Expected: "ECDH-ES EC private key", Actual: fmt.Sprintf("%T", privateKey)},
		}
	}

	jweDecrypter := xkmssdk.NewJWEDecrypter()
	plaintext, err := jweDecrypter.Decrypt(jweData, ecPrivateKey)
	if err != nil {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrDecryption,
			Op:   "xkms.unwrapKeyWithECDHES",
			Err:  &api.OperationFailedError{Operation: "decrypt JWE", Cause: err},
		}
	}

	// Suppress unused variable warning for crypto.Decrypter
	_ = decrypter

	return plaintext, nil
}

// IsECDHESAlgorithm checks if the algorithm is an ECDH-ES key wrapping algorithm.
func IsECDHESAlgorithm(algorithm string) bool {
	return strings.HasPrefix(algorithm, "ECDH-ES")
}

// ImportKey imports externally generated key material into a backend.
func ImportKey(ctx context.Context, backendName string, attrs *xkmssdk.KeyAttributes, wrapped *xkmssdk.WrappedKeyMaterial) error {
	if err := checkInitialized(); err != nil {
		return err
	}

	return xkmssdk.ServiceImportKey(ctx, backendName, attrs, wrapped)
}

// -----------------------------------------------------------------------------
// Cleanup
// -----------------------------------------------------------------------------

// Close closes all backends and the RNG (called at application shutdown).
func Close() error {
	initMu.Lock()
	defer initMu.Unlock()

	if !initialized {
		// Still close RNG even if xkms wasn't initialized
		// since RNG auto-initializes on first use
		return CloseRNG()
	}

	var errs []error

	// Close the RNG first
	if err := CloseRNG(); err != nil {
		errs = append(errs, &api.OperationFailedError{Operation: "close RNG", Cause: err})
	}

	// Close xkms backends
	if err := xkmssdk.ServiceClose(); err != nil {
		errs = append(errs, &api.OperationFailedError{Operation: "close xkms", Cause: err})
	}

	// go-xkms's Close leaves its singleton registered, and its Initialize
	// runs once per process, so a later Initialize returned success while
	// every operation still reached the closed backends ("storage is
	// closed"). Drop the singleton so the next Initialize builds a fresh one.
	xkmssdk.ServiceReset()

	initialized = false
	logger = nil

	if len(errs) > 0 {
		return &api.CloseError{Errors: errs}
	}
	return nil
}

// -----------------------------------------------------------------------------
// JWT Operations
// -----------------------------------------------------------------------------

// JWTSigner wraps go-xkms's JWT signer for xkms-backed signing.
type JWTSigner = xkmssdk.JWTXKMSSigner

// JWTVerifier wraps go-xkms's JWT verifier for xkms-backed verification.
type JWTVerifier = xkmssdk.JWTXKMSVerifier

// JWTClaims is an alias for jwt.MapClaims for convenience.
type JWTClaims = jwt.MapClaims

// JWTAlgorithm represents supported JWT signing algorithms.
type JWTAlgorithm = xkmssdk.JWTAlgorithm

// JWT algorithm constants.
const (
	JWTAlgRS256 JWTAlgorithm = xkmssdk.JWTRS256
	JWTAlgRS384 JWTAlgorithm = xkmssdk.JWTRS384
	JWTAlgRS512 JWTAlgorithm = xkmssdk.JWTRS512
	JWTAlgES256 JWTAlgorithm = xkmssdk.JWTES256
	JWTAlgES384 JWTAlgorithm = xkmssdk.JWTES384
	JWTAlgES512 JWTAlgorithm = xkmssdk.JWTES512
	JWTAlgEdDSA JWTAlgorithm = xkmssdk.JWTEdDSA
	JWTAlgPS256 JWTAlgorithm = xkmssdk.JWTPS256
	JWTAlgPS384 JWTAlgorithm = xkmssdk.JWTPS384
	JWTAlgPS512 JWTAlgorithm = xkmssdk.JWTPS512
)

// ErrJWTSigningFailed is returned when JWT signing fails.
var ErrJWTSigningFailed = &api.JWTSigningError{}

// ErrJWTVerificationFailed is returned when JWT verification fails.
var ErrJWTVerificationFailed = &api.JWTVerificationError{}

// ErrJWTParsingFailed is returned when JWT parsing fails.
var ErrJWTParsingFailed = &api.JWTParsingError{}

// NewJWTSigner creates a new xkms-backed JWT signer.
// This signer uses keys from the xkms package, which may be hardware-backed (HSM, TPM).
func NewJWTSigner() *JWTSigner {
	return xkmssdk.NewJWTXKMSSigner(
		func(keyID string) (crypto.PrivateKey, error) {
			return KeyByID(context.Background(), keyID)
		},
		func(keyID string) (crypto.Signer, error) {
			return SignerByID(context.Background(), keyID)
		},
	)
}

// NewJWTVerifier creates a new xkms-backed JWT verifier.
// This verifier uses keys from the xkms package for signature verification.
func NewJWTVerifier() *JWTVerifier {
	return xkmssdk.NewJWTXKMSVerifier(func(keyID string) (crypto.PrivateKey, error) {
		return KeyByID(context.Background(), keyID)
	})
}

// JWTSign signs a JWT using the specified xkms key.
// The kid is used to identify the signing key in the xkms package.
// Algorithm is automatically determined from the key type.
//
// Example:
//
//	claims := xkms.JWTClaims{
//	    "sub": "user123",
//	    "exp": time.Now().Add(time.Hour).Unix(),
//	}
//	token, err := xkms.JWTSign("signing-key", claims)
func JWTSign(kid string, claims JWTClaims) (string, error) {
	if err := checkInitialized(); err != nil {
		return "", err
	}

	signer := NewJWTSigner()
	token, err := signer.SignWithKeyID(kid, claims)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return token, nil
}

// JWTSignWithAlgorithm signs a JWT using a specific algorithm.
// This allows overriding the automatic algorithm selection.
//
// Example:
//
//	token, err := xkms.JWTSignWithAlgorithm("rsa-key", claims, xkms.JWTAlgRS512)
func JWTSignWithAlgorithm(kid string, claims JWTClaims, alg JWTAlgorithm) (string, error) {
	if err := checkInitialized(); err != nil {
		return "", err
	}

	signer := NewJWTSigner()
	token, err := signer.SignWithKeyIDAndAlgorithm(kid, claims, alg)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return token, nil
}

// JWTSignWithSigner signs a JWT using a provided crypto.Signer.
// This is useful when you already have a signer from the xkms package.
// Note: This function does not require xkms initialization since
// the signer is provided directly.
//
// Example:
//
//	signer, _ := xkms.SignerByID("signing-key")
//	token, err := xkms.JWTSignWithSigner(signer, claims, "RS256")
func JWTSignWithSigner(signer crypto.Signer, claims JWTClaims, algorithm string) (string, error) {
	method := jwt.GetSigningMethod(algorithm)
	if method == nil {
		return "", &api.JWTSigningError{Detail: "unsupported algorithm: " + algorithm}
	}

	token := jwt.NewWithClaims(method, claims)
	tokenString, err := token.SignedString(signer)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return tokenString, nil
}

// JWTSignWithSignerKID signs a JWT with the given signer and sets the "kid"
// (Key ID) header so verifiers can select the matching public key from a
// multi-key JWKS. This is required for key rotation: tokens signed by the
// current key carry its kid, and a JWKS publishing both current and previous
// keys lets validators verify either during the rotation overlap window.
// An empty kid behaves identically to JWTSignWithSigner (no kid header).
//
// Example:
//
//	token, err := xkms.JWTSignWithSignerKID(signer, claims, "ES256", kid)
func JWTSignWithSignerKID(signer crypto.Signer, claims JWTClaims, algorithm, kid string) (string, error) {
	method := jwt.GetSigningMethod(algorithm)
	if method == nil {
		return "", &api.JWTSigningError{Detail: "unsupported algorithm: " + algorithm}
	}

	token := jwt.NewWithClaims(method, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	tokenString, err := token.SignedString(signer)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return tokenString, nil
}

// JWTSignWithSignerKIDTyp signs a JWT exactly as JWTSignWithSignerKID but also
// stamps the JOSE "typ" header. This is used to mint RFC 9068 OAuth2 JWT access
// tokens, which MUST carry the explicit media type "at+jwt" so a resource server
// can distinguish an access token from an OIDC ID token (or any other JWT) and
// refuse cross-type confusion. An empty typ behaves identically to
// JWTSignWithSignerKID (no typ header).
//
// Example:
//
//	token, err := xkms.JWTSignWithSignerKIDTyp(signer, claims, "ES256", kid, "at+jwt")
func JWTSignWithSignerKIDTyp(signer crypto.Signer, claims JWTClaims, algorithm, kid, typ string) (string, error) {
	method := jwt.GetSigningMethod(algorithm)
	if method == nil {
		return "", &api.JWTSigningError{Detail: "unsupported algorithm: " + algorithm}
	}

	token := jwt.NewWithClaims(method, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	if typ != "" {
		token.Header["typ"] = typ
	}
	tokenString, err := token.SignedString(signer)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return tokenString, nil
}

// JWTVerify verifies a JWT using the specified xkms key.
//
// Example:
//
//	token, err := xkms.JWTVerify(tokenString, "signing-key")
func JWTVerify(tokenString, kid string) (*jwt.Token, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	verifier := NewJWTVerifier()
	token, err := verifier.VerifyWithKeyID(tokenString, kid)
	if err != nil {
		return nil, &api.JWTVerificationError{Cause: err}
	}

	return token, nil
}

// JWTVerifyAuto verifies a JWT by extracting the kid from the token header
// and using it to retrieve the verification key from the xkms package.
//
// Example:
//
//	token, err := xkms.JWTVerifyAuto(tokenString)
func JWTVerifyAuto(tokenString string) (*jwt.Token, error) {
	if err := checkInitialized(); err != nil {
		return nil, err
	}

	verifier := NewJWTVerifier()
	token, err := verifier.VerifyWithAutoKeyID(tokenString)
	if err != nil {
		return nil, &api.JWTVerificationError{Cause: err}
	}

	return token, nil
}

// JWTExtractKID extracts the Key ID (kid) from a JWT token header without verifying.
//
// Example:
//
//	kid, err := xkms.JWTExtractKID(tokenString)
func JWTExtractKID(tokenString string) (string, error) {
	kid, err := xkmssdk.ExtractJWTKID(tokenString)
	if err != nil {
		return "", &api.JWTParsingError{Cause: err}
	}
	return kid, nil
}

// JWTParse parses a JWT without verification (use with caution).
// This is useful for extracting claims when you need to process them before verification.
// Note: This function does not require xkms initialization since
// it only parses the token string without accessing xkms.
//
// Example:
//
//	claims, err := xkms.JWTParse(tokenString)
func JWTParse(tokenString string) (JWTClaims, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, &api.JWTParsingError{Cause: err}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &api.JWTParsingError{Detail: "invalid claims type"}
	}

	return JWTClaims(claims), nil
}

// JWTParseAlgorithm converts an algorithm string to a JWTAlgorithm type.
//
// Example:
//
//	alg, err := xkms.JWTParseAlgorithm("RS256")
func JWTParseAlgorithm(alg string) (JWTAlgorithm, error) {
	return xkmssdk.ParseJWTAlgorithm(alg)
}

// JWTVerifyWithKey verifies a JWT using a provided public key.
// This is useful when you have the public key directly and don't need xkms lookup.
// Note: This function does not require xkms initialization since
// the public key is provided directly.
//
// Example:
//
//	claims, err := xkms.JWTVerifyWithKey(tokenString, publicKey, "RS256")
func JWTVerifyWithKey(tokenString string, publicKey crypto.PublicKey, expectedAlgorithm string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method if algorithm specified
		if expectedAlgorithm != "" && token.Method.Alg() != expectedAlgorithm {
			return nil, &api.UnexpectedSigningMethodError{Algorithm: token.Header["alg"]}
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, &api.JWTVerificationError{Cause: err}
	}

	if !token.Valid {
		return nil, &api.JWTVerificationError{Detail: "invalid token"}
	}

	return token, nil
}

// JWTVerifyWithKeyAndClaims verifies a JWT using a provided public key and parses claims.
// Returns the parsed claims as a map.
// Note: This function does not require xkms initialization since
// the public key is provided directly.
//
// Example:
//
//	claims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, publicKey, "RS256")
//	userID := claims["sub"].(string)
func JWTVerifyWithKeyAndClaims(tokenString string, publicKey crypto.PublicKey, expectedAlgorithm string) (JWTClaims, error) {
	token, err := JWTVerifyWithKey(tokenString, publicKey, expectedAlgorithm)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &api.JWTParsingError{Detail: "invalid claims type"}
	}

	return JWTClaims(claims), nil
}

// -----------------------------------------------------------------------------
// HMAC JWT Operations (Symmetric)
// -----------------------------------------------------------------------------

// JWTSignHMAC signs a JWT using HMAC (HS256) with a shared secret.
// This is for symmetric JWT signing where a shared secret is used.
// Note: This function does not require xkms initialization.
//
// Example:
//
//	claims := xkms.JWTClaims{
//	    "sub": "user123",
//	    "exp": time.Now().Add(time.Hour).Unix(),
//	}
//	token, err := xkms.JWTSignHMAC(claims, []byte("my-secret"))
func JWTSignHMAC(claims JWTClaims, secret []byte) (string, error) {
	return JWTSignHMACWithAlgorithm(claims, secret, "HS256")
}

// JWTSignHMACWithAlgorithm signs a JWT using HMAC with a specific algorithm.
// Supported algorithms: HS256, HS384, HS512.
// Note: This function does not require xkms initialization.
//
// Example:
//
//	token, err := xkms.JWTSignHMACWithAlgorithm(claims, []byte("secret"), "HS384")
func JWTSignHMACWithAlgorithm(claims JWTClaims, secret []byte, algorithm string) (string, error) {
	method := jwt.GetSigningMethod(algorithm)
	if method == nil {
		return "", &api.JWTSigningError{Detail: "unsupported algorithm: " + algorithm}
	}

	// Verify it's an HMAC method
	if _, ok := method.(*jwt.SigningMethodHMAC); !ok {
		return "", &api.JWTSigningError{Detail: "algorithm " + algorithm + " is not HMAC"}
	}

	token := jwt.NewWithClaims(method, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", &api.JWTSigningError{Cause: err}
	}

	return tokenString, nil
}

// JWTVerifyHMAC verifies an HMAC-signed JWT with a shared secret.
// Returns the parsed claims.
// Note: This function does not require xkms initialization.
//
// Example:
//
//	claims, err := xkms.JWTVerifyHMAC(tokenString, []byte("my-secret"))
func JWTVerifyHMAC(tokenString string, secret []byte) (JWTClaims, error) {
	return JWTVerifyHMACWithAlgorithm(tokenString, secret, "")
}

// JWTVerifyHMACWithAlgorithm verifies an HMAC-signed JWT with algorithm validation.
// If expectedAlgorithm is empty, any HMAC algorithm is accepted.
// Note: This function does not require xkms initialization.
//
// Example:
//
//	claims, err := xkms.JWTVerifyHMACWithAlgorithm(tokenString, []byte("secret"), "HS256")
func JWTVerifyHMACWithAlgorithm(tokenString string, secret []byte, expectedAlgorithm string) (JWTClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify it's an HMAC method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, &api.UnexpectedSigningMethodError{Algorithm: token.Header["alg"]}
		}

		// Verify specific algorithm if specified
		if expectedAlgorithm != "" && token.Method.Alg() != expectedAlgorithm {
			return nil, &api.UnexpectedSigningMethodError{Algorithm: fmt.Sprintf("%v, expected %s", token.Header["alg"], expectedAlgorithm)}
		}

		return secret, nil
	})

	if err != nil {
		return nil, &api.JWTVerificationError{Cause: err}
	}

	if !token.Valid {
		return nil, &api.JWTVerificationError{Detail: "invalid token"}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &api.JWTParsingError{Detail: "invalid claims type"}
	}

	return JWTClaims(claims), nil
}

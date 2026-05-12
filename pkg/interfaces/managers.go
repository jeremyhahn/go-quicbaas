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

// Package interfaces defines core interfaces for QuicBaaS components.
package interfaces

import (
	"context"
	"crypto"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// CacheManager defines the interface for cache operations.
// This interface allows for mock implementations in tests.
// Note: Methods with type-specific return values (Stats, GetConfig, UpdateConfig)
// are not included in the interface to allow the concrete *cache.CacheManager
// to implement this interface. Use type assertions when needed.
type CacheManager interface {
	// Get retrieves a value from the cache.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in the cache.
	Set(ctx context.Context, key string, value []byte) error

	// Delete removes a value from the cache.
	Delete(ctx context.Context, key string) error

	// Clear removes all entries from the cache.
	Clear()

	// Close closes the cache manager and releases resources.
	Close() error
}

// UserInfo represents basic user information returned by UserStore.
// This interface abstracts the concrete auth.User type.
type UserInfo interface {
	GetID() string
	GetUsername() string
	GetRoles() []string
	GetCreated() time.Time
	GetLastAuth() time.Time
}

// UserStore defines the interface for user management operations.
// This interface allows for mock implementations in tests.
type UserStore interface {
	// AddUser adds a user to the store.
	AddUser(user UserInfo)

	// GetByID retrieves a user by ID.
	GetByID(userID string) UserInfo

	// GetByUsername retrieves a user by username.
	GetByUsername(username string) UserInfo

	// UpdateUser updates user information.
	UpdateUser(user UserInfo)

	// DeleteUser removes a user.
	DeleteUser(userID string)

	// ListUsers returns all users.
	ListUsers() []UserInfo

	// AssignRole assigns a role to a user.
	AssignRole(userID, roleName string) error

	// RevokeRole revokes a role from a user.
	RevokeRole(userID, roleName string) error

	// SetRoleManager sets the role manager for role validation.
	SetRoleManager(rm RoleManager)
}

// PermissionInfo represents permission information.
type PermissionInfo interface {
	GetResource() string
	GetActions() []string
}

// RoleInfo represents basic role information returned by RoleManager.
// This interface abstracts the concrete auth.Role type.
type RoleInfo interface {
	GetName() string
	GetDescription() string
	GetPermissions() []PermissionInfo
}

// RoleManager defines the interface for role-based access control operations.
// This interface allows for mock implementations in tests.
type RoleManager interface {
	// HasPermission checks if any of the user's roles have permission for the action.
	HasPermission(userRoles []string, resource, action string) bool

	// AllowsAnonymous checks if a resource/action allows anonymous access.
	AllowsAnonymous(resource, action string) bool

	// AddRole adds a custom role.
	AddRole(role RoleInfo) error

	// GetRole retrieves a role by name.
	GetRole(name string) RoleInfo

	// ListRoles returns all available roles.
	ListRoles() []RoleInfo

	// DeleteRole removes a custom role (cannot delete built-in roles).
	DeleteRole(name string) error

	// UpdateRole updates an existing role.
	UpdateRole(role RoleInfo) error

	// ValidateRoles checks if all role names exist.
	ValidateRoles(roleNames []string) bool
}

// AuthManager defines the interface for authentication operations.
// This interface allows for mock implementations in tests.
//
// Authentication Flow:
//  1. Default: mTLS with client certificates (server generates admin cert on bootstrap)
//  2. Optional: FIDO2/WebAuthn key registration after initial mTLS connection
//  3. After successful authentication, call IssueTokenForUser to get JWT tokens
type AuthManager interface {
	// Authenticate validates credentials and issues authentication tokens.
	// DEPRECATED: Password-based authentication is disabled. Use mTLS certificates
	// or FIDO2/WebAuthn for authentication, then call IssueTokenForUser.
	// Returns *auth.Token
	Authenticate(ctx context.Context, username string, password *secure.Buffer) (interface{}, error)

	// IssueTokenForUser issues JWT tokens for a user who has been authenticated
	// via mTLS certificate, FIDO2/WebAuthn, or other IAM provider.
	// This should be called after successful authentication verification.
	// Returns *auth.Token
	IssueTokenForUser(ctx context.Context, user UserInfo) (interface{}, error)

	// Authorize checks if a token has permission to perform an action on a resource.
	Authorize(ctx context.Context, token, resource, action string) error

	// RefreshToken generates a new access token from a refresh token.
	// Returns *auth.Token
	RefreshToken(ctx context.Context, refreshToken string) (interface{}, error)

	// RevokeToken revokes an authentication token.
	RevokeToken(ctx context.Context, token string) error

	// ValidateToken validates a token and returns validation information.
	// Returns *auth.TokenValidationResult
	ValidateToken(ctx context.Context, token string) (interface{}, error)

	// CreateUser creates a new user with specified roles.
	// Returns *auth.User
	CreateUser(username, password string, roles []string) (interface{}, error)

	// GetUserStore returns the user store for user management operations.
	GetUserStore() UserStore

	// GetRoleManager returns the role manager for RBAC operations.
	GetRoleManager() RoleManager
}

// BackupManager defines the interface for backup operations.
// This interface allows for mock implementations in tests.
type BackupManager interface {
	// CreateBackup creates a new backup.
	// backupType should be backup.BackupType, returns *backup.Backup
	CreateBackup(ctx context.Context, backupType interface{}) (interface{}, error)

	// RestoreBackup restores from a backup.
	// opts should be *backup.RestoreOptions
	RestoreBackup(ctx context.Context, backupID string, opts interface{}) error

	// ListBackups returns a list of available backups.
	// Returns *backup.BackupList
	ListBackups(ctx context.Context, limit int) (interface{}, error)

	// DeleteBackup deletes a backup.
	DeleteBackup(ctx context.Context, backupID string) error

	// VerifyBackup verifies the integrity of a backup.
	// Returns *backup.VerificationResult
	VerifyBackup(ctx context.Context, backupID string) (interface{}, error)

	// Close closes the backup manager.
	Close() error
}

// KMSManager defines the interface for Key Management Service operations.
// This interface provides a unified abstraction over various KMS backends
// (local, Vault, AWS KMS, Azure Key Vault, GCP Cloud KMS, HSM, TPM).
type KMSManager interface {
	// GenerateKey creates a new cryptographic key with the specified parameters.
	// attrs contains the key configuration from go-xkms including:
	//   - CN: common name/identifier for the key
	//   - KeyAlgorithm: public key algorithm (RSA, ECDSA, Ed25519)
	//   - SymmetricAlgorithm: symmetric algorithm (AES-GCM, ChaCha20-Poly1305)
	//   - StoreType: backend storage type
	//   - RSAAttributes/ECCAttributes: algorithm-specific configuration
	// Returns key metadata (never raw key material).
	GenerateKey(ctx context.Context, attrs *types.KeyAttributes) (interface{}, error)

	// ImportKey imports an existing key into the KMS.
	// material contains the raw key bytes (will be encrypted at rest).
	// Returns key metadata.
	ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error)

	// GetKey retrieves key metadata by ID.
	// Never returns raw key material.
	GetKey(ctx context.Context, keyID string) (interface{}, error)

	// GetPublicKey retrieves the DER-encoded public key for an asymmetric key.
	// Returns an error if the key is symmetric (has no public component).
	GetPublicKey(ctx context.Context, keyID string) ([]byte, error)

	// UpdateStoredMetadata updates metadata fields (Name, Description, Tags)
	// for a key in the internal store. This is used to persist application-level
	// metadata that isn't part of the cryptographic key itself.
	// metadata should be *service.KeyMetadata.
	UpdateStoredMetadata(metadata interface{}) error

	// ListKeys returns all keys matching the optional prefix.
	ListKeys(ctx context.Context, prefix string) ([]interface{}, error)

	// RotateKey creates a new version of an existing key.
	// The old version remains available for decryption.
	RotateKey(ctx context.Context, keyID string) (interface{}, error)

	// RevokeKey marks a key as revoked (cannot be used for new operations).
	RevokeKey(ctx context.Context, keyID string) error

	// EnableKey enables a disabled key for use.
	EnableKey(ctx context.Context, keyID string) error

	// DisableKey disables a key, preventing its use for new operations.
	DisableKey(ctx context.Context, keyID string) error

	// DeleteKey permanently removes a key.
	// Most backends require the key to be disabled first.
	DeleteKey(ctx context.Context, keyID string) error

	// Encrypt encrypts plaintext using the specified key.
	// aad is optional additional authenticated data for AEAD ciphers.
	Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)

	// Decrypt decrypts ciphertext using the specified key.
	// aad must match the value used during encryption.
	Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)

	// Sign creates a digital signature of the digest.
	// algorithm specifies the signature algorithm (e.g., "SHA256withRSA", "ECDSA-P256-SHA256").
	Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error)

	// Signer returns a crypto.Signer backed by the stored key. The returned
	// signer delegates Sign calls to the KMS backend so the private
	// material never leaves the KMS boundary. Callers that need to interop
	// with x/crypto/ssh, crypto/tls, and other standard-library APIs that
	// accept crypto.Signer can use this without owning the raw key.
	Signer(ctx context.Context, keyID string) (crypto.Signer, error)

	// Verify validates a signature against the digest.
	Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error)

	// GenerateDataKey generates a new data encryption key.
	// attrs contains the key configuration; keyBytes specifies the key size.
	// Returns both plaintext and wrapped (encrypted) versions.
	// The wrapped key can be stored; the plaintext should be used and discarded.
	GenerateDataKey(ctx context.Context, attrs *types.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error)

	// GetBackendType returns the backend type (e.g., software, vault, awskms, azurekv, gcpkms).
	GetBackendType() config.BackendType

	// IsAvailable checks if the KMS backend is accessible.
	IsAvailable(ctx context.Context) bool

	// GetSupportedAlgorithms returns the list of cryptographic algorithms
	// supported by the current backend. Different backends support different
	// algorithms (e.g., ChaCha20-Poly1305 is only supported by the software backend).
	GetSupportedAlgorithms(ctx context.Context) ([]string, error)

	// Close releases any resources held by the KMS manager.
	Close() error
}

// StoreOptions specifies encryption options for storing secrets.
type StoreOptions struct {
	// TenantID specifies a tenant-specific DEK to use.
	// If set, uses "dek:tenant:{TenantID}" as the key identifier.
	TenantID string

	// KeyIdentifier specifies a custom key identifier to use.
	// Can be a DEKID or a KeyID in go-xkms format.
	// Takes precedence over TenantID.
	KeyIdentifier string

	// Algorithm specifies the encryption algorithm to use.
	// If empty, uses the default algorithm configured on the backend.
	Algorithm string
}

// SecretVersionMetadata contains metadata about a specific version of a secret.
// This type is defined in the interfaces package to avoid circular dependencies
// between the interfaces and secrets packages.
type SecretVersionMetadata struct {
	// Version is the version number.
	Version int

	// CreatedAt is when this version was created.
	CreatedAt time.Time

	// Deleted indicates if this version has been destroyed.
	Deleted bool

	// DeletedAt is when this version was destroyed (nil if not destroyed).
	DeletedAt *time.Time
}

// SecretsManager defines the interface for secret storage and retrieval.
// This interface provides access to secrets from various backends
// (internal QuicBaaS, Vault, Kubernetes Secrets, environment, file).
type SecretsManager interface {
	// Store saves a secret at the specified path.
	// metadata contains additional non-sensitive information about the secret.
	Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error

	// StoreWithOptions saves a secret with custom encryption options.
	StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *StoreOptions) error

	// Get retrieves a secret from the specified path.
	// Returns the secret data and metadata.
	Get(ctx context.Context, path string) (data map[string][]byte, metadata map[string]string, err error)

	// GetVersion retrieves a specific version of a secret.
	GetVersion(ctx context.Context, path string, version int) (data map[string][]byte, metadata map[string]string, err error)

	// List returns all secret paths matching the prefix.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes a secret (soft delete, keeps versions).
	Delete(ctx context.Context, path string) error

	// Destroy permanently removes all versions of a secret.
	Destroy(ctx context.Context, path string) error

	// ListVersions returns metadata for all versions of a secret.
	ListVersions(ctx context.Context, path string) ([]*SecretVersionMetadata, error)

	// Rollback restores a previous version as the current version.
	Rollback(ctx context.Context, path string, version int) error

	// GetBackendType returns the backend type (e.g., "internal", "vault", "k8s", "file").
	GetBackendType() string

	// Close releases any resources held by the secrets manager.
	Close() error
}

// CryptoManager defines the interface for crypto encryption operations.
// Crypto encryption allows clients to encrypt/decrypt data without
// having direct access to the encryption keys.
type CryptoManager interface {
	// Encrypt encrypts plaintext using the named key.
	// context is optional application-specific context for key derivation.
	Encrypt(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error)

	// Decrypt decrypts ciphertext using the named key.
	Decrypt(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error)

	// Rewrap re-encrypts ciphertext with the latest key version.
	// Used after key rotation to update encrypted data.
	Rewrap(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error)

	// EncryptBatch encrypts multiple plaintexts in a single call.
	EncryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error)

	// DecryptBatch decrypts multiple ciphertexts in a single call.
	DecryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error)

	// Sign creates a signature of the input data.
	// When preHashed is true, input is already a hash digest and should not be hashed again.
	// This supports crypto.Signer implementations that pass pre-hashed digests.
	Sign(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error)

	// Verify validates a signature.
	Verify(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error)

	// GenerateHMAC generates an HMAC of the input.
	GenerateHMAC(ctx context.Context, keyID string, input []byte, algorithm string) ([]byte, error)

	// VerifyHMAC validates an HMAC.
	VerifyHMAC(ctx context.Context, keyID string, input, hmac []byte, algorithm string) (bool, error)

	// GenerateRandom generates cryptographically secure random bytes.
	GenerateRandom(ctx context.Context, numBytes int) ([]byte, error)

	// Hash computes a cryptographic hash of the input.
	Hash(ctx context.Context, input []byte, algorithm string) ([]byte, error)

	// Close releases any resources held by the crypto manager.
	Close() error
}

// BarrierManager defines the interface for cryptographic barrier operations.
// The barrier protects the master encryption key at rest using a strategy
// chain (software passphrase, Shamir secret sharing, TPM2, PKCS#11, AWS
// KMS, GCP KMS, Azure Key Vault, HashiCorp Vault). Concrete implementations
// live in pkg/barrier and forward to a go-qrdb StorageBarrier.
type BarrierManager interface {
	// Status returns the current barrier status. The returned value carries
	// the active strategy ID so that callers can distinguish software,
	// shamir, and hardware-backed deployments without further probing.
	Status(ctx context.Context) (interface{}, error)

	// IsSealed returns true if the barrier is currently sealed.
	IsSealed() bool

	// IsInitialized returns true if the barrier has been initialized.
	IsInitialized() bool

	// Initialize sets up the barrier with the specified number of shares
	// and threshold. Returns the key shares (which must be distributed to
	// operators). The barrier is left in the sealed state so the caller
	// must drive an explicit unseal flow.
	Initialize(ctx context.Context, shares, threshold int) (interface{}, error)

	// Unseal submits a key share toward the quorum threshold and returns
	// progress information.
	Unseal(ctx context.Context, share []byte) (interface{}, error)

	// Seal re-seals the barrier, clearing the master key from memory.
	Seal(ctx context.Context) error

	// Rekey generates new key shares with potentially different parameters.
	// Requires the barrier to be unsealed.
	Rekey(ctx context.Context, shares, threshold int) (interface{}, error)

	// GenerateRecoveryKeys generates new recovery keys using Shamir secret
	// sharing. Recovery keys provide an emergency unseal path if the
	// primary key shares are lost. threshold is the minimum keys needed
	// and totalKeys is the total number of recovery keys to generate.
	GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error)

	// DeleteRecoveryKeys removes all stored recovery key metadata. This is
	// a destructive operation that cannot be undone.
	DeleteRecoveryKeys(ctx context.Context) error

	// GenerateRootToken derives a new root token by proving knowledge of
	// the master key through key shares. strategy selects between
	// "recovery_keys" and "shamir_shares".
	GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error)

	// RotateRootKey rotates the root encryption key used to protect master
	// key shares. Returns an error if the underlying barrier does not
	// support a standalone rotation primitive.
	RotateRootKey(ctx context.Context) error

	// Close releases any resources held by the barrier manager.
	Close() error
}

// SealManager is a deprecated alias for BarrierManager retained for one
// release while existing service-layer code migrates to the BarrierManager
// name. New code MUST reference BarrierManager directly.
//
// Deprecated: use BarrierManager.
type SealManager = BarrierManager

// ClusterDEKManager defines the interface for cluster-wide Data Encryption Key management.
// The cluster DEK is distributed via Raft and used for encrypting data at rest.
//
// Key Hierarchy:
//  1. Master Key (per node) - from seal, in-memory only
//  2. Cluster DEK (shared) - distributed via Raft, wrapped per-node
//  3. Data Keys (per entity) - wrapped by cluster DEK
type ClusterDEKManager interface {
	// Initialize generates and distributes the initial cluster DEK.
	// Must be called on the leader after the cluster is formed.
	Initialize(ctx context.Context, algorithm string, keyBytes int) error

	// GetStatus returns the current cluster DEK status.
	GetStatus(ctx context.Context) (interface{}, error)

	// Rotate generates a new cluster DEK version and initiates re-encryption.
	// The old version remains available for decryption during cryptoion.
	Rotate(ctx context.Context) error

	// GetCurrentDEK returns the current cluster DEK (for local encryption).
	// Returns nil if not initialized or node is out of sync.
	GetCurrentDEK() ([]byte, error)

	// GetDEKVersion returns the current DEK version number.
	GetDEKVersion() uint64

	// SyncToNode synchronizes the DEK to a specific node.
	// Used for new nodes joining the cluster.
	SyncToNode(ctx context.Context, nodeID uint64) error

	// IsNodeSynced returns true if the specified node has the current DEK.
	IsNodeSynced(nodeID uint64) bool

	// StartReencryption begins background re-encryption of all data.
	// Called after DEK rotation to re-encrypt with the new key.
	StartReencryption(ctx context.Context) error

	// GetReencryptionStatus returns progress of the re-encryption process.
	GetReencryptionStatus(ctx context.Context) (interface{}, error)

	// CancelReencryption stops the re-encryption process.
	CancelReencryption(ctx context.Context) error

	// RecoverDEK recovers the cluster DEK using recovery keys or backup.
	RecoverDEK(ctx context.Context, recoveryData interface{}) error

	// Close releases any resources held by the cluster DEK manager.
	Close() error
}

// ParameterManager defines the interface for parameter storage operations.
// Parameters are tenant-scoped key-value pairs with optional encryption, TTL, and tagging.
// This interface provides low-level storage operations used by the ParameterStoreService.
type ParameterManager interface {
	// Store saves a parameter at the specified path.
	// If the parameter exists and overwrite is true, it will be updated.
	// Returns ErrParameterExists if parameter exists and overwrite is false.
	Store(ctx context.Context, path string, value []byte, metadata *ParameterMetadata, overwrite bool) error

	// StoreWithOptions saves a parameter with custom encryption options.
	StoreWithOptions(ctx context.Context, path string, value []byte, metadata *ParameterMetadata, overwrite bool, opts *StoreOptions) error

	// Get retrieves a parameter from the specified path.
	// Returns the value, metadata, and any error.
	// Returns ErrParameterNotFound if the parameter doesn't exist.
	// Returns ErrParameterExpired if the parameter has expired.
	Get(ctx context.Context, path string, withDecryption bool) (value []byte, metadata *ParameterMetadata, err error)

	// Delete removes a parameter at the specified path.
	// Returns ErrParameterNotFound if the parameter doesn't exist.
	Delete(ctx context.Context, path string) error

	// List returns all parameter paths matching the prefix.
	// If recursive is true, includes nested paths.
	// Returns paths sorted alphabetically.
	List(ctx context.Context, prefix string, recursive bool, limit int, cursor string) (paths []string, nextCursor string, err error)

	// GetBatch retrieves multiple parameters by paths.
	// Returns a map of path to value/metadata, and a map of failed paths to errors.
	GetBatch(ctx context.Context, paths []string, withDecryption bool) (results map[string]*ParameterResult, errors map[string]error, err error)

	// StoreBatch stores multiple parameters atomically.
	// All succeed or all fail.
	StoreBatch(ctx context.Context, params []*ParameterEntry, overwrite bool, opts *StoreOptions) error

	// DeleteBatch removes multiple parameters.
	// Returns a map of path to deletion success/error.
	DeleteBatch(ctx context.Context, paths []string) (results map[string]bool, errors map[string]error, err error)

	// SearchByTags finds parameters matching all specified tags.
	// Tags are ANDed together.
	SearchByTags(ctx context.Context, tags map[string]string, prefix string, limit int, cursor string) (paths []string, nextCursor string, err error)

	// GetBackendType returns the backend type (e.g., "internal", "external").
	GetBackendType() string

	// Close releases any resources held by the parameter manager.
	Close() error
}

// ParameterMetadata contains metadata for a parameter.
type ParameterMetadata struct {
	// Type is the parameter value type (string, number, json, binary).
	Type string

	// Description is optional documentation.
	Description string

	// Tags are key-value labels.
	Tags map[string]string

	// KMSKeyID is the encryption key ID (if encrypted).
	KMSKeyID string

	// IsEncrypted indicates if the value is encrypted.
	IsEncrypted bool

	// TTLSeconds is the time-to-live in seconds (0 = no expiration).
	TTLSeconds int64

	// ExpiresAt is when the parameter expires (nil = no expiration).
	ExpiresAt *time.Time

	// AuditEnabled indicates if audit logging is enabled.
	AuditEnabled bool

	// CreatedAt is when the parameter was created.
	CreatedAt time.Time

	// UpdatedAt is when the parameter was last updated.
	UpdatedAt time.Time

	// CreatedBy is the user who created the parameter.
	CreatedBy string

	// UpdatedBy is the user who last updated the parameter.
	UpdatedBy string
}

// ParameterResult contains the result of getting a parameter.
type ParameterResult struct {
	// Value is the parameter value (decrypted if requested).
	Value []byte

	// Metadata contains the parameter metadata.
	Metadata *ParameterMetadata
}

// ParameterEntry represents a parameter to store in batch operations.
type ParameterEntry struct {
	// Path is the parameter path.
	Path string

	// Value is the parameter value.
	Value []byte

	// Metadata contains the parameter metadata.
	Metadata *ParameterMetadata
}

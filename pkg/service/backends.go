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

// Package service provides high-level service abstractions that coordinate
// backend components for QuicBaaS operations. It acts as a facade layer that
// simplifies complex interactions between Raft consensus, storage, caching,
// authentication, and backup subsystems.
package service

import (
	"context"
	"io"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// StorageBackend defines the interface for direct storage access.
// Provides low-level key-value operations with ACID guarantees.
// Used when bypassing Raft for local-only operations or internal components.
type StorageBackend interface {
	// Basic operations

	// Get retrieves the value for the given key.
	// Returns ErrNotFound if the key doesn't exist.
	Get(key []byte) ([]byte, error)

	// Set stores a key-value pair, overwriting if exists.
	Set(key, value []byte) error

	// Delete removes a key-value pair.
	// No error if key doesn't exist (idempotent).
	Delete(key []byte) error

	// Context-aware operations

	// GetWithContext retrieves a value with cancellation support.
	GetWithContext(ctx context.Context, key []byte) ([]byte, error)

	// SetWithContext stores a value with cancellation support.
	SetWithContext(ctx context.Context, key, value []byte) error

	// Batch operations

	// Batch creates a new atomic write batch.
	// All operations in the batch succeed or fail together.
	Batch() storage.Batch

	// Iterator operations

	// NewIterator creates an iterator for range scans.
	// Must be closed to release resources.
	NewIterator(opts *storage.IteratorOptions) storage.Iterator

	// Snapshot operations

	// Snapshot creates a consistent point-in-time view.
	// Must be closed to release resources.
	Snapshot() (storage.Snapshot, error)

	// Maintenance operations

	// Compact manually triggers compaction for key range.
	// Pass nil for full database compaction.
	Compact(start, end []byte) error

	// Flush ensures all pending writes are persisted to disk.
	Flush() error

	// Close releases all storage resources.
	Close() error
}

// IndexBackend defines the interface for secondary indexing operations.
// Enables fast lookups by entity fields without scanning all keys.
type IndexBackend interface {
	// Index operations

	// QueryIndex retrieves all entity IDs with the given field value.
	// Returns empty slice if no matches found.
	QueryIndex(indexName string, value interface{}) ([]uint64, error)

	// ScanIndex retrieves entity IDs within field value range [start, end).
	// Returns map of field values to entity ID slices.
	ScanIndex(indexName string, start, end interface{}) (map[string][]uint64, error)

	// UpdateIndex adds or updates an index entry for an entity.
	// Used internally when entities are created or modified.
	UpdateIndex(indexName string, entityID uint64, fieldValue interface{}) error

	// RemoveIndex removes index entries for an entity.
	// Used internally when entities are deleted.
	RemoveIndex(indexName string, entityID uint64, fieldValue interface{}) error

	// Close releases index resources.
	Close() error
}

// CacheBackend defines the interface for caching layer operations.
// Provides read-only L1 (in-memory LRU) caching with TTL expiration.
// Cache is advisory - misses should fall through to storage.
type CacheBackend interface {
	// Cache operations

	// Get retrieves a value from cache.
	// Returns ErrNotFound on cache miss.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in cache with TTL.
	// Cache writes are asynchronous and may fail silently.
	Set(ctx context.Context, key string, value []byte) error

	// Delete removes a value from cache.
	// Used for cache invalidation after writes.
	Delete(ctx context.Context, key string) error

	// Clear removes all entries from cache.
	// Used for bulk invalidation or testing.
	Clear()

	// Cache management

	// Stats returns cache statistics (hit rate, size, evictions).
	Stats() CacheStats

	// UpdateConfig applies new cache configuration at runtime.
	// Allows resizing cache or changing TTL without restart.
	UpdateConfig(config *CacheConfig) error

	// Close releases cache resources.
	Close() error
}

// CacheStats represents cache performance metrics.
type CacheStats struct {
	Enabled     bool    // Whether caching is enabled
	L1Size      int     // Current number of entries
	L1Capacity  int     // Maximum entries
	L1Hits      int64   // Total cache hits
	L1Misses    int64   // Total cache misses
	L1HitRate   float64 // Hit rate (0.0 to 1.0)
	L1Evictions int64   // Total evictions due to capacity
}

// CacheConfig configures cache behavior.
type CacheConfig struct {
	L1Size          int           // Maximum entries in L1 cache
	L1TTL           time.Duration // Entry time-to-live
	CleanupInterval time.Duration // Expired entry cleanup frequency
}

// AuthBackend defines the interface for authentication and authorization.
// Provides JWT-based authentication, RBAC, and token lifecycle management.
type AuthBackend interface {
	// Authentication

	// Authenticate validates credentials and issues tokens.
	// Returns access and refresh tokens on success.
	Authenticate(ctx context.Context, username, password string) (*AuthToken, error)

	// RefreshToken generates new access token from refresh token.
	// Extends session without requiring credentials.
	RefreshToken(ctx context.Context, refreshToken string) (*AuthToken, error)

	// Authorization

	// Authorize checks if token has permission for resource/action.
	// Returns nil if authorized, error otherwise.
	Authorize(ctx context.Context, token, resource, action string) error

	// ValidateToken validates token and returns user information.
	// Returns validation result with user ID, roles, expiry.
	ValidateToken(ctx context.Context, token string) (*TokenValidation, error)

	// Token management

	// RevokeToken invalidates a token immediately.
	// Token will fail validation after revocation.
	RevokeToken(ctx context.Context, token string) error

	// User management

	// CreateUser creates a new user with specified roles.
	// Used for initial setup and user provisioning.
	CreateUser(username, password string, roles []string) (*AuthUser, error)
}

// AuthToken represents authentication credentials.
type AuthToken struct {
	AccessToken  string    // JWT access token
	RefreshToken string    // JWT refresh token
	ExpiresAt    time.Time // Access token expiration
	TokenType    string    // Token type (usually "Bearer")
}

// TokenValidation contains token validation results.
type TokenValidation struct {
	Valid     bool      // Whether token is valid
	UserID    string    // User identifier
	Username  string    // User name
	Roles     []string  // User roles for RBAC
	ExpiresAt time.Time // Token expiration time
	IsRevoked bool      // Whether token was revoked
}

// AuthUser represents an authenticated user.
type AuthUser struct {
	ID       string    // User identifier
	Username string    // User name
	Roles    []string  // User roles
	Created  time.Time // Creation timestamp
	LastAuth time.Time // Last authentication timestamp
}

// BackupBackend defines the interface for backup and restore operations.
// Enables point-in-time snapshots of cluster state for disaster recovery.
type BackupBackend interface {
	// Backup operations

	// CreateBackup creates a new backup of specified type.
	// Returns backup metadata including ID, size, and checksum.
	CreateBackup(ctx context.Context, backupType backup.BackupType) (*backup.Backup, error)

	// RestoreBackup restores database state from a backup.
	// Optionally filters by target shards for partial restore.
	RestoreBackup(ctx context.Context, backupID string, opts *backup.RestoreOptions) error

	// Backup management

	// VerifyBackup validates backup integrity (checksums, structure).
	// Returns verification result with details.
	VerifyBackup(ctx context.Context, backupID string) (*backup.VerificationResult, error)

	// ListBackups returns available backups with optional limit.
	// Pass 0 for limit to list all backups.
	ListBackups(ctx context.Context, limit int) (*backup.BackupList, error)

	// DeleteBackup removes a backup from storage.
	// Cannot be undone - use with caution.
	DeleteBackup(ctx context.Context, backupID string) error

	// Policy management

	// ApplyRetentionPolicy enforces configured retention rules.
	// Deletes backups exceeding age or count limits.
	ApplyRetentionPolicy(ctx context.Context) error

	// Close releases backup manager resources.
	Close() error
}

// ShardRouter provides shard selection and routing logic.
// Determines which shard should handle a given key based on routing strategy.
type ShardRouter interface {
	// GetShardForKey returns the shard ID responsible for the key.
	// Uses consistent hashing or other routing algorithm.
	GetShardForKey(key []byte) uint64

	// GetShardForID returns the shard ID for a specific entity ID.
	// Used when entity ID is known directly.
	GetShardForID(entityID uint64) uint64

	// ListShards returns all shard IDs managed by this node.
	ListShards() []uint64

	// IsHealthy returns whether the shard is healthy and available.
	// Checks leader availability and connectivity.
	IsHealthy(shardID uint64) bool
}

// WriteOptions configures write operation behavior.
// Allows fine-grained control over durability and consistency tradeoffs.
type WriteOptions struct {
	// Mode specifies durability guarantee (Sync, WALDurable, Batched).
	Mode qrdbtransport.WriteMode

	// Timeout specifies maximum time to wait for write completion.
	// Zero means use default timeout.
	Timeout time.Duration

	// RetryOnFailure enables automatic retry on transient failures.
	RetryOnFailure bool

	// MaxRetries limits retry attempts (only if RetryOnFailure is true).
	MaxRetries int
}

// ReadOptions configures read operation behavior.
// Allows tuning consistency vs performance tradeoff.
type ReadOptions struct {
	// Consistency specifies required consistency level.
	Consistency qrdbtransport.ConsistencyLevel

	// Timeout specifies maximum time to wait for read completion.
	// Zero means use default timeout.
	Timeout time.Duration

	// AllowStale explicitly allows stale reads (only for Eventual consistency).
	AllowStale bool

	// UseCache enables cache lookup before storage access.
	UseCache bool
}

// BatchOperation represents a single operation in a batch.
// Used for atomic execution of multiple operations.
type BatchOperation struct {
	// Type specifies operation type (Set, Delete).
	Type BatchOpType

	// Key is the operation key.
	Key []byte

	// Value is the operation value (nil for Delete).
	Value []byte
}

// BatchOpType specifies the type of batch operation.
type BatchOpType int

const (
	// BatchOpSet indicates a key-value set operation.
	BatchOpSet BatchOpType = iota

	// BatchOpDelete indicates a key deletion operation.
	BatchOpDelete
)

// String returns string representation of batch operation type.
func (t BatchOpType) String() string {
	switch t {
	case BatchOpSet:
		return "Set"
	case BatchOpDelete:
		return "Delete"
	default:
		return "Unknown"
	}
}

// BackupReaderCloser combines io.Reader with io.Closer for backup streaming.
// Used when downloading backup data from remote storage.
type BackupReaderCloser interface {
	io.Reader
	io.Closer
}

// Default option constructors for convenience.

// DefaultWriteOptions returns write options with recommended defaults.
func DefaultWriteOptions() *WriteOptions {
	return &WriteOptions{
		Mode:           qrdbtransport.WriteModeSync,
		Timeout:        5 * time.Second,
		RetryOnFailure: true,
		MaxRetries:     3,
	}
}

// DefaultReadOptions returns read options with recommended defaults.
func DefaultReadOptions() *ReadOptions {
	return &ReadOptions{
		Consistency: qrdbtransport.ConsistencyLinearizable,
		Timeout:     5 * time.Second,
		AllowStale:  false,
		UseCache:    true,
	}
}

// DefaultCacheConfig returns cache configuration with recommended defaults.
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

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

// Package service provides the unified service facade for QuicBaaS.
package service

import (
	"context"
	"time"
)

// =============================================================================
// Cluster DEK Service - Cluster-Wide Data Encryption Key Management
// =============================================================================

// ClusterDEKService handles cluster-wide Data Encryption Key (DEK) management.
// The cluster DEK is distributed via Raft consensus and used for encrypting
// data at rest across all nodes in the cluster.
//
// Key Hierarchy Overview:
//
//  1. Master Key (per node, from seal)
//     - Derived from Shamir shares during unseal process
//     - Held in memory only, never persisted in plaintext
//     - Used to wrap the node's copy of the cluster DEK
//     - Node-specific: each node has its own master key
//
//  2. Cluster DEK (shared, distributed via Raft)
//     - Single cluster-wide encryption key for all data
//     - Distributed to all nodes via Raft consensus
//     - Each node stores it encrypted (wrapped) by their master key
//     - Rotated periodically for enhanced security
//     - Used to wrap per-entity data keys
//
//  3. Data Keys (per entity, wrapped by cluster DEK)
//     - Generated per-entity for granular encryption
//     - Wrapped by cluster DEK before storage
//     - Provides envelope encryption pattern
//     - Minimizes cluster DEK exposure
//
// Cluster DEK Lifecycle:
//   - Initialize: Creates initial cluster DEK, distributed via Raft
//   - Sync: New nodes receive wrapped cluster DEK from leader
//   - Rotate: Generates new cluster DEK, triggers re-encryption
//   - Re-encrypt: Background process to re-encrypt data with new DEK
//   - Recover: Emergency recovery using recovery keys
//
// Security Model:
//   - Cluster DEK is never stored in plaintext
//   - Each node wraps cluster DEK with their master key
//   - DEK rotation doesn't require node restarts
//   - Re-encryption happens in background with progress tracking
//   - Recovery mechanism for disaster scenarios
type ClusterDEKService interface {
	// =============================================================================
	// DEK Lifecycle Operations
	// =============================================================================

	// InitializeClusterDEK creates and distributes the initial cluster DEK.
	// This should be called once during initial cluster setup after all nodes
	// are unsealed and have their master keys available.
	//
	// Process:
	//   1. Leader generates new cluster DEK
	//   2. DEK is wrapped with leader's master key
	//   3. DEK distributed to all nodes via Raft proposal
	//   4. Each node wraps DEK with their master key and persists
	//   5. Responds when quorum of nodes confirm DEK stored
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Initialize request with algorithm, key size, and metadata
	//
	// Returns:
	//   - InitClusterDEKResponse: Contains DEK version, creation timestamp, nodes synced
	//   - error: ErrUnauthorized, ErrAlreadyInitialized, ErrNotSealed, ErrTimeout
	//
	// Authorization: Requires admin/root privileges
	// Idempotency: Returns existing DEK info if already initialized
	InitializeClusterDEK(ctx context.Context, req *InitClusterDEKRequest) (*InitClusterDEKResponse, error)

	// GetClusterDEKStatus retrieves current cluster DEK status and metadata.
	// This provides visibility into DEK version, age, rotation status, and
	// which nodes have successfully synced the current DEK.
	//
	// Status includes:
	//   - Current DEK version
	//   - Creation and last rotation timestamps
	//   - List of nodes with DEK synced
	//   - Re-encryption progress (if in progress)
	//   - Next scheduled rotation time (if configured)
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//
	// Returns:
	//   - ClusterDEKStatusResponse: Contains comprehensive DEK status
	//   - error: ErrNotInitialized, ErrTimeout
	//
	// Authorization: Requires authenticated access
	GetClusterDEKStatus(ctx context.Context) (*ClusterDEKStatusResponse, error)

	// RotateClusterDEK generates a new cluster DEK and initiates re-encryption.
	// This operation creates a new DEK version while maintaining access to old
	// DEK for reading existing data. Background re-encryption migrates data to
	// new DEK over time.
	//
	// Rotation process:
	//   1. Generate new cluster DEK (version N+1)
	//   2. Distribute new DEK to all nodes via Raft
	//   3. Mark old DEK (version N) as deprecated but readable
	//   4. Initiate background re-encryption process
	//   5. Monitor re-encryption progress
	//   6. Archive old DEK once re-encryption completes
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Rotate request with rotation reason, scheduled time, and options
	//
	// Returns:
	//   - RotateClusterDEKResponse: Contains new DEK version, rotation ID, progress tracking
	//   - error: ErrUnauthorized, ErrNotInitialized, ErrRotationInProgress, ErrTimeout
	//
	// Authorization: Requires admin/root privileges
	// Side effects: Starts background re-encryption process
	RotateClusterDEK(ctx context.Context, req *RotateClusterDEKRequest) (*RotateClusterDEKResponse, error)

	// =============================================================================
	// Node Key Management
	// =============================================================================

	// SyncNodeDEK synchronizes cluster DEK to a specific node.
	// Used when a new node joins the cluster or when a node's local DEK copy
	// is corrupted/lost. The leader provides the wrapped DEK which the node
	// re-wraps with its own master key.
	//
	// Sync process:
	//   1. Node requests DEK from leader
	//   2. Leader verifies node is part of cluster
	//   3. Leader provides current and previous DEK versions (encrypted in crypto)
	//   4. Node unwraps DEK temporarily, re-wraps with its master key
	//   5. Node persists wrapped DEK locally
	//   6. Node confirms sync completion
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Sync request with node ID and current DEK version (if any)
	//
	// Returns:
	//   - SyncNodeDEKResponse: Contains DEK versions synced, sync timestamp
	//   - error: ErrUnauthorized, ErrNodeNotInCluster, ErrNotInitialized, ErrTimeout
	//
	// Authorization: Requires node authentication (mutual TLS)
	// Network: Uses encrypted channel for DEK transfer
	SyncNodeDEK(ctx context.Context, req *SyncNodeDEKRequest) (*SyncNodeDEKResponse, error)

	// GetNodeDEKStatus retrieves DEK sync status for a specific node.
	// This helps diagnose sync issues and verify all nodes have current DEK.
	//
	// Status includes:
	//   - Node ID and name
	//   - Current DEK version on node
	//   - Last sync timestamp
	//   - Sync status (synced, out-of-date, missing)
	//   - Error details if sync failed
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Status request with node ID
	//
	// Returns:
	//   - GetNodeDEKStatusResponse: Contains node-specific DEK status
	//   - error: ErrNodeNotFound, ErrTimeout
	//
	// Authorization: Requires authenticated access
	GetNodeDEKStatus(ctx context.Context, req *GetNodeDEKStatusRequest) (*GetNodeDEKStatusResponse, error)

	// =============================================================================
	// Re-encryption Operations (After DEK Rotation)
	// =============================================================================

	// StartReencryption manually initiates re-encryption of data with new DEK.
	// Typically started automatically after DEK rotation, but can be manually
	// triggered for resuming interrupted re-encryption or controlling timing.
	//
	// Re-encryption process:
	//   1. Scan all encrypted data entities
	//   2. For each entity encrypted with old DEK:
	//      a. Decrypt with old DEK
	//      b. Re-encrypt with new DEK
	//      c. Atomic swap in storage
	//   3. Track progress per node
	//   4. Handle failures with retry logic
	//   5. Mark old DEK for archival when complete
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Re-encryption request with DEK version, batch size, concurrency limits
	//
	// Returns:
	//   - StartReencryptionResponse: Contains operation ID, initial progress status
	//   - error: ErrUnauthorized, ErrNoRotation, ErrAlreadyRunning, ErrTimeout
	//
	// Authorization: Requires admin/root privileges
	// Side effects: CPU and I/O intensive background operation
	StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error)

	// GetReencryptionStatus retrieves current re-encryption progress.
	// Provides detailed status of ongoing or completed re-encryption operations.
	//
	// Status includes:
	//   - Operation ID and state (running, paused, completed, failed)
	//   - Progress percentage and entities processed
	//   - Started timestamp and estimated completion time
	//   - Per-node progress breakdown
	//   - Error count and recent errors
	//   - Performance metrics (entities/sec, throughput)
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//
	// Returns:
	//   - ReencryptionStatusResponse: Contains comprehensive re-encryption status
	//   - error: ErrNoReencryption, ErrTimeout
	//
	// Authorization: Requires authenticated access
	GetReencryptionStatus(ctx context.Context) (*ReencryptionStatusResponse, error)

	// CancelReencryption cancels ongoing re-encryption process.
	// This gracefully stops re-encryption, leaving data in consistent state
	// (each entity is either old-DEK or new-DEK encrypted, never partial).
	//
	// Cancel process:
	//   1. Signal all nodes to stop re-encryption
	//   2. Wait for in-flight operations to complete
	//   3. Persist cancellation state
	//   4. Mark old DEK as still in use
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//
	// Returns:
	//   - error: ErrUnauthorized, ErrNoReencryption, ErrTimeout
	//
	// Authorization: Requires admin/root privileges
	// Side effects: Stops background re-encryption, old DEK remains active
	CancelReencryption(ctx context.Context) error

	// =============================================================================
	// Recovery Operations
	// =============================================================================

	// RecoverDEK recovers cluster DEK using recovery keys or backup mechanism.
	// Used in disaster recovery scenarios when cluster DEK is lost or corrupted
	// across majority of nodes.
	//
	// Recovery methods:
	//   1. Recovery keys (Shamir-split backup of DEK)
	//   2. Encrypted backup from secure storage
	//   3. Manual key injection (break-glass procedure)
	//
	// Recovery process:
	//   1. Verify recovery authorization (requires threshold of recovery keys)
	//   2. Reconstruct or decrypt cluster DEK
	//   3. Wrap DEK with current master keys
	//   4. Distribute to all nodes via Raft
	//   5. Verify data accessibility
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Recovery request with recovery keys, backup location, or manual key
	//
	// Returns:
	//   - RecoverDEKResponse: Contains recovery status, DEK version, audit info
	//   - error: ErrUnauthorized, ErrInvalidRecoveryKeys, ErrThresholdNotMet, ErrTimeout
	//
	// Authorization: Requires recovery key threshold or super-admin access
	// Audit: All recovery attempts are logged for security review
	RecoverDEK(ctx context.Context, req *RecoverDEKRequest) (*RecoverDEKResponse, error)
}

// =============================================================================
// Cluster DEK Service Request/Response Types
// =============================================================================

// InitClusterDEKRequest contains parameters for initializing cluster DEK.
type InitClusterDEKRequest struct {
	// Algorithm specifies the encryption algorithm (e.g., "AES-256-GCM")
	Algorithm string `json:"algorithm"`

	// KeySize is the key size in bits (e.g., 256)
	KeySize int `json:"key_size"`

	// Metadata for audit and tracking
	Metadata map[string]string `json:"metadata,omitempty"`

	// RotationPolicy defines automatic rotation settings
	RotationPolicy *DEKRotationPolicy `json:"rotation_policy,omitempty"`
}

// InitClusterDEKResponse contains the result of DEK initialization.
type InitClusterDEKResponse struct {
	// Version is the initial DEK version (typically 1)
	Version uint64 `json:"version"`

	// CreatedAt is when the DEK was created
	CreatedAt time.Time `json:"created_at"`

	// NodesSynced is the count of nodes that have the DEK
	NodesSynced int `json:"nodes_synced"`

	// Algorithm used for the DEK
	Algorithm string `json:"algorithm"`

	// KeySize in bits
	KeySize int `json:"key_size"`
}

// ClusterDEKStatusResponse contains comprehensive cluster DEK status.
type ClusterDEKStatusResponse struct {
	// Initialized indicates if cluster DEK has been set up
	Initialized bool `json:"initialized"`

	// CurrentVersion is the active DEK version
	CurrentVersion uint64 `json:"current_version"`

	// CreatedAt is when current DEK was created
	CreatedAt time.Time `json:"created_at"`

	// RotatedAt is when DEK was last rotated (nil if never rotated)
	RotatedAt *time.Time `json:"rotated_at,omitempty"`

	// NextRotation is scheduled rotation time (if configured)
	NextRotation *time.Time `json:"next_rotation,omitempty"`

	// NodesSynced is count of nodes with current DEK
	NodesSynced int `json:"nodes_synced"`

	// TotalNodes is total nodes in cluster
	TotalNodes int `json:"total_nodes"`

	// NodesOutOfSync lists nodes missing current DEK
	NodesOutOfSync []NodeDEKInfo `json:"nodes_out_of_sync,omitempty"`

	// Algorithm for current DEK
	Algorithm string `json:"algorithm"`

	// KeySize in bits
	KeySize int `json:"key_size"`

	// PreviousVersions lists historical DEK versions still in use
	PreviousVersions []DEKVersionInfo `json:"previous_versions,omitempty"`

	// ReencryptionInProgress indicates if re-encryption is running
	ReencryptionInProgress bool `json:"reencryption_in_progress"`

	// ReencryptionProgress is percentage complete (0-100)
	ReencryptionProgress float64 `json:"reencryption_progress,omitempty"`
}

// RotateClusterDEKRequest contains parameters for DEK rotation.
type RotateClusterDEKRequest struct {
	// Reason for rotation (audit trail)
	Reason string `json:"reason"`

	// ScheduledTime for rotation (nil = immediate)
	ScheduledTime *time.Time `json:"scheduled_time,omitempty"`

	// ForceRotation skips safety checks (use with caution)
	ForceRotation bool `json:"force_rotation,omitempty"`

	// AutoStartReencryption starts re-encryption automatically
	AutoStartReencryption bool `json:"auto_start_reencryption"`

	// Metadata for audit and tracking
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RotateClusterDEKResponse contains the result of DEK rotation.
type RotateClusterDEKResponse struct {
	// NewVersion is the new DEK version
	NewVersion uint64 `json:"new_version"`

	// OldVersion is the previous DEK version
	OldVersion uint64 `json:"old_version"`

	// RotatedAt is when rotation occurred
	RotatedAt time.Time `json:"rotated_at"`

	// RotationID for tracking this rotation operation
	RotationID string `json:"rotation_id"`

	// NodesSynced is count of nodes with new DEK
	NodesSynced int `json:"nodes_synced"`

	// ReencryptionStarted indicates if re-encryption was initiated
	ReencryptionStarted bool `json:"reencryption_started"`

	// EstimatedReencryptionTime is estimated completion duration
	EstimatedReencryptionTime time.Duration `json:"estimated_reencryption_time,omitempty"`
}

// SyncNodeDEKRequest contains parameters for node DEK synchronization.
type SyncNodeDEKRequest struct {
	// NodeID is the node requesting sync
	NodeID uint64 `json:"node_id"`

	// CurrentVersion is the DEK version node currently has (0 if none)
	CurrentVersion uint64 `json:"current_version"`

	// ForceResync forces sync even if version matches
	ForceResync bool `json:"force_resync,omitempty"`
}

// SyncNodeDEKResponse contains the result of node DEK sync.
type SyncNodeDEKResponse struct {
	// NodeID that was synced
	NodeID uint64 `json:"node_id"`

	// VersionsSynced lists all DEK versions provided to node
	VersionsSynced []uint64 `json:"versions_synced"`

	// CurrentVersion is the active DEK version
	CurrentVersion uint64 `json:"current_version"`

	// SyncedAt is when sync completed
	SyncedAt time.Time `json:"synced_at"`
}

// GetNodeDEKStatusRequest contains parameters for getting node DEK status.
type GetNodeDEKStatusRequest struct {
	// NodeID to get status for
	NodeID uint64 `json:"node_id"`
}

// GetNodeDEKStatusResponse contains node-specific DEK status.
type GetNodeDEKStatusResponse struct {
	// NodeID of the node
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability
	NodeName string `json:"node_name,omitempty"`

	// CurrentVersion is DEK version on this node
	CurrentVersion uint64 `json:"current_version"`

	// LastSyncedAt is when node last synced DEK
	LastSyncedAt time.Time `json:"last_synced_at"`

	// Status indicates sync state
	Status NodeDEKSyncStatus `json:"status"`

	// Error message if sync failed
	Error string `json:"error,omitempty"`

	// VersionsAvailable lists all DEK versions node has
	VersionsAvailable []uint64 `json:"versions_available"`
}

// StartReencryptionRequest contains parameters for starting re-encryption.
type StartReencryptionRequest struct {
	// Path is the DEK path to re-encrypt (optional, empty means all DEKs)
	Path string `json:"path,omitempty"`

	// FromVersion is the old DEK version to re-encrypt from
	FromVersion uint64 `json:"from_version"`

	// ToVersion is the new DEK version to re-encrypt to
	ToVersion uint64 `json:"to_version"`

	// BatchSize is entities to process per batch
	BatchSize int `json:"batch_size,omitempty"`

	// Concurrency is parallel re-encryption workers
	Concurrency int `json:"concurrency,omitempty"`

	// ThrottleRate limits re-encryption rate (entities/sec, 0 = unlimited)
	ThrottleRate int `json:"throttle_rate,omitempty"`

	// Force re-encryption even if already in progress
	Force bool `json:"force,omitempty"`
}

// StartReencryptionResponse contains the result of starting re-encryption.
type StartReencryptionResponse struct {
	// OperationID for tracking this re-encryption
	OperationID string `json:"operation_id"`

	// Path of the DEK being re-encrypted
	Path string `json:"path,omitempty"`

	// Started indicates if the re-encryption was started
	Started bool `json:"started"`

	// EstimatedItems is the estimated number of items to process
	EstimatedItems int `json:"estimated_items"`

	// Timestamp is when the operation started
	Timestamp time.Time `json:"timestamp"`

	// StartedAt is when re-encryption began
	StartedAt time.Time `json:"started_at"`

	// TotalEntities is estimated total entities to re-encrypt
	TotalEntities int64 `json:"total_entities"`

	// EstimatedCompletion is estimated completion time
	EstimatedCompletion time.Time `json:"estimated_completion"`
}

// ReencryptionStatusResponse contains re-encryption progress status.
type ReencryptionStatusResponse struct {
	// OperationID of the re-encryption operation
	OperationID string `json:"operation_id"`

	// State of the operation
	State ReencryptionState `json:"state"`

	// StartedAt is when operation started
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when operation finished (nil if running)
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// EstimatedCompletion is estimated finish time (nil if complete)
	EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`

	// Progress is completion percentage (0-100)
	Progress float64 `json:"progress"`

	// TotalEntities is total entities to re-encrypt
	TotalEntities int64 `json:"total_entities"`

	// ProcessedEntities is entities already re-encrypted
	ProcessedEntities int64 `json:"processed_entities"`

	// FailedEntities is count of failures
	FailedEntities int64 `json:"failed_entities"`

	// ThroughputRate is current entities/second
	ThroughputRate float64 `json:"throughput_rate"`

	// NodeProgress is per-node progress breakdown
	NodeProgress []NodeReencryptionProgress `json:"node_progress,omitempty"`

	// Errors lists recent errors (limited to last 100)
	Errors []ReencryptionError `json:"errors,omitempty"`
}

// RecoverDEKRequest contains parameters for DEK recovery.
type RecoverDEKRequest struct {
	// RecoveryKeys are Shamir-split recovery keys
	RecoveryKeys [][]byte `json:"recovery_keys,omitempty"`

	// BackupLocation is path/URL to encrypted DEK backup
	BackupLocation string `json:"backup_location,omitempty"`

	// ManualKey is directly injected DEK (break-glass only)
	ManualKey []byte `json:"manual_key,omitempty"`

	// RecoveryMethod specifies which recovery method to use
	RecoveryMethod RecoveryMethod `json:"recovery_method"`

	// Reason for recovery (audit trail)
	Reason string `json:"reason"`
}

// RecoverDEKResponse contains the result of DEK recovery.
type RecoverDEKResponse struct {
	// Success indicates if recovery succeeded
	Success bool `json:"success"`

	// RecoveredVersion is the DEK version recovered
	RecoveredVersion uint64 `json:"recovered_version"`

	// RecoveredAt is when recovery completed
	RecoveredAt time.Time `json:"recovered_at"`

	// NodesSynced is count of nodes that received recovered DEK
	NodesSynced int `json:"nodes_synced"`

	// AuditID for tracking this recovery operation
	AuditID string `json:"audit_id"`

	// Warnings lists any issues encountered during recovery
	Warnings []string `json:"warnings,omitempty"`
}

// =============================================================================
// Supporting Types
// =============================================================================

// DEKRotationPolicy defines automatic DEK rotation settings.
type DEKRotationPolicy struct {
	// Enabled turns on automatic rotation
	Enabled bool `json:"enabled"`

	// RotationInterval is time between rotations
	RotationInterval time.Duration `json:"rotation_interval"`

	// AutoReencrypt automatically starts re-encryption after rotation
	AutoReencrypt bool `json:"auto_reencrypt"`
}

// NodeDEKInfo contains information about a node's DEK state.
type NodeDEKInfo struct {
	// NodeID of the node
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability
	NodeName string `json:"node_name,omitempty"`

	// CurrentVersion is the DEK version on this node
	CurrentVersion uint64 `json:"current_version"`

	// LastSyncedAt is when this node last synced
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// DEKVersionInfo contains information about a DEK version.
type DEKVersionInfo struct {
	// Version number
	Version uint64 `json:"version"`

	// CreatedAt is when this version was created
	CreatedAt time.Time `json:"created_at"`

	// DeprecatedAt is when this version was superseded
	DeprecatedAt *time.Time `json:"deprecated_at,omitempty"`

	// EntitiesEncrypted is count of entities still using this version
	EntitiesEncrypted int64 `json:"entities_encrypted"`

	// CanDelete indicates if this version can be safely removed
	CanDelete bool `json:"can_delete"`
}

// NodeDEKSyncStatus represents node DEK synchronization state.
type NodeDEKSyncStatus string

const (
	// NodeDEKSynced indicates node has current DEK.
	NodeDEKSynced NodeDEKSyncStatus = "synced"

	// NodeDEKOutOfDate indicates node has old DEK version.
	NodeDEKOutOfDate NodeDEKSyncStatus = "out_of_date"

	// NodeDEKMissing indicates node has no DEK.
	NodeDEKMissing NodeDEKSyncStatus = "missing"

	// NodeDEKSyncFailed indicates sync attempt failed.
	NodeDEKSyncFailed NodeDEKSyncStatus = "sync_failed"
)

// ReencryptionState represents re-encryption operation state.
type ReencryptionState string

const (
	// ReencryptionRunning indicates operation is in progress.
	ReencryptionRunning ReencryptionState = "running"

	// ReencryptionPaused indicates operation is paused.
	ReencryptionPaused ReencryptionState = "paused"

	// ReencryptionCompleted indicates operation finished successfully.
	ReencryptionCompleted ReencryptionState = "completed"

	// ReencryptionFailed indicates operation failed.
	ReencryptionFailed ReencryptionState = "failed"

	// ReencryptionCancelled indicates operation was cancelled.
	ReencryptionCancelled ReencryptionState = "cancelled"
)

// NodeReencryptionProgress contains per-node re-encryption progress.
type NodeReencryptionProgress struct {
	// NodeID of the node
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability
	NodeName string `json:"node_name,omitempty"`

	// Progress percentage (0-100)
	Progress float64 `json:"progress"`

	// ProcessedEntities on this node
	ProcessedEntities int64 `json:"processed_entities"`

	// TotalEntities on this node
	TotalEntities int64 `json:"total_entities"`

	// ThroughputRate for this node (entities/sec)
	ThroughputRate float64 `json:"throughput_rate"`

	// LastUpdatedAt is when progress was last updated
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// ReencryptionError contains information about a re-encryption error.
type ReencryptionError struct {
	// Timestamp when error occurred
	Timestamp time.Time `json:"timestamp"`

	// NodeID where error occurred
	NodeID uint64 `json:"node_id"`

	// EntityID that failed
	EntityID string `json:"entity_id,omitempty"`

	// Error message
	Error string `json:"error"`

	// Retried indicates if operation was retried
	Retried bool `json:"retried"`
}

// RecoveryMethod specifies the DEK recovery method.
type RecoveryMethod string

const (
	// RecoveryMethodKeys uses Shamir recovery keys.
	RecoveryMethodKeys RecoveryMethod = "recovery_keys"

	// RecoveryMethodBackup uses encrypted backup.
	RecoveryMethodBackup RecoveryMethod = "backup"

	// RecoveryMethodManual uses manually injected key.
	RecoveryMethodManual RecoveryMethod = "manual"
)

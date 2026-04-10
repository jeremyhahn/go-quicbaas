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

package clusterdek

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.ClusterDEK"
)

// Client provides cluster DEK management operations.
type Client interface {
	// InitializeClusterDEK creates and distributes the initial cluster DEK.
	InitializeClusterDEK(ctx context.Context, req *InitClusterDEKRequest) (*InitClusterDEKResponse, error)

	// GetClusterDEKStatus retrieves current cluster DEK status and metadata.
	GetClusterDEKStatus(ctx context.Context) (*ClusterDEKStatusResponse, error)

	// RotateClusterDEK generates a new cluster DEK and initiates re-encryption.
	RotateClusterDEK(ctx context.Context, req *RotateClusterDEKRequest) (*RotateClusterDEKResponse, error)

	// SyncNodeDEK synchronizes cluster DEK to a specific node.
	SyncNodeDEK(ctx context.Context, req *SyncNodeDEKRequest) (*SyncNodeDEKResponse, error)

	// GetNodeDEKStatus retrieves DEK sync status for a specific node.
	GetNodeDEKStatus(ctx context.Context, req *GetNodeDEKStatusRequest) (*GetNodeDEKStatusResponse, error)

	// StartReencryption manually initiates re-encryption of data with new DEK.
	StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error)

	// GetReencryptionStatus retrieves current re-encryption progress.
	GetReencryptionStatus(ctx context.Context) (*ReencryptionStatusResponse, error)

	// CancelReencryption cancels ongoing re-encryption process.
	CancelReencryption(ctx context.Context) error

	// RecoverDEK recovers cluster DEK using recovery keys or backup mechanism.
	RecoverDEK(ctx context.Context, req *RecoverDEKRequest) (*RecoverDEKResponse, error)

	// EnsureTenantDEK creates a per-tenant DEK derived from the cluster DEK
	// if one does not already exist. Returns the tenant DEK identifier.
	EnsureTenantDEK(ctx context.Context, req *EnsureTenantDEKRequest) (*EnsureTenantDEKResponse, error)

	// DeleteTenantDEK securely deletes a tenant's DEK and marks all data
	// encrypted with it for re-encryption or deletion.
	DeleteTenantDEK(ctx context.Context, req *DeleteTenantDEKRequest) (*DeleteTenantDEKResponse, error)
}

// =============================================================================
// Request/Response Types
// =============================================================================

// InitClusterDEKRequest contains parameters for initializing cluster DEK.
type InitClusterDEKRequest struct {
	// Algorithm specifies the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `json:"algorithm"`

	// KeySize is the key size in bits (e.g., 256).
	KeySize int `json:"key_size"`

	// Metadata for audit and tracking.
	Metadata map[string]string `json:"metadata,omitempty"`

	// RotationPolicy defines automatic rotation settings.
	RotationPolicy *DEKRotationPolicy `json:"rotation_policy,omitempty"`
}

// InitClusterDEKResponse contains the result of DEK initialization.
type InitClusterDEKResponse struct {
	// Version is the initial DEK version (typically 1).
	Version uint64 `json:"version"`

	// CreatedAt is when the DEK was created.
	CreatedAt time.Time `json:"created_at"`

	// NodesSynced is the count of nodes that have the DEK.
	NodesSynced int `json:"nodes_synced"`

	// Algorithm used for the DEK.
	Algorithm string `json:"algorithm"`

	// KeySize in bits.
	KeySize int `json:"key_size"`
}

// ClusterDEKStatusResponse contains comprehensive cluster DEK status.
type ClusterDEKStatusResponse struct {
	// Initialized indicates if cluster DEK has been set up.
	Initialized bool `json:"initialized"`

	// CurrentVersion is the active DEK version.
	CurrentVersion uint64 `json:"current_version"`

	// CreatedAt is when current DEK was created.
	CreatedAt time.Time `json:"created_at"`

	// RotatedAt is when DEK was last rotated (nil if never rotated).
	RotatedAt *time.Time `json:"rotated_at,omitempty"`

	// NextRotation is scheduled rotation time (if configured).
	NextRotation *time.Time `json:"next_rotation,omitempty"`

	// NodesSynced is count of nodes with current DEK.
	NodesSynced int `json:"nodes_synced"`

	// TotalNodes is total nodes in cluster.
	TotalNodes int `json:"total_nodes"`

	// NodesOutOfSync lists nodes missing current DEK.
	NodesOutOfSync []NodeDEKInfo `json:"nodes_out_of_sync,omitempty"`

	// Algorithm for current DEK.
	Algorithm string `json:"algorithm"`

	// KeySize in bits.
	KeySize int `json:"key_size"`

	// PreviousVersions lists historical DEK versions still in use.
	PreviousVersions []DEKVersionInfo `json:"previous_versions,omitempty"`

	// ReencryptionInProgress indicates if re-encryption is running.
	ReencryptionInProgress bool `json:"reencryption_in_progress"`

	// ReencryptionProgress is percentage complete (0-100).
	ReencryptionProgress float64 `json:"reencryption_progress,omitempty"`
}

// RotateClusterDEKRequest contains parameters for DEK rotation.
type RotateClusterDEKRequest struct {
	// Reason for rotation (audit trail).
	Reason string `json:"reason"`

	// ScheduledTime for rotation (nil = immediate).
	ScheduledTime *time.Time `json:"scheduled_time,omitempty"`

	// ForceRotation skips safety checks (use with caution).
	ForceRotation bool `json:"force_rotation,omitempty"`

	// AutoStartReencryption starts re-encryption automatically.
	AutoStartReencryption bool `json:"auto_start_reencryption"`

	// Metadata for audit and tracking.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RotateClusterDEKResponse contains the result of DEK rotation.
type RotateClusterDEKResponse struct {
	// NewVersion is the new DEK version.
	NewVersion uint64 `json:"new_version"`

	// OldVersion is the previous DEK version.
	OldVersion uint64 `json:"old_version"`

	// RotatedAt is when rotation occurred.
	RotatedAt time.Time `json:"rotated_at"`

	// RotationID for tracking this rotation operation.
	RotationID string `json:"rotation_id"`

	// NodesSynced is count of nodes with new DEK.
	NodesSynced int `json:"nodes_synced"`

	// ReencryptionStarted indicates if re-encryption was initiated.
	ReencryptionStarted bool `json:"reencryption_started"`

	// EstimatedReencryptionTime is estimated completion duration.
	EstimatedReencryptionTime time.Duration `json:"estimated_reencryption_time,omitempty"`
}

// SyncNodeDEKRequest contains parameters for node DEK synchronization.
type SyncNodeDEKRequest struct {
	// NodeID is the node requesting sync.
	NodeID uint64 `json:"node_id"`

	// CurrentVersion is the DEK version node currently has (0 if none).
	CurrentVersion uint64 `json:"current_version"`

	// ForceResync forces sync even if version matches.
	ForceResync bool `json:"force_resync,omitempty"`
}

// SyncNodeDEKResponse contains the result of node DEK sync.
type SyncNodeDEKResponse struct {
	// NodeID that was synced.
	NodeID uint64 `json:"node_id"`

	// VersionsSynced lists all DEK versions provided to node.
	VersionsSynced []uint64 `json:"versions_synced"`

	// CurrentVersion is the active DEK version.
	CurrentVersion uint64 `json:"current_version"`

	// SyncedAt is when sync completed.
	SyncedAt time.Time `json:"synced_at"`
}

// GetNodeDEKStatusRequest contains parameters for getting node DEK status.
type GetNodeDEKStatusRequest struct {
	// NodeID to get status for.
	NodeID uint64 `json:"node_id"`
}

// GetNodeDEKStatusResponse contains node-specific DEK status.
type GetNodeDEKStatusResponse struct {
	// NodeID of the node.
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability.
	NodeName string `json:"node_name,omitempty"`

	// CurrentVersion is DEK version on this node.
	CurrentVersion uint64 `json:"current_version"`

	// LastSyncedAt is when node last synced DEK.
	LastSyncedAt time.Time `json:"last_synced_at"`

	// Status indicates sync state.
	Status NodeDEKSyncStatus `json:"status"`

	// Error message if sync failed.
	Error string `json:"error,omitempty"`

	// VersionsAvailable lists all DEK versions node has.
	VersionsAvailable []uint64 `json:"versions_available"`
}

// StartReencryptionRequest contains parameters for starting re-encryption.
type StartReencryptionRequest struct {
	// Path is the DEK path to re-encrypt (optional, empty means all DEKs).
	Path string `json:"path,omitempty"`

	// FromVersion is the old DEK version to re-encrypt from.
	FromVersion uint64 `json:"from_version"`

	// ToVersion is the new DEK version to re-encrypt to.
	ToVersion uint64 `json:"to_version"`

	// BatchSize is entities to process per batch.
	BatchSize int `json:"batch_size,omitempty"`

	// Concurrency is parallel re-encryption workers.
	Concurrency int `json:"concurrency,omitempty"`

	// ThrottleRate limits re-encryption rate (entities/sec, 0 = unlimited).
	ThrottleRate int `json:"throttle_rate,omitempty"`

	// Force re-encryption even if already in progress.
	Force bool `json:"force,omitempty"`
}

// StartReencryptionResponse contains the result of starting re-encryption.
type StartReencryptionResponse struct {
	// OperationID for tracking this re-encryption.
	OperationID string `json:"operation_id"`

	// Path of the DEK being re-encrypted.
	Path string `json:"path,omitempty"`

	// Started indicates if the re-encryption was started.
	Started bool `json:"started"`

	// EstimatedItems is the estimated number of items to process.
	EstimatedItems int `json:"estimated_items"`

	// Timestamp is when the operation started.
	Timestamp time.Time `json:"timestamp"`

	// StartedAt is when re-encryption began.
	StartedAt time.Time `json:"started_at"`

	// TotalEntities is estimated total entities to re-encrypt.
	TotalEntities int64 `json:"total_entities"`

	// EstimatedCompletion is estimated completion time.
	EstimatedCompletion time.Time `json:"estimated_completion"`
}

// ReencryptionStatusResponse contains re-encryption progress status.
type ReencryptionStatusResponse struct {
	// OperationID of the re-encryption operation.
	OperationID string `json:"operation_id"`

	// State of the operation.
	State ReencryptionState `json:"state"`

	// StartedAt is when operation started.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when operation finished (nil if running).
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// EstimatedCompletion is estimated finish time (nil if complete).
	EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`

	// Progress is completion percentage (0-100).
	Progress float64 `json:"progress"`

	// TotalEntities is total entities to re-encrypt.
	TotalEntities int64 `json:"total_entities"`

	// ProcessedEntities is entities already re-encrypted.
	ProcessedEntities int64 `json:"processed_entities"`

	// FailedEntities is count of failures.
	FailedEntities int64 `json:"failed_entities"`

	// ThroughputRate is current entities/second.
	ThroughputRate float64 `json:"throughput_rate"`

	// NodeProgress is per-node progress breakdown.
	NodeProgress []NodeReencryptionProgress `json:"node_progress,omitempty"`

	// Errors lists recent errors (limited to last 100).
	Errors []ReencryptionError `json:"errors,omitempty"`
}

// RecoverDEKRequest contains parameters for DEK recovery.
type RecoverDEKRequest struct {
	// RecoveryKeys are Shamir-split recovery keys.
	RecoveryKeys [][]byte `json:"recovery_keys,omitempty"`

	// BackupLocation is path/URL to encrypted DEK backup.
	BackupLocation string `json:"backup_location,omitempty"`

	// ManualKey is directly injected DEK (break-glass only).
	ManualKey []byte `json:"manual_key,omitempty"`

	// RecoveryMethod specifies which recovery method to use.
	RecoveryMethod RecoveryMethod `json:"recovery_method"`

	// Reason for recovery (audit trail).
	Reason string `json:"reason"`
}

// RecoverDEKResponse contains the result of DEK recovery.
type RecoverDEKResponse struct {
	// Success indicates if recovery succeeded.
	Success bool `json:"success"`

	// RecoveredVersion is the DEK version recovered.
	RecoveredVersion uint64 `json:"recovered_version"`

	// RecoveredAt is when recovery completed.
	RecoveredAt time.Time `json:"recovered_at"`

	// NodesSynced is count of nodes that received recovered DEK.
	NodesSynced int `json:"nodes_synced"`

	// AuditID for tracking this recovery operation.
	AuditID string `json:"audit_id"`

	// Warnings lists any issues encountered during recovery.
	Warnings []string `json:"warnings,omitempty"`
}

// EnsureTenantDEKRequest contains parameters for ensuring a per-tenant DEK exists.
type EnsureTenantDEKRequest struct {
	// TenantID is the unique identifier of the tenant.
	TenantID string `json:"tenant_id"`

	// Algorithm specifies the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `json:"algorithm"`
}

// EnsureTenantDEKResponse contains the result of ensuring a tenant DEK.
type EnsureTenantDEKResponse struct {
	// DEKID is the identifier of the tenant DEK (e.g., "dek:tenant:{tenantID}").
	DEKID string `json:"dek_id"`

	// Created indicates whether a new DEK was created (false if it already existed).
	Created bool `json:"created"`

	// Algorithm used for the tenant DEK.
	Algorithm string `json:"algorithm"`

	// CreatedAt is when the DEK was created.
	CreatedAt time.Time `json:"created_at"`
}

// DeleteTenantDEKRequest contains parameters for deleting a per-tenant DEK.
type DeleteTenantDEKRequest struct {
	// TenantID is the unique identifier of the tenant whose DEK should be deleted.
	TenantID string `json:"tenant_id"`
}

// DeleteTenantDEKResponse contains the result of deleting a tenant DEK.
type DeleteTenantDEKResponse struct {
	// DEKID is the identifier of the deleted tenant DEK.
	DEKID string `json:"dek_id"`

	// DeletedAt is when the DEK was deleted.
	DeletedAt time.Time `json:"deleted_at"`
}

// =============================================================================
// Supporting Types
// =============================================================================

// DEKRotationPolicy defines automatic DEK rotation settings.
type DEKRotationPolicy struct {
	// Enabled turns on automatic rotation.
	Enabled bool `json:"enabled"`

	// RotationInterval is time between rotations.
	RotationInterval time.Duration `json:"rotation_interval"`

	// AutoReencrypt automatically starts re-encryption after rotation.
	AutoReencrypt bool `json:"auto_reencrypt"`
}

// NodeDEKInfo contains information about a node's DEK state.
type NodeDEKInfo struct {
	// NodeID of the node.
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability.
	NodeName string `json:"node_name,omitempty"`

	// CurrentVersion is the DEK version on this node.
	CurrentVersion uint64 `json:"current_version"`

	// LastSyncedAt is when this node last synced.
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// DEKVersionInfo contains information about a DEK version.
type DEKVersionInfo struct {
	// Version number.
	Version uint64 `json:"version"`

	// CreatedAt is when this version was created.
	CreatedAt time.Time `json:"created_at"`

	// DeprecatedAt is when this version was superseded.
	DeprecatedAt *time.Time `json:"deprecated_at,omitempty"`

	// EntitiesEncrypted is count of entities still using this version.
	EntitiesEncrypted int64 `json:"entities_encrypted"`

	// CanDelete indicates if this version can be safely removed.
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
	// NodeID of the node.
	NodeID uint64 `json:"node_id"`

	// NodeName for human readability.
	NodeName string `json:"node_name,omitempty"`

	// Progress percentage (0-100).
	Progress float64 `json:"progress"`

	// ProcessedEntities on this node.
	ProcessedEntities int64 `json:"processed_entities"`

	// TotalEntities on this node.
	TotalEntities int64 `json:"total_entities"`

	// ThroughputRate for this node (entities/sec).
	ThroughputRate float64 `json:"throughput_rate"`

	// LastUpdatedAt is when progress was last updated.
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// ReencryptionError contains information about a re-encryption error.
type ReencryptionError struct {
	// Timestamp when error occurred.
	Timestamp time.Time `json:"timestamp"`

	// NodeID where error occurred.
	NodeID uint64 `json:"node_id"`

	// EntityID that failed.
	EntityID string `json:"entity_id,omitempty"`

	// Error message.
	Error string `json:"error"`

	// Retried indicates if operation was retried.
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

// =============================================================================
// Internal Request Types
// =============================================================================

// getStatusRequest is the internal status request.
type getStatusRequest struct{}

// getReencryptionStatusRequest is the internal re-encryption status request.
type getReencryptionStatusRequest struct{}

// cancelReencryptionRequest is the internal cancel re-encryption request.
type cancelReencryptionRequest struct{}

// cancelReencryptionResponse is the internal cancel re-encryption response.
type cancelReencryptionResponse struct{}

// =============================================================================
// Client Implementation
// =============================================================================

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new cluster DEK client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// InitializeClusterDEK creates and distributes the initial cluster DEK.
func (c *client) InitializeClusterDEK(ctx context.Context, req *InitClusterDEKRequest) (*InitClusterDEKResponse, error) {
	if err := validateInitClusterDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "InitializeClusterDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.InitializeClusterDEK", "nil response from server")
	}

	initResp, ok := resp.(*InitClusterDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.InitializeClusterDEK", "invalid response type")
	}

	return initResp, nil
}

// GetClusterDEKStatus retrieves current cluster DEK status and metadata.
func (c *client) GetClusterDEKStatus(ctx context.Context) (*ClusterDEKStatusResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.GetClusterDEKStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetClusterDEKStatus", &getStatusRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.GetClusterDEKStatus", "nil response from server")
	}

	statusResp, ok := resp.(*ClusterDEKStatusResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.GetClusterDEKStatus", "invalid response type")
	}

	return statusResp, nil
}

// RotateClusterDEK generates a new cluster DEK and initiates re-encryption.
func (c *client) RotateClusterDEK(ctx context.Context, req *RotateClusterDEKRequest) (*RotateClusterDEKResponse, error) {
	if err := validateRotateClusterDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.RotateClusterDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RotateClusterDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.RotateClusterDEK", "nil response from server")
	}

	rotateResp, ok := resp.(*RotateClusterDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.RotateClusterDEK", "invalid response type")
	}

	return rotateResp, nil
}

// SyncNodeDEK synchronizes cluster DEK to a specific node.
func (c *client) SyncNodeDEK(ctx context.Context, req *SyncNodeDEKRequest) (*SyncNodeDEKResponse, error) {
	if err := validateSyncNodeDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.SyncNodeDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SyncNodeDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.SyncNodeDEK", "nil response from server")
	}

	syncResp, ok := resp.(*SyncNodeDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.SyncNodeDEK", "invalid response type")
	}

	return syncResp, nil
}

// GetNodeDEKStatus retrieves DEK sync status for a specific node.
func (c *client) GetNodeDEKStatus(ctx context.Context, req *GetNodeDEKStatusRequest) (*GetNodeDEKStatusResponse, error) {
	if err := validateGetNodeDEKStatusRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.GetNodeDEKStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetNodeDEKStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("clusterdek.GetNodeDEKStatus", "node not found")
	}

	statusResp, ok := resp.(*GetNodeDEKStatusResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.GetNodeDEKStatus", "invalid response type")
	}

	return statusResp, nil
}

// StartReencryption manually initiates re-encryption of data with new DEK.
func (c *client) StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error) {
	if err := validateStartReencryptionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.StartReencryption", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "StartReencryption", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.StartReencryption", "nil response from server")
	}

	startResp, ok := resp.(*StartReencryptionResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.StartReencryption", "invalid response type")
	}

	return startResp, nil
}

// GetReencryptionStatus retrieves current re-encryption progress.
func (c *client) GetReencryptionStatus(ctx context.Context) (*ReencryptionStatusResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.GetReencryptionStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetReencryptionStatus", &getReencryptionStatusRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("clusterdek.GetReencryptionStatus", "no reencryption in progress")
	}

	statusResp, ok := resp.(*ReencryptionStatusResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.GetReencryptionStatus", "invalid response type")
	}

	return statusResp, nil
}

// CancelReencryption cancels ongoing re-encryption process.
func (c *client) CancelReencryption(ctx context.Context) error {
	if ctx == nil {
		return errors.NewInvalidInput("clusterdek.CancelReencryption", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CancelReencryption", &cancelReencryptionRequest{})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*cancelReencryptionResponse); !ok {
		return errors.NewInternal("clusterdek.CancelReencryption", "invalid response type")
	}

	return nil
}

// RecoverDEK recovers cluster DEK using recovery keys or backup mechanism.
func (c *client) RecoverDEK(ctx context.Context, req *RecoverDEKRequest) (*RecoverDEKResponse, error) {
	if err := validateRecoverDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.RecoverDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RecoverDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.RecoverDEK", "nil response from server")
	}

	recoverResp, ok := resp.(*RecoverDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.RecoverDEK", "invalid response type")
	}

	return recoverResp, nil
}

// EnsureTenantDEK creates a per-tenant DEK if one does not already exist.
func (c *client) EnsureTenantDEK(ctx context.Context, req *EnsureTenantDEKRequest) (*EnsureTenantDEKResponse, error) {
	if err := validateEnsureTenantDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.EnsureTenantDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "EnsureTenantDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.EnsureTenantDEK", "nil response from server")
	}

	tenantResp, ok := resp.(*EnsureTenantDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.EnsureTenantDEK", "invalid response type")
	}

	return tenantResp, nil
}

// DeleteTenantDEK securely deletes a tenant's DEK.
func (c *client) DeleteTenantDEK(ctx context.Context, req *DeleteTenantDEKRequest) (*DeleteTenantDEKResponse, error) {
	if err := validateDeleteTenantDEKRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("clusterdek.DeleteTenantDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteTenantDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("clusterdek.DeleteTenantDEK", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteTenantDEKResponse)
	if !ok {
		return nil, errors.NewInternal("clusterdek.DeleteTenantDEK", "invalid response type")
	}

	return deleteResp, nil
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateInitClusterDEKRequest(req *InitClusterDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "request is required")
	}
	if req.Algorithm == "" {
		return errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "algorithm is required")
	}
	if req.KeySize <= 0 {
		return errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "key size must be positive")
	}
	// Validate common key sizes
	validKeySizes := map[int]bool{128: true, 192: true, 256: true, 384: true, 512: true}
	if !validKeySizes[req.KeySize] {
		return errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "invalid key size").
			WithContext("key_size", req.KeySize).
			WithContext("valid_sizes", "128, 192, 256, 384, 512")
	}
	if req.RotationPolicy != nil {
		if req.RotationPolicy.Enabled && req.RotationPolicy.RotationInterval <= 0 {
			return errors.NewInvalidInput("clusterdek.InitializeClusterDEK", "rotation interval must be positive when rotation is enabled")
		}
	}
	return nil
}

func validateRotateClusterDEKRequest(req *RotateClusterDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.RotateClusterDEK", "request is required")
	}
	if req.Reason == "" {
		return errors.NewInvalidInput("clusterdek.RotateClusterDEK", "reason is required for audit trail")
	}
	return nil
}

func validateSyncNodeDEKRequest(req *SyncNodeDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.SyncNodeDEK", "request is required")
	}
	if req.NodeID == 0 {
		return errors.NewInvalidInput("clusterdek.SyncNodeDEK", "node ID is required")
	}
	return nil
}

func validateGetNodeDEKStatusRequest(req *GetNodeDEKStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.GetNodeDEKStatus", "request is required")
	}
	if req.NodeID == 0 {
		return errors.NewInvalidInput("clusterdek.GetNodeDEKStatus", "node ID is required")
	}
	return nil
}

func validateStartReencryptionRequest(req *StartReencryptionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "request is required")
	}
	if req.FromVersion == 0 {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "from version is required")
	}
	if req.ToVersion == 0 {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "to version is required")
	}
	if req.FromVersion >= req.ToVersion {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "to version must be greater than from version")
	}
	if req.BatchSize < 0 {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "batch size cannot be negative")
	}
	if req.Concurrency < 0 {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "concurrency cannot be negative")
	}
	if req.ThrottleRate < 0 {
		return errors.NewInvalidInput("clusterdek.StartReencryption", "throttle rate cannot be negative")
	}
	return nil
}

func validateRecoverDEKRequest(req *RecoverDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.RecoverDEK", "request is required")
	}
	if req.Reason == "" {
		return errors.NewInvalidInput("clusterdek.RecoverDEK", "reason is required for audit trail")
	}

	switch req.RecoveryMethod {
	case RecoveryMethodKeys:
		if len(req.RecoveryKeys) == 0 {
			return errors.NewInvalidInput("clusterdek.RecoverDEK", "recovery keys are required for recovery_keys method")
		}
		for i, key := range req.RecoveryKeys {
			if len(key) == 0 {
				return errors.NewInvalidInput("clusterdek.RecoverDEK", "recovery key cannot be empty").
					WithContext("key_index", i)
			}
		}
	case RecoveryMethodBackup:
		if req.BackupLocation == "" {
			return errors.NewInvalidInput("clusterdek.RecoverDEK", "backup location is required for backup method")
		}
	case RecoveryMethodManual:
		if len(req.ManualKey) == 0 {
			return errors.NewInvalidInput("clusterdek.RecoverDEK", "manual key is required for manual method")
		}
	default:
		return errors.NewInvalidInput("clusterdek.RecoverDEK", "invalid recovery method").
			WithContext("method", string(req.RecoveryMethod)).
			WithContext("valid_methods", "recovery_keys, backup, manual")
	}

	return nil
}

func validateEnsureTenantDEKRequest(req *EnsureTenantDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.EnsureTenantDEK", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("clusterdek.EnsureTenantDEK", "tenant ID is required")
	}
	if req.Algorithm == "" {
		return errors.NewInvalidInput("clusterdek.EnsureTenantDEK", "algorithm is required")
	}
	return nil
}

func validateDeleteTenantDEKRequest(req *DeleteTenantDEKRequest) error {
	if req == nil {
		return errors.NewInvalidInput("clusterdek.DeleteTenantDEK", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("clusterdek.DeleteTenantDEK", "tenant ID is required")
	}
	return nil
}

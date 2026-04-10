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

// Package secrets provides cluster synchronization for DEK management.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ClusterKVProposer abstracts the KV write path used for DEK synchronization.
// In the BaaS deployment model this is backed by the qrdb SDK client's
// KV().Put() method; for embedded deployments it may wrap a direct Raft
// proposal.
type ClusterKVProposer interface {
	// ProposeKV writes a key-value pair through Raft consensus on the
	// given shard. The caller serializes the command payload into value.
	ProposeKV(ctx context.Context, shardID uint64, key string, value []byte) error
}

// Cluster sync errors.
var (
	// ErrClusterSyncFailed indicates cluster synchronization failed.
	ErrClusterSyncFailed = errors.New("secrets: cluster sync failed")

	// ErrNotClusterMode indicates operation requires cluster mode.
	ErrNotClusterMode = errors.New("secrets: not in cluster mode")

	// ErrReencryptionInProgress indicates re-encryption is already running.
	ErrReencryptionInProgress = errors.New("secrets: re-encryption already in progress")

	// ErrReencryptionFailed indicates background re-encryption failed.
	ErrReencryptionFailed = errors.New("secrets: re-encryption failed")
)

const (
	// DefaultKMSShardID is the default shard for KMS operations.
	DefaultKMSShardID = uint64(1000)

	// DefaultSecretsShardID is the default shard for secrets operations.
	DefaultSecretsShardID = uint64(1001)

	// reencryptionBatchSize is the number of secrets to process per batch.
	reencryptionBatchSize = 100

	// reencryptionBatchDelay is the delay between batches to avoid overload.
	reencryptionBatchDelay = 100 * time.Millisecond

	// dekSyncKeyPrefix is the key prefix for DEK cluster synchronization
	// entries written through the SDK KV path.
	dekSyncKeyPrefix = "_dek/sync/"
)

// ClusterDEKConfig configures the cluster DEK manager.
type ClusterDEKConfig struct {
	// KMSShardID is the Raft shard for KMS/DEK operations.
	KMSShardID uint64

	// SecretsShardID is the Raft shard for secrets storage.
	SecretsShardID uint64

	// EnableClusterSync enables Raft-based cluster synchronization.
	EnableClusterSync bool
}

// DefaultClusterDEKConfig returns the default cluster DEK configuration.
func DefaultClusterDEKConfig() ClusterDEKConfig {
	return ClusterDEKConfig{
		KMSShardID:        DefaultKMSShardID,
		SecretsShardID:    DefaultSecretsShardID,
		EnableClusterSync: false,
	}
}

// ClusterDEKManager provides cluster-aware DEK management.
// It wraps SecretsDEK and adds Raft-based synchronization for DEK rotation.
type ClusterDEKManager struct {
	dek            *SecretsDEK
	kvProposer     ClusterKVProposer
	config         ClusterDEKConfig
	cmdBuilder     *KMSCommandBuilder
	logger         interfaces.Logger
	mu             sync.RWMutex
	reencrypting   bool
	reencryptStats *ReencryptionStats
}

// ReencryptionStats tracks background re-encryption progress.
type ReencryptionStats struct {
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	DEKID          DEKID     `json:"dekid"`
	SourceVersion  uint64    `json:"source_version"`
	TargetVersion  uint64    `json:"target_version"`
	TotalSecrets   int       `json:"total_secrets"`
	ProcessedCount int       `json:"processed_count"`
	SuccessCount   int       `json:"success_count"`
	ErrorCount     int       `json:"error_count"`
	InProgress     bool      `json:"in_progress"`
	LastError      string    `json:"last_error,omitempty"`
}

// NewClusterDEKManager creates a new cluster-aware DEK manager.
// The kvProposer handles cluster-wide DEK synchronization through the
// qrdb SDK's KV write path. Pass nil to disable cluster sync.
func NewClusterDEKManager(
	dek *SecretsDEK,
	kvProposer ClusterKVProposer,
	config ClusterDEKConfig,
	logger interfaces.Logger,
) *ClusterDEKManager {
	return &ClusterDEKManager{
		dek:        dek,
		kvProposer: kvProposer,
		config:     config,
		cmdBuilder: NewKMSCommandBuilder(),
		logger:     logger,
	}
}

// Initialize creates the global DEK if it doesn't exist.
// In cluster mode, this is synchronized via Raft.
func (c *ClusterDEKManager) Initialize(ctx context.Context, algorithm string) error {
	if !c.config.EnableClusterSync || c.kvProposer == nil {
		// Single-node mode: use local DEK
		return c.dek.Initialize(ctx, algorithm)
	}

	// Cluster mode: check if already initialized first
	_, _, err := c.dek.GetKey(ctx, DEKIDGlobal)
	if err == nil {
		// Already initialized
		return nil
	}

	// Need to initialize through local DEK first (leader will propagate)
	if err := c.dek.Initialize(ctx, algorithm); err != nil {
		return err
	}

	// Sync to cluster via Raft
	entry, err := c.dek.loadDEKEntry(ctx, DEKIDGlobal)
	if err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrClusterSyncFailed, Cause: &SecretsCauseError{Message: "failed to load DEK entry", Cause: err}}
	}

	return c.syncDEKToCluster(ctx, DEKIDGlobal, entry)
}

// Rotate creates a new version of the DEK with cluster synchronization.
// In cluster mode, rotation is coordinated via Raft consensus.
func (c *ClusterDEKManager) Rotate(ctx context.Context, dekid DEKID) (uint64, error) {
	if !c.config.EnableClusterSync || c.kvProposer == nil {
		// Single-node mode: use local rotation
		return c.dek.Rotate(ctx, dekid)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Get current version before rotation
	status, err := c.dek.GetStatus(ctx, dekid)
	if err != nil {
		return 0, err
	}
	oldVersion := status.Version

	// Perform local rotation first
	newVersion, err := c.dek.Rotate(ctx, dekid)
	if err != nil {
		return 0, err
	}

	// Load the new entry for cluster sync
	entry, err := c.dek.loadDEKEntry(ctx, dekid)
	if err != nil {
		c.logger.WithError(err).WithField("dekid", dekid).
			Error("Failed to load rotated DEK for cluster sync")
		return newVersion, &SecretsSentinelCauseError{Sentinel: ErrClusterSyncFailed, Cause: &SecretsCauseError{Message: "failed to sync rotation", Cause: err}}
	}

	// Sync to cluster via Raft
	if err := c.syncDEKRotationToCluster(ctx, dekid, entry); err != nil {
		c.logger.WithError(err).WithField("dekid", dekid).
			Error("Failed to sync DEK rotation to cluster")
		// Note: local rotation succeeded, but cluster sync failed
		// The state may be inconsistent until next rotation
		return newVersion, &SecretsSentinelCauseError{Sentinel: ErrClusterSyncFailed, Cause: err}
	}

	c.logger.WithField("dekid", dekid).
		WithField("old_version", oldVersion).
		WithField("new_version", newVersion).
		Info("DEK rotation synchronized to cluster")

	return newVersion, nil
}

// syncDEKToCluster synchronizes a DEK to the cluster via the SDK KV path.
func (c *ClusterDEKManager) syncDEKToCluster(ctx context.Context, dekid DEKID, entry *DEKEntry) error {
	// Build SetDEK command
	dekEntry := &KMSDEKEntry{
		ID:         string(dekid),
		Version:    entry.Version,
		Algorithm:  entry.Algorithm,
		SealedData: entry.WrappedKey,
		State:      KeyStateActive,
		KeyBytes:   defaultDEKKeyBytes,
		CreatedAt:  entry.CreatedAt,
	}

	cmd, err := c.cmdBuilder.SetDEK(dekEntry)
	if err != nil {
		return &SecretsCauseError{Message: "failed to build SetDEK command", Cause: err}
	}

	// Propose through SDK KV path
	key := dekSyncKeyPrefix + string(dekid)
	if err := c.kvProposer.ProposeKV(ctx, c.config.KMSShardID, key, cmd); err != nil {
		return &SecretsCauseError{Message: "failed to propose SetDEK", Cause: err}
	}

	return nil
}

// syncDEKRotationToCluster synchronizes a DEK rotation to the cluster via the SDK KV path.
func (c *ClusterDEKManager) syncDEKRotationToCluster(ctx context.Context, dekid DEKID, entry *DEKEntry) error {
	// Build RotateDEK command
	cmd, err := c.cmdBuilder.RotateDEK(
		string(dekid),
		entry.WrappedKey,
		entry.Algorithm,
		defaultDEKKeyBytes,
	)
	if err != nil {
		return &SecretsCauseError{Message: "failed to build RotateDEK command", Cause: err}
	}

	// Propose through SDK KV path
	key := dekSyncKeyPrefix + string(dekid) + "/rotate"
	if err := c.kvProposer.ProposeKV(ctx, c.config.KMSShardID, key, cmd); err != nil {
		return &SecretsCauseError{Message: "failed to propose RotateDEK", Cause: err}
	}

	return nil
}

// GetKey retrieves key material for a DEKID.
func (c *ClusterDEKManager) GetKey(ctx context.Context, dekid DEKID) ([]byte, uint64, error) {
	return c.dek.GetKey(ctx, dekid)
}

// GetKeyVersion retrieves a specific version of a DEK.
func (c *ClusterDEKManager) GetKeyVersion(ctx context.Context, dekid DEKID, version uint64) ([]byte, error) {
	return c.dek.GetKeyVersion(ctx, dekid, version)
}

// GetStatus returns status information about a DEK.
func (c *ClusterDEKManager) GetStatus(ctx context.Context, dekid DEKID) (*DEKStatus, error) {
	return c.dek.GetStatus(ctx, dekid)
}

// ListVersions returns all available versions of a DEK.
func (c *ClusterDEKManager) ListVersions(ctx context.Context, dekid DEKID) ([]DEKVersionInfo, error) {
	return c.dek.ListVersions(ctx, dekid)
}

// EnsureTenantDEK creates a tenant DEK if it doesn't exist.
func (c *ClusterDEKManager) EnsureTenantDEK(ctx context.Context, tenantID string, algorithm string) (DEKID, error) {
	dekid, err := c.dek.EnsureTenantDEK(ctx, tenantID, algorithm)
	if err != nil {
		return "", err
	}

	// Sync to cluster if enabled
	if c.config.EnableClusterSync && c.kvProposer != nil {
		entry, loadErr := c.dek.loadDEKEntry(ctx, dekid)
		if loadErr == nil {
			if syncErr := c.syncDEKToCluster(ctx, dekid, entry); syncErr != nil {
				c.logger.WithError(syncErr).WithField("tenant_id", tenantID).
					Warn("Failed to sync tenant DEK to cluster")
			}
		}
	}

	return dekid, nil
}

// DeleteTenantDEK deletes a tenant's DEK.
func (c *ClusterDEKManager) DeleteTenantDEK(ctx context.Context, tenantID string) error {
	return c.dek.DeleteTenantDEK(ctx, tenantID)
}

// CleanupOldVersions removes old DEK versions beyond the retention limit.
func (c *ClusterDEKManager) CleanupOldVersions(ctx context.Context, dekid DEKID, retainCount int) (int, error) {
	return c.dek.CleanupOldVersions(ctx, dekid, retainCount)
}

// Close releases resources.
func (c *ClusterDEKManager) Close() error {
	return c.dek.Close()
}

// IsClusterMode returns true if cluster synchronization is enabled.
func (c *ClusterDEKManager) IsClusterMode() bool {
	return c.config.EnableClusterSync && c.kvProposer != nil
}

// GetReencryptionStats returns the current re-encryption statistics.
func (c *ClusterDEKManager) GetReencryptionStats() *ReencryptionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.reencryptStats == nil {
		return nil
	}

	// Return a copy
	stats := *c.reencryptStats
	return &stats
}

// ReencryptionRequest specifies a background re-encryption request.
type ReencryptionRequest struct {
	// DEKID is the DEK to re-encrypt secrets for.
	DEKID DEKID

	// SourceVersion is the old DEK version to migrate from.
	SourceVersion uint64

	// TargetVersion is the current DEK version to migrate to (0 = current).
	TargetVersion uint64

	// SecretsService is the service for re-encryption operations.
	SecretsService SecretsServiceForReencryption

	// BatchSize is the number of secrets to process per batch.
	BatchSize int

	// OnProgress is called after each batch (optional).
	OnProgress func(stats *ReencryptionStats)
}

// StartReencryption begins background re-encryption of secrets.
// This scans all secrets encrypted with the source DEK version
// and re-encrypts them with the target (current) version.
func (c *ClusterDEKManager) StartReencryption(ctx context.Context, req *ReencryptionRequest) error {
	c.mu.Lock()
	if c.reencrypting {
		c.mu.Unlock()
		return ErrReencryptionInProgress
	}
	c.reencrypting = true
	c.mu.Unlock()

	// Validate request
	if req.DEKID == "" {
		req.DEKID = DEKIDGlobal
	}
	if req.BatchSize <= 0 {
		req.BatchSize = reencryptionBatchSize
	}

	// Get current version if target not specified
	targetVersion := req.TargetVersion
	if targetVersion == 0 {
		status, err := c.dek.GetStatus(ctx, req.DEKID)
		if err != nil {
			c.mu.Lock()
			c.reencrypting = false
			c.mu.Unlock()
			return &SecretsSentinelCauseError{Sentinel: ErrReencryptionFailed, Cause: &SecretsCauseError{Message: "failed to get current DEK version", Cause: err}}
		}
		targetVersion = status.Version
	}

	// Initialize stats
	stats := &ReencryptionStats{
		StartedAt:     time.Now(),
		DEKID:         req.DEKID,
		SourceVersion: req.SourceVersion,
		TargetVersion: targetVersion,
		InProgress:    true,
	}

	c.mu.Lock()
	c.reencryptStats = stats
	c.mu.Unlock()

	// Run re-encryption in background
	go c.runReencryption(ctx, req, stats)

	return nil
}

// runReencryption performs the actual re-encryption work.
func (c *ClusterDEKManager) runReencryption(ctx context.Context, req *ReencryptionRequest, stats *ReencryptionStats) {
	defer func() {
		c.mu.Lock()
		c.reencrypting = false
		stats.InProgress = false
		stats.CompletedAt = time.Now()
		c.mu.Unlock()

		c.logger.WithField("dekid", stats.DEKID).
			WithField("source_version", stats.SourceVersion).
			WithField("target_version", stats.TargetVersion).
			WithField("processed", stats.ProcessedCount).
			WithField("success", stats.SuccessCount).
			WithField("errors", stats.ErrorCount).
			Info("Re-encryption completed")
	}()

	if req.SecretsService == nil {
		c.logger.Warn("No secrets service provided for re-encryption")
		return
	}

	// List all secrets
	cursor := ""
	for {
		select {
		case <-ctx.Done():
			stats.LastError = ctx.Err().Error()
			return
		default:
		}

		// List a batch of secrets
		secrets, nextCursor, hasMore, totalCount, err := req.SecretsService.ListSecrets(ctx, req.BatchSize, cursor)
		if err != nil {
			c.logger.WithError(err).Error("Failed to list secrets for re-encryption")
			stats.LastError = err.Error()
			return
		}

		if stats.TotalSecrets == 0 {
			stats.TotalSecrets = totalCount
		}

		// Process each secret in the batch
		for _, secret := range secrets {
			if err := c.reencryptSecret(ctx, req, &secret, stats); err != nil {
				c.logger.WithError(err).
					WithField("secret_id", secret.SecretID).
					Warn("Failed to re-encrypt secret")
				stats.ErrorCount++
				stats.LastError = err.Error()
			} else {
				stats.SuccessCount++
			}
			stats.ProcessedCount++
		}

		// Notify progress callback
		if req.OnProgress != nil {
			c.mu.RLock()
			statsCopy := *stats
			c.mu.RUnlock()
			req.OnProgress(&statsCopy)
		}

		// Check if there are more secrets
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor

		// Small delay between batches to avoid overload
		select {
		case <-ctx.Done():
			stats.LastError = ctx.Err().Error()
			return
		case <-time.After(reencryptionBatchDelay):
		}
	}
}

// reencryptSecret re-encrypts a single secret with the target DEK version.
func (c *ClusterDEKManager) reencryptSecret(
	ctx context.Context,
	req *ReencryptionRequest,
	secret *SecretInfo,
	stats *ReencryptionStats,
) error {
	// Check if this secret is encrypted with the target DEKID
	keyID := secret.KeyIdentifier
	if !IsDEKID(keyID) {
		// Not using a DEKID, skip
		return nil
	}

	dekid := DEKID(keyID)
	if dekid != req.DEKID {
		// Different DEK, skip
		return nil
	}

	// Get the secret's current data
	data, metadata, _, err := req.SecretsService.GetSecretData(ctx, secret.SecretID)
	if err != nil {
		return &SecretsCauseError{Message: "failed to get secret data", Cause: err}
	}

	// Re-encrypt by storing with the current (target) DEK version
	if err := req.SecretsService.ReencryptSecret(ctx, secret.SecretID, data, metadata); err != nil {
		return &SecretsCauseError{Message: "failed to re-encrypt secret", Cause: err}
	}

	return nil
}

// StopReencryption cancels an in-progress re-encryption.
// Note: The actual cancellation is done via context cancellation.
func (c *ClusterDEKManager) StopReencryption() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reencryptStats != nil {
		c.reencryptStats.InProgress = false
		c.reencryptStats.LastError = "cancelled by user"
	}
}

// SecretsServiceForReencryption defines the minimal interface needed for re-encryption.
// This avoids a circular import with the service package.
type SecretsServiceForReencryption interface {
	ListSecrets(ctx context.Context, limit int, cursor string) (secrets []SecretInfo, nextCursor string, hasMore bool, totalCount int, err error)
	GetSecretData(ctx context.Context, secretID string) (data []byte, metadata map[string]string, keyIdentifier string, err error)
	ReencryptSecret(ctx context.Context, secretID string, data []byte, metadata map[string]string) error
}

// SecretInfo contains minimal information about a secret for re-encryption listing.
type SecretInfo struct {
	SecretID      string
	KeyIdentifier string
}

// Ensure ClusterDEKManager satisfies the DEK interface.
var _ interface {
	Initialize(ctx context.Context, algorithm string) error
	Rotate(ctx context.Context, dekid DEKID) (uint64, error)
	GetKey(ctx context.Context, dekid DEKID) ([]byte, uint64, error)
	GetStatus(ctx context.Context, dekid DEKID) (*DEKStatus, error)
} = (*ClusterDEKManager)(nil)

// MarshalJSON implements json.Marshaler for ReencryptionStats.
func (s *ReencryptionStats) MarshalJSON() ([]byte, error) {
	type Alias ReencryptionStats
	return json.Marshal(&struct {
		*Alias
		Duration string `json:"duration,omitempty"`
	}{
		Alias:    (*Alias)(s),
		Duration: s.Duration().String(),
	})
}

// Duration returns the duration of the re-encryption operation.
func (s *ReencryptionStats) Duration() time.Duration {
	if s.CompletedAt.IsZero() {
		return time.Since(s.StartedAt)
	}
	return s.CompletedAt.Sub(s.StartedAt)
}

// Progress returns the completion percentage (0-100).
func (s *ReencryptionStats) Progress() float64 {
	if s.TotalSecrets == 0 {
		return 0
	}
	return float64(s.ProcessedCount) / float64(s.TotalSecrets) * 100
}

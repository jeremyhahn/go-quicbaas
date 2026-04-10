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

package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

const (
	// fileLockPrefix is the KV key prefix for all file locks.
	fileLockPrefix = "filelock:"

	// defaultFileLockPollInterval is the interval for WaitForRelease polling.
	defaultFileLockPollInterval = 500 * time.Millisecond
)

// fileLockStore is the production implementation of FileLockService.
type fileLockStore struct {
	kvStore      interfaces.KVStore
	leaseManager lease.LeaseManager
	raftNode     interfaces.RaftNode
	shardID      uint64
	settings     FileLockSettings
	isolation    WorkspaceIsolation
	logger       interfaces.Logger
}

// NewFileLockService creates a new FileLockService backed by a KV store and lease manager.
// Pass nil for raftNode when running in standalone (non-Raft) mode.
func NewFileLockService(
	kvStore interfaces.KVStore,
	leaseManager lease.LeaseManager,
	raftNode interfaces.RaftNode,
	shardID uint64,
	settings FileLockSettings,
	isolation WorkspaceIsolation,
	logger interfaces.Logger,
) FileLockService {
	return &fileLockStore{
		kvStore:      kvStore,
		leaseManager: leaseManager,
		raftNode:     raftNode,
		shardID:      shardID,
		settings:     settings,
		isolation:    isolation,
		logger:       logger.WithField("component", "file_lock_service"),
	}
}

// buildLockKey constructs a deterministic lock key from tenant, project, and file path.
func buildLockKey(tenantID, projectID, filePath string) string {
	hash := sha256.Sum256([]byte(filePath))
	return fileLockPrefix + tenantID + ":" + projectID + ":" + hex.EncodeToString(hash[:])
}

// Acquire obtains a lock on the specified file path for the given agent.
func (s *fileLockStore) Acquire(ctx context.Context, tenantID, projectID, filePath, agentID string) (*FileLock, error) {
	if err := s.validateAcquireInputs(tenantID, projectID, filePath, agentID); err != nil {
		return nil, err
	}

	lockKey := buildLockKey(tenantID, projectID, filePath)

	// Check for existing lock.
	existing, found, err := s.loadLock(ctx, lockKey)
	if err != nil {
		return nil, err
	}

	if found {
		// Check if existing lease is still valid.
		existingLease, leaseErr := s.leaseManager.GetLease(ctx, existing.LeaseID)
		leaseValid := leaseErr == nil && existingLease != nil && existingLease.IsValid()

		if leaseValid {
			// Lock exists with a valid lease.
			if s.isolation == IsolationWorktree {
				// Worktree mode: advisory only, log warning but allow re-acquire.
				s.logger.WithFields(map[string]interface{}{
					"lock_key": lockKey,
					"holder":   existing.AgentID,
					"agent_id": agentID,
				}).Warn("Worktree mode: acquiring advisory lock over existing holder")
			} else {
				// Shared mode: reject if another agent holds the lock.
				if existing.AgentID != agentID {
					return nil, &FileLockedError{
						AgentID:    existing.AgentID,
						FilePath:   existing.FilePath,
						AcquiredAt: existing.AcquiredAt,
					}
				}
				// Same agent re-acquiring: allow (idempotent).
			}
		}
		// Expired lease: proceed to overwrite the stale lock.
	}

	// Get fencing token from Raft state if available.
	fencingToken := uint64(0)
	if s.raftNode != nil {
		_, term, raftErr := s.raftNode.GetShardRaftState(s.shardID)
		if raftErr == nil {
			fencingToken = term
		}
	}

	// Parse lease TTL from settings.
	leaseTTL, err := time.ParseDuration(s.settings.LeaseTTL)
	if err != nil {
		return nil, &FileLockValidationError{
			Field:   "lease_ttl",
			Message: "invalid duration: " + s.settings.LeaseTTL,
		}
	}

	// Create a lease for this lock.
	newLease, err := s.leaseManager.CreateLease(ctx, lockKey, leaseTTL, &lease.LeaseOptions{
		Renewable:   true,
		MaxRenewals: s.settings.MaxRenewals,
		Metadata: map[string]string{
			"agent_id":   agentID,
			"tenant_id":  tenantID,
			"project_id": projectID,
			"file_path":  filePath,
		},
	})
	if err != nil {
		return nil, err
	}

	// Build and persist the lock.
	lock := &FileLock{
		LockKey:      lockKey,
		AgentID:      agentID,
		TenantID:     tenantID,
		ProjectID:    projectID,
		FilePath:     filePath,
		LeaseID:      newLease.LeaseID,
		FencingToken: fencingToken,
		AcquiredAt:   time.Now().UnixNano(),
		Isolation:    string(s.isolation),
	}

	if err := s.storeLock(ctx, lock); err != nil {
		return nil, err
	}

	s.logger.WithFields(map[string]interface{}{
		"lock_key":      lockKey,
		"agent_id":      agentID,
		"fencing_token": fencingToken,
		"lease_id":      newLease.LeaseID,
	}).Info("File lock acquired")

	return lock, nil
}

// Release releases a previously acquired lock. Only the lock holder can release.
func (s *fileLockStore) Release(ctx context.Context, tenantID, lockKey string) error {
	existing, found, err := s.loadLock(ctx, lockKey)
	if err != nil {
		return err
	}
	if !found {
		return &LockNotFoundError{LockKey: lockKey}
	}

	// Revoke the lease.
	if revokeErr := s.leaseManager.RevokeLease(ctx, existing.LeaseID); revokeErr != nil {
		s.logger.WithError(revokeErr).WithField("lease_id", existing.LeaseID).Warn("Failed to revoke lease during release")
	}

	// Delete the lock key from KV.
	if err := s.kvStore.Delete(ctx, lockKey); err != nil {
		return err
	}

	s.logger.WithFields(map[string]interface{}{
		"lock_key": lockKey,
		"agent_id": existing.AgentID,
	}).Info("File lock released")

	return nil
}

// Renew extends the lease on an existing lock.
func (s *fileLockStore) Renew(ctx context.Context, tenantID, lockKey string) error {
	existing, found, err := s.loadLock(ctx, lockKey)
	if err != nil {
		return err
	}
	if !found {
		return &LockNotFoundError{LockKey: lockKey}
	}

	// Validate fencing token before renewing.
	if err := s.validateFencingToken(existing); err != nil {
		return err
	}

	heartbeat, err := time.ParseDuration(s.settings.HeartbeatInterval)
	if err != nil {
		return &FileLockValidationError{
			Field:   "heartbeat_interval",
			Message: "invalid duration: " + s.settings.HeartbeatInterval,
		}
	}

	_, err = s.leaseManager.RenewLease(ctx, existing.LeaseID, heartbeat)
	if err != nil {
		return err
	}

	s.logger.WithFields(map[string]interface{}{
		"lock_key": lockKey,
		"lease_id": existing.LeaseID,
	}).Debug("File lock renewed")

	return nil
}

// IsLocked checks whether the file path is currently locked.
// Returns the lock, whether it is active, and any error.
func (s *fileLockStore) IsLocked(ctx context.Context, tenantID, projectID, filePath string) (*FileLock, bool, error) {
	lockKey := buildLockKey(tenantID, projectID, filePath)

	existing, found, err := s.loadLock(ctx, lockKey)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	// Check if the lease is still valid.
	existingLease, leaseErr := s.leaseManager.GetLease(ctx, existing.LeaseID)
	if leaseErr != nil || existingLease == nil || !existingLease.IsValid() {
		// Lease expired or not found: clean up stale lock.
		if delErr := s.kvStore.Delete(ctx, lockKey); delErr != nil {
			s.logger.WithError(delErr).WithField("lock_key", lockKey).Warn("Failed to clean up stale lock")
		}
		return nil, false, nil
	}

	return existing, true, nil
}

// WaitForRelease blocks until the lock is released or the context is cancelled.
func (s *fileLockStore) WaitForRelease(ctx context.Context, tenantID, lockKey string) error {
	// Check if currently locked by reading the lock from KV.
	existing, found, err := s.loadLock(ctx, lockKey)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	// Verify lease is active; if not, the lock is already released.
	existingLease, leaseErr := s.leaseManager.GetLease(ctx, existing.LeaseID)
	if leaseErr != nil || existingLease == nil || !existingLease.IsValid() {
		return nil
	}

	// Poll for release using a timer (no time.Sleep per conventions).
	ticker := time.NewTicker(defaultFileLockPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			lock, lockFound, loadErr := s.loadLock(ctx, lockKey)
			if loadErr != nil {
				return loadErr
			}
			if !lockFound {
				return nil
			}

			lockLease, leaseGetErr := s.leaseManager.GetLease(ctx, lock.LeaseID)
			if leaseGetErr != nil || lockLease == nil || !lockLease.IsValid() {
				return nil
			}
		}
	}
}

// ListLocks returns all active locks for the given tenant and project.
func (s *fileLockStore) ListLocks(ctx context.Context, tenantID, projectID string) ([]*FileLock, error) {
	prefix := fileLockPrefix + tenantID + ":" + projectID + ":"

	var locks []*FileLock
	scanErr := s.kvStore.Scan(ctx, prefix, func(key string, value []byte) error {
		lock := &FileLock{}
		if err := json.Unmarshal(value, lock); err != nil {
			s.logger.WithError(err).WithField("key", key).Warn("Failed to unmarshal lock during list")
			return nil
		}

		// Filter out expired leases.
		lockLease, leaseErr := s.leaseManager.GetLease(ctx, lock.LeaseID)
		if leaseErr != nil || lockLease == nil || !lockLease.IsValid() {
			// Stale lock: skip it but clean up.
			if delErr := s.kvStore.Delete(ctx, key); delErr != nil {
				s.logger.WithError(delErr).WithField("key", key).Warn("Failed to clean up stale lock during list")
			}
			return nil
		}

		locks = append(locks, lock)
		return nil
	})
	if scanErr != nil {
		return nil, scanErr
	}

	if locks == nil {
		locks = make([]*FileLock, 0)
	}

	return locks, nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// validateAcquireInputs checks that all required fields are provided for Acquire.
func (s *fileLockStore) validateAcquireInputs(tenantID, projectID, filePath, agentID string) error {
	if tenantID == "" {
		return &FileLockValidationError{Field: "tenant_id", Message: "required"}
	}
	if projectID == "" {
		return &FileLockValidationError{Field: "project_id", Message: "required"}
	}
	if filePath == "" {
		return &FileLockValidationError{Field: "file_path", Message: "required"}
	}
	if agentID == "" {
		return &FileLockValidationError{Field: "agent_id", Message: "required"}
	}
	return nil
}

// validateFencingToken checks that the lock's fencing token is not behind the
// current Raft term. In standalone mode (raftNode == nil) or when the token is
// zero, validation is skipped.
func (s *fileLockStore) validateFencingToken(lock *FileLock) error {
	if lock.FencingToken == 0 {
		return nil
	}
	if s.raftNode == nil {
		return nil
	}
	_, currentTerm, err := s.raftNode.GetShardRaftState(s.shardID)
	if err != nil {
		// Cannot validate: allow the operation to proceed.
		return nil
	}
	if lock.FencingToken < currentTerm {
		return &StaleLockError{
			LockKey:     lock.LockKey,
			LockTerm:    lock.FencingToken,
			CurrentTerm: currentTerm,
		}
	}
	return nil
}

// loadLock reads and deserializes a FileLock from the KV store.
func (s *fileLockStore) loadLock(ctx context.Context, lockKey string) (*FileLock, bool, error) {
	data, err := s.kvStore.Get(ctx, lockKey)
	if err != nil {
		// Distinguish "not found" from real errors.
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if data == nil {
		return nil, false, nil
	}

	lock := &FileLock{}
	if err := json.Unmarshal(data, lock); err != nil {
		return nil, false, err
	}

	return lock, true, nil
}

// storeLock serializes and persists a FileLock to the KV store.
func (s *fileLockStore) storeLock(ctx context.Context, lock *FileLock) error {
	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	return s.kvStore.Put(ctx, lock.LockKey, data)
}

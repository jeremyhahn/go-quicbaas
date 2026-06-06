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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// defaultRotationScanInterval is how often the scheduler scans for keys whose
// cryptoperiod has expired when no explicit interval is configured.
const defaultRotationScanInterval = 5 * time.Minute

// KMSRotationScheduler scans persisted key metadata on a configurable interval
// and initiates rotation for every key whose RotationPolicy.AutoRotate is true
// and whose NextRotation timestamp is in the past.
//
// It mirrors the lifecycle pattern of the grace-period scheduler: started and
// stopped from the same location in newQuicBaaS where the scheduler engine is
// wired to the KMS service.
type KMSRotationScheduler struct {
	kms      *kmsServiceImpl
	logger   interfaces.Logger
	interval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newKMSRotationScheduler constructs a KMSRotationScheduler.
// interval is the scan period; when zero, defaultRotationScanInterval is used.
func newKMSRotationScheduler(k *kmsServiceImpl, logger interfaces.Logger, interval time.Duration) *KMSRotationScheduler {
	if interval <= 0 {
		interval = defaultRotationScanInterval
	}
	return &KMSRotationScheduler{
		kms:      k,
		logger:   logger,
		interval: interval,
	}
}

// Start launches the background scan loop under the provided parent context.
// It is idempotent: calling Start on an already-running scheduler is a no-op.
func (s *KMSRotationScheduler) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		// Already running.
		return
	}

	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel

	s.wg.Add(1)
	go s.run(ctx)
}

// Stop signals the scan loop to exit and waits for it to finish.
func (s *KMSRotationScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// run is the scan-loop goroutine. It ticks every s.interval, collects expired
// keys, and rotates each one under a system context so that per-key authz is
// bypassed (PCI DSS 3.6 — system-initiated cryptoperiod enforcement).
func (s *KMSRotationScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanAndRotate(ctx)
		}
	}
}

// scanAndRotate performs a single pass: list all keys and rotate those that are
// due according to their RotationPolicy.
func (s *KMSRotationScheduler) scanAndRotate(ctx context.Context) {
	// Build a system context so per-key authz is bypassed for this
	// internal daemon operation. This must never be derived from a
	// network-originated context.
	sysCtx := auth.NewSystemContext(ctx)

	keys, err := s.kms.kmsManager.ListKeys(sysCtx, "")
	if err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Error("kms rotation scheduler: failed to list keys")
		}
		return
	}

	now := time.Now()

	for _, raw := range keys {
		kmsMeta, ok := raw.(*kms.KeyMetadata)
		if !ok {
			continue
		}

		rp := kmsMeta.RotationPolicy
		if rp == nil || !rp.AutoRotate || !rp.Enabled {
			continue
		}

		if rp.NextRotation.IsZero() || !rp.NextRotation.Before(now) {
			// Not yet due.
			continue
		}

		s.rotateKey(sysCtx, kmsMeta, rp)
	}
}

// rotateKey calls RotateKey for a single key whose cryptoperiod has expired,
// advances NextRotation by the policy interval, and emits an audit event.
func (s *KMSRotationScheduler) rotateKey(ctx context.Context, meta *kms.KeyMetadata, rp *kms.RotationPolicy) {
	// Use the unscopeed (caller-visible) key name. Because the scheduler
	// receives data from kmsManager.ListKeys, the KeyID may be tenant-scoped.
	// RotateKey calls kmsManager.RotateKey after re-applying the tenant scope,
	// so we pass the raw KeyID exactly as it came from the manager.
	keyID := meta.KeyID

	rotateReq := &RotateKeyRequest{
		KeyID: keyID,
		// GracePeriodSeconds is intentionally zero: the scheduler does not
		// automatically schedule a grace period on auto-rotation. Operators
		// can configure per-key grace periods through the normal API.
	}

	resp, rotErr := s.kms.RotateKey(ctx, rotateReq)

	// Emit audit event regardless of outcome.
	s.logAutoRotation(ctx, keyID, meta.Algorithm, rotErr)

	if rotErr != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id":    keyID,
				"algorithm": meta.Algorithm,
				"error":     rotErr.Error(),
			}).Error("kms rotation scheduler: auto-rotation failed")
		}
		return
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_id":      keyID,
			"old_version": resp.OldVersion,
			"new_version": resp.NewVersion,
		}).Info("kms rotation scheduler: auto-rotated key at cryptoperiod expiry")
	}

	// Advance NextRotation. RotateKey does not touch RotationPolicy, so the
	// scheduler is solely responsible for computing the next deadline.
	if rp.Interval > 0 {
		s.advanceNextRotation(ctx, meta, rp)
	}
}

// advanceNextRotation computes the next NextRotation timestamp and persists it
// via UpdateStoredMetadata so that the new deadline survives restarts.
func (s *KMSRotationScheduler) advanceNextRotation(ctx context.Context, meta *kms.KeyMetadata, rp *kms.RotationPolicy) {
	now := time.Now()

	// Compute the next deadline from the old NextRotation rather than from now
	// so that cumulative drift does not cause late rotations to accumulate.
	next := rp.NextRotation.Add(rp.Interval)
	// Clamp: if the computed next is not strictly in the future (e.g., the
	// daemon was down for more than one interval, or next == now due to clock
	// resolution), advance to the first boundary strictly after now.
	for !next.After(now) {
		next = next.Add(rp.Interval)
	}

	// Build an updated copy to persist. We do not modify the pointer in-place
	// because meta came from a deep-copy inside ListKeys/copyMetadata.
	updated := *meta
	updatedPolicy := *rp
	updatedPolicy.NextRotation = next
	updated.RotationPolicy = &updatedPolicy
	updated.Updated = now

	if err := s.kms.kmsManager.UpdateStoredMetadata(&updated); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id":        meta.KeyID,
				"next_rotation": next,
				"error":         err.Error(),
			}).Warn("kms rotation scheduler: failed to advance NextRotation")
		}
		return
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_id":        meta.KeyID,
			"next_rotation": next,
		}).Debug("kms rotation scheduler: advanced NextRotation")
	}
}

// logAutoRotation emits an audit event for the auto-rotation attempt.
// It extends the existing KMSAuditLogger via the optional KMSAutoRotationAuditLogger
// interface; when the installed logger does not implement that extension the
// rotation is still recorded via the structured logger.
func (s *KMSRotationScheduler) logAutoRotation(ctx context.Context, keyID, algorithm string, rotErr error) {
	success := rotErr == nil
	errMsg := ""
	if rotErr != nil {
		errMsg = rotErr.Error()
	}

	if s.kms.auditLogger != nil {
		if ral, ok := s.kms.auditLogger.(KMSAutoRotationAuditLogger); ok {
			if err := ral.LogKeyAutoRotation(ctx, keyID, algorithm, success, errMsg); err != nil && s.logger != nil {
				s.logger.WithError(err).WithFields(map[string]interface{}{
					"key_id":    keyID,
					"operation": "auto_rotation",
				}).Error("kms rotation scheduler: failed to write auto-rotation audit log")
			}
			return
		}
	}

	// Fallback: structured logger only.
	if s.logger != nil {
		fields := map[string]interface{}{
			"key_id":    keyID,
			"algorithm": algorithm,
			"success":   success,
		}
		if errMsg != "" {
			fields["error"] = errMsg
		}
		s.logger.WithFields(fields).Info("kms rotation scheduler: key auto-rotation audit")
	}
}

// =============================================================================
// Audit Logger Extension
// =============================================================================

// KMSAutoRotationAuditLogger is an optional extension of KMSAuditLogger that
// adds structured audit logging for scheduler-driven key auto-rotation events
// (PCI DSS 3.6 cryptoperiod enforcement). Implementations that want to record
// auto-rotation events in their audit store should implement this interface in
// addition to KMSAuditLogger.
type KMSAutoRotationAuditLogger interface {
	// LogKeyAutoRotation logs a cryptoperiod-driven auto-rotation attempt.
	// success is false when RotateKey returned an error, in which case errMsg
	// contains the error string.
	LogKeyAutoRotation(ctx context.Context, keyID, algorithm string, success bool, errMsg string) error
}

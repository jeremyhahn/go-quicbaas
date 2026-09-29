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

package service

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
)

// KMSRotationScanError reports a rotation scan that could not list the keys it
// was to examine. No key was rotated; the scheduler retries the scan.
type KMSRotationScanError struct {
	Cause error
}

// Error implements error.
func (e *KMSRotationScanError) Error() string {
	return "kms rotation: list keys: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *KMSRotationScanError) Unwrap() error {
	return e.Cause
}

// Verify kmsServiceImpl drives the scheduler's rotation handler.
var _ schedulerhandlers.KMSKeyRotator = (*kmsServiceImpl)(nil)

// registerKMSRotation registers the rotation handler on engine and declares
// the rotation system job, scanning every interval.
//
// Cryptoperiod enforcement (PCI DSS 3.6) runs on the scheduler rather than on a
// timer of its own so that a cluster rotates each due key once: the job is
// cluster-wide, so only the leader dispatches it, and the firing claim lets
// exactly one node execute each scan even while leadership is changing hands.
// A standalone node has no peers and runs every scan itself.
func registerKMSRotation(engine *scheduler.SchedulerEngine, k *kmsServiceImpl, interval time.Duration) error {
	job, err := schedulerhandlers.KMSRotationJob(interval)
	if err != nil {
		return err
	}
	handler := schedulerhandlers.NewKMSRotationHandler(k)
	if err := engine.RegisterHandler(handler.Name(), handler.Handler()); err != nil {
		return err
	}
	return engine.RegisterSystemJob(job)
}

// RotateDueKeys rotates every key whose RotationPolicy is enabled, set to
// auto-rotate, and past its NextRotation deadline. It is the body of the KMS
// rotation system job.
//
// Rotation runs under a system context so per-key authz is bypassed: this is
// system-initiated cryptoperiod enforcement and must never be derived from a
// network-originated context.
func (k *kmsServiceImpl) RotateDueKeys(ctx context.Context) (*schedulerhandlers.KMSRotationResult, error) {
	sysCtx := auth.NewSystemContext(ctx)

	keys, err := k.kmsManager.ListKeys(sysCtx, "")
	if err != nil {
		return nil, &KMSRotationScanError{Cause: err}
	}

	now := time.Now()
	result := &schedulerhandlers.KMSRotationResult{}
	for _, raw := range keys {
		meta, ok := raw.(*kms.KeyMetadata)
		if !ok {
			continue
		}
		rp := meta.RotationPolicy
		if rp == nil || !rp.AutoRotate || !rp.Enabled {
			continue
		}
		if rp.NextRotation.IsZero() || !rp.NextRotation.Before(now) {
			continue
		}

		result.Due++
		if k.rotateDueKey(sysCtx, meta, rp) {
			result.Rotated++
		} else {
			result.Failed++
		}
	}
	return result, nil
}

// rotateDueKey rotates one key whose cryptoperiod has expired, advances its
// NextRotation by the policy interval, and records an audit event. It reports
// whether the rotation succeeded.
func (k *kmsServiceImpl) rotateDueKey(ctx context.Context, meta *kms.KeyMetadata, rp *kms.RotationPolicy) bool {
	// The KeyID comes from kmsManager.ListKeys and may be tenant-scoped.
	// RotateKey re-applies the tenant scope, so it is passed through as is.
	// GracePeriodSeconds stays zero: auto-rotation schedules no grace period;
	// operators configure per-key grace periods through the API.
	resp, rotErr := k.RotateKey(ctx, &RotateKeyRequest{KeyID: meta.KeyID})

	k.logAutoRotation(ctx, meta.KeyID, meta.Algorithm, rotErr)

	if rotErr != nil {
		if k.logger != nil {
			k.logger.WithFields(map[string]interface{}{
				"key_id":    meta.KeyID,
				"algorithm": meta.Algorithm,
				"error":     rotErr.Error(),
			}).Error("kms rotation: auto-rotation failed")
		}
		return false
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":      meta.KeyID,
			"old_version": resp.OldVersion,
			"new_version": resp.NewVersion,
		}).Info("kms rotation: auto-rotated key at cryptoperiod expiry")
	}

	// RotateKey does not touch RotationPolicy, so the scan owns the next
	// deadline.
	if rp.Interval > 0 {
		k.advanceNextRotation(meta, rp)
	}
	return true
}

// advanceNextRotation computes the next NextRotation deadline and persists it
// via UpdateStoredMetadata so that it survives restarts.
func (k *kmsServiceImpl) advanceNextRotation(meta *kms.KeyMetadata, rp *kms.RotationPolicy) {
	now := time.Now()

	// Advance from the old deadline rather than from now so late scans do not
	// accumulate drift. If the node was down for more than one interval, skip
	// forward to the first boundary strictly after now.
	next := rp.NextRotation.Add(rp.Interval)
	for !next.After(now) {
		next = next.Add(rp.Interval)
	}

	// meta is ListKeys' deep copy; persist an updated copy of it.
	updated := *meta
	updatedPolicy := *rp
	updatedPolicy.NextRotation = next
	updated.RotationPolicy = &updatedPolicy
	updated.Updated = now

	if err := k.kmsManager.UpdateStoredMetadata(&updated); err != nil {
		if k.logger != nil {
			k.logger.WithFields(map[string]interface{}{
				"key_id":        meta.KeyID,
				"next_rotation": next,
				"error":         err.Error(),
			}).Warn("kms rotation: failed to advance NextRotation")
		}
		return
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":        meta.KeyID,
			"next_rotation": next,
		}).Debug("kms rotation: advanced NextRotation")
	}
}

// logAutoRotation records an auto-rotation attempt. It writes through the
// optional KMSAutoRotationAuditLogger extension when the installed audit
// logger implements it, and through the structured logger otherwise.
func (k *kmsServiceImpl) logAutoRotation(ctx context.Context, keyID, algorithm string, rotErr error) {
	success := rotErr == nil
	errMsg := ""
	if rotErr != nil {
		errMsg = rotErr.Error()
	}

	if ral, ok := k.auditLogger.(KMSAutoRotationAuditLogger); ok {
		if err := ral.LogKeyAutoRotation(ctx, keyID, algorithm, success, errMsg); err != nil && k.logger != nil {
			k.logger.WithError(err).WithFields(map[string]interface{}{
				"key_id":    keyID,
				"operation": "auto_rotation",
			}).Error("kms rotation: failed to write auto-rotation audit log")
		}
		return
	}

	if k.logger != nil {
		fields := map[string]interface{}{
			"key_id":    keyID,
			"algorithm": algorithm,
			"success":   success,
		}
		if errMsg != "" {
			fields["error"] = errMsg
		}
		k.logger.WithFields(fields).Info("kms rotation: key auto-rotation audit")
	}
}

// KMSAutoRotationAuditLogger is an optional extension of KMSAuditLogger that
// records scheduler-driven key auto-rotation events (PCI DSS 3.6 cryptoperiod
// enforcement). Audit loggers that want those events in their audit store
// implement it alongside KMSAuditLogger.
type KMSAutoRotationAuditLogger interface {
	// LogKeyAutoRotation logs a cryptoperiod-driven auto-rotation attempt.
	// success is false when RotateKey returned an error, in which case errMsg
	// contains the error string.
	LogKeyAutoRotation(ctx context.Context, keyID, algorithm string, success bool, errMsg string) error
}

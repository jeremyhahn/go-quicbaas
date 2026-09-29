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

package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// HandlerKMSRotation is the handler name for the KMS cryptoperiod rotation
// scan (PCI DSS 3.6).
const HandlerKMSRotation = "kms.rotation"

// KMSRotationJobID is the reserved ID of the KMS rotation system job. Every
// node declares the job under this ID, so a cluster holds exactly one record
// and the firing claim lets exactly one node run each scan.
const KMSRotationJobID = scheduler.SystemJobIDFloor + 1

// DefaultKMSRotationScanInterval is how often the rotation scan runs when no
// interval is configured.
const DefaultKMSRotationScanInterval = 5 * time.Minute

// KMSRotationResult summarizes one rotation scan.
type KMSRotationResult struct {
	// Due is the number of keys whose cryptoperiod had expired.
	Due int
	// Rotated is the number of due keys rotated successfully.
	Rotated int
	// Failed is the number of due keys whose rotation failed. They stay due
	// and are retried by the next scan.
	Failed int
}

// KMSKeyRotator rotates every key whose auto-rotation policy is due.
// It is satisfied by the service layer's KMS service.
type KMSKeyRotator interface {
	// RotateDueKeys runs one scan. It returns an error only when the scan
	// itself could not run; per-key failures are counted in the result.
	RotateDueKeys(ctx context.Context) (*KMSRotationResult, error)
}

// KMSRotationHandler runs the KMS rotation scan as a scheduler job.
type KMSRotationHandler struct {
	rotator KMSKeyRotator
}

// NewKMSRotationHandler creates a rotation handler over rotator.
func NewKMSRotationHandler(rotator KMSKeyRotator) *KMSRotationHandler {
	return &KMSRotationHandler{rotator: rotator}
}

// Name returns the handler name for registration.
func (h *KMSRotationHandler) Name() string {
	return HandlerKMSRotation
}

// Handle runs one rotation scan.
func (h *KMSRotationHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	result, err := h.rotator.RotateDueKeys(ctx)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerKMSRotation,
			JobID:   job.ID,
			Cause:   err,
		}
	}
	return &scheduler.JobResult{
		Success: true,
		Message: fmt.Sprintf("rotated %d of %d due keys, %d failed",
			result.Rotated, result.Due, result.Failed),
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *KMSRotationHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// KMSRotationJob returns the system job definition for the rotation scan,
// running every interval. A zero interval selects
// DefaultKMSRotationScanInterval. The scheduler's cron schedules resolve to
// whole seconds, so interval must be a positive whole number of seconds.
//
// A scan's timeout equals its interval: a scan still running when the next is
// due has stalled.
//
// The job carries the scheduler's default retry budget. That budget is per
// firing, so a failed scan is retried with backoff and, once its budget is
// spent, the job moves on to its next scan rather than retiring: a system job
// that enforces cryptoperiods never stops scanning because scans failed.
func KMSRotationJob(interval time.Duration) (*scheduler.Job, error) {
	if interval == 0 {
		interval = DefaultKMSRotationScanInterval
	}
	if interval < time.Second || interval%time.Second != 0 {
		return nil, scheduler.InvalidJobError{
			Field:   "schedule",
			Message: fmt.Sprintf("kms rotation scan interval %s must be a positive whole number of seconds", interval),
		}
	}
	return &scheduler.Job{
		ID:          KMSRotationJobID,
		Name:        "kms-rotation",
		Description: "Rotates KMS keys whose auto-rotation cryptoperiod has expired (PCI DSS 3.6).",
		OwnerType:   scheduler.OwnerTypeSystem,
		Type:        scheduler.JobTypeRecurring,
		Schedule:    "@every " + interval.String(),
		Handler:     HandlerKMSRotation,
		Timeout:     interval,
	}, nil
}

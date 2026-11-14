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

// HandlerSecretsRotation is the handler name for the scan that performs the
// secrets rotation policies set by SetRotationPolicy.
const HandlerSecretsRotation = "secrets.rotation"

// SecretsRotationJobID is the reserved ID of the secrets rotation system job.
const SecretsRotationJobID = scheduler.SystemJobIDFloor + 3

// DefaultSecretsRotationScanInterval is how often the secrets rotation scan
// runs when no interval is configured.
const DefaultSecretsRotationScanInterval = 5 * time.Minute

// SecretsRotationResult summarizes one secrets rotation scan.
type SecretsRotationResult struct {
	// Due is the number of secrets whose rotation policy was due.
	Due int
	// Rotated is the number of due secrets rotated successfully.
	Rotated int
	// Failed is the number of due secrets whose rotation failed. They stay
	// due and are retried by the next scan.
	Failed int
}

// SecretsRotator performs the due secrets rotation policies.
type SecretsRotator interface {
	// RotateDueSecrets runs one scan. It returns an error only when the scan
	// itself could not run; per-secret failures are counted in the result.
	RotateDueSecrets(ctx context.Context) (*SecretsRotationResult, error)
}

// SecretsRotationHandler is the scheduler handler that runs the scan.
type SecretsRotationHandler struct {
	rotator SecretsRotator
}

// NewSecretsRotationHandler creates a rotation handler over rotator.
func NewSecretsRotationHandler(rotator SecretsRotator) *SecretsRotationHandler {
	return &SecretsRotationHandler{rotator: rotator}
}

// Name returns the handler name.
func (h *SecretsRotationHandler) Name() string {
	return HandlerSecretsRotation
}

// Handle runs one rotation scan.
func (h *SecretsRotationHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	result, err := h.rotator.RotateDueSecrets(ctx)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerSecretsRotation,
			JobID:   job.ID,
			Cause:   err,
		}
	}
	return &scheduler.JobResult{
		Success: true,
		Message: fmt.Sprintf("rotated %d of %d due secrets, %d failed",
			result.Rotated, result.Due, result.Failed),
	}, nil
}

// Handler returns the scheduler.JobHandler for registration.
func (h *SecretsRotationHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// SecretsRotationJob returns the system job definition for the secrets
// rotation scan, running every interval. A zero interval selects
// DefaultSecretsRotationScanInterval. The interval must be a whole number of
// seconds.
func SecretsRotationJob(interval time.Duration) (*scheduler.Job, error) {
	if interval == 0 {
		interval = DefaultSecretsRotationScanInterval
	}
	if interval < time.Second || interval%time.Second != 0 {
		return nil, scheduler.InvalidJobError{
			Field:   "schedule",
			Message: fmt.Sprintf("secrets rotation scan interval %s must be a positive whole number of seconds", interval),
		}
	}
	return &scheduler.Job{
		ID:          SecretsRotationJobID,
		Name:        "secrets-rotation",
		Description: "Performs the secrets rotation policies whose next rotation is due.",
		OwnerType:   scheduler.OwnerTypeSystem,
		Type:        scheduler.JobTypeRecurring,
		Schedule:    "@every " + interval.String(),
		Handler:     HandlerSecretsRotation,
		Timeout:     interval,
	}, nil
}

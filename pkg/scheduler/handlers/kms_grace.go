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

// Package handlers provides built-in job handlers for the scheduler.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// HandlerKMSGracePeriod is the handler name for KMS grace period jobs.
const HandlerKMSGracePeriod = "kms.grace_period"

// KeyStatus defines the status of a key for the grace period handler.
type KeyStatus string

const (
	// KeyStatusDisabled indicates the key is disabled.
	KeyStatusDisabled KeyStatus = "disabled"
)

// UpdateKeyStatusRequest is the request to update key status.
// This mirrors service.UpdateKeyStatusRequest to avoid import cycle.
type UpdateKeyStatusRequest struct {
	KeyID   string    `json:"key_id"`
	Version uint64    `json:"version"`
	Status  KeyStatus `json:"status"`
}

// UpdateKeyStatusResponse is the response from updating key status.
// This mirrors service.UpdateKeyStatusResponse to avoid import cycle.
type UpdateKeyStatusResponse struct {
	KeyID     string `json:"key_id"`
	Version   uint64 `json:"version"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
}

// GracePeriodResult is the result of a grace period update operation.
type GracePeriodResult struct {
	KeyID     string
	Version   uint64
	OldStatus string
	NewStatus string
}

// KMSKeyStatusUpdater defines the interface for updating key status.
// This interface is satisfied by service.kmsServiceImpl.
type KMSKeyStatusUpdater interface {
	// UpdateKeyStatusForGracePeriod disables a key version.
	// This method is called by the grace period handler.
	UpdateKeyStatusForGracePeriod(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error)
}

// KMSGracePeriodPayload contains the data needed to disable a key version.
type KMSGracePeriodPayload struct {
	KeyID   string `json:"key_id"`
	Version uint64 `json:"version"`
}

// KMSGracePeriodHandler handles grace period key disabling.
type KMSGracePeriodHandler struct {
	kmsUpdater KMSKeyStatusUpdater
}

// NewKMSGracePeriodHandler creates a new grace period handler.
func NewKMSGracePeriodHandler(kmsUpdater KMSKeyStatusUpdater) *KMSGracePeriodHandler {
	return &KMSGracePeriodHandler{
		kmsUpdater: kmsUpdater,
	}
}

// Name returns the handler name for registration.
func (h *KMSGracePeriodHandler) Name() string {
	return HandlerKMSGracePeriod
}

// Handle executes the grace period job by disabling the old key version.
func (h *KMSGracePeriodHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	var payload KMSGracePeriodPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload",
			Message: fmt.Sprintf("failed to parse grace period payload: %v", err),
		}
	}

	if payload.KeyID == "" {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload.key_id",
			Message: "key_id is required",
		}
	}
	if payload.Version == 0 {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload.version",
			Message: "version is required and must be > 0",
		}
	}

	if !keyBelongsToTarget(job.TargetTenantID, payload.KeyID) {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload.key_id",
			Message: fmt.Sprintf("key %q is not owned by the job's target tenant %q", payload.KeyID, job.TargetTenantID),
		}
	}

	resp, err := h.kmsUpdater.UpdateKeyStatusForGracePeriod(ctx, payload.KeyID, payload.Version)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerKMSGracePeriod,
			JobID:   job.ID,
			Cause:   err,
		}
	}

	return &scheduler.JobResult{
		Success: true,
		Message: fmt.Sprintf("Disabled key %s version %d (previous status: %s)",
			resp.KeyID, payload.Version, resp.OldStatus),
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *KMSGracePeriodHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// keyBelongsToTarget reports whether keyID is a KMS key the target tenant
// owns. It follows the scoped-ID rule of pkg/service (tenantKeyBelongsTo):
// a tenant's keys are stored as "tenant.<id>.<key>", and a job targeting the
// system tenant (or no tenant) may name any key.
func keyBelongsToTarget(targetTenantID, keyID string) bool {
	if targetTenantID == "" || tenantctx.IsSystem(targetTenantID) {
		return true
	}
	return strings.HasPrefix(keyID, "tenant."+targetTenantID+".")
}

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
//
// AuditCleanupHandler provides scheduled cleanup of audit log entries based on
// configurable retention policies. It supports filtering by event type, tenant,
// and retention period while offering dry-run capability for validation.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// HandlerAuditCleanup is the handler name for audit log cleanup jobs.
const HandlerAuditCleanup = "audit.cleanup"

// Default configuration values for audit cleanup operations.
const (
	DefaultRetentionDays = 90
	DefaultBatchSize     = 1000
	MinRetentionDays     = 1
	MaxBatchSize         = 10000
)

// AuditCleanupOptions defines the configuration for an audit log cleanup operation.
type AuditCleanupOptions struct {
	// RetentionDays specifies the age threshold in days. Logs older than this
	// many days from the current time will be eligible for deletion.
	RetentionDays int `json:"retention_days"`

	// EventTypes optionally filters cleanup to specific event types.
	// If empty, all event types are included.
	EventTypes []string `json:"event_types,omitempty"`

	// TenantID optionally restricts cleanup to a specific tenant.
	// If empty, cleanup applies to all tenants (system-wide).
	TenantID string `json:"tenant_id,omitempty"`

	// DryRun when true causes the cleaner to report what would be deleted
	// without actually performing the deletion.
	DryRun bool `json:"dry_run"`

	// BatchSize controls the number of records to delete per batch.
	// This helps manage memory and transaction size for large cleanups.
	BatchSize int `json:"batch_size"`
}

// AuditCleanupResult contains the outcome of an audit log cleanup operation.
type AuditCleanupResult struct {
	// DeletedCount is the number of audit log entries that were deleted
	// (or would be deleted in dry-run mode).
	DeletedCount int64 `json:"deleted_count"`

	// ScannedCount is the total number of entries that were examined.
	ScannedCount int64 `json:"scanned_count"`

	// OldestDeleted is the timestamp of the oldest deleted entry.
	OldestDeleted time.Time `json:"oldest_deleted,omitempty"`

	// NewestDeleted is the timestamp of the newest deleted entry.
	NewestDeleted time.Time `json:"newest_deleted,omitempty"`

	// DryRun indicates whether this was a dry-run operation.
	DryRun bool `json:"dry_run"`
}

// AuditCleaner defines the interface for performing audit log cleanup operations.
// Implementations handle the actual deletion logic against the audit log storage.
type AuditCleaner interface {
	// CleanupAuditLogs deletes audit log entries matching the specified options.
	// Returns a result summarizing the cleanup operation or an error if the
	// cleanup could not be performed.
	CleanupAuditLogs(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error)
}

// AuditCleanupPayload is the JSON payload structure for audit cleanup jobs.
// This is serialized into the job's Payload field.
type AuditCleanupPayload struct {
	// RetentionDays specifies how many days of logs to retain.
	// Logs older than this value will be deleted.
	RetentionDays int `json:"retention_days"`

	// EventTypes optionally filters cleanup to specific event types.
	EventTypes []string `json:"event_types,omitempty"`

	// TenantID optionally restricts cleanup to a specific tenant.
	TenantID string `json:"tenant_id,omitempty"`

	// DryRun when true reports what would be deleted without deleting.
	DryRun bool `json:"dry_run"`

	// BatchSize controls records deleted per batch for memory management.
	BatchSize int `json:"batch_size,omitempty"`
}

// AuditCleanupHandler handles scheduled audit log cleanup jobs.
type AuditCleanupHandler struct {
	cleaner AuditCleaner
}

// NewAuditCleanupHandler creates a new audit cleanup handler with the given cleaner.
func NewAuditCleanupHandler(cleaner AuditCleaner) *AuditCleanupHandler {
	return &AuditCleanupHandler{
		cleaner: cleaner,
	}
}

// Name returns the handler name for registration.
func (h *AuditCleanupHandler) Name() string {
	return HandlerAuditCleanup
}

// Handle executes the audit cleanup job by parsing the payload and invoking the cleaner.
func (h *AuditCleanupHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	if h.cleaner == nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerAuditCleanup,
			JobID:   job.ID,
			Cause:   &CleanerNotConfiguredError{},
		}
	}

	var payload AuditCleanupPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload",
			Message: fmt.Sprintf("failed to parse audit cleanup payload: %v", err),
		}
	}

	if err := validatePayload(&payload); err != nil {
		return nil, err
	}

	opts := &AuditCleanupOptions{
		RetentionDays: payload.RetentionDays,
		EventTypes:    payload.EventTypes,
		TenantID:      payload.TenantID,
		DryRun:        payload.DryRun,
		BatchSize:     payload.BatchSize,
	}

	result, err := h.cleaner.CleanupAuditLogs(ctx, opts)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerAuditCleanup,
			JobID:   job.ID,
			Cause:   err,
		}
	}

	message := buildResultMessage(result, opts)

	return &scheduler.JobResult{
		Success: true,
		Message: message,
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *AuditCleanupHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// validatePayload validates the audit cleanup payload and applies defaults.
func validatePayload(payload *AuditCleanupPayload) error {
	if payload.RetentionDays == 0 {
		payload.RetentionDays = DefaultRetentionDays
	}

	if payload.RetentionDays < MinRetentionDays {
		return &scheduler.InvalidJobError{
			Field:   "payload.retention_days",
			Message: fmt.Sprintf("retention_days must be at least %d", MinRetentionDays),
		}
	}

	if payload.BatchSize == 0 {
		payload.BatchSize = DefaultBatchSize
	}

	if payload.BatchSize < 1 || payload.BatchSize > MaxBatchSize {
		return &scheduler.InvalidJobError{
			Field:   "payload.batch_size",
			Message: fmt.Sprintf("batch_size must be between 1 and %d", MaxBatchSize),
		}
	}

	return nil
}

// buildResultMessage constructs a human-readable message describing the cleanup result.
func buildResultMessage(result *AuditCleanupResult, opts *AuditCleanupOptions) string {
	prefix := "Deleted"
	if result.DryRun {
		prefix = "Would delete"
	}

	message := fmt.Sprintf("%s %d audit log entries (scanned %d) older than %d days",
		prefix, result.DeletedCount, result.ScannedCount, opts.RetentionDays)

	if opts.TenantID != "" {
		message += fmt.Sprintf(" for tenant %s", opts.TenantID)
	}

	if len(opts.EventTypes) > 0 {
		message += fmt.Sprintf(" (event types: %v)", opts.EventTypes)
	}

	if result.DeletedCount > 0 && !result.OldestDeleted.IsZero() && !result.NewestDeleted.IsZero() {
		message += fmt.Sprintf(" [range: %s to %s]",
			result.OldestDeleted.Format(time.RFC3339),
			result.NewestDeleted.Format(time.RFC3339))
	}

	return message
}

// CleanerNotConfiguredError indicates the audit cleaner was not provided.
type CleanerNotConfiguredError struct{}

func (e *CleanerNotConfiguredError) Error() string {
	return "audit cleaner not configured"
}

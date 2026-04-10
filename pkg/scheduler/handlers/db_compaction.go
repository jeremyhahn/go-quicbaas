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
// The db_compaction handler triggers database compaction operations to reclaim
// storage space and optimize database performance. Compaction consolidates
// fragmented data and removes obsolete entries, improving read performance
// and reducing disk usage.
//
// Usage example:
//
//	compactor := storage.NewCompactor(db)
//	handler := handlers.NewDBCompactionHandler(compactor)
//	registry.Register(handler)
//
//	// Schedule a compaction job
//	payload := handlers.DBCompactionPayload{
//	    Tables:    []string{"keys", "metadata"},
//	    Threshold: 0.3,
//	    Force:     false,
//	}
//	payloadBytes, _ := json.Marshal(payload)
//	job := &scheduler.Job{
//	    Handler: handlers.HandlerDBCompaction,
//	    Payload: payloadBytes,
//	}
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// HandlerDBCompaction is the handler name for database compaction jobs.
const HandlerDBCompaction = "db.compaction"

// CompactionOptions contains configuration for a compaction operation.
type CompactionOptions struct {
	// Tables specifies which tables to compact. Empty means all tables.
	Tables []string `json:"tables,omitempty"`

	// Threshold is the fragmentation threshold (0-1) above which compaction runs.
	// A value of 0.3 means compact if fragmentation exceeds 30%.
	Threshold float64 `json:"threshold"`

	// Force triggers compaction regardless of fragmentation threshold.
	Force bool `json:"force"`
}

// CompactionResult contains the outcome of a compaction operation.
type CompactionResult struct {
	// TablesCompacted is the number of tables that were compacted.
	TablesCompacted int `json:"tables_compacted"`

	// BytesReclaimed is the total storage space freed by compaction.
	BytesReclaimed int64 `json:"bytes_reclaimed"`

	// Duration is how long the compaction operation took.
	Duration time.Duration `json:"duration"`
}

// DBCompactor defines the interface for performing database compaction.
// This interface is implemented by storage engines that support compaction.
type DBCompactor interface {
	// Compact performs database compaction with the given options.
	// Returns the result of the compaction or an error if the operation fails.
	Compact(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error)
}

// DBCompactionPayload contains the data needed to configure a compaction job.
type DBCompactionPayload struct {
	// Tables specifies which tables to compact. Empty means all tables.
	Tables []string `json:"tables,omitempty"`

	// Threshold is the fragmentation threshold (0-1) above which compaction runs.
	// A value of 0.3 means compact if fragmentation exceeds 30%.
	// Must be between 0 and 1 inclusive.
	Threshold float64 `json:"threshold"`

	// Force triggers compaction regardless of fragmentation threshold.
	Force bool `json:"force"`
}

// DBCompactionHandler handles database compaction jobs.
type DBCompactionHandler struct {
	compactor DBCompactor
}

// NewDBCompactionHandler creates a new database compaction handler.
func NewDBCompactionHandler(compactor DBCompactor) *DBCompactionHandler {
	return &DBCompactionHandler{
		compactor: compactor,
	}
}

// Name returns the handler name for registration.
func (h *DBCompactionHandler) Name() string {
	return HandlerDBCompaction
}

// Handle executes the compaction job by invoking the compactor with parsed options.
func (h *DBCompactionHandler) Handle(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	var payload DBCompactionPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, &scheduler.InvalidJobError{
			Field:   "payload",
			Message: fmt.Sprintf("failed to parse compaction payload: %v", err),
		}
	}

	if err := validateCompactionPayload(&payload); err != nil {
		return nil, err
	}

	if h.compactor == nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerDBCompaction,
			JobID:   job.ID,
			Cause:   &CompactorNotConfiguredError{},
		}
	}

	opts := &CompactionOptions{
		Tables:    payload.Tables,
		Threshold: payload.Threshold,
		Force:     payload.Force,
	}

	result, err := h.compactor.Compact(ctx, opts)
	if err != nil {
		return nil, &scheduler.HandlerFailedError{
			Handler: HandlerDBCompaction,
			JobID:   job.ID,
			Cause:   err,
		}
	}

	return &scheduler.JobResult{
		Success: true,
		Message: formatCompactionMessage(result, &payload),
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *DBCompactionHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// validateCompactionPayload validates the compaction payload fields.
func validateCompactionPayload(payload *DBCompactionPayload) error {
	if payload.Threshold < 0 || payload.Threshold > 1 {
		return &scheduler.InvalidJobError{
			Field:   "payload.threshold",
			Message: "threshold must be between 0 and 1",
		}
	}
	return nil
}

// formatCompactionMessage creates a human-readable result message.
func formatCompactionMessage(result *CompactionResult, payload *DBCompactionPayload) string {
	tableDesc := "all tables"
	if len(payload.Tables) > 0 {
		tableDesc = fmt.Sprintf("%d specified table(s)", len(payload.Tables))
	}

	return fmt.Sprintf(
		"Compaction completed: %d table(s) compacted from %s, %d bytes reclaimed in %s",
		result.TablesCompacted,
		tableDesc,
		result.BytesReclaimed,
		result.Duration.Round(time.Millisecond),
	)
}

// CompactorNotConfiguredError indicates the handler was created without a compactor.
type CompactorNotConfiguredError struct{}

func (e *CompactorNotConfiguredError) Error() string {
	return "compactor not configured"
}

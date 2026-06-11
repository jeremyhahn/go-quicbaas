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

package iam

import (
	"context"
	"errors"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Batch operation errors.
var (
	// ErrBatchOperationNotFound is returned when a batch operation is not found.
	ErrBatchOperationNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.batch.operation",
		Err:  errors.New("batch operation not found"),
	}

	// ErrBatchOperationCancelled is returned when a batch operation is cancelled.
	ErrBatchOperationCancelled = &dberrors.QBError{
		Code: dberrors.ErrCanceled,
		Op:   "iam.batch.operation",
		Err:  errors.New("batch operation cancelled"),
	}
)

// BatchStatus represents the status of a batch operation.
type BatchStatus string

const (
	// BatchStatusPending indicates the operation is pending.
	BatchStatusPending BatchStatus = "pending"

	// BatchStatusValidating indicates the operation is being validated.
	BatchStatusValidating BatchStatus = "validating"

	// BatchStatusProcessing indicates the operation is being processed.
	BatchStatusProcessing BatchStatus = "processing"

	// BatchStatusCompleted indicates the operation completed successfully.
	BatchStatusCompleted BatchStatus = "completed"

	// BatchStatusFailed indicates the operation failed.
	BatchStatusFailed BatchStatus = "failed"

	// BatchStatusCancelled indicates the operation was cancelled.
	BatchStatusCancelled BatchStatus = "cancelled"

	// BatchStatusPartialSuccess indicates some items succeeded and some failed.
	BatchStatusPartialSuccess BatchStatus = "partial_success"
)

// BatchOperationType represents the type of batch operation.
type BatchOperationType string

const (
	// BatchOperationTypeUsers indicates a user batch operation.
	BatchOperationTypeUsers BatchOperationType = "users"

	// BatchOperationTypeRoles indicates a role batch operation.
	BatchOperationTypeRoles BatchOperationType = "roles"

	// BatchOperationTypeGroups indicates a group batch operation.
	BatchOperationTypeGroups BatchOperationType = "groups"

	// BatchOperationTypePolicies indicates a policy batch operation.
	BatchOperationTypePolicies BatchOperationType = "policies"
)

// BatchOperationProgress tracks progress of a batch operation.
type BatchOperationProgress struct {
	// Total is the total number of items.
	Total int `json:"total" yaml:"total"`

	// Processed is the number of items processed.
	Processed int `json:"processed" yaml:"processed"`

	// Succeeded is the number of items that succeeded.
	Succeeded int `json:"succeeded" yaml:"succeeded"`

	// Successful is an alias for Succeeded for compatibility.
	Successful int `json:"successful" yaml:"successful"`

	// Failed is the number of items that failed.
	Failed int `json:"failed" yaml:"failed"`

	// Skipped is the number of items that were skipped.
	Skipped int `json:"skipped" yaml:"skipped"`
}

// BatchOperation represents a batch import/export operation.
type BatchOperation struct {
	// ID is the unique identifier for this operation.
	ID string `json:"id" yaml:"id"`

	// TenantID is the tenant this operation belongs to.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Type is the type of batch operation.
	Type BatchOperationType `json:"type" yaml:"type"`

	// Status is the current status of the operation.
	Status BatchStatus `json:"status" yaml:"status"`

	// Progress tracks the operation progress.
	Progress BatchOperationProgress `json:"progress" yaml:"progress"`

	// TotalItems is the total number of items to process.
	TotalItems int `json:"total_items" yaml:"total_items"`

	// ProcessedItems is the number of items processed so far.
	ProcessedItems int `json:"processed_items" yaml:"processed_items"`

	// SuccessCount is the number of items processed successfully.
	SuccessCount int `json:"success_count" yaml:"success_count"`

	// FailureCount is the number of items that failed to process.
	FailureCount int `json:"failure_count" yaml:"failure_count"`

	// Errors contains the errors that occurred during processing.
	Errors []BatchError `json:"errors,omitempty" yaml:"errors,omitempty"`

	// Options contains the options used for this operation.
	Options BatchOptions `json:"options" yaml:"options"`

	// CreatedAt is when the operation was created.
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`

	// UpdatedAt is when the operation was last updated.
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"`

	// CompletedAt is when the operation completed.
	CompletedAt *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`

	// CancelledAt is when the operation was cancelled.
	CancelledAt *time.Time `json:"cancelled_at,omitempty" yaml:"cancelled_at,omitempty"`
}

// Validate validates the batch operation.
func (op *BatchOperation) Validate() error {
	if op.ID == "" {
		return &ValidationError{Field: "id", Message: "operation ID is required"}
	}
	if op.TenantID == "" {
		return &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if op.Type == "" {
		return &ValidationError{Field: "type", Message: "operation type is required"}
	}
	return nil
}

// IsTerminal returns true if the operation is in a terminal state.
func (op *BatchOperation) IsTerminal() bool {
	switch op.Status {
	case BatchStatusCompleted, BatchStatusFailed, BatchStatusCancelled, BatchStatusPartialSuccess:
		return true
	default:
		return false
	}
}

// CanCancel returns true if the operation can be cancelled.
func (op *BatchOperation) CanCancel() bool {
	switch op.Status {
	case BatchStatusPending, BatchStatusValidating, BatchStatusProcessing:
		return true
	default:
		return false
	}
}

// BatchOptions configures batch operation behavior.
type BatchOptions struct {
	// ContinueOnError continues processing even if some items fail.
	ContinueOnError bool `json:"continue_on_error" yaml:"continue_on_error"`

	// UpdateExisting updates existing items instead of skipping.
	UpdateExisting bool `json:"update_existing" yaml:"update_existing"`

	// DryRun validates without making changes.
	DryRun bool `json:"dry_run" yaml:"dry_run"`

	// ValidateOnly only validates items without importing.
	ValidateOnly bool `json:"validate_only" yaml:"validate_only"`

	// RateLimit is the number of items to process per second.
	RateLimit int `json:"rate_limit" yaml:"rate_limit"`

	// Timeout is the overall timeout for the batch operation.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// BatchSize is the number of items to process per batch.
	BatchSize int `json:"batch_size" yaml:"batch_size"`

	// Parallelism is the number of parallel workers.
	Parallelism int `json:"parallelism" yaml:"parallelism"`

	// Atomic ensures all items succeed or all fail.
	Atomic bool `json:"atomic" yaml:"atomic"`

	// SkipDuplicates skips items that already exist.
	SkipDuplicates bool `json:"skip_duplicates" yaml:"skip_duplicates"`
}

// Validate validates the batch options.
func (o *BatchOptions) Validate() error {
	if o.BatchSize < 0 {
		return &ValidationError{Field: "batch_size", Message: "batch size cannot be negative"}
	}
	if o.Parallelism < 0 {
		return &ValidationError{Field: "parallelism", Message: "parallelism cannot be negative"}
	}
	if o.RateLimit < 0 {
		return &ValidationError{Field: "rate_limit", Message: "rate limit cannot be negative"}
	}
	return nil
}

// BatchStorage defines the storage interface for batch operations.
type BatchStorage interface {
	// SaveOperation saves a batch operation.
	SaveOperation(ctx context.Context, op *BatchOperation) error

	// GetOperation retrieves a batch operation by ID.
	GetOperation(ctx context.Context, operationID string) (*BatchOperation, error)

	// UpdateOperation updates a batch operation.
	UpdateOperation(ctx context.Context, op *BatchOperation) error

	// ListOperations lists batch operations for a tenant.
	ListOperations(ctx context.Context, tenantID string, status *BatchStatus, limit int) ([]*BatchOperation, error)

	// DeleteOperation deletes a batch operation.
	DeleteOperation(ctx context.Context, operationID string) error
}

// UserImportItem represents a user item for batch import.
type UserImportItem struct {
	// ID is the user ID.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`

	// Email is the user's email address.
	Email string `json:"email" yaml:"email"`

	// Name is the user's display name.
	Name string `json:"name" yaml:"name"`

	// Username is the user's username.
	Username string `json:"username,omitempty" yaml:"username,omitempty"`

	// Attributes contains additional user attributes.
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Roles is the list of roles to assign to the user.
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups is the list of groups to add the user to.
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Enabled indicates whether the user is enabled.
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// Validate validates the user import item.
func (u *UserImportItem) Validate() error {
	if u.Email == "" {
		return &ValidationError{Field: "email", Message: "email is required"}
	}
	return nil
}

// RoleImportItem represents a role item for batch import.
type RoleImportItem struct {
	// ID is the role ID.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`

	// Name is the role name.
	Name string `json:"name" yaml:"name"`

	// Description is the role description.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Permissions is the list of permissions to assign to the role.
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// Attributes contains additional role attributes.
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`
}

// Validate validates the role import item.
func (r *RoleImportItem) Validate() error {
	if r.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	return nil
}

// GroupImportItem represents a group item for batch import.
type GroupImportItem struct {
	// ID is the group ID.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`

	// Name is the group name.
	Name string `json:"name" yaml:"name"`

	// Description is the group description.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Members is the list of member IDs to add to the group.
	Members []string `json:"members,omitempty" yaml:"members,omitempty"`

	// Attributes contains additional group attributes.
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`
}

// Validate validates the group import item.
func (g *GroupImportItem) Validate() error {
	if g.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	return nil
}

// PolicyImportItem represents a policy item for batch import.
type PolicyImportItem struct {
	// Policy is the policy to import.
	Policy *Policy `json:"policy,omitempty" yaml:"policy,omitempty"`

	// Name is the policy name (alternative to Policy field).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Description is the policy description.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Priority is the policy priority.
	Priority int `json:"priority" yaml:"priority"`

	// Subjects defines who the policy applies to.
	Subjects SubjectSelector `json:"subjects,omitempty" yaml:"subjects,omitempty"`

	// Rules defines the policy rules.
	Rules []Rule `json:"rules,omitempty" yaml:"rules,omitempty"`

	// Variables contains policy variables.
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// Validate validates the policy import item.
func (p *PolicyImportItem) Validate() error {
	if p.Policy == nil && p.Name == "" {
		return &ValidationError{Field: "policy/name", Message: "policy or name is required"}
	}
	return nil
}

// BatchProgress represents the progress of a batch operation.
type BatchProgress struct {
	// Total is the total number of items.
	Total int `json:"total" yaml:"total"`

	// Processed is the number of items processed.
	Processed int `json:"processed" yaml:"processed"`

	// TotalItems is the total number of items (legacy field).
	TotalItems int `json:"total_items" yaml:"total_items"`

	// ProcessedItems is the number of items processed (legacy field).
	ProcessedItems int `json:"processed_items" yaml:"processed_items"`

	// SuccessCount is the number of successful items.
	SuccessCount int `json:"success_count" yaml:"success_count"`

	// FailureCount is the number of failed items.
	FailureCount int `json:"failure_count" yaml:"failure_count"`

	// PercentComplete is the percentage complete.
	PercentComplete float64 `json:"percent_complete" yaml:"percent_complete"`

	// CurrentType is the current item type being processed.
	CurrentType string `json:"current_type" yaml:"current_type"`

	// ItemsPerSecond is the processing rate.
	ItemsPerSecond float64 `json:"items_per_second" yaml:"items_per_second"`

	// EstimatedTimeRemaining is the estimated time to completion.
	EstimatedTimeRemaining time.Duration `json:"estimated_time_remaining" yaml:"estimated_time_remaining"`
}

// UpdateProgress recalculates the percentage based on current progress.
func (p *BatchProgress) UpdateProgress() {
	if p.Total > 0 {
		p.PercentComplete = float64(p.Processed) / float64(p.Total) * 100
	}
}

// CalculateRate calculates the processing rate based on elapsed time.
func (p *BatchProgress) CalculateRate(duration time.Duration) {
	if duration > 0 && p.Processed > 0 {
		p.ItemsPerSecond = float64(p.Processed) / duration.Seconds()
		remaining := p.Total - p.Processed
		if remaining > 0 && p.ItemsPerSecond > 0 {
			p.EstimatedTimeRemaining = time.Duration(float64(remaining)/p.ItemsPerSecond) * time.Second
		}
	}
}

// BatchResult represents the result of a batch operation.
type BatchResult struct {
	// OperationID is the operation ID.
	OperationID string `json:"operation_id" yaml:"operation_id"`

	// Status is the final status.
	Status BatchStatus `json:"status" yaml:"status"`

	// TotalItems is the total number of items.
	TotalItems int `json:"total_items" yaml:"total_items"`

	// ProcessedItems tracks processed items by type.
	ProcessedItems map[string][]string `json:"processed_items" yaml:"processed_items"`

	// SuccessCount is the number of successful items.
	SuccessCount int `json:"success_count" yaml:"success_count"`

	// FailureCount is the number of failed items.
	FailureCount int `json:"failure_count" yaml:"failure_count"`

	// SkippedCount is the number of skipped items.
	SkippedCount int `json:"skipped_count" yaml:"skipped_count"`

	// Errors contains the errors that occurred.
	Errors []BatchError `json:"errors,omitempty" yaml:"errors,omitempty"`

	// Duration is the operation duration.
	Duration time.Duration `json:"duration" yaml:"duration"`

	// DryRun indicates if this was a dry run.
	DryRun bool `json:"dry_run" yaml:"dry_run"`
}

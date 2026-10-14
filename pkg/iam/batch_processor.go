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
	"encoding/csv"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BatchResourceManager defines the interface for batch resource operations.
type BatchResourceManager interface {
	// User operations
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error

	// Role operations
	CreateRole(ctx context.Context, role *Role) error
	GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)
	UpdateRole(ctx context.Context, role *Role) error

	// Policy operations
	CreatePolicy(ctx context.Context, policy *Policy) error
	GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error)
	UpdatePolicy(ctx context.Context, policy *Policy) error
}

// IAMBatchProcessor handles batch import/export operations for IAM resources.
type IAMBatchProcessor struct {
	storage     BatchStorage
	tenantMgr   BatchResourceManager
	cancelFuncs sync.Map
}

// NewIAMBatchProcessor creates a new IAM batch processor.
func NewIAMBatchProcessor(storage BatchStorage, tenantMgr BatchResourceManager) *IAMBatchProcessor {
	return &IAMBatchProcessor{
		storage:   storage,
		tenantMgr: tenantMgr,
	}
}

// ImportUsers imports users in batch.
func (bp *IAMBatchProcessor) ImportUsers(ctx context.Context, tenantID string, items []UserImportItem, opts BatchOptions) (*BatchOperation, error) {
	op := bp.createOperation(tenantID, BatchOperationTypeUsers, len(items), opts)

	if err := bp.storage.SaveOperation(ctx, op); err != nil {
		return nil, err
	}

	// Start async processing. The import must outlive the request that
	// started it, so it detaches from the caller context deliberately.
	go bp.processUsers(context.Background(), op, items, opts) // #nosec G118 -- the work must outlive the caller; a request-scoped context would cancel it.

	return op, nil
}

// ImportRoles imports roles in batch.
func (bp *IAMBatchProcessor) ImportRoles(ctx context.Context, tenantID string, items []RoleImportItem, opts BatchOptions) (*BatchOperation, error) {
	op := bp.createOperation(tenantID, BatchOperationTypeRoles, len(items), opts)

	if err := bp.storage.SaveOperation(ctx, op); err != nil {
		return nil, err
	}

	// Start async processing. The import must outlive the request that
	// started it, so it detaches from the caller context deliberately.
	go bp.processRoles(context.Background(), op, items, opts) // #nosec G118 -- the work must outlive the caller; a request-scoped context would cancel it.

	return op, nil
}

// ImportPolicies imports policies in batch.
func (bp *IAMBatchProcessor) ImportPolicies(ctx context.Context, tenantID string, items []PolicyImportItem, opts BatchOptions) (*BatchOperation, error) {
	op := bp.createOperation(tenantID, BatchOperationTypePolicies, len(items), opts)

	if err := bp.storage.SaveOperation(ctx, op); err != nil {
		return nil, err
	}

	// Start async processing. The import must outlive the request that
	// started it, so it detaches from the caller context deliberately.
	go bp.processPolicies(context.Background(), op, items, opts) // #nosec G118 -- the work must outlive the caller; a request-scoped context would cancel it.

	return op, nil
}

// ImportFromJSON imports items from a JSON reader.
func (bp *IAMBatchProcessor) ImportFromJSON(ctx context.Context, tenantID string, opType BatchOperationType, reader io.Reader, opts BatchOptions) (*BatchOperation, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	switch opType {
	case BatchOperationTypeUsers:
		var items []UserImportItem
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		return bp.ImportUsers(ctx, tenantID, items, opts)

	case BatchOperationTypeRoles:
		var items []RoleImportItem
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		return bp.ImportRoles(ctx, tenantID, items, opts)

	case BatchOperationTypePolicies:
		var items []PolicyImportItem
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		return bp.ImportPolicies(ctx, tenantID, items, opts)

	default:
		return nil, &BatchUnsupportedOperationError{}
	}
}

// ImportFromCSV imports items from a CSV reader.
func (bp *IAMBatchProcessor) ImportFromCSV(ctx context.Context, tenantID string, opType BatchOperationType, reader io.Reader, opts BatchOptions) (*BatchOperation, error) {
	csvReader := csv.NewReader(reader)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, &BatchCSVHeaderError{}
	}

	headers := records[0]
	headerMap := make(map[string]int)
	for i, h := range headers {
		headerMap[h] = i
	}

	switch opType {
	case BatchOperationTypeUsers:
		items := make([]UserImportItem, 0, len(records)-1)
		for _, row := range records[1:] {
			item := UserImportItem{}
			if idx, ok := headerMap["id"]; ok && idx < len(row) {
				item.ID = row[idx]
			}
			if idx, ok := headerMap["email"]; ok && idx < len(row) {
				item.Email = row[idx]
			}
			if idx, ok := headerMap["name"]; ok && idx < len(row) {
				item.Name = row[idx]
			}
			items = append(items, item)
		}
		return bp.ImportUsers(ctx, tenantID, items, opts)

	case BatchOperationTypeRoles:
		items := make([]RoleImportItem, 0, len(records)-1)
		for _, row := range records[1:] {
			item := RoleImportItem{}
			if idx, ok := headerMap["id"]; ok && idx < len(row) {
				item.ID = row[idx]
			}
			if idx, ok := headerMap["name"]; ok && idx < len(row) {
				item.Name = row[idx]
			}
			if idx, ok := headerMap["description"]; ok && idx < len(row) {
				item.Description = row[idx]
			}
			items = append(items, item)
		}
		return bp.ImportRoles(ctx, tenantID, items, opts)

	case BatchOperationTypePolicies:
		return nil, &BatchCSVNotSupportedError{OperationType: "policies"}

	default:
		return nil, &BatchUnsupportedOperationError{}
	}
}

// GetOperation retrieves a batch operation by ID.
func (bp *IAMBatchProcessor) GetOperation(ctx context.Context, operationID string) (*BatchOperation, error) {
	return bp.storage.GetOperation(ctx, operationID)
}

// CancelOperation cancels a running batch operation.
func (bp *IAMBatchProcessor) CancelOperation(ctx context.Context, operationID string) error {
	op, err := bp.storage.GetOperation(ctx, operationID)
	if err != nil {
		return err
	}

	if !op.CanCancel() {
		return &BatchCannotCancelError{}
	}

	// Signal cancellation
	if entry, ok := bp.cancelFuncs.Load(operationID); ok {
		if cancelFunc, ok := entry.(context.CancelFunc); ok {
			cancelFunc()
		}
	}

	now := time.Now()
	op.Status = BatchStatusCancelled
	op.CancelledAt = &now
	op.UpdatedAt = now

	return bp.storage.UpdateOperation(ctx, op)
}

// ListOperations lists batch operations for a tenant.
func (bp *IAMBatchProcessor) ListOperations(ctx context.Context, tenantID string, status *BatchStatus, limit int) ([]*BatchOperation, error) {
	return bp.storage.ListOperations(ctx, tenantID, status, limit)
}

// createOperation creates a new batch operation.
func (bp *IAMBatchProcessor) createOperation(tenantID string, opType BatchOperationType, totalItems int, opts BatchOptions) *BatchOperation {
	now := time.Now()
	return &BatchOperation{
		ID:       uuid.New().String(),
		TenantID: tenantID,
		Type:     opType,
		Status:   BatchStatusPending,
		Progress: BatchOperationProgress{
			Total: totalItems,
		},
		TotalItems: totalItems,
		Options:    opts,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// processUsers processes user import items.
func (bp *IAMBatchProcessor) processUsers(ctx context.Context, op *BatchOperation, items []UserImportItem, opts BatchOptions) {
	ctx, cancel := context.WithCancel(ctx)
	bp.cancelFuncs.Store(op.ID, cancel)
	defer bp.cancelFuncs.Delete(op.ID)

	op.Status = BatchStatusProcessing
	_ = bp.storage.UpdateOperation(ctx, op)

	// For atomic operations, pre-validate all items first
	if opts.Atomic {
		for i, item := range items {
			if err := item.Validate(); err != nil {
				op.Progress.Failed++
				op.Progress.Processed++
				op.FailureCount++
				op.Errors = append(op.Errors, BatchError{
					Index: i,
					Type:  "user",
					ID:    item.Email,
					Error: err,
				})
				// Atomic mode fails on first error
				bp.finalizeOperation(ctx, op)
				return
			}
		}
	}

	for i, item := range items {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Rate limiting
		if opts.RateLimit > 0 {
			limiter := time.NewTimer(time.Second / time.Duration(opts.RateLimit))
			select {
			case <-limiter.C:
			case <-ctx.Done():
				limiter.Stop()
				return
			}
		}

		// Validate item
		if err := item.Validate(); err != nil {
			op.Progress.Failed++
			op.Progress.Processed++
			op.FailureCount++
			op.Errors = append(op.Errors, BatchError{
				Index: i,
				Type:  "user",
				ID:    item.Email,
				Error: err,
			})
			if !opts.ContinueOnError && !opts.ValidateOnly {
				break
			}
			continue
		}

		// Skip actual import if dry run or validate only
		if opts.DryRun || opts.ValidateOnly {
			op.Progress.Processed++
			op.Progress.Succeeded++
			op.Progress.Successful++
			op.ProcessedItems++
			op.SuccessCount++
			continue
		}

		// Check for existing user
		existingUser, _ := bp.tenantMgr.GetUserByEmail(ctx, op.TenantID, item.Email)

		// Handle skip duplicates
		if existingUser != nil && opts.SkipDuplicates {
			op.Progress.Skipped++
			op.Progress.Processed++
			continue
		}

		// Handle update existing
		if existingUser != nil && opts.UpdateExisting {
			existingUser.Name = item.Name
			existingUser.Roles = item.Roles
			if err := bp.tenantMgr.UpdateUser(ctx, existingUser); err != nil {
				op.Progress.Failed++
				op.Progress.Processed++
				op.FailureCount++
				op.Errors = append(op.Errors, BatchError{
					Index: i,
					Type:  "user",
					ID:    item.Email,
					Error: err,
				})
				if !opts.ContinueOnError {
					break
				}
				continue
			}
			op.Progress.Processed++
			op.Progress.Succeeded++
			op.Progress.Successful++
			op.ProcessedItems++
			op.SuccessCount++
			continue
		}

		// No id is set. The STORE assigns it — an id carried in an import item
		// names a record in whatever system produced the file, and minting a
		// UUID here would put a second identifier shape into a store whose ids
		// are Snowflakes, which nothing downstream can resolve.
		user := &User{
			Email:    item.Email,
			Name:     item.Name,
			TenantID: op.TenantID,
			Roles:    item.Roles,
		}

		if err := bp.tenantMgr.CreateUser(ctx, user); err != nil {
			op.Progress.Failed++
			op.Progress.Processed++
			op.FailureCount++
			op.Errors = append(op.Errors, BatchError{
				Index: i,
				Type:  "user",
				ID:    item.Email,
				Error: err,
			})
			if !opts.ContinueOnError {
				break
			}
			continue
		}

		op.Progress.Processed++
		op.Progress.Succeeded++
		op.Progress.Successful++
		op.ProcessedItems++
		op.SuccessCount++
	}

	bp.finalizeOperation(ctx, op)
}

// processRoles processes role import items.
func (bp *IAMBatchProcessor) processRoles(ctx context.Context, op *BatchOperation, items []RoleImportItem, opts BatchOptions) {
	ctx, cancel := context.WithCancel(ctx)
	bp.cancelFuncs.Store(op.ID, cancel)
	defer bp.cancelFuncs.Delete(op.ID)

	op.Status = BatchStatusProcessing
	_ = bp.storage.UpdateOperation(ctx, op)

	for i, item := range items {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if opts.RateLimit > 0 {
			limiter := time.NewTimer(time.Second / time.Duration(opts.RateLimit))
			select {
			case <-limiter.C:
			case <-ctx.Done():
				limiter.Stop()
				return
			}
		}

		if err := item.Validate(); err != nil {
			op.Progress.Failed++
			op.FailureCount++
			op.Errors = append(op.Errors, BatchError{
				Index: i,
				Type:  "role",
				ID:    item.Name,
				Error: err,
			})
			if !opts.ContinueOnError && !opts.ValidateOnly {
				break
			}
			continue
		}

		if !opts.DryRun && !opts.ValidateOnly {
			role := &Role{
				ID:          item.ID,
				Name:        item.Name,
				Description: item.Description,
				Permissions: item.Permissions,
				TenantID:    op.TenantID,
			}
			if role.ID == "" {
				role.ID = uuid.New().String()
			}

			if err := bp.tenantMgr.CreateRole(ctx, role); err != nil {
				op.Progress.Failed++
				op.FailureCount++
				op.Errors = append(op.Errors, BatchError{
					Index: i,
					Type:  "role",
					ID:    item.Name,
					Error: err,
				})
				if !opts.ContinueOnError {
					break
				}
				continue
			}
		}

		op.Progress.Processed++
		op.Progress.Succeeded++
		op.ProcessedItems++
		op.SuccessCount++
	}

	bp.finalizeOperation(ctx, op)
}

// processPolicies processes policy import items.
func (bp *IAMBatchProcessor) processPolicies(ctx context.Context, op *BatchOperation, items []PolicyImportItem, opts BatchOptions) {
	ctx, cancel := context.WithCancel(ctx)
	bp.cancelFuncs.Store(op.ID, cancel)
	defer bp.cancelFuncs.Delete(op.ID)

	op.Status = BatchStatusProcessing
	_ = bp.storage.UpdateOperation(ctx, op)

	for i, item := range items {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if opts.RateLimit > 0 {
			limiter := time.NewTimer(time.Second / time.Duration(opts.RateLimit))
			select {
			case <-limiter.C:
			case <-ctx.Done():
				limiter.Stop()
				return
			}
		}

		if err := item.Validate(); err != nil {
			op.Progress.Failed++
			op.FailureCount++
			op.Errors = append(op.Errors, BatchError{
				Index: i,
				Type:  "policy",
				ID:    item.Name,
				Error: err,
			})
			if !opts.ContinueOnError && !opts.ValidateOnly {
				break
			}
			continue
		}

		if !opts.DryRun && !opts.ValidateOnly {
			var policy *Policy
			if item.Policy != nil {
				policy = item.Policy
			} else {
				policy = &Policy{
					APIVersion: "v1",
					Kind:       "Policy",
					Metadata: PolicyMetadata{
						Name:        item.Name,
						Tenant:      op.TenantID,
						Description: item.Description,
						Priority:    item.Priority,
					},
					Spec: PolicySpec{
						Subjects:  item.Subjects,
						Variables: item.Variables,
						Rules:     item.Rules,
					},
				}
			}

			if err := bp.tenantMgr.CreatePolicy(ctx, policy); err != nil {
				op.Progress.Failed++
				op.FailureCount++
				op.Errors = append(op.Errors, BatchError{
					Index: i,
					Type:  "policy",
					ID:    item.Name,
					Error: err,
				})
				if !opts.ContinueOnError {
					break
				}
				continue
			}
		}

		op.Progress.Processed++
		op.Progress.Succeeded++
		op.ProcessedItems++
		op.SuccessCount++
	}

	bp.finalizeOperation(ctx, op)
}

// finalizeOperation finalizes a batch operation.
func (bp *IAMBatchProcessor) finalizeOperation(ctx context.Context, op *BatchOperation) {
	now := time.Now()
	op.CompletedAt = &now
	op.UpdatedAt = now

	switch {
	case op.FailureCount > 0 && op.SuccessCount > 0:
		op.Status = BatchStatusPartialSuccess
	case op.FailureCount > 0:
		op.Status = BatchStatusFailed
	default:
		op.Status = BatchStatusCompleted
	}

	_ = bp.storage.UpdateOperation(ctx, op)
}

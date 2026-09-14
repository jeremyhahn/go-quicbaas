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
	"sync"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ImportAll imports all IAM resources from a BatchImportRequest.
// It processes users, groups, roles, and policies in order to handle dependencies.
func (bm *BatchManager) ImportAll(ctx context.Context, req *BatchImportRequest) (*BatchImportResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	// Apply timeout if specified
	if req.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Options.Timeout)
		defer cancel()
	}

	// Calculate total items
	totalItems := len(req.Users) + len(req.Groups) + len(req.Roles) + len(req.Policies)

	result := &BatchImportResult{
		TotalItems:     totalItems,
		ProcessedItems: make(map[string][]string),
		DryRun:         req.Options.DryRun,
	}
	result.ProcessedItems["users"] = make([]string, 0)
	result.ProcessedItems["groups"] = make([]string, 0)
	result.ProcessedItems["roles"] = make([]string, 0)
	result.ProcessedItems["policies"] = make([]string, 0)

	// Create progress tracker
	progressTracker := newProgressTracker(totalItems, req.Options.ProgressFunc)

	// Import in order: roles first (no dependencies), then groups, then users, then policies
	// This order helps resolve dependencies correctly

	// 1. Import Roles
	if len(req.Roles) > 0 {
		progressTracker.setCurrentType("role")
		roleResult, err := bm.ImportRoles(ctx, req.TenantID, req.Roles, req.Options)
		if err != nil && !req.Options.ContinueOnError {
			result.Duration = time.Since(startTime)
			return result, err
		}
		bm.mergeResult(result, roleResult, "role")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		result.Duration = time.Since(startTime)
		return result, ErrBatchCanceled
	}

	// 2. Import Groups
	if len(req.Groups) > 0 {
		progressTracker.setCurrentType("group")
		groupResult, err := bm.ImportGroups(ctx, req.TenantID, req.Groups, req.Options)
		if err != nil && !req.Options.ContinueOnError {
			result.Duration = time.Since(startTime)
			return result, err
		}
		bm.mergeResult(result, groupResult, "group")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		result.Duration = time.Since(startTime)
		return result, ErrBatchCanceled
	}

	// 3. Import Users
	if len(req.Users) > 0 {
		progressTracker.setCurrentType("user")
		userResult, err := bm.ImportUsers(ctx, req.TenantID, req.Users, req.Options)
		if err != nil && !req.Options.ContinueOnError {
			result.Duration = time.Since(startTime)
			return result, err
		}
		bm.mergeResult(result, userResult, "user")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		result.Duration = time.Since(startTime)
		return result, ErrBatchCanceled
	}

	// 4. Import Policies
	if len(req.Policies) > 0 {
		progressTracker.setCurrentType("policy")
		policyResult, err := bm.ImportPolicies(ctx, req.TenantID, req.Policies, req.Options)
		if err != nil && !req.Options.ContinueOnError {
			result.Duration = time.Since(startTime)
			return result, err
		}
		bm.mergeResult(result, policyResult, "policy")
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// mergeResult merges a sub-result into the main result.
func (bm *BatchManager) mergeResult(result *BatchImportResult, subResult *BatchImportResult, itemType string) {
	if subResult == nil {
		return
	}
	result.SuccessCount += subResult.SuccessCount
	result.FailureCount += subResult.FailureCount
	result.SkippedCount += subResult.SkippedCount
	result.Errors = append(result.Errors, subResult.Errors...)
	if ids, ok := subResult.ProcessedItems[itemType+"s"]; ok {
		result.ProcessedItems[itemType+"s"] = append(result.ProcessedItems[itemType+"s"], ids...)
	}
}

// ImportUsers imports users in bulk.
func (bm *BatchManager) ImportUsers(ctx context.Context, tenantID string, users []*UserImport, opts BatchImportOptions) (*BatchImportResult, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if len(users) == 0 {
		return &BatchImportResult{ProcessedItems: map[string][]string{"users": {}}}, nil
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	batchSize := bm.getBatchSize(opts)
	parallelism := bm.getParallelism(opts)

	result := &BatchImportResult{
		TotalItems:     len(users),
		ProcessedItems: map[string][]string{"users": make([]string, 0, len(users))},
		DryRun:         opts.DryRun,
	}

	// Create worker pool
	wp := newWorkerPool(ctx, parallelism)
	wp.start()
	defer wp.stop()

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process users in batches
	for i := 0; i < len(users); i += batchSize {
		end := i + batchSize
		if end > len(users) {
			end = len(users)
		}
		batch := users[i:end]

		for j, user := range batch {
			idx := i + j
			u := user // Capture for closure

			wg.Add(1)
			submitted := wp.submit(workerJob{
				index:    idx,
				itemType: "user",
				itemID:   u.ID,
				fn: func(jobCtx context.Context) error {
					defer wg.Done()
					return bm.importUser(jobCtx, tenantID, u, opts)
				},
			})

			if !submitted {
				wg.Done()
				mu.Lock()
				result.Errors = append(result.Errors, BatchError{
					Index: idx,
					Type:  "user",
					ID:    u.ID,
					Error: ctx.Err(),
				})
				result.FailureCount++
				mu.Unlock()
			}
		}
	}

	// Collect results
	go func() {
		wg.Wait()
	}()

	// Process results from worker pool
	resultCount := 0
	for resultCount < len(users) {
		select {
		case <-ctx.Done():
			mu.Lock()
			result.Duration = time.Since(startTime)
			mu.Unlock()
			return result, ErrBatchCanceled
		case res, ok := <-wp.resultChan:
			if !ok {
				mu.Lock()
				result.Duration = time.Since(startTime)
				mu.Unlock()
				return result, nil
			}
			resultCount++
			mu.Lock()
			if res.err != nil {
				result.Errors = append(result.Errors, BatchError{
					Index: res.index,
					Type:  res.itemType,
					ID:    res.itemID,
					Error: res.err,
				})
				if dberrors.Is(res.err, dberrors.ErrAlreadyExists) && !opts.UpdateExisting {
					result.SkippedCount++
				} else {
					result.FailureCount++
				}
			} else {
				result.SuccessCount++
				result.ProcessedItems["users"] = append(result.ProcessedItems["users"], res.itemID)
			}
			mu.Unlock()
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// importUser imports a single user.
func (bm *BatchManager) importUser(ctx context.Context, tenantID string, user *UserImport, opts BatchImportOptions) error {
	if bm.identityStore == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.importUser",
			Err:  errBatchOperationFailed,
		}
	}

	// Validate user
	if user.Email == "" {
		return &ValidationError{Field: "email", Message: "user email is required"}
	}
	if user.Username == "" {
		return &ValidationError{Field: "username", Message: "user username is required"}
	}

	// Dry run - just validate
	if opts.DryRun {
		return nil
	}

	// Check if user exists
	existing, err := bm.identityStore.GetUserByEmail(ctx, tenantID, user.Email)
	if err == nil && existing != nil {
		if opts.UpdateExisting {
			// Update existing user
			updateOpts := UpdateUserOptions{}
			if user.Name != "" {
				updateOpts.Name = &user.Name
			}
			// The handle round-trips too. It is a field of its own now, so an
			// import that did not carry it back left the account answering to
			// whatever username it already had while the export said otherwise.
			if user.Username != "" {
				updateOpts.Username = &user.Username
			}
			if user.Attributes != nil {
				updateOpts.Attributes = &user.Attributes
			}
			if user.Roles != nil {
				updateOpts.Roles = &user.Roles
			}
			if user.Groups != nil {
				updateOpts.Groups = &user.Groups
			}
			// Only when the import states it. This used to be
			// `&user.Enabled` over a plain bool, which is never nil — so an
			// import record that said nothing about Enabled carried an
			// explicit false and SUSPENDED the existing user it was meant to
			// refresh.
			updateOpts.Enabled = user.Enabled
			// An exact state wins over the Enabled shorthand here for the same
			// reason it does on create: Enabled is one bool over six states, so
			// letting it overrule a named lifecycle turns a re-imported PENDING
			// account into a disabled one.
			if user.Lifecycle != "" {
				state := user.Lifecycle
				updateOpts.Lifecycle = &state
			}

			_, err = bm.identityStore.UpdateUser(ctx, tenantID, existing.ID, updateOpts)
			return err
		}
		return ErrEmailAlreadyExists
	}

	// Create new user
	// The import's own ID is deliberately NOT forwarded. The identity is the
	// Snowflake qrdb assigns, so an id carried in an import file names a record
	// in whatever system produced it, not one here; honouring it would be
	// inventing a second identifier space for exactly as long as it took
	// somebody to import a file twice.
	createOpts := CreateUserOptions{
		Email:      user.Email,
		Username:   user.Username,
		Name:       user.Name,
		Attributes: user.Attributes,
		// Forwarded, now that UserImport.Enabled is a pointer and can express
		// "the import did not say". While it was a plain bool this could not
		// be forwarded safely — false was indistinguishable from unset, so
		// honouring it would have suspended every user an import did not
		// explicitly enable. Dropping it was not safe either, only
		// differently unsafe: an export of a disabled account re-imported as
		// enabled, which is the fail-open direction.
		Enabled: user.Enabled,
	}
	// An explicit state wins over the Enabled shorthand, which cannot express
	// the difference the state carries.
	//
	// Only pending, active and disabled are creatable: suspended and locked
	// describe something that HAPPENED to an account that already existed, and
	// the transition table can only reach them FROM active. An import therefore
	// cannot reconstruct either one — the only route runs through a state in
	// which the account can authenticate, which for a record the import
	// explicitly said was suspended is precisely the window that must not exist.
	//
	// So they land on DISABLED: also non-authenticating, reachable directly, and
	// honest about what an import can assert. The original state is still in the
	// export file for anyone reconciling by hand.
	switch user.Lifecycle {
	case "":
	case LifecyclePending, LifecycleActive, LifecycleDisabled:
		createOpts.Lifecycle = user.Lifecycle
	default:
		createOpts.Lifecycle = LifecycleDisabled
	}

	createdUser, err := bm.identityStore.CreateUser(ctx, tenantID, createOpts)
	if err != nil {
		return err
	}

	// Assign roles if specified
	for _, roleID := range user.Roles {
		if err := bm.identityStore.AssignRole(ctx, tenantID, createdUser.ID, roleID); err != nil {
			// Log but don't fail - role might not exist yet
			if !dberrors.IsNotFound(err) {
				return err
			}
		}
	}

	// Add to groups if specified
	for _, groupID := range user.Groups {
		if err := bm.identityStore.AddUserToGroup(ctx, tenantID, groupID, createdUser.ID); err != nil {
			// Log but don't fail - group might not exist yet
			if !dberrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// ImportGroups imports groups in bulk.
func (bm *BatchManager) ImportGroups(ctx context.Context, tenantID string, groups []*GroupImport, opts BatchImportOptions) (*BatchImportResult, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if len(groups) == 0 {
		return &BatchImportResult{ProcessedItems: map[string][]string{"groups": {}}}, nil
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	batchSize := bm.getBatchSize(opts)
	parallelism := bm.getParallelism(opts)

	result := &BatchImportResult{
		TotalItems:     len(groups),
		ProcessedItems: map[string][]string{"groups": make([]string, 0, len(groups))},
		DryRun:         opts.DryRun,
	}

	// Create worker pool
	wp := newWorkerPool(ctx, parallelism)
	wp.start()
	defer wp.stop()

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process groups in batches
	for i := 0; i < len(groups); i += batchSize {
		end := i + batchSize
		if end > len(groups) {
			end = len(groups)
		}
		batch := groups[i:end]

		for j, group := range batch {
			idx := i + j
			g := group // Capture for closure

			wg.Add(1)
			submitted := wp.submit(workerJob{
				index:    idx,
				itemType: "group",
				itemID:   g.ID,
				fn: func(jobCtx context.Context) error {
					defer wg.Done()
					return bm.importGroup(jobCtx, tenantID, g, opts)
				},
			})

			if !submitted {
				wg.Done()
				mu.Lock()
				result.Errors = append(result.Errors, BatchError{
					Index: idx,
					Type:  "group",
					ID:    g.ID,
					Error: ctx.Err(),
				})
				result.FailureCount++
				mu.Unlock()
			}
		}
	}

	// Collect results
	go func() {
		wg.Wait()
	}()

	// Process results from worker pool
	resultCount := 0
	for resultCount < len(groups) {
		select {
		case <-ctx.Done():
			mu.Lock()
			result.Duration = time.Since(startTime)
			mu.Unlock()
			return result, ErrBatchCanceled
		case res, ok := <-wp.resultChan:
			if !ok {
				mu.Lock()
				result.Duration = time.Since(startTime)
				mu.Unlock()
				return result, nil
			}
			resultCount++
			mu.Lock()
			if res.err != nil {
				result.Errors = append(result.Errors, BatchError{
					Index: res.index,
					Type:  res.itemType,
					ID:    res.itemID,
					Error: res.err,
				})
				if dberrors.Is(res.err, dberrors.ErrAlreadyExists) && !opts.UpdateExisting {
					result.SkippedCount++
				} else {
					result.FailureCount++
				}
			} else {
				result.SuccessCount++
				result.ProcessedItems["groups"] = append(result.ProcessedItems["groups"], res.itemID)
			}
			mu.Unlock()
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// importGroup imports a single group.
func (bm *BatchManager) importGroup(ctx context.Context, tenantID string, group *GroupImport, opts BatchImportOptions) error {
	if bm.identityStore == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.importGroup",
			Err:  errBatchOperationFailed,
		}
	}

	// Validate group
	if group.Name == "" {
		return &ValidationError{Field: "name", Message: "group name is required"}
	}

	// Dry run - just validate
	if opts.DryRun {
		return nil
	}

	// Check if group exists by ID
	if group.ID != "" {
		existing, err := bm.identityStore.GetGroup(ctx, tenantID, group.ID)
		if err == nil && existing != nil {
			if opts.UpdateExisting {
				// Update existing group
				updateOpts := UpdateGroupOptions{}
				updateOpts.Name = &group.Name
				if group.Description != "" {
					updateOpts.Description = &group.Description
				}
				if group.Members != nil {
					updateOpts.Members = &group.Members
				}

				_, err = bm.identityStore.UpdateGroup(ctx, tenantID, existing.ID, updateOpts)
				return err
			}
			return ErrGroupAlreadyExists
		}
	}

	// Create new group
	createOpts := CreateGroupOptions{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		Attributes:  group.Attributes,
	}

	createdGroup, err := bm.identityStore.CreateGroup(ctx, tenantID, createOpts)
	if err != nil {
		return err
	}

	// Add members if specified
	for _, memberID := range group.Members {
		if err := bm.identityStore.AddUserToGroup(ctx, tenantID, createdGroup.ID, memberID); err != nil {
			// Log but don't fail - user might not exist yet
			if !dberrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// ImportRoles imports roles in bulk.
func (bm *BatchManager) ImportRoles(ctx context.Context, tenantID string, roles []*RoleImport, opts BatchImportOptions) (*BatchImportResult, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if len(roles) == 0 {
		return &BatchImportResult{ProcessedItems: map[string][]string{"roles": {}}}, nil
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	batchSize := bm.getBatchSize(opts)
	parallelism := bm.getParallelism(opts)

	result := &BatchImportResult{
		TotalItems:     len(roles),
		ProcessedItems: map[string][]string{"roles": make([]string, 0, len(roles))},
		DryRun:         opts.DryRun,
	}

	// Create worker pool
	wp := newWorkerPool(ctx, parallelism)
	wp.start()
	defer wp.stop()

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process roles in batches
	for i := 0; i < len(roles); i += batchSize {
		end := i + batchSize
		if end > len(roles) {
			end = len(roles)
		}
		batch := roles[i:end]

		for j, role := range batch {
			idx := i + j
			r := role // Capture for closure

			wg.Add(1)
			submitted := wp.submit(workerJob{
				index:    idx,
				itemType: "role",
				itemID:   r.ID,
				fn: func(jobCtx context.Context) error {
					defer wg.Done()
					return bm.importRole(jobCtx, tenantID, r, opts)
				},
			})

			if !submitted {
				wg.Done()
				mu.Lock()
				result.Errors = append(result.Errors, BatchError{
					Index: idx,
					Type:  "role",
					ID:    r.ID,
					Error: ctx.Err(),
				})
				result.FailureCount++
				mu.Unlock()
			}
		}
	}

	// Collect results
	go func() {
		wg.Wait()
	}()

	// Process results from worker pool
	resultCount := 0
	for resultCount < len(roles) {
		select {
		case <-ctx.Done():
			mu.Lock()
			result.Duration = time.Since(startTime)
			mu.Unlock()
			return result, ErrBatchCanceled
		case res, ok := <-wp.resultChan:
			if !ok {
				mu.Lock()
				result.Duration = time.Since(startTime)
				mu.Unlock()
				return result, nil
			}
			resultCount++
			mu.Lock()
			if res.err != nil {
				result.Errors = append(result.Errors, BatchError{
					Index: res.index,
					Type:  res.itemType,
					ID:    res.itemID,
					Error: res.err,
				})
				if dberrors.Is(res.err, dberrors.ErrAlreadyExists) && !opts.UpdateExisting {
					result.SkippedCount++
				} else {
					result.FailureCount++
				}
			} else {
				result.SuccessCount++
				result.ProcessedItems["roles"] = append(result.ProcessedItems["roles"], res.itemID)
			}
			mu.Unlock()
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// importRole imports a single role.
func (bm *BatchManager) importRole(ctx context.Context, tenantID string, role *RoleImport, opts BatchImportOptions) error {
	if bm.identityStore == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.importRole",
			Err:  errBatchOperationFailed,
		}
	}

	// Validate role
	if role.Name == "" {
		return &ValidationError{Field: "name", Message: "role name is required"}
	}

	// Dry run - just validate
	if opts.DryRun {
		return nil
	}

	// Check if role exists by ID
	if role.ID != "" {
		existing, err := bm.identityStore.GetRole(ctx, tenantID, role.ID)
		if err == nil && existing != nil {
			if opts.UpdateExisting {
				// Update existing role
				updateOpts := UpdateRoleOptions{}
				updateOpts.Name = &role.Name
				if role.Description != "" {
					updateOpts.Description = &role.Description
				}
				if role.Permissions != nil {
					updateOpts.Permissions = &role.Permissions
				}

				_, err = bm.identityStore.UpdateRole(ctx, tenantID, existing.ID, updateOpts)
				return err
			}
			return ErrRoleAlreadyExists
		}
	}

	// Create new role
	createOpts := CreateRoleOptions{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		Permissions: role.Permissions,
		Attributes:  role.Attributes,
	}

	_, err := bm.identityStore.CreateRole(ctx, tenantID, createOpts)
	return err
}

// ImportPolicies imports policies in bulk.
func (bm *BatchManager) ImportPolicies(ctx context.Context, tenantID string, policies []*PolicyImport, opts BatchImportOptions) (*BatchImportResult, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if len(policies) == 0 {
		return &BatchImportResult{ProcessedItems: map[string][]string{"policies": {}}}, nil
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	batchSize := bm.getBatchSize(opts)
	parallelism := bm.getParallelism(opts)

	result := &BatchImportResult{
		TotalItems:     len(policies),
		ProcessedItems: map[string][]string{"policies": make([]string, 0, len(policies))},
		DryRun:         opts.DryRun,
	}

	// Create worker pool
	wp := newWorkerPool(ctx, parallelism)
	wp.start()
	defer wp.stop()

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process policies in batches
	for i := 0; i < len(policies); i += batchSize {
		end := i + batchSize
		if end > len(policies) {
			end = len(policies)
		}
		batch := policies[i:end]

		for j, policy := range batch {
			idx := i + j
			p := policy // Capture for closure

			wg.Add(1)
			submitted := wp.submit(workerJob{
				index:    idx,
				itemType: "policy",
				itemID:   p.Name,
				fn: func(jobCtx context.Context) error {
					defer wg.Done()
					return bm.importPolicy(jobCtx, tenantID, p, opts)
				},
			})

			if !submitted {
				wg.Done()
				mu.Lock()
				result.Errors = append(result.Errors, BatchError{
					Index: idx,
					Type:  "policy",
					ID:    p.Name,
					Error: ctx.Err(),
				})
				result.FailureCount++
				mu.Unlock()
			}
		}
	}

	// Collect results
	go func() {
		wg.Wait()
	}()

	// Process results from worker pool
	resultCount := 0
	for resultCount < len(policies) {
		select {
		case <-ctx.Done():
			mu.Lock()
			result.Duration = time.Since(startTime)
			mu.Unlock()
			return result, ErrBatchCanceled
		case res, ok := <-wp.resultChan:
			if !ok {
				mu.Lock()
				result.Duration = time.Since(startTime)
				mu.Unlock()
				return result, nil
			}
			resultCount++
			mu.Lock()
			if res.err != nil {
				result.Errors = append(result.Errors, BatchError{
					Index: res.index,
					Type:  res.itemType,
					ID:    res.itemID,
					Error: res.err,
				})
				if dberrors.Is(res.err, dberrors.ErrAlreadyExists) && !opts.UpdateExisting {
					result.SkippedCount++
				} else {
					result.FailureCount++
				}
			} else {
				result.SuccessCount++
				result.ProcessedItems["policies"] = append(result.ProcessedItems["policies"], res.itemID)
			}
			mu.Unlock()
		}
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// importPolicy imports a single policy.
func (bm *BatchManager) importPolicy(ctx context.Context, tenantID string, policyImport *PolicyImport, opts BatchImportOptions) error {
	if bm.policyEngine == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.importPolicy",
			Err:  errBatchOperationFailed,
		}
	}

	// Validate policy
	if policyImport.Name == "" {
		return &ValidationError{Field: "name", Message: "policy name is required"}
	}
	if len(policyImport.Rules) == 0 {
		return &ValidationError{Field: "rules", Message: "policy must have at least one rule"}
	}

	// Convert PolicyImport to Policy
	policy := &Policy{
		APIVersion: "iam/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        policyImport.Name,
			Tenant:      tenantID,
			Description: policyImport.Description,
			Priority:    policyImport.Priority,
			Created:     time.Now().UTC(),
			Updated:     time.Now().UTC(),
		},
		Spec: PolicySpec{
			Subjects:  policyImport.Subjects,
			Variables: policyImport.Variables,
			Rules:     policyImport.Rules,
		},
	}

	// Validate the policy
	if err := policy.Validate(); err != nil {
		return err
	}

	// Dry run - just validate
	if opts.DryRun {
		return nil
	}

	// Check if policy exists
	existing, err := bm.policyEngine.GetPolicy(ctx, tenantID, policyImport.Name)
	if err == nil && existing != nil {
		if opts.UpdateExisting {
			return bm.policyEngine.UpdatePolicy(ctx, tenantID, policy)
		}
		return &PolicyExistsError{TenantID: tenantID, Name: policyImport.Name}
	}

	// Create new policy
	return bm.policyEngine.CreatePolicy(ctx, tenantID, policy)
}

// getBatchSize returns the batch size to use.
func (bm *BatchManager) getBatchSize(opts BatchImportOptions) int {
	if opts.BatchSize > 0 {
		return opts.BatchSize
	}
	return bm.batchSize
}

// getParallelism returns the parallelism to use.
func (bm *BatchManager) getParallelism(opts BatchImportOptions) int {
	if opts.Parallelism > 0 {
		return opts.Parallelism
	}
	return bm.parallelism
}

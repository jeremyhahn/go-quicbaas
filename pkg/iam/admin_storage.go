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

// MemoryAdminStorage implements AdminStorage interface using in-memory maps.
// This implementation is thread-safe and suitable for testing and single-node deployments.
type MemoryAdminStorage struct {
	mu          sync.RWMutex
	assignments map[string]*AdminAssignment // key: assignmentID
}

// NewMemoryAdminStorage creates a new in-memory admin storage instance.
func NewMemoryAdminStorage() *MemoryAdminStorage {
	return &MemoryAdminStorage{
		assignments: make(map[string]*AdminAssignment),
	}
}

// CreateAssignment stores a new admin assignment.
func (m *MemoryAdminStorage) CreateAssignment(ctx context.Context, assignment *AdminAssignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if assignment == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.create_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}

	if assignment.ID == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.create_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}

	if _, exists := m.assignments[assignment.ID]; exists {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.admin_storage.create_assignment",
		}
	}

	// Create a copy to avoid external modifications
	a := *assignment
	m.assignments[assignment.ID] = &a

	return nil
}

// GetAssignment retrieves an admin assignment by ID.
func (m *MemoryAdminStorage) GetAssignment(ctx context.Context, id string) (*AdminAssignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id == "" {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.get_assignment",
		}
	}

	assignment, exists := m.assignments[id]
	if !exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.admin_storage.get_assignment",
		}
	}

	// Return a copy
	a := *assignment
	return &a, nil
}

// UpdateAssignment updates an existing admin assignment.
func (m *MemoryAdminStorage) UpdateAssignment(ctx context.Context, assignment *AdminAssignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if assignment == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.update_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}

	if assignment.ID == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.update_assignment",
			Err:  ErrAdminAssignmentInvalid,
		}
	}

	if _, exists := m.assignments[assignment.ID]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.admin_storage.update_assignment",
		}
	}

	// Create a copy
	a := *assignment
	m.assignments[assignment.ID] = &a

	return nil
}

// DeleteAssignment removes an admin assignment.
func (m *MemoryAdminStorage) DeleteAssignment(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.delete_assignment",
		}
	}

	if _, exists := m.assignments[id]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.admin_storage.delete_assignment",
		}
	}

	delete(m.assignments, id)
	return nil
}

// ListAssignments retrieves admin assignments with pagination and filtering.
func (m *MemoryAdminStorage) ListAssignments(ctx context.Context, opts ListAdminAssignmentsOptions) ([]*AdminAssignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*AdminAssignment
	now := time.Now().UTC()

	for _, assignment := range m.assignments {
		// Apply filters
		if opts.UserID != 0 && assignment.UserID != opts.UserID {
			continue
		}

		if opts.AdminRole != "" && assignment.AdminRole != opts.AdminRole {
			continue
		}

		// Same tenant rule as CheckPermission (assignmentMatchesTenant,
		// #357): a tenant filter matches only assignments that name that
		// tenant; empty TenantIDs is system-tenant scope, so a
		// tenant-filtered listing must not surface system-scope grants.
		if !assignmentMatchesTenant(assignment, opts.TenantID) {
			continue
		}

		if opts.ActiveOnly {
			// Skip inactive assignments
			if !assignment.Active {
				continue
			}
			// Skip expired assignments
			if assignment.ExpiresAt != nil && now.After(*assignment.ExpiresAt) {
				continue
			}
		}

		// Create a copy
		a := *assignment
		result = append(result, &a)
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*AdminAssignment{}, nil
	}

	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// GetUserAssignments retrieves all active assignments for a specific user.
func (m *MemoryAdminStorage) GetUserAssignments(ctx context.Context, userID uint64) ([]*AdminAssignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if userID == 0 {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.admin_storage.get_user_assignments",
		}
	}

	var result []*AdminAssignment

	for _, assignment := range m.assignments {
		if assignment.UserID != userID {
			continue
		}

		// Only return active assignments (but include expired ones for proper error messaging)
		if !assignment.Active {
			continue
		}

		// Create a copy
		a := *assignment
		result = append(result, &a)
	}

	return result, nil
}

// MemoryImpersonationStorage implements ImpersonationStorage interface using in-memory maps.
// This implementation is thread-safe and suitable for testing and single-node deployments.
type MemoryImpersonationStorage struct {
	mu       sync.RWMutex
	sessions map[string]*ImpersonationSession // key: sessionID
}

// NewMemoryImpersonationStorage creates a new in-memory impersonation storage instance.
func NewMemoryImpersonationStorage() *MemoryImpersonationStorage {
	return &MemoryImpersonationStorage{
		sessions: make(map[string]*ImpersonationSession),
	}
}

// CreateImpersonationSession stores a new impersonation session.
func (m *MemoryImpersonationStorage) CreateImpersonationSession(ctx context.Context, session *ImpersonationSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.create_session",
		}
	}

	if session.ID == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.create_session",
		}
	}

	if _, exists := m.sessions[session.ID]; exists {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.impersonation_storage.create_session",
		}
	}

	// Create a copy to avoid external modifications
	s := *session
	m.sessions[session.ID] = &s

	return nil
}

// GetImpersonationSession retrieves an impersonation session by ID.
func (m *MemoryImpersonationStorage) GetImpersonationSession(ctx context.Context, id string) (*ImpersonationSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id == "" {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.get_session",
		}
	}

	session, exists := m.sessions[id]
	if !exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.impersonation_storage.get_session",
		}
	}

	// Return a copy
	s := *session
	return &s, nil
}

// EndImpersonationSession marks an impersonation session as ended.
func (m *MemoryImpersonationStorage) EndImpersonationSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.end_session",
		}
	}

	session, exists := m.sessions[id]
	if !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.impersonation_storage.end_session",
		}
	}

	// Mark session as ended
	now := time.Now().UTC()
	session.EndedAt = &now

	return nil
}

// IncrementOperationCount atomically increments the operation count for a session.
func (m *MemoryImpersonationStorage) IncrementOperationCount(ctx context.Context, id string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return 0, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.increment_operation_count",
		}
	}

	session, exists := m.sessions[id]
	if !exists {
		return 0, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.impersonation_storage.increment_operation_count",
		}
	}

	session.OperationCount++
	return session.OperationCount, nil
}

// ListActiveImpersonations retrieves active impersonation sessions for an admin user.
func (m *MemoryImpersonationStorage) ListActiveImpersonations(ctx context.Context, adminUserID uint64) ([]*ImpersonationSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if adminUserID == 0 {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "iam.impersonation_storage.list_active",
		}
	}

	var result []*ImpersonationSession

	for _, session := range m.sessions {
		// Filter by admin user ID
		if session.AdminUserID != adminUserID {
			continue
		}

		// Only return active sessions (EndedAt is nil)
		if session.EndedAt != nil {
			continue
		}

		// Create a copy
		s := *session
		result = append(result, &s)
	}

	return result, nil
}

// ListImpersonations retrieves impersonation sessions with optional filters.
func (m *MemoryImpersonationStorage) ListImpersonations(ctx context.Context, activeOnly bool, adminUserID uint64) ([]*ImpersonationSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now().UTC()
	var result []*ImpersonationSession

	for _, session := range m.sessions {
		// Filter by admin user ID if specified
		if adminUserID != 0 && session.AdminUserID != adminUserID {
			continue
		}

		// Filter by active status if requested
		if activeOnly {
			// Skip ended sessions
			if session.EndedAt != nil {
				continue
			}
			// Skip expired sessions
			if now.After(session.ExpiresAt) {
				continue
			}
		}

		// Create a copy
		s := *session
		result = append(result, &s)
	}

	return result, nil
}

// DeleteExpiredSessions removes expired impersonation sessions (cleanup).
func (m *MemoryImpersonationStorage) DeleteExpiredSessions(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	var expiredIDs []string

	for id, session := range m.sessions {
		// Consider a session expired if:
		// 1. It has passed its ExpiresAt time
		// 2. It has been explicitly ended (EndedAt is set)
		if now.After(session.ExpiresAt) || session.EndedAt != nil {
			expiredIDs = append(expiredIDs, id)
		}
	}

	// Delete expired sessions
	for _, id := range expiredIDs {
		delete(m.sessions, id)
	}

	return nil
}

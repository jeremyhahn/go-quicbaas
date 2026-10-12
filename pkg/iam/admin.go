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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// AdminStorage defines the interface for admin assignment persistence operations.
// Implementations must provide thread-safe, atomic operations for admin assignment data.
type AdminStorage interface {
	// CreateAssignment stores a new admin assignment.
	// Returns ErrAlreadyExists if an assignment with the same ID exists.
	CreateAssignment(ctx context.Context, assignment *AdminAssignment) error

	// GetAssignment retrieves an admin assignment by ID.
	// Returns ErrAdminAssignmentNotFound if the assignment does not exist.
	GetAssignment(ctx context.Context, id string) (*AdminAssignment, error)

	// UpdateAssignment updates an existing admin assignment.
	// Returns ErrAdminAssignmentNotFound if the assignment does not exist.
	UpdateAssignment(ctx context.Context, assignment *AdminAssignment) error

	// DeleteAssignment removes an admin assignment.
	// Returns ErrAdminAssignmentNotFound if the assignment does not exist.
	DeleteAssignment(ctx context.Context, id string) error

	// ListAssignments retrieves admin assignments with pagination and filtering.
	ListAssignments(ctx context.Context, opts ListAdminAssignmentsOptions) ([]*AdminAssignment, error)

	// GetUserAssignments retrieves all active assignments for a specific user.
	GetUserAssignments(ctx context.Context, userID string) ([]*AdminAssignment, error)
}

// ImpersonationStorage defines the interface for impersonation session persistence.
type ImpersonationStorage interface {
	// CreateImpersonationSession stores a new impersonation session.
	CreateImpersonationSession(ctx context.Context, session *ImpersonationSession) error

	// GetImpersonationSession retrieves an impersonation session by ID.
	// Returns ErrImpersonationSessionNotFound if the session does not exist.
	GetImpersonationSession(ctx context.Context, id string) (*ImpersonationSession, error)

	// EndImpersonationSession marks an impersonation session as ended.
	// Returns ErrImpersonationSessionNotFound if the session does not exist.
	EndImpersonationSession(ctx context.Context, id string) error

	// IncrementOperationCount atomically increments the operation count for a session.
	// Returns the new operation count. Returns ErrImpersonationSessionNotFound if the session does not exist.
	IncrementOperationCount(ctx context.Context, id string) (int64, error)

	// ListActiveImpersonations retrieves active impersonation sessions for an admin user.
	ListActiveImpersonations(ctx context.Context, adminUserID string) ([]*ImpersonationSession, error)

	// ListImpersonations retrieves impersonation sessions with optional filters.
	// If activeOnly is true, only returns sessions where EndedAt is nil and not expired.
	// If adminUserID is not empty, filters by the admin user.
	ListImpersonations(ctx context.Context, activeOnly bool, adminUserID string) ([]*ImpersonationSession, error)

	// DeleteExpiredSessions removes expired impersonation sessions (cleanup).
	DeleteExpiredSessions(ctx context.Context) error
}

// AdminManager manages delegated administration assignments and impersonation.
type AdminManager struct {
	mu                   sync.RWMutex
	adminStorage         AdminStorage
	impersonationStorage ImpersonationStorage
	auditLogger          AuditLogger // Optional audit logger (defined in audit.go)
	targetResolver       ImpersonationTargetResolver
}

// AdminManagerOptions contains options for creating an AdminManager.
type AdminManagerOptions struct {
	AdminStorage         AdminStorage
	ImpersonationStorage ImpersonationStorage
	AuditLogger          AuditLogger

	// TargetResolver verifies that an impersonation target exists in the
	// target tenant before a session is minted. *IdentityStore satisfies
	// it. StartImpersonation fails closed when it is absent — a manager
	// without a resolver cannot impersonate at all — so any deployment
	// that serves impersonation must wire one.
	TargetResolver ImpersonationTargetResolver
}

// ImpersonationTargetResolver resolves a user within a tenant. It is the
// identity-store seam StartImpersonation uses to verify the impersonation
// target really is a user of the target tenant before minting a session
// (*IdentityStore implements it via GetUser).
type ImpersonationTargetResolver interface {
	GetUser(ctx context.Context, tenantID, userID string) (*User, error)
}

// NewAdminManager creates a new admin manager with the provided storage backends.
func NewAdminManager(opts AdminManagerOptions) (*AdminManager, error) {
	if opts.AdminStorage == nil {
		return nil, api.NewRequiredFieldError("admin storage")
	}
	if opts.ImpersonationStorage == nil {
		return nil, api.NewRequiredFieldError("impersonation storage")
	}

	return &AdminManager{
		adminStorage:         opts.AdminStorage,
		impersonationStorage: opts.ImpersonationStorage,
		auditLogger:          opts.AuditLogger,
		targetResolver:       opts.TargetResolver,
	}, nil
}

// CreateAssignment creates a new admin assignment.
func (m *AdminManager) CreateAssignment(ctx context.Context, opts CreateAdminAssignmentOptions) (*AdminAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate required fields
	if opts.UserID == "" {
		return nil, api.NewRequiredFieldError("user_id")
	}
	if opts.AdminRole == "" {
		return nil, api.NewRequiredFieldError("admin_role")
	}
	if !IsValidAdminRole(opts.AdminRole) {
		return nil, api.NewInvalidFormatMessageError("admin_role", string(opts.AdminRole))
	}
	if opts.GrantedBy == "" {
		return nil, api.NewRequiredFieldError("granted_by")
	}

	// Generate assignment ID
	assignmentID, err := generateAdminAssignmentID()
	if err != nil {
		return nil, api.NewOperationFailedError("generate assignment ID", err)
	}

	now := time.Now().UTC()

	// Create assignment
	assignment := &AdminAssignment{
		ID:                assignmentID,
		UserID:            opts.UserID,
		AdminRole:         opts.AdminRole,
		TenantIDs:         opts.TenantIDs,
		Permissions:       opts.Permissions,
		DeniedPermissions: opts.DeniedPermissions,
		ResourceScopes:    opts.ResourceScopes,
		ExpiresAt:         opts.ExpiresAt,
		GrantedBy:         opts.GrantedBy,
		GrantedAt:         now,
		Active:            true,
		Metadata:          opts.Metadata,
	}

	// Validate assignment
	if err := assignment.Validate(); err != nil {
		return nil, err
	}

	// Mint the audit ID before the write, so an assignment never exists
	// without the record of who granted it.
	auditID, err := generateAuditLogID()
	if err != nil {
		return nil, err
	}

	// Store assignment
	if err := m.adminStorage.CreateAssignment(ctx, assignment); err != nil {
		return nil, api.NewOperationFailedError("create assignment", err)
	}

	// Audit log
	m.logAuditEvent(ctx, &AuditEvent{
		ID:        auditID,
		Timestamp: now,
		Action:    "admin.assignment.create",
		EventType: AuditEventUserManagement,
		Category:  AuditCategoryAdministration,
		Outcome:   AuditOutcomeSuccess,
		Actor: &AuditActor{
			UserID: opts.GrantedBy,
		},
		Resource: &AuditResource{
			Type: "admin_assignment",
			ID:   assignmentID,
		},
		Details: map[string]interface{}{
			"target_user_id": opts.UserID,
			"admin_role":     string(opts.AdminRole),
			"tenant_ids":     opts.TenantIDs,
		},
	})

	return assignment, nil
}

// GetAssignment retrieves an admin assignment by ID.
func (m *AdminManager) GetAssignment(ctx context.Context, id string) (*AdminAssignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id == "" {
		return nil, api.NewRequiredFieldError("assignment ID")
	}

	assignment, err := m.adminStorage.GetAssignment(ctx, id)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrAdminAssignmentNotFound
		}
		return nil, api.NewOperationFailedError("get assignment", err)
	}

	return assignment, nil
}

// UpdateAssignment updates an existing admin assignment.
func (m *AdminManager) UpdateAssignment(ctx context.Context, id string, opts UpdateAdminAssignmentOptions) (*AdminAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return nil, api.NewRequiredFieldError("assignment ID")
	}

	// Get existing assignment
	assignment, err := m.adminStorage.GetAssignment(ctx, id)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrAdminAssignmentNotFound
		}
		return nil, api.NewOperationFailedError("get assignment", err)
	}

	// Apply updates
	if opts.AdminRole != nil {
		if !IsValidAdminRole(*opts.AdminRole) {
			return nil, api.NewInvalidFormatMessageError("admin_role", string(*opts.AdminRole))
		}
		assignment.AdminRole = *opts.AdminRole
	}
	if opts.TenantIDs != nil {
		assignment.TenantIDs = opts.TenantIDs
	}
	if opts.Permissions != nil {
		assignment.Permissions = opts.Permissions
	}
	if opts.DeniedPermissions != nil {
		assignment.DeniedPermissions = opts.DeniedPermissions
	}
	if opts.ResourceScopes != nil {
		assignment.ResourceScopes = opts.ResourceScopes
	}
	if opts.ExpiresAt != nil {
		assignment.ExpiresAt = opts.ExpiresAt
	}
	if opts.Active != nil {
		assignment.Active = *opts.Active
	}
	if opts.Metadata != nil {
		if assignment.Metadata == nil {
			assignment.Metadata = make(map[string]string)
		}
		for k, v := range opts.Metadata {
			assignment.Metadata[k] = v
		}
	}

	// Validate updated assignment
	if err := assignment.Validate(); err != nil {
		return nil, err
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return nil, err
	}

	// Store updated assignment
	if err := m.adminStorage.UpdateAssignment(ctx, assignment); err != nil {
		return nil, api.NewOperationFailedError("update assignment", err)
	}

	// Audit log
	m.logAuditEvent(ctx, &AuditEvent{
		ID:        auditID,
		Timestamp: time.Now().UTC(),
		Action:    "admin.assignment.update",
		EventType: AuditEventUserManagement,
		Category:  AuditCategoryAdministration,
		Outcome:   AuditOutcomeSuccess,
		Resource: &AuditResource{
			Type: "admin_assignment",
			ID:   id,
		},
		Details: map[string]interface{}{
			"user_id": assignment.UserID,
		},
	})

	return assignment, nil
}

// DeleteAssignment deletes an admin assignment.
func (m *AdminManager) DeleteAssignment(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return api.NewRequiredFieldError("assignment ID")
	}

	// Get assignment for audit logging
	assignment, err := m.adminStorage.GetAssignment(ctx, id)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrAdminAssignmentNotFound
		}
		return api.NewOperationFailedError("get assignment", err)
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return err
	}

	// Delete assignment
	if err := m.adminStorage.DeleteAssignment(ctx, id); err != nil {
		if dberrors.IsNotFound(err) {
			return ErrAdminAssignmentNotFound
		}
		return api.NewOperationFailedError("delete assignment", err)
	}

	// Audit log
	m.logAuditEvent(ctx, &AuditEvent{
		ID:        auditID,
		Timestamp: time.Now().UTC(),
		Action:    "admin.assignment.delete",
		EventType: AuditEventUserManagement,
		Category:  AuditCategoryAdministration,
		Outcome:   AuditOutcomeSuccess,
		Resource: &AuditResource{
			Type: "admin_assignment",
			ID:   id,
		},
		Details: map[string]interface{}{
			"user_id":    assignment.UserID,
			"admin_role": string(assignment.AdminRole),
		},
	})

	return nil
}

// ListAssignments retrieves admin assignments with filtering.
func (m *AdminManager) ListAssignments(ctx context.Context, opts ListAdminAssignmentsOptions) ([]*AdminAssignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	assignments, err := m.adminStorage.ListAssignments(ctx, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list assignments", err)
	}

	return assignments, nil
}

// CheckPermission checks if an admin user has the required permission.
func (m *AdminManager) CheckPermission(ctx context.Context, req *AdminCheckRequest) (*AdminCheckResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if req == nil {
		return nil, api.NewRequiredFieldError("check request")
	}
	if req.UserID == "" {
		return nil, api.NewRequiredFieldError("user_id")
	}
	if req.Permission == "" {
		return nil, api.NewRequiredFieldError("permission")
	}

	// Get user's active assignments
	assignments, err := m.adminStorage.GetUserAssignments(ctx, req.UserID)
	if err != nil {
		return nil, api.NewOperationFailedError("get user assignments", err)
	}

	now := time.Now().UTC()

	// Track if any assignments were skipped due to expiration
	hasExpiredAssignments := false

	// Check each assignment for the required permission
	for _, assignment := range assignments {
		// Skip inactive assignments
		if !assignment.Active {
			continue
		}

		// Check expiration
		if assignment.ExpiresAt != nil && now.After(*assignment.ExpiresAt) {
			hasExpiredAssignments = true
			continue
		}

		// Check tenant scope on a tenant-scoped request
		// (assignmentMatchesTenant, the single statement of the rule): an
		// assignment lists the tenants it applies to explicitly; one with
		// EMPTY TenantIDs is scoped to the system tenant alone (the
		// firstboot super_admin's shape — see AdminAssignment.TenantIDs),
		// so it matches an unscoped check or a system-tenant target and
		// NOTHING else. Empty must never mean "every tenant": that read
		// let the platform operator pass tenant-scoped checks —
		// impersonation included — for any tenant on the platform (#357),
		// and this path never reaches the qrdb authorizer, so its scope
		// model cannot catch it. There is deliberately no all-tenants
		// sentinel; reach into a tenant requires naming that tenant in
		// the grant.
		if !assignmentMatchesTenant(assignment, req.TenantID) {
			continue
		}

		// Check resource scope if specified
		if req.ResourceType != "" && len(assignment.ResourceScopes) > 0 {
			resourceMatch := false
			for _, scope := range assignment.ResourceScopes {
				if scope.ResourceType == req.ResourceType {
					// Check if resource ID matches pattern
					// Pattern might be in format "type:pattern" - strip the type prefix if present
					pattern := scope.ResourcePattern
					resourceID := req.ResourceID
					typePrefix := req.ResourceType + ":"
					if strings.HasPrefix(pattern, typePrefix) {
						pattern = strings.TrimPrefix(pattern, typePrefix)
					}
					// Also strip type prefix from resourceID for consistent matching
					if strings.HasPrefix(resourceID, typePrefix) {
						resourceID = strings.TrimPrefix(resourceID, typePrefix)
					}
					if matchResourcePattern(pattern, resourceID) {
						resourceMatch = true
						break
					}
				}
			}
			if !resourceMatch {
				continue
			}
		}

		// Check if permission is explicitly denied
		for _, deniedPerm := range assignment.DeniedPermissions {
			if deniedPerm == req.Permission {
				return &AdminCheckResponse{
					Allowed: false,
					Reason:  fmt.Sprintf("permission %s is explicitly denied", req.Permission),
				}, nil
			}
		}

		// Get permissions for this role
		rolePerms := GetDefaultPermissions(assignment.AdminRole)

		// Add explicit permissions. Copy first: appending straight onto
		// rolePerms would write into the shared default-permission slice.
		allPerms := make([]AdminPermission, 0, len(rolePerms)+len(assignment.Permissions))
		allPerms = append(allPerms, rolePerms...)
		allPerms = append(allPerms, assignment.Permissions...)

		// Check if permission is granted
		for _, perm := range allPerms {
			if perm == req.Permission {
				// Update last used timestamp
				assignment.LastUsedAt = &now
				_ = m.adminStorage.UpdateAssignment(ctx, assignment)

				return &AdminCheckResponse{
					Allowed:      true,
					AssignmentID: assignment.ID,
					AdminRole:    assignment.AdminRole,
					Reason:       fmt.Sprintf("granted by assignment %s", assignment.ID),
				}, nil
			}
		}
	}

	// No assignment grants the permission - include expiration info if applicable
	reason := fmt.Sprintf("permission %s not granted", req.Permission)
	if hasExpiredAssignments {
		reason = fmt.Sprintf("permission %s not granted (some assignments expired)", req.Permission)
	}
	return &AdminCheckResponse{
		Allowed: false,
		Reason:  reason,
	}, nil
}

// StartImpersonation starts an impersonation session. The acting admin is
// the AUTHENTICATED principal bound into ctx by the transport auth
// middleware (auth.SubjectFromContext) — never a caller-supplied value.
// This is the single authoritative choke point: the request shapes on
// every transport (REST/gRPC/MCP/Unix/QUIC/embedded) no longer carry an
// actor id, and this method would refuse a caller-named actor even if one
// slipped through, because it never reads one. Authorizing on a
// request-supplied admin_user_id let any caller obtain a session as any
// privileged user and falsified the audit trail in the same request
// (go-quicbaas#383); resolving the actor here fixes both, for all
// transports at once.
//
// It is gated on
// PermUserImpersonate checked IN THE TARGET TENANT (opts.TenantID), and
// CheckPermission's tenant rule makes that an explicit-grant requirement:
// an assignment matches a tenant-scoped check only by naming the tenant in
// TenantIDs (empty means system-tenant scope, never all tenants — #357).
// A system-tenant principal — the firstboot super_admin included — is
// therefore denied impersonation of any tenant's users by default; the
// one retained break-glass path is an assignment explicitly scoped to
// that tenant (an auditable grant: the assignment records GrantedBy, and
// the session itself is audit-logged below).
//
// The target must really be a user of the target tenant: after the
// permission check, the configured ImpersonationTargetResolver (the
// identity store) must resolve TargetUserID within opts.TenantID, or no
// session is minted (ErrImpersonationTargetNotFound). A manager with no
// resolver wired fails CLOSED (ErrImpersonationTargetResolverRequired)
// rather than minting sessions for unverified principals. The minted
// session's authority is additionally bounded by ITS TenantID — every
// consumer acts as TargetUserID strictly within session.TenantID.
func (m *AdminManager) StartImpersonation(ctx context.Context, opts *StartImpersonationOptions) (*ImpersonationSession, error) {
	// Validate options before checking permission (no lock needed yet)
	if opts == nil {
		return nil, api.NewRequiredFieldError("start impersonation options")
	}
	if opts.TargetUserID == "" {
		return nil, api.NewRequiredFieldError("target_user_id")
	}
	if opts.TenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}
	if opts.Reason == "" {
		return nil, api.NewRequiredFieldError("reason")
	}

	// The acting admin is the authenticated caller, resolved from the
	// verified identity the auth middleware bound into ctx — the same
	// source the REST/gRPC hardened surfaces already trust for every other
	// authorization decision. No authenticated principal means no actor to
	// authorize, so fail closed rather than mint a session for an
	// anonymous caller.
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated || subject.ID == "" {
		return nil, ErrImpersonationActorRequired
	}
	actorUserID := subject.ID
	actorUsername := subject.Username

	// Check permission BEFORE acquiring the write lock to avoid deadlock
	// (CheckPermission acquires RLock internally). The decision is made
	// about the AUTHENTICATED caller, not about whoever a request claimed
	// to be.
	checkResp, err := m.CheckPermission(ctx, &AdminCheckRequest{
		UserID:     actorUserID,
		Permission: PermUserImpersonate,
		TenantID:   opts.TenantID,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("check permission", err)
	}
	if !checkResp.Allowed {
		return nil, ErrImpersonationNotAllowed
	}

	// Verify the target exists in the target tenant before minting
	// anything. Fail closed when no resolver is wired: a session for an
	// unverified principal must never come into existence. Only a genuine
	// not-found becomes ErrImpersonationTargetNotFound — any other
	// resolver failure (sealed store, invalid input, cancellation) is a
	// lookup that could not be performed, and masking it as "not found"
	// would hide operational faults from the caller.
	if m.targetResolver == nil {
		return nil, ErrImpersonationTargetResolverRequired
	}
	if _, err := m.targetResolver.GetUser(ctx, opts.TenantID, opts.TargetUserID); err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrImpersonationTargetNotFound
		}
		return nil, api.NewOperationFailedError("resolve impersonation target", err)
	}

	// Now acquire the write lock for session creation
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate session ID
	sessionID, err := generateImpersonationSessionID()
	if err != nil {
		return nil, api.NewOperationFailedError("generate session ID", err)
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Default duration is 1 hour
	duration := opts.Duration
	if duration == 0 {
		duration = time.Hour
	}

	// Create impersonation session. The acting admin is the authenticated
	// caller resolved above, never a request-supplied value.
	session := &ImpersonationSession{
		ID:             sessionID,
		AdminUserID:    actorUserID,
		AdminUsername:  actorUsername,
		TargetUserID:   opts.TargetUserID,
		TargetUsername: opts.TargetUsername,
		TenantID:       opts.TenantID,
		Reason:         opts.Reason,
		StartedAt:      now,
		ExpiresAt:      now.Add(duration),
		OperationCount: 0,
		AuditLogID:     auditID,
	}

	// Store session
	if err := m.impersonationStorage.CreateImpersonationSession(ctx, session); err != nil {
		return nil, api.NewOperationFailedError("create impersonation session", err)
	}

	// Audit log - impersonation session start
	m.logAuditEvent(ctx, &AuditEvent{
		ID:                     session.AuditLogID,
		TenantID:               opts.TenantID,
		Timestamp:              now,
		Action:                 "admin.impersonation.start",
		EventType:              AuditEventSessionEvent,
		Category:               AuditCategorySecurity,
		Outcome:                AuditOutcomeSuccess,
		ImpersonationSessionID: sessionID,
		CorrelationID:          sessionID, // Use session ID as correlation for all related events
		Actor: &AuditActor{
			UserID:   actorUserID,
			Username: actorUsername,
		},
		Resource: &AuditResource{
			Type: "impersonation_session",
			ID:   sessionID,
		},
		Details: map[string]interface{}{
			"target_user_id":  opts.TargetUserID,
			"target_username": opts.TargetUsername,
			"reason":          opts.Reason,
			"duration":        duration.String(),
			"expires_at":      session.ExpiresAt.Format(time.RFC3339),
		},
	})

	return session, nil
}

// EndImpersonation ends an impersonation session.
func (m *AdminManager) EndImpersonation(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sessionID == "" {
		return api.NewRequiredFieldError("session ID")
	}

	// Get session for audit logging
	session, err := m.impersonationStorage.GetImpersonationSession(ctx, sessionID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrImpersonationSessionNotFound
		}
		return api.NewOperationFailedError("get impersonation session", err)
	}

	// Check if session is already ended
	if session.EndedAt != nil {
		return api.NewInvalidInputError("impersonation session already ended")
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return err
	}

	// End session
	if err := m.impersonationStorage.EndImpersonationSession(ctx, sessionID); err != nil {
		if dberrors.IsNotFound(err) {
			return ErrImpersonationSessionNotFound
		}
		return api.NewOperationFailedError("end impersonation session", err)
	}

	// Calculate session duration
	endTime := time.Now().UTC()
	duration := endTime.Sub(session.StartedAt)

	// Audit log - impersonation session end with full summary
	m.logAuditEvent(ctx, &AuditEvent{
		ID:                     auditID,
		TenantID:               session.TenantID,
		Timestamp:              endTime,
		Action:                 "admin.impersonation.end",
		EventType:              AuditEventSessionEvent,
		Category:               AuditCategorySecurity,
		Outcome:                AuditOutcomeSuccess,
		ImpersonationSessionID: sessionID,
		CorrelationID:          sessionID, // Link to start event and all operations
		Actor: &AuditActor{
			UserID:   session.AdminUserID,
			Username: session.AdminUsername,
		},
		Resource: &AuditResource{
			Type: "impersonation_session",
			ID:   sessionID,
		},
		Details: map[string]interface{}{
			"target_user_id":   session.TargetUserID,
			"target_username":  session.TargetUsername,
			"reason":           session.Reason,
			"started_at":       session.StartedAt.Format(time.RFC3339),
			"ended_at":         endTime.Format(time.RFC3339),
			"duration_seconds": int64(duration.Seconds()),
			"duration_human":   duration.String(),
			"operation_count":  session.OperationCount,
		},
	})

	return nil
}

// RecordImpersonatedOperation records an operation performed during an impersonation session.
// This increments the operation count and optionally logs the operation to the audit log.
func (m *AdminManager) RecordImpersonatedOperation(ctx context.Context, sessionID string, operation string, resource *AuditResource, outcome AuditOutcome, details map[string]interface{}) error {
	if sessionID == "" {
		return api.NewRequiredFieldError("session ID")
	}
	if operation == "" {
		return api.NewRequiredFieldError("operation")
	}

	// Get session to validate and get details
	session, err := m.impersonationStorage.GetImpersonationSession(ctx, sessionID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrImpersonationSessionNotFound
		}
		return api.NewOperationFailedError("get impersonation session", err)
	}

	// Check if session is still active
	if session.EndedAt != nil {
		return api.NewInvalidInputError("impersonation session has ended")
	}

	// Check if session has expired
	if time.Now().UTC().After(session.ExpiresAt) {
		return ErrImpersonationSessionExpired
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return err
	}

	// Increment operation count
	newCount, err := m.impersonationStorage.IncrementOperationCount(ctx, sessionID)
	if err != nil {
		return api.NewOperationFailedError("increment operation count", err)
	}

	// Build audit details
	auditDetails := map[string]interface{}{
		"impersonated_user_id":  session.TargetUserID,
		"impersonated_username": session.TargetUsername,
		"operation_number":      newCount,
	}
	for k, v := range details {
		auditDetails[k] = v
	}

	// Audit log the operation
	m.logAuditEvent(ctx, &AuditEvent{
		ID:                     auditID,
		TenantID:               session.TenantID,
		Timestamp:              time.Now().UTC(),
		Action:                 operation,
		EventType:              AuditEventAuthorization,
		Category:               AuditCategorySecurity,
		Outcome:                outcome,
		ImpersonationSessionID: sessionID,
		CorrelationID:          sessionID,
		Actor: &AuditActor{
			UserID:                session.TargetUserID, // Acting as the impersonated user
			Username:              session.TargetUsername,
			ImpersonatingUserID:   session.AdminUserID,
			ImpersonatingUsername: session.AdminUsername,
		},
		Resource: resource,
		Details:  auditDetails,
	})

	return nil
}

// GetActiveImpersonations retrieves active impersonation sessions for an admin user.
func (m *AdminManager) GetActiveImpersonations(ctx context.Context, adminUserID string) ([]*ImpersonationSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if adminUserID == "" {
		return nil, api.NewRequiredFieldError("admin_user_id")
	}

	sessions, err := m.impersonationStorage.ListActiveImpersonations(ctx, adminUserID)
	if err != nil {
		return nil, api.NewOperationFailedError("list active impersonations", err)
	}

	return sessions, nil
}

// ListImpersonations retrieves impersonation sessions with optional filters.
func (m *AdminManager) ListImpersonations(ctx context.Context, activeOnly bool, adminUserID string) ([]*ImpersonationSession, error) {
	sessions, err := m.impersonationStorage.ListImpersonations(ctx, activeOnly, adminUserID)
	if err != nil {
		return nil, api.NewOperationFailedError("list impersonations", err)
	}

	return sessions, nil
}

// GetImpersonationSession retrieves an impersonation session by ID.
func (m *AdminManager) GetImpersonationSession(ctx context.Context, sessionID string) (*ImpersonationSession, error) {
	session, err := m.impersonationStorage.GetImpersonationSession(ctx, sessionID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrImpersonationSessionNotFound
		}
		return nil, api.NewOperationFailedError("get impersonation session", err)
	}

	return session, nil
}

// Helper functions

// generateAdminAssignmentID generates a cryptographically secure random assignment ID.
func generateAdminAssignmentID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "adm_" + hex.EncodeToString(bytes), nil
}

// generateImpersonationSessionID generates a cryptographically secure random session ID.
func generateImpersonationSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "imp_" + hex.EncodeToString(bytes), nil
}

// auditLogIDEntropyBytes is the size of an audit event ID. 128 bits keeps the
// chance of two events sharing an ID negligible.
const auditLogIDEntropyBytes = 16

// auditLogIDEntropy is the randomness generateAuditLogID draws on. It is a
// variable so a test can make the CSPRNG fail without touching the
// process-global RNG state every other test in this package shares.
var auditLogIDEntropy = xkms.Rand

// generateAuditLogID generates a cryptographically secure random audit log ID.
//
// The ID is the key the audit store files an event under (AuditStorage
// saves and fetches events by it), not a secret: it is returned to admins on the
// impersonation session. So the property it must keep is uniqueness. A
// timestamp fallback would give two events failing in the same nanosecond, or
// on hosts with coarse clocks, the same key, and the second would silently
// overwrite the first. A CSPRNG failure is therefore refused with
// AuditLogIDGenerationError, and the caller does not perform the audited
// operation.
func generateAuditLogID() (string, error) {
	b, err := auditLogIDEntropy(auditLogIDEntropyBytes)
	if err != nil {
		return "", &AuditLogIDGenerationError{Cause: err}
	}
	return "audit_" + hex.EncodeToString(b), nil
}

// matchResourcePattern checks if a resource ID matches a resource pattern.
// Supports wildcard patterns like "user:*" or "group:engineering-*".
func matchResourcePattern(pattern, resourceID string) bool {
	// Exact match
	if pattern == resourceID {
		return true
	}

	// Wildcard match
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(resourceID, prefix)
	}

	// Prefix wildcard
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(resourceID, suffix)
	}

	return false
}

// logAuditEvent logs an audit event if an audit logger is configured.
func (m *AdminManager) logAuditEvent(ctx context.Context, event *AuditEvent) {
	if m.auditLogger != nil {
		_ = m.auditLogger.Log(ctx, event)
	}
}

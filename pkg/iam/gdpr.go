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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

const (
	// DefaultGracePeriodDays is the default grace period for deletion requests.
	DefaultGracePeriodDays = 30

	// DefaultExportDownloadTTL is the default time a download link is valid.
	DefaultExportDownloadTTL = 7 * 24 * time.Hour

	// ExportSchemaVersion is the current export schema version.
	ExportSchemaVersion = "1.0"
)

// GDPRAuditLogger defines a minimal interface for audit logging in GDPR operations.
// This avoids conflicts with other AuditLogger definitions in the package.
type GDPRAuditLogger interface {
	// LogGDPREvent logs a GDPR-related audit event
	LogGDPREvent(ctx context.Context, tenantID, userID, action string, details map[string]interface{}) error
}

// ConsentStorage defines the interface for consent persistence operations.
type ConsentStorage interface {
	// CreateConsent stores a new consent record.
	CreateConsent(ctx context.Context, opts CreateConsentOptions) (*Consent, error)

	// GetConsent retrieves a consent by user, tenant, and purpose.
	GetConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose) (*Consent, error)

	// UpdateConsent updates an existing consent record.
	UpdateConsent(ctx context.Context, id string, opts UpdateConsentOptions) (*Consent, error)

	// DeleteConsent removes a consent record.
	DeleteConsent(ctx context.Context, id string) error

	// ListConsents retrieves consents matching the filter.
	ListConsents(ctx context.Context, opts ListConsentsOptions) ([]*Consent, error)

	// GetUserConsents retrieves all consents for a user.
	GetUserConsents(ctx context.Context, tenantID, userID string) ([]*Consent, error)
}

// DataExportStorage defines the interface for data export request persistence.
type DataExportStorage interface {
	// CreateExportRequest creates a new export request.
	CreateExportRequest(ctx context.Context, opts CreateDataExportOptions) (*DataExportRequest, error)

	// GetExportRequest retrieves an export request by ID.
	GetExportRequest(ctx context.Context, id string) (*DataExportRequest, error)

	// UpdateExportRequest updates an existing export request.
	UpdateExportRequest(ctx context.Context, req *DataExportRequest) error

	// ListExportRequests retrieves export requests matching the filter.
	ListExportRequests(ctx context.Context, opts ListExportRequestsOptions) ([]*DataExportRequest, error)
}

// DeletionStorage defines the interface for deletion request persistence.
type DeletionStorage interface {
	// CreateDeletionRequest creates a new deletion request.
	CreateDeletionRequest(ctx context.Context, opts CreateDeletionRequestOptions) (*DataDeletionRequest, error)

	// GetDeletionRequest retrieves a deletion request by ID.
	GetDeletionRequest(ctx context.Context, id string) (*DataDeletionRequest, error)

	// UpdateDeletionRequest updates an existing deletion request.
	UpdateDeletionRequest(ctx context.Context, req *DataDeletionRequest) error

	// ListDeletionRequests retrieves deletion requests matching the filter.
	ListDeletionRequests(ctx context.Context, opts ListDeletionRequestsOptions) ([]*DataDeletionRequest, error)
}

// GDPRManagerDependencies contains dependencies for the GDPR manager.
type GDPRManagerDependencies struct {
	// Storage backends
	ConsentStorage    ConsentStorage
	DataExportStorage DataExportStorage
	DeletionStorage   DeletionStorage
	UserStorage       UserStorage
	SessionStorage    SessionStorage

	// Optional audit logger
	AuditLogger GDPRAuditLogger

	// Optional group and role storage for complete data export
	GroupStorage GroupStorage
	RoleStorage  RoleStorage
}

// GDPRManager manages GDPR compliance operations including consent, data export, and deletion.
type GDPRManager struct {
	mu sync.RWMutex

	// Dependencies
	deps GDPRManagerDependencies

	// Configuration
	defaultGracePeriod time.Duration
	exportDownloadTTL  time.Duration
}

// NewGDPRManager creates a new GDPR compliance manager.
func NewGDPRManager(deps GDPRManagerDependencies) (*GDPRManager, error) {
	if deps.ConsentStorage == nil {
		return nil, api.NewRequiredFieldError("consent storage")
	}
	if deps.DataExportStorage == nil {
		return nil, api.NewRequiredFieldError("data export storage")
	}
	if deps.DeletionStorage == nil {
		return nil, api.NewRequiredFieldError("deletion storage")
	}
	if deps.UserStorage == nil {
		return nil, api.NewRequiredFieldError("user storage")
	}

	return &GDPRManager{
		deps:               deps,
		defaultGracePeriod: DefaultGracePeriodDays * 24 * time.Hour,
		exportDownloadTTL:  DefaultExportDownloadTTL,
	}, nil
}

// ==================== Consent Management ====================

// GrantConsent records user consent for a specific purpose.
// If consent already exists for the given purpose, it will be updated with the new version.
func (m *GDPRManager) GrantConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*Consent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !IsValidConsentPurpose(purpose) {
		return nil, NewIAMError(ErrConsentInvalid, "iam.gdpr.grant_consent", "invalid consent purpose", nil)
	}

	// Check if consent already exists - if so, update it
	existing, err := m.deps.ConsentStorage.GetConsent(ctx, tenantID, userID, purpose)
	if err == nil && existing != nil {
		// Update existing consent with new version
		updateOpts := UpdateConsentOptions{
			Status:    ConsentStatusGranted,
			Version:   version,
			ExpiresAt: expiresAt,
			IPAddress: ipAddress,
			UserAgent: userAgent,
		}
		consent, updateErr := m.deps.ConsentStorage.UpdateConsent(ctx, existing.ID, updateOpts)
		if updateErr != nil {
			return nil, api.NewOperationFailedError("update consent", updateErr)
		}

		// Audit log (optional)
		if m.deps.AuditLogger != nil {
			_ = m.deps.AuditLogger.LogGDPREvent(ctx, tenantID, userID, "consent.updated", map[string]interface{}{
				"purpose":    string(purpose),
				"version":    version,
				"consent_id": consent.ID,
			})
		}
		return consent, nil
	}

	// Create new consent
	opts := CreateConsentOptions{
		UserID:    userID,
		TenantID:  tenantID,
		Purpose:   purpose,
		Status:    ConsentStatusGranted,
		Version:   version,
		ExpiresAt: expiresAt,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	consent, err := m.deps.ConsentStorage.CreateConsent(ctx, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("create consent", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, tenantID, userID, "consent.granted", map[string]interface{}{
			"purpose":    string(purpose),
			"version":    version,
			"consent_id": consent.ID,
		})
	}

	return consent, nil
}

// WithdrawConsent withdraws previously granted consent.
func (m *GDPRManager) WithdrawConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose, ipAddress, userAgent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	consent, err := m.deps.ConsentStorage.GetConsent(ctx, tenantID, userID, purpose)
	if err != nil {
		return err
	}

	updateOpts := UpdateConsentOptions{
		Status:    ConsentStatusWithdrawn,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	_, err = m.deps.ConsentStorage.UpdateConsent(ctx, consent.ID, updateOpts)
	if err != nil {
		return api.NewOperationFailedError("withdraw consent", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, tenantID, userID, "consent.withdrawn", map[string]interface{}{
			"purpose":    string(purpose),
			"consent_id": consent.ID,
		})
	}

	return nil
}

// GetConsent retrieves a specific consent record.
func (m *GDPRManager) GetConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose) (*Consent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deps.ConsentStorage.GetConsent(ctx, tenantID, userID, purpose)
}

// ListUserConsents retrieves all consents for a user.
func (m *GDPRManager) ListUserConsents(ctx context.Context, tenantID, userID string) ([]*Consent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deps.ConsentStorage.GetUserConsents(ctx, tenantID, userID)
}

// ==================== Data Export ====================

// RequestExport creates a new data export request and starts background processing.
// The export will be processed asynchronously and its status can be checked via GetExportStatus.
func (m *GDPRManager) RequestExport(ctx context.Context, tenantID, userID, requestedBy string, format DataExportFormat, includeCategories, excludeCategories []string) (*DataExportRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing pending or processing export
	existing, err := m.deps.DataExportStorage.ListExportRequests(ctx, ListExportRequestsOptions{
		UserID:   userID,
		TenantID: tenantID,
		Status:   DataExportStatusPending,
		Limit:    1,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("check existing exports", err)
	}
	if len(existing) > 0 {
		return nil, ErrDataExportPending
	}

	// Also check for processing exports
	processing, err := m.deps.DataExportStorage.ListExportRequests(ctx, ListExportRequestsOptions{
		UserID:   userID,
		TenantID: tenantID,
		Status:   DataExportStatusProcessing,
		Limit:    1,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("check processing exports", err)
	}
	if len(processing) > 0 {
		return nil, ErrDataExportPending
	}

	// Validate categories
	for _, cat := range includeCategories {
		if !IsValidDataCategory(cat) {
			return nil, NewIAMError(ErrDataExportInvalid, "iam.gdpr.request_export", fmt.Sprintf("invalid category: %s", cat), nil)
		}
	}
	for _, cat := range excludeCategories {
		if !IsValidDataCategory(cat) {
			return nil, NewIAMError(ErrDataExportInvalid, "iam.gdpr.request_export", fmt.Sprintf("invalid category: %s", cat), nil)
		}
	}

	opts := CreateDataExportOptions{
		UserID:            userID,
		TenantID:          tenantID,
		RequestedBy:       requestedBy,
		Format:            format,
		IncludeCategories: includeCategories,
		ExcludeCategories: excludeCategories,
	}

	request, err := m.deps.DataExportStorage.CreateExportRequest(ctx, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("create export request", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, tenantID, requestedBy, "data_export.requested", map[string]interface{}{
			"export_id":          request.ID,
			"subject_user_id":    userID,
			"format":             string(format),
			"include_categories": includeCategories,
			"exclude_categories": excludeCategories,
		})
	}

	// Start background processing of the export. It must outlive the request.
	go m.processExportAsync(request.ID) //nolint:gosec // G118: the work must outlive the caller; a request-scoped context would cancel it.

	return request, nil
}

// processExportAsync processes an export request in the background.
// This is called automatically by RequestExport and runs in a separate goroutine.
func (m *GDPRManager) processExportAsync(exportID string) {
	// Recover from any panic to prevent crashing the server
	defer func() {
		if r := recover(); r != nil {
			// Log panic but don't crash - mark export as failed
			m.markExportFailed(exportID, fmt.Sprintf("panic during export processing: %v", r))
		}
	}()

	// Use a background context with a reasonable timeout since the original request context may be cancelled
	// The timeout is needed because the underlying storage (Raft) requires a deadline on the context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Small delay to ensure the request is fully committed and lock is released
	delayTimer := time.NewTimer(50 * time.Millisecond)
	select {
	case <-delayTimer.C:
	case <-ctx.Done():
		delayTimer.Stop()
		return
	}

	// GenerateExport handles all processing, status updates, and error marking internally
	m.GenerateExport(ctx, exportID) //nolint:errcheck,gosec // Error handling is internal to GenerateExport
}

// markExportFailed marks an export as failed with an error message.
// This is used for panic recovery in async processing.
func (m *GDPRManager) markExportFailed(exportID, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.deps.DataExportStorage.GetExportRequest(ctx, exportID)
	if err != nil {
		return
	}

	now := time.Now()
	request.Status = DataExportStatusFailed
	request.ErrorMessage = errMsg
	request.CompletedAt = &now
	_ = m.deps.DataExportStorage.UpdateExportRequest(ctx, request)
}

// GetExportStatus retrieves the status of an export request.
func (m *GDPRManager) GetExportStatus(ctx context.Context, exportID string) (*DataExportRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deps.DataExportStorage.GetExportRequest(ctx, exportID)
}

// GenerateExport generates a user data export.
// This method is typically called automatically by RequestExport in the background,
// but can also be called directly for synchronous processing or to re-fetch completed exports.
func (m *GDPRManager) GenerateExport(ctx context.Context, exportID string) (*UserDataExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.deps.DataExportStorage.GetExportRequest(ctx, exportID)
	if err != nil {
		return nil, api.NewOperationFailedError("get export request", err)
	}

	// For completed exports, just regenerate and return the data without status updates
	if request.Status == DataExportStatusCompleted {
		return m.collectUserData(ctx, request)
	}

	// Failed exports cannot be regenerated
	if request.Status == DataExportStatusFailed {
		return nil, api.NewOperationFailedError("export request", &ExportFailedError{Message: request.ErrorMessage})
	}

	// Update status to processing
	now := time.Now()
	request.Status = DataExportStatusProcessing
	request.StartedAt = &now
	if err := m.deps.DataExportStorage.UpdateExportRequest(ctx, request); err != nil {
		return nil, api.NewOperationFailedError("update export status", err)
	}

	// Generate export
	export, err := m.collectUserData(ctx, request)
	if err != nil {
		// Mark as failed
		request.Status = DataExportStatusFailed
		request.ErrorMessage = err.Error()
		completedAt := time.Now()
		request.CompletedAt = &completedAt
		_ = m.deps.DataExportStorage.UpdateExportRequest(ctx, request)
		return nil, api.NewOperationFailedError("collect user data", err)
	}

	// Update status to completed
	completedAt := time.Now()
	expiresAt := completedAt.Add(m.exportDownloadTTL)
	request.Status = DataExportStatusCompleted
	request.CompletedAt = &completedAt
	request.ExpiresAt = &expiresAt
	request.DownloadURL = fmt.Sprintf("/api/v1/tenants/%s/users/%s/gdpr/export/%s/download",
		request.TenantID, request.UserID, exportID)

	if err := m.deps.DataExportStorage.UpdateExportRequest(ctx, request); err != nil {
		return nil, api.NewOperationFailedError("update export completion", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, request.TenantID, request.RequestedBy, "data_export.completed", map[string]interface{}{
			"export_id":       exportID,
			"subject_user_id": request.UserID,
		})
	}

	return export, nil
}

// collectUserData gathers all user data for export.
func (m *GDPRManager) collectUserData(ctx context.Context, request *DataExportRequest) (*UserDataExport, error) {
	export := &UserDataExport{
		ExportMetadata: ExportMetadata{
			ExportID:      request.ID,
			UserID:        request.UserID,
			TenantID:      request.TenantID,
			ExportedAt:    time.Now(),
			Format:        request.Format,
			Categories:    m.determineCategories(request),
			SchemaVersion: ExportSchemaVersion,
		},
	}

	// Get user profile
	if m.shouldIncludeCategory("profile", request) {
		user, err := m.deps.UserStorage.GetUser(ctx, request.TenantID, request.UserID)
		if err != nil {
			return nil, api.NewOperationFailedError("get user", err)
		}

		export.Profile = &UserProfileExport{
			ID:         user.ID,
			Email:      user.Email,
			Name:       user.Name,
			Attributes: user.Attributes,
			Created:    user.Created,
			Updated:    user.Updated,
			LastAuth:   user.LastAuth,
		}
	}

	// Get sessions
	if m.shouldIncludeCategory("sessions", request) && m.deps.SessionStorage != nil {
		sessions, err := m.deps.SessionStorage.GetSessionsByUser(ctx, request.TenantID, request.UserID)
		if err == nil && len(sessions) > 0 {
			export.Sessions = make([]*SessionExport, 0, len(sessions))
			for _, session := range sessions {
				export.Sessions = append(export.Sessions, &SessionExport{
					ID:         session.ID,
					StartedAt:  session.Created,
					ExpiresAt:  session.Expires,
					LastActive: session.LastActivity,
					IPAddress:  session.IPAddress,
					UserAgent:  session.UserAgent,
					AuthMethod: session.AuthMethod,
				})
			}
		}
	}

	// Get consents
	if m.shouldIncludeCategory("consents", request) {
		consents, err := m.deps.ConsentStorage.GetUserConsents(ctx, request.TenantID, request.UserID)
		if err == nil && len(consents) > 0 {
			export.Consents = make([]*ConsentExport, 0, len(consents))
			for _, consent := range consents {
				export.Consents = append(export.Consents, &ConsentExport{
					Purpose:     consent.Purpose,
					Status:      consent.Status,
					Version:     consent.Version,
					GrantedAt:   consent.GrantedAt,
					WithdrawnAt: consent.WithdrawnAt,
				})
			}
		}
	}

	// NOTE: WebAuthn credential export was removed in Phase 5 of the
	// identity refactor. Credentials are now owned by go-xkms and
	// surfaced through xkms's CredentialStore. A future iteration will
	// add a credential-export hook against the xkms SDK; until then the
	// "credentials" export category is a no-op.

	return export, nil
}

// shouldIncludeCategory determines if a category should be included in the export.
func (m *GDPRManager) shouldIncludeCategory(category string, request *DataExportRequest) bool {
	// Check exclude list first
	for _, exc := range request.ExcludeCategories {
		if exc == category {
			return false
		}
	}

	// If include list is empty, include all (except excluded)
	if len(request.IncludeCategories) == 0 {
		return true
	}

	// Check include list
	for _, inc := range request.IncludeCategories {
		if inc == category {
			return true
		}
	}

	return false
}

// determineCategories determines which categories are included in the export.
func (m *GDPRManager) determineCategories(request *DataExportRequest) []string {
	if len(request.IncludeCategories) > 0 {
		// Filter out excluded categories
		categories := make([]string, 0, len(request.IncludeCategories))
		for _, cat := range request.IncludeCategories {
			excluded := false
			for _, exc := range request.ExcludeCategories {
				if cat == exc {
					excluded = true
					break
				}
			}
			if !excluded {
				categories = append(categories, cat)
			}
		}
		return categories
	}

	// All categories except excluded
	categories := make([]string, 0, len(DataCategories))
	for _, cat := range DataCategories {
		excluded := false
		for _, exc := range request.ExcludeCategories {
			if cat == exc {
				excluded = true
				break
			}
		}
		if !excluded {
			categories = append(categories, cat)
		}
	}
	return categories
}

// ==================== Data Deletion ====================

// RequestDeletion creates a new data deletion request with grace period.
// A gracePeriodDays of 0 means immediate deletion (no grace period).
// A gracePeriodDays < 0 (e.g., -1) means use the default grace period.
func (m *GDPRManager) RequestDeletion(ctx context.Context, tenantID, userID, requestedBy, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*DataDeletionRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing pending deletion
	existing, err := m.deps.DeletionStorage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		UserID:   userID,
		TenantID: tenantID,
		Status:   DeletionStatusPending,
		Limit:    1,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("check existing deletions", err)
	}
	if len(existing) > 0 {
		return nil, ErrDeletionPending
	}

	// Validate categories
	for _, cat := range deleteCategories {
		if !IsValidDataCategory(cat) {
			return nil, NewIAMError(ErrDeletionInvalid, "iam.gdpr.request_deletion", fmt.Sprintf("invalid delete category: %s", cat), nil)
		}
	}
	for _, cat := range preserveCategories {
		if !IsValidDataCategory(cat) {
			return nil, NewIAMError(ErrDeletionInvalid, "iam.gdpr.request_deletion", fmt.Sprintf("invalid preserve category: %s", cat), nil)
		}
	}

	// Use default grace period only if negative value is passed (sentinel for "use default")
	// A value of 0 means immediate deletion (no grace period)
	if gracePeriodDays < 0 {
		gracePeriodDays = DefaultGracePeriodDays
	}

	opts := CreateDeletionRequestOptions{
		UserID:             userID,
		TenantID:           tenantID,
		RequestedBy:        requestedBy,
		Reason:             reason,
		DeleteCategories:   deleteCategories,
		PreserveCategories: preserveCategories,
		GracePeriodDays:    gracePeriodDays,
	}

	request, err := m.deps.DeletionStorage.CreateDeletionRequest(ctx, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("create deletion request", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, tenantID, requestedBy, "data_deletion.requested", map[string]interface{}{
			"deletion_id":         request.ID,
			"subject_user_id":     userID,
			"grace_period_days":   gracePeriodDays,
			"delete_categories":   deleteCategories,
			"preserve_categories": preserveCategories,
		})
	}

	// If immediate deletion (gracePeriodDays=0), start background processing
	if gracePeriodDays == 0 {
		// The deletion must outlive the request that scheduled it.
		go m.processDeletionAsync(request.ID) //nolint:gosec // G118: the work must outlive the caller; a request-scoped context would cancel it.
	}

	return request, nil
}

// processDeletionAsync processes a deletion request in the background.
// This is called automatically by RequestDeletion for immediate deletions.
func (m *GDPRManager) processDeletionAsync(deletionID string) {
	// Recover from any panic to prevent crashing the server
	defer func() {
		if r := recover(); r != nil {
			// Mark deletion as failed
			m.markDeletionFailed(deletionID, fmt.Sprintf("panic during deletion processing: %v", r))
		}
	}()

	// Use a background context with a reasonable timeout since the original request context may be cancelled
	// The timeout is needed because the underlying storage (Raft) requires a deadline on the context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Small delay to ensure the request is fully committed and lock is released
	delayTimer := time.NewTimer(50 * time.Millisecond)
	select {
	case <-delayTimer.C:
	case <-ctx.Done():
		delayTimer.Stop()
		return
	}

	// ProcessDeletion handles all processing, status updates, and error marking internally
	m.ProcessDeletion(ctx, deletionID) //nolint:errcheck,gosec // Error handling is internal to ProcessDeletion
}

// markDeletionFailed marks a deletion as failed with an error message.
// This is used for panic recovery in async processing.
func (m *GDPRManager) markDeletionFailed(deletionID, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.deps.DeletionStorage.GetDeletionRequest(ctx, deletionID)
	if err != nil {
		return
	}

	now := time.Now()
	request.Status = DeletionStatusFailed
	request.ErrorMessage = errMsg
	request.CompletedAt = &now
	_ = m.deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
}

// CancelDeletion cancels a pending deletion request during grace period.
func (m *GDPRManager) CancelDeletion(ctx context.Context, deletionID, cancelledBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.deps.DeletionStorage.GetDeletionRequest(ctx, deletionID)
	if err != nil {
		return api.NewOperationFailedError("get deletion request", err)
	}

	if request.Status != DeletionStatusPending {
		return ErrDeletionInProgress
	}

	now := time.Now()
	request.Status = DeletionStatusCancelled
	request.CancelledAt = &now
	request.CancelledBy = cancelledBy

	if err := m.deps.DeletionStorage.UpdateDeletionRequest(ctx, request); err != nil {
		return api.NewOperationFailedError("cancel deletion", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, request.TenantID, cancelledBy, "data_deletion.cancelled", map[string]interface{}{
			"deletion_id":     deletionID,
			"subject_user_id": request.UserID,
		})
	}

	return nil
}

// ProcessDeletion executes a deletion request after grace period.
func (m *GDPRManager) ProcessDeletion(ctx context.Context, deletionID string) (*DeletionSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.deps.DeletionStorage.GetDeletionRequest(ctx, deletionID)
	if err != nil {
		return nil, api.NewOperationFailedError("get deletion request", err)
	}

	if request.Status != DeletionStatusPending {
		return nil, api.NewInvalidInputError("deletion request is not in pending status")
	}

	// Check grace period
	if request.GracePeriodEndsAt != nil && time.Now().Before(*request.GracePeriodEndsAt) {
		return nil, ErrGracePeriodActive
	}

	// Update status to processing
	now := time.Now()
	request.Status = DeletionStatusProcessing
	request.StartedAt = &now
	if err := m.deps.DeletionStorage.UpdateDeletionRequest(ctx, request); err != nil {
		return nil, api.NewOperationFailedError("update deletion status", err)
	}

	// Execute deletion
	summary, err := m.deleteUserData(ctx, request)
	if err != nil {
		// Mark as failed
		request.Status = DeletionStatusFailed
		request.ErrorMessage = err.Error()
		completedAt := time.Now()
		request.CompletedAt = &completedAt
		_ = m.deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
		return nil, api.NewOperationFailedError("delete user data", err)
	}

	// Update status to completed
	completedAt := time.Now()
	request.Status = DeletionStatusCompleted
	request.CompletedAt = &completedAt
	request.DeletedDataSummary = summary

	if err := m.deps.DeletionStorage.UpdateDeletionRequest(ctx, request); err != nil {
		return nil, api.NewOperationFailedError("update deletion completion", err)
	}

	// Audit log (optional)
	if m.deps.AuditLogger != nil {
		_ = m.deps.AuditLogger.LogGDPREvent(ctx, request.TenantID, request.RequestedBy, "data_deletion.completed", map[string]interface{}{
			"deletion_id":     deletionID,
			"subject_user_id": request.UserID,
			"summary":         summary,
		})
	}

	return summary, nil
}

// GetDeletionStatus retrieves the status of a deletion request.
func (m *GDPRManager) GetDeletionStatus(ctx context.Context, deletionID string) (*DataDeletionRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deps.DeletionStorage.GetDeletionRequest(ctx, deletionID)
}

// deleteUserData performs the actual data deletion.
func (m *GDPRManager) deleteUserData(ctx context.Context, request *DataDeletionRequest) (*DeletionSummary, error) {
	summary := &DeletionSummary{
		AdditionalData: make(map[string]int),
	}

	// Determine what to delete (everything except preserved)
	shouldDelete := func(category string) bool {
		// Check preserve list
		for _, preserve := range request.PreserveCategories {
			if preserve == category {
				return false
			}
		}

		// If delete list is empty, delete all (except preserved)
		if len(request.DeleteCategories) == 0 {
			return true
		}

		// Check delete list
		for _, del := range request.DeleteCategories {
			if del == category {
				return true
			}
		}

		return false
	}

	// Delete sessions
	if shouldDelete("sessions") && m.deps.SessionStorage != nil {
		sessions, _ := m.deps.SessionStorage.GetSessionsByUser(ctx, request.TenantID, request.UserID)
		for _, session := range sessions {
			_ = m.deps.SessionStorage.DeleteSession(ctx, session.ID)
			summary.SessionsDeleted++
		}
	}

	// NOTE: WebAuthn credential deletion is now owned by go-xkms; see
	// the export-side note above. The CredentialsDeleted counter is
	// retained for wire-format stability but stays at zero until the
	// xkms-backed deletion hook lands.

	// Delete consents
	if shouldDelete("consents") {
		consents, _ := m.deps.ConsentStorage.GetUserConsents(ctx, request.TenantID, request.UserID)
		for _, consent := range consents {
			_ = m.deps.ConsentStorage.DeleteConsent(ctx, consent.ID)
			summary.ConsentsDeleted++
		}
	}

	// Anonymize audit logs (don't delete for compliance)
	if shouldDelete("audit_logs") && m.deps.AuditLogger != nil {
		// Note: Actual anonymization would be implemented in the audit logger
		// For now, we just mark it in the summary
		summary.AuditLogsAnonymized = 0 // Would be populated by actual anonymization
	}

	// Delete user profile (last step)
	if shouldDelete("profile") {
		if err := m.deps.UserStorage.DeleteUser(ctx, request.TenantID, request.UserID); err != nil {
			return nil, api.NewOperationFailedError("delete user profile", err)
		}
		summary.ProfileDeleted = true
	}

	return summary, nil
}

// generateID generates a cryptographically secure random ID.
func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// ExportFailedError indicates a data export request has failed.
type ExportFailedError struct {
	Message string
}

// Error implements the error interface.
func (e *ExportFailedError) Error() string {
	return "export request failed: " + e.Message
}

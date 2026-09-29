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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ==================== Test Fixtures and Helpers ====================

// MockGDPRAuditLogger is a mock implementation of GDPRAuditLogger for testing.
type MockGDPRAuditLogger struct {
	events []GDPRAuditEvent
}

type GDPRAuditEvent struct {
	TenantID string
	UserID   string
	Action   string
	Details  map[string]interface{}
}

func (m *MockGDPRAuditLogger) LogGDPREvent(ctx context.Context, tenantID, userID, action string, details map[string]interface{}) error {
	m.events = append(m.events, GDPRAuditEvent{
		TenantID: tenantID,
		UserID:   userID,
		Action:   action,
		Details:  details,
	})
	return nil
}

func (m *MockGDPRAuditLogger) GetEvents() []GDPRAuditEvent {
	return m.events
}

func (m *MockGDPRAuditLogger) Clear() {
	m.events = nil
}

// Test fixture helpers

func createTestGDPRManager(t *testing.T) (*GDPRManager, *MockGDPRAuditLogger) {
	t.Helper()

	auditLogger := &MockGDPRAuditLogger{}
	userStorage := NewMemoryStorage() // MemoryStorage implements UserStorage
	sessionStorage := NewMemorySessionStorage()

	deps := GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
		UserStorage:       userStorage,
		SessionStorage:    sessionStorage,
		AuditLogger:       auditLogger,
	}

	manager, err := NewGDPRManager(deps)
	require.NoError(t, err)
	require.NotNil(t, manager)

	return manager, auditLogger
}

func createTestUser(t *testing.T, userStorage UserStorage, tenantID, userID string) *User {
	t.Helper()

	user := &User{
		ID:         userID,
		TenantID:   tenantID,
		Email:      fmt.Sprintf("%s@example.com", userID),
		Name:       fmt.Sprintf("Test User %s", userID),
		Attributes: map[string]string{"test": "value"},
		Created:    time.Now(),
		Updated:    time.Now(),
	}

	err := userStorage.CreateUser(context.Background(), user)
	require.NoError(t, err)

	return user
}

func createTestSession(t *testing.T, sessionStorage SessionStorage, tenantID, userID string) *Session {
	t.Helper()

	session := &Session{
		ID:           fmt.Sprintf("session_%s", userID),
		TenantID:     tenantID,
		UserID:       userID,
		Created:      time.Now(),
		Expires:      time.Now().Add(24 * time.Hour),
		LastActivity: time.Now(),
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
		AuthMethod:   "password",
	}

	err := sessionStorage.CreateSession(context.Background(), session)
	require.NoError(t, err)

	return session
}

// ==================== GDPRManager Constructor Tests ====================

func TestNewGDPRManager_Success(t *testing.T) {
	deps := GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
		UserStorage:       NewMemoryStorage(),
	}

	manager, err := NewGDPRManager(deps)
	require.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, DefaultGracePeriodDays*24*time.Hour, manager.defaultGracePeriod)
	assert.Equal(t, DefaultExportDownloadTTL, manager.exportDownloadTTL)
}

func TestNewGDPRManager_MissingConsentStorage(t *testing.T) {
	deps := GDPRManagerDependencies{
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
		UserStorage:       NewMemoryStorage(),
	}

	manager, err := NewGDPRManager(deps)
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "consent storage is required")
}

func TestNewGDPRManager_MissingDataExportStorage(t *testing.T) {
	deps := GDPRManagerDependencies{
		ConsentStorage:  NewMemoryConsentStorage(),
		DeletionStorage: NewMemoryDeletionStorage(),
		UserStorage:     NewMemoryStorage(),
	}

	manager, err := NewGDPRManager(deps)
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "data export storage is required")
}

func TestNewGDPRManager_MissingDeletionStorage(t *testing.T) {
	deps := GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		UserStorage:       NewMemoryStorage(),
	}

	manager, err := NewGDPRManager(deps)
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "deletion storage is required")
}

func TestNewGDPRManager_MissingUserStorage(t *testing.T) {
	deps := GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
	}

	manager, err := NewGDPRManager(deps)
	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "user storage is required")
}

// ==================== Consent Management Tests ====================

func TestGDPRManager_GrantConsent_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"
	purpose := ConsentPurposeMarketing
	version := "1.0"
	expiresAt := time.Now().Add(365 * 24 * time.Hour)
	ipAddress := "192.168.1.1"
	userAgent := "Mozilla/5.0"

	consent, err := manager.GrantConsent(ctx, tenantID, userID, purpose, version, &expiresAt, ipAddress, userAgent)
	require.NoError(t, err)
	require.NotNil(t, consent)

	assert.NotEmpty(t, consent.ID)
	assert.Equal(t, userID, consent.UserID)
	assert.Equal(t, tenantID, consent.TenantID)
	assert.Equal(t, purpose, consent.Purpose)
	assert.Equal(t, ConsentStatusGranted, consent.Status)
	assert.Equal(t, version, consent.Version)
	assert.NotNil(t, consent.GrantedAt)
	assert.NotNil(t, consent.ExpiresAt)
	assert.Equal(t, ipAddress, consent.IPAddress)
	assert.Equal(t, userAgent, consent.UserAgent)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "consent.granted", events[0].Action)
	assert.Equal(t, tenantID, events[0].TenantID)
	assert.Equal(t, userID, events[0].UserID)
}

func TestGDPRManager_GrantConsent_InvalidPurpose(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	consent, err := manager.GrantConsent(ctx, "tenant1", "11", "invalid_purpose", "1.0", nil, "", "")
	require.Error(t, err)
	assert.Nil(t, consent)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrValidation, qbErr.Code)
}

func TestGDPRManager_GrantConsent_AllPurposes(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	purposes := []ConsentPurpose{
		ConsentPurposeMarketing,
		ConsentPurposeAnalytics,
		ConsentPurposePersonalization,
		ConsentPurposeThirdParty,
		ConsentPurposeDataProcessing,
		ConsentPurposeDataRetention,
	}

	for _, purpose := range purposes {
		consent, err := manager.GrantConsent(ctx, "tenant1", "11", purpose, "1.0", nil, "", "")
		require.NoError(t, err, "Failed for purpose: %s", purpose)
		assert.Equal(t, purpose, consent.Purpose)
	}
}

func TestGDPRManager_WithdrawConsent_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"
	purpose := ConsentPurposeMarketing

	// Grant consent first
	consent, err := manager.GrantConsent(ctx, tenantID, userID, purpose, "1.0", nil, "192.168.1.1", "Mozilla")
	require.NoError(t, err)
	require.NotNil(t, consent.GrantedAt)
	assert.Nil(t, consent.WithdrawnAt)

	auditLogger.Clear()

	// Withdraw consent
	err = manager.WithdrawConsent(ctx, tenantID, userID, purpose, "192.168.1.2", "Chrome")
	require.NoError(t, err)

	// Verify withdrawal
	withdrawn, err := manager.GetConsent(ctx, tenantID, userID, purpose)
	require.NoError(t, err)
	assert.Equal(t, ConsentStatusWithdrawn, withdrawn.Status)
	assert.NotNil(t, withdrawn.WithdrawnAt)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "consent.withdrawn", events[0].Action)
}

func TestGDPRManager_WithdrawConsent_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	err := manager.WithdrawConsent(ctx, "tenant1", "11", ConsentPurposeMarketing, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not_found")
}

func TestGDPRManager_GetConsent_Found(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"
	purpose := ConsentPurposeAnalytics

	// Grant consent
	original, err := manager.GrantConsent(ctx, tenantID, userID, purpose, "1.0", nil, "", "")
	require.NoError(t, err)

	// Retrieve consent
	retrieved, err := manager.GetConsent(ctx, tenantID, userID, purpose)
	require.NoError(t, err)
	assert.Equal(t, original.ID, retrieved.ID)
	assert.Equal(t, original.Status, retrieved.Status)
}

func TestGDPRManager_GetConsent_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	consent, err := manager.GetConsent(ctx, "tenant1", "11", ConsentPurposeMarketing)
	require.Error(t, err)
	assert.Nil(t, consent)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
}

func TestGDPRManager_ListUserConsents(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Grant multiple consents
	purposes := []ConsentPurpose{
		ConsentPurposeMarketing,
		ConsentPurposeAnalytics,
		ConsentPurposePersonalization,
	}

	for _, purpose := range purposes {
		_, err := manager.GrantConsent(ctx, tenantID, userID, purpose, "1.0", nil, "", "")
		require.NoError(t, err)
	}

	// List user consents
	consents, err := manager.ListUserConsents(ctx, tenantID, userID)
	require.NoError(t, err)
	assert.Len(t, consents, 3)

	// Verify all purposes are present
	foundPurposes := make(map[ConsentPurpose]bool)
	for _, consent := range consents {
		foundPurposes[consent.Purpose] = true
	}
	for _, purpose := range purposes {
		assert.True(t, foundPurposes[purpose], "Missing purpose: %s", purpose)
	}
}

func TestGDPRManager_ListUserConsents_Empty(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	consents, err := manager.ListUserConsents(ctx, "tenant1", "11")
	require.NoError(t, err)
	assert.Empty(t, consents)
}

// ==================== Data Export Tests ====================

func TestGDPRManager_RequestExport_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"
	requestedBy := "11"

	request, err := manager.RequestExport(ctx, tenantID, userID, requestedBy, DataExportFormatJSON,
		[]string{"profile", "sessions"}, []string{"audit_logs"})
	require.NoError(t, err)
	require.NotNil(t, request)

	assert.NotEmpty(t, request.ID)
	assert.Equal(t, userID, request.UserID)
	assert.Equal(t, tenantID, request.TenantID)
	assert.Equal(t, requestedBy, request.RequestedBy)
	assert.Equal(t, DataExportFormatJSON, request.Format)
	assert.Equal(t, DataExportStatusPending, request.Status)
	assert.Equal(t, []string{"profile", "sessions"}, request.IncludeCategories)
	assert.Equal(t, []string{"audit_logs"}, request.ExcludeCategories)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "data_export.requested", events[0].Action)
}

func TestGDPRManager_RequestExport_InvalidCategory(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	request, err := manager.RequestExport(ctx, "tenant1", "11", "11", DataExportFormatJSON,
		[]string{"invalid_category"}, nil)
	require.Error(t, err)
	assert.Nil(t, request)
	assert.Contains(t, err.Error(), "invalid category")
}

func TestGDPRManager_RequestExport_PendingExists(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create first export request
	_, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON, nil, nil)
	require.NoError(t, err)

	// Try to create another while first is pending
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON, nil, nil)
	require.Error(t, err)
	assert.Nil(t, request)
	assert.ErrorIs(t, err, ErrDataExportPending)
}

func TestGDPRManager_RequestExport_AllFormats(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	formats := []DataExportFormat{
		DataExportFormatJSON,
		DataExportFormatCSV,
		DataExportFormatXML,
	}

	for i, format := range formats {
		userID := FormatUserID(uint64(i) + 10)
		request, err := manager.RequestExport(ctx, "tenant1", userID, userID, format, nil, nil)
		require.NoError(t, err, "Failed for format: %s", format)
		assert.Equal(t, format, request.Format)
	}
}

func TestGDPRManager_GetExportStatus(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Create export request
	original, err := manager.RequestExport(ctx, "tenant1", "11", "11", DataExportFormatJSON, nil, nil)
	require.NoError(t, err)

	// Get status
	status, err := manager.GetExportStatus(ctx, original.ID)
	require.NoError(t, err)
	assert.Equal(t, original.ID, status.ID)
	assert.Equal(t, DataExportStatusPending, status.Status)
}

func TestGDPRManager_GetExportStatus_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	status, err := manager.GetExportStatus(ctx, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, status)
}

func TestGDPRManager_GenerateExport_ProfileOnly(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create user
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Request export
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON,
		[]string{"profile"}, nil)
	require.NoError(t, err)

	// Generate export
	export, err := manager.GenerateExport(ctx, request.ID)
	require.NoError(t, err)
	require.NotNil(t, export)

	assert.Equal(t, request.ID, export.ExportMetadata.ExportID)
	assert.Equal(t, userID, export.ExportMetadata.UserID)
	assert.Equal(t, tenantID, export.ExportMetadata.TenantID)
	assert.NotNil(t, export.Profile)
	assert.Equal(t, userID, export.Profile.ID)
	assert.Nil(t, export.Sessions)
	assert.Nil(t, export.Consents)

	// Verify export request updated to completed
	status, err := manager.GetExportStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DataExportStatusCompleted, status.Status)
	assert.NotNil(t, status.CompletedAt)
	assert.NotNil(t, status.ExpiresAt)
	assert.NotEmpty(t, status.DownloadURL)
}

func TestGDPRManager_GenerateExport_AllCategories(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create test data
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)
	createTestSession(t, manager.deps.SessionStorage, tenantID, userID)
	_, err := manager.GrantConsent(ctx, tenantID, userID, ConsentPurposeMarketing, "1.0", nil, "", "")
	require.NoError(t, err)

	// Request export with all categories
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON, nil, nil)
	require.NoError(t, err)

	// Generate export
	export, err := manager.GenerateExport(ctx, request.ID)
	require.NoError(t, err)

	assert.NotNil(t, export.Profile)
	assert.NotNil(t, export.Sessions)
	assert.Len(t, export.Sessions, 1)
	assert.NotNil(t, export.Consents)
	assert.Len(t, export.Consents, 1)
	// WebAuthn credentials are owned by go-xkms after Phase 5; the
	// quicbaas-side GDPR export no longer enumerates them.
	assert.Nil(t, export.Credentials)
}

func TestGDPRManager_GenerateExport_WithExclusions(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create test data
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)
	createTestSession(t, manager.deps.SessionStorage, tenantID, userID)

	// Request export excluding sessions
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON,
		nil, []string{"sessions"})
	require.NoError(t, err)

	// Generate export
	export, err := manager.GenerateExport(ctx, request.ID)
	require.NoError(t, err)

	assert.NotNil(t, export.Profile)
	assert.Nil(t, export.Sessions) // Should be excluded
}

func TestGDPRManager_GenerateExport_UserNotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Request export for non-existent user
	request, err := manager.RequestExport(ctx, "tenant1", "nonexistent", "admin", DataExportFormatJSON,
		[]string{"profile"}, nil)
	require.NoError(t, err)

	// Generate export should fail
	export, err := manager.GenerateExport(ctx, request.ID)
	require.Error(t, err)
	assert.Nil(t, export)

	// Verify request marked as failed
	status, err := manager.GetExportStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DataExportStatusFailed, status.Status)
	assert.NotEmpty(t, status.ErrorMessage)
}

// ==================== Data Deletion Tests ====================

func TestGDPRManager_RequestDeletion_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"
	reason := "User requested account deletion"

	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, reason,
		[]string{"profile", "sessions"}, []string{"audit_logs"}, 7)
	require.NoError(t, err)
	require.NotNil(t, request)

	assert.NotEmpty(t, request.ID)
	assert.Equal(t, userID, request.UserID)
	assert.Equal(t, tenantID, request.TenantID)
	assert.Equal(t, reason, request.Reason)
	assert.Equal(t, DeletionStatusPending, request.Status)
	assert.Equal(t, []string{"profile", "sessions"}, request.DeleteCategories)
	assert.Equal(t, []string{"audit_logs"}, request.PreserveCategories)
	assert.NotNil(t, request.GracePeriodEndsAt)

	// Verify grace period
	expectedEnd := time.Now().Add(7 * 24 * time.Hour)
	assert.WithinDuration(t, expectedEnd, *request.GracePeriodEndsAt, 5*time.Second)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "data_deletion.requested", events[0].Action)
}

func TestGDPRManager_RequestDeletion_DefaultGracePeriod(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Pass -1 to use default grace period
	request, err := manager.RequestDeletion(ctx, "tenant1", "11", "11", "", nil, nil, -1)
	require.NoError(t, err)
	require.NotNil(t, request.GracePeriodEndsAt)

	expectedEnd := time.Now().Add(DefaultGracePeriodDays * 24 * time.Hour)
	assert.WithinDuration(t, expectedEnd, *request.GracePeriodEndsAt, 5*time.Second)
}

func TestGDPRManager_RequestDeletion_ImmediateDeletion(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Pass 0 for immediate deletion (no grace period)
	request, err := manager.RequestDeletion(ctx, "tenant1", "user2", "user2", "", nil, nil, 0)
	require.NoError(t, err)

	// For immediate deletion (0 days), GracePeriodEndsAt should be nil
	assert.Nil(t, request.GracePeriodEndsAt, "Immediate deletion should have no grace period")
}

func TestGDPRManager_RequestDeletion_InvalidCategory(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	request, err := manager.RequestDeletion(ctx, "tenant1", "11", "11", "",
		[]string{"invalid_category"}, nil, 7)
	require.Error(t, err)
	assert.Nil(t, request)
	assert.Contains(t, err.Error(), "invalid delete category")
}

func TestGDPRManager_RequestDeletion_InvalidPreserveCategory(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	request, err := manager.RequestDeletion(ctx, "tenant1", "11", "11", "",
		nil, []string{"invalid_category"}, 7)
	require.Error(t, err)
	assert.Nil(t, request)
	assert.Contains(t, err.Error(), "invalid preserve category")
}

func TestGDPRManager_RequestDeletion_PendingExists(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create first deletion request
	_, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 7)
	require.NoError(t, err)

	// Try to create another while first is pending
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 7)
	require.Error(t, err)
	assert.Nil(t, request)
	assert.ErrorIs(t, err, ErrDeletionPending)
}

func TestGDPRManager_CancelDeletion_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create deletion request
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 7)
	require.NoError(t, err)

	auditLogger.Clear()

	// Cancel deletion
	err = manager.CancelDeletion(ctx, request.ID, userID)
	require.NoError(t, err)

	// Verify cancellation
	cancelled, err := manager.GetDeletionStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DeletionStatusCancelled, cancelled.Status)
	assert.NotNil(t, cancelled.CancelledAt)
	assert.Equal(t, userID, cancelled.CancelledBy)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "data_deletion.cancelled", events[0].Action)
}

func TestGDPRManager_CancelDeletion_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	err := manager.CancelDeletion(ctx, "nonexistent", "11")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get deletion request failed")
}

func TestGDPRManager_CancelDeletion_AlreadyProcessing(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Create deletion request
	request, err := manager.RequestDeletion(ctx, "tenant1", "11", "11", "", nil, nil, 7)
	require.NoError(t, err)

	// Manually set to processing status
	request.Status = DeletionStatusProcessing
	err = manager.deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
	require.NoError(t, err)

	// Try to cancel
	err = manager.CancelDeletion(ctx, request.ID, "11")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeletionInProgress)
}

func TestGDPRManager_ProcessDeletion_GracePeriodActive(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create user
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Create deletion request with future grace period
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 7)
	require.NoError(t, err)

	// Try to process during grace period
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.Error(t, err)
	assert.Nil(t, summary)
	assert.ErrorIs(t, err, ErrGracePeriodActive)
}

func TestGDPRManager_ProcessDeletion_Success(t *testing.T) {
	manager, auditLogger := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create test data
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)
	createTestSession(t, manager.deps.SessionStorage, tenantID, userID)
	_, err := manager.GrantConsent(ctx, tenantID, userID, ConsentPurposeMarketing, "1.0", nil, "", "")
	require.NoError(t, err)

	// Create deletion request with immediate deletion (0 days grace period)
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 0)
	require.NoError(t, err)

	auditLogger.Clear()

	// Process deletion - should succeed immediately since grace period is 0
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.True(t, summary.ProfileDeleted)
	assert.Equal(t, 1, summary.SessionsDeleted)
	assert.Equal(t, 0, summary.CredentialsDeleted) // no WebAuthn reaper wired in this fixture
	assert.Equal(t, 1, summary.ConsentsDeleted)

	// Verify deletion request updated
	deleted, err := manager.GetDeletionStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DeletionStatusCompleted, deleted.Status)
	assert.NotNil(t, deleted.CompletedAt)

	// Verify user actually deleted
	_, err = manager.deps.UserStorage.GetUser(ctx, tenantID, userID)
	require.Error(t, err)

	// Verify audit log
	events := auditLogger.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "data_deletion.completed", events[0].Action)
}

func TestGDPRManager_ProcessDeletion_PartialDeletion(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create test data
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)
	createTestSession(t, manager.deps.SessionStorage, tenantID, userID)

	// Request deletion of sessions only, preserve profile
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "",
		[]string{"sessions"}, []string{"profile"}, 0)
	require.NoError(t, err)

	// Process deletion
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.NoError(t, err)

	assert.False(t, summary.ProfileDeleted) // Profile preserved
	assert.Equal(t, 1, summary.SessionsDeleted)

	// Verify user still exists
	user, err := manager.deps.UserStorage.GetUser(ctx, tenantID, userID)
	require.NoError(t, err)
	assert.NotNil(t, user)
}

func TestGDPRManager_ProcessDeletion_DeleteAll(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create test data
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)
	createTestSession(t, manager.deps.SessionStorage, tenantID, userID)

	// Request complete deletion (empty delete list means delete all)
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 0)
	require.NoError(t, err)

	// Process deletion
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.NoError(t, err)

	assert.True(t, summary.ProfileDeleted)
	assert.Equal(t, 1, summary.SessionsDeleted)
	assert.Equal(t, 0, summary.CredentialsDeleted) // no WebAuthn reaper wired in this fixture
}

func TestGDPRManager_GetDeletionStatus(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Create deletion request
	original, err := manager.RequestDeletion(ctx, "tenant1", "11", "11", "", nil, nil, 7)
	require.NoError(t, err)

	// Get status
	status, err := manager.GetDeletionStatus(ctx, original.ID)
	require.NoError(t, err)
	assert.Equal(t, original.ID, status.ID)
	assert.Equal(t, DeletionStatusPending, status.Status)
}

func TestGDPRManager_GetDeletionStatus_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	status, err := manager.GetDeletionStatus(ctx, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, status)
}

// ==================== Helper Function Tests ====================

func TestIsValidConsentPurpose(t *testing.T) {
	tests := []struct {
		purpose ConsentPurpose
		valid   bool
	}{
		{ConsentPurposeMarketing, true},
		{ConsentPurposeAnalytics, true},
		{ConsentPurposePersonalization, true},
		{ConsentPurposeThirdParty, true},
		{ConsentPurposeDataProcessing, true},
		{ConsentPurposeDataRetention, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.purpose), func(t *testing.T) {
			assert.Equal(t, tt.valid, IsValidConsentPurpose(tt.purpose))
		})
	}
}

func TestIsValidConsentStatus(t *testing.T) {
	tests := []struct {
		status ConsentStatus
		valid  bool
	}{
		{ConsentStatusGranted, true},
		{ConsentStatusDenied, true},
		{ConsentStatusWithdrawn, true},
		{ConsentStatusPending, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.valid, IsValidConsentStatus(tt.status))
		})
	}
}

func TestIsValidDataCategory(t *testing.T) {
	validCategories := []string{
		"profile", "sessions", "consents",
		"roles", "audit_logs", "credentials", "mfa",
	}

	for _, cat := range validCategories {
		t.Run(cat, func(t *testing.T) {
			assert.True(t, IsValidDataCategory(cat))
		})
	}

	// "groups" is not a category: quicbaas keeps no groups to export or erase.
	invalidCategories := []string{"invalid", "", "unknown", "groups"}
	for _, cat := range invalidCategories {
		t.Run(cat, func(t *testing.T) {
			assert.False(t, IsValidDataCategory(cat))
		})
	}
}

func TestConsent_Validate_Success(t *testing.T) {
	consent := &Consent{
		UserID:   "11",
		TenantID: "tenant1",
		Purpose:  ConsentPurposeMarketing,
		Version:  "1.0",
	}

	err := consent.Validate()
	assert.NoError(t, err)
}

func TestConsent_Validate_MissingUserID(t *testing.T) {
	consent := &Consent{
		TenantID: "tenant1",
		Purpose:  ConsentPurposeMarketing,
		Version:  "1.0",
	}

	err := consent.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_id is required")
}

func TestConsent_Validate_MissingTenantID(t *testing.T) {
	consent := &Consent{
		UserID:  "11",
		Purpose: ConsentPurposeMarketing,
		Version: "1.0",
	}

	err := consent.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id is required")
}

func TestConsent_Validate_MissingPurpose(t *testing.T) {
	consent := &Consent{
		UserID:   "11",
		TenantID: "tenant1",
		Version:  "1.0",
	}

	err := consent.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "purpose is required")
}

func TestConsent_Validate_MissingVersion(t *testing.T) {
	consent := &Consent{
		UserID:   "11",
		TenantID: "tenant1",
		Purpose:  ConsentPurposeMarketing,
	}

	err := consent.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version is required")
}

// ==================== MemoryConsentStorage Tests ====================

func TestMemoryConsentStorage_CreateConsent_Success(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	opts := CreateConsentOptions{
		UserID:    "11",
		TenantID:  "tenant1",
		Purpose:   ConsentPurposeMarketing,
		Status:    ConsentStatusGranted,
		Version:   "1.0",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
		Metadata:  map[string]string{"key": "value"},
	}

	consent, err := storage.CreateConsent(ctx, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, consent.ID)
	assert.Equal(t, opts.UserID, consent.UserID)
	assert.Equal(t, opts.Status, consent.Status)
	assert.NotNil(t, consent.GrantedAt)
	assert.Equal(t, "value", consent.Metadata["key"])
}

func TestMemoryConsentStorage_CreateConsent_DuplicatePurpose(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	opts := CreateConsentOptions{
		UserID:   "11",
		TenantID: "tenant1",
		Purpose:  ConsentPurposeMarketing,
		Status:   ConsentStatusGranted,
		Version:  "1.0",
	}

	// Create first consent
	_, err := storage.CreateConsent(ctx, opts)
	require.NoError(t, err)

	// Try to create duplicate
	_, err = storage.CreateConsent(ctx, opts)
	require.Error(t, err)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrAlreadyExists, qbErr.Code)
}

func TestMemoryConsentStorage_CreateConsent_ValidationErrors(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	tests := []struct {
		name string
		opts CreateConsentOptions
		err  string
	}{
		{
			name: "missing user_id",
			opts: CreateConsentOptions{
				TenantID: "tenant1",
				Purpose:  ConsentPurposeMarketing,
				Version:  "1.0",
			},
			err: "user_id is required",
		},
		{
			name: "missing tenant_id",
			opts: CreateConsentOptions{
				UserID:  "11",
				Purpose: ConsentPurposeMarketing,
				Version: "1.0",
			},
			err: "tenant_id is required",
		},
		{
			name: "missing purpose",
			opts: CreateConsentOptions{
				UserID:   "11",
				TenantID: "tenant1",
				Version:  "1.0",
			},
			err: "purpose is required",
		},
		{
			name: "missing version",
			opts: CreateConsentOptions{
				UserID:   "11",
				TenantID: "tenant1",
				Purpose:  ConsentPurposeMarketing,
			},
			err: "version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := storage.CreateConsent(ctx, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestMemoryConsentStorage_GetUserConsents(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create multiple consents
	purposes := []ConsentPurpose{ConsentPurposeMarketing, ConsentPurposeAnalytics}
	for _, purpose := range purposes {
		opts := CreateConsentOptions{
			UserID:   userID,
			TenantID: tenantID,
			Purpose:  purpose,
			Status:   ConsentStatusGranted,
			Version:  "1.0",
		}
		_, err := storage.CreateConsent(ctx, opts)
		require.NoError(t, err)
	}

	// Get user consents
	consents, err := storage.GetUserConsents(ctx, tenantID, userID)
	require.NoError(t, err)
	assert.Len(t, consents, 2)
}

func TestMemoryConsentStorage_ListConsents_WithFilters(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	// Create test consents
	for i := 0; i < 5; i++ {
		opts := CreateConsentOptions{
			UserID:   FormatUserID(uint64(i) + 10),
			TenantID: "tenant1",
			Purpose:  ConsentPurposeMarketing,
			Status:   ConsentStatusGranted,
			Version:  "1.0",
		}
		_, err := storage.CreateConsent(ctx, opts)
		require.NoError(t, err)
	}

	// Test user filter
	consents, err := storage.ListConsents(ctx, ListConsentsOptions{UserID: "11"})
	require.NoError(t, err)
	assert.Len(t, consents, 1)

	// Test tenant filter
	consents, err = storage.ListConsents(ctx, ListConsentsOptions{TenantID: "tenant1"})
	require.NoError(t, err)
	assert.Len(t, consents, 5)

	// Test purpose filter
	consents, err = storage.ListConsents(ctx, ListConsentsOptions{Purpose: ConsentPurposeMarketing})
	require.NoError(t, err)
	assert.Len(t, consents, 5)

	// Test status filter
	consents, err = storage.ListConsents(ctx, ListConsentsOptions{Status: ConsentStatusGranted})
	require.NoError(t, err)
	assert.Len(t, consents, 5)
}

func TestMemoryConsentStorage_ListConsents_Pagination(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	// Create test consents
	for i := 0; i < 10; i++ {
		opts := CreateConsentOptions{
			UserID:   FormatUserID(uint64(i) + 10),
			TenantID: "tenant1",
			Purpose:  ConsentPurposeMarketing,
			Status:   ConsentStatusGranted,
			Version:  "1.0",
		}
		_, err := storage.CreateConsent(ctx, opts)
		require.NoError(t, err)
	}

	// Test limit
	consents, err := storage.ListConsents(ctx, ListConsentsOptions{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, consents, 5)

	// Test offset
	consents, err = storage.ListConsents(ctx, ListConsentsOptions{Offset: 5, Limit: 5})
	require.NoError(t, err)
	assert.Len(t, consents, 5)

	// Test offset beyond range
	consents, err = storage.ListConsents(ctx, ListConsentsOptions{Offset: 20})
	require.NoError(t, err)
	assert.Empty(t, consents)
}

func TestMemoryConsentStorage_DeleteConsent(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	opts := CreateConsentOptions{
		UserID:   "11",
		TenantID: "tenant1",
		Purpose:  ConsentPurposeMarketing,
		Status:   ConsentStatusGranted,
		Version:  "1.0",
	}

	consent, err := storage.CreateConsent(ctx, opts)
	require.NoError(t, err)

	// Delete consent
	err = storage.DeleteConsent(ctx, consent.ID)
	require.NoError(t, err)

	// Verify deletion
	_, err = storage.GetConsent(ctx, "tenant1", "11", ConsentPurposeMarketing)
	require.Error(t, err)
}

// ==================== MemoryDataExportStorage Tests ====================

func TestMemoryDataExportStorage_CreateExportRequest_Success(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	opts := CreateDataExportOptions{
		UserID:            "11",
		TenantID:          "tenant1",
		RequestedBy:       "admin",
		Format:            DataExportFormatJSON,
		IncludeCategories: []string{"profile"},
		ExcludeCategories: []string{"audit_logs"},
		Metadata:          map[string]string{"key": "value"},
	}

	request, err := storage.CreateExportRequest(ctx, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, request.ID)
	assert.Equal(t, opts.UserID, request.UserID)
	assert.Equal(t, DataExportStatusPending, request.Status)
	assert.Equal(t, "value", request.Metadata["key"])
}

func TestMemoryDataExportStorage_CreateExportRequest_ValidationErrors(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	tests := []struct {
		name string
		opts CreateDataExportOptions
		err  string
	}{
		{
			name: "missing user_id",
			opts: CreateDataExportOptions{
				TenantID:    "tenant1",
				RequestedBy: "admin",
			},
			err: "user_id is required",
		},
		{
			name: "missing tenant_id",
			opts: CreateDataExportOptions{
				UserID:      "11",
				RequestedBy: "admin",
			},
			err: "tenant_id is required",
		},
		{
			name: "missing requested_by",
			opts: CreateDataExportOptions{
				UserID:   "11",
				TenantID: "tenant1",
			},
			err: "requested_by is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := storage.CreateExportRequest(ctx, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestMemoryDataExportStorage_ListExportRequests_WithFilters(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create test requests
	for i := 0; i < 5; i++ {
		opts := CreateDataExportOptions{
			UserID:      FormatUserID(uint64(i) + 10),
			TenantID:    "tenant1",
			RequestedBy: "admin",
			Format:      DataExportFormatJSON,
		}
		_, err := storage.CreateExportRequest(ctx, opts)
		require.NoError(t, err)
	}

	// Test user filter
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{UserID: "11"})
	require.NoError(t, err)
	assert.Len(t, requests, 1)

	// Test status filter
	requests, err = storage.ListExportRequests(ctx, ListExportRequestsOptions{Status: DataExportStatusPending})
	require.NoError(t, err)
	assert.Len(t, requests, 5)
}

// ==================== MemoryDeletionStorage Tests ====================

func TestMemoryDeletionStorage_CreateDeletionRequest_Success(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	opts := CreateDeletionRequestOptions{
		UserID:             "11",
		TenantID:           "tenant1",
		RequestedBy:        "11",
		Reason:             "Account closure",
		DeleteCategories:   []string{"profile"},
		PreserveCategories: []string{"audit_logs"},
		GracePeriodDays:    7,
		Metadata:           map[string]string{"key": "value"},
	}

	request, err := storage.CreateDeletionRequest(ctx, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, request.ID)
	assert.Equal(t, opts.UserID, request.UserID)
	assert.Equal(t, DeletionStatusPending, request.Status)
	assert.NotNil(t, request.GracePeriodEndsAt)
	assert.Equal(t, "value", request.Metadata["key"])
}

func TestMemoryDeletionStorage_CreateDeletionRequest_ValidationErrors(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	tests := []struct {
		name string
		opts CreateDeletionRequestOptions
		err  string
	}{
		{
			name: "missing user_id",
			opts: CreateDeletionRequestOptions{
				TenantID:    "tenant1",
				RequestedBy: "admin",
			},
			err: "user_id is required",
		},
		{
			name: "missing tenant_id",
			opts: CreateDeletionRequestOptions{
				UserID:      "11",
				RequestedBy: "admin",
			},
			err: "tenant_id is required",
		},
		{
			name: "missing requested_by",
			opts: CreateDeletionRequestOptions{
				UserID:   "11",
				TenantID: "tenant1",
			},
			err: "requested_by is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := storage.CreateDeletionRequest(ctx, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestMemoryDeletionStorage_ListDeletionRequests_WithFilters(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create test requests
	for i := 0; i < 5; i++ {
		opts := CreateDeletionRequestOptions{
			UserID:      FormatUserID(uint64(i) + 10),
			TenantID:    "tenant1",
			RequestedBy: "admin",
		}
		_, err := storage.CreateDeletionRequest(ctx, opts)
		require.NoError(t, err)
	}

	// Test user filter
	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{UserID: "11"})
	require.NoError(t, err)
	assert.Len(t, requests, 1)

	// Test status filter
	requests, err = storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{Status: DeletionStatusPending})
	require.NoError(t, err)
	assert.Len(t, requests, 5)
}

// ==================== Edge Cases and Concurrency Tests ====================

func TestGDPRManager_ConcurrentConsentGrants(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			_, err := manager.GrantConsent(ctx, "tenant1", FormatUserID(uint64(id)+10),
				ConsentPurposeMarketing, "1.0", nil, "", "")
			if err != nil {
				errors <- err
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent grant failed: %v", err)
	}

	// Verify all consents created
	for i := 0; i < numGoroutines; i++ {
		consent, err := manager.GetConsent(ctx, "tenant1", FormatUserID(uint64(i)+10), ConsentPurposeMarketing)
		require.NoError(t, err)
		assert.NotNil(t, consent)
	}
}

func TestGDPRManager_ExportWithNoOptionalDeps(t *testing.T) {
	// Create manager without optional dependencies
	deps := GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: NewMemoryDataExportStorage(),
		DeletionStorage:   NewMemoryDeletionStorage(),
		UserStorage:       NewMemoryStorage(),
		// SessionStorage omitted; CredentialStorage was removed in Phase 5
	}

	manager, err := NewGDPRManager(deps)
	require.NoError(t, err)

	ctx := context.Background()
	tenantID := "tenant1"
	userID := "11"

	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Request and generate export
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON, nil, nil)
	require.NoError(t, err)

	export, err := manager.GenerateExport(ctx, request.ID)
	require.NoError(t, err)

	assert.NotNil(t, export.Profile)
	assert.Nil(t, export.Sessions)    // Should be nil without SessionStorage
	assert.Nil(t, export.Credentials) // Credentials are now sourced from xkms (Phase 5)
}

func TestGDPRManager_ProcessDeletion_NotFound(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	// Try to process non-existent deletion request
	summary, err := manager.ProcessDeletion(ctx, "nonexistent_id")
	require.Error(t, err)
	assert.Nil(t, summary)
	assert.Contains(t, err.Error(), "get deletion request failed")
}

func TestGDPRManager_ProcessDeletion_NotPending(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create user
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Create deletion request
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 0)
	require.NoError(t, err)

	// Manually set to completed status
	request.Status = DeletionStatusCompleted
	err = manager.deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
	require.NoError(t, err)

	// Try to process - should fail because not in pending state
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.Error(t, err)
	assert.Nil(t, summary)
	assert.Contains(t, err.Error(), "not in pending status")
}

func TestGDPRManager_ProcessDeletion_AlreadyProcessing(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create user
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Create deletion request
	request, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "", nil, nil, 0)
	require.NoError(t, err)

	// Manually set to processing status
	request.Status = DeletionStatusProcessing
	err = manager.deps.DeletionStorage.UpdateDeletionRequest(ctx, request)
	require.NoError(t, err)

	// Try to process again - should fail because already processing
	summary, err := manager.ProcessDeletion(ctx, request.ID)
	require.Error(t, err)
	assert.Nil(t, summary)
	assert.Contains(t, err.Error(), "not in pending status")
}

func TestGDPRManager_determineCategories(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant1"
	userID := "11"

	// Create user
	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	// Request export with specific categories
	request, err := manager.RequestExport(ctx, tenantID, userID, userID, DataExportFormatJSON,
		[]string{"profile"}, []string{"sessions"})
	require.NoError(t, err)
	require.NotNil(t, request)

	// Generate export with specified categories
	export, err := manager.GenerateExport(ctx, request.ID)
	require.NoError(t, err)
	assert.NotNil(t, export.Profile)
	assert.Nil(t, export.Sessions) // Excluded

	// Request export with exclude only
	request2, err := manager.RequestExport(ctx, tenantID, userID+"2", userID+"2", DataExportFormatJSON,
		nil, []string{"sessions", "credentials"})
	require.NoError(t, err)

	createTestUser(t, manager.deps.UserStorage, tenantID, userID+"2")

	export2, err := manager.GenerateExport(ctx, request2.ID)
	require.NoError(t, err)
	assert.NotNil(t, export2.Profile)
	assert.Nil(t, export2.Sessions)    // Excluded
	assert.Nil(t, export2.Credentials) // Excluded
}

// =============================================================================
// GDPR Error Type Tests
// =============================================================================

func TestExportFailedError_Error(t *testing.T) {
	t.Run("includes message", func(t *testing.T) {
		err := &ExportFailedError{Message: "disk full"}
		expected := "export request failed: disk full"
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("empty message", func(t *testing.T) {
		err := &ExportFailedError{}
		expected := "export request failed: "
		if result := err.Error(); result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})
}

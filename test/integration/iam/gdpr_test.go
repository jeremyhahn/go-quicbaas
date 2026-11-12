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

//go:build integration
// +build integration

package iam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ConsentRequest represents a request to grant consent.
type ConsentRequest struct {
	Purpose   string     `json:"purpose"`
	Version   string     `json:"version"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IPAddress string     `json:"ip_address,omitempty"`
	UserAgent string     `json:"user_agent,omitempty"`
}

// ConsentResponse represents a consent record response.
type ConsentResponse struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	TenantID    string     `json:"tenant_id"`
	Purpose     string     `json:"purpose"`
	Status      string     `json:"status"`
	Version     string     `json:"version"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	IPAddress   string     `json:"ip_address,omitempty"`
	UserAgent   string     `json:"user_agent,omitempty"`
	Created     time.Time  `json:"created"`
	Updated     time.Time  `json:"updated"`
}

// ExportRequest represents a data export request.
type ExportRequest struct {
	Format            string   `json:"format"`
	IncludeCategories []string `json:"include_categories,omitempty"`
	ExcludeCategories []string `json:"exclude_categories,omitempty"`
}

// ExportResponse represents a data export response.
type ExportResponse struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	TenantID          string     `json:"tenant_id"`
	Format            string     `json:"format"`
	Status            string     `json:"status"`
	IncludeCategories []string   `json:"include_categories,omitempty"`
	ExcludeCategories []string   `json:"exclude_categories,omitempty"`
	RequestedAt       time.Time  `json:"requested_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	DownloadURL       string     `json:"download_url,omitempty"`
	FileSizeBytes     int64      `json:"file_size_bytes,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
}

// DeletionRequest represents a data deletion request.
type DeletionRequest struct {
	Reason             string   `json:"reason,omitempty"`
	DeleteCategories   []string `json:"delete_categories,omitempty"`
	PreserveCategories []string `json:"preserve_categories,omitempty"`
	GracePeriodDays    int      `json:"grace_period_days"`
}

// DeletionResponse represents a data deletion response.
type DeletionResponse struct {
	ID                 string           `json:"id"`
	UserID             string           `json:"user_id"`
	TenantID           string           `json:"tenant_id"`
	Reason             string           `json:"reason,omitempty"`
	Status             string           `json:"status"`
	DeleteCategories   []string         `json:"delete_categories,omitempty"`
	PreserveCategories []string         `json:"preserve_categories,omitempty"`
	RequestedAt        time.Time        `json:"requested_at"`
	GracePeriodEndsAt  *time.Time       `json:"grace_period_ends_at,omitempty"`
	StartedAt          *time.Time       `json:"started_at,omitempty"`
	CompletedAt        *time.Time       `json:"completed_at,omitempty"`
	CancelledAt        *time.Time       `json:"cancelled_at,omitempty"`
	CancelledBy        string           `json:"cancelled_by,omitempty"`
	DeletedDataSummary *DeletionSummary `json:"deleted_data_summary,omitempty"`
	ErrorMessage       string           `json:"error_message,omitempty"`
}

// DeletionSummary summarizes what data was deleted.
type DeletionSummary struct {
	ProfileDeleted         bool           `json:"profile_deleted"`
	SessionsDeleted        int            `json:"sessions_deleted"`
	AuditLogsAnonymized    int            `json:"audit_logs_anonymized"`
	ConsentsDeleted        int            `json:"consents_deleted"`
	CredentialsDeleted     int            `json:"credentials_deleted"`
	RoleAssignmentsRemoved int            `json:"role_assignments_removed"`
	TokensRevoked          int            `json:"tokens_revoked"`
	AdditionalData         map[string]int `json:"additional_data,omitempty"`
}

// UserDataExport contains all exportable user data.
type UserDataExport struct {
	ExportMetadata ExportMetadata      `json:"export_metadata"`
	Profile        *UserProfileExport  `json:"profile,omitempty"`
	Sessions       []SessionExport     `json:"sessions,omitempty"`
	Consents       []ConsentExport     `json:"consents,omitempty"`
	Groups         []GroupMemberExport `json:"groups,omitempty"`
	Roles          []RoleAssignExport  `json:"roles,omitempty"`
	AuditLogs      []AuditLogExport    `json:"audit_logs,omitempty"`
	Credentials    []CredentialExport  `json:"credentials,omitempty"`
	MFAInfo        *MFAInfoExport      `json:"mfa_info,omitempty"`
}

// ExportMetadata contains metadata about the export.
type ExportMetadata struct {
	ExportID      string    `json:"export_id"`
	UserID        string    `json:"user_id"`
	TenantID      string    `json:"tenant_id"`
	ExportedAt    time.Time `json:"exported_at"`
	Format        string    `json:"format"`
	Categories    []string  `json:"categories"`
	SchemaVersion string    `json:"schema_version"`
}

// UserProfileExport contains exportable profile data.
type UserProfileExport struct {
	ID         string                 `json:"id"`
	Email      string                 `json:"email"`
	Name       string                 `json:"name"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	Created    time.Time              `json:"created"`
	Updated    time.Time              `json:"updated"`
	LastAuth   *time.Time             `json:"last_auth,omitempty"`
}

// SessionExport contains exportable session data.
type SessionExport struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastActive time.Time  `json:"last_active"`
	IPAddress  string     `json:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	AuthMethod string     `json:"auth_method"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// ConsentExport contains exportable consent data.
type ConsentExport struct {
	Purpose     string     `json:"purpose"`
	Status      string     `json:"status"`
	Version     string     `json:"version"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
}

// GroupMemberExport contains exportable group membership data.
type GroupMemberExport struct {
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name"`
	JoinedAt  time.Time `json:"joined_at"`
}

// RoleAssignExport contains exportable role assignment data.
type RoleAssignExport struct {
	RoleID     string    `json:"role_id"`
	RoleName   string    `json:"role_name"`
	AssignedAt time.Time `json:"assigned_at"`
}

// AuditLogExport contains exportable audit log data.
type AuditLogExport struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	IPAddress string    `json:"ip_address,omitempty"`
	Success   bool      `json:"success"`
}

// CredentialExport contains exportable credential metadata.
type CredentialExport struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Name        string    `json:"name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	Attestation string    `json:"attestation,omitempty"`
	Transports  []string  `json:"transports,omitempty"`
}

// MFAInfoExport contains exportable MFA information.
type MFAInfoExport struct {
	Enabled     bool       `json:"enabled"`
	Method      string     `json:"method"`
	EnrolledAt  *time.Time `json:"enrolled_at,omitempty"`
	BackupCodes int        `json:"backup_codes_remaining"`
}

// generateGDPRUserID generates a unique user ID for GDPR tests.
func generateGDPRUserID() string {
	return fmt.Sprintf("gdpr_user_%d", time.Now().UnixNano())
}

// ============================================================================
// Consent Management Tests
// ============================================================================

// TestGDPR_GrantConsent_AllPurposes tests granting consent for each valid purpose.
func TestGDPR_GrantConsent_AllPurposes(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	purposes := []string{
		"marketing",
		"analytics",
		"personalization",
		"third_party",
		"data_processing",
		"data_retention",
		"functional",
	}

	for _, purpose := range purposes {
		t.Run(purpose, func(t *testing.T) {
			userID := generateGDPRUserID()

			consent := ConsentRequest{
				Purpose:   purpose,
				Version:   "1.0",
				IPAddress: "192.0.2.100",
				UserAgent: "TestClient/1.0",
			}

			body, err := json.Marshal(consent)
			require.NoError(t, err)

			resp, err := cluster.rawPost(
				cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
				"application/json",
				bytes.NewReader(body),
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusCreated, resp.StatusCode, "Expected 201 Created for purpose %s", purpose)

			var result ConsentResponse
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)

			assert.NotEmpty(t, result.ID, "Consent ID should not be empty")
			assert.Equal(t, userID, result.UserID, "UserID should match")
			assert.Equal(t, tenantID, result.TenantID, "TenantID should match")
			assert.Equal(t, purpose, result.Purpose, "Purpose should match")
			assert.Equal(t, "granted", result.Status, "Status should be granted")
			assert.Equal(t, "1.0", result.Version, "Version should match")
			assert.Equal(t, "192.0.2.100", result.IPAddress, "IPAddress should match")
			assert.Equal(t, "TestClient/1.0", result.UserAgent, "UserAgent should match")
			assert.NotNil(t, result.GrantedAt, "GrantedAt should be set")
			assert.NotZero(t, result.Created, "Created timestamp should be set")
			assert.NotZero(t, result.Updated, "Updated timestamp should be set")
		})
	}
}

// TestGDPR_GrantConsent_WithExpiry tests granting consent with expiration time.
func TestGDPR_GrantConsent_WithExpiry(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	expiryTime := time.Now().Add(365 * 24 * time.Hour) // 1 year from now

	consent := ConsentRequest{
		Purpose:   "marketing",
		Version:   "1.0",
		ExpiresAt: &expiryTime,
		IPAddress: "10.0.0.1",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var result ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result.ID)
	assert.Equal(t, "granted", result.Status)
	assert.NotNil(t, result.ExpiresAt, "ExpiresAt should be set")

	// Verify expiry is approximately 1 year from now (within 1 minute tolerance)
	expectedExpiry := expiryTime.UTC()
	actualExpiry := result.ExpiresAt.UTC()
	diff := expectedExpiry.Sub(actualExpiry)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, time.Minute, "Expiry time should be close to requested time")
}

// TestGDPR_GrantConsent_UpdateExisting tests updating an existing consent with new version.
func TestGDPR_GrantConsent_UpdateExisting(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Grant initial consent
	consent1 := ConsentRequest{
		Purpose: "marketing",
		Version: "1.0",
	}

	body, err := json.Marshal(consent1)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Update consent with new version
	consent2 := ConsentRequest{
		Purpose: "marketing",
		Version: "2.0",
	}

	body, err = json.Marshal(consent2)
	require.NoError(t, err)

	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Update should return 200 OK")

	var result ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "marketing", result.Purpose)
	assert.Equal(t, "2.0", result.Version, "Version should be updated to 2.0")
	assert.Equal(t, "granted", result.Status)

	// Verify only one consent exists (not duplicate)
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var consents []ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&consents)
	require.NoError(t, err)

	marketingConsents := 0
	for _, c := range consents {
		if c.Purpose == "marketing" {
			marketingConsents++
			assert.Equal(t, "2.0", c.Version, "Should have latest version")
		}
	}
	assert.Equal(t, 1, marketingConsents, "Should have exactly one marketing consent")
}

// TestGDPR_WithdrawConsent_Success tests successfully withdrawing consent.
func TestGDPR_WithdrawConsent_Success(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Grant consent first
	consent := ConsentRequest{
		Purpose: "analytics",
		Version: "1.0",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Withdraw consent
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent/analytics",
		nil,
	)
	require.NoError(t, err)

	resp, err = cluster.rawDo(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "analytics", result.Purpose)
	assert.Equal(t, "withdrawn", result.Status, "Status should be withdrawn")
	assert.NotNil(t, result.WithdrawnAt, "WithdrawnAt should be set")
}

// TestGDPR_WithdrawConsent_NotFound tests withdrawing non-existent consent.
func TestGDPR_WithdrawConsent_NotFound(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Attempt to withdraw consent that was never granted
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent/marketing",
		nil,
	)
	require.NoError(t, err)

	resp, err := cluster.rawDo(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Should return 404 for non-existent consent")

	// Verify error message
	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(bodyBytes), "not found", "Error should indicate consent not found")
}

// TestGDPR_GetConsent_Success tests getting a specific consent by purpose.
func TestGDPR_GetConsent_Success(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Grant consent
	consent := ConsentRequest{
		Purpose:   "functional",
		Version:   "3.5",
		IPAddress: "172.16.0.50",
		UserAgent: "Mozilla/5.0",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Get consent by purpose
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consent/functional",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result.ID)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, tenantID, result.TenantID)
	assert.Equal(t, "functional", result.Purpose)
	assert.Equal(t, "granted", result.Status)
	assert.Equal(t, "3.5", result.Version)
	assert.Equal(t, "172.16.0.50", result.IPAddress)
	assert.Equal(t, "Mozilla/5.0", result.UserAgent)
}

// TestGDPR_ListConsents_Empty tests listing consents for user with no consents.
func TestGDPR_ListConsents_Empty(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// List consents for user that has none
	resp, err := cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var consents []ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&consents)
	require.NoError(t, err)

	assert.Empty(t, consents, "Consents list should be empty")
	assert.Len(t, consents, 0)
}

// TestGDPR_ListConsents_Multiple tests listing multiple consents.
func TestGDPR_ListConsents_Multiple(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Grant multiple consents
	purposes := []string{"marketing", "analytics", "functional", "personalization"}
	for _, purpose := range purposes {
		consent := ConsentRequest{
			Purpose: purpose,
			Version: "1.0",
		}

		body, err := json.Marshal(consent)
		require.NoError(t, err)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	// List all consents
	resp, err := cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var consents []ConsentResponse
	err = json.NewDecoder(resp.Body).Decode(&consents)
	require.NoError(t, err)

	assert.Len(t, consents, len(purposes), "Should return all consents")

	// Verify all purposes are present
	foundPurposes := make(map[string]bool)
	for _, c := range consents {
		foundPurposes[c.Purpose] = true
		assert.Equal(t, "granted", c.Status)
		assert.Equal(t, "1.0", c.Version)
	}

	for _, purpose := range purposes {
		assert.True(t, foundPurposes[purpose], "Purpose %s should be in list", purpose)
	}
}

// ============================================================================
// Data Export Tests
// ============================================================================

// TestGDPR_RequestExport_AllFormats tests that JSON, the only format the
// export generator writes, is accepted and CSV and XML are refused (#1724).
func TestGDPR_RequestExport_AllFormats(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	for _, format := range []string{"csv", "xml"} {
		t.Run(format+"_refused", func(t *testing.T) {
			body, err := json.Marshal(ExportRequest{Format: format})
			require.NoError(t, err)

			resp, err := cluster.rawPost(
				cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+generateGDPRUserID()+"/gdpr/export",
				"application/json",
				bytes.NewReader(body),
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Export request should be refused for format %s", format)
		})
	}

	for _, format := range []string{"json"} {
		t.Run(format, func(t *testing.T) {
			userID := generateGDPRUserID()

			exportReq := ExportRequest{
				Format: format,
			}

			body, err := json.Marshal(exportReq)
			require.NoError(t, err)

			resp, err := cluster.rawPost(
				cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
				"application/json",
				bytes.NewReader(body),
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusAccepted, resp.StatusCode, "Export request should be accepted for format %s", format)

			var result ExportResponse
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)

			assert.NotEmpty(t, result.ID, "Export ID should not be empty")
			assert.Equal(t, userID, result.UserID)
			assert.Equal(t, tenantID, result.TenantID)
			assert.Equal(t, format, result.Format)
			assert.Equal(t, "pending", result.Status)
			assert.NotZero(t, result.RequestedAt)
		})
	}
}

// TestGDPR_RequestExport_DuplicatePending tests rejecting duplicate pending export requests.
func TestGDPR_RequestExport_DuplicatePending(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Request first export
	exportReq := ExportRequest{
		Format: "json",
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Request second export before first completes
	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be rejected with 409 Conflict
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "Duplicate pending export should be rejected")

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(bodyBytes), "pending", "Error should mention pending export")
}

// TestGDPR_GetExportStatus_Transitions tests export status transitions.
func TestGDPR_GetExportStatus_Transitions(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Request export
	exportReq := ExportRequest{
		Format: "json",
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result ExportResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	exportID := result.ID
	assert.Equal(t, "pending", result.Status)

	// Poll for status changes with timeout
	validStatuses := map[string]bool{
		"pending":    true,
		"processing": true,
		"completed":  true,
		"failed":     true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lastStatus := "pending"

	for {
		resp, err = cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/export/" + exportID,
		)
		require.NoError(t, err)

		var statusResult ExportResponse
		err = json.NewDecoder(resp.Body).Decode(&statusResult)
		resp.Body.Close()
		require.NoError(t, err)

		assert.True(t, validStatuses[statusResult.Status], "Status %s should be valid", statusResult.Status)
		assert.Equal(t, exportID, statusResult.ID)

		lastStatus = statusResult.Status

		// If completed or failed, we are done
		if lastStatus == "completed" || lastStatus == "failed" {
			break
		}

		if !pause(ctx, 250*time.Millisecond) {
			break
		}
	}

	// Verify we reached a terminal state
	assert.True(t, lastStatus == "completed" || lastStatus == "failed",
		"Export should reach terminal state, got %s", lastStatus)
}

// TestGDPR_GenerateExport_AllCategories tests generating export with all data categories.
func TestGDPR_GenerateExport_AllCategories(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()

	// Create user with some data
	createTestUser(t, cluster, tenantID, userID)

	// Grant some consents
	for _, purpose := range []string{"marketing", "analytics"} {
		consent := ConsentRequest{Purpose: purpose, Version: "1.0"}
		body, err := json.Marshal(consent)
		require.NoError(t, err)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Request export with all categories (empty include = all)
	exportReq := ExportRequest{
		Format: "json",
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var exportResult ExportResponse
	err = json.NewDecoder(resp.Body).Decode(&exportResult)
	require.NoError(t, err)

	// Wait for export to complete
	exportData := waitForExportCompletion(t, cluster, tenantID, userID, exportResult.ID, 60*time.Second)

	// Verify all categories are included
	assert.NotNil(t, exportData, "Export data should not be nil")
	assert.NotEmpty(t, exportData.ExportMetadata.ExportID)
	assert.Equal(t, userID, exportData.ExportMetadata.UserID)
	assert.Equal(t, tenantID, exportData.ExportMetadata.TenantID)

	// Should include profile
	assert.NotNil(t, exportData.Profile, "Profile should be included")

	// Should include consents we created
	assert.GreaterOrEqual(t, len(exportData.Consents), 2, "Should have at least 2 consents")
}

// TestGDPR_GenerateExport_SelectiveCategories tests export with category filters.
func TestGDPR_GenerateExport_SelectiveCategories(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Grant consent
	consent := ConsentRequest{Purpose: "marketing", Version: "1.0"}
	body, err := json.Marshal(consent)
	require.NoError(t, err)
	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	t.Run("IncludeOnly", func(t *testing.T) {
		exportReq := ExportRequest{
			Format:            "json",
			IncludeCategories: []string{"profile", "consents"},
		}

		body, err := json.Marshal(exportReq)
		require.NoError(t, err)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)

		var result ExportResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, []string{"profile", "consents"}, result.IncludeCategories)
	})

	t.Run("ExcludeCategories", func(t *testing.T) {
		exportReq := ExportRequest{
			Format:            "json",
			ExcludeCategories: []string{"sessions", "audit_logs"},
		}

		body, err := json.Marshal(exportReq)
		require.NoError(t, err)

		// Need different user to avoid duplicate pending conflict
		userID2 := generateGDPRUserID()
		createTestUser(t, cluster, tenantID, userID2)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID2+"/gdpr/export",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)

		var result ExportResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, []string{"sessions", "audit_logs"}, result.ExcludeCategories)
	})
}

// ============================================================================
// Data Deletion Tests
// ============================================================================

// TestGDPR_RequestDeletion_Success tests requesting data deletion.
func TestGDPR_RequestDeletion_Success(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	deletionReq := DeletionRequest{
		Reason:          "User requested account deletion",
		GracePeriodDays: 7,
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result.ID)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, tenantID, result.TenantID)
	assert.Equal(t, "pending", result.Status)
	assert.Equal(t, "User requested account deletion", result.Reason)
	assert.NotNil(t, result.GracePeriodEndsAt, "Grace period end should be set")

	// Verify grace period is approximately 7 days from now
	expectedEnd := time.Now().Add(7 * 24 * time.Hour)
	diff := result.GracePeriodEndsAt.Sub(expectedEnd)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, 5*time.Minute, "Grace period should be approximately 7 days")
}

// TestGDPR_RequestDeletion_CustomGracePeriod tests deletion with custom grace period.
func TestGDPR_RequestDeletion_CustomGracePeriod(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	testCases := []struct {
		name            string
		gracePeriodDays int
	}{
		{"ZeroDays_Immediate", 0},
		{"OneDay", 1},
		{"FourteenDays", 14},
		{"ThirtyDays", 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := generateGDPRUserID()
			createTestUser(t, cluster, tenantID, userID)

			deletionReq := DeletionRequest{
				Reason:          "Custom grace period test",
				GracePeriodDays: tc.gracePeriodDays,
			}

			body, err := json.Marshal(deletionReq)
			require.NoError(t, err)

			resp, err := cluster.rawPost(
				cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
				"application/json",
				bytes.NewReader(body),
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusAccepted, resp.StatusCode)

			var result DeletionResponse
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)

			if tc.gracePeriodDays == 0 {
				// For immediate deletion, grace period should be now or very close
				if result.GracePeriodEndsAt != nil {
					diff := time.Until(*result.GracePeriodEndsAt)
					assert.Less(t, diff, time.Minute, "Immediate deletion should have no grace period")
				}
			} else {
				require.NotNil(t, result.GracePeriodEndsAt)
				expectedEnd := time.Now().Add(time.Duration(tc.gracePeriodDays) * 24 * time.Hour)
				diff := result.GracePeriodEndsAt.Sub(expectedEnd)
				if diff < 0 {
					diff = -diff
				}
				assert.Less(t, diff, 5*time.Minute, "Grace period should match requested days")
			}
		})
	}
}

// TestGDPR_CancelDeletion_DuringGracePeriod tests cancelling deletion during grace period.
func TestGDPR_CancelDeletion_DuringGracePeriod(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Request deletion with long grace period
	deletionReq := DeletionRequest{
		Reason:          "Will be cancelled",
		GracePeriodDays: 30,
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	deletionID := result.ID
	require.Equal(t, "pending", result.Status)

	// Cancel deletion during grace period
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion/"+deletionID,
		nil,
	)
	require.NoError(t, err)

	resp, err = cluster.rawDo(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cancelResult DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&cancelResult)
	require.NoError(t, err)

	assert.Equal(t, deletionID, cancelResult.ID)
	assert.Equal(t, "cancelled", cancelResult.Status)
	assert.NotNil(t, cancelResult.CancelledAt, "CancelledAt should be set")
}

// TestGDPR_CancelDeletion_AfterProcessing tests that cancellation fails after processing starts.
func TestGDPR_CancelDeletion_AfterProcessing(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Request immediate deletion
	deletionReq := DeletionRequest{
		Reason:          "Immediate deletion",
		GracePeriodDays: 0, // Immediate
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	deletionID := result.ID

	// Poll the current status until processing starts (the status leaves
	// "pending"), for at most 5 seconds: processing may not start at all
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var statusResult DeletionResponse
	for {
		resp, err = cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/deletion/" + deletionID,
		)
		require.NoError(t, err)

		err = json.NewDecoder(resp.Body).Decode(&statusResult)
		resp.Body.Close()
		require.NoError(t, err)

		if statusResult.Status != "pending" || !pause(ctx, 250*time.Millisecond) {
			break
		}
	}

	// If already processing or completed, cancellation should fail
	if statusResult.Status == "processing" || statusResult.Status == "completed" {
		req, err := http.NewRequest(
			http.MethodDelete,
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion/"+deletionID,
			nil,
		)
		require.NoError(t, err)

		resp, err = cluster.rawDo(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusConflict, resp.StatusCode, "Should not be able to cancel after processing")
	}
}

// TestGDPR_ProcessDeletion_Success tests successful data deletion processing.
func TestGDPR_ProcessDeletion_Success(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Create some data for the user
	consent := ConsentRequest{Purpose: "marketing", Version: "1.0"}
	body, err := json.Marshal(consent)
	require.NoError(t, err)
	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Request immediate deletion
	deletionReq := DeletionRequest{
		Reason:          "Complete deletion test",
		GracePeriodDays: 0,
	}

	body, err = json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	deletionID := result.ID

	// Wait for deletion to complete
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var finalStatus DeletionResponse
	for {
		resp, err = cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/deletion/" + deletionID,
		)
		require.NoError(t, err)

		err = json.NewDecoder(resp.Body).Decode(&finalStatus)
		resp.Body.Close()
		require.NoError(t, err)

		if finalStatus.Status == "completed" || finalStatus.Status == "failed" {
			break
		}

		if !pause(ctx, 250*time.Millisecond) {
			break
		}
	}

	assert.Equal(t, "completed", finalStatus.Status, "Deletion should complete successfully")
	assert.NotNil(t, finalStatus.CompletedAt, "CompletedAt should be set")
	assert.NotNil(t, finalStatus.DeletedDataSummary, "Deletion summary should be provided")

	// Verify user data is actually deleted
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID,
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "User should not exist after deletion")
}

// TestGDPR_ProcessDeletion_PartialCategories tests deleting specific categories only.
func TestGDPR_ProcessDeletion_PartialCategories(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Create consent data
	consent := ConsentRequest{Purpose: "analytics", Version: "1.0"}
	body, err := json.Marshal(consent)
	require.NoError(t, err)
	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Request deletion of only sessions category
	deletionReq := DeletionRequest{
		Reason:           "Partial deletion - sessions only",
		DeleteCategories: []string{"sessions"},
		GracePeriodDays:  0,
	}

	body, err = json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, []string{"sessions"}, result.DeleteCategories)

	// Wait for completion
	deletionID := result.ID
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		resp, err = cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/deletion/" + deletionID,
		)
		require.NoError(t, err)

		var status DeletionResponse
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		if !pause(ctx, 250*time.Millisecond) {
			break
		}
	}

	// Profile should still exist
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID,
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "User profile should still exist after partial deletion")

	// Consents should still exist
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var consents []ConsentResponse
	json.NewDecoder(resp.Body).Decode(&consents)
	assert.NotEmpty(t, consents, "Consents should still exist after partial deletion")
}

// TestGDPR_ProcessDeletion_PreserveCategories tests preserving specific categories during deletion.
func TestGDPR_ProcessDeletion_PreserveCategories(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()
	createTestTenant(t, cluster, tenantID)
	defer cleanupTestTenant(t, cluster, tenantID)

	userID := generateGDPRUserID()
	createTestUser(t, cluster, tenantID, userID)

	// Create consent and audit data
	consent := ConsentRequest{Purpose: "data_processing", Version: "1.0"}
	body, err := json.Marshal(consent)
	require.NoError(t, err)
	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Request deletion but preserve audit_logs (for legal compliance)
	deletionReq := DeletionRequest{
		Reason:             "Deletion with preserved audit logs",
		PreserveCategories: []string{"audit_logs", "consents"},
		GracePeriodDays:    0,
	}

	body, err = json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result DeletionResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, []string{"audit_logs", "consents"}, result.PreserveCategories)

	deletionID := result.ID

	// Wait for completion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		resp, err = cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/deletion/" + deletionID,
		)
		require.NoError(t, err)

		var status DeletionResponse
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status.Status == "completed" || status.Status == "failed" {
			// Verify preserved data still exists
			if status.Status == "completed" {
				// Consents should be preserved
				consResp, err := cluster.rawGet(
					cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
				)
				if err == nil {
					var consents []ConsentResponse
					json.NewDecoder(consResp.Body).Decode(&consents)
					consResp.Body.Close()
					assert.NotEmpty(t, consents, "Preserved consents should still exist")
				}
			}
			break
		}
		if !pause(ctx, 250*time.Millisecond) {
			break
		}
	}
}

// ============================================================================
// Legacy Test Compatibility (Original Tests)
// ============================================================================

// TestGDPR_GrantConsent tests granting user consent (legacy compatibility).
func TestGDPR_GrantConsent(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_1"

	consent := ConsentRequest{
		Purpose:   "marketing",
		Version:   "1.0",
		IPAddress: "192.0.2.1",
		UserAgent: "Test/1.0",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Handler returns 201 Created for new consent, 200 OK for existing
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"expected 200 or 201, got %d", resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["id"])
	assert.Equal(t, "marketing", result["purpose"])
	assert.Equal(t, "granted", result["status"])
}

// TestGDPR_WithdrawConsent tests withdrawing user consent (legacy compatibility).
func TestGDPR_WithdrawConsent(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_2"

	// First grant consent
	consent := ConsentRequest{
		Purpose: "marketing",
		Version: "1.0",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()
	// Handler returns 201 Created for new consent, 200 OK for existing
	require.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"expected 200 or 201, got %d", resp.StatusCode)

	// Withdraw consent
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent/marketing",
		nil,
	)
	require.NoError(t, err)

	resp, err = cluster.rawDo(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "withdrawn", result["status"])
}

// TestGDPR_ListConsents tests listing user consents (legacy compatibility).
func TestGDPR_ListConsents(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_3"

	// Grant multiple consents
	purposes := []string{"marketing", "analytics", "functional"}
	for _, purpose := range purposes {
		consent := ConsentRequest{
			Purpose: purpose,
			Version: "1.0",
		}

		body, err := json.Marshal(consent)
		require.NoError(t, err)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// List consents
	resp, err := cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consents",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var consents []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&consents)
	require.NoError(t, err)

	assert.Len(t, consents, 3)
}

// TestGDPR_RequestDataExport tests requesting a data export (legacy compatibility).
func TestGDPR_RequestDataExport(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_4"

	exportReq := ExportRequest{
		Format: "json",
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["id"])
	assert.Equal(t, "pending", result["status"])
	assert.Equal(t, "json", result["format"])
}

// TestGDPR_GetExportStatus tests getting the status of a data export (legacy compatibility).
func TestGDPR_GetExportStatus(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_5"

	// Request export
	exportReq := ExportRequest{
		Format: "json",
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	exportID := result["id"].(string)

	// Get export status
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/export/" + exportID,
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, exportID, result["id"])
}

// TestGDPR_RequestDataDeletion tests requesting data deletion (legacy compatibility).
func TestGDPR_RequestDataDeletion(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	// Use a unique tenant ID to avoid conflicts with other GDPR tests that
	// may leave deletion state in shared "test-tenant"
	tenantID := fmt.Sprintf("deletion-test-tenant-%d", time.Now().UnixNano())
	userID := generateGDPRUserID()

	deletionReq := DeletionRequest{
		Reason:          "User requested account deletion",
		GracePeriodDays: 7,
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["id"])
	assert.Equal(t, "pending", result["status"])
	assert.NotEmpty(t, result["grace_period_ends_at"])
}

// TestGDPR_CancelDeletion tests cancelling a deletion request (legacy compatibility).
func TestGDPR_CancelDeletion(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := generateGDPRUserID()

	// Request deletion
	deletionReq := DeletionRequest{
		Reason:          "User requested account deletion",
		GracePeriodDays: 7,
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	deletionID := result["id"].(string)

	// Cancel deletion
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion/"+deletionID,
		nil,
	)
	require.NoError(t, err)

	resp, err = cluster.rawDo(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "cancelled", result["status"])
}

// TestGDPR_SelectiveExport tests requesting a selective data export (legacy compatibility).
func TestGDPR_SelectiveExport(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_8"

	// Request export with specific categories
	exportReq := ExportRequest{
		Format:            "json",
		IncludeCategories: []string{"profile", "consents"},
		ExcludeCategories: []string{"sessions"},
	}

	body, err := json.Marshal(exportReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["id"])
}

// TestGDPR_PartialDeletion tests requesting partial data deletion (legacy compatibility).
func TestGDPR_PartialDeletion(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := generateGDPRUserID()

	// Request partial deletion - delete sessions but preserve profile
	deletionReq := DeletionRequest{
		Reason:             "User requested session cleanup",
		DeleteCategories:   []string{"sessions"},
		PreserveCategories: []string{"profile"},
		GracePeriodDays:    0, // Immediate
	}

	body, err := json.Marshal(deletionReq)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/deletion",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["id"])
}

// TestGDPR_ConsentHistory tests viewing consent history (legacy compatibility).
func TestGDPR_ConsentHistory(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"
	userID := "gdpr_user_10"

	// Grant consent
	consent := ConsentRequest{
		Purpose: "marketing",
		Version: "1.0",
	}

	body, err := json.Marshal(consent)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Update consent version
	consent.Version = "2.0"
	body, err = json.Marshal(consent)
	require.NoError(t, err)

	resp, err = cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/consent",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Get consent - should show latest version
	resp, err = cluster.rawGet(
		cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/consent/marketing",
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "2.0", result["version"])
}

// TestGDPR_ExportFormats tests that only JSON exports are accepted (#1724).
func TestGDPR_ExportFormats(t *testing.T) {
	cluster := setupGDPRCluster(t)
	defer cluster.Close()

	tenantID := "test-tenant"

	formats := []string{"json", "csv", "xml"}

	for i, format := range formats {
		userID := fmt.Sprintf("gdpr_export_user_%d", i)

		exportReq := ExportRequest{
			Format: format,
		}

		body, err := json.Marshal(exportReq)
		require.NoError(t, err)

		resp, err := cluster.rawPost(
			cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/gdpr/export",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		if format != "json" {
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "format %s", format)
			continue
		}
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, format, result["format"])
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

// setupGDPRCluster returns the fixture system admin's view of a test
// cluster. Every call the GDPR tests make is the system admin's: the tenant
// create and delete as system-tenant calls, the user create and the GDPR
// calls in the tenant the URL names, through the tenant lift
// (pkg/api/rest/tenant_target.go) when that is not the admin's own tenant.
// Most tests create their own tenant; the legacy ones use the cluster's
// default tenant. The base cluster is closed when t ends; closing the view
// is a no-op.
func setupGDPRCluster(t *testing.T) *testCluster {
	t.Helper()
	base := setupTestCluster(t)
	t.Cleanup(base.Close)
	return base.asSystemAdmin()
}

// createTestTenant creates a test tenant for GDPR tests.
func createTestTenant(t *testing.T, cluster *testCluster, tenantID string) {
	t.Helper()

	tenant := map[string]interface{}{
		"id":   tenantID,
		"name": "GDPR Test Tenant " + tenantID,
	}

	body, err := json.Marshal(tenant)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Accept 201 Created or 409 Conflict (if already exists)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("Failed to create tenant: %d - %s", resp.StatusCode, string(bodyBytes))
	}
}

// cleanupTestTenant removes a test tenant.
func cleanupTestTenant(t *testing.T, cluster *testCluster, tenantID string) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID,
		nil,
	)
	if err != nil {
		return
	}

	resp, err := cluster.rawDo(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// createTestUser creates a test user for GDPR tests.
func createTestUser(t *testing.T, cluster *testCluster, tenantID, userID string) {
	t.Helper()

	user := map[string]interface{}{
		"id":       userID,
		"email":    userID + "@test.local",
		"username": userID,
		"name":     "GDPR Test User " + userID,
	}

	body, err := json.Marshal(user)
	require.NoError(t, err)

	resp, err := cluster.rawPost(
		cluster.httpEndpoint+"/api/v1/tenants/"+tenantID+"/users",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Accept 201 Created or 409 Conflict (if already exists)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("Failed to create user: %d - %s", resp.StatusCode, string(bodyBytes))
	}
}

// waitForExportCompletion waits for an export to complete and returns the data.
func waitForExportCompletion(t *testing.T, cluster *testCluster, tenantID, userID, exportID string, timeout time.Duration) *UserDataExport {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		resp, err := cluster.rawGet(
			cluster.httpEndpoint + "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr/export/" + exportID,
		)
		require.NoError(t, err)

		var status ExportResponse
		err = json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		require.NoError(t, err)

		if status.Status == "completed" {
			// Download the export data
			if status.DownloadURL != "" {
				// DownloadURL is a relative path, construct full URL
				fullURL := cluster.httpEndpoint + status.DownloadURL
				dataResp, err := cluster.rawGet(fullURL)
				if err != nil {
					t.Fatalf("Failed to download export data: %v", err)
				}
				defer dataResp.Body.Close()

				if dataResp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(dataResp.Body)
					t.Fatalf("Download returned status %d: %s", dataResp.StatusCode, string(body))
				}

				var exportData UserDataExport
				if err := json.NewDecoder(dataResp.Body).Decode(&exportData); err != nil {
					t.Fatalf("Failed to decode export data: %v", err)
				}
				return &exportData
			}
			// Return minimal export data if download URL not available
			return &UserDataExport{
				ExportMetadata: ExportMetadata{
					ExportID: exportID,
					UserID:   userID,
					TenantID: tenantID,
				},
			}
		}

		if status.Status == "failed" {
			t.Fatalf("Export failed: %s", status.ErrorMessage)
		}

		if !pause(ctx, 250*time.Millisecond) {
			break
		}
	}

	t.Fatalf("Export did not complete within timeout")
	return nil
}

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
	"time"
)

// ConsentPurpose represents a purpose for which consent can be granted.
type ConsentPurpose string

const (
	// ConsentPurposeMarketing for marketing communications.
	ConsentPurposeMarketing ConsentPurpose = "marketing"

	// ConsentPurposeAnalytics for analytics and usage tracking.
	ConsentPurposeAnalytics ConsentPurpose = "analytics"

	// ConsentPurposePersonalization for personalized experiences.
	ConsentPurposePersonalization ConsentPurpose = "personalization"

	// ConsentPurposeThirdParty for sharing with third parties.
	ConsentPurposeThirdParty ConsentPurpose = "third_party"

	// ConsentPurposeDataProcessing for general data processing.
	ConsentPurposeDataProcessing ConsentPurpose = "data_processing"

	// ConsentPurposeDataRetention for extended data retention.
	ConsentPurposeDataRetention ConsentPurpose = "data_retention"

	// ConsentPurposeFunctional for functional features and cookies.
	ConsentPurposeFunctional ConsentPurpose = "functional"
)

// ConsentStatus represents the status of a consent.
type ConsentStatus string

const (
	// ConsentStatusGranted indicates consent was given.
	ConsentStatusGranted ConsentStatus = "granted"

	// ConsentStatusDenied indicates consent was explicitly denied.
	ConsentStatusDenied ConsentStatus = "denied"

	// ConsentStatusWithdrawn indicates consent was previously granted but withdrawn.
	ConsentStatusWithdrawn ConsentStatus = "withdrawn"

	// ConsentStatusPending indicates consent has not been given or denied.
	ConsentStatusPending ConsentStatus = "pending"
)

// DataExportFormat represents the format for data exports.
type DataExportFormat string

const (
	// DataExportFormatJSON exports data as JSON.
	DataExportFormatJSON DataExportFormat = "json"

	// DataExportFormatCSV exports data as CSV.
	DataExportFormatCSV DataExportFormat = "csv"

	// DataExportFormatXML exports data as XML.
	DataExportFormatXML DataExportFormat = "xml"
)

// DataExportStatus represents the status of a data export request.
type DataExportStatus string

const (
	// DataExportStatusPending indicates the export is queued.
	DataExportStatusPending DataExportStatus = "pending"

	// DataExportStatusProcessing indicates the export is being generated.
	DataExportStatusProcessing DataExportStatus = "processing"

	// DataExportStatusCompleted indicates the export is ready for download.
	DataExportStatusCompleted DataExportStatus = "completed"

	// DataExportStatusFailed indicates the export failed.
	DataExportStatusFailed DataExportStatus = "failed"

	// DataExportStatusExpired indicates the export download link has expired.
	DataExportStatusExpired DataExportStatus = "expired"
)

// DeletionStatus represents the status of a deletion request.
type DeletionStatus string

const (
	// DeletionStatusPending indicates the deletion is queued.
	DeletionStatusPending DeletionStatus = "pending"

	// DeletionStatusProcessing indicates the deletion is in progress.
	DeletionStatusProcessing DeletionStatus = "processing"

	// DeletionStatusCompleted indicates the deletion is complete.
	DeletionStatusCompleted DeletionStatus = "completed"

	// DeletionStatusFailed indicates the deletion failed.
	DeletionStatusFailed DeletionStatus = "failed"

	// DeletionStatusCancelled indicates the deletion was cancelled.
	DeletionStatusCancelled DeletionStatus = "cancelled"
)

// Consent represents a user's consent for a specific purpose.
type Consent struct {
	// ID is the unique identifier for this consent record.
	ID string `json:"id" yaml:"id"`

	// UserID is the user who gave or denied consent.
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Purpose is what the consent is for.
	Purpose ConsentPurpose `json:"purpose" yaml:"purpose"`

	// Status is the current consent status.
	Status ConsentStatus `json:"status" yaml:"status"`

	// Version is the version of the consent policy.
	Version string `json:"version" yaml:"version"`

	// GrantedAt is when consent was granted (nil if not granted).
	GrantedAt *time.Time `json:"granted_at,omitempty" yaml:"granted_at,omitempty"`

	// WithdrawnAt is when consent was withdrawn (nil if not withdrawn).
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty" yaml:"withdrawn_at,omitempty"`

	// ExpiresAt is when consent expires (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`

	// IPAddress is the IP address when consent was given.
	IPAddress string `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`

	// UserAgent is the user agent when consent was given.
	UserAgent string `json:"user_agent,omitempty" yaml:"user_agent,omitempty"`

	// Metadata contains additional context.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	// Created is when the record was created.
	Created time.Time `json:"created" yaml:"created"`

	// Updated is when the record was last updated.
	Updated time.Time `json:"updated" yaml:"updated"`
}

// DataExportRequest represents a request to export user data.
type DataExportRequest struct {
	// ID is the unique identifier for this export request.
	ID string `json:"id" yaml:"id"`

	// UserID is the user whose data is being exported.
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// RequestedBy is the qrdb Snowflake id of the user or admin who
	// requested the export.
	RequestedBy uint64 `json:"requested_by,string" yaml:"requested_by"`

	// Format is the export format.
	Format DataExportFormat `json:"format" yaml:"format"`

	// Status is the current export status.
	Status DataExportStatus `json:"status" yaml:"status"`

	// IncludeCategories specifies which data categories to include.
	// Empty means all categories.
	IncludeCategories []string `json:"include_categories,omitempty" yaml:"include_categories,omitempty"`

	// ExcludeCategories specifies which data categories to exclude.
	ExcludeCategories []string `json:"exclude_categories,omitempty" yaml:"exclude_categories,omitempty"`

	// RequestedAt is when the export was requested.
	RequestedAt time.Time `json:"requested_at" yaml:"requested_at"`

	// StartedAt is when processing began.
	StartedAt *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`

	// CompletedAt is when processing completed.
	CompletedAt *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`

	// ExpiresAt is when the download link expires.
	ExpiresAt *time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`

	// DownloadURL is the URL to download the export (set when completed).
	DownloadURL string `json:"download_url,omitempty" yaml:"download_url,omitempty"`

	// FileSizeBytes is the size of the export file.
	FileSizeBytes int64 `json:"file_size_bytes,omitempty" yaml:"file_size_bytes,omitempty"`

	// ErrorMessage contains error details if failed.
	ErrorMessage string `json:"error_message,omitempty" yaml:"error_message,omitempty"`

	// Metadata contains additional context.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// DataDeletionRequest represents a request to delete user data.
type DataDeletionRequest struct {
	// ID is the unique identifier for this deletion request.
	ID string `json:"id" yaml:"id"`

	// UserID is the user whose data is being deleted.
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// RequestedBy is the qrdb Snowflake id of the user or admin who
	// requested the deletion.
	RequestedBy uint64 `json:"requested_by,string" yaml:"requested_by"`

	// Reason is the reason for deletion.
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`

	// Status is the current deletion status.
	Status DeletionStatus `json:"status" yaml:"status"`

	// DeleteCategories specifies which data categories to delete.
	// Empty means complete deletion.
	DeleteCategories []string `json:"delete_categories,omitempty" yaml:"delete_categories,omitempty"`

	// PreserveCategories specifies which data categories to preserve.
	// Used for legal holds or compliance requirements.
	PreserveCategories []string `json:"preserve_categories,omitempty" yaml:"preserve_categories,omitempty"`

	// RequestedAt is when the deletion was requested.
	RequestedAt time.Time `json:"requested_at" yaml:"requested_at"`

	// GracePeriodEndsAt is when the grace period ends and deletion begins.
	GracePeriodEndsAt *time.Time `json:"grace_period_ends_at,omitempty" yaml:"grace_period_ends_at,omitempty"`

	// StartedAt is when processing began.
	StartedAt *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`

	// CompletedAt is when processing completed.
	CompletedAt *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`

	// CancelledAt is when the request was cancelled.
	CancelledAt *time.Time `json:"cancelled_at,omitempty" yaml:"cancelled_at,omitempty"`

	// CancelledBy is who cancelled the request.
	CancelledBy string `json:"cancelled_by,omitempty" yaml:"cancelled_by,omitempty"`

	// DeletedDataSummary contains a summary of what was deleted.
	DeletedDataSummary *DeletionSummary `json:"deleted_data_summary,omitempty" yaml:"deleted_data_summary,omitempty"`

	// ErrorMessage contains error details if failed.
	ErrorMessage string `json:"error_message,omitempty" yaml:"error_message,omitempty"`

	// Metadata contains additional context.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// DeletionSummary summarizes what data was deleted.
type DeletionSummary struct {
	// ProfileDeleted indicates if the user profile was deleted.
	ProfileDeleted bool `json:"profile_deleted" yaml:"profile_deleted"`

	// SessionsDeleted is the number of sessions deleted.
	SessionsDeleted int `json:"sessions_deleted" yaml:"sessions_deleted"`

	// AuditLogsAnonymized is the number of audit logs anonymized.
	AuditLogsAnonymized int `json:"audit_logs_anonymized" yaml:"audit_logs_anonymized"`

	// ConsentsDeleted is the number of consent records deleted.
	ConsentsDeleted int `json:"consents_deleted" yaml:"consents_deleted"`

	// CredentialsDeleted is the number of credentials deleted.
	CredentialsDeleted int `json:"credentials_deleted" yaml:"credentials_deleted"`

	// RoleAssignmentsRemoved is the number of role assignments removed.
	RoleAssignmentsRemoved int `json:"role_assignments_removed" yaml:"role_assignments_removed"`

	// TokensRevoked is the number of tokens revoked.
	TokensRevoked int `json:"tokens_revoked" yaml:"tokens_revoked"`

	// AdditionalData contains category-specific deletion counts.
	AdditionalData map[string]int `json:"additional_data,omitempty" yaml:"additional_data,omitempty"`
}

// UserDataExport contains all exportable user data.
type UserDataExport struct {
	// ExportMetadata contains export metadata.
	ExportMetadata ExportMetadata `json:"export_metadata" yaml:"export_metadata"`

	// Profile contains user profile data.
	Profile *UserProfileExport `json:"profile,omitempty" yaml:"profile,omitempty"`

	// Sessions contains session history.
	Sessions []*SessionExport `json:"sessions,omitempty" yaml:"sessions,omitempty"`

	// Consents contains consent records.
	Consents []*ConsentExport `json:"consents,omitempty" yaml:"consents,omitempty"`

	// Roles contains role assignments.
	Roles []*RoleAssignmentExport `json:"roles,omitempty" yaml:"roles,omitempty"`

	// AuditLogs contains user's audit log entries.
	AuditLogs []*AuditLogExport `json:"audit_logs,omitempty" yaml:"audit_logs,omitempty"`

	// Credentials contains credential metadata (not secrets).
	Credentials []*CredentialExport `json:"credentials,omitempty" yaml:"credentials,omitempty"`

	// MFAInfo contains MFA enrollment info.
	MFAInfo *MFAInfoExport `json:"mfa_info,omitempty" yaml:"mfa_info,omitempty"`
}

// ExportMetadata contains metadata about the export.
type ExportMetadata struct {
	// ExportID is the export request ID.
	ExportID string `json:"export_id" yaml:"export_id"`

	// UserID is the user whose data was exported.
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// ExportedAt is when the export was generated.
	ExportedAt time.Time `json:"exported_at" yaml:"exported_at"`

	// Format is the export format.
	Format DataExportFormat `json:"format" yaml:"format"`

	// Categories lists the categories included.
	Categories []string `json:"categories" yaml:"categories"`

	// SchemaVersion is the export schema version.
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
}

// UserProfileExport contains exportable profile data.
type UserProfileExport struct {
	ID         uint64            `json:"id,string" yaml:"id"`
	Email      string            `json:"email" yaml:"email"`
	Name       string            `json:"name" yaml:"name"`
	Attributes map[string]string `json:"attributes,omitempty" yaml:"attributes,omitempty"`
	Created    time.Time         `json:"created" yaml:"created"`
	Updated    time.Time         `json:"updated" yaml:"updated"`
	LastAuth   *time.Time        `json:"last_auth,omitempty" yaml:"last_auth,omitempty"`
}

// SessionExport contains exportable session data.
type SessionExport struct {
	ID         string     `json:"id" yaml:"id"`
	StartedAt  time.Time  `json:"started_at" yaml:"started_at"`
	ExpiresAt  time.Time  `json:"expires_at" yaml:"expires_at"`
	LastActive time.Time  `json:"last_active" yaml:"last_active"`
	IPAddress  string     `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty" yaml:"user_agent,omitempty"`
	AuthMethod string     `json:"auth_method" yaml:"auth_method"`
	EndedAt    *time.Time `json:"ended_at,omitempty" yaml:"ended_at,omitempty"`
}

// ConsentExport contains exportable consent data.
type ConsentExport struct {
	Purpose     ConsentPurpose `json:"purpose" yaml:"purpose"`
	Status      ConsentStatus  `json:"status" yaml:"status"`
	Version     string         `json:"version" yaml:"version"`
	GrantedAt   *time.Time     `json:"granted_at,omitempty" yaml:"granted_at,omitempty"`
	WithdrawnAt *time.Time     `json:"withdrawn_at,omitempty" yaml:"withdrawn_at,omitempty"`
}

// RoleAssignmentExport contains exportable role assignment data.
type RoleAssignmentExport struct {
	RoleID     string    `json:"role_id" yaml:"role_id"`
	RoleName   string    `json:"role_name" yaml:"role_name"`
	AssignedAt time.Time `json:"assigned_at" yaml:"assigned_at"`
}

// AuditLogExport contains exportable audit log data.
type AuditLogExport struct {
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	Action    string    `json:"action" yaml:"action"`
	Resource  string    `json:"resource" yaml:"resource"`
	IPAddress string    `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`
	Success   bool      `json:"success" yaml:"success"`
}

// CredentialExport contains exportable credential metadata.
type CredentialExport struct {
	ID          string    `json:"id" yaml:"id"`
	Type        string    `json:"type" yaml:"type"`
	Name        string    `json:"name,omitempty" yaml:"name,omitempty"`
	CreatedAt   time.Time `json:"created_at" yaml:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at" yaml:"last_used_at"`
	Attestation string    `json:"attestation,omitempty" yaml:"attestation,omitempty"`
	Transports  []string  `json:"transports,omitempty" yaml:"transports,omitempty"`
}

// MFAInfoExport contains exportable MFA information.
type MFAInfoExport struct {
	Enabled     bool       `json:"enabled" yaml:"enabled"`
	Method      string     `json:"method" yaml:"method"`
	EnrolledAt  *time.Time `json:"enrolled_at,omitempty" yaml:"enrolled_at,omitempty"`
	BackupCodes int        `json:"backup_codes_remaining" yaml:"backup_codes_remaining"`
}

// CreateConsentOptions contains options for creating a consent record.
type CreateConsentOptions struct {
	UserID    uint64            `json:"user_id,string"`
	TenantID  string            `json:"tenant_id"`
	Purpose   ConsentPurpose    `json:"purpose"`
	Status    ConsentStatus     `json:"status"`
	Version   string            `json:"version"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	IPAddress string            `json:"ip_address,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// UpdateConsentOptions contains options for updating a consent record.
type UpdateConsentOptions struct {
	Status    ConsentStatus     `json:"status"`
	Version   string            `json:"version,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	IPAddress string            `json:"ip_address,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// CreateDataExportOptions contains options for creating a data export request.
type CreateDataExportOptions struct {
	UserID            uint64            `json:"user_id,string"`
	TenantID          string            `json:"tenant_id"`
	RequestedBy       uint64            `json:"requested_by,string"`
	Format            DataExportFormat  `json:"format"`
	IncludeCategories []string          `json:"include_categories,omitempty"`
	ExcludeCategories []string          `json:"exclude_categories,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// CreateDeletionRequestOptions contains options for creating a deletion request.
type CreateDeletionRequestOptions struct {
	UserID             uint64            `json:"user_id,string"`
	TenantID           string            `json:"tenant_id"`
	RequestedBy        uint64            `json:"requested_by,string"`
	Reason             string            `json:"reason,omitempty"`
	DeleteCategories   []string          `json:"delete_categories,omitempty"`
	PreserveCategories []string          `json:"preserve_categories,omitempty"`
	GracePeriodDays    int               `json:"grace_period_days,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// ListConsentsOptions contains options for listing consent records.
type ListConsentsOptions struct {
	UserID   uint64         `json:"user_id,string,omitempty"`
	TenantID string         `json:"tenant_id,omitempty"`
	Purpose  ConsentPurpose `json:"purpose,omitempty"`
	Status   ConsentStatus  `json:"status,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}

// ListExportRequestsOptions contains options for listing export requests.
type ListExportRequestsOptions struct {
	UserID   uint64           `json:"user_id,string,omitempty"`
	TenantID string           `json:"tenant_id,omitempty"`
	Status   DataExportStatus `json:"status,omitempty"`
	Limit    int              `json:"limit,omitempty"`
	Offset   int              `json:"offset,omitempty"`
}

// ListDeletionRequestsOptions contains options for listing deletion requests.
type ListDeletionRequestsOptions struct {
	UserID   uint64         `json:"user_id,string,omitempty"`
	TenantID string         `json:"tenant_id,omitempty"`
	Status   DeletionStatus `json:"status,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Offset   int            `json:"offset,omitempty"`
}

// Validate validates a consent record.
func (c *Consent) Validate() error {
	if c.UserID == 0 {
		return NewIAMError(ErrConsentInvalid, "iam.consent.validate", "user_id is required", nil)
	}
	if c.TenantID == "" {
		return NewIAMError(ErrConsentInvalid, "iam.consent.validate", "tenant_id is required", nil)
	}
	if c.Purpose == "" {
		return NewIAMError(ErrConsentInvalid, "iam.consent.validate", "purpose is required", nil)
	}
	if c.Version == "" {
		return NewIAMError(ErrConsentInvalid, "iam.consent.validate", "version is required", nil)
	}
	return nil
}

// IsValidConsentPurpose checks if a consent purpose is valid.
func IsValidConsentPurpose(purpose ConsentPurpose) bool {
	switch purpose {
	case ConsentPurposeMarketing, ConsentPurposeAnalytics, ConsentPurposePersonalization,
		ConsentPurposeThirdParty, ConsentPurposeDataProcessing, ConsentPurposeDataRetention,
		ConsentPurposeFunctional:
		return true
	default:
		return false
	}
}

// IsValidConsentStatus checks if a consent status is valid.
func IsValidConsentStatus(status ConsentStatus) bool {
	switch status {
	case ConsentStatusGranted, ConsentStatusDenied, ConsentStatusWithdrawn, ConsentStatusPending:
		return true
	default:
		return false
	}
}

// DataCategories defines the available data categories for export/deletion.
var DataCategories = []string{
	"profile",
	"sessions",
	"consents",
	"roles",
	"audit_logs",
	"credentials",
	"mfa",
}

// IsValidDataCategory checks if a data category is valid.
func IsValidDataCategory(category string) bool {
	for _, c := range DataCategories {
		if c == category {
			return true
		}
	}
	return false
}

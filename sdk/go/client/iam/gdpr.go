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
	"encoding/json"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	gdprServiceName = "quicbaas.IAM.GDPR"
)

// GDPRClient provides GDPR compliance operations.
//
// The client supports data subject rights under GDPR:
//   - Consent management (Article 7)
//   - Right to access / data portability (Articles 15, 20)
//   - Right to erasure / right to be forgotten (Article 17)
type GDPRClient interface {
	// GrantConsent records user consent for a specific purpose.
	GrantConsent(ctx context.Context, tenantID, userID string, req *GrantConsentRequest) error

	// WithdrawConsent removes user consent for a specific purpose.
	WithdrawConsent(ctx context.Context, tenantID, userID, purpose string) error

	// GetConsent retrieves a user's consent for a specific purpose.
	GetConsent(ctx context.Context, tenantID, userID, purpose string) (*Consent, error)

	// ListConsents returns all consent records for a user.
	ListConsents(ctx context.Context, tenantID, userID string) (*ListConsentsResponse, error)

	// RequestExport initiates a data export request.
	RequestExport(ctx context.Context, tenantID, userID string, req *ExportRequest) (*DataExportRequest, error)

	// GetExportStatus retrieves the status of a data export request.
	GetExportStatus(ctx context.Context, tenantID, userID, exportID string) (*DataExportRequest, error)

	// GenerateExport generates the actual user data export.
	GenerateExport(ctx context.Context, tenantID, userID, exportID string) (*UserDataExport, error)

	// RequestDeletion initiates a data deletion request.
	RequestDeletion(ctx context.Context, tenantID, userID string, req *DeletionRequest) (*DataDeletionRequest, error)

	// CancelDeletion cancels a pending data deletion request.
	CancelDeletion(ctx context.Context, tenantID, userID, deletionID string) error

	// GetDeletionStatus retrieves the status of a data deletion request.
	GetDeletionStatus(ctx context.Context, tenantID, userID, deletionID string) (*DataDeletionRequest, error)
}

// LegalBasis represents the legal basis for data processing under GDPR.
type LegalBasis int

const (
	// LegalBasisConsent means processing is based on explicit consent.
	LegalBasisConsent LegalBasis = iota
	// LegalBasisContract means processing is necessary for contract performance.
	LegalBasisContract
	// LegalBasisLegalObligation means processing is required by law.
	LegalBasisLegalObligation
	// LegalBasisVitalInterests means processing is necessary to protect life.
	LegalBasisVitalInterests
	// LegalBasisPublicTask means processing is necessary for a public task.
	LegalBasisPublicTask
	// LegalBasisLegitimateInterests means processing is based on legitimate interests.
	LegalBasisLegitimateInterests
)

// String returns the string representation of the legal basis.
func (l LegalBasis) String() string {
	switch l {
	case LegalBasisConsent:
		return "consent"
	case LegalBasisContract:
		return "contract"
	case LegalBasisLegalObligation:
		return "legal_obligation"
	case LegalBasisVitalInterests:
		return "vital_interests"
	case LegalBasisPublicTask:
		return "public_task"
	case LegalBasisLegitimateInterests:
		return "legitimate_interests"
	default:
		return "unknown"
	}
}

// ConsentStatus represents the status of a consent record.
type ConsentStatus int

const (
	// ConsentStatusGranted means consent has been given.
	ConsentStatusGranted ConsentStatus = iota
	// ConsentStatusWithdrawn means consent has been withdrawn.
	ConsentStatusWithdrawn
	// ConsentStatusExpired means consent has expired.
	ConsentStatusExpired
)

// String returns the string representation of the consent status.
func (s ConsentStatus) String() string {
	switch s {
	case ConsentStatusGranted:
		return "granted"
	case ConsentStatusWithdrawn:
		return "withdrawn"
	case ConsentStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// MarshalJSON implements json.Marshaler for ConsentStatus.
func (s ConsentStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler for ConsentStatus.
// It handles both integer values (from gRPC) and string values (from REST).
func (s *ConsentStatus) UnmarshalJSON(data []byte) error {
	// Try integer first
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*s = ConsentStatus(intVal)
		return nil
	}

	// Try string
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		switch strVal {
		case "granted":
			*s = ConsentStatusGranted
		case "withdrawn":
			*s = ConsentStatusWithdrawn
		case "expired":
			*s = ConsentStatusExpired
		default:
			return fmt.Errorf("unknown consent status: %s", strVal)
		}
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into ConsentStatus", string(data))
}

// GrantConsentRequest contains the parameters for granting consent.
type GrantConsentRequest struct {
	// Purpose is the data processing purpose.
	Purpose string `json:"purpose"`

	// Version is the consent policy version.
	Version string `json:"version,omitempty"`

	// LegalBasis is the legal basis for processing.
	LegalBasis LegalBasis `json:"legal_basis"`

	// Description is a human-readable description of what is being consented to.
	Description string `json:"description,omitempty"`

	// DataCategories are the categories of personal data involved.
	DataCategories []string `json:"data_categories,omitempty"`

	// ProcessingActivities describe the processing activities.
	ProcessingActivities []string `json:"processing_activities,omitempty"`

	// ThirdParties lists any third parties who may receive the data.
	ThirdParties []string `json:"third_parties,omitempty"`

	// ExpiresAt is when the consent expires (optional).
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Metadata contains additional consent metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Consent represents a user's consent record.
type Consent struct {
	// ID is the unique identifier for this consent.
	ID string `json:"id"`

	// TenantID is the tenant this consent belongs to.
	TenantID string `json:"tenant_id"`

	// UserID is the user who gave consent.
	UserID string `json:"user_id"`

	// Purpose is the data processing purpose.
	Purpose string `json:"purpose"`

	// LegalBasis is the legal basis for processing.
	LegalBasis LegalBasis `json:"legal_basis"`

	// Description is the human-readable consent description.
	Description string `json:"description,omitempty"`

	// DataCategories are the categories of personal data.
	DataCategories []string `json:"data_categories,omitempty"`

	// ProcessingActivities are the processing activities.
	ProcessingActivities []string `json:"processing_activities,omitempty"`

	// ThirdParties are the third parties involved.
	ThirdParties []string `json:"third_parties,omitempty"`

	// Status is the current consent status.
	Status ConsentStatus `json:"status"`

	// GrantedAt is when consent was granted.
	GrantedAt time.Time `json:"granted_at"`

	// WithdrawnAt is when consent was withdrawn (if applicable).
	WithdrawnAt time.Time `json:"withdrawn_at,omitempty"`

	// ExpiresAt is when consent expires.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// IPAddress is the IP address from which consent was given.
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the user agent used when giving consent.
	UserAgent string `json:"user_agent,omitempty"`

	// Metadata contains additional consent metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Version is the consent version for tracking changes.
	Version FlexInt `json:"version"`
}

// ListConsentsResponse contains the list of consent records.
type ListConsentsResponse struct {
	// Consents is the list of consent records.
	Consents []Consent `json:"consents"`

	// TotalCount is the total number of consents.
	TotalCount int `json:"total_count"`
}

// ExportRequest contains the parameters for a data export request.
type ExportRequest struct {
	// Format is the desired export format (json, csv, etc.).
	Format ExportFormat `json:"format"`

	// IncludeCategories limits export to specific data categories.
	IncludeCategories []string `json:"include_categories,omitempty"`

	// ExcludeCategories excludes specific data categories from export.
	ExcludeCategories []string `json:"exclude_categories,omitempty"`

	// EncryptionKey is an optional public key for encrypting the export.
	EncryptionKey string `json:"encryption_key,omitempty"`

	// NotifyEmail is an optional email to notify when export is ready.
	NotifyEmail string `json:"notify_email,omitempty"`

	// Metadata contains additional request metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ExportFormat represents the format of a data export.
type ExportFormat int

const (
	// ExportFormatJSON exports data as JSON.
	ExportFormatJSON ExportFormat = iota
	// ExportFormatCSV exports data as CSV.
	ExportFormatCSV
	// ExportFormatXML exports data as XML.
	ExportFormatXML
)

// String returns the string representation of the export format.
func (f ExportFormat) String() string {
	switch f {
	case ExportFormatJSON:
		return "json"
	case ExportFormatCSV:
		return "csv"
	case ExportFormatXML:
		return "xml"
	default:
		return "unknown"
	}
}

// MarshalJSON implements json.Marshaler for ExportFormat.
func (f ExportFormat) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

// UnmarshalJSON implements json.Unmarshaler for ExportFormat.
// It handles both integer values (from gRPC) and string values (from REST/MCP).
func (f *ExportFormat) UnmarshalJSON(data []byte) error {
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*f = ExportFormat(intVal)
		return nil
	}
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		switch strVal {
		case "json":
			*f = ExportFormatJSON
		case "csv":
			*f = ExportFormatCSV
		case "xml":
			*f = ExportFormatXML
		default:
			return fmt.Errorf("unknown export format: %s", strVal)
		}
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into ExportFormat", string(data))
}

// ExportStatus represents the status of a data export request.
type ExportStatus int

const (
	// ExportStatusPending means the export is queued.
	ExportStatusPending ExportStatus = iota
	// ExportStatusProcessing means the export is being generated.
	ExportStatusProcessing
	// ExportStatusCompleted means the export is ready.
	ExportStatusCompleted
	// ExportStatusFailed means the export failed.
	ExportStatusFailed
	// ExportStatusExpired means the export has expired.
	ExportStatusExpired
)

// String returns the string representation of the export status.
func (s ExportStatus) String() string {
	switch s {
	case ExportStatusPending:
		return "pending"
	case ExportStatusProcessing:
		return "processing"
	case ExportStatusCompleted:
		return "completed"
	case ExportStatusFailed:
		return "failed"
	case ExportStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// MarshalJSON implements json.Marshaler for ExportStatus.
func (s ExportStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler for ExportStatus.
func (s *ExportStatus) UnmarshalJSON(data []byte) error {
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*s = ExportStatus(intVal)
		return nil
	}
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		switch strVal {
		case "pending":
			*s = ExportStatusPending
		case "processing":
			*s = ExportStatusProcessing
		case "completed":
			*s = ExportStatusCompleted
		case "failed":
			*s = ExportStatusFailed
		case "expired":
			*s = ExportStatusExpired
		default:
			return fmt.Errorf("unknown export status: %s", strVal)
		}
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into ExportStatus", string(data))
}

// DataExportRequest represents a data export request.
type DataExportRequest struct {
	// ID is the unique identifier for this request.
	ID string `json:"id"`

	// TenantID is the tenant this request belongs to.
	TenantID string `json:"tenant_id"`

	// UserID is the user whose data is being exported.
	UserID string `json:"user_id"`

	// Status is the current request status.
	Status ExportStatus `json:"status"`

	// Format is the export format.
	Format ExportFormat `json:"format"`

	// IncludeCategories are the data categories included.
	IncludeCategories []string `json:"include_categories,omitempty"`

	// ExcludeCategories are the data categories excluded.
	ExcludeCategories []string `json:"exclude_categories,omitempty"`

	// RequestedAt is when the request was made.
	RequestedAt time.Time `json:"requested_at"`

	// CompletedAt is when the export was completed.
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// ExpiresAt is when the export download link expires.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// DownloadURL is the URL to download the export.
	DownloadURL string `json:"download_url,omitempty"`

	// SizeBytes is the size of the export file.
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// ErrorMessage contains any error message if failed.
	ErrorMessage string `json:"error_message,omitempty"`

	// Metadata contains additional request metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UserDataExport represents the actual exported user data.
type UserDataExport struct {
	// RequestID is the export request ID.
	RequestID string `json:"request_id"`

	// UserID is the user whose data was exported.
	UserID string `json:"user_id"`

	// Format is the export format.
	Format ExportFormat `json:"format"`

	// Data is the exported data.
	Data []byte `json:"data,omitempty"`

	// Encrypted indicates if the data is encrypted.
	Encrypted bool `json:"encrypted"`

	// GeneratedAt is when the export was generated.
	GeneratedAt time.Time `json:"generated_at"`

	// ExpiresAt is when the export expires.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Checksum is the data checksum for verification.
	Checksum string `json:"checksum,omitempty"`

	// Categories lists the data categories included.
	Categories []string `json:"categories,omitempty"`
}

// DeletionRequest contains the parameters for a data deletion request.
type DeletionRequest struct {
	// Reason is the reason for deletion.
	Reason string `json:"reason"`

	// ConfirmEmail confirms the user's email for verification.
	ConfirmEmail string `json:"confirm_email"`

	// RetainCategories are data categories to retain (for legal requirements).
	RetainCategories []string `json:"retain_categories,omitempty"`

	// ScheduledFor schedules the deletion for a future date.
	ScheduledFor time.Time `json:"scheduled_for,omitempty"`

	// NotifyEmail is an email to notify when deletion is complete.
	NotifyEmail string `json:"notify_email,omitempty"`

	// Metadata contains additional request metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DeletionStatus represents the status of a data deletion request.
type DeletionStatus int

const (
	// DeletionStatusPending means deletion is pending confirmation.
	DeletionStatusPending DeletionStatus = iota
	// DeletionStatusScheduled means deletion is scheduled.
	DeletionStatusScheduled
	// DeletionStatusProcessing means deletion is in progress.
	DeletionStatusProcessing
	// DeletionStatusCompleted means deletion is complete.
	DeletionStatusCompleted
	// DeletionStatusCancelled means deletion was cancelled.
	DeletionStatusCancelled
	// DeletionStatusFailed means deletion failed.
	DeletionStatusFailed
)

// String returns the string representation of the deletion status.
func (s DeletionStatus) String() string {
	switch s {
	case DeletionStatusPending:
		return "pending"
	case DeletionStatusScheduled:
		return "scheduled"
	case DeletionStatusProcessing:
		return "processing"
	case DeletionStatusCompleted:
		return "completed"
	case DeletionStatusCancelled:
		return "cancelled"
	case DeletionStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// MarshalJSON implements json.Marshaler for DeletionStatus.
func (s DeletionStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler for DeletionStatus.
func (s *DeletionStatus) UnmarshalJSON(data []byte) error {
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*s = DeletionStatus(intVal)
		return nil
	}
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		switch strVal {
		case "pending":
			*s = DeletionStatusPending
		case "scheduled":
			*s = DeletionStatusScheduled
		case "processing":
			*s = DeletionStatusProcessing
		case "completed":
			*s = DeletionStatusCompleted
		case "cancelled":
			*s = DeletionStatusCancelled
		case "failed":
			*s = DeletionStatusFailed
		default:
			return fmt.Errorf("unknown deletion status: %s", strVal)
		}
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into DeletionStatus", string(data))
}

// DataDeletionRequest represents a data deletion request.
type DataDeletionRequest struct {
	// ID is the unique identifier for this request.
	ID string `json:"id"`

	// TenantID is the tenant this request belongs to.
	TenantID string `json:"tenant_id"`

	// UserID is the user whose data is being deleted.
	UserID string `json:"user_id"`

	// Status is the current request status.
	Status DeletionStatus `json:"status"`

	// Reason is the reason for deletion.
	Reason string `json:"reason,omitempty"`

	// RetainCategories are data categories being retained.
	RetainCategories []string `json:"retain_categories,omitempty"`

	// RequestedAt is when the request was made.
	RequestedAt time.Time `json:"requested_at"`

	// ScheduledFor is when deletion is scheduled.
	ScheduledFor time.Time `json:"scheduled_for,omitempty"`

	// ProcessingStartedAt is when deletion processing started.
	ProcessingStartedAt time.Time `json:"processing_started_at,omitempty"`

	// CompletedAt is when deletion was completed.
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// CancelledAt is when deletion was cancelled.
	CancelledAt time.Time `json:"cancelled_at,omitempty"`

	// CancellationReason is the reason for cancellation.
	CancellationReason string `json:"cancellation_reason,omitempty"`

	// DeletedRecordCount is the number of records deleted.
	DeletedRecordCount int64 `json:"deleted_record_count,omitempty"`

	// ErrorMessage contains any error message if failed.
	ErrorMessage string `json:"error_message,omitempty"`

	// Metadata contains additional request metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Internal request/response types for transport.

type grantConsentRequest struct {
	TenantID string               `json:"tenant_id"`
	UserID   string               `json:"user_id"`
	Consent  *GrantConsentRequest `json:"consent"`
}

type grantConsentResponse struct{}

type withdrawConsentRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Purpose  string `json:"purpose"`
}

type withdrawConsentResponse struct{}

type getConsentRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Purpose  string `json:"purpose"`
}

type listConsentsRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type requestExportRequest struct {
	TenantID string         `json:"tenant_id"`
	UserID   string         `json:"user_id"`
	Export   *ExportRequest `json:"export"`
}

type getExportStatusRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	ExportID string `json:"export_id"`
}

type generateExportRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	ExportID string `json:"export_id"`
}

type requestDeletionRequest struct {
	TenantID string           `json:"tenant_id"`
	UserID   string           `json:"user_id"`
	Deletion *DeletionRequest `json:"deletion"`
}

type cancelDeletionRequest struct {
	TenantID    string `json:"tenant_id"`
	UserID      string `json:"user_id"`
	DeletionID  string `json:"deletion_id"`
	CancelledBy string `json:"cancelled_by"`
}

type cancelDeletionResponse struct{}

type getDeletionStatusRequest struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	DeletionID string `json:"deletion_id"`
}

// gdprClient implements the GDPRClient interface.
type gdprClient struct {
	transport transport.Transport
}

// NewGDPRClient creates a new GDPR compliance client.
//
// Returns nil if the transport is nil.
func NewGDPRClient(t transport.Transport) GDPRClient {
	if t == nil {
		return nil
	}
	return &gdprClient{
		transport: t,
	}
}

// GrantConsent records user consent for a specific purpose.
func (c *gdprClient) GrantConsent(ctx context.Context, tenantID, userID string, req *GrantConsentRequest) error {
	if err := validateGrantConsentRequest(tenantID, userID, req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.gdpr.GrantConsent", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "GrantConsent", &grantConsentRequest{
		TenantID: tenantID,
		UserID:   userID,
		Consent:  req,
	})
	if err != nil {
		return err
	}

	// GrantConsent is a void operation. Any non-error response (nil, typed struct,
	// or map from REST transport) indicates success.
	_ = resp
	return nil
}

// WithdrawConsent removes user consent for a specific purpose.
func (c *gdprClient) WithdrawConsent(ctx context.Context, tenantID, userID, purpose string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.gdpr.WithdrawConsent", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.gdpr.WithdrawConsent", "user ID is required")
	}
	if purpose == "" {
		return errors.NewInvalidInput("iam.gdpr.WithdrawConsent", "purpose is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.gdpr.WithdrawConsent", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "WithdrawConsent", &withdrawConsentRequest{
		TenantID: tenantID,
		UserID:   userID,
		Purpose:  purpose,
	})
	if err != nil {
		return err
	}

	// WithdrawConsent is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// GetConsent retrieves a user's consent for a specific purpose.
func (c *gdprClient) GetConsent(ctx context.Context, tenantID, userID, purpose string) (*Consent, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetConsent", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetConsent", "user ID is required")
	}
	if purpose == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetConsent", "purpose is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.GetConsent", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "GetConsent", &getConsentRequest{
		TenantID: tenantID,
		UserID:   userID,
		Purpose:  purpose,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.gdpr.GetConsent", "consent not found")
	}

	if consent, ok := resp.(*Consent); ok {
		return consent, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[Consent](resp, "iam.gdpr.GetConsent")
}

// ListConsents returns all consent records for a user.
func (c *gdprClient) ListConsents(ctx context.Context, tenantID, userID string) (*ListConsentsResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.ListConsents", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.ListConsents", "user ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.ListConsents", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "ListConsents", &listConsentsRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListConsentsResponse{Consents: []Consent{}}, nil
	}

	if listResp, ok := resp.(*ListConsentsResponse); ok {
		return listResp, nil
	}

	// Handle []interface{} response (REST server may return a JSON array directly).
	if arr, ok := resp.([]interface{}); ok {
		wrapped := map[string]interface{}{
			"consents":    arr,
			"total_count": len(arr),
		}
		return convertMapResponse[ListConsentsResponse](wrapped, "iam.gdpr.ListConsents")
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[ListConsentsResponse](resp, "iam.gdpr.ListConsents")
}

// RequestExport initiates a data export request.
func (c *gdprClient) RequestExport(ctx context.Context, tenantID, userID string, req *ExportRequest) (*DataExportRequest, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestExport", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestExport", "user ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestExport", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "RequestExport", &requestExportRequest{
		TenantID: tenantID,
		UserID:   userID,
		Export:   req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.gdpr.RequestExport", "nil response from server")
	}

	if exportReq, ok := resp.(*DataExportRequest); ok {
		return exportReq, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[DataExportRequest](resp, "iam.gdpr.RequestExport")
}

// GetExportStatus retrieves the status of a data export request.
func (c *gdprClient) GetExportStatus(ctx context.Context, tenantID, userID, exportID string) (*DataExportRequest, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetExportStatus", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetExportStatus", "user ID is required")
	}
	if exportID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetExportStatus", "export ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.GetExportStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "GetExportStatus", &getExportStatusRequest{
		TenantID: tenantID,
		UserID:   userID,
		ExportID: exportID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.gdpr.GetExportStatus", "export request not found")
	}

	if exportReq, ok := resp.(*DataExportRequest); ok {
		return exportReq, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[DataExportRequest](resp, "iam.gdpr.GetExportStatus")
}

// GenerateExport generates the actual user data export.
func (c *gdprClient) GenerateExport(ctx context.Context, tenantID, userID, exportID string) (*UserDataExport, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GenerateExport", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GenerateExport", "user ID is required")
	}
	if exportID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GenerateExport", "export ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.GenerateExport", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "GenerateExport", &generateExportRequest{
		TenantID: tenantID,
		UserID:   userID,
		ExportID: exportID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.gdpr.GenerateExport", "nil response from server")
	}

	if export, ok := resp.(*UserDataExport); ok {
		return export, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[UserDataExport](resp, "iam.gdpr.GenerateExport")
}

// RequestDeletion initiates a data deletion request.
func (c *gdprClient) RequestDeletion(ctx context.Context, tenantID, userID string, req *DeletionRequest) (*DataDeletionRequest, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestDeletion", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestDeletion", "user ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.RequestDeletion", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "RequestDeletion", &requestDeletionRequest{
		TenantID: tenantID,
		UserID:   userID,
		Deletion: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.gdpr.RequestDeletion", "nil response from server")
	}

	if deletionReq, ok := resp.(*DataDeletionRequest); ok {
		return deletionReq, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[DataDeletionRequest](resp, "iam.gdpr.RequestDeletion")
}

// CancelDeletion cancels a pending data deletion request.
func (c *gdprClient) CancelDeletion(ctx context.Context, tenantID, userID, deletionID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.gdpr.CancelDeletion", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.gdpr.CancelDeletion", "user ID is required")
	}
	if deletionID == "" {
		return errors.NewInvalidInput("iam.gdpr.CancelDeletion", "deletion ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.gdpr.CancelDeletion", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "CancelDeletion", &cancelDeletionRequest{
		TenantID:    tenantID,
		UserID:      userID,
		DeletionID:  deletionID,
		CancelledBy: userID,
	})
	if err != nil {
		return err
	}

	// CancelDeletion is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// GetDeletionStatus retrieves the status of a data deletion request.
func (c *gdprClient) GetDeletionStatus(ctx context.Context, tenantID, userID, deletionID string) (*DataDeletionRequest, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetDeletionStatus", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetDeletionStatus", "user ID is required")
	}
	if deletionID == "" {
		return nil, errors.NewInvalidInput("iam.gdpr.GetDeletionStatus", "deletion ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.gdpr.GetDeletionStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, gdprServiceName, "GetDeletionStatus", &getDeletionStatusRequest{
		TenantID:   tenantID,
		UserID:     userID,
		DeletionID: deletionID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.gdpr.GetDeletionStatus", "deletion request not found")
	}

	if deletionReq, ok := resp.(*DataDeletionRequest); ok {
		return deletionReq, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[DataDeletionRequest](resp, "iam.gdpr.GetDeletionStatus")
}

// Validation functions.

func validateGrantConsentRequest(tenantID, userID string, req *GrantConsentRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.gdpr.GrantConsent", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.gdpr.GrantConsent", "user ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.gdpr.GrantConsent", "request is required")
	}
	if req.Purpose == "" {
		return errors.NewInvalidInput("iam.gdpr.GrantConsent", "purpose is required")
	}
	return nil
}

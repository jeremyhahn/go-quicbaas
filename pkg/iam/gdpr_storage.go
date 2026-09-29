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
	"sync"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ==================== MemoryConsentStorage ====================

// MemoryConsentStorage implements ConsentStorage using in-memory maps.
// This implementation is thread-safe and suitable for testing and development.
type MemoryConsentStorage struct {
	mu sync.RWMutex

	// consents maps consent ID to consent record
	consents map[string]*Consent

	// consentsByUser maps tenantID:userID to list of consent IDs
	consentsByUser map[string][]string

	// consentsByPurpose maps tenantID:userID:purpose to consent ID
	consentsByPurpose map[string]string
}

// NewMemoryConsentStorage creates a new in-memory consent storage instance.
func NewMemoryConsentStorage() *MemoryConsentStorage {
	return &MemoryConsentStorage{
		consents:          make(map[string]*Consent),
		consentsByUser:    make(map[string][]string),
		consentsByPurpose: make(map[string]string),
	}
}

// CreateConsent stores a new consent record.
func (m *MemoryConsentStorage) CreateConsent(ctx context.Context, opts CreateConsentOptions) (*Consent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate required fields
	if opts.UserID == "" {
		return nil, NewIAMError(ErrConsentInvalid, "iam.consent.create", "user_id is required", nil)
	}
	if opts.TenantID == "" {
		return nil, NewIAMError(ErrConsentInvalid, "iam.consent.create", "tenant_id is required", nil)
	}
	if opts.Purpose == "" {
		return nil, NewIAMError(ErrConsentInvalid, "iam.consent.create", "purpose is required", nil)
	}
	if opts.Version == "" {
		return nil, NewIAMError(ErrConsentInvalid, "iam.consent.create", "version is required", nil)
	}

	// Check if consent already exists for this user/tenant/purpose combination
	purposeKey := makeKey(opts.TenantID, opts.UserID, string(opts.Purpose))
	if existingID, exists := m.consentsByPurpose[purposeKey]; exists {
		return nil, NewIAMError(
			&dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.consent.create"},
			"iam.consent.create",
			(&ConsentExistsError{Purpose: string(opts.Purpose), ConsentID: existingID}).Error(),
			nil,
		)
	}

	// Generate unique ID
	id, err := generateConsentID()
	if err != nil {
		return nil, &ConsentIDGenerationError{Cause: err}
	}

	now := time.Now()
	consent := &Consent{
		ID:       id,
		UserID:   opts.UserID,
		TenantID: opts.TenantID,
		Purpose:  opts.Purpose,
		Status:   opts.Status,
		Version:  opts.Version,
		Created:  now,
		Updated:  now,
	}

	// Set optional fields
	if opts.ExpiresAt != nil {
		consent.ExpiresAt = opts.ExpiresAt
	}
	if opts.IPAddress != "" {
		consent.IPAddress = opts.IPAddress
	}
	if opts.UserAgent != "" {
		consent.UserAgent = opts.UserAgent
	}
	if len(opts.Metadata) > 0 {
		consent.Metadata = make(map[string]string)
		for k, v := range opts.Metadata {
			consent.Metadata[k] = v
		}
	}

	// Set granted/withdrawn timestamps based on status
	switch opts.Status {
	case ConsentStatusGranted:
		consent.GrantedAt = &now
	case ConsentStatusWithdrawn:
		consent.WithdrawnAt = &now
	}

	// Store consent
	m.consents[id] = consent
	m.consentsByPurpose[purposeKey] = id

	// Add to user's consent list
	userKey := makeKey(opts.TenantID, opts.UserID)
	m.consentsByUser[userKey] = append(m.consentsByUser[userKey], id)

	// Return a copy
	return copyConsent(consent), nil
}

// GetConsent retrieves a consent by user, tenant, and purpose.
func (m *MemoryConsentStorage) GetConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose) (*Consent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	purposeKey := makeKey(tenantID, userID, string(purpose))
	consentID, exists := m.consentsByPurpose[purposeKey]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.get"}
	}

	consent, exists := m.consents[consentID]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.get"}
	}

	return copyConsent(consent), nil
}

// UpdateConsent updates an existing consent record.
func (m *MemoryConsentStorage) UpdateConsent(ctx context.Context, id string, opts UpdateConsentOptions) (*Consent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	consent, exists := m.consents[id]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.update"}
	}

	// Update status
	now := time.Now()
	consent.Status = opts.Status
	consent.Updated = now

	// Update timestamps based on status change
	switch opts.Status {
	case ConsentStatusGranted:
		consent.GrantedAt = &now
		consent.WithdrawnAt = nil
	case ConsentStatusWithdrawn:
		consent.WithdrawnAt = &now
	}

	// Update optional fields
	if opts.Version != "" {
		consent.Version = opts.Version
	}
	if opts.ExpiresAt != nil {
		consent.ExpiresAt = opts.ExpiresAt
	}
	if opts.IPAddress != "" {
		consent.IPAddress = opts.IPAddress
	}
	if opts.UserAgent != "" {
		consent.UserAgent = opts.UserAgent
	}
	if len(opts.Metadata) > 0 {
		if consent.Metadata == nil {
			consent.Metadata = make(map[string]string)
		}
		for k, v := range opts.Metadata {
			consent.Metadata[k] = v
		}
	}

	return copyConsent(consent), nil
}

// DeleteConsent removes a consent record.
func (m *MemoryConsentStorage) DeleteConsent(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	consent, exists := m.consents[id]
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.delete"}
	}

	// Remove from purpose index
	purposeKey := makeKey(consent.TenantID, consent.UserID, string(consent.Purpose))
	delete(m.consentsByPurpose, purposeKey)

	// Remove from user's consent list
	userKey := makeKey(consent.TenantID, consent.UserID)
	if consentIDs, exists := m.consentsByUser[userKey]; exists {
		filtered := make([]string, 0, len(consentIDs))
		for _, cid := range consentIDs {
			if cid != id {
				filtered = append(filtered, cid)
			}
		}
		if len(filtered) > 0 {
			m.consentsByUser[userKey] = filtered
		} else {
			delete(m.consentsByUser, userKey)
		}
	}

	// Remove consent
	delete(m.consents, id)

	return nil
}

// ListConsents retrieves consents matching the filter.
func (m *MemoryConsentStorage) ListConsents(ctx context.Context, opts ListConsentsOptions) ([]*Consent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Consent

	// Collect all matching consents
	for _, consent := range m.consents {
		if m.matchesConsentFilter(consent, opts) {
			result = append(result, copyConsent(consent))
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Consent{}, nil
	}

	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// GetUserConsents retrieves all consents for a user.
func (m *MemoryConsentStorage) GetUserConsents(ctx context.Context, tenantID, userID string) ([]*Consent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userKey := makeKey(tenantID, userID)
	consentIDs, exists := m.consentsByUser[userKey]
	if !exists || len(consentIDs) == 0 {
		return []*Consent{}, nil
	}

	result := make([]*Consent, 0, len(consentIDs))
	for _, id := range consentIDs {
		if consent, exists := m.consents[id]; exists {
			result = append(result, copyConsent(consent))
		}
	}

	return result, nil
}

// matchesConsentFilter checks if a consent matches the filter criteria.
func (m *MemoryConsentStorage) matchesConsentFilter(consent *Consent, opts ListConsentsOptions) bool {
	if opts.UserID != "" && consent.UserID != opts.UserID {
		return false
	}
	if opts.TenantID != "" && consent.TenantID != opts.TenantID {
		return false
	}
	if opts.Purpose != "" && consent.Purpose != opts.Purpose {
		return false
	}
	if opts.Status != "" && consent.Status != opts.Status {
		return false
	}
	return true
}

// ==================== MemoryDataExportStorage ====================

// MemoryDataExportStorage implements DataExportStorage using in-memory maps.
type MemoryDataExportStorage struct {
	mu sync.RWMutex

	// exports maps export request ID to export request
	exports map[string]*DataExportRequest

	// exportsByUser maps tenantID:userID to list of export request IDs
	exportsByUser map[string][]string
}

// NewMemoryDataExportStorage creates a new in-memory data export storage instance.
func NewMemoryDataExportStorage() *MemoryDataExportStorage {
	return &MemoryDataExportStorage{
		exports:       make(map[string]*DataExportRequest),
		exportsByUser: make(map[string][]string),
	}
}

// CreateExportRequest creates a new export request.
func (m *MemoryDataExportStorage) CreateExportRequest(ctx context.Context, opts CreateDataExportOptions) (*DataExportRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate required fields
	if opts.UserID == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "user_id is required", nil)
	}
	if opts.TenantID == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "tenant_id is required", nil)
	}
	if opts.RequestedBy == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "requested_by is required", nil)
	}

	// Generate unique ID
	id, err := generateExportID()
	if err != nil {
		return nil, &ExportIDGenerationError{Cause: err}
	}

	now := time.Now()
	request := &DataExportRequest{
		ID:                id,
		UserID:            opts.UserID,
		TenantID:          opts.TenantID,
		RequestedBy:       opts.RequestedBy,
		Format:            opts.Format,
		Status:            DataExportStatusPending,
		RequestedAt:       now,
		IncludeCategories: make([]string, len(opts.IncludeCategories)),
		ExcludeCategories: make([]string, len(opts.ExcludeCategories)),
	}

	// Copy categories
	copy(request.IncludeCategories, opts.IncludeCategories)
	copy(request.ExcludeCategories, opts.ExcludeCategories)

	// Copy metadata
	if len(opts.Metadata) > 0 {
		request.Metadata = make(map[string]string)
		for k, v := range opts.Metadata {
			request.Metadata[k] = v
		}
	}

	// Store export request
	m.exports[id] = request

	// Add to user's export list
	userKey := makeKey(opts.TenantID, opts.UserID)
	m.exportsByUser[userKey] = append(m.exportsByUser[userKey], id)

	// Return a copy
	return copyDataExportRequest(request), nil
}

// GetExportRequest retrieves an export request by ID.
func (m *MemoryDataExportStorage) GetExportRequest(ctx context.Context, id string) (*DataExportRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	request, exists := m.exports[id]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.export.get"}
	}

	return copyDataExportRequest(request), nil
}

// UpdateExportRequest updates an existing export request.
func (m *MemoryDataExportStorage) UpdateExportRequest(ctx context.Context, req *DataExportRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.exports[req.ID]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.export.update"}
	}

	// Store updated copy
	m.exports[req.ID] = copyDataExportRequest(req)

	return nil
}

// ListExportRequests retrieves export requests matching the filter.
func (m *MemoryDataExportStorage) ListExportRequests(ctx context.Context, opts ListExportRequestsOptions) ([]*DataExportRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*DataExportRequest

	// Collect all matching export requests
	for _, request := range m.exports {
		if m.matchesExportFilter(request, opts) {
			result = append(result, copyDataExportRequest(request))
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*DataExportRequest{}, nil
	}

	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// matchesExportFilter checks if an export request matches the filter criteria.
func (m *MemoryDataExportStorage) matchesExportFilter(request *DataExportRequest, opts ListExportRequestsOptions) bool {
	if opts.UserID != "" && request.UserID != opts.UserID {
		return false
	}
	if opts.TenantID != "" && request.TenantID != opts.TenantID {
		return false
	}
	if opts.Status != "" && request.Status != opts.Status {
		return false
	}
	return true
}

// ==================== MemoryDeletionStorage ====================

// MemoryDeletionStorage implements DeletionStorage using in-memory maps.
type MemoryDeletionStorage struct {
	mu sync.RWMutex

	// deletions maps deletion request ID to deletion request
	deletions map[string]*DataDeletionRequest

	// deletionsByUser maps tenantID:userID to list of deletion request IDs
	deletionsByUser map[string][]string
}

// NewMemoryDeletionStorage creates a new in-memory deletion storage instance.
func NewMemoryDeletionStorage() *MemoryDeletionStorage {
	return &MemoryDeletionStorage{
		deletions:       make(map[string]*DataDeletionRequest),
		deletionsByUser: make(map[string][]string),
	}
}

// CreateDeletionRequest creates a new deletion request.
func (m *MemoryDeletionStorage) CreateDeletionRequest(ctx context.Context, opts CreateDeletionRequestOptions) (*DataDeletionRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate required fields
	if opts.UserID == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "user_id is required", nil)
	}
	if opts.TenantID == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "tenant_id is required", nil)
	}
	if opts.RequestedBy == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "requested_by is required", nil)
	}

	// Generate unique ID
	id, err := generateDeletionID()
	if err != nil {
		return nil, &DeletionIDGenerationError{Cause: err}
	}

	now := time.Now()
	request := &DataDeletionRequest{
		ID:                 id,
		UserID:             opts.UserID,
		TenantID:           opts.TenantID,
		RequestedBy:        opts.RequestedBy,
		Reason:             opts.Reason,
		Status:             DeletionStatusPending,
		RequestedAt:        now,
		DeleteCategories:   make([]string, len(opts.DeleteCategories)),
		PreserveCategories: make([]string, len(opts.PreserveCategories)),
	}

	// Copy categories
	copy(request.DeleteCategories, opts.DeleteCategories)
	copy(request.PreserveCategories, opts.PreserveCategories)

	// Calculate grace period end time
	if opts.GracePeriodDays > 0 {
		gracePeriodEnd := now.Add(time.Duration(opts.GracePeriodDays) * 24 * time.Hour)
		request.GracePeriodEndsAt = &gracePeriodEnd
	}

	// Copy metadata
	if len(opts.Metadata) > 0 {
		request.Metadata = make(map[string]string)
		for k, v := range opts.Metadata {
			request.Metadata[k] = v
		}
	}

	// Store deletion request
	m.deletions[id] = request

	// Add to user's deletion list
	userKey := makeKey(opts.TenantID, opts.UserID)
	m.deletionsByUser[userKey] = append(m.deletionsByUser[userKey], id)

	// Return a copy
	return copyDataDeletionRequest(request), nil
}

// GetDeletionRequest retrieves a deletion request by ID.
func (m *MemoryDeletionStorage) GetDeletionRequest(ctx context.Context, id string) (*DataDeletionRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	request, exists := m.deletions[id]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.deletion.get"}
	}

	return copyDataDeletionRequest(request), nil
}

// UpdateDeletionRequest updates an existing deletion request.
func (m *MemoryDeletionStorage) UpdateDeletionRequest(ctx context.Context, req *DataDeletionRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.deletions[req.ID]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.deletion.update"}
	}

	// Store updated copy
	m.deletions[req.ID] = copyDataDeletionRequest(req)

	return nil
}

// ListDeletionRequests retrieves deletion requests matching the filter.
func (m *MemoryDeletionStorage) ListDeletionRequests(ctx context.Context, opts ListDeletionRequestsOptions) ([]*DataDeletionRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*DataDeletionRequest

	// Collect all matching deletion requests
	for _, request := range m.deletions {
		if m.matchesDeletionFilter(request, opts) {
			result = append(result, copyDataDeletionRequest(request))
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*DataDeletionRequest{}, nil
	}

	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// matchesDeletionFilter checks if a deletion request matches the filter criteria.
func (m *MemoryDeletionStorage) matchesDeletionFilter(request *DataDeletionRequest, opts ListDeletionRequestsOptions) bool {
	if opts.UserID != "" && request.UserID != opts.UserID {
		return false
	}
	if opts.TenantID != "" && request.TenantID != opts.TenantID {
		return false
	}
	if opts.Status != "" && request.Status != opts.Status {
		return false
	}
	return true
}

// ==================== Helper Functions ====================

// copyConsent creates a deep copy of a consent record.
func copyConsent(c *Consent) *Consent {
	if c == nil {
		return nil
	}

	result := &Consent{
		ID:        c.ID,
		UserID:    c.UserID,
		TenantID:  c.TenantID,
		Purpose:   c.Purpose,
		Status:    c.Status,
		Version:   c.Version,
		IPAddress: c.IPAddress,
		UserAgent: c.UserAgent,
		Created:   c.Created,
		Updated:   c.Updated,
	}

	if c.GrantedAt != nil {
		t := *c.GrantedAt
		result.GrantedAt = &t
	}
	if c.WithdrawnAt != nil {
		t := *c.WithdrawnAt
		result.WithdrawnAt = &t
	}
	if c.ExpiresAt != nil {
		t := *c.ExpiresAt
		result.ExpiresAt = &t
	}

	if len(c.Metadata) > 0 {
		result.Metadata = make(map[string]string, len(c.Metadata))
		for k, v := range c.Metadata {
			result.Metadata[k] = v
		}
	}

	return result
}

// copyDataExportRequest creates a deep copy of a data export request.
func copyDataExportRequest(req *DataExportRequest) *DataExportRequest {
	if req == nil {
		return nil
	}

	result := &DataExportRequest{
		ID:                req.ID,
		UserID:            req.UserID,
		TenantID:          req.TenantID,
		RequestedBy:       req.RequestedBy,
		Format:            req.Format,
		Status:            req.Status,
		RequestedAt:       req.RequestedAt,
		DownloadURL:       req.DownloadURL,
		FileSizeBytes:     req.FileSizeBytes,
		ErrorMessage:      req.ErrorMessage,
		IncludeCategories: make([]string, len(req.IncludeCategories)),
		ExcludeCategories: make([]string, len(req.ExcludeCategories)),
	}

	copy(result.IncludeCategories, req.IncludeCategories)
	copy(result.ExcludeCategories, req.ExcludeCategories)

	if req.StartedAt != nil {
		t := *req.StartedAt
		result.StartedAt = &t
	}
	if req.CompletedAt != nil {
		t := *req.CompletedAt
		result.CompletedAt = &t
	}
	if req.ExpiresAt != nil {
		t := *req.ExpiresAt
		result.ExpiresAt = &t
	}

	if len(req.Metadata) > 0 {
		result.Metadata = make(map[string]string, len(req.Metadata))
		for k, v := range req.Metadata {
			result.Metadata[k] = v
		}
	}

	return result
}

// copyDataDeletionRequest creates a deep copy of a data deletion request.
func copyDataDeletionRequest(req *DataDeletionRequest) *DataDeletionRequest {
	if req == nil {
		return nil
	}

	result := &DataDeletionRequest{
		ID:                 req.ID,
		UserID:             req.UserID,
		TenantID:           req.TenantID,
		RequestedBy:        req.RequestedBy,
		Reason:             req.Reason,
		Status:             req.Status,
		RequestedAt:        req.RequestedAt,
		CancelledBy:        req.CancelledBy,
		ErrorMessage:       req.ErrorMessage,
		DeleteCategories:   make([]string, len(req.DeleteCategories)),
		PreserveCategories: make([]string, len(req.PreserveCategories)),
	}

	copy(result.DeleteCategories, req.DeleteCategories)
	copy(result.PreserveCategories, req.PreserveCategories)

	if req.GracePeriodEndsAt != nil {
		t := *req.GracePeriodEndsAt
		result.GracePeriodEndsAt = &t
	}
	if req.StartedAt != nil {
		t := *req.StartedAt
		result.StartedAt = &t
	}
	if req.CompletedAt != nil {
		t := *req.CompletedAt
		result.CompletedAt = &t
	}
	if req.CancelledAt != nil {
		t := *req.CancelledAt
		result.CancelledAt = &t
	}

	if req.DeletedDataSummary != nil {
		result.DeletedDataSummary = &DeletionSummary{
			ProfileDeleted:         req.DeletedDataSummary.ProfileDeleted,
			SessionsDeleted:        req.DeletedDataSummary.SessionsDeleted,
			AuditLogsAnonymized:    req.DeletedDataSummary.AuditLogsAnonymized,
			ConsentsDeleted:        req.DeletedDataSummary.ConsentsDeleted,
			CredentialsDeleted:     req.DeletedDataSummary.CredentialsDeleted,
			RoleAssignmentsRemoved: req.DeletedDataSummary.RoleAssignmentsRemoved,
			TokensRevoked:          req.DeletedDataSummary.TokensRevoked,
		}

		if len(req.DeletedDataSummary.AdditionalData) > 0 {
			result.DeletedDataSummary.AdditionalData = make(map[string]int, len(req.DeletedDataSummary.AdditionalData))
			for k, v := range req.DeletedDataSummary.AdditionalData {
				result.DeletedDataSummary.AdditionalData[k] = v
			}
		}
	}

	if len(req.Metadata) > 0 {
		result.Metadata = make(map[string]string, len(req.Metadata))
		for k, v := range req.Metadata {
			result.Metadata[k] = v
		}
	}

	return result
}

// generateConsentID generates a unique consent ID.
func generateConsentID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "consent_" + hex.EncodeToString(bytes), nil
}

// generateExportID generates a unique export request ID.
func generateExportID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "export_" + hex.EncodeToString(bytes), nil
}

// generateDeletionID generates a unique deletion request ID.
func generateDeletionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "deletion_" + hex.EncodeToString(bytes), nil
}

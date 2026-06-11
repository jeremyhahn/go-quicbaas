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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// GDPR storage key helpers. Records carry an explicit TenantID and the
// create/get-by-user verbs carry an explicit tenantID, so those route directly.
// The by-ID verbs (UpdateConsent/DeleteConsent, GetExportRequest,
// GetDeletionRequest) arrive with only an id, so generated ids are
// tenant-encoded. Per-tenant indexes resolve the by-user and by-purpose lookups
// within the owning tenant's store.
const (
	keyPrefixConsentRec      = KeyPrefixIAM + "gdpr/consent/"
	keyPrefixConsentUserIdx  = KeyPrefixIAM + "gdpr/consent_user/"
	keyPrefixConsentPurpose  = KeyPrefixIAM + "gdpr/consent_purpose/"
	keyPrefixExportRec       = KeyPrefixIAM + "gdpr/export/"
	keyPrefixExportUserIdx   = KeyPrefixIAM + "gdpr/export_user/"
	keyPrefixDeletionRec     = KeyPrefixIAM + "gdpr/deletion/"
	keyPrefixDeletionUserIdx = KeyPrefixIAM + "gdpr/deletion_user/"
)

func consentRecKey(id string) string { return keyPrefixConsentRec + id }
func consentUserIdxKey(userID, id string) string {
	return keyPrefixConsentUserIdx + userID + "/" + id
}
func consentUserIdxPrefix(userID string) string { return keyPrefixConsentUserIdx + userID + "/" }
func consentPurposeKey(userID, purpose string) string {
	return keyPrefixConsentPurpose + userID + "/" + purpose
}
func exportRecKey(id string) string { return keyPrefixExportRec + id }
func exportUserIdxKey(userID, id string) string {
	return keyPrefixExportUserIdx + userID + "/" + id
}
func deletionRecKey(id string) string { return keyPrefixDeletionRec + id }
func deletionUserIdxKey(userID, id string) string {
	return keyPrefixDeletionUserIdx + userID + "/" + id
}

// listTenantIDs returns the tenants a List verb must visit when its filter
// carries no tenant: every known tenant via the lister, or just the context
// tenant when none is configured. A filter that DOES carry a tenant routes
// directly and never reaches this helper.
func listTenantIDs(ctx context.Context, tenants tenantLister, filterTenantID string) ([]string, error) {
	if filterTenantID != "" {
		return []string{filterTenantID}, nil
	}
	return sweepTenantIDs(ctx, tenants)
}

// -----------------------------------------------------------------------------
// BackendConsentStorage — ConsentStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendConsentStorage implements ConsentStorage over the per-tenant routing
// KVStore. Consent records are encrypted-at-rest in the owning tenant's barrier.
type BackendConsentStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendConsentStorage constructs a persistent, per-tenant consent store.
func NewBackendConsentStorage(kv interfaces.KVStore, tenants tenantLister) *BackendConsentStorage {
	return &BackendConsentStorage{kv: kv, tenants: tenants}
}

var _ ConsentStorage = (*BackendConsentStorage)(nil)

// CreateConsent routes by opts.TenantID, mints a tenant-encoded consent id, and
// writes the record plus the per-user and per-purpose indexes.
func (s *BackendConsentStorage) CreateConsent(ctx context.Context, opts CreateConsentOptions) (*Consent, error) {
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
	ctx = withTenant(ctx, opts.TenantID)

	purposeKey := consentPurposeKey(opts.UserID, string(opts.Purpose))
	if existing, err := s.kv.Get(ctx, purposeKey); err == nil {
		return nil, NewIAMError(
			&dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.consent.create"},
			"iam.consent.create",
			(&ConsentExistsError{Purpose: string(opts.Purpose), ConsentID: string(existing)}).Error(),
			nil,
		)
	}

	rawID, err := generateConsentID()
	if err != nil {
		return nil, &ConsentIDGenerationError{Cause: err}
	}
	id := encodeTenantID(opts.TenantID, rawID)

	now := time.Now()
	consent := &Consent{
		ID:        id,
		UserID:    opts.UserID,
		TenantID:  opts.TenantID,
		Purpose:   opts.Purpose,
		Status:    opts.Status,
		Version:   opts.Version,
		IPAddress: opts.IPAddress,
		UserAgent: opts.UserAgent,
		ExpiresAt: opts.ExpiresAt,
		Created:   now,
		Updated:   now,
	}
	if len(opts.Metadata) > 0 {
		consent.Metadata = make(map[string]string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			consent.Metadata[k] = v
		}
	}
	if opts.Status == ConsentStatusGranted {
		consent.GrantedAt = &now
	} else if opts.Status == ConsentStatusWithdrawn {
		consent.WithdrawnAt = &now
	}

	data, err := json.Marshal(consent)
	if err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.consent.create", Err: err}
	}
	if err := s.kv.Put(ctx, consentRecKey(id), data); err != nil {
		return nil, err
	}
	if err := s.kv.Put(ctx, purposeKey, []byte(id)); err != nil {
		return nil, err
	}
	if err := s.kv.Put(ctx, consentUserIdxKey(opts.UserID, id), []byte(id)); err != nil {
		return nil, err
	}
	return copyConsent(consent), nil
}

// GetConsent routes by tenantID and resolves the per-purpose index.
func (s *BackendConsentStorage) GetConsent(ctx context.Context, tenantID, userID string, purpose ConsentPurpose) (*Consent, error) {
	ctx = withTenant(ctx, tenantID)
	idData, err := s.kv.Get(ctx, consentPurposeKey(userID, string(purpose)))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.get"}
		}
		return nil, err
	}
	_, fullID := decodeTenantID(string(idData))
	return s.getRouted(ctx, fullID)
}

func (s *BackendConsentStorage) getRouted(ctx context.Context, fullID string) (*Consent, error) {
	data, err := s.kv.Get(ctx, consentRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.consent.get"}
		}
		return nil, err
	}
	var consent Consent
	if err := json.Unmarshal(data, &consent); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.consent.get", Err: err}
	}
	return &consent, nil
}

// UpdateConsent decodes the tenant from the id, routes, and applies the update.
func (s *BackendConsentStorage) UpdateConsent(ctx context.Context, id string, opts UpdateConsentOptions) (*Consent, error) {
	tenantID, fullID := decodeTenantID(id)
	ctx = withTenant(ctx, tenantID)
	consent, err := s.getRouted(ctx, fullID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	consent.Status = opts.Status
	consent.Updated = now
	if opts.Status == ConsentStatusGranted {
		consent.GrantedAt = &now
		consent.WithdrawnAt = nil
	} else if opts.Status == ConsentStatusWithdrawn {
		consent.WithdrawnAt = &now
	}
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
	data, err := json.Marshal(consent)
	if err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.consent.update", Err: err}
	}
	if err := s.kv.Put(ctx, consentRecKey(fullID), data); err != nil {
		return nil, err
	}
	return copyConsent(consent), nil
}

// DeleteConsent decodes the tenant from the id and removes the record plus
// indexes.
func (s *BackendConsentStorage) DeleteConsent(ctx context.Context, id string) error {
	tenantID, fullID := decodeTenantID(id)
	ctx = withTenant(ctx, tenantID)
	consent, err := s.getRouted(ctx, fullID)
	if err != nil {
		return err
	}
	_ = s.kv.Delete(ctx, consentPurposeKey(consent.UserID, string(consent.Purpose)))
	_ = s.kv.Delete(ctx, consentUserIdxKey(consent.UserID, fullID))
	return s.kv.Delete(ctx, consentRecKey(fullID))
}

// ListConsents routes by opts.TenantID when set; otherwise it iterates the
// tenant list (or the context tenant when no lister is configured) and never
// performs a single global scan.
func (s *BackendConsentStorage) ListConsents(ctx context.Context, opts ListConsentsOptions) ([]*Consent, error) {
	tenantIDs, err := listTenantIDs(ctx, s.tenants, opts.TenantID)
	if err != nil {
		return nil, err
	}
	var result []*Consent
	for _, tid := range tenantIDs {
		tctx := withTenant(ctx, tid)
		if err := s.kv.Scan(tctx, keyPrefixConsentRec, func(_ string, value []byte) error {
			var consent Consent
			if json.Unmarshal(value, &consent) != nil {
				return nil
			}
			if matchesConsentFilter(&consent, opts) {
				result = append(result, copyConsent(&consent))
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return paginateConsents(result, opts), nil
}

// GetUserConsents routes by tenantID and returns every consent in the user
// index.
func (s *BackendConsentStorage) GetUserConsents(ctx context.Context, tenantID, userID string) ([]*Consent, error) {
	ctx = withTenant(ctx, tenantID)
	var ids []string
	if err := s.kv.Scan(ctx, consentUserIdxPrefix(userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	}); err != nil {
		return nil, err
	}
	result := make([]*Consent, 0, len(ids))
	for _, id := range ids {
		_, fullID := decodeTenantID(id)
		consent, err := s.getRouted(ctx, fullID)
		if err != nil {
			if dberrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		result = append(result, copyConsent(consent))
	}
	return result, nil
}

func matchesConsentFilter(consent *Consent, opts ListConsentsOptions) bool {
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

func paginateConsents(in []*Consent, opts ListConsentsOptions) []*Consent {
	start := opts.Offset
	if start >= len(in) {
		return []*Consent{}
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(in) {
		end = len(in)
	}
	return in[start:end]
}

// -----------------------------------------------------------------------------
// BackendDataExportStorage — DataExportStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendDataExportStorage implements DataExportStorage over the per-tenant
// routing KVStore.
type BackendDataExportStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendDataExportStorage constructs a persistent, per-tenant export store.
func NewBackendDataExportStorage(kv interfaces.KVStore, tenants tenantLister) *BackendDataExportStorage {
	return &BackendDataExportStorage{kv: kv, tenants: tenants}
}

var _ DataExportStorage = (*BackendDataExportStorage)(nil)

// CreateExportRequest routes by opts.TenantID, mints a tenant-encoded id, and
// writes the record plus the per-user index.
func (s *BackendDataExportStorage) CreateExportRequest(ctx context.Context, opts CreateDataExportOptions) (*DataExportRequest, error) {
	if opts.UserID == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "user_id is required", nil)
	}
	if opts.TenantID == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "tenant_id is required", nil)
	}
	if opts.RequestedBy == "" {
		return nil, NewIAMError(ErrDataExportInvalid, "iam.export.create", "requested_by is required", nil)
	}
	ctx = withTenant(ctx, opts.TenantID)

	rawID, err := generateExportID()
	if err != nil {
		return nil, &ExportIDGenerationError{Cause: err}
	}
	id := encodeTenantID(opts.TenantID, rawID)

	now := time.Now()
	request := &DataExportRequest{
		ID:                id,
		UserID:            opts.UserID,
		TenantID:          opts.TenantID,
		RequestedBy:       opts.RequestedBy,
		Format:            opts.Format,
		Status:            DataExportStatusPending,
		RequestedAt:       now,
		IncludeCategories: append([]string(nil), opts.IncludeCategories...),
		ExcludeCategories: append([]string(nil), opts.ExcludeCategories...),
	}
	if len(opts.Metadata) > 0 {
		request.Metadata = make(map[string]string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			request.Metadata[k] = v
		}
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.export.create", Err: err}
	}
	if err := s.kv.Put(ctx, exportRecKey(id), data); err != nil {
		return nil, err
	}
	if err := s.kv.Put(ctx, exportUserIdxKey(opts.UserID, id), []byte(id)); err != nil {
		return nil, err
	}
	return copyDataExportRequest(request), nil
}

// GetExportRequest decodes the tenant from the id and reads the record.
func (s *BackendDataExportStorage) GetExportRequest(ctx context.Context, id string) (*DataExportRequest, error) {
	tenantID, fullID := decodeTenantID(id)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, exportRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.export.get"}
		}
		return nil, err
	}
	var request DataExportRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.export.get", Err: err}
	}
	return copyDataExportRequest(&request), nil
}

// UpdateExportRequest decodes the tenant from the record's id, verifies
// existence, and overwrites.
func (s *BackendDataExportStorage) UpdateExportRequest(ctx context.Context, req *DataExportRequest) error {
	tenantID, fullID := decodeTenantID(req.ID)
	ctx = withTenant(ctx, tenantID)
	if _, err := s.kv.Get(ctx, exportRecKey(fullID)); err != nil {
		if dberrors.IsNotFound(err) {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.export.update"}
		}
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.export.update", Err: err}
	}
	return s.kv.Put(ctx, exportRecKey(fullID), data)
}

// ListExportRequests routes by opts.TenantID when set; otherwise iterates the
// tenant list (or context tenant) per tenant.
func (s *BackendDataExportStorage) ListExportRequests(ctx context.Context, opts ListExportRequestsOptions) ([]*DataExportRequest, error) {
	tenantIDs, err := listTenantIDs(ctx, s.tenants, opts.TenantID)
	if err != nil {
		return nil, err
	}
	var result []*DataExportRequest
	for _, tid := range tenantIDs {
		tctx := withTenant(ctx, tid)
		if err := s.kv.Scan(tctx, keyPrefixExportRec, func(_ string, value []byte) error {
			var request DataExportRequest
			if json.Unmarshal(value, &request) != nil {
				return nil
			}
			if matchesExportFilter(&request, opts) {
				result = append(result, copyDataExportRequest(&request))
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return paginateExports(result, opts), nil
}

func matchesExportFilter(request *DataExportRequest, opts ListExportRequestsOptions) bool {
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

func paginateExports(in []*DataExportRequest, opts ListExportRequestsOptions) []*DataExportRequest {
	start := opts.Offset
	if start >= len(in) {
		return []*DataExportRequest{}
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(in) {
		end = len(in)
	}
	return in[start:end]
}

// -----------------------------------------------------------------------------
// BackendDeletionStorage — DeletionStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendDeletionStorage implements DeletionStorage over the per-tenant routing
// KVStore.
type BackendDeletionStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendDeletionStorage constructs a persistent, per-tenant deletion store.
func NewBackendDeletionStorage(kv interfaces.KVStore, tenants tenantLister) *BackendDeletionStorage {
	return &BackendDeletionStorage{kv: kv, tenants: tenants}
}

var _ DeletionStorage = (*BackendDeletionStorage)(nil)

// CreateDeletionRequest routes by opts.TenantID, mints a tenant-encoded id, and
// writes the record plus the per-user index.
func (s *BackendDeletionStorage) CreateDeletionRequest(ctx context.Context, opts CreateDeletionRequestOptions) (*DataDeletionRequest, error) {
	if opts.UserID == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "user_id is required", nil)
	}
	if opts.TenantID == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "tenant_id is required", nil)
	}
	if opts.RequestedBy == "" {
		return nil, NewIAMError(ErrDeletionInvalid, "iam.deletion.create", "requested_by is required", nil)
	}
	ctx = withTenant(ctx, opts.TenantID)

	rawID, err := generateDeletionID()
	if err != nil {
		return nil, &DeletionIDGenerationError{Cause: err}
	}
	id := encodeTenantID(opts.TenantID, rawID)

	now := time.Now()
	request := &DataDeletionRequest{
		ID:                 id,
		UserID:             opts.UserID,
		TenantID:           opts.TenantID,
		RequestedBy:        opts.RequestedBy,
		Reason:             opts.Reason,
		Status:             DeletionStatusPending,
		RequestedAt:        now,
		DeleteCategories:   append([]string(nil), opts.DeleteCategories...),
		PreserveCategories: append([]string(nil), opts.PreserveCategories...),
	}
	if opts.GracePeriodDays > 0 {
		gracePeriodEnd := now.Add(time.Duration(opts.GracePeriodDays) * 24 * time.Hour)
		request.GracePeriodEndsAt = &gracePeriodEnd
	}
	if len(opts.Metadata) > 0 {
		request.Metadata = make(map[string]string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			request.Metadata[k] = v
		}
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.deletion.create", Err: err}
	}
	if err := s.kv.Put(ctx, deletionRecKey(id), data); err != nil {
		return nil, err
	}
	if err := s.kv.Put(ctx, deletionUserIdxKey(opts.UserID, id), []byte(id)); err != nil {
		return nil, err
	}
	return copyDataDeletionRequest(request), nil
}

// GetDeletionRequest decodes the tenant from the id and reads the record.
func (s *BackendDeletionStorage) GetDeletionRequest(ctx context.Context, id string) (*DataDeletionRequest, error) {
	tenantID, fullID := decodeTenantID(id)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, deletionRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.deletion.get"}
		}
		return nil, err
	}
	var request DataDeletionRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.deletion.get", Err: err}
	}
	return copyDataDeletionRequest(&request), nil
}

// UpdateDeletionRequest decodes the tenant from the record's id, verifies
// existence, and overwrites.
func (s *BackendDeletionStorage) UpdateDeletionRequest(ctx context.Context, req *DataDeletionRequest) error {
	tenantID, fullID := decodeTenantID(req.ID)
	ctx = withTenant(ctx, tenantID)
	if _, err := s.kv.Get(ctx, deletionRecKey(fullID)); err != nil {
		if dberrors.IsNotFound(err) {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.deletion.update"}
		}
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.deletion.update", Err: err}
	}
	return s.kv.Put(ctx, deletionRecKey(fullID), data)
}

// ListDeletionRequests routes by opts.TenantID when set; otherwise iterates the
// tenant list (or context tenant) per tenant.
func (s *BackendDeletionStorage) ListDeletionRequests(ctx context.Context, opts ListDeletionRequestsOptions) ([]*DataDeletionRequest, error) {
	tenantIDs, err := listTenantIDs(ctx, s.tenants, opts.TenantID)
	if err != nil {
		return nil, err
	}
	var result []*DataDeletionRequest
	for _, tid := range tenantIDs {
		tctx := withTenant(ctx, tid)
		if err := s.kv.Scan(tctx, keyPrefixDeletionRec, func(_ string, value []byte) error {
			var request DataDeletionRequest
			if json.Unmarshal(value, &request) != nil {
				return nil
			}
			if matchesDeletionFilter(&request, opts) {
				result = append(result, copyDataDeletionRequest(&request))
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return paginateDeletions(result, opts), nil
}

func matchesDeletionFilter(request *DataDeletionRequest, opts ListDeletionRequestsOptions) bool {
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

func paginateDeletions(in []*DataDeletionRequest, opts ListDeletionRequestsOptions) []*DataDeletionRequest {
	start := opts.Offset
	if start >= len(in) {
		return []*DataDeletionRequest{}
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(in) {
		end = len(in)
	}
	return in[start:end]
}

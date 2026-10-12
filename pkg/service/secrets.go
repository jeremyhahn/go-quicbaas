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

// Package service provides the unified service facade for QuicBaaS.
// This file implements the SecretsService interface with multi-tenant isolation.
// When a tenantID is present in the request context, all operations are scoped
// to that tenant's namespace. When tenantID is empty, the system operates in
// single-tenant mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strconv"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// secretsEnsurer is the narrow interface that secretsService.EnsureSecret
// type-asserts on the underlying SecretsManager when a caller requests
// a Vault-style generate-on-first-access operation. The canonical
// implementation lives on *secrets.Manager; any alternative backend
// (vault, k8s, file, memory) that wants to support EnsureSecret must
// implement the same method signature.
//
// The type is narrow on purpose: it is NOT added to interfaces.SecretsManager
// so that unrelated backends do not need stub implementations. Callers
// that only implement Store/Get will receive ErrEnsureSecretUnsupported
// from secretsService.EnsureSecret.
type secretsEnsurer interface {
	EnsureSecret(ctx context.Context, path string, spec *secrets.SecretSpec) (data []byte, generated bool, err error)
}

// ErrEnsureSecretUnsupported is returned by secretsService.EnsureSecret
// when the configured SecretsManager backend does not implement the
// optional EnsureSecret method.
var ErrEnsureSecretUnsupported = stderrors.New("secrets: EnsureSecret is not supported by the configured backend")

// secretsService is the concrete implementation of SecretsService.
// It orchestrates secret storage, versioning, and rotation through the SecretsManager.
type secretsService struct {
	secretsManager interfaces.SecretsManager
	logger         interfaces.Logger
	metrics        *secretsMetrics
}

// secretsMetrics holds Prometheus metrics for secrets operations.
type secretsMetrics struct {
	secretsStoredTotal    *metrics.Counter
	secretsRetrievedTotal *metrics.Counter
	secretsRotatedTotal   *metrics.Counter
	secretsDeletedTotal   *metrics.Counter
	operationDuration     *metrics.HistogramVec
	operationErrors       *metrics.CounterVec
}

// newSecretsService creates a new secrets service.
func newSecretsService(secretsManager interfaces.SecretsManager, logger interfaces.Logger) *secretsService {
	svc := &secretsService{
		secretsManager: secretsManager,
		logger:         logger,
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// initMetrics initializes Prometheus metrics for the secrets service.
func (s *secretsService) initMetrics() {
	s.metrics = &secretsMetrics{
		secretsStoredTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "stored_total",
			Help:      "Total number of secrets stored",
		}),

		secretsRetrievedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "retrieved_total",
			Help:      "Total number of secrets retrieved",
		}),

		secretsRotatedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "rotated_total",
			Help:      "Total number of secrets rotated",
		}),

		secretsDeletedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "deleted_total",
			Help:      "Total number of secrets deleted",
		}),

		operationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "operation_duration_seconds",
			Help:      "Duration of secrets operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"operation"}),

		operationErrors: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "secrets",
			Name:      "operation_errors_total",
			Help:      "Total errors by operation type",
		}, []string{"operation", "error_type"}),
	}
}

// =============================================================================
// SecretsService Implementation
// =============================================================================

// StoreSecret stores a secret with optional metadata and encryption.
func (s *secretsService) StoreSecret(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("store").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateStoreSecretRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("store", "invalid_input").Inc()
		}
		return nil, err
	}

	// Resolve the tenant namespace this write addresses. req.TenantID is
	// reconciled with the context tenant here rather than being applied to
	// the DEK alone, so the write lands in the namespace GetSecret reads
	// and a request naming another tenant is refused (go-quicbaas#534).
	tenantID, err := secretsScopedTenant(ctx, req.TenantID, "StoreSecret")
	if err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":   req.SecretID,
			"secret_name": req.Name,
			"has_data":    len(req.Data) > 0,
			"tenant_id":   tenantID,
		}).Debug("Storing secret")
	}

	// Prepare secret data map
	secretData := map[string][]byte{
		"data": req.Data,
	}

	// Prepare metadata
	metadata := make(map[string]string)
	if req.Description != "" {
		metadata["description"] = req.Description
	}
	if req.ExpiresAt != nil {
		metadata["expires_at"] = req.ExpiresAt.Format(time.RFC3339)
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	for k, v := range req.Tags {
		metadata["tag:"+k] = v
	}

	// Determine path and scope by tenant
	path := req.SecretID
	if path == "" {
		path = req.Name
	}
	scopedPath := secretsTenantScopedID(tenantID, path)

	// Determine the next version by checking existing versions
	var nextVersion uint64 = 1
	existingVersions, listErr := s.secretsManager.ListVersions(ctx, scopedPath)
	if listErr == nil && len(existingVersions) > 0 {
		// Find the highest version number
		nextVersion = uint64(len(existingVersions)) + 1
	}

	// Store the version in metadata
	metadata["version"] = fmt.Sprintf("%d", nextVersion)

	// Build StoreOptions if encryption options are specified
	var opts *interfaces.StoreOptions
	if req.TenantID != "" || req.KeyID != "" || req.Algorithm != "" {
		opts = &interfaces.StoreOptions{
			TenantID:      req.TenantID,
			KeyIdentifier: req.KeyID,
			Algorithm:     req.Algorithm,
		}

		// Store encryption metadata for retrieval
		if req.Algorithm != "" {
			metadata["encryption_algorithm"] = req.Algorithm
		}
		if req.KeyID != "" {
			metadata["encryption_key_id"] = req.KeyID
		}
		if req.TenantID != "" {
			metadata["encryption_tenant_id"] = req.TenantID
		}
	}

	// Store secret through secrets manager with options
	if opts != nil {
		err = s.secretsManager.StoreWithOptions(ctx, scopedPath, secretData, metadata, opts)
	} else {
		err = s.secretsManager.Store(ctx, scopedPath, secretData, metadata)
	}

	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("store", "store_failed").Inc()
		}
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"secret_id": scopedPath,
				"error":     err.Error(),
			}).Error("Failed to store secret")
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.StoreSecret",
			Err:  &api.OperationFailedError{Operation: "store secret", Cause: err},
		}
	}

	// Increment metrics
	if s.metrics != nil {
		s.metrics.secretsStoredTotal.Inc()
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": scopedPath,
		}).Info("Secret stored successfully")
	}

	// Return the original (unscoped) path to callers
	return &StoreSecretResponse{
		SecretID:  path,
		Version:   nextVersion,
		Timestamp: time.Now(),
	}, nil
}

// GetSecret retrieves the latest version of a secret.
func (s *secretsService) GetSecret(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateGetSecretRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get", "invalid_input").Inc()
		}
		return nil, err
	}

	// Resolve the tenant namespace this read addresses. GetSecretRequest
	// carries no tenant of its own, so this is the context tenant — routed
	// through the same resolver StoreSecret uses so the read and the write
	// can never compute different namespaces (go-quicbaas#534).
	tenantID, err := secretsScopedTenant(ctx, "", "GetSecret")
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "GetSecret"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"version":   req.Version,
			"tenant_id": tenantID,
		}).Debug("Retrieving secret")
	}

	// Retrieve secret from secrets manager
	var data map[string][]byte
	var metadata map[string]string

	if req.Version > 0 {
		// Get specific version
		data, metadata, err = s.secretsManager.GetVersion(ctx, scopedID, int(req.Version)) //nolint:gosec // G115: Version represents version number, safe conversion
	} else {
		// Get latest version
		data, metadata, err = s.secretsManager.Get(ctx, scopedID)
	}

	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get", "not_found").Inc()
		}
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"secret_id": req.SecretID,
				"error":     err.Error(),
			}).Warn("Secret not found")
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.GetSecret",
			Err:  &api.OperationFailedError{Operation: "find secret", Cause: err},
		}
	}

	// Check if secret is disabled
	if statusStr, ok := metadata["status"]; ok && statusStr == string(SecretStatusDisabled) {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get", "disabled").Inc()
		}
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"secret_id": req.SecretID,
			}).Warn("Secret is disabled")
		}
		return nil, &errors.QBError{
			Code: errors.ErrForbidden,
			Op:   "secrets.GetSecret",
			Err:  &api.InvalidInputError{Message: "secret is disabled"},
		}
	}

	// Extract secret data
	secretData, ok := data["data"]
	if !ok {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get", "invalid_data").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.GetSecret",
			Err:  &api.NotFoundError{Resource: "secret data field", ID: req.SecretID},
		}
	}

	// Increment metrics
	if s.metrics != nil {
		s.metrics.secretsRetrievedTotal.Inc()
	}

	// Parse metadata
	respMetadata := make(map[string]string)
	for k, v := range metadata {
		if k != "description" && k != "expires_at" && (len(k) < 4 || k[:4] != "tag:") {
			respMetadata[k] = v
		}
	}

	// Parse creation time from metadata if available
	createdAt := time.Now()
	if createdAtStr, ok := metadata["created_at"]; ok {
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = t
		}
	}

	// Extract encryption info from metadata
	algorithm := metadata["encryption_algorithm"]
	keyIdentifier := metadata["encryption_key_id"]
	if keyIdentifier == "" {
		keyIdentifier = metadata["encryption_tenant_id"]
	}

	// Parse version from metadata (backend populates this)
	version := req.Version
	if versionStr, ok := metadata["version"]; ok {
		if v, err := strconv.ParseUint(versionStr, 10, 64); err == nil {
			version = v
		}
	}

	// Return the original (unscoped) secret ID to callers
	return &GetSecretResponse{
		SecretID:      req.SecretID,
		Version:       version,
		Data:          secretData,
		Metadata:      respMetadata,
		CreatedAt:     createdAt,
		Timestamp:     time.Now(),
		Algorithm:     algorithm,
		KeyIdentifier: keyIdentifier,
	}, nil
}

// ListSecrets returns all secrets accessible to the authenticated user.
func (s *secretsService) ListSecrets(ctx context.Context, req *ListSecretsRequest) (*ListSecretsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("list").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if req == nil {
		req = &ListSecretsRequest{}
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the list prefix by tenant
	scopedPrefix := secretsTenantListPrefix(tenantID) + req.Prefix

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"prefix":    req.Prefix,
			"limit":     req.Limit,
			"cursor":    req.Cursor,
			"tenant_id": tenantID,
		}).Debug("Listing secrets")
	}

	// List secrets from secrets manager with tenant-scoped prefix
	paths, err := s.secretsManager.List(ctx, scopedPrefix)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("list", "list_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.ListSecrets",
			Err:  &api.OperationFailedError{Operation: "list secrets", Cause: err},
		}
	}

	// Convert paths to Secret objects, stripping tenant prefix from results
	secrets := make([]*Secret, 0, len(paths))
	for _, path := range paths {
		// Filter: only include secrets that belong to this tenant
		if !secretsTenantBelongsTo(tenantID, path) {
			continue
		}

		// Get metadata for each secret
		_, metadata, err := s.secretsManager.Get(ctx, path)
		if err != nil {
			continue // Skip secrets we can't read
		}

		// Strip tenant prefix for the response
		strippedPath := secretsTenantStripID(tenantID, path)

		secret := &Secret{
			SecretID:       strippedPath,
			Name:           strippedPath,
			Description:    metadata["description"],
			CurrentVersion: 1,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		// Parse tags from metadata
		tags := make(map[string]string)
		for k, v := range metadata {
			if len(k) > 4 && k[:4] == "tag:" {
				tags[k[4:]] = v
			}
		}
		if len(tags) > 0 {
			secret.Tags = tags
		}

		secrets = append(secrets, secret)
	}

	// Apply pagination
	total := len(secrets)
	start_idx := 0
	end_idx := total

	// Simple pagination logic
	if req.Limit > 0 && req.Limit < total {
		end_idx = req.Limit
	}

	hasMore := end_idx < total
	paginatedSecrets := secrets[start_idx:end_idx]

	return &ListSecretsResponse{
		Secrets:    paginatedSecrets,
		NextCursor: "",
		HasMore:    hasMore,
		TotalCount: total,
		Timestamp:  time.Now(),
	}, nil
}

// DeleteSecret permanently removes a secret and all its versions.
func (s *secretsService) DeleteSecret(ctx context.Context, req *DeleteSecretRequest) (*DeleteSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("delete").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateDeleteSecretRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("delete", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "DeleteSecret"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":          req.SecretID,
			"permanently_delete": req.PermanentlyDelete,
			"tenant_id":          tenantID,
		}).Debug("Deleting secret")
	}

	// Delete secret using tenant-scoped ID
	if req.PermanentlyDelete {
		err = s.secretsManager.Destroy(ctx, scopedID)
	} else {
		err = s.secretsManager.Delete(ctx, scopedID)
	}

	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("delete", "delete_failed").Inc()
		}
		// A path absent from the caller's namespace — including one that
		// exists only in another tenant's — is not_found, as on every
		// other verb, not an internal failure (go-quicbaas#281).
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.DeleteSecret",
			Err:  &api.OperationFailedError{Operation: "delete secret", Cause: err},
		}
	}

	// Increment metrics
	if s.metrics != nil {
		s.metrics.secretsDeletedTotal.Inc()
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
		}).Info("Secret deleted successfully")
	}

	// Return the original (unscoped) secret ID to callers
	return &DeleteSecretResponse{
		SecretID:  req.SecretID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

// GetSecretVersion retrieves a specific version of a secret.
func (s *secretsService) GetSecretVersion(ctx context.Context, req *GetSecretVersionRequest) (*GetSecretVersionResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get_version").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateGetSecretVersionRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_version", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "GetSecretVersion"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"version":   req.Version,
			"tenant_id": tenantID,
		}).Debug("Retrieving secret version")
	}

	// Get secret version from secrets manager using tenant-scoped ID
	data, metadata, err := s.secretsManager.GetVersion(ctx, scopedID, int(req.Version)) //nolint:gosec // G115: Version represents version number, safe conversion
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_version", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.GetSecretVersion",
			Err:  &api.OperationFailedError{Operation: "find secret version", Cause: err},
		}
	}

	// Extract secret data
	secretData, ok := data["data"]
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.GetSecretVersion",
			Err:  &api.NotFoundError{Resource: "secret data field", ID: req.SecretID},
		}
	}

	// The version's author lives in the backend's per-version record, which
	// GetVersion does not return. An outage reading it fails the call rather
	// than answering with an empty created_by that reads as "nobody" (#862).
	createdBy, err := s.secretVersionCreatedBy(ctx, scopedID, req.Version)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_version", "list_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.GetSecretVersion",
			Err:  &api.OperationFailedError{Operation: "read secret version author", Cause: err},
		}
	}

	// Build version response with original (unscoped) secret ID
	version := &SecretVersion{
		SecretID:  req.SecretID,
		Version:   req.Version,
		Data:      secretData,
		Metadata:  metadata,
		CreatedAt: time.Now(),
		CreatedBy: createdBy,
	}

	return &GetSecretVersionResponse{
		Version:   version,
		Timestamp: time.Now(),
	}, nil
}

// secretVersionCreatedBy returns the principal recorded as the author of one
// version of the secret at scopedID, "" when the version records none.
func (s *secretsService) secretVersionCreatedBy(ctx context.Context, scopedID string, version uint64) (string, error) {
	versions, err := s.secretsManager.ListVersions(ctx, scopedID)
	if err != nil {
		return "", err
	}
	for _, vm := range versions {
		if uint64(vm.Version) == version { //nolint:gosec // G115: Version is bounded by backend
			return vm.CreatedBy, nil
		}
	}
	return "", nil
}

// ListSecretVersions returns all versions of a secret.
func (s *secretsService) ListSecretVersions(ctx context.Context, req *ListSecretVersionsRequest) (*ListSecretVersionsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("list_versions").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateListSecretVersionsRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("list_versions", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "ListSecretVersions"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"tenant_id": tenantID,
		}).Debug("Listing secret versions")
	}

	// List versions from secrets manager using tenant-scoped ID
	versionMetadata, err := s.secretsManager.ListVersions(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("list_versions", "list_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.ListSecretVersions",
			Err:  &api.OperationFailedError{Operation: "list secret versions", Cause: err},
		}
	}

	// Convert to SecretVersion objects using actual metadata from backend
	// Return the original (unscoped) secret ID in each version
	versions := make([]*SecretVersion, 0, len(versionMetadata))
	for _, vm := range versionMetadata {
		version := &SecretVersion{
			SecretID:  req.SecretID,
			Version:   uint64(vm.Version), //nolint:gosec // G115: Version is bounded by backend
			CreatedAt: vm.CreatedAt,
			CreatedBy: vm.CreatedBy,
		}
		versions = append(versions, version)
	}

	return &ListSecretVersionsResponse{
		Versions:   versions,
		NextCursor: "",
		HasMore:    false,
		Timestamp:  time.Now(),
	}, nil
}

// RollbackSecret rolls back to a previous version of a secret.
func (s *secretsService) RollbackSecret(ctx context.Context, req *RollbackSecretRequest) (*RollbackSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("rollback").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateRollbackSecretRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("rollback", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "RollbackSecret"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":      req.SecretID,
			"target_version": req.TargetVersion,
			"tenant_id":      tenantID,
		}).Debug("Rolling back secret")
	}

	// Rollback through secrets manager using tenant-scoped ID
	if err := s.secretsManager.Rollback(ctx, scopedID, int(req.TargetVersion)); err != nil { //nolint:gosec // G115: TargetVersion represents version number, safe conversion
		if s.metrics != nil {
			s.metrics.operationErrors.With("rollback", "rollback_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.RollbackSecret",
			Err:  &api.OperationFailedError{Operation: "rollback secret", Cause: err},
		}
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
		}).Info("Secret rolled back successfully")
	}

	// Return the original (unscoped) secret ID to callers
	return &RollbackSecretResponse{
		SecretID:   req.SecretID,
		NewVersion: req.TargetVersion + 1, // Rollback creates a new version
		Timestamp:  time.Now(),
	}, nil
}

// RotateSecret manually rotates a secret to a new value.
func (s *secretsService) RotateSecret(ctx context.Context, req *RotateSecretRequest) (*RotateSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("rotate").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateRotateSecretRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("rotate", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "RotateSecret"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"tenant_id": tenantID,
		}).Debug("Rotating secret")
	}

	// Get current version first using tenant-scoped ID
	_, metadata, err := s.secretsManager.Get(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("rotate", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.RotateSecret",
			Err:  &api.OperationFailedError{Operation: "find secret", Cause: err},
		}
	}

	// Determine the old version from metadata
	var oldVersion uint64 = 1
	if versionStr, ok := metadata["version"]; ok {
		if v, parseErr := strconv.ParseUint(versionStr, 10, 64); parseErr == nil {
			oldVersion = v
		}
	}
	newVersion := oldVersion + 1

	// Store new version
	secretData := map[string][]byte{
		"data": req.NewData,
	}

	// Merge metadata
	newMetadata := make(map[string]string)
	for k, v := range metadata {
		newMetadata[k] = v
	}
	for k, v := range req.Metadata {
		newMetadata[k] = v
	}
	// Update the version in metadata
	newMetadata["version"] = fmt.Sprintf("%d", newVersion)

	if err := s.secretsManager.Store(ctx, scopedID, secretData, newMetadata); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("rotate", "store_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.RotateSecret",
			Err:  &api.OperationFailedError{Operation: "rotate secret", Cause: err},
		}
	}

	// Increment metrics
	if s.metrics != nil {
		s.metrics.secretsRotatedTotal.Inc()
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":   req.SecretID,
			"old_version": oldVersion,
			"new_version": newVersion,
		}).Info("Secret rotated successfully")
	}

	// Return the original (unscoped) secret ID to callers
	return &RotateSecretResponse{
		SecretID:   req.SecretID,
		OldVersion: oldVersion,
		NewVersion: newVersion,
		Timestamp:  time.Now(),
	}, nil
}

// SetRotationPolicy sets or updates the automatic rotation policy.
func (s *secretsService) SetRotationPolicy(ctx context.Context, req *SetRotationPolicyRequest) (*SetRotationPolicyResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("set_rotation_policy").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateSetRotationPolicyRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_rotation_policy", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "SetRotationPolicy"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"enabled":   req.Policy.Enabled,
			"tenant_id": tenantID,
		}).Debug("Setting rotation policy")
	}

	// Get current secret metadata using tenant-scoped ID
	_, metadata, err := s.secretsManager.Get(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_rotation_policy", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.SetRotationPolicy",
			Err:  &api.OperationFailedError{Operation: "find secret", Cause: err},
		}
	}

	// Store rotation policy in metadata
	metadata["rotation_enabled"] = fmt.Sprintf("%t", req.Policy.Enabled)
	metadata["rotation_interval"] = req.Policy.Interval.String()
	metadata["rotation_next"] = req.Policy.NextRotation.Format(time.RFC3339)
	metadata["rotation_retain_versions"] = fmt.Sprintf("%d", req.Policy.RetainVersions)

	// Retrieve current secret data for re-storing with updated metadata
	data, _, err := s.secretsManager.Get(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_rotation_policy", "retrieve_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.SetRotationPolicy",
			Err:  &api.OperationFailedError{Operation: "retrieve secret data", Cause: err},
		}
	}
	if err := s.secretsManager.Store(ctx, scopedID, data, metadata); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_rotation_policy", "update_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.SetRotationPolicy",
			Err:  &api.OperationFailedError{Operation: "set rotation policy", Cause: err},
		}
	}

	// Return the original (unscoped) secret ID to callers
	return &SetRotationPolicyResponse{
		SecretID:  req.SecretID,
		Policy:    req.Policy,
		Timestamp: time.Now(),
	}, nil
}

// GetRotationPolicy retrieves the current rotation policy for a secret.
func (s *secretsService) GetRotationPolicy(ctx context.Context, req *GetRotationPolicyRequest) (*GetRotationPolicyResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get_rotation_policy").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateGetRotationPolicyRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_rotation_policy", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "GetRotationPolicy"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"tenant_id": tenantID,
		}).Debug("Getting rotation policy")
	}

	// Get secret metadata using tenant-scoped ID
	_, metadata, err := s.secretsManager.Get(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_rotation_policy", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.GetRotationPolicy",
			Err:  &api.OperationFailedError{Operation: "find secret", Cause: err},
		}
	}

	// Parse rotation policy from metadata
	policy := &SecretRotationPolicy{
		Enabled: false,
	}

	if enabled, ok := metadata["rotation_enabled"]; ok && enabled == "true" {
		policy.Enabled = true
	}
	if interval, ok := metadata["rotation_interval"]; ok {
		if d, err := time.ParseDuration(interval); err == nil {
			policy.Interval = d
		}
	}
	if next, ok := metadata["rotation_next"]; ok {
		if t, err := time.Parse(time.RFC3339, next); err == nil {
			policy.NextRotation = t
		}
	}

	// Return the original (unscoped) secret ID to callers
	return &GetRotationPolicyResponse{
		SecretID:  req.SecretID,
		Policy:    policy,
		Timestamp: time.Now(),
	}, nil
}

// UpdateSecretStatus enables or disables a secret.
func (s *secretsService) UpdateSecretStatus(ctx context.Context, req *UpdateSecretStatusRequest) (*UpdateSecretStatusResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("update_status").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateUpdateSecretStatusRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("update_status", "invalid_input").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the secret ID by tenant
	scopedID := secretsTenantScopedID(tenantID, req.SecretID)

	// Enforce tenant access on the scoped ID
	if err := enforceSecretsTenantAccess(tenantID, scopedID, "UpdateSecretStatus"); err != nil {
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id": req.SecretID,
			"status":    req.Status,
			"tenant_id": tenantID,
		}).Debug("Updating secret status")
	}

	// Get current secret metadata using tenant-scoped ID
	data, metadata, err := s.secretsManager.Get(ctx, scopedID)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("update_status", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: secretLookupCode(err),
			Op:   "secrets.UpdateSecretStatus",
			Err:  &api.OperationFailedError{Operation: "find secret", Cause: err},
		}
	}

	// Get current status
	oldStatus := SecretStatusEnabled
	if statusStr, ok := metadata["status"]; ok {
		oldStatus = SecretStatus(statusStr)
	}

	// Update status in metadata
	metadata["status"] = string(req.Status)
	metadata["status_updated_at"] = time.Now().Format(time.RFC3339)

	// Store updated metadata using tenant-scoped ID
	if err := s.secretsManager.Store(ctx, scopedID, data, metadata); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("update_status", "update_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.UpdateSecretStatus",
			Err:  &api.OperationFailedError{Operation: "update secret status", Cause: err},
		}
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":  req.SecretID,
			"old_status": oldStatus,
			"new_status": req.Status,
		}).Info("Secret status updated successfully")
	}

	// Return the original (unscoped) secret ID to callers
	return &UpdateSecretStatusResponse{
		SecretID:  req.SecretID,
		OldStatus: oldStatus,
		NewStatus: req.Status,
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Validation Methods
// =============================================================================

// validateStoreSecretRequest validates the StoreSecretRequest.
func (s *secretsService) validateStoreSecretRequest(req *StoreSecretRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateStoreSecretRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.Name == "" && req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateStoreSecretRequest",
			Err:  &api.InvalidInputError{Message: "either name or secret_id must be provided"},
		}
	}

	if len(req.Data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateStoreSecretRequest",
			Err:  &api.RequiredFieldError{Field: "secret data"},
		}
	}

	return nil
}

// validateGetSecretRequest validates the GetSecretRequest.
func (s *secretsService) validateGetSecretRequest(req *GetSecretRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetSecretRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetSecretRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	return nil
}

// validateDeleteSecretRequest validates the DeleteSecretRequest.
func (s *secretsService) validateDeleteSecretRequest(req *DeleteSecretRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateDeleteSecretRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateDeleteSecretRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	return nil
}

// validateGetSecretVersionRequest validates the GetSecretVersionRequest.
func (s *secretsService) validateGetSecretVersionRequest(req *GetSecretVersionRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetSecretVersionRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetSecretVersionRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	if req.Version == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetSecretVersionRequest",
			Err:  &api.InvalidInputError{Message: "version must be greater than 0"},
		}
	}

	return nil
}

// validateListSecretVersionsRequest validates the ListSecretVersionsRequest.
func (s *secretsService) validateListSecretVersionsRequest(req *ListSecretVersionsRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateListSecretVersionsRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateListSecretVersionsRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	return nil
}

// validateRollbackSecretRequest validates the RollbackSecretRequest.
func (s *secretsService) validateRollbackSecretRequest(req *RollbackSecretRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRollbackSecretRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRollbackSecretRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	if req.TargetVersion == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRollbackSecretRequest",
			Err:  &api.InvalidInputError{Message: "target_version must be greater than 0"},
		}
	}

	return nil
}

// validateRotateSecretRequest validates the RotateSecretRequest.
func (s *secretsService) validateRotateSecretRequest(req *RotateSecretRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRotateSecretRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRotateSecretRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	if len(req.NewData) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateRotateSecretRequest",
			Err:  &api.RequiredFieldError{Field: "new_data"},
		}
	}

	return nil
}

// validateSetRotationPolicyRequest validates the SetRotationPolicyRequest.
func (s *secretsService) validateSetRotationPolicyRequest(req *SetRotationPolicyRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateSetRotationPolicyRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateSetRotationPolicyRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	if req.Policy == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateSetRotationPolicyRequest",
			Err:  &api.RequiredFieldError{Field: "policy"},
		}
	}

	if req.Policy.Enabled && req.Policy.Interval <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateSetRotationPolicyRequest",
			Err:  &api.InvalidInputError{Message: "interval must be positive when rotation is enabled"},
		}
	}

	return nil
}

// validateGetRotationPolicyRequest validates the GetRotationPolicyRequest.
func (s *secretsService) validateGetRotationPolicyRequest(req *GetRotationPolicyRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetRotationPolicyRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateGetRotationPolicyRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	return nil
}

// validateUpdateSecretStatusRequest validates the UpdateSecretStatusRequest.
func (s *secretsService) validateUpdateSecretStatusRequest(req *UpdateSecretStatusRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateUpdateSecretStatusRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.SecretID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateUpdateSecretStatusRequest",
			Err:  &api.RequiredFieldError{Field: "secret_id"},
		}
	}

	// Validate status value
	switch req.Status {
	case SecretStatusEnabled, SecretStatusDisabled:
		// Valid status values
	default:
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.validateUpdateSecretStatusRequest",
			Err:  &api.InvalidInputError{Message: "invalid status: must be 'enabled' or 'disabled'"},
		}
	}

	return nil
}

// =============================================================================
// Application Secrets - Bulk Retrieval by Prefix
// =============================================================================

// GetSecretsByPrefix fetches all secrets matching a path prefix with their data.
func (s *secretsService) GetSecretsByPrefix(ctx context.Context, req *GetSecretsByPrefixRequest) (*GetSecretsByPrefixResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get_by_prefix").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.GetSecretsByPrefix",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.Prefix == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.GetSecretsByPrefix",
			Err:  &api.RequiredFieldError{Field: "prefix"},
		}
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the prefix by tenant
	scopedPrefix := secretsTenantListPrefix(tenantID) + req.Prefix

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"prefix":       req.Prefix,
			"include_data": req.IncludeData,
			"limit":        req.Limit,
			"tenant_id":    tenantID,
		}).Debug("Fetching secrets by prefix")
	}

	// List secrets with tenant-scoped prefix
	paths, err := s.secretsManager.List(ctx, scopedPrefix)
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_by_prefix", "list_failed").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.GetSecretsByPrefix",
			Err:  &api.OperationFailedError{Operation: "list secrets", Cause: err},
		}
	}

	// Apply limit if specified
	total := len(paths)
	hasMore := false
	if req.Limit > 0 && req.Limit < total {
		paths = paths[:req.Limit]
		hasMore = true
	}

	// Fetch data for each secret, stripping tenant prefix from result keys
	secrets := make(map[string]map[string][]byte)
	metadata := make(map[string]map[string]string)

	for _, path := range paths {
		// Filter: only include secrets that belong to this tenant
		if !secretsTenantBelongsTo(tenantID, path) {
			continue
		}

		data, meta, err := s.secretsManager.Get(ctx, path)
		if err != nil {
			// Skip secrets we can't read
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"path":  path,
					"error": err.Error(),
				}).Warn("Failed to read secret during prefix fetch")
			}
			continue
		}

		// Check if secret is enabled
		if statusStr, ok := meta["status"]; ok && statusStr == string(SecretStatusDisabled) {
			continue
		}

		// Strip tenant prefix for the response
		strippedPath := secretsTenantStripID(tenantID, path)

		// Add to results with unscoped path
		if req.IncludeData {
			secrets[strippedPath] = data
		}
		metadata[strippedPath] = meta
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"prefix":       req.Prefix,
			"secret_count": len(metadata),
		}).Info("Fetched secrets by prefix")
	}

	return &GetSecretsByPrefixResponse{
		Secrets:    secrets,
		Metadata:   metadata,
		Version:    time.Now().UnixNano(), // Use timestamp as version for now
		HasMore:    hasMore,
		TotalCount: total,
		Timestamp:  time.Now(),
	}, nil
}

// WatchSecretsByPrefix streams secret changes for a prefix.
func (s *secretsService) WatchSecretsByPrefix(ctx context.Context, req *WatchSecretsRequest) (<-chan *SecretChange, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.WatchSecretsByPrefix",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if req.Prefix == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.WatchSecretsByPrefix",
			Err:  &api.RequiredFieldError{Field: "prefix"},
		}
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the prefix by tenant
	scopedPrefix := secretsTenantListPrefix(tenantID) + req.Prefix

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"prefix":        req.Prefix,
			"start_version": req.StartVersion,
			"tenant_id":     tenantID,
		}).Debug("Starting watch on secrets prefix")
	}

	// Create the change channel
	changes := make(chan *SecretChange, 100)

	// Start a goroutine to watch for changes
	go func() {
		defer close(changes)

		// Get initial list of secrets for comparison using tenant-scoped prefix
		initialPaths, err := s.secretsManager.List(ctx, scopedPrefix)
		if err != nil {
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"prefix": req.Prefix,
					"error":  err.Error(),
				}).Error("Failed to get initial secrets for watch")
			}
			return
		}

		// Track known secrets and their versions
		knownSecrets := make(map[string]string) // path -> version
		for _, path := range initialPaths {
			if !secretsTenantBelongsTo(tenantID, path) {
				continue
			}
			_, meta, err := s.secretsManager.Get(ctx, path)
			if err == nil {
				knownSecrets[path] = meta["version"]
			}
		}

		// Poll for changes (in a real implementation, this would use the watch API)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Check for new/updated/deleted secrets using tenant-scoped prefix
				currentPaths, err := s.secretsManager.List(ctx, scopedPrefix)
				if err != nil {
					continue
				}

				// Check for new and updated secrets
				currentPathsSet := make(map[string]bool)
				for _, path := range currentPaths {
					if !secretsTenantBelongsTo(tenantID, path) {
						continue
					}
					currentPathsSet[path] = true

					data, meta, err := s.secretsManager.Get(ctx, path)
					if err != nil {
						continue
					}

					// Strip tenant prefix for change events
					strippedPath := secretsTenantStripID(tenantID, path)

					currentVersion := meta["version"]
					if oldVersion, exists := knownSecrets[path]; !exists {
						// New secret
						select {
						case changes <- &SecretChange{
							Path:       strippedPath,
							ChangeType: SecretChangeCreated,
							Data:       data,
							Metadata:   meta,
							Version:    time.Now().UnixNano(),
							Timestamp:  time.Now(),
						}:
						case <-ctx.Done():
							return
						}
						knownSecrets[path] = currentVersion
					} else if oldVersion != currentVersion {
						// Updated secret
						select {
						case changes <- &SecretChange{
							Path:       strippedPath,
							ChangeType: SecretChangeUpdated,
							Data:       data,
							Metadata:   meta,
							Version:    time.Now().UnixNano(),
							Timestamp:  time.Now(),
						}:
						case <-ctx.Done():
							return
						}
						knownSecrets[path] = currentVersion
					}
				}

				// Check for deleted secrets
				for path := range knownSecrets {
					if !currentPathsSet[path] {
						// Strip tenant prefix for change events
						strippedPath := secretsTenantStripID(tenantID, path)
						select {
						case changes <- &SecretChange{
							Path:       strippedPath,
							ChangeType: SecretChangeDeleted,
							Version:    time.Now().UnixNano(),
							Timestamp:  time.Now(),
						}:
						case <-ctx.Done():
							return
						}
						delete(knownSecrets, path)
					}
				}
			}
		}
	}()

	return changes, nil
}

// EnsureSecret implements the Vault-style generate-on-first-access
// primitive at the service layer. See SecretsService.EnsureSecret in
// pkg/service/secrets_service.go for the full contract.
//
// Implementation delegates to the underlying SecretsManager via the
// secretsEnsurer narrow interface. The concrete backing is
// *secrets.Manager which implements the distributed-lock-serialized
// generate-or-read logic. Callers whose backend does not implement
// secretsEnsurer receive ErrEnsureSecretUnsupported.
func (s *secretsService) EnsureSecret(ctx context.Context, req *EnsureSecretRequest) (*EnsureSecretResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("ensure").Observe(time.Since(start).Seconds())
		}
	}()

	if req == nil || req.SecretID == "" {
		if s.metrics != nil {
			s.metrics.operationErrors.With("ensure", "invalid_input").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.EnsureSecret",
			Err:  stderrors.New("secret_id (path) is required"),
		}
	}

	spec := &secrets.SecretSpec{Length: req.Length, Encoding: req.Encoding}
	if err := spec.Validate(); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("ensure", "invalid_input").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "secrets.EnsureSecret",
			Err:  err,
		}
	}

	// Type-assert the backend to the optional ensurer interface.
	// Unsupported backends fail fast with a clear error — no silent
	// fallback to a non-persistent generator.
	ensurer, ok := s.secretsManager.(secretsEnsurer)
	if !ok {
		if s.metrics != nil {
			s.metrics.operationErrors.With("ensure", "unsupported_backend").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "secrets.EnsureSecret",
			Err:  ErrEnsureSecretUnsupported,
		}
	}

	// Resolve the tenant namespace — the same resolver StoreSecret and
	// GetSecret use, so a generated secret lands where the tenant's other
	// verbs look for it. req.TenantID used to be applied unchecked here,
	// which let any caller steer a generate-and-store into another
	// tenant's namespace (go-quicbaas#534).
	tenantID, err := secretsScopedTenant(ctx, req.TenantID, "EnsureSecret")
	if err != nil {
		return nil, err
	}
	scopedPath := secretsTenantScopedID(tenantID, req.SecretID)

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"path":      scopedPath,
			"tenant_id": tenantID,
			"length":    spec.Length,
			"encoding":  spec.Encoding,
		}).Debug("EnsureSecret")
	}

	// Delegate to the Manager. The generated flag returned here is
	// authoritative: it is computed inside the Manager's locked
	// critical section, so exactly one caller in a concurrent race
	// on the same path sees generated=true and the rest see false.
	// No pre-check outside the lock — that introduces a race where
	// multiple callers report Generated=true based on stale reads.
	data, generated, ensureErr := ensurer.EnsureSecret(ctx, scopedPath, spec)
	if ensureErr != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("ensure", "backend_error").Inc()
		}
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"path":  scopedPath,
				"error": ensureErr.Error(),
			}).Error("EnsureSecret backend error")
		}
		return nil, ensureErr
	}

	if generated && s.metrics != nil {
		s.metrics.secretsStoredTotal.Inc()
	}
	if !generated && s.metrics != nil {
		s.metrics.secretsRetrievedTotal.Inc()
	}

	return &EnsureSecretResponse{
		SecretID:  req.SecretID,
		Data:      data,
		Generated: generated,
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

// secretLookupCode classifies a secrets-manager read failure.
//
// ONLY A STORE THAT ANSWERED "ABSENT" IS NOT-FOUND. Every read here once
// reported any failure as ErrNotFound, so a sealed barrier or a failing
// backend told the caller that a secret it had named correctly did not exist
// — and a reference check on save then refused a correct reference. A
// failure that is not an absence keeps its own code, or is unavailable when
// it carries none.
func secretLookupCode(err error) errors.ErrorCode {
	switch {
	case errors.IsNotFound(err),
		stderrors.Is(err, secrets.ErrSecretNotFound),
		stderrors.Is(err, secrets.ErrSecretDeleted),
		stderrors.Is(err, secrets.ErrVersionNotFound):
		return errors.ErrNotFound
	case errors.Code(err) != errors.ErrUnknown:
		return errors.Code(err)
	default:
		return errors.ErrUnavailable
	}
}

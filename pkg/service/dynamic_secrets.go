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
package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

const (
	// RolePrefix is the storage key prefix for credential roles.
	RolePrefix = "/_roles/"

	// CredentialPrefix is the storage key prefix for generated credentials.
	CredentialPrefix = "/_credentials/"

	// MinRoleTTL is the minimum allowed default TTL for a role.
	MinRoleTTL = 1 * time.Minute

	// MaxRoleTTL is the maximum allowed TTL for a role.
	MaxRoleTTL = 365 * 24 * time.Hour
)

// dynamicSecretsHandler is the concrete implementation of dynamicSecretsHandler.
type dynamicSecretsHandler struct {
	store           storage.Storage
	leaseManager    lease.LeaseManager
	generatorReg    *credentials.GeneratorRegistry
	logger          interfaces.Logger
	metrics         *dynamicSecretsMetrics
	mu              sync.RWMutex
	credentialRoles map[string]*credentials.Role
}

// dynamicSecretsMetrics tracks dynamic secrets operations.
type dynamicSecretsMetrics struct {
	rolesCreated         *metrics.CounterVec
	credentialsGenerated *metrics.CounterVec
	leasesRenewed        *metrics.CounterVec
	leasesRevoked        *metrics.CounterVec
	activeCredentials    *metrics.Gauge
	opDuration           *metrics.HistogramVec
}

// DynamicSecretsConfig contains configuration for the dynamic secrets service.
type DynamicSecretsConfig struct {
	// Store is the underlying storage for roles and credentials.
	Store storage.Storage

	// LeaseManager manages credential leases.
	LeaseManager lease.LeaseManager

	// GeneratorRegistry contains registered credential generators.
	GeneratorRegistry *credentials.GeneratorRegistry

	// Logger for operation logging.
	Logger interfaces.Logger
}

// NewDynamicSecretsHandler creates a new dynamic secrets service implementation.
func NewDynamicSecretsHandler(config *DynamicSecretsConfig) (*dynamicSecretsHandler, error) {
	if config == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "NewDynamicSecretsHandler",
			Err:  stderrors.New("config cannot be nil"),
		}
	}

	if config.Store == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "NewDynamicSecretsHandler",
			Err:  stderrors.New("store cannot be nil"),
		}
	}

	if config.LeaseManager == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "NewDynamicSecretsHandler",
			Err:  stderrors.New("lease manager cannot be nil"),
		}
	}

	if config.GeneratorRegistry == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "NewDynamicSecretsHandler",
			Err:  stderrors.New("generator registry cannot be nil"),
		}
	}

	if config.Logger == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "NewDynamicSecretsHandler",
			Err:  stderrors.New("logger cannot be nil"),
		}
	}

	svc := &dynamicSecretsHandler{
		store:           config.Store,
		leaseManager:    config.LeaseManager,
		generatorReg:    config.GeneratorRegistry,
		logger:          config.Logger.WithField("component", "dynamic_secrets_service"),
		credentialRoles: make(map[string]*credentials.Role),
	}

	svc.initMetrics()

	// Load existing roles from storage
	if err := svc.loadRoles(); err != nil {
		return nil, err
	}

	svc.logger.Info("Dynamic secrets service initialized")

	return svc, nil
}

// initMetrics initializes Prometheus metrics for dynamic secrets operations.
func (s *dynamicSecretsHandler) initMetrics() {
	s.metrics = &dynamicSecretsMetrics{
		rolesCreated: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "roles_created_total",
			Help:      "Total number of credential roles created",
		}, []string{"generator_type", "status"}),

		credentialsGenerated: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "credentials_generated_total",
			Help:      "Total number of credentials generated",
		}, []string{"generator_type", "status"}),

		leasesRenewed: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "leases_renewed_total",
			Help:      "Total number of credential leases renewed",
		}, []string{"status"}),

		leasesRevoked: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "leases_revoked_total",
			Help:      "Total number of credential leases revoked",
		}, []string{"status"}),

		activeCredentials: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "active_credentials",
			Help:      "Current number of active credentials",
		}),

		opDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "dynamic_secrets",
			Name:      "operation_duration_seconds",
			Help:      "Duration of dynamic secrets operations",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"operation"}),
	}
}

// loadRoles loads existing roles from storage into memory.
func (s *dynamicSecretsHandler) loadRoles() error {
	iter := s.store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte(RolePrefix),
		UpperBound: []byte(RolePrefix + "~"),
	})
	defer iter.Close()

	count := 0
	for iter.Next() {
		role := &credentials.Role{}
		if err := json.Unmarshal(iter.Value(), role); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal role")
			continue
		}

		s.credentialRoles[role.Name] = role
		count++
	}

	s.logger.WithField("count", count).Info("Loaded credential roles")
	return nil
}

// CreateRole creates a new credential generation role.
func (s *dynamicSecretsHandler) CreateRole(ctx context.Context, req *CreateCredentialRoleRequest) (*CreateCredentialRoleResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("create_role").Observe(time.Since(start).Seconds())
	}()

	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.CreateRole",
			Err:  stderrors.New("request cannot be nil"),
		}
	}

	if req.Name == "" {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.CreateRole",
			Err:  stderrors.New("role name cannot be empty"),
		}
	}

	if req.GeneratorType == "" {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.CreateRole",
			Err:  stderrors.New("generator type cannot be empty"),
		}
	}

	if req.DefaultTTL < MinRoleTTL {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewMinDurationValueError(
			"dynamicSecretsHandler.CreateRole", "default TTL", MinRoleTTL)
	}

	if req.MaxTTL > MaxRoleTTL {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewMaxDurationValueError(
			"dynamicSecretsHandler.CreateRole", "max TTL", MaxRoleTTL)
	}

	if req.DefaultTTL > req.MaxTTL {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewDefaultTTLExceedsMaxError("dynamicSecretsHandler.CreateRole")
	}

	if ctx.Err() != nil {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.CreateRole",
			Err:  ctx.Err(),
		}
	}

	// Check if generator exists
	generator, err := s.generatorReg.Get(req.GeneratorType)
	if err != nil {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewGeneratorNotFoundError("dynamicSecretsHandler.CreateRole", err)
	}

	// Validate configuration
	if err := generator.ValidateConfig(req.Config); err != nil {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewGeneratorConfigError("dynamicSecretsHandler.CreateRole", err)
	}

	// Check if role already exists
	s.mu.RLock()
	_, exists := s.credentialRoles[req.Name]
	s.mu.RUnlock()

	if exists {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, api.NewAlreadyExistsError("dynamicSecretsHandler.CreateRole", "role", req.Name)
	}

	// Create role
	role := &credentials.Role{
		Name:          req.Name,
		GeneratorType: req.GeneratorType,
		Config:        req.Config,
		DefaultTTL:    req.DefaultTTL,
		MaxTTL:        req.MaxTTL,
	}

	// Persist role
	if err := s.storeRole(role); err != nil {
		s.metrics.rolesCreated.With(req.GeneratorType, "error").Inc()
		return nil, err
	}

	// Update in-memory cache
	s.mu.Lock()
	s.credentialRoles[role.Name] = role
	s.mu.Unlock()

	s.metrics.rolesCreated.With(req.GeneratorType, "success").Inc()

	s.logger.WithFields(map[string]interface{}{
		"role":           req.Name,
		"generator_type": req.GeneratorType,
		"default_ttl":    req.DefaultTTL.String(),
		"max_ttl":        req.MaxTTL.String(),
	}).Info("Credential role created")

	return &CreateCredentialRoleResponse{
		Role:      role,
		CreatedAt: time.Now(),
	}, nil
}

// GetRole retrieves a credential generation role by name.
func (s *dynamicSecretsHandler) GetRole(ctx context.Context, req *GetCredentialRoleRequest) (*GetCredentialRoleResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("get_role").Observe(time.Since(start).Seconds())
	}()

	if req == nil || req.Name == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.GetRole",
			Err:  stderrors.New("role name cannot be empty"),
		}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.GetRole",
			Err:  ctx.Err(),
		}
	}

	s.mu.RLock()
	role, exists := s.credentialRoles[req.Name]
	s.mu.RUnlock()

	if !exists {
		return nil, api.NewNotFoundByNameError("dynamicSecretsHandler.GetRole", "role", req.Name)
	}

	return &GetCredentialRoleResponse{Role: role}, nil
}

// ListRoles returns all credential generation roles.
func (s *dynamicSecretsHandler) ListRoles(ctx context.Context, req *ListCredentialRolesRequest) (*ListCredentialRolesResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("list_roles").Observe(time.Since(start).Seconds())
	}()

	if req == nil {
		req = &ListCredentialRolesRequest{}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.ListRoles",
			Err:  ctx.Err(),
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	roles := make([]*credentials.Role, 0, len(s.credentialRoles))
	for _, role := range s.credentialRoles {
		// Filter by prefix if specified
		if req.Prefix != "" && !strings.HasPrefix(role.Name, req.Prefix) {
			continue
		}
		roles = append(roles, role)
	}

	return &ListCredentialRolesResponse{
		Roles: roles,
		Count: len(roles),
	}, nil
}

// DeleteRole removes a credential generation role.
func (s *dynamicSecretsHandler) DeleteRole(ctx context.Context, req *DeleteCredentialRoleRequest) (*DeleteCredentialRoleResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("delete_role").Observe(time.Since(start).Seconds())
	}()

	if req == nil || req.Name == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.DeleteRole",
			Err:  stderrors.New("role name cannot be empty"),
		}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.DeleteRole",
			Err:  ctx.Err(),
		}
	}

	// Check if role exists
	s.mu.RLock()
	_, exists := s.credentialRoles[req.Name]
	s.mu.RUnlock()

	if !exists {
		return nil, api.NewNotFoundByNameError("dynamicSecretsHandler.DeleteRole", "role", req.Name)
	}

	// Delete from storage
	key := []byte(RolePrefix + req.Name)
	if err := s.store.Delete(key); err != nil {
		return nil, api.NewOperationFailedError("delete role", err)
	}

	// Remove from in-memory cache
	s.mu.Lock()
	delete(s.credentialRoles, req.Name)
	s.mu.Unlock()

	s.logger.WithField("role", req.Name).Info("Credential role deleted")

	return &DeleteCredentialRoleResponse{DeletedAt: time.Now()}, nil
}

// GenerateCredential generates a new dynamic credential based on a role.
func (s *dynamicSecretsHandler) GenerateCredential(ctx context.Context, req *GenerateCredentialRequest) (*GenerateCredentialResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("generate_credential").Observe(time.Since(start).Seconds())
	}()

	if req == nil || req.RoleName == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.GenerateCredential",
			Err:  stderrors.New("role name cannot be empty"),
		}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.GenerateCredential",
			Err:  ctx.Err(),
		}
	}

	// Get role
	s.mu.RLock()
	role, exists := s.credentialRoles[req.RoleName]
	s.mu.RUnlock()

	if !exists {
		s.metrics.credentialsGenerated.With("unknown", "error").Inc()
		return nil, api.NewNotFoundByNameError("dynamicSecretsHandler.GenerateCredential", "role", req.RoleName)
	}

	// Determine TTL
	ttl := req.TTL
	if ttl == 0 {
		ttl = role.DefaultTTL
	}
	if ttl > role.MaxTTL {
		s.metrics.credentialsGenerated.With(role.GeneratorType, "error").Inc()
		return nil, api.NewTTLExceedsMaxError("dynamicSecretsHandler.GenerateCredential", ttl, role.MaxTTL)
	}

	// Get generator
	generator, err := s.generatorReg.Get(role.GeneratorType)
	if err != nil {
		s.metrics.credentialsGenerated.With(role.GeneratorType, "error").Inc()
		return nil, api.NewOperationFailedError("get generator", err)
	}

	// Generate credential
	credential, err := generator.Generate(ctx, role)
	if err != nil {
		s.metrics.credentialsGenerated.With(role.GeneratorType, "error").Inc()
		return nil, api.NewOperationFailedError("generate credential", err)
	}

	// Create lease
	leasePath := fmt.Sprintf("credentials/%s/%s", req.RoleName, credential.CredentialID)
	leaseOpts := &lease.LeaseOptions{
		Renewable:   true,
		MaxRenewals: 10,
		Metadata: map[string]string{
			"role":          req.RoleName,
			"credential_id": credential.CredentialID,
			"generator":     role.GeneratorType,
		},
	}

	credLease, err := s.leaseManager.CreateLease(ctx, leasePath, ttl, leaseOpts)
	if err != nil {
		s.metrics.credentialsGenerated.With(role.GeneratorType, "error").Inc()
		return nil, api.NewOperationFailedError("create lease", err)
	}

	// Associate lease with credential
	credential.LeaseID = credLease.LeaseID

	// Store credential
	if err := s.storeCredential(credential); err != nil {
		s.metrics.credentialsGenerated.With(role.GeneratorType, "error").Inc()
		return nil, err
	}

	s.metrics.credentialsGenerated.With(role.GeneratorType, "success").Inc()
	s.metrics.activeCredentials.Inc()

	s.logger.WithFields(map[string]interface{}{
		"credential_id": credential.CredentialID,
		"role":          req.RoleName,
		"lease_id":      credLease.LeaseID,
		"ttl":           ttl.String(),
	}).Info("Dynamic credential generated")

	return &GenerateCredentialResponse{
		Credential: credential,
		Lease:      credLease,
		LeaseID:    credLease.LeaseID,
	}, nil
}

// RenewLease extends the expiration time of a credential lease.
func (s *dynamicSecretsHandler) RenewLease(ctx context.Context, req *RenewLeaseRequest) (*RenewLeaseResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("renew_lease").Observe(time.Since(start).Seconds())
	}()

	if req == nil || req.LeaseID == "" {
		s.metrics.leasesRenewed.With("error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.RenewLease",
			Err:  stderrors.New("lease ID cannot be empty"),
		}
	}

	if ctx.Err() != nil {
		s.metrics.leasesRenewed.With("error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.RenewLease",
			Err:  ctx.Err(),
		}
	}

	renewedLease, err := s.leaseManager.RenewLease(ctx, req.LeaseID, req.Increment)
	if err != nil {
		s.metrics.leasesRenewed.With("error").Inc()
		return nil, err
	}

	s.metrics.leasesRenewed.With("success").Inc()

	s.logger.WithFields(map[string]interface{}{
		"lease_id":    req.LeaseID,
		"renew_count": renewedLease.RenewCount,
	}).Info("Credential lease renewed")

	return &RenewLeaseResponse{
		Lease:     renewedLease,
		RenewedAt: time.Now(),
	}, nil
}

// RevokeLease revokes a credential lease immediately.
func (s *dynamicSecretsHandler) RevokeLease(ctx context.Context, req *RevokeLeaseRequest) (*RevokeLeaseResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("revoke_lease").Observe(time.Since(start).Seconds())
	}()

	if req == nil || req.LeaseID == "" {
		s.metrics.leasesRevoked.With("error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "dynamicSecretsHandler.RevokeLease",
			Err:  stderrors.New("lease ID cannot be empty"),
		}
	}

	if ctx.Err() != nil {
		s.metrics.leasesRevoked.With("error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.RevokeLease",
			Err:  ctx.Err(),
		}
	}

	// Revoke lease
	if err := s.leaseManager.RevokeLease(ctx, req.LeaseID); err != nil {
		s.metrics.leasesRevoked.With("error").Inc()
		return nil, err
	}

	s.metrics.leasesRevoked.With("success").Inc()
	s.metrics.activeCredentials.Dec()

	s.logger.WithField("lease_id", req.LeaseID).Info("Credential lease revoked")

	return &RevokeLeaseResponse{RevokedAt: time.Now()}, nil
}

// ListLeases returns all active leases.
func (s *dynamicSecretsHandler) ListLeases(ctx context.Context, req *ListLeasesRequest) (*ListLeasesResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.opDuration.With("list_leases").Observe(time.Since(start).Seconds())
	}()

	if req == nil {
		req = &ListLeasesRequest{}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "dynamicSecretsHandler.ListLeases",
			Err:  ctx.Err(),
		}
	}

	leases, err := s.leaseManager.ListLeases(ctx, req.Prefix)
	if err != nil {
		return nil, err
	}

	return &ListLeasesResponse{
		Leases: leases,
		Count:  len(leases),
	}, nil
}

// storeRole persists a role to storage.
func (s *dynamicSecretsHandler) storeRole(role *credentials.Role) error {
	data, err := json.Marshal(role)
	if err != nil {
		return api.NewMarshalError(err)
	}

	key := []byte(RolePrefix + role.Name)
	if err := s.store.Set(key, data); err != nil {
		return api.NewStorageOperationError("dynamicSecretsHandler.storeRole", "store role", err)
	}

	return nil
}

// storeCredential persists a credential to storage.
func (s *dynamicSecretsHandler) storeCredential(credential *credentials.Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return api.NewMarshalError(err)
	}

	key := []byte(CredentialPrefix + credential.CredentialID)
	if err := s.store.Set(key, data); err != nil {
		return api.NewStorageOperationError("dynamicSecretsHandler.storeCredential", "store credential", err)
	}

	return nil
}

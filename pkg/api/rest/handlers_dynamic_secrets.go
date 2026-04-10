//go:build rest

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

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Dynamic Secrets Service Route Registration
// =============================================================================

// registerDynamicSecretsRoutes registers all dynamic secrets-related routes.
func registerDynamicSecretsRoutes(s *Server, r chi.Router) {
	r.Route("/secrets/dynamic", func(r chi.Router) {
		// Role management
		r.Post("/roles", s.HandleCreateDynamicSecretRole)
		r.Get("/roles/{name}", s.HandleGetDynamicSecretRole)
		r.Get("/roles", s.HandleListDynamicSecretRoles)
		r.Delete("/roles/{name}", s.HandleDeleteDynamicSecretRole)

		// Credential generation
		r.Post("/credentials/{role}", s.HandleGenerateDynamicCredential)

		// Lease management
		r.Post("/leases/{id}/renew", s.HandleRenewDynamicSecretLease)
		r.Delete("/leases/{id}", s.HandleRevokeDynamicSecretLease)
		r.Get("/leases", s.HandleListDynamicSecretLeases)
	})
}

// =============================================================================
// Request/Response Types for REST API
// =============================================================================

// CreateDynamicSecretRoleRESTRequest represents the JSON request body for creating a role.
type CreateDynamicSecretRoleRESTRequest struct {
	Name          string            `json:"name"`
	GeneratorType string            `json:"generator_type"`
	Config        map[string]string `json:"config,omitempty"`
	DefaultTTL    string            `json:"default_ttl"`
	MaxTTL        string            `json:"max_ttl"`
}

// DynamicSecretRoleRESTResponse represents a role in REST API responses.
type DynamicSecretRoleRESTResponse struct {
	Name          string            `json:"name"`
	GeneratorType string            `json:"generator_type"`
	Config        map[string]string `json:"config,omitempty"`
	DefaultTTL    string            `json:"default_ttl"`
	MaxTTL        string            `json:"max_ttl"`
	CreatedAt     int64             `json:"created_at,omitempty"`
	Timestamp     int64             `json:"timestamp"`
}

// ListDynamicSecretRolesRESTResponse represents the response for listing roles.
type ListDynamicSecretRolesRESTResponse struct {
	Roles     []DynamicSecretRoleRESTResponse `json:"roles"`
	Count     int                             `json:"count"`
	Timestamp int64                           `json:"timestamp"`
}

// DeleteDynamicSecretRoleRESTResponse represents the response for deleting a role.
type DeleteDynamicSecretRoleRESTResponse struct {
	Success   bool  `json:"success"`
	DeletedAt int64 `json:"deleted_at"`
	Timestamp int64 `json:"timestamp"`
}

// GenerateDynamicCredentialRESTRequest represents the JSON request body for generating credentials.
type GenerateDynamicCredentialRESTRequest struct {
	TTL string `json:"ttl,omitempty"`
}

// GenerateDynamicCredentialRESTResponse represents the JSON response for generating credentials.
type GenerateDynamicCredentialRESTResponse struct {
	CredentialID string            `json:"credential_id"`
	Username     string            `json:"username,omitempty"`
	Password     string            `json:"password,omitempty"`
	Secret       map[string]string `json:"secret,omitempty"`
	LeaseID      string            `json:"lease_id"`
	LeaseTTL     string            `json:"lease_ttl"`
	LeaseExpiry  int64             `json:"lease_expiry"`
	Renewable    bool              `json:"renewable"`
	Timestamp    int64             `json:"timestamp"`
}

// RenewDynamicSecretLeaseRESTRequest represents the JSON request body for renewing a lease.
type RenewDynamicSecretLeaseRESTRequest struct {
	Increment string `json:"increment,omitempty"`
}

// RenewDynamicSecretLeaseRESTResponse represents the JSON response for renewing a lease.
type RenewDynamicSecretLeaseRESTResponse struct {
	LeaseID     string `json:"lease_id"`
	LeaseTTL    string `json:"lease_ttl"`
	LeaseExpiry int64  `json:"lease_expiry"`
	RenewCount  int    `json:"renew_count"`
	RenewedAt   int64  `json:"renewed_at"`
	Timestamp   int64  `json:"timestamp"`
}

// RevokeDynamicSecretLeaseRESTResponse represents the JSON response for revoking a lease.
type RevokeDynamicSecretLeaseRESTResponse struct {
	Success   bool  `json:"success"`
	RevokedAt int64 `json:"revoked_at"`
	Timestamp int64 `json:"timestamp"`
}

// LeaseInfoRESTResponse represents a lease in REST API responses.
type LeaseInfoRESTResponse struct {
	LeaseID    string            `json:"lease_id"`
	Path       string            `json:"path"`
	TTL        string            `json:"ttl"`
	Expiry     int64             `json:"expiry"`
	Renewable  bool              `json:"renewable"`
	RenewCount int               `json:"renew_count"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ListDynamicSecretLeasesRESTResponse represents the JSON response for listing leases.
type ListDynamicSecretLeasesRESTResponse struct {
	Leases    []LeaseInfoRESTResponse `json:"leases"`
	Count     int                     `json:"count"`
	Timestamp int64                   `json:"timestamp"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleCreateDynamicSecretRole handles POST /v1/secrets/dynamic/roles - Create a role.
func (s *Server) HandleCreateDynamicSecretRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req CreateDynamicSecretRoleRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Name == "" {
		RespondBadRequest(w, r, "name is required")
		return
	}

	if req.GeneratorType == "" {
		RespondBadRequest(w, r, "generator_type is required")
		return
	}

	if req.DefaultTTL == "" {
		RespondBadRequest(w, r, "default_ttl is required")
		return
	}

	if req.MaxTTL == "" {
		RespondBadRequest(w, r, "max_ttl is required")
		return
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	// Parse durations
	defaultTTL, err := time.ParseDuration(req.DefaultTTL)
	if err != nil {
		RespondBadRequest(w, r, "invalid default_ttl duration format")
		return
	}

	maxTTL, err := time.ParseDuration(req.MaxTTL)
	if err != nil {
		RespondBadRequest(w, r, "invalid max_ttl duration format")
		return
	}

	svcReq := &service.CreateCredentialRoleRequest{
		Name:          req.Name,
		GeneratorType: req.GeneratorType,
		Config:        req.Config,
		DefaultTTL:    defaultTTL,
		MaxTTL:        maxTTL,
	}

	svcResp, err := dynamicSecretsService.CreateRole(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &DynamicSecretRoleRESTResponse{
		Name:          svcResp.Role.Name,
		GeneratorType: svcResp.Role.GeneratorType,
		Config:        svcResp.Role.Config,
		DefaultTTL:    svcResp.Role.DefaultTTL.String(),
		MaxTTL:        svcResp.Role.MaxTTL.String(),
		CreatedAt:     svcResp.CreatedAt.Unix(),
		Timestamp:     now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetDynamicSecretRole handles GET /v1/secrets/dynamic/roles/{name} - Get a role.
func (s *Server) HandleGetDynamicSecretRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	roleName := chi.URLParam(r, "name")
	if roleName == "" {
		RespondBadRequest(w, r, "role name is required")
		return
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	svcReq := &service.GetCredentialRoleRequest{
		Name: roleName,
	}

	svcResp, err := dynamicSecretsService.GetRole(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &DynamicSecretRoleRESTResponse{
		Name:          svcResp.Role.Name,
		GeneratorType: svcResp.Role.GeneratorType,
		Config:        svcResp.Role.Config,
		DefaultTTL:    svcResp.Role.DefaultTTL.String(),
		MaxTTL:        svcResp.Role.MaxTTL.String(),
		Timestamp:     now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListDynamicSecretRoles handles GET /v1/secrets/dynamic/roles - List all roles.
func (s *Server) HandleListDynamicSecretRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	prefix := r.URL.Query().Get("prefix")

	svcReq := &service.ListCredentialRolesRequest{
		Prefix: prefix,
	}

	svcResp, err := dynamicSecretsService.ListRoles(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	roles := make([]DynamicSecretRoleRESTResponse, 0, len(svcResp.Roles))
	for _, role := range svcResp.Roles {
		roles = append(roles, DynamicSecretRoleRESTResponse{
			Name:          role.Name,
			GeneratorType: role.GeneratorType,
			Config:        role.Config,
			DefaultTTL:    role.DefaultTTL.String(),
			MaxTTL:        role.MaxTTL.String(),
			Timestamp:     now.Unix(),
		})
	}

	resp := &ListDynamicSecretRolesRESTResponse{
		Roles:     roles,
		Count:     svcResp.Count,
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteDynamicSecretRole handles DELETE /v1/secrets/dynamic/roles/{name} - Delete a role.
func (s *Server) HandleDeleteDynamicSecretRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	roleName := chi.URLParam(r, "name")
	if roleName == "" {
		RespondBadRequest(w, r, "role name is required")
		return
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	svcReq := &service.DeleteCredentialRoleRequest{
		Name: roleName,
	}

	svcResp, err := dynamicSecretsService.DeleteRole(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &DeleteDynamicSecretRoleRESTResponse{
		Success:   true,
		DeletedAt: svcResp.DeletedAt.Unix(),
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGenerateDynamicCredential handles POST /v1/secrets/dynamic/credentials/{role} - Generate credential.
func (s *Server) HandleGenerateDynamicCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	roleName := chi.URLParam(r, "role")
	if roleName == "" {
		RespondBadRequest(w, r, "role name is required")
		return
	}

	var req GenerateDynamicCredentialRESTRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	var ttl time.Duration
	if req.TTL != "" {
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			RespondBadRequest(w, r, "invalid ttl duration format")
			return
		}
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	svcReq := &service.GenerateCredentialRequest{
		RoleName: roleName,
		TTL:      ttl,
	}

	svcResp, err := dynamicSecretsService.GenerateCredential(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &GenerateDynamicCredentialRESTResponse{
		CredentialID: svcResp.Credential.CredentialID,
		Username:     svcResp.Credential.Data["username"],
		Password:     svcResp.Credential.Data["password"],
		Secret:       svcResp.Credential.Data,
		LeaseID:      svcResp.LeaseID,
		LeaseTTL:     svcResp.Lease.TTL.String(),
		LeaseExpiry:  svcResp.Lease.ExpiresAt.Unix(),
		Renewable:    svcResp.Lease.Renewable,
		Timestamp:    now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleRenewDynamicSecretLease handles POST /v1/secrets/dynamic/leases/{id}/renew - Renew a lease.
func (s *Server) HandleRenewDynamicSecretLease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	leaseID := chi.URLParam(r, "id")
	if leaseID == "" {
		RespondBadRequest(w, r, "lease ID is required")
		return
	}

	var req RenewDynamicSecretLeaseRESTRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	var increment time.Duration
	if req.Increment != "" {
		increment, err = time.ParseDuration(req.Increment)
		if err != nil {
			RespondBadRequest(w, r, "invalid increment duration format")
			return
		}
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	svcReq := &service.RenewLeaseRequest{
		LeaseID:   leaseID,
		Increment: increment,
	}

	svcResp, err := dynamicSecretsService.RenewLease(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &RenewDynamicSecretLeaseRESTResponse{
		LeaseID:     svcResp.Lease.LeaseID,
		LeaseTTL:    svcResp.Lease.TTL.String(),
		LeaseExpiry: svcResp.Lease.ExpiresAt.Unix(),
		RenewCount:  svcResp.Lease.RenewCount,
		RenewedAt:   svcResp.RenewedAt.Unix(),
		Timestamp:   now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRevokeDynamicSecretLease handles DELETE /v1/secrets/dynamic/leases/{id} - Revoke a lease.
func (s *Server) HandleRevokeDynamicSecretLease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	leaseID := chi.URLParam(r, "id")
	if leaseID == "" {
		RespondBadRequest(w, r, "lease ID is required")
		return
	}

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	svcReq := &service.RevokeLeaseRequest{
		LeaseID: leaseID,
	}

	svcResp, err := dynamicSecretsService.RevokeLease(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	resp := &RevokeDynamicSecretLeaseRESTResponse{
		Success:   true,
		RevokedAt: svcResp.RevokedAt.Unix(),
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListDynamicSecretLeases handles GET /v1/secrets/dynamic/leases - List all leases.
func (s *Server) HandleListDynamicSecretLeases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	dynamicSecretsService := s.service.DynamicSecrets()
	if dynamicSecretsService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrDynamicSecretsUnavailable)
		return
	}

	prefix := r.URL.Query().Get("prefix")

	svcReq := &service.ListLeasesRequest{
		Prefix: prefix,
	}

	svcResp, err := dynamicSecretsService.ListLeases(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	now := time.Now()
	leases := make([]LeaseInfoRESTResponse, 0, len(svcResp.Leases))
	for _, lease := range svcResp.Leases {
		leases = append(leases, LeaseInfoRESTResponse{
			LeaseID:    lease.LeaseID,
			Path:       lease.SecretPath,
			TTL:        lease.TTL.String(),
			Expiry:     lease.ExpiresAt.Unix(),
			Renewable:  lease.Renewable,
			RenewCount: lease.RenewCount,
			Metadata:   lease.Metadata,
		})
	}

	resp := &ListDynamicSecretLeasesRESTResponse{
		Leases:    leases,
		Count:     svcResp.Count,
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

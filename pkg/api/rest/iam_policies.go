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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListPolicies handles GET /api/v1/tenants/{tenant_id}/policies - List policies in a tenant.
func (h *IAMHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get policy engine from service
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// List policies
	opts := iam.ListPoliciesOptions{
		Limit:  limit,
		Offset: offset,
	}

	policies, err := engine.ListPolicies(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"policies": policies,
		"limit":    limit,
		"offset":   offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// parsePolicyFromBody parses a policy from the request body. It supports two formats:
//  1. Direct iam.Policy JSON (with apiVersion, kind, metadata, spec)
//  2. SDK CreatePolicyRequest JSON (with name, spec_yaml, etc.) - the spec_yaml
//     field contains raw YAML that is parsed into a full policy document.
func parsePolicyFromBody(body []byte, tenantID string) (*iam.Policy, error) {
	// First, try to parse as a full policy document
	var policy iam.Policy
	if err := json.Unmarshal(body, &policy); err == nil && policy.APIVersion != "" {
		policy.Metadata.Tenant = tenantID
		return &policy, nil
	}

	// Try SDK CreatePolicyRequest format with spec_yaml
	var createReq struct {
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Enabled     bool              `json:"enabled,omitempty"`
		Priority    int               `json:"priority,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
		SpecYAML    []byte            `json:"spec_yaml,omitempty"`
	}

	if err := json.Unmarshal(body, &createReq); err != nil {
		return nil, &PolicyParseError{Reason: "invalid JSON body", Cause: err}
	}

	if len(createReq.SpecYAML) > 0 {
		// Parse the YAML - try full policy format first
		var fullPolicy iam.Policy
		if err := yaml.Unmarshal(createReq.SpecYAML, &fullPolicy); err != nil {
			return nil, &PolicyParseError{Reason: "invalid spec_yaml", Cause: err}
		}

		if fullPolicy.APIVersion != "" && fullPolicy.Kind != "" {
			// Full policy format from YAML
			if createReq.Name != "" {
				fullPolicy.Metadata.Name = createReq.Name
			}
			fullPolicy.Metadata.Tenant = tenantID
			if createReq.Description != "" {
				fullPolicy.Metadata.Description = createReq.Description
			}
			if len(createReq.Labels) > 0 {
				fullPolicy.Metadata.Labels = createReq.Labels
			}
			if createReq.Priority > 0 {
				fullPolicy.Metadata.Priority = createReq.Priority
			}
			return &fullPolicy, nil
		}

		// Spec-only YAML format
		var spec iam.PolicySpec
		if err := yaml.Unmarshal(createReq.SpecYAML, &spec); err != nil {
			return nil, &PolicyParseError{Reason: "invalid spec_yaml", Cause: err}
		}
		return &iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: iam.PolicyMetadata{
				Name:        createReq.Name,
				Tenant:      tenantID,
				Description: createReq.Description,
				Labels:      createReq.Labels,
				Priority:    createReq.Priority,
			},
			Spec: spec,
		}, nil
	}

	// Fallback: try direct iam.Policy unmarshal with relaxed validation
	if err := json.Unmarshal(body, &policy); err != nil {
		return nil, &PolicyParseError{Reason: "invalid policy format", Cause: err}
	}
	policy.Metadata.Tenant = tenantID
	return &policy, nil
}

// CreatePolicy handles POST /api/v1/tenants/{tenant_id}/policies - Create a new policy.
func (h *IAMHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse policy from body (supports both direct Policy and CreatePolicyRequest with spec_yaml)
	policy, err := parsePolicyFromBody(body, tenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}

	// Validate policy
	if err := policy.Validate(); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Create policy
	if err := engine.CreatePolicy(ctx, tenantID, policy); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, policy)
}

// GetPolicy handles GET /api/v1/tenants/{tenant_id}/policies/{policy_name} - Get a specific policy.
func (h *IAMHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	policyName := chi.URLParam(r, "policy_name")
	if policyName == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "policy name is required"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Get policy
	policy, err := engine.GetPolicy(ctx, tenantID, policyName)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, policy)
}

// UpdatePolicy handles PUT /api/v1/tenants/{tenant_id}/policies/{policy_name} - Update a policy.
func (h *IAMHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	policyName := chi.URLParam(r, "policy_name")
	if policyName == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "policy name is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse policy from body (supports both formats)
	policy, err := parsePolicyFromBody(body, tenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}

	// Ensure tenant ID and name match
	policy.Metadata.Tenant = tenantID
	policy.Metadata.Name = policyName

	// Validate policy
	if err := policy.Validate(); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Update policy
	if err := engine.UpdatePolicy(ctx, tenantID, policy); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, policy)
}

// DeletePolicy handles DELETE /api/v1/tenants/{tenant_id}/policies/{policy_name} - Delete a policy.
func (h *IAMHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	policyName := chi.URLParam(r, "policy_name")
	if policyName == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "policy name is required"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Delete policy
	if err := engine.DeletePolicy(ctx, tenantID, policyName); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// ValidatePolicy handles POST /api/v1/tenants/{tenant_id}/policies/validate - Validate a policy document.
func (h *IAMHandler) ValidatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse policy from body (supports both formats)
	policy, err := parsePolicyFromBody(body, tenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Validate policy
	if err := engine.ValidatePolicy(policy); err != nil {
		resp := map[string]interface{}{
			"valid":  false,
			"errors": []string{err.Error()},
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	resp := map[string]interface{}{
		"valid": true,
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// TestPolicy handles POST /api/v1/tenants/{tenant_id}/policies/test - Test policy evaluation.
func (h *IAMHandler) TestPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse check request
	var checkReq iam.CheckRequest
	if err := json.Unmarshal(body, &checkReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Ensure the tenant ID is set on the subject so the policy engine
	// can locate the correct tenant policies for evaluation.
	checkReq.Subject.TenantID = tenantID
	// Perform authorization check
	checkResp, err := engine.Evaluate(ctx, &checkReq)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, checkResp)
}

// CheckAccess handles POST /api/v1/pep/check - PEP single authorization check.
func (h *IAMHandler) CheckAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse check request
	var checkReq iam.CheckRequest
	if err := json.Unmarshal(body, &checkReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Perform authorization check
	checkResp, err := engine.Evaluate(ctx, &checkReq)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, checkResp)
}

// BatchCheckAccess handles POST /api/v1/pep/batch-check - PEP batch authorization check.
func (h *IAMHandler) BatchCheckAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse batch request
	var batchReq struct {
		Checks []iam.CheckRequest `json:"checks"`
	}
	if err := json.Unmarshal(body, &batchReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Perform batch authorization checks
	var results []*iam.CheckResponse
	for _, checkReq := range batchReq.Checks {
		checkResp, err := engine.Evaluate(ctx, &checkReq)
		if err != nil {
			// Include error in result but continue processing
			results = append(results, &iam.CheckResponse{
				Allowed: false,
				Reason:  err.Error(),
			})
			continue
		}
		results = append(results, checkResp)
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"results": results,
	})
}

// SimulatePolicy handles POST /api/v1/tenants/{tenant_id}/policies/simulate - Policy simulation/dry-run.
func (h *IAMHandler) SimulatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse simulation request
	var simReq iam.PolicySimulationRequest
	if err := json.Unmarshal(body, &simReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if simReq.Policy == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "policy is required"))
		return
	}
	if simReq.CheckRequest == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "check_request is required"))
		return
	}

	// Set tenant ID on policy
	simReq.Policy.Metadata.Tenant = tenantID

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Perform policy simulation
	result, err := engine.SimulatePolicy(ctx, &simReq)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, result)
}

// GetEffectivePermissions handles POST /api/v1/tenants/{tenant_id}/policies/effective-permissions - Calculate effective permissions.
func (h *IAMHandler) GetEffectivePermissions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse effective permissions request
	var permReq iam.EffectivePermissionsRequest
	if err := json.Unmarshal(body, &permReq); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if permReq.Subject == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "subject is required"))
		return
	}
	if len(permReq.ResourcePatterns) == 0 {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "resource_patterns is required"))
		return
	}

	// Get policy engine
	policyEngine, ok := h.server.service.(interface {
		GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not available"))
		return
	}

	engine, err := policyEngine.GetPolicyEngine(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if engine == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "policy engine not initialized"))
		return
	}

	// Calculate effective permissions
	result, err := engine.GetEffectivePermissions(ctx, tenantID, &permReq)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, result)
}

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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const policyTenant = "tenant-pol"

// policyMockService exposes a real PolicyEngine to the IAM handlers.
type policyMockService struct {
	*testutil.MockQuicBaaS
	engine *iam.PolicyEngine
}

func (m *policyMockService) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	return m.engine, nil
}

// testPolicySimulator is a minimal iam.PolicySimulator test double: exact
// subject-ID match, exact action match, resource prefix-wildcard match
// ("/test/*" matches "/test/x"). Mirrors pkg/iam's own simplePolicySimulator
// test double (unexported there); this package cannot import it directly.
type testPolicySimulator struct{}

func (testPolicySimulator) Simulate(_ context.Context, policy *iam.Policy, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	subjectMatches := len(policy.Spec.Subjects.Users) == 0
	for _, u := range policy.Spec.Subjects.Users {
		if u == req.Subject.ID {
			subjectMatches = true
		}
	}
	if !subjectMatches {
		return &iam.CheckResponse{Allowed: false, Reason: "subject does not match policy"}, nil
	}
	for _, rule := range policy.Spec.Rules {
		actionMatches := false
		for _, a := range rule.Actions {
			if a == "*" || a == req.Action {
				actionMatches = true
			}
		}
		if !actionMatches {
			continue
		}
		for _, pattern := range rule.Resources {
			prefix := pattern
			if len(prefix) > 0 && prefix[len(prefix)-1] == '*' {
				prefix = prefix[:len(prefix)-1]
			}
			if len(req.Resource) < len(prefix) || req.Resource[:len(prefix)] != prefix {
				continue
			}
			allowed := rule.Effect != iam.EffectDeny
			return &iam.CheckResponse{
				Allowed: allowed, MatchedPolicy: policy.Metadata.Name, MatchedRule: rule.Name,
			}, nil
		}
	}
	return &iam.CheckResponse{Allowed: false, Reason: "no matching rule"}, nil
}

func setupPolicyHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:   iam.NewMemoryStorage(),
		Simulator: testPolicySimulator{},
	})

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &policyMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), engine: engine}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, func() { server.Close() }
}

func validPolicyDoc(name string) *iam.Policy {
	return &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:     name,
			Tenant:   policyTenant,
			Priority: 100,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Authenticated: true},
			Rules: []iam.Rule{{
				Name:      "allow-read",
				Effect:    iam.EffectAllow,
				Priority:  100,
				Actions:   []string{"read"},
				Resources: []string{"/test/*"},
			}},
		},
	}
}

func policyRequest(t *testing.T, method, target string, body interface{}, params map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, target, &buf)
	return withChiParams(req, params)
}

// Full CRUD lifecycle against a real policy engine: create, get, list, update,
// delete.
func TestPolicyLifecycle(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	tenantParams := map[string]string{"tenant_id": policyTenant}

	// Create
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost,
		"/api/v1/tenants/"+policyTenant+"/policies", validPolicyDoc("p1"), tenantParams))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	// Get
	w = httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// List
	w = httptest.NewRecorder()
	handler.ListPolicies(w, policyRequest(t, http.MethodGet,
		"/api/v1/tenants/"+policyTenant+"/policies?limit=10&offset=0", nil, tenantParams))
	require.Equal(t, http.StatusOK, w.Code)
	var listResp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&listResp))
	assert.Contains(t, listResp, "policies")

	// Update
	updated := validPolicyDoc("p1")
	updated.Metadata.Description = "updated"
	w = httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", updated,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Delete
	w = httptest.NewRecorder()
	handler.DeletePolicy(w, policyRequest(t, http.MethodDelete, "/x", nil,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCreatePolicy_InvalidDocument(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	// Missing apiVersion/kind -> Validate fails -> 400.
	bad := map[string]interface{}{"metadata": map[string]string{"name": "x"}}
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", bad,
		map[string]string{"tenant_id": policyTenant}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestValidatePolicy_ValidAndInvalid(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()
	params := map[string]string{"tenant_id": policyTenant}

	// Valid
	w := httptest.NewRecorder()
	handler.ValidatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("v1"), params))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["valid"])

	// Invalid (missing rules etc.)
	invalid := &iam.Policy{APIVersion: "iam.quicbaas.io/v1", Kind: "Policy",
		Metadata: iam.PolicyMetadata{Name: "v2", Tenant: policyTenant}}
	w = httptest.NewRecorder()
	handler.ValidatePolicy(w, policyRequest(t, http.MethodPost, "/x", invalid, params))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["valid"])
}

func TestTestPolicy_And_CheckAccess(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	check := iam.CheckRequest{
		Subject:  iam.Subject{ID: "u1", TenantID: policyTenant, Authenticated: true},
		Action:   "read",
		Resource: "/test/object",
	}

	// TestPolicy (tenant-scoped)
	w := httptest.NewRecorder()
	handler.TestPolicy(w, policyRequest(t, http.MethodPost, "/x", check,
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// CheckAccess (PEP endpoint, no tenant param)
	w = httptest.NewRecorder()
	handler.CheckAccess(w, policyRequest(t, http.MethodPost, "/api/v1/pep/check", check, nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestBatchCheckAccess_Success(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	body := map[string]interface{}{
		"checks": []iam.CheckRequest{
			{Subject: iam.Subject{ID: "u1", TenantID: policyTenant, Authenticated: true}, Action: "read", Resource: "/a"},
			{Subject: iam.Subject{ID: "u2", TenantID: policyTenant, Authenticated: true}, Action: "write", Resource: "/b"},
		},
	}
	w := httptest.NewRecorder()
	handler.BatchCheckAccess(w, policyRequest(t, http.MethodPost, "/api/v1/pep/batch-check", body, nil))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp, "results")
}

func TestSimulatePolicy_Success_And_Validation(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()
	params := map[string]string{"tenant_id": policyTenant}

	// Missing policy -> 400
	w := httptest.NewRecorder()
	handler.SimulatePolicy(w, policyRequest(t, http.MethodPost, "/x",
		map[string]interface{}{"check_request": map[string]string{}}, params))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing check_request -> 400
	w = httptest.NewRecorder()
	handler.SimulatePolicy(w, policyRequest(t, http.MethodPost, "/x",
		iam.PolicySimulationRequest{Policy: validPolicyDoc("s1")}, params))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Full simulation -> 200
	sim := iam.PolicySimulationRequest{
		Policy: validPolicyDoc("s1"),
		CheckRequest: &iam.CheckRequest{
			Subject:  iam.Subject{ID: "u1", TenantID: policyTenant, Authenticated: true},
			Action:   "read",
			Resource: "/test/x",
		},
	}
	w = httptest.NewRecorder()
	handler.SimulatePolicy(w, policyRequest(t, http.MethodPost, "/x", sim, params))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestGetEffectivePermissions_Validation(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()
	params := map[string]string{"tenant_id": policyTenant}

	// Missing subject -> 400
	w := httptest.NewRecorder()
	handler.GetEffectivePermissions(w, policyRequest(t, http.MethodPost, "/x",
		map[string]interface{}{"resource_patterns": []string{"/a"}}, params))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing resource_patterns -> 400
	w = httptest.NewRecorder()
	handler.GetEffectivePermissions(w, policyRequest(t, http.MethodPost, "/x",
		iam.EffectivePermissionsRequest{Subject: &iam.Subject{ID: "u1", TenantID: policyTenant}}, params))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// parsePolicyFromBody handles the direct, spec_yaml and malformed inputs.
func TestParsePolicyFromBody(t *testing.T) {
	// Direct policy JSON.
	direct, err := json.Marshal(validPolicyDoc("d1"))
	require.NoError(t, err)
	p, err := parsePolicyFromBody(direct, policyTenant)
	require.NoError(t, err)
	assert.Equal(t, policyTenant, p.Metadata.Tenant)

	// SDK CreatePolicyRequest with spec_yaml (full policy document).
	specYAML := []byte("apiVersion: iam.quicbaas.io/v1\nkind: Policy\nspec:\n  subjects:\n    authenticated: true\n  rules:\n    - name: r\n      effect: allow\n      actions: [read]\n      resources: ['/x']\n")
	req := map[string]interface{}{"name": "from-yaml", "spec_yaml": specYAML}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	p, err = parsePolicyFromBody(body, policyTenant)
	require.NoError(t, err)
	assert.Equal(t, "from-yaml", p.Metadata.Name)
	assert.Equal(t, policyTenant, p.Metadata.Tenant)

	// Invalid JSON.
	_, err = parsePolicyFromBody([]byte("{not json"), policyTenant)
	assert.Error(t, err)
}

// parsePolicyFromBody handles the spec-only YAML and the relaxed-fallback paths.
func TestParsePolicyFromBody_SpecOnlyAndFallback(t *testing.T) {
	// spec_yaml that is a bare spec (no apiVersion/kind) -> spec-only branch.
	specOnly := []byte("subjects:\n  authenticated: true\nrules:\n  - name: r\n    effect: allow\n    actions: [read]\n    resources: ['/x']\n")
	body, err := json.Marshal(map[string]interface{}{"name": "spec-only", "spec_yaml": specOnly})
	require.NoError(t, err)
	p, err := parsePolicyFromBody(body, policyTenant)
	require.NoError(t, err)
	assert.Equal(t, "spec-only", p.Metadata.Name)
	assert.Equal(t, "iam.quicbaas.io/v1", p.APIVersion)

	// invalid spec_yaml -> error.
	badBody, err := json.Marshal(map[string]interface{}{"name": "bad", "spec_yaml": []byte("{not: [valid")})
	require.NoError(t, err)
	_, err = parsePolicyFromBody(badBody, policyTenant)
	assert.Error(t, err)

	// No apiVersion and no spec_yaml -> relaxed fallback unmarshal succeeds with tenant set.
	fallback := []byte(`{"metadata":{"name":"fb"}}`)
	p, err = parsePolicyFromBody(fallback, policyTenant)
	require.NoError(t, err)
	assert.Equal(t, policyTenant, p.Metadata.Tenant)
}

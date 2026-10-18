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
	return setupPolicyHandlerWithEngine(t, iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:   iam.NewMemoryStorage(),
		Simulator: testPolicySimulator{},
	}))
}

// setupPolicyHandlerWithEngine builds an IAMHandler whose service serves the
// given policy engine.
func setupPolicyHandlerWithEngine(t *testing.T, engine *iam.PolicyEngine) (*IAMHandler, func()) {
	t.Helper()
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

// getPolicyBody returns the body of a GET for the named policy.
func getPolicyBody(t *testing.T, handler *IAMHandler, name string) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil,
		map[string]string{"tenant_id": policyTenant, "policy_name": name}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	return w.Body.String()
}

func TestCreatePolicy_RespondsWithStoredPolicy(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost,
		"/api/v1/tenants/"+policyTenant+"/policies", validPolicyDoc("p1"),
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	// The store stamps Created and Updated; an echo of the request would not
	// carry them.
	assert.JSONEq(t, getPolicyBody(t, handler, "p1"), w.Body.String())
}

func TestCreatePolicy_DuplicateLeavesStoredPolicy(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	tenantParams := map[string]string{"tenant_id": policyTenant}
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"), tenantParams))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	before := getPolicyBody(t, handler, "p1")

	duplicate := validPolicyDoc("p1")
	duplicate.Metadata.Description = "duplicate"
	w = httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", duplicate, tenantParams))
	assert.NotEqual(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	assert.JSONEq(t, before, getPolicyBody(t, handler, "p1"))
}

func TestUpdatePolicy_ResponseMatchesFollowingGet(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"),
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	updated := validPolicyDoc("p1")
	updated.Metadata.Description = "updated"
	w = httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", updated,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var got iam.Policy
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "updated", got.Metadata.Description)
	assert.False(t, got.Metadata.Updated.IsZero(), "response must carry the stored Updated stamp")
	assert.JSONEq(t, getPolicyBody(t, handler, "p1"), w.Body.String())
}

// TestUpdatePolicy_DocumentName pins that Update refuses a body whose
// document names another policy, for both body formats, and stores nothing;
// a matching name is accepted.
func TestUpdatePolicy_DocumentName(t *testing.T) {
	sdkBody := func(docName string) map[string]interface{} {
		return map[string]interface{}{"name": "p1", "spec_yaml": []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: ` + docName + `
  description: updated
spec:
  subjects:
    authenticated: true
  rules:
    - name: r1
      effect: allow
      actions: [read]
      resources: ["docs/*"]
`)}
	}
	fullDoc := func(docName string) interface{} {
		doc := validPolicyDoc(docName)
		doc.Metadata.Description = "updated"
		return doc
	}
	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{"full document mismatch", fullDoc("other"), http.StatusBadRequest},
		{"sdk spec_yaml mismatch", sdkBody("other"), http.StatusBadRequest},
		{"full document match", fullDoc("p1"), http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cleanup := setupPolicyHandler(t)
			defer cleanup()
			w := httptest.NewRecorder()
			handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"),
				map[string]string{"tenant_id": policyTenant}))
			require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
			before := getPolicyBody(t, handler, "p1")

			w = httptest.NewRecorder()
			handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", tt.body,
				map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
			require.Equal(t, tt.wantCode, w.Code, "body: %s", w.Body.String())

			if tt.wantCode == http.StatusOK {
				var got iam.Policy
				require.NoError(t, json.Unmarshal([]byte(getPolicyBody(t, handler, "p1")), &got))
				assert.Equal(t, "updated", got.Metadata.Description)
				return
			}
			var resp struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			want := (&iam.PolicyNameMismatchError{Requested: "p1", Document: "other"}).Error()
			assert.Contains(t, resp.Error, want)
			assert.JSONEq(t, before, getPolicyBody(t, handler, "p1"), "a refused update must not change the stored policy")
		})
	}
}

// TestCreatePolicy_DocumentName pins that an SDK-format Create whose
// spec_yaml document names another policy is refused and stores nothing; a
// matching document and a bare spec are stored under the body's name.
func TestCreatePolicy_DocumentName(t *testing.T) {
	fullSpec := func(docName string) []byte {
		return []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: ` + docName + `
spec:
  subjects:
    authenticated: true
  rules:
    - name: r1
      effect: allow
      actions: [read]
      resources: ["docs/*"]
`)
	}
	bareSpec := []byte(`subjects:
  authenticated: true
rules:
  - name: r1
    effect: allow
    actions: [read]
    resources: ["docs/*"]
`)
	tests := []struct {
		name     string
		specYAML []byte
		wantCode int
	}{
		{"document names another policy", fullSpec("other"), http.StatusBadRequest},
		{"document names this policy", fullSpec("p1"), http.StatusCreated},
		{"bare spec takes the body name", bareSpec, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cleanup := setupPolicyHandler(t)
			defer cleanup()
			w := httptest.NewRecorder()
			handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x",
				map[string]interface{}{"name": "p1", "spec_yaml": tt.specYAML},
				map[string]string{"tenant_id": policyTenant}))
			require.Equal(t, tt.wantCode, w.Code, "body: %s", w.Body.String())

			if tt.wantCode == http.StatusCreated {
				var got iam.Policy
				require.NoError(t, json.Unmarshal([]byte(getPolicyBody(t, handler, "p1")), &got))
				assert.Equal(t, "p1", got.Metadata.Name)
				return
			}
			var resp struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			want := (&iam.PolicyNameMismatchError{Requested: "p1", Document: "other"}).Error()
			assert.Contains(t, resp.Error, want)
			for _, name := range []string{"p1", "other"} {
				w = httptest.NewRecorder()
				handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil,
					map[string]string{"tenant_id": policyTenant, "policy_name": name}))
				assert.Equal(t, http.StatusNotFound, w.Code, "a refused create must store nothing under %q", name)
			}
		})
	}
}

func TestUpdatePolicy_UnknownPolicyIsNotFound(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", validPolicyDoc("missing"),
		map[string]string{"tenant_id": policyTenant, "policy_name": "missing"}))
	assert.Equal(t, http.StatusNotFound, w.Code, "body: %s", w.Body.String())
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

// TestValidatePolicy_Name checks the document against the optional ?name=
// query parameter: a mismatch is reported invalid, a match or no name is not.
func TestValidatePolicy_Name(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()
	params := map[string]string{"tenant_id": policyTenant}

	tests := []struct {
		name      string
		target    string
		wantValid bool
	}{
		{name: "match", target: "/x?name=v1", wantValid: true},
		{name: "mismatch", target: "/x?name=other", wantValid: false},
		{name: "empty", target: "/x?name=", wantValid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ValidatePolicy(w, policyRequest(t, http.MethodPost, tt.target, validPolicyDoc("v1"), params))
			require.Equal(t, http.StatusOK, w.Code)
			var resp map[string]interface{}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, tt.wantValid, resp["valid"])
			if !tt.wantValid {
				assert.Contains(t, resp["errors"], iam.CheckPolicyName(validPolicyDoc("v1"), "other").Error())
			}
		})
	}
}

// TestValidatePolicy_SDKBodyDocumentName pins that for an SDK-format body the
// spec_yaml document's own metadata.name is checked against ?name= before
// the body's name overrides it.
func TestValidatePolicy_SDKBodyDocumentName(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()
	params := map[string]string{"tenant_id": policyTenant}

	specYAML := []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: other
spec:
  subjects:
    authenticated: true
  rules:
    - name: r1
      effect: allow
      actions: [read]
      resources: ["docs/*"]
`)
	body := map[string]interface{}{"name": "requested", "spec_yaml": specYAML}

	w := httptest.NewRecorder()
	handler.ValidatePolicy(w, policyRequest(t, http.MethodPost, "/x?name=requested", body, params))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["valid"])
	want := iam.CheckPolicyName(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "other"}}, "requested").Error()
	assert.Contains(t, resp["errors"], want)
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

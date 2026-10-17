//go:build rest

package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// policyDocWithVersion returns validPolicyDoc(name) as a JSON object with a
// "version" key set to version.
func policyDocWithVersion(t *testing.T, name string, version interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(validPolicyDoc(name))
	require.NoError(t, err)
	var doc map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &doc))
	doc["version"] = version
	return doc
}

func TestParsePolicyFromBody_RefusesVersion(t *testing.T) {
	for name, body := range map[string]string{
		"top level":     `{"name":"p1","version":3}`,
		"inside policy": `{"name":"p1","policy":{"version":3}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePolicyFromBody([]byte(body), policyTenant)
			var versionErr *iam.PolicyVersionUnsupportedError
			require.True(t, errors.As(err, &versionErr), "got %v", err)
			assert.Equal(t, "3", versionErr.Version)
		})
	}
}

func TestCreatePolicy_NonZeroVersionIsBadRequest(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x",
		policyDocWithVersion(t, "p1", 2), map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "policy version is not supported")

	// Nothing was stored.
	w = httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	assert.Equal(t, http.StatusNotFound, w.Code, "body: %s", w.Body.String())
}

func TestUpdatePolicy_NonZeroVersionIsBadRequest(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"),
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	before := getPolicyBody(t, handler, "p1")

	doc := policyDocWithVersion(t, "p1", 5)
	doc["metadata"].(map[string]interface{})["description"] = "updated"
	w = httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", doc,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "policy version is not supported")
	assert.JSONEq(t, before, getPolicyBody(t, handler, "p1"))
}

func TestPolicy_ZeroVersionCreatesAndUpdates(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x",
		policyDocWithVersion(t, "p1", 0), map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	doc := policyDocWithVersion(t, "p1", 0)
	doc["metadata"].(map[string]interface{})["description"] = "updated"
	w = httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", doc,
		map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, getPolicyBody(t, handler, "p1"), `"description":"updated"`)
}

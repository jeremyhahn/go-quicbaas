//go:build rest

package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// unrelatedServiceError is an error handleServiceError has no mapping for.
type unrelatedServiceError struct{}

func (unrelatedServiceError) Error() string { return "unrelated failure" }

func errorBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Error
}

// A second create of the same policy name is the caller's conflict, not a
// server fault (go-quicbaas#1417).
func TestCreatePolicy_DuplicateAnswersConflict(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	params := map[string]string{"tenant_id": policyTenant}
	first := httptest.NewRecorder()
	handler.CreatePolicy(first, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("dup"), params))
	require.Equal(t, http.StatusCreated, first.Code)

	second := httptest.NewRecorder()
	handler.CreatePolicy(second, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("dup"), params))

	assert.Equal(t, http.StatusConflict, second.Code)
	assert.Contains(t, errorBody(t, second), "policy 'dup' already exists")
}

func TestHandleServiceError_WrappedPolicyExistsIsConflict(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	err := fmt.Errorf("batch import: %w", &iam.PolicyExistsError{TenantID: "t1", Name: "p1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/policies", nil)
	server.handleServiceError(w, r, err)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, errorBody(t, w), "policy 'p1' already exists in tenant 't1'")
}

func TestHandleServiceError_UnrelatedErrorStaysInternal(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/policies", nil)
	server.handleServiceError(w, r, unrelatedServiceError{})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

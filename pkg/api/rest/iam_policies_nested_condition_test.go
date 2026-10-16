//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	nestedConditionPolicyBody = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","metadata":{"name":"p1"},
		"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"],
		"when":[{"not":{"expression":"subject.dept == 'eng'"}}]}]}}`
	flatConditionPolicyBody = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","metadata":{"name":"p1"},
		"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"],
		"when":[{"expression":"subject.dept == 'eng'"}]}]}}`
)

func setupNestedConditionHandler(t *testing.T) (*IAMHandler, *iam.MemoryStorage, func()) {
	t.Helper()
	storage := iam.NewMemoryStorage()
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: storage})
	h, cleanup := setupDeepHandler(t, &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engine: engine})
	return h, storage, cleanup
}

func storedPolicy(storage *iam.MemoryStorage) *iam.Policy {
	p, err := storage.GetPolicy(context.Background(), "t1", "p1")
	if err != nil {
		return nil
	}
	return p
}

func TestCreatePolicy_NestedConditionRefused(t *testing.T) {
	h, storage, cleanup := setupNestedConditionHandler(t)
	defer cleanup()

	r := withChiParams(httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(nestedConditionPolicyBody)),
		map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreatePolicy(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "nested conditions are not supported")
	assert.Nil(t, storedPolicy(storage), "refused policy was stored")
}

func TestCreatePolicy_FlatConditionStored(t *testing.T) {
	h, storage, cleanup := setupNestedConditionHandler(t)
	defer cleanup()

	r := withChiParams(httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(flatConditionPolicyBody)),
		map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreatePolicy(w, r)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	stored := storedPolicy(storage)
	require.NotNil(t, stored)
	assert.Equal(t, "subject.dept == 'eng'", stored.Spec.Rules[0].When[0].Expression)
}

func TestUpdatePolicy_NestedConditionRefused(t *testing.T) {
	h, storage, cleanup := setupNestedConditionHandler(t)
	defer cleanup()

	create := withChiParams(httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(flatConditionPolicyBody)),
		map[string]string{"tenant_id": "t1"})
	created := httptest.NewRecorder()
	h.CreatePolicy(created, create)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	r := withChiParams(httptest.NewRequest(http.MethodPut, "/p/p1", strings.NewReader(nestedConditionPolicyBody)),
		map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.UpdatePolicy(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "nested conditions are not supported")
	stored := storedPolicy(storage)
	require.NotNil(t, stored)
	when := stored.Spec.Rules[0].When
	assert.Equal(t, "subject.dept == 'eng'", when[0].Expression)
	assert.Nil(t, when[0].Not, "refused update changed the stored policy")
}

func TestUpdatePolicy_FlatConditionStored(t *testing.T) {
	h, storage, cleanup := setupNestedConditionHandler(t)
	defer cleanup()

	create := withChiParams(httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(flatConditionPolicyBody)),
		map[string]string{"tenant_id": "t1"})
	created := httptest.NewRecorder()
	h.CreatePolicy(created, create)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	updated := strings.Replace(flatConditionPolicyBody, "'eng'", "'ops'", 1)
	r := withChiParams(httptest.NewRequest(http.MethodPut, "/p/p1", strings.NewReader(updated)),
		map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	h.UpdatePolicy(w, r)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored := storedPolicy(storage)
	require.NotNil(t, stored)
	assert.Equal(t, "subject.dept == 'ops'", stored.Spec.Rules[0].When[0].Expression)
}

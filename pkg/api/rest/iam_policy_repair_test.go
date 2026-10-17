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
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// flaggedPolicyRecord is a stored policy record with an unknown key: the
// strict load flags it and never applies it.
const flaggedPolicyRecord = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
	`"metadata":{"name":"p1","tenant":"tenant-pol"},` +
	`"spec":{"subjects":{"authenticated":true},"rules":[{"name":"allow-read","effect":"allow",` +
	`"actions":["read"],"resources":["/test/*"],"bogus":"secret-value"}]}}`

// setupFlaggedPolicyHandler serves the IAM handlers over a backend store
// that holds one flagged policy record, p1, in policyTenant.
func setupFlaggedPolicyHandler(t *testing.T) (*IAMHandler, *iam.PolicyEngine) {
	t.Helper()
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	key := iam.KeyPrefixTenant + policyTenant + iam.KeyPrefixPolicy + "p1"
	require.NoError(t, kv.Put(ctx, key, []byte(flaggedPolicyRecord)))
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewBackendStorage(kv)})

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &policyMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), engine: engine}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	t.Cleanup(func() { server.Close() })

	return NewIAMHandler(IAMHandlerDependencies{Server: server}), engine
}

var flaggedPolicyParams = map[string]string{"tenant_id": policyTenant, "policy_name": "p1"}

func TestListPolicies_FlaggedRecordCarriesLoadError(t *testing.T) {
	handler, _ := setupFlaggedPolicyHandler(t)

	w := httptest.NewRecorder()
	handler.ListPolicies(w, policyRequest(t, http.MethodGet, "/x", nil,
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Policies []struct {
			Metadata  iam.PolicyMetadata       `json:"metadata"`
			LoadError *iam.PolicyLoadErrorView `json:"load_error"`
		} `json:"policies"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Policies, 1)
	assert.Equal(t, "p1", resp.Policies[0].Metadata.Name)
	require.NotNil(t, resp.Policies[0].LoadError)
	assert.NotEmpty(t, resp.Policies[0].LoadError.Reason)
	assert.Equal(t, []string{"spec.rules[0].bogus"}, resp.Policies[0].LoadError.UnknownKeys)
	assert.NotContains(t, w.Body.String(), "secret-value", "load_error must never carry values")
}

func TestListPolicies_HealthyRecordOmitsLoadError(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"),
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	w = httptest.NewRecorder()
	handler.ListPolicies(w, policyRequest(t, http.MethodGet, "/x", nil,
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "load_error")
	assert.Contains(t, w.Body.String(), `"allow-read"`)
}

func TestUpdatePolicy_RepairsFlaggedRecord(t *testing.T) {
	handler, engine := setupFlaggedPolicyHandler(t)

	w := httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", validPolicyDoc("p1"), flaggedPolicyParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	w = httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil, flaggedPolicyParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "load_error")

	repaired, err := engine.GetPolicy(context.Background(), policyTenant, "p1")
	require.NoError(t, err, "a strict Get must return the repaired record whole")
	assert.Nil(t, repaired.LoadError)
	assert.Equal(t, validPolicyDoc("p1").Spec.Rules, repaired.Spec.Rules)
}

func TestDeletePolicy_RemovesFlaggedRecord(t *testing.T) {
	handler, engine := setupFlaggedPolicyHandler(t)

	w := httptest.NewRecorder()
	handler.DeletePolicy(w, policyRequest(t, http.MethodDelete, "/x", nil, flaggedPolicyParams))
	require.Less(t, w.Code, http.StatusMultipleChoices, "body: %s", w.Body.String())

	policies, err := engine.ListPolicies(context.Background(), policyTenant, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, policies)
}

func TestUpdatePolicy_MalformedDocumentLeavesFlaggedRecord(t *testing.T) {
	handler, engine := setupFlaggedPolicyHandler(t)

	malformed := validPolicyDoc("p1")
	malformed.Spec.Rules = nil
	w := httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", malformed, flaggedPolicyParams))
	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())

	flagged, err := engine.GetPolicy(context.Background(), policyTenant, "p1")
	require.NoError(t, err)
	require.NotNil(t, flagged.LoadError, "the flagged record must be left as it was")
	assert.Equal(t, []string{"spec.rules[0].bogus"}, flagged.LoadError.UnknownKeys)
}

func TestGetPolicy_FlaggedRecordCarriesLoadError(t *testing.T) {
	handler, _ := setupFlaggedPolicyHandler(t)

	w := httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil, flaggedPolicyParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Metadata  iam.PolicyMetadata       `json:"metadata"`
		Spec      iam.PolicySpec           `json:"spec"`
		LoadError *iam.PolicyLoadErrorView `json:"load_error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "p1", resp.Metadata.Name)
	assert.Empty(t, resp.Spec.Rules, "a flagged record applies no rules")
	require.NotNil(t, resp.LoadError)
	assert.NotEmpty(t, resp.LoadError.Reason)
	assert.Equal(t, []string{"spec.rules[0].bogus"}, resp.LoadError.UnknownKeys)
	assert.NotContains(t, w.Body.String(), "secret-value", "load_error must never carry values")
}

func TestGetPolicy_HealthyRecordOmitsLoadError(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.CreatePolicy(w, policyRequest(t, http.MethodPost, "/x", validPolicyDoc("p1"),
		map[string]string{"tenant_id": policyTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	w = httptest.NewRecorder()
	handler.GetPolicy(w, policyRequest(t, http.MethodGet, "/x", nil, flaggedPolicyParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "load_error")
	assert.Contains(t, w.Body.String(), `"allow-read"`)
}

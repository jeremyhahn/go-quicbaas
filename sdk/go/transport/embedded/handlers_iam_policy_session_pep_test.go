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

package embedded

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newIAMVerbsTransport(t *testing.T, svc service.QuicBaaS) *Transport {
	t.Helper()
	tr, err := New(svc)
	require.NoError(t, err)
	return tr
}

func tenantCtx(tenant string) context.Context {
	return transport.ContextWithTenant(context.Background(), tenant)
}

func TestEmbeddedPolicySimulate_Success(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPolicyClient(tr)

	resp, err := client.Simulate(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.SimulateRequest{Subject: iamsdk.Subject{ID: "u1"}, Action: "read", Resource: "doc/1"})
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.Equal(t, iamsdk.EffectAllow, resp.Effect)
}

func TestEmbeddedPolicySimulate_CrossTenantRefused(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPolicyClient(tr)

	_, err := client.Simulate(tenantCtx("other"), policyConditionsTenant,
		&iamsdk.SimulateRequest{Subject: iamsdk.Subject{ID: "u1"}, Action: "read", Resource: "doc/1"})
	require.Error(t, err)
}

func TestEmbeddedPolicySimulate_NotConfigured(t *testing.T) {
	tr := newIAMVerbsTransport(t, &mockQuicBaaS{})
	client := iamsdk.NewPolicyClient(tr)

	_, err := client.Simulate(tenantCtx(policyConditionsTenant), policyConditionsTenant,
		&iamsdk.SimulateRequest{Subject: iamsdk.Subject{ID: "u1"}, Action: "read", Resource: "doc/1"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "IAM.Policy")
}

func TestHandleIAMPolicySimulate_TypedNilRequest(t *testing.T) {
	svc := newPolicyVerbsService(t)
	envelope := &struct {
		TenantID string
		Request  *iamsdk.SimulateRequest
	}{TenantID: policyConditionsTenant}

	_, err := handleIAMPolicySimulate(tenantCtx(policyConditionsTenant), svc, envelope)
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
}

// The client's Check reaches the policy engine through the embedded
// transport: the handler unwraps the request the client sends and answers
// with the engine's decision.
func TestEmbeddedPEPCheck_Success(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPEPClient(tr)

	resp, err := client.Check(tenantCtx(policyConditionsTenant), &iamsdk.CheckRequest{
		Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1",
	})
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.Equal(t, iamsdk.EffectAllow, resp.Effect)
}

func TestEmbeddedPEPCheck_CrossTenantRefused(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPEPClient(tr)

	_, err := client.Check(tenantCtx(policyConditionsTenant), &iamsdk.CheckRequest{
		Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1", TenantID: "other",
	})
	require.Error(t, err)
}

func TestEmbeddedPEPCheck_NotConfigured(t *testing.T) {
	tr := newIAMVerbsTransport(t, &mockQuicBaaS{})
	client := iamsdk.NewPEPClient(tr)

	_, err := client.Check(tenantCtx(policyConditionsTenant), &iamsdk.CheckRequest{
		Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "IAM.Policy")
}

func TestHandleIAMPEPCheck_TypedNilRequest(t *testing.T) {
	svc := newPolicyVerbsService(t)
	envelope := &struct{ Request *iamsdk.CheckRequest }{}

	_, err := handleIAMPEPCheck(tenantCtx(policyConditionsTenant), svc, envelope)
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
}

// The denying authorizer's refusal reaches the client as a denial, not an
// error, on Check as on CheckBatch.
func TestEmbeddedPEPCheck_Denied(t *testing.T) {
	authz := &countingAuthorizer{deny: "doc/2"}
	svc := newAuthorizedAdminTestService(t)
	svc.policy = iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: authz,
	})
	client := iamsdk.NewPEPClient(newIAMVerbsTransport(t, svc))

	resp, err := client.Check(tenantCtx(policyConditionsTenant), &iamsdk.CheckRequest{
		Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/2",
	})
	require.NoError(t, err)
	assert.False(t, resp.Allowed)
	assert.Equal(t, iamsdk.EffectDeny, resp.Effect)
}

func TestEmbeddedPEPCheckBatch_Success(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPEPClient(tr)

	resp, err := client.CheckBatch(tenantCtx(policyConditionsTenant), &iamsdk.CheckBatchRequest{
		Requests: []iamsdk.CheckRequest{
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1"},
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/2"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.True(t, resp.AllAllowed)
	assert.Equal(t, 2, resp.AllowedCount)
	assert.Equal(t, iamsdk.EffectAllow, resp.Results[1].Effect)
}

func TestEmbeddedPEPCheckBatch_CrossTenantRefused(t *testing.T) {
	tr := newIAMVerbsTransport(t, newPolicyVerbsService(t))
	client := iamsdk.NewPEPClient(tr)

	_, err := client.CheckBatch(tenantCtx(policyConditionsTenant), &iamsdk.CheckBatchRequest{
		Requests: []iamsdk.CheckRequest{
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1"},
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/2", TenantID: "other"},
		},
	})
	require.Error(t, err)
}

func TestEmbeddedPEPCheckBatch_NotConfigured(t *testing.T) {
	tr := newIAMVerbsTransport(t, &mockQuicBaaS{})
	client := iamsdk.NewPEPClient(tr)

	_, err := client.CheckBatch(tenantCtx(policyConditionsTenant), &iamsdk.CheckBatchRequest{
		Requests: []iamsdk.CheckRequest{
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1"},
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "IAM.Policy")
}

func TestHandleIAMPEPCheckBatch_TypedNilRequest(t *testing.T) {
	svc := newPolicyVerbsService(t)
	envelope := &struct{ Request *iamsdk.CheckBatchRequest }{}

	_, err := handleIAMPEPCheckBatch(tenantCtx(policyConditionsTenant), svc, envelope)
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
}

// resourceDeniedError is the refusal countingAuthorizer returns for its
// denied resource.
type resourceDeniedError struct {
	Resource string
}

func (e *resourceDeniedError) Error() string {
	return "access denied to " + e.Resource
}

// countingAuthorizer counts every Authorize call and refuses one resource.
type countingAuthorizer struct {
	deny  string
	calls atomic.Int32
}

func (a *countingAuthorizer) Authorize(_ context.Context, _ *iam.Subject, resource, _ string) error {
	a.calls.Add(1)
	if resource == a.deny {
		return &resourceDeniedError{Resource: resource}
	}
	return nil
}

func checkBatchAllowDenyAllow(t *testing.T, stopOnDeny bool) (*iamsdk.CheckBatchResponse, *countingAuthorizer) {
	t.Helper()
	authz := &countingAuthorizer{deny: "doc/2"}
	svc := newAuthorizedAdminTestService(t)
	svc.policy = iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewMemoryStorage(),
		RBACAuthorizer: authz,
	})
	client := iamsdk.NewPEPClient(newIAMVerbsTransport(t, svc))

	resp, err := client.CheckBatch(tenantCtx(policyConditionsTenant), &iamsdk.CheckBatchRequest{
		Requests: []iamsdk.CheckRequest{
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/1"},
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/2"},
			{Subject: iamsdk.CheckSubject{ID: "u1"}, Action: "read", Resource: "doc/3"},
		},
		StopOnDeny: stopOnDeny,
	})
	require.NoError(t, err)
	return resp, authz
}

func TestEmbeddedPEPCheckBatch_StopOnDenyStopsAtFirstDenial(t *testing.T) {
	resp, authz := checkBatchAllowDenyAllow(t, true)

	require.Len(t, resp.Results, 2)
	assert.Equal(t, int32(2), authz.calls.Load())
	assert.Equal(t, 1, resp.DeniedCount)
	assert.Equal(t, 1, resp.AllowedCount)
	assert.False(t, resp.AllAllowed)
	assert.False(t, resp.Results[1].Allowed)
}

func TestEmbeddedPEPCheckBatch_WithoutStopOnDenyEvaluatesEveryCheck(t *testing.T) {
	resp, authz := checkBatchAllowDenyAllow(t, false)

	require.Len(t, resp.Results, 3)
	assert.Equal(t, int32(3), authz.calls.Load())
	assert.Equal(t, 1, resp.DeniedCount)
	assert.Equal(t, 2, resp.AllowedCount)
	assert.False(t, resp.AllAllowed)
}

func TestIAMPolicySessionPEPDispatchKeysRegistered(t *testing.T) {
	for _, key := range []string{
		"quicbaas.IAM.Policy/Simulate",
		"quicbaas.IAM.PEP/CheckBatch",
	} {
		_, ok := iamUnaryHandlers[key]
		assert.True(t, ok, "%s is not registered", key)
	}
}

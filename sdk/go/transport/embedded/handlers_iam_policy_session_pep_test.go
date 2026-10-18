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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"

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

func TestIAMPolicySessionPEPDispatchKeysRegistered(t *testing.T) {
	for _, key := range []string{
		"quicbaas.IAM.Policy/Simulate",
		"quicbaas.IAM.PEP/CheckBatch",
	} {
		_, ok := iamUnaryHandlers[key]
		assert.True(t, ok, "%s is not registered", key)
	}
}

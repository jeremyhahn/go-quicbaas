package service

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// flaggedPolicyRecord is a stored record for policy p1 in tenant t1 with an
// unknown rule key: the strict load flags it and never applies it.
const flaggedPolicyRecord = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
	`"metadata":{"name":"p1","tenant":"t1"},` +
	`"spec":{"rules":[{"name":"allow-read","effect":"allow","actions":["read"],` +
	`"resources":["/test/*"],"bogus":true}]}}`

// newFlaggedPolicyWrapper serves the IAM service over a backend store that
// holds the flagged record p1 in tenant t1.
func newFlaggedPolicyWrapper(t *testing.T) (*iamServiceWrapper, *iam.PolicyEngine) {
	t.Helper()
	kv := qrdbsdk.NewMemoryKVStore()
	key := iam.KeyPrefixTenant + "t1" + iam.KeyPrefixPolicy + "p1"
	require.NoError(t, kv.Put(context.Background(), key, []byte(flaggedPolicyRecord)))
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewBackendStorage(kv)})
	return &iamServiceWrapper{provider: &mockIAMServiceProvider{policyEngine: pe}}, pe
}

func TestIAMServiceWrapper_UpdatePolicy_RepairsFlaggedRecord(t *testing.T) {
	ctx := context.Background()
	w, pe := newFlaggedPolicyWrapper(t)

	_, err := w.UpdatePolicy(ctx, &UpdatePolicyRequest{TenantID: "t1", Name: "p1", Policy: happyPolicy("t1", "p1")})
	require.NoError(t, err)

	repaired, err := pe.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err, "a strict Get must return the repaired record whole")
	assert.Nil(t, repaired.LoadError)
	assert.Equal(t, happyPolicy("t1", "p1").Spec, repaired.Spec)
}

func TestIAMServiceWrapper_DeletePolicy_RemovesFlaggedRecord(t *testing.T) {
	ctx := context.Background()
	w, pe := newFlaggedPolicyWrapper(t)

	_, err := w.DeletePolicy(ctx, &DeletePolicyRequest{TenantID: "t1", Name: "p1"})
	require.NoError(t, err)

	policies, err := pe.ListPolicies(ctx, "t1", iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, policies)
}

func TestIAMServiceWrapper_UpdatePolicy_MalformedLeavesFlaggedRecord(t *testing.T) {
	ctx := context.Background()
	w, pe := newFlaggedPolicyWrapper(t)

	malformed := happyPolicy("t1", "p1")
	malformed.Spec.Rules = nil
	_, err := w.UpdatePolicy(ctx, &UpdatePolicyRequest{TenantID: "t1", Name: "p1", Policy: malformed})
	require.ErrorIs(t, err, iam.ErrPolicyInvalid)

	flagged, err := pe.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	require.NotNil(t, flagged.LoadError, "the flagged record must be left as it was")
	assert.Equal(t, []string{"spec.rules[0].bogus"}, flagged.LoadError.UnknownKeys)
}

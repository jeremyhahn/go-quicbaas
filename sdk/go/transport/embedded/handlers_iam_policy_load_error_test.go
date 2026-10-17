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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// flaggedEmbeddedRecord is a stored policy whose rule carries a key the
// strict decoder refuses, so the engine lists and gets it as a flagged entry.
const flaggedEmbeddedRecord = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
	`"metadata":{"name":"flagged","tenant":"t1"},` +
	`"spec":{"rules":[{"name":"allow-read","effect":"allow","actions":["read"],` +
	`"resources":["/test/*"],"bogus":"secret-value"}]}}`

const healthyEmbeddedRecord = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
	`"metadata":{"name":"healthy","tenant":"t1"},` +
	`"spec":{"rules":[{"name":"allow-read","effect":"allow","actions":["read"],` +
	`"resources":["/test/*"]}]}}`

// policyLookupEnvelope mirrors the SDK client's get and list envelopes,
// which the handlers read by field name.
type policyLookupEnvelope struct {
	TenantID   string
	PolicyName string
}

// newFlaggedPolicyTestService returns the authorized-admin double with a
// policy engine over a KV store holding one flagged and one healthy record.
func newFlaggedPolicyTestService(t *testing.T) *mockQuicBaaSWithAuthorizedAdmin {
	t.Helper()
	kv := qrdbsdk.NewMemoryKVStore()
	records := map[string]string{"flagged": flaggedEmbeddedRecord, "healthy": healthyEmbeddedRecord}
	for name, record := range records {
		key := iam.KeyPrefixTenant + "t1" + iam.KeyPrefixPolicy + name
		require.NoError(t, kv.Put(context.Background(), key, []byte(record)))
	}

	svc := newAuthorizedAdminTestService(t)
	svc.policy = iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        iam.NewBackendStorage(kv),
		RBACAuthorizer: allowAllAuthorizer{},
	})
	return svc
}

func assertEmbeddedFlagged(t *testing.T, p iamsdk.Policy) {
	t.Helper()
	require.NotNil(t, p.LoadError, "a flagged record must carry load_error")
	assert.Equal(t, []string{"spec.rules[0].bogus"}, p.LoadError.UnknownKeys)
	assert.NotEmpty(t, p.LoadError.Reason)
	assert.NotContains(t, p.LoadError.Reason, "secret-value", "load_error never carries values")
	assert.Equal(t, "flagged", p.Name)
	assert.Equal(t, "t1", p.TenantID)
}

func TestHandleIAMPolicyGet_LoadError(t *testing.T) {
	svc := newFlaggedPolicyTestService(t)
	tests := []struct {
		name    string
		policy  string
		flagged bool
	}{
		{name: "flagged record carries load_error", policy: "flagged", flagged: true},
		{name: "healthy record has none", policy: "healthy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := handleIAMPolicyGet(context.Background(), svc,
				&policyLookupEnvelope{TenantID: "t1", PolicyName: tt.policy})
			require.NoError(t, err)
			p, ok := out.(*iamsdk.Policy)
			require.True(t, ok, "result = %T", out)
			if !tt.flagged {
				assert.Nil(t, p.LoadError)
				assert.Equal(t, "healthy", p.Name)
				return
			}
			assertEmbeddedFlagged(t, *p)
		})
	}
}

func TestHandleIAMPolicyLookup_NoPolicyEngineFails(t *testing.T) {
	svc := newAuthorizedAdminTestService(t)
	svc.policy = nil
	req := &policyLookupEnvelope{TenantID: "t1", PolicyName: "flagged"}

	_, err := handleIAMPolicyGet(context.Background(), svc, req)
	assert.Error(t, err, "get without a policy engine")
	_, err = handleIAMPolicyList(context.Background(), svc, req)
	assert.Error(t, err, "list without a policy engine")
}

func TestHandleIAMPolicyList_LoadError(t *testing.T) {
	svc := newFlaggedPolicyTestService(t)
	out, err := handleIAMPolicyList(context.Background(), svc, &policyLookupEnvelope{TenantID: "t1"})
	require.NoError(t, err)
	resp, ok := out.(*iamsdk.ListPoliciesResponse)
	require.True(t, ok, "result = %T", out)
	require.Len(t, resp.Policies, 2, "a flagged record is listed, never skipped")

	byName := make(map[string]iamsdk.Policy, len(resp.Policies))
	for _, p := range resp.Policies {
		byName[p.Name] = p
	}
	assertEmbeddedFlagged(t, byName["flagged"])
	assert.Nil(t, byName["healthy"].LoadError, "a healthy record has no load_error")
}

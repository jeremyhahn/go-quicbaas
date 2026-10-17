package grpc

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

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

// A stored policy record that does not decode strictly is flagged and never
// applied. DeletePolicy must still remove it: Delete is the operator's
// repair path when no corrected document is at hand.
func TestIAMServer_DeletePolicy_RemovesFlaggedRecord(t *testing.T) {
	ctx := context.Background()
	kv := qrdbsdk.NewMemoryKVStore()
	key := iam.KeyPrefixTenant + "t1" + iam.KeyPrefixPolicy + "p1"
	require.NoError(t, kv.Put(ctx, key, []byte(`{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","bogus":true}`)))
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewBackendStorage(kv)})

	flagged, err := engine.GetPolicy(ctx, "t1", "p1")
	require.NoError(t, err)
	require.ErrorIs(t, flagged.LoadError, iam.ErrPolicyRecordInvalid, "precondition: the record is flagged")

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	srv := NewIAMServer(NewIAMServiceAdapter(nil, nil, engine, nil, nil, nil), log)

	_, err = srv.DeletePolicy(callerContext("t1"), &iampb.DeletePolicyRequest{TenantId: "t1", PolicyName: "p1"})
	require.NoError(t, err)

	policies, err := engine.ListPolicies(ctx, "t1", iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, policies, "the flagged record must be gone")
}

// flaggedRecordWithSecret is a stored record with an unknown rule key; its
// value must never reach the wire.
const flaggedRecordWithSecret = `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",` +
	`"metadata":{"name":"p1","tenant":"t1"},"spec":{"rules":[{"name":"r","bogus":"secret-value"}]}}`

// newFlaggedPolicyIAMServer serves one flagged record, t1/p1.
func newFlaggedPolicyIAMServer(t *testing.T) *IAMServer {
	t.Helper()
	kv := qrdbsdk.NewMemoryKVStore()
	key := iam.KeyPrefixTenant + "t1" + iam.KeyPrefixPolicy + "p1"
	require.NoError(t, kv.Put(context.Background(), key, []byte(flaggedRecordWithSecret)))
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewBackendStorage(kv)})

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewIAMServer(NewIAMServiceAdapter(nil, nil, engine, nil, nil, nil), log)
}

func assertFlaggedProtoPolicy(t *testing.T, p *iampb.Policy) {
	t.Helper()
	assert.Equal(t, "p1", p.GetName())
	require.NotNil(t, p.GetLoadError(), "a flagged record must carry load_error")
	assert.NotEmpty(t, p.GetLoadError().GetReason())
	assert.Equal(t, []string{"spec.rules[0].bogus"}, p.GetLoadError().GetUnknownKeys())
	assert.NotContains(t, p.String(), "secret-value", "load_error must never carry values")
}

func TestIAMServer_GetPolicy_FlaggedRecordCarriesLoadError(t *testing.T) {
	srv := newFlaggedPolicyIAMServer(t)

	got, err := srv.GetPolicy(callerContext("t1"), &iampb.GetPolicyRequest{TenantId: "t1", PolicyName: "p1"})
	require.NoError(t, err)
	assertFlaggedProtoPolicy(t, got)
}

func TestIAMServer_ListPolicies_FlaggedRecordCarriesLoadError(t *testing.T) {
	srv := newFlaggedPolicyIAMServer(t)

	got, err := srv.ListPolicies(callerContext("t1"), &iampb.ListPoliciesRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.Len(t, got.GetPolicies(), 1)
	assertFlaggedProtoPolicy(t, got.GetPolicies()[0])
}

func TestConvertPolicyToProto_LoadError(t *testing.T) {
	healthy, err := convertPolicyToProto(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "ok", Tenant: "t1"}})
	require.NoError(t, err)
	assert.Nil(t, healthy.GetLoadError(), "a well-formed policy has no load_error")

	flagged, err := convertPolicyToProto(&iam.Policy{
		Metadata:  iam.PolicyMetadata{Name: "p1", Tenant: "t1"},
		LoadError: &iam.StoredPolicyDecodeError{Reason: "bad", UnknownKeys: []string{"spec.x"}},
	})
	require.NoError(t, err)
	require.NotNil(t, flagged.GetLoadError())
	assert.Equal(t, "bad", flagged.GetLoadError().GetReason())
	assert.Equal(t, []string{"spec.x"}, flagged.GetLoadError().GetUnknownKeys())
}

func TestPolicyLoadError_ProtoRoundTrip(t *testing.T) {
	sent := &iampb.Policy{
		Name:      "p1",
		TenantId:  "t1",
		LoadError: &iampb.PolicyLoadError{Reason: "bad", UnknownKeys: []string{"spec.a", "spec.b"}},
	}
	wire, err := proto.Marshal(sent)
	require.NoError(t, err)

	var received iampb.Policy
	require.NoError(t, proto.Unmarshal(wire, &received))
	assert.True(t, proto.Equal(sent, &received), "load_error must survive the wire: got %v", &received)

	wire, err = proto.Marshal(&iampb.Policy{Name: "ok"})
	require.NoError(t, err)
	var healthy iampb.Policy
	require.NoError(t, proto.Unmarshal(wire, &healthy))
	assert.Nil(t, healthy.GetLoadError(), "a policy sent without load_error has none")
}

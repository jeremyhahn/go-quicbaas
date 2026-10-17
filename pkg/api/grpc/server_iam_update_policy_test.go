// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// These tests drive gRPC UpdatePolicy through the real adapter, policy engine
// and in-memory storage, because the defect (#1406) lived in the translation
// between the request and the stored policy: spec_yaml was never parsed and
// the response echoed it back as though it had been applied.

const updatePolicyTenant = "tenant-1"

// newPolicyGRPCStack builds an IAMServer over a real policy engine holding one
// policy named name with a single allow-read rule.
func newPolicyGRPCStack(t *testing.T, name string) *IAMServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	server := NewIAMServer(createTestIAMAdapter91(t), log)

	_, err = server.CreatePolicy(callerContext(updatePolicyTenant), &iampb.CreatePolicyRequest{
		TenantId:    updatePolicyTenant,
		Name:        name,
		Description: "original",
		SpecYaml: []byte(`subjects:
  users: ["*"]
rules:
  - name: allow-read
    effect: allow
    actions: ["read"]
    resources: ["/*"]
`),
	})
	require.NoError(t, err)
	return server
}

// storedPolicy reads the policy back through gRPC GetPolicy and decodes it.
func storedPolicy(t *testing.T, server *IAMServer, name string) (*iampb.Policy, iam.Policy) {
	t.Helper()
	pb, err := server.GetPolicy(callerContext(updatePolicyTenant), &iampb.GetPolicyRequest{
		TenantId:   updatePolicyTenant,
		PolicyName: name,
	})
	require.NoError(t, err)
	var policy iam.Policy
	require.NoError(t, yaml.Unmarshal(pb.SpecYaml, &policy))
	return pb, policy
}

func ruleNames(policy iam.Policy) []string {
	names := make([]string, 0, len(policy.Spec.Rules))
	for _, rule := range policy.Spec.Rules {
		names = append(names, rule.Name)
	}
	return names
}

func TestGRPC_UpdatePolicyAppliesTheNewSpec(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "spec-update")

	resp, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:    updatePolicyTenant,
		PolicyName:  "spec-update",
		Description: "tightened",
		SpecYaml: []byte(`subjects:
  users: ["*"]
rules:
  - name: deny-write
    effect: deny
    actions: ["write"]
    resources: ["/secrets/*"]
`),
	})
	require.NoError(t, err)

	storedPB, stored := storedPolicy(t, server, "spec-update")
	assert.Equal(t, []string{"deny-write"}, ruleNames(stored), "the new spec was not stored")
	assert.Equal(t, iam.EffectDeny, stored.Spec.Rules[0].Effect)
	assert.Equal(t, "tightened", stored.Metadata.Description)

	var returned iam.Policy
	require.NoError(t, yaml.Unmarshal(resp.SpecYaml, &returned))
	assert.Equal(t, stored.Spec, returned.Spec, "the response does not describe the stored spec")
	assert.Equal(t, storedPB.Description, resp.Description)
}

func TestGRPC_UpdatePolicyMetadataOnlyKeepsTheRules(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "metadata-update")

	resp, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:    updatePolicyTenant,
		PolicyName:  "metadata-update",
		Description: "relabelled",
		Labels:      map[string]string{"team": "platform"},
		Priority:    7,
	})
	require.NoError(t, err)

	_, stored := storedPolicy(t, server, "metadata-update")
	assert.Equal(t, []string{"allow-read"}, ruleNames(stored), "a metadata-only update changed the rules")
	assert.Equal(t, []string{"*"}, stored.Spec.Subjects.Users)
	assert.Equal(t, "relabelled", stored.Metadata.Description)
	assert.Equal(t, map[string]string{"team": "platform"}, stored.Metadata.Labels)
	assert.Equal(t, 7, stored.Metadata.Priority)

	var returned iam.Policy
	require.NoError(t, yaml.Unmarshal(resp.SpecYaml, &returned))
	assert.Equal(t, stored.Spec, returned.Spec)
	assert.Equal(t, int32(7), resp.Priority)
}

func TestGRPC_UpdatePolicyRefusesBadYAMLAndStoresNothing(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"malformed":    "rules: [unclosed",
		"no rules":     "subjects:\n  users: [\"*\"]\n",
		"invalid rule": "rules:\n  - name: broken\n    effect: maybe\n    actions: [\"read\"]\n    resources: [\"/*\"]\n",
	}
	for name, specYAML := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := newPolicyGRPCStack(t, "bad-update")

			_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
				TenantId:    updatePolicyTenant,
				PolicyName:  "bad-update",
				Description: "must not land",
				SpecYaml:    []byte(specYAML),
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))

			_, stored := storedPolicy(t, server, "bad-update")
			assert.Equal(t, []string{"allow-read"}, ruleNames(stored))
			assert.Equal(t, "original", stored.Metadata.Description)
		})
	}
}

// namedPolicyDocument is a full policy document declaring metadata.name.
func namedPolicyDocument(name string) []byte {
	return []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: ` + name + `
spec:
  subjects:
    users: ["*"]
  rules:
    - name: deny-write
      effect: deny
      actions: ["write"]
      resources: ["/secrets/*"]
`)
}

func TestGRPC_UpdatePolicyRefusesMismatchedDocumentName(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "name-target")

	_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:    updatePolicyTenant,
		PolicyName:  "name-target",
		Description: "must not land",
		SpecYaml:    namedPolicyDocument("other"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	want := iam.CheckPolicyName(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "other"}}, "name-target").Error()
	assert.Contains(t, status.Convert(err).Message(), want)

	_, stored := storedPolicy(t, server, "name-target")
	assert.Equal(t, []string{"allow-read"}, ruleNames(stored))
	assert.Equal(t, "original", stored.Metadata.Description)
}

func TestGRPC_UpdatePolicyAcceptsMatchingDocumentName(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "name-match")

	_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:    updatePolicyTenant,
		PolicyName:  "name-match",
		Description: "tightened",
		SpecYaml:    namedPolicyDocument("name-match"),
	})
	require.NoError(t, err)

	_, stored := storedPolicy(t, server, "name-match")
	assert.Equal(t, []string{"deny-write"}, ruleNames(stored))
	assert.Equal(t, "tightened", stored.Metadata.Description)
}

func TestGRPC_ValidatePolicyReportsDocumentName(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "validate-name")
	want := iam.CheckPolicyName(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "other"}}, "validate-name").Error()

	tests := []struct {
		docName   string
		wantValid bool
	}{
		{"other", false},
		{"validate-name", true},
	}
	for _, tt := range tests {
		resp, err := server.ValidatePolicy(callerContext(updatePolicyTenant), &iampb.ValidatePolicyRequest{
			TenantId: updatePolicyTenant,
			Name:     "validate-name",
			SpecYaml: namedPolicyDocument(tt.docName),
		})
		require.NoError(t, err, tt.docName)
		assert.Equal(t, tt.wantValid, resp.Valid, tt.docName)
		if !tt.wantValid {
			assert.Contains(t, resp.Errors, want)
		}
	}
}

func TestGRPC_UpdatePolicyUnknownPolicyIsNotFound(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "exists")

	_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:    updatePolicyTenant,
		PolicyName:  "missing",
		Description: "nothing to update",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPC_CreatePolicyAnswersWithTheStoredPolicy(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "created")

	resp, err := server.CreatePolicy(callerContext(updatePolicyTenant), &iampb.CreatePolicyRequest{
		TenantId:    updatePolicyTenant,
		Name:        "second",
		Description: "fresh",
		SpecYaml: []byte(`# caller comment that is not part of the stored policy
subjects:
  users: ["*"]
rules:
  - name: allow-list
    effect: allow
    actions: ["list"]
    resources: ["/*"]
`),
	})
	require.NoError(t, err)

	storedPB, stored := storedPolicy(t, server, "second")
	assert.Equal(t, []string{"allow-list"}, ruleNames(stored))
	assert.Equal(t, storedPB.SpecYaml, resp.SpecYaml, "the response does not describe the stored policy")
	assert.NotContains(t, string(resp.SpecYaml), "caller comment", "the response echoes the request's spec_yaml")
	assert.Equal(t, "fresh", resp.Description)
}

func TestGRPC_CreatePolicyRefusesADuplicateAndKeepsTheStoredPolicy(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "taken")

	_, err := server.CreatePolicy(callerContext(updatePolicyTenant), &iampb.CreatePolicyRequest{
		TenantId:    updatePolicyTenant,
		Name:        "taken",
		Description: "must not land",
		SpecYaml:    []byte("rules:\n  - name: deny-all\n    effect: deny\n    actions: [\"*\"]\n    resources: [\"/*\"]\n"),
	})
	require.Error(t, err)

	_, stored := storedPolicy(t, server, "taken")
	assert.Equal(t, []string{"allow-read"}, ruleNames(stored))
	assert.Equal(t, "original", stored.Metadata.Description)
}

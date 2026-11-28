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

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func twoProtoRules() []*iampb.PolicyRule {
	return []*iampb.PolicyRule{
		{Name: "read-orders", Effect: "allow", Priority: 10, Actions: []string{"data:read"}, Resources: []string{"orders://*"}},
		{Name: "no-delete", Effect: "deny", Priority: 20, Actions: []string{"data:delete", "data:purge"}, Resources: []string{"orders://*", "users://*"}},
	}
}

func TestGRPC_CreatePolicyStoresStructuredRules(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "base")

	_, err := server.CreatePolicy(callerContext(updatePolicyTenant), &iampb.CreatePolicyRequest{
		TenantId:    updatePolicyTenant,
		Name:        "structured",
		Description: "from rules",
		Labels:      map[string]string{"team": "platform"},
		Priority:    5,
		Rules:       twoProtoRules(),
	})
	require.NoError(t, err)

	_, stored := storedPolicy(t, server, "structured")
	assert.Equal(t, "structured", stored.Metadata.Name)
	assert.Equal(t, updatePolicyTenant, stored.Metadata.Tenant)
	assert.Equal(t, "from rules", stored.Metadata.Description)
	assert.Equal(t, map[string]string{"team": "platform"}, stored.Metadata.Labels)
	assert.Equal(t, 5, stored.Metadata.Priority)
	assert.Equal(t, []iam.Rule{
		{Name: "read-orders", Effect: iam.EffectAllow, Priority: 10, Actions: []string{"data:read"}, Resources: []string{"orders://*"}},
		{Name: "no-delete", Effect: iam.EffectDeny, Priority: 20, Actions: []string{"data:delete", "data:purge"}, Resources: []string{"orders://*", "users://*"}},
	}, stored.Spec.Rules)
}

func TestGRPC_UpdatePolicyWithRulesReplacesTheRules(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "rules-update")

	_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), &iampb.UpdatePolicyRequest{
		TenantId:   updatePolicyTenant,
		PolicyName: "rules-update",
		Rules:      twoProtoRules(),
	})
	require.NoError(t, err)

	_, stored := storedPolicy(t, server, "rules-update")
	assert.Equal(t, []string{"read-orders", "no-delete"}, ruleNames(stored))
	assert.Equal(t, []string{"*"}, stored.Spec.Subjects.Users, "a rules update dropped the stored subjects")
	assert.Equal(t, "original", stored.Metadata.Description)
}

func TestGRPC_PolicyRulesRefusalsStoreNothing(t *testing.T) {
	t.Parallel()
	server := newPolicyGRPCStack(t, "refusals")
	badEffect := []*iampb.PolicyRule{
		{Name: "ok", Effect: "allow", Actions: []string{"read"}, Resources: []string{"/*"}},
		{Name: "bad", Effect: "Allow", Actions: []string{"read"}, Resources: []string{"/*"}},
	}
	emptyEffect := []*iampb.PolicyRule{{Name: "none", Actions: []string{"read"}, Resources: []string{"/*"}}}
	spec := []byte("rules:\n  - name: x\n    effect: allow\n    actions: [\"read\"]\n    resources: [\"/*\"]\n")

	creates := []struct {
		name string
		req  *iampb.CreatePolicyRequest
		msg  string
	}{
		{"both", &iampb.CreatePolicyRequest{SpecYaml: spec, Rules: twoProtoRules()}, "set spec_yaml or rules, not both"},
		{"neither", &iampb.CreatePolicyRequest{}, "spec_yaml or rules is required"},
		{"bad effect", &iampb.CreatePolicyRequest{Rules: badEffect}, "rules[1].effect"},
		{"empty effect", &iampb.CreatePolicyRequest{Rules: emptyEffect}, "rules[0].effect"},
	}
	for _, tc := range creates {
		tc.req.TenantId = updatePolicyTenant
		tc.req.Name = "refused-" + tc.name
		_, err := server.CreatePolicy(callerContext(updatePolicyTenant), tc.req)
		require.Error(t, err, tc.name)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), tc.name)
		assert.Contains(t, status.Convert(err).Message(), tc.msg, tc.name)
		_, err = server.GetPolicy(callerContext(updatePolicyTenant), &iampb.GetPolicyRequest{
			TenantId: updatePolicyTenant, PolicyName: tc.req.Name,
		})
		assert.Equal(t, codes.NotFound, status.Code(err), "%s: a refused create stored a policy", tc.name)
	}

	updates := []struct {
		name string
		req  *iampb.UpdatePolicyRequest
		msg  string
	}{
		{"both", &iampb.UpdatePolicyRequest{SpecYaml: spec, Rules: twoProtoRules()}, "set spec_yaml or rules, not both"},
		{"bad effect", &iampb.UpdatePolicyRequest{Rules: badEffect, Description: "changed"}, "rules[1].effect"},
	}
	for _, tc := range updates {
		tc.req.TenantId = updatePolicyTenant
		tc.req.PolicyName = "refusals"
		_, err := server.UpdatePolicy(callerContext(updatePolicyTenant), tc.req)
		require.Error(t, err, tc.name)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), tc.name)
		assert.Contains(t, status.Convert(err).Message(), tc.msg, tc.name)
		_, stored := storedPolicy(t, server, "refusals")
		assert.Equal(t, []string{"allow-read"}, ruleNames(stored), "%s: a refused update changed the rules", tc.name)
		assert.Equal(t, "original", stored.Metadata.Description, "%s: a refused update changed the metadata", tc.name)
	}
}

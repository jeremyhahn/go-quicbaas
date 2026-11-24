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
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// plainSDKRules are rules the engine stores whole: names, effects,
// priorities and action and resource patterns.
func plainSDKRules() []iam.Rule {
	return []iam.Rule{
		{
			Name:      "read-orders",
			Effect:    iam.EffectAllow,
			Priority:  10,
			Actions:   []iam.ActionMatcher{{Pattern: "data:read"}},
			Resources: []iam.ResourceMatcher{{Pattern: "orders://*"}},
		},
		{
			Name:      "no-delete",
			Effect:    iam.EffectDeny,
			Priority:  20,
			Actions:   []iam.ActionMatcher{{Pattern: "data:delete"}, {Pattern: "data:purge"}},
			Resources: []iam.ResourceMatcher{{Pattern: "orders://*"}},
		},
	}
}

func wantProtoRules() []*iampb.PolicyRule {
	return []*iampb.PolicyRule{
		{Name: "read-orders", Effect: "allow", Priority: 10, Actions: []string{"data:read"}, Resources: []string{"orders://*"}},
		{Name: "no-delete", Effect: "deny", Priority: 20, Actions: []string{"data:delete", "data:purge"}, Resources: []string{"orders://*"}},
	}
}

func assertProtoRules(t *testing.T, got []*iampb.PolicyRule) {
	t.Helper()
	want := wantProtoRules()
	if len(got) != len(want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	for i := range want {
		if !proto.Equal(got[i], want[i]) {
			t.Fatalf("rules[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestIAMHandler_CreatePolicy_SendsPlainRules(t *testing.T) {
	t.Parallel()
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	req := map[string]interface{}{"tenant_id": "t1", "request": &iam.CreatePolicyRequest{
		Name:     "p",
		Priority: 5,
		Rules:    plainSDKRules(),
	}}

	if _, err := h.handleCreatePolicy(context.Background(), client, req); err != nil {
		t.Fatalf("handleCreatePolicy() = %v, want nil", err)
	}
	if len(client.created) != 1 {
		t.Fatalf("CreatePolicy requests = %d, want 1", len(client.created))
	}
	got := client.created[0]
	if got.GetName() != "p" || got.GetPriority() != 5 || len(got.GetSpecYaml()) != 0 {
		t.Fatalf("CreatePolicy request = %v, want name p, priority 5, no spec_yaml", got)
	}
	assertProtoRules(t, got.GetRules())
}

func TestIAMHandler_UpdatePolicy_SendsPlainRules(t *testing.T) {
	t.Parallel()
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	req := policyRequest(map[string]interface{}{"rules": plainSDKRules()})

	if _, err := h.handleUpdatePolicy(context.Background(), client, req); err != nil {
		t.Fatalf("handleUpdatePolicy() = %v, want nil", err)
	}
	if len(client.updated) != 1 {
		t.Fatalf("UpdatePolicy requests = %d, want 1", len(client.updated))
	}
	assertProtoRules(t, client.updated[0].GetRules())
}

func TestIAMHandler_UpdatePolicy_NoRulesSendsNone(t *testing.T) {
	t.Parallel()
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	desc := "d"
	req := policyRequest(map[string]interface{}{"request": nil})
	req["request"] = &iam.UpdatePolicyRequest{Description: &desc}

	if _, err := h.handleUpdatePolicy(context.Background(), client, req); err != nil {
		t.Fatalf("handleUpdatePolicy() = %v, want nil", err)
	}
	if len(client.updated) != 1 || client.updated[0].GetRules() != nil {
		t.Fatalf("UpdatePolicy requests = %v, want one with no rules", client.updated)
	}
}

func TestIAMHandler_PolicyWrite_RefusesUnstorableRuleFields(t *testing.T) {
	t.Parallel()
	withCondition := plainSDKRules()
	withCondition[1].Conditions = []iam.Condition{{Key: "subject.role"}}
	negated := plainSDKRules()
	negated[0].Actions[0].Negate = true
	typed := plainSDKRules()
	typed[0].Resources[0].Type = "order"
	badEffect := plainSDKRules()
	badEffect[1].Effect = iam.Effect(7)

	cases := map[string][]iam.Rule{
		"rule conditions": withCondition,
		"action negate":   negated,
		"resource type":   typed,
		"unknown effect":  badEffect,
	}
	for name, rules := range cases {
		h := &iamHandler{}
		client := &recordingPolicyClient{}
		create := map[string]interface{}{"tenant_id": "t1", "request": &iam.CreatePolicyRequest{Name: "p", Rules: rules}}
		if _, err := h.handleCreatePolicy(context.Background(), client, create); !errors.IsInvalidInput(err) {
			t.Fatalf("%s: handleCreatePolicy() = %v, want INVALID_INPUT", name, err)
		}
		update := policyRequest(map[string]interface{}{"rules": rules})
		if _, err := h.handleUpdatePolicy(context.Background(), client, update); !errors.IsInvalidInput(err) {
			t.Fatalf("%s: handleUpdatePolicy() = %v, want INVALID_INPUT", name, err)
		}
		if len(client.created)+len(client.updated) != 0 {
			t.Fatalf("%s: refused request reached the server: %d create, %d update", name, len(client.created), len(client.updated))
		}
	}
}

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

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingPolicyClient records the policy RPCs it receives. Any other
// method panics through the nil embedded interface.
type recordingPolicyClient struct {
	iampb.IAMServiceClient
	created []*iampb.CreatePolicyRequest
	updated []*iampb.UpdatePolicyRequest
}

func (c *recordingPolicyClient) CreatePolicy(_ context.Context, in *iampb.CreatePolicyRequest, _ ...grpc.CallOption) (*iampb.Policy, error) {
	c.created = append(c.created, in)
	return samplePolicy(), nil
}

func (c *recordingPolicyClient) UpdatePolicy(_ context.Context, in *iampb.UpdatePolicyRequest, _ ...grpc.CallOption) (*iampb.Policy, error) {
	c.updated = append(c.updated, in)
	return samplePolicy(), nil
}

func policyRequest(request map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"tenant_id": "t1", "policy_name": "p", "request": request}
}

func TestIAMHandler_PolicyWrite_RefusesStructuredFields(t *testing.T) {
	request := map[string]interface{}{
		"name":      "p",
		"spec_yaml": []byte("rules: []"),
		"rules":     []map[string]interface{}{{"effect": "allow", "subjects": []string{"alice"}}},
	}
	h := &iamHandler{}
	client := &recordingPolicyClient{}

	if _, err := h.handleCreatePolicy(context.Background(), client, policyRequest(request)); !errors.IsInvalidInput(err) {
		t.Fatalf("handleCreatePolicy() = %v, want INVALID_INPUT", err)
	}
	if _, err := h.handleUpdatePolicy(context.Background(), client, policyRequest(request)); !errors.IsInvalidInput(err) {
		t.Fatalf("handleUpdatePolicy() = %v, want INVALID_INPUT", err)
	}
	if len(client.created)+len(client.updated) != 0 {
		t.Fatalf("refused request reached the server: %d create, %d update", len(client.created), len(client.updated))
	}
}

func TestIAMHandler_CreatePolicy_SendsSpecYAMLOnly(t *testing.T) {
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	request := map[string]interface{}{"name": "p", "spec_yaml": []byte("rules: []")}

	if _, err := h.handleCreatePolicy(context.Background(), client, policyRequest(request)); err != nil {
		t.Fatalf("handleCreatePolicy() = %v, want nil", err)
	}
	if len(client.created) != 1 || string(client.created[0].GetSpecYaml()) != "rules: []" {
		t.Fatalf("CreatePolicy requests = %v, want one carrying spec_yaml", client.created)
	}
}

func TestIAMHandler_UpdatePolicy_RefusesSpecYAML(t *testing.T) {
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	request := map[string]interface{}{"spec_yaml": []byte("rules: []")}

	if _, err := h.handleUpdatePolicy(context.Background(), client, policyRequest(request)); !errors.IsInvalidInput(err) {
		t.Fatalf("handleUpdatePolicy() = %v, want INVALID_INPUT", err)
	}
	if len(client.updated) != 0 {
		t.Fatalf("refused update reached the server: %v", client.updated)
	}
}

func TestIAMHandler_UpdatePolicy_SendsMetadata(t *testing.T) {
	h := &iamHandler{}
	client := &recordingPolicyClient{}
	request := map[string]interface{}{"description": "d", "priority": 7}

	if _, err := h.handleUpdatePolicy(context.Background(), client, policyRequest(request)); err != nil {
		t.Fatalf("handleUpdatePolicy() = %v, want nil", err)
	}
	if len(client.updated) != 1 {
		t.Fatalf("UpdatePolicy requests = %d, want 1", len(client.updated))
	}
	got := client.updated[0]
	if got.GetPolicyName() != "p" || got.GetDescription() != "d" || got.GetPriority() != 7 || len(got.GetSpecYaml()) != 0 {
		t.Fatalf("UpdatePolicy request = %v, want name p, description d, priority 7, no spec_yaml", got)
	}
}

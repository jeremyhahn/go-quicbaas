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

package iam

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockPolicyTransport implements transport.Transport for testing.
type mockPolicyTransport struct {
	requestFunc func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
}

func (m *mockPolicyTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFunc != nil {
		return m.requestFunc(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockPolicyTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockPolicyTransport) Conn() interface{} {
	return nil
}

func (m *mockPolicyTransport) Close() error {
	return nil
}

func (m *mockPolicyTransport) Healthy() bool {
	return true
}

func TestNewPolicyClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewPolicyClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		if client == nil {
			t.Error("expected non-nil client")
		}
	})
}

func TestPolicyClient_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		expectedPolicy := &Policy{
			Name:        "test-policy",
			TenantID:    "tenant-1",
			Description: "Test policy",
			Version:     1,
			Enabled:     true,
			Priority:    100,
			Effect:      EffectAllow,
			Rules: []Rule{
				{
					Name:   "allow-read",
					Effect: EffectAllow,
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != policyServiceName {
					t.Errorf("expected service %s, got %s", policyServiceName, service)
				}
				if method != "Create" {
					t.Errorf("expected method Create, got %s", method)
				}
				return expectedPolicy, nil
			},
		}

		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name:        "test-policy",
			Description: "Test policy",
			Enabled:     true,
			Priority:    100,
			Effect:      EffectAllow,
			Rules: []Rule{
				{
					Name:   "allow-read",
					Effect: EffectAllow,
				},
			},
		}

		policy, err := client.Create(context.Background(), "tenant-1", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		if policy.Name != expectedPolicy.Name {
			t.Errorf("expected name %s, got %s", expectedPolicy.Name, policy.Name)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		_, err := client.Create(nil, "tenant-1", req)
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Create(context.Background(), "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		_, err := client.Create(context.Background(), "", req)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty policy name", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name: "",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		_, err := client.Create(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty policy name")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("no rules and no spec YAML", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name:  "test-policy",
			Rules: []Rule{},
		}

		_, err := client.Create(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for a create with no rules and no spec YAML")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	// gRPC and MCP carry the rules in SpecYAML and refuse structured rules
	// on create, so a rule-less create with a spec must reach the transport.
	t.Run("spec YAML without rules", func(t *testing.T) {
		spec := []byte("rules:\n  - name: allow-read\n    effect: allow\n")
		var sent *createPolicyRequest
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if r, ok := req.(*createPolicyRequest); ok {
					sent = r
				}
				return &Policy{Name: "test-policy", TenantID: "tenant-1"}, nil
			},
		}
		client := NewPolicyClient(tr)

		policy, err := client.Create(context.Background(), "tenant-1",
			&CreatePolicyRequest{Name: "test-policy", SpecYAML: spec})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if policy == nil || policy.Name != "test-policy" {
			t.Fatalf("expected the created policy, got %+v", policy)
		}
		if sent == nil || string(sent.Request.SpecYAML) != string(spec) {
			t.Errorf("expected the spec YAML sent to the transport, got %+v", sent)
		}
	})

	t.Run("nil response from server", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		_, err := client.Create(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}
		client := NewPolicyClient(tr)
		req := &CreatePolicyRequest{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		_, err := client.Create(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestPolicyClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedPolicy := &Policy{
			Name:     "test-policy",
			TenantID: "tenant-1",
			Enabled:  true,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				return expectedPolicy, nil
			},
		}

		client := NewPolicyClient(tr)
		policy, err := client.Get(context.Background(), "tenant-1", "test-policy")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		if policy.Name != expectedPolicy.Name {
			t.Errorf("expected name %s, got %s", expectedPolicy.Name, policy.Name)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Get(nil, "tenant-1", "test-policy")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Get(context.Background(), "", "test-policy")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty policy name", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Get(context.Background(), "tenant-1", "")
		if err == nil {
			t.Error("expected error for empty policy name")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewPolicyClient(tr)

		_, err := client.Get(context.Background(), "tenant-1", "nonexistent")
		if err == nil {
			t.Error("expected error for not found")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestPolicyClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedPolicy := &Policy{
			Name:        "test-policy",
			TenantID:    "tenant-1",
			Description: "Updated description",
			Version:     2,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				return expectedPolicy, nil
			},
		}

		client := NewPolicyClient(tr)
		desc := "Updated description"
		req := &UpdatePolicyRequest{
			Description: &desc,
		}

		policy, err := client.Update(context.Background(), "tenant-1", "test-policy", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		if policy.Description != expectedPolicy.Description {
			t.Errorf("expected description %s, got %s", expectedPolicy.Description, policy.Description)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &UpdatePolicyRequest{}

		_, err := client.Update(nil, "tenant-1", "test-policy", req)
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &UpdatePolicyRequest{}

		_, err := client.Update(context.Background(), "", "test-policy", req)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty policy name", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &UpdatePolicyRequest{}

		_, err := client.Update(context.Background(), "tenant-1", "", req)
		if err == nil {
			t.Error("expected error for empty policy name")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Update(context.Background(), "tenant-1", "test-policy", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})
}

func TestPolicyClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				return &deletePolicyResponse{}, nil
			},
		}

		client := NewPolicyClient(tr)
		err := client.Delete(context.Background(), "tenant-1", "test-policy")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("success with nil response", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewPolicyClient(tr)
		err := client.Delete(context.Background(), "tenant-1", "test-policy")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		err := client.Delete(nil, "tenant-1", "test-policy")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		err := client.Delete(context.Background(), "", "test-policy")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty policy name", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		err := client.Delete(context.Background(), "tenant-1", "")
		if err == nil {
			t.Error("expected error for empty policy name")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewPolicyClient(tr)
		err := client.Delete(context.Background(), "tenant-1", "test-policy")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestPolicyClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedResp := &ListPoliciesResponse{
			Policies: []Policy{
				{Name: "policy-1"},
				{Name: "policy-2"},
			},
			NextCursor: "next",
			HasMore:    true,
			TotalCount: 10,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPolicyClient(tr)
		req := &ListPoliciesRequest{
			Limit:  10,
			Filter: "policy-*",
		}

		resp, err := client.List(context.Background(), "tenant-1", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.Policies) != 2 {
			t.Errorf("expected 2 policies, got %d", len(resp.Policies))
		}
	})

	t.Run("nil request uses defaults", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &ListPoliciesResponse{Policies: []Policy{}}, nil
			},
		}

		client := NewPolicyClient(tr)
		resp, err := client.List(context.Background(), "tenant-1", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.List(nil, "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.List(context.Background(), "", nil)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil response returns empty list", func(t *testing.T) {
		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewPolicyClient(tr)
		resp, err := client.List(context.Background(), "tenant-1", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.Policies) != 0 {
			t.Errorf("expected empty policies list")
		}
	})
}

func TestPolicyClient_Validate(t *testing.T) {
	t.Run("success - valid policy", func(t *testing.T) {
		expectedResult := &ValidationResult{
			Valid:    true,
			Errors:   nil,
			Warnings: nil,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Validate" {
					t.Errorf("expected method Validate, got %s", method)
				}
				return expectedResult, nil
			},
		}

		client := NewPolicyClient(tr)
		policy := &Policy{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test-rule"},
			},
		}

		result, err := client.Validate(context.Background(), "tenant-1", policy)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if !result.Valid {
			t.Error("expected valid result")
		}
	})

	t.Run("success - invalid policy", func(t *testing.T) {
		expectedResult := &ValidationResult{
			Valid: false,
			Errors: []ValidationError{
				{
					Field:   "rules",
					Code:    "EMPTY",
					Message: "at least one rule is required",
				},
			},
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return expectedResult, nil
			},
		}

		client := NewPolicyClient(tr)
		policy := &Policy{Name: "test-policy"}

		result, err := client.Validate(context.Background(), "tenant-1", policy)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Valid {
			t.Error("expected invalid result")
		}
		if len(result.Errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(result.Errors))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		policy := &Policy{Name: "test-policy"}

		_, err := client.Validate(nil, "tenant-1", policy)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		policy := &Policy{Name: "test-policy"}

		_, err := client.Validate(context.Background(), "", policy)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil policy", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Validate(context.Background(), "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil policy")
		}
	})
}

// TestPolicyClient_ValidateSendsName pins that Validate carries the policy's
// name, which the server checks against metadata.name, and that an unnamed
// policy omits the field rather than sending an empty value.
func TestPolicyClient_ValidateSendsName(t *testing.T) {
	tests := []struct {
		name       string
		policyName string
		wantKey    bool
	}{
		{name: "named policy sends name", policyName: "p1", wantKey: true},
		{name: "unnamed policy omits name", policyName: "", wantKey: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []byte
			tr := &mockPolicyTransport{
				requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					body, err := json.Marshal(req)
					if err != nil {
						return nil, err
					}
					sent = body
					return &ValidationResult{Valid: true}, nil
				},
			}
			if _, err := NewPolicyClient(tr).Validate(context.Background(), "t1", &Policy{Name: tt.policyName}); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			var wire map[string]interface{}
			if err := json.Unmarshal(sent, &wire); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			got, ok := wire["name"]
			if ok != tt.wantKey {
				t.Fatalf("name present = %v, want %v (request %s)", ok, tt.wantKey, sent)
			}
			if tt.wantKey && got != tt.policyName {
				t.Errorf("name = %v, want %q", got, tt.policyName)
			}
		})
	}
}

func TestPolicyClient_Test(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedResp := &TestPolicyResponse{
			Passed: 2,
			Failed: 0,
			Results: []TestCaseResult{
				{Name: "test-1", Passed: true, ActualResult: EffectAllow},
				{Name: "test-2", Passed: true, ActualResult: EffectDeny},
			},
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Test" {
					t.Errorf("expected method Test, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPolicyClient(tr)
		req := &TestPolicyRequest{
			PolicyName: "test-policy",
			TestCases: []TestCase{
				{
					Name:           "test-1",
					Subject:        Subject{ID: "user-1"},
					Action:         "data:read",
					Resource:       "orders://123",
					ExpectedResult: EffectAllow,
				},
				{
					Name:           "test-2",
					Subject:        Subject{ID: "user-2"},
					Action:         "data:delete",
					Resource:       "orders://456",
					ExpectedResult: EffectDeny,
				},
			},
		}

		resp, err := client.Test(context.Background(), "tenant-1", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if resp.Passed != 2 {
			t.Errorf("expected 2 passed, got %d", resp.Passed)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &TestPolicyRequest{
			PolicyName: "test-policy",
			TestCases:  []TestCase{{Name: "test-1"}},
		}

		_, err := client.Test(nil, "tenant-1", req)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &TestPolicyRequest{
			PolicyName: "test-policy",
			TestCases:  []TestCase{{Name: "test-1"}},
		}

		_, err := client.Test(context.Background(), "", req)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Test(context.Background(), "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty policy name", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &TestPolicyRequest{
			PolicyName: "",
			TestCases:  []TestCase{{Name: "test-1"}},
		}

		_, err := client.Test(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty policy name")
		}
	})

	t.Run("empty test cases", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &TestPolicyRequest{
			PolicyName: "test-policy",
			TestCases:  []TestCase{},
		}

		_, err := client.Test(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty test cases")
		}
	})
}

func TestPolicyClient_Simulate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedResp := &SimulateResponse{
			Allowed:          true,
			Effect:           EffectAllow,
			MatchedPolicies:  []string{"policy-1"},
			MatchedRules:     []string{"allow-read"},
			EvaluationTimeNs: 1000,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Simulate" {
					t.Errorf("expected method Simulate, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject: Subject{
				ID:       "user-1",
				TenantID: "tenant-1",
				Roles:    []string{"admin"},
			},
			Action:       "data:read",
			Resource:     "orders://123",
			IncludeTrace: true,
		}

		resp, err := client.Simulate(context.Background(), "tenant-1", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !resp.Allowed {
			t.Error("expected allowed result")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject:  Subject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Simulate(nil, "tenant-1", req)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject:  Subject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Simulate(context.Background(), "", req)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.Simulate(context.Background(), "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty subject ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject:  Subject{ID: ""},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Simulate(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty subject ID")
		}
	})

	t.Run("empty action", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject:  Subject{ID: "user-1"},
			Action:   "",
			Resource: "orders://123",
		}

		_, err := client.Simulate(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty action")
		}
	})

	t.Run("empty resource", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &SimulateRequest{
			Subject:  Subject{ID: "user-1"},
			Action:   "data:read",
			Resource: "",
		}

		_, err := client.Simulate(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty resource")
		}
	})
}

func TestPolicyClient_GetEffectivePermissions(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedResp := &EffectivePermissionsResponse{
			Permissions: []EffectivePermission{
				{
					Action:          "data:read",
					ResourcePattern: "orders://*",
					Effect:          EffectAllow,
					SourcePolicies:  []string{"policy-1"},
				},
			},
			EvaluatedPolicies: []string{"policy-1", "policy-2"},
			EvaluationTimeNs:  2000,
		}

		tr := &mockPolicyTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetEffectivePermissions" {
					t.Errorf("expected method GetEffectivePermissions, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPolicyClient(tr)
		req := &EffectivePermissionsRequest{
			Subject: Subject{
				ID:       "user-1",
				TenantID: "tenant-1",
			},
			ResourceType: "orders",
		}

		resp, err := client.GetEffectivePermissions(context.Background(), "tenant-1", req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.Permissions) != 1 {
			t.Errorf("expected 1 permission, got %d", len(resp.Permissions))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &EffectivePermissionsRequest{
			Subject: Subject{ID: "user-1"},
		}

		_, err := client.GetEffectivePermissions(nil, "tenant-1", req)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &EffectivePermissionsRequest{
			Subject: Subject{ID: "user-1"},
		}

		_, err := client.GetEffectivePermissions(context.Background(), "", req)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)

		_, err := client.GetEffectivePermissions(context.Background(), "tenant-1", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty subject ID", func(t *testing.T) {
		tr := &mockPolicyTransport{}
		client := NewPolicyClient(tr)
		req := &EffectivePermissionsRequest{
			Subject: Subject{ID: ""},
		}

		_, err := client.GetEffectivePermissions(context.Background(), "tenant-1", req)
		if err == nil {
			t.Error("expected error for empty subject ID")
		}
	})
}

func TestEffect_String(t *testing.T) {
	tests := []struct {
		effect   Effect
		expected string
	}{
		{EffectAllow, "ALLOW"},
		{EffectDeny, "DENY"},
		{Effect(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.effect.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestSubjectType_String(t *testing.T) {
	tests := []struct {
		subjectType SubjectType
		expected    string
	}{
		{SubjectTypeUser, "USER"},
		{SubjectTypeService, "SERVICE"},
		{SubjectTypeGroup, "GROUP"},
		{SubjectTypeRole, "ROLE"},
		{SubjectTypeAny, "ANY"},
		{SubjectType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.subjectType.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestComparisonOperator_String(t *testing.T) {
	tests := []struct {
		op       ComparisonOperator
		expected string
	}{
		{ComparisonEquals, "EQUALS"},
		{ComparisonNotEquals, "NOT_EQUALS"},
		{ComparisonGreaterThan, "GREATER_THAN"},
		{ComparisonGreaterThanOrEqual, "GREATER_THAN_OR_EQUAL"},
		{ComparisonLessThan, "LESS_THAN"},
		{ComparisonLessThanOrEqual, "LESS_THAN_OR_EQUAL"},
		{ComparisonIn, "IN"},
		{ComparisonNotIn, "NOT_IN"},
		{ComparisonContains, "CONTAINS"},
		{ComparisonStartsWith, "STARTS_WITH"},
		{ComparisonEndsWith, "ENDS_WITH"},
		{ComparisonMatches, "MATCHES"},
		{ComparisonExists, "EXISTS"},
		{ComparisonNotExists, "NOT_EXISTS"},
		{ComparisonOperator(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.op.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestConditionType_String(t *testing.T) {
	tests := []struct {
		condType ConditionType
		expected string
	}{
		{ConditionTypeAttribute, "ATTRIBUTE"},
		{ConditionTypeTime, "TIME"},
		{ConditionTypeIP, "IP"},
		{ConditionTypeContext, "CONTEXT"},
		{ConditionTypeAnd, "AND"},
		{ConditionTypeOr, "OR"},
		{ConditionTypeNot, "NOT"},
		{ConditionType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.condType.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

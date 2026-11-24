package rest

// Tests for the REST SDK transport's quicbaas.IAM.Policy
// GetEffectivePermissions verb, which reaches quicbaasd's
// POST /api/v1/tenants/{tenant_id}/policies/effective-permissions route.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const effectiveServerResult = `{
	"subject": {"id": "u-1", "tenant_id": "t-1"},
	"permissions": {
		"orders://*": {
			"allowed": ["read", "write"],
			"denied": ["delete"],
			"sources": {"read": ["p-read"], "write": ["p-write"], "delete": ["p-deny"]}
		}
	},
	"total_policies_evaluated": 3,
	"evaluation_time_ns": 1500
}`

func TestIAMPolicyEffective_REST_PostsServerBody(t *testing.T) {
	rec := newIAMRecorder(t)
	rec.respBody = effectiveServerResult
	tr := newTestTransport(t, rec.server)

	_, err := iam.NewPolicyClient(tr).GetEffectivePermissions(context.Background(), "t-1", &iam.EffectivePermissionsRequest{
		Subject:       iam.Subject{ID: "u-1", Roles: []string{"r-1"}},
		ResourceType:  "orders",
		ActionPattern: "read",
		IncludeDenied: true,
	})
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	if rec.gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.gotMethod)
	}
	if want := "/api/v1/tenants/t-1/policies/effective-permissions"; rec.gotPath != want {
		t.Errorf("path = %q, want %q", rec.gotPath, want)
	}

	var body struct {
		Subject struct {
			ID       string   `json:"id"`
			TenantID string   `json:"tenant_id"`
			Roles    []string `json:"roles"`
		} `json:"subject"`
		ResourcePatterns   []string `json:"resource_patterns"`
		Actions            []string `json:"actions"`
		IncludeDenyDetails bool     `json:"include_deny_details"`
	}
	if err := json.Unmarshal(rec.gotBody, &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.gotBody, err)
	}
	if body.Subject.ID != "u-1" || body.Subject.TenantID != "t-1" || len(body.Subject.Roles) != 1 {
		t.Errorf("subject = %+v, want id u-1, tenant t-1, one role", body.Subject)
	}
	if len(body.ResourcePatterns) != 1 || body.ResourcePatterns[0] != "orders://*" {
		t.Errorf("resource_patterns = %v, want [orders://*]", body.ResourcePatterns)
	}
	if len(body.Actions) != 1 || body.Actions[0] != "read" {
		t.Errorf("actions = %v, want [read]", body.Actions)
	}
	if !body.IncludeDenyDetails {
		t.Error("include_deny_details = false, want true")
	}
}

func TestIAMPolicyEffective_REST_DefaultsToEveryResource(t *testing.T) {
	rec := newIAMRecorder(t)
	rec.respBody = `{"permissions": {}}`
	tr := newTestTransport(t, rec.server)

	_, err := iam.NewPolicyClient(tr).GetEffectivePermissions(context.Background(), "t-1",
		&iam.EffectivePermissionsRequest{Subject: iam.Subject{ID: "u-1"}})
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.gotBody, &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.gotBody, err)
	}
	patterns, _ := body["resource_patterns"].([]interface{})
	if len(patterns) != 1 || patterns[0] != "*" {
		t.Errorf("resource_patterns = %v, want [*]", body["resource_patterns"])
	}
	if _, ok := body["actions"]; ok {
		t.Errorf("actions = %v, want unset", body["actions"])
	}
	if _, ok := body["include_deny_details"]; ok {
		t.Errorf("include_deny_details = %v, want unset", body["include_deny_details"])
	}
}

func TestIAMPolicyEffective_REST_DecodesServerResult(t *testing.T) {
	rec := newIAMRecorder(t)
	rec.respBody = effectiveServerResult
	tr := newTestTransport(t, rec.server)

	resp, err := iam.NewPolicyClient(tr).GetEffectivePermissions(context.Background(), "t-1",
		&iam.EffectivePermissionsRequest{Subject: iam.Subject{ID: "u-1"}, ResourceType: "orders", IncludeDenied: true})
	if err != nil {
		t.Fatalf("GetEffectivePermissions: %v", err)
	}
	want := []iam.EffectivePermission{
		{Action: "read", ResourcePattern: "orders://*", Effect: iam.EffectAllow, SourcePolicies: []string{"p-read"}},
		{Action: "write", ResourcePattern: "orders://*", Effect: iam.EffectAllow, SourcePolicies: []string{"p-write"}},
		{Action: "delete", ResourcePattern: "orders://*", Effect: iam.EffectDeny, SourcePolicies: []string{"p-deny"}},
	}
	if len(resp.Permissions) != len(want) {
		t.Fatalf("permissions = %+v, want %d entries", resp.Permissions, len(want))
	}
	for i, w := range want {
		got := resp.Permissions[i]
		if got.Action != w.Action || got.ResourcePattern != w.ResourcePattern || got.Effect != w.Effect ||
			len(got.SourcePolicies) != 1 || got.SourcePolicies[0] != w.SourcePolicies[0] {
			t.Errorf("permissions[%d] = %+v, want %+v", i, got, w)
		}
	}
	if len(resp.EvaluatedPolicies) != 3 || resp.EvaluatedPolicies[0] != "p-deny" {
		t.Errorf("evaluated policies = %v, want [p-deny p-read p-write]", resp.EvaluatedPolicies)
	}
	if resp.EvaluationTimeNs != 1500 {
		t.Errorf("evaluation_time_ns = %d, want 1500", resp.EvaluationTimeNs)
	}
}

func TestIAMPolicyEffective_REST_ServerRefusalSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"refused"}`))
	}))
	defer server.Close()

	_, err := iam.NewPolicyClient(newTestTransport(t, server)).GetEffectivePermissions(context.Background(), "t-1",
		&iam.EffectivePermissionsRequest{Subject: iam.Subject{ID: "u-1"}})
	if !errors.IsForbidden(err) {
		t.Errorf("err = %v, want forbidden", err)
	}
}

package rest

import (
	"context"
	"encoding/json"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestMapServerPolicyToSDK_Enabled asserts a policy read over REST reports
// the stored spec.enabled, where an absent value means enabled.
func TestMapServerPolicyToSDK_Enabled(t *testing.T) {
	tests := []struct {
		name       string
		serverJSON string
		want       bool
	}{
		{name: "explicit true", serverJSON: `{"metadata":{"name":"p1"},"spec":{"enabled":true}}`, want: true},
		{name: "explicit false", serverJSON: `{"metadata":{"name":"p1"},"spec":{"enabled":false}}`, want: false},
		{name: "absent enabled", serverJSON: `{"metadata":{"name":"p1"},"spec":{}}`, want: true},
		{name: "absent spec", serverJSON: `{"apiVersion":"iam.quicbaas.io/v1","metadata":{"name":"p1"}}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp interface{}
			if err := json.Unmarshal([]byte(tt.serverJSON), &resp); err != nil {
				t.Fatalf("server JSON: %v", err)
			}
			mapped, err := json.Marshal(mapServerPolicyToSDK(resp))
			if err != nil {
				t.Fatalf("marshal mapped policy: %v", err)
			}
			var policy iam.Policy
			if err := json.Unmarshal(mapped, &policy); err != nil {
				t.Fatalf("decode SDK policy: %v", err)
			}
			if policy.Enabled != tt.want {
				t.Errorf("Enabled = %v, want %v", policy.Enabled, tt.want)
			}
		})
	}
}

// TestIAMHandlerPolicyEnabledThroughTransport drives Create, Get and Update
// over REST and requires each to report the same Enabled value for a policy
// whose server document omits spec.enabled.
func TestIAMHandlerPolicyEnabledThroughTransport(t *testing.T) {
	decode := func(t *testing.T, result interface{}) iam.Policy {
		t.Helper()
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		var policy iam.Policy
		if err := json.Unmarshal(raw, &policy); err != nil {
			t.Fatalf("decode SDK policy: %v", err)
		}
		return policy
	}
	ctx := context.Background()

	t.Run("absent enabled defaults true", func(t *testing.T) {
		rec := newIAMRecorder(t)
		rec.respBody = `{"apiVersion":"iam.quicbaas.io/v1","metadata":{"name":"p1","tenant":"t1"},"spec":{}}`
		tr := newTestTransport(t, rec.server)
		calls := []struct {
			verb string
			req  map[string]interface{}
		}{
			{"Create", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "p1"}}},
			{"Get", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"}},
			{"Update", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1", "request": map[string]interface{}{"effect": "allow"}}},
		}
		for _, c := range calls {
			result, err := tr.Request(ctx, iamPolicyServicePrefix, c.verb, c.req)
			if err != nil {
				t.Fatalf("%s: %v", c.verb, err)
			}
			policy := decode(t, result)
			if !policy.Enabled {
				t.Errorf("%s: Enabled = false, want true", c.verb)
			}
			if policy.Name != "p1" {
				t.Errorf("%s: Name = %q, want p1", c.verb, policy.Name)
			}
		}
	})

	t.Run("explicit false on Get", func(t *testing.T) {
		rec := newIAMRecorder(t)
		rec.respBody = `{"apiVersion":"iam.quicbaas.io/v1","metadata":{"name":"p1","tenant":"t1"},"spec":{"enabled":false}}`
		tr := newTestTransport(t, rec.server)
		result, err := tr.Request(ctx, iamPolicyServicePrefix, "Get", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if policy := decode(t, result); policy.Enabled {
			t.Errorf("Get: Enabled = true, want false")
		}
	})
}

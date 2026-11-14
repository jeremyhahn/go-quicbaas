package rest

import (
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

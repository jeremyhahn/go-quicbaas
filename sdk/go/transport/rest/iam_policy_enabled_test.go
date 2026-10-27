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

package rest

import (
	"encoding/json"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// decodeSDKPolicy maps one server-format policy JSON to the SDK Policy.
func decodeSDKPolicy(t *testing.T, serverJSON string) iam.Policy {
	t.Helper()
	var resp interface{}
	if err := json.Unmarshal([]byte(serverJSON), &resp); err != nil {
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
	return policy
}

// TestMapServerPolicyToSDK_ReportsStoredEnabled covers issue #1929: an
// absent spec.enabled means enabled, and an explicit value is reported as
// stored.
func TestMapServerPolicyToSDK_ReportsStoredEnabled(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{
			name: "spec.enabled absent",
			json: `{"apiVersion":"v1","metadata":{"name":"p","tenant":"t1"},"spec":{"rules":[]}}`,
			want: true,
		},
		{
			name: "spec.enabled true",
			json: `{"apiVersion":"v1","metadata":{"name":"p"},"spec":{"enabled":true}}`,
			want: true,
		},
		{
			name: "spec.enabled false",
			json: `{"apiVersion":"v1","metadata":{"name":"p"},"spec":{"enabled":false}}`,
			want: false,
		},
		{
			name: "load error is never applied",
			json: `{"apiVersion":"v1","metadata":{"name":"p"},"spec":{},"load_error":{"reason":"bad"}}`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeSDKPolicy(t, tt.json); got.Enabled != tt.want {
				t.Fatalf("Enabled = %v, want %v", got.Enabled, tt.want)
			}
		})
	}
}

func TestMapServerPolicyListToSDK_ReportsStoredEnabled(t *testing.T) {
	policies := decodeSDKPolicyList(t, serverPolicyListJSON)
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}
	if policies[0].Enabled {
		t.Errorf("flagged policy reports enabled; it is never applied")
	}
	if !policies[1].Enabled {
		t.Errorf("healthy policy without spec.enabled reports disabled")
	}
}

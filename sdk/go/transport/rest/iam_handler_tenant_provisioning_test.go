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
	"testing"
)

// mapTenantCreateRequest is what the REST (and therefore QUIC) transport puts
// on the wire, so it is the single place where a dropped provisioning knob
// would go unnoticed.

func TestMapTenantCreateRequest_CarriesProvisioningKnobs(t *testing.T) {
	body := mapTenantCreateRequest(map[string]interface{}{
		"id":             "acme",
		"name":           "Acme",
		"isolation_type": "barrier",
		"engine_quotas": map[string]interface{}{
			"max_databases": float64(4),
		},
		"engine_entitlements": map[string]interface{}{
			"allow_dedicated_raft": true,
		},
	})

	if got := body["isolation_type"]; got != "barrier" {
		t.Errorf("isolation_type = %v, want \"barrier\"", got)
	}
	quotas, ok := body["engine_quotas"].(map[string]interface{})
	if !ok {
		t.Fatalf("engine_quotas is %T, want an object", body["engine_quotas"])
	}
	if got := quotas["max_databases"]; got != float64(4) {
		t.Errorf("engine_quotas.max_databases = %v, want 4", got)
	}
	entitlements, ok := body["engine_entitlements"].(map[string]interface{})
	if !ok {
		t.Fatalf("engine_entitlements is %T, want an object", body["engine_entitlements"])
	}
	if got := entitlements["allow_dedicated_raft"]; got != true {
		t.Errorf("engine_entitlements.allow_dedicated_raft = %v, want true", got)
	}
}

// The change must be additive: a request that never named the knobs produces
// the same body it produced before they existed.
func TestMapTenantCreateRequest_OmitsUnsetProvisioningKnobs(t *testing.T) {
	tests := []struct {
		name string
		req  map[string]interface{}
	}{
		{
			name: "knobs absent",
			req:  map[string]interface{}{"id": "acme", "name": "Acme"},
		},
		{
			name: "knobs present but empty",
			req: map[string]interface{}{
				"id":                  "acme",
				"name":                "Acme",
				"isolation_type":      "",
				"engine_quotas":       nil,
				"engine_entitlements": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := mapTenantCreateRequest(tt.req)
			for _, key := range []string{"isolation_type", "engine_quotas", "engine_entitlements"} {
				if _, present := body[key]; present {
					t.Errorf("%s must be absent from the body, got %v", key, body[key])
				}
			}
			if body["id"] != "acme" || body["name"] != "Acme" {
				t.Errorf("existing fields changed: %v", body)
			}
		})
	}
}

func TestMapTenantCreateRequest_NilRequest(t *testing.T) {
	if body := mapTenantCreateRequest(nil); body != nil {
		t.Errorf("expected nil body for a nil request, got %v", body)
	}
}

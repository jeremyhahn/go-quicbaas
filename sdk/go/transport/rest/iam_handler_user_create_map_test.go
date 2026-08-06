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

package rest

import (
	"testing"
)

// TestMapUserCreateRequest_RolesForwarded proves the SDK REST mapper no
// longer drops the "roles" field: roles must reach the REST body so the
// server can validate-before-create (transport parity with gRPC/Unix).
func TestMapUserCreateRequest_RolesForwarded(t *testing.T) {
	body := mapUserCreateRequest(map[string]interface{}{
		"email":        "roles@example.com",
		"username":     "roleuser",
		"display_name": "Role User",
		"roles":        []interface{}{"role-a", "role-b"},
	})

	roles, ok := body["roles"].([]interface{})
	if !ok {
		t.Fatalf("Expected roles in mapped body, got %v", body)
	}
	if len(roles) != 2 || roles[0] != "role-a" || roles[1] != "role-b" {
		t.Fatalf("Expected roles [role-a role-b], got %v", roles)
	}
}

// TestMapUserCreateRequest_NoRolesOmitted proves an absent or empty roles
// list does not inject a roles key into the REST body.
func TestMapUserCreateRequest_NoRolesOmitted(t *testing.T) {
	tests := []struct {
		name string
		req  map[string]interface{}
	}{
		{"absent", map[string]interface{}{"email": "a@b.co", "username": "abc"}},
		{"empty", map[string]interface{}{"email": "a@b.co", "username": "abc", "roles": []interface{}{}}},
		{"wrong type", map[string]interface{}{"email": "a@b.co", "username": "abc", "roles": "not-a-list"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := mapUserCreateRequest(tt.req)
			if _, exists := body["roles"]; exists {
				t.Fatalf("Expected no roles key for %s input, got %v", tt.name, body["roles"])
			}
		})
	}
}

// TestMapUserCreateRequest_GroupsForwarded proves the SDK REST mapper no
// longer drops the "groups" field: groups must reach the REST body so the
// server can validate-before-create (transport parity with gRPC/Unix/QUIC).
func TestMapUserCreateRequest_GroupsForwarded(t *testing.T) {
	body := mapUserCreateRequest(map[string]interface{}{
		"email":        "groups@example.com",
		"username":     "groupuser",
		"display_name": "Group User",
		"groups":       []interface{}{"group-a", "group-b"},
	})

	groups, ok := body["groups"].([]interface{})
	if !ok {
		t.Fatalf("Expected groups in mapped body, got %v", body)
	}
	if len(groups) != 2 || groups[0] != "group-a" || groups[1] != "group-b" {
		t.Fatalf("Expected groups [group-a group-b], got %v", groups)
	}
}

// TestMapUserCreateRequest_NoGroupsOmitted proves an absent, empty, or
// mistyped groups list does not inject a groups key into the REST body.
func TestMapUserCreateRequest_NoGroupsOmitted(t *testing.T) {
	tests := []struct {
		name string
		req  map[string]interface{}
	}{
		{"absent", map[string]interface{}{"email": "a@b.co", "username": "abc"}},
		{"empty", map[string]interface{}{"email": "a@b.co", "username": "abc", "groups": []interface{}{}}},
		{"wrong type", map[string]interface{}{"email": "a@b.co", "username": "abc", "groups": "not-a-list"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := mapUserCreateRequest(tt.req)
			if _, exists := body["groups"]; exists {
				t.Fatalf("Expected no groups key for %s input, got %v", tt.name, body["groups"])
			}
		})
	}
}

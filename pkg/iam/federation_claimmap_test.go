// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"errors"
	"sort"
	"testing"
)

func provWithMappings(mappings []GroupRoleMapping, defaults []string) *ExternalProvider {
	p := &ExternalProvider{
		ID:           "provider-1",
		DefaultRoles: defaults,
		ClaimMappings: &ClaimMappings{
			Groups:            "groups",
			Roles:             "roles",
			GroupRoleMappings: mappings,
		},
	}
	return p
}

func TestResolveRoles_MappedGroupsMatch(t *testing.T) {
	p := provWithMappings([]GroupRoleMapping{
		{Group: "Acme Administrators", RoleID: "role-admin"},
		{Group: "Acme Auditors", RoleID: "role-auditor"},
	}, nil)

	claims := map[string]interface{}{
		"groups": []interface{}{"Acme Auditors", "Some Other Group"},
	}
	res, err := resolveRoles(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedDefaults {
		t.Error("expected mapped resolution, not defaults")
	}
	if len(res.RoleIDs) != 1 || res.RoleIDs[0] != "role-auditor" {
		t.Errorf("RoleIDs = %v, want [role-auditor]", res.RoleIDs)
	}
}

func TestResolveRoles_MultipleMatches(t *testing.T) {
	p := provWithMappings([]GroupRoleMapping{
		{Group: "admins", RoleID: "role-admin"},
		{Group: "devs", RoleID: "role-dev"},
	}, nil)
	claims := map[string]interface{}{"groups": []interface{}{"admins", "devs"}}
	res, err := resolveRoles(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := append([]string{}, res.RoleIDs...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "role-admin" || got[1] != "role-dev" {
		t.Errorf("RoleIDs = %v, want [role-admin role-dev]", got)
	}
}

func TestResolveRoles_RolesClaimMatch(t *testing.T) {
	p := provWithMappings([]GroupRoleMapping{
		{Group: "platform-admin", RoleID: "role-admin"},
	}, nil)
	// Match comes from the `roles` claim (single string), not groups.
	claims := map[string]interface{}{"roles": "platform-admin"}
	res, err := resolveRoles(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.RoleIDs) != 1 || res.RoleIDs[0] != "role-admin" {
		t.Errorf("RoleIDs = %v", res.RoleIDs)
	}
}

func TestResolveRoles_PrefixMatch(t *testing.T) {
	p := provWithMappings([]GroupRoleMapping{
		{Group: "acme-", RoleID: "role-tenant", Prefix: true},
	}, nil)
	claims := map[string]interface{}{"groups": []interface{}{"acme-engineering"}}
	res, err := resolveRoles(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.RoleIDs) != 1 || res.RoleIDs[0] != "role-tenant" {
		t.Errorf("RoleIDs = %v, want [role-tenant]", res.RoleIDs)
	}
}

func TestResolveRoles_FailClosedNoMatch(t *testing.T) {
	p := provWithMappings([]GroupRoleMapping{
		{Group: "Acme Administrators", RoleID: "role-admin"},
	}, []string{"role-default"})

	claims := map[string]interface{}{"groups": []interface{}{"Unrelated Group"}}
	_, err := resolveRoles(p, claims)
	if err == nil {
		t.Fatal("expected fail-closed NoGroupMatchError when mappings configured but none match")
	}
	var noMatch *NoGroupMatchError
	if !errors.As(err, &noMatch) {
		t.Errorf("error = %v, want NoGroupMatchError", err)
	}
}

func TestResolveRoles_DefaultsFallback(t *testing.T) {
	// No GroupRoleMappings configured: fall back to DefaultRoles.
	p := &ExternalProvider{
		ID:           "provider-1",
		DefaultRoles: []string{"role-default-a", "role-default-b"},
		ClaimMappings: &ClaimMappings{
			Groups: "groups",
		},
	}
	claims := map[string]interface{}{"groups": []interface{}{"anything"}}
	res, err := resolveRoles(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedDefaults {
		t.Error("expected UsedDefaults=true")
	}
	if len(res.RoleIDs) != 2 {
		t.Errorf("RoleIDs = %v, want the two defaults", res.RoleIDs)
	}
}

func TestResolveRoles_DefaultsFallbackNilMappings(t *testing.T) {
	p := &ExternalProvider{ID: "p", DefaultRoles: []string{"role-default"}}
	res, err := resolveRoles(p, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedDefaults || len(res.RoleIDs) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestClaimStringSlice(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"empty string", "", nil},
		{"single string", "a", []string{"a"}},
		{"string slice", []string{"a", "b"}, []string{"a", "b"}},
		{"interface slice", []interface{}{"a", "b", 3}, []string{"a", "b"}},
		{"wrong type", 42, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimStringSlice(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

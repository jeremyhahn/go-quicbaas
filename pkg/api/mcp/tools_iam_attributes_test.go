//go:build mcp

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

package mcp

import (
	"strings"
	"testing"
)

// TestStringAttributes_RefusesANonStringValue.
//
// These are AUTHORIZATION attributes. Keeping the strings and discarding the
// rest accepts a request and persists less than it asked for, so the policy
// evaluated afterwards is not the policy the caller wrote — and nothing in the
// response says so. Refusing is the only honest answer.
func TestStringAttributes_RefusesANonStringValue(t *testing.T) {
	for name, in := range map[string]map[string]interface{}{
		"number": {"clearance": 3},
		"bool":   {"admin": true},
		"nested": {"scope": map[string]interface{}{"a": "b"}},
		"null":   {"team": nil},
		"list":   {"tags": []interface{}{"a"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := stringAttributes(in)
			if err == nil {
				t.Fatalf("a non-string attribute must be refused, got %v", got)
			}
			for k := range in {
				if !strings.Contains(err.Error(), k) {
					t.Errorf("the error must name the offending key %q, got %q", k, err.Error())
				}
			}
		})
	}
}

func TestStringAttributes_AcceptsStrings(t *testing.T) {
	got, err := stringAttributes(map[string]interface{}{"dept": "eng", "region": "us-east"})
	if err != nil {
		t.Fatalf("stringAttributes: %v", err)
	}
	if got["dept"] != "eng" || got["region"] != "us-east" {
		t.Fatalf("got = %v", got)
	}
	empty, err := stringAttributes(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("got = %v, err = %v", empty, err)
	}
}

// TestStringSlice keeps the string members of an MCP array field and drops what
// cannot be one, which is what every enum-valued array here carries.
func TestStringSlice(t *testing.T) {
	got := stringSlice([]interface{}{"webauthn", 42, "piv", nil})
	if len(got) != 2 || got[0] != "webauthn" || got[1] != "piv" {
		t.Fatalf("got = %v, want [webauthn piv]", got)
	}
	if got := stringSlice(nil); len(got) != 0 {
		t.Fatalf("got = %v, want empty", got)
	}
}

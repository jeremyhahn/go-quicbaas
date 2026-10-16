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

package policyfields

import (
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

type rule struct {
	Effect     string                   `json:"effect"`
	Subjects   []string                 `json:"subjects,omitempty"`
	Conditions []map[string]interface{} `json:"conditions,omitempty"`
}

func TestRefuse_NamesMostSpecificField(t *testing.T) {
	nested := []map[string]interface{}{{
		"attribute":      "subject.role",
		"sub_conditions": []map[string]interface{}{{"attribute": "x"}},
	}}
	flat := []map[string]interface{}{{"attribute": "subject.role"}}
	tests := []struct {
		name    string
		request map[string]interface{}
		field   string
	}{
		{"nested policy condition", map[string]interface{}{"conditions": nested}, "conditions[0].sub_conditions"},
		{"policy condition", map[string]interface{}{"conditions": flat}, "conditions"},
		{"targets", map[string]interface{}{"targets": []string{"db/*"}}, "targets"},
		{"rule subjects", map[string]interface{}{"rules": []rule{{Effect: "allow", Subjects: []string{"alice"}}}}, "rules[0].subjects"},
		{"nested rule condition", map[string]interface{}{"rules": []rule{{Effect: "allow"}, {Conditions: nested}}}, "rules[1].conditions[0].sub_conditions"},
		{"rule condition", map[string]interface{}{"rules": []rule{{Conditions: flat}}}, "rules[0].conditions"},
		{"bare rules", map[string]interface{}{"rules": []rule{{Effect: "allow"}}, "spec_yaml": "rules: []"}, "rules"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Refuse("op", "gRPC", tt.request)
			if !errors.IsInvalidInput(err) {
				t.Fatalf("Refuse() = %v, want INVALID_INPUT", err)
			}
			if !strings.Contains(err.Error(), tt.field+" cannot be sent over gRPC") {
				t.Fatalf("Refuse() = %q, want it to name %q", err, tt.field)
			}
		})
	}
}

func TestRefuse_AllowsSpecYAMLOnly(t *testing.T) {
	requests := []map[string]interface{}{
		nil,
		{"name": "p", "spec_yaml": "rules: []", "rules": []rule{}, "targets": []string{}},
	}
	for _, request := range requests {
		if err := Refuse("op", "MCP", request); err != nil {
			t.Fatalf("Refuse(%v) = %v, want nil", request, err)
		}
	}
}

func TestRefuse_NonEncodableRequest(t *testing.T) {
	err := Refuse("op", "MCP", map[string]interface{}{"rules": make(chan int)})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("Refuse() = %v, want INVALID_INPUT", err)
	}
}

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

package policyenabled

import "testing"

func TestResolve(t *testing.T) {
	enabled, disabled := true, false
	tests := []struct {
		name       string
		stored     *bool
		loadFailed bool
		want       bool
	}{
		{name: "absent means enabled", stored: nil, want: true},
		{name: "explicit true", stored: &enabled, want: true},
		{name: "explicit false", stored: &disabled, want: false},
		{name: "load error is never applied", stored: nil, loadFailed: true, want: false},
		{name: "load error wins over explicit true", stored: &enabled, loadFailed: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.stored, tt.loadFailed); got != tt.want {
				t.Fatalf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopy(t *testing.T) {
	tests := []struct {
		name   string
		server map[string]interface{}
		want   bool
	}{
		{name: "no spec", server: map[string]interface{}{}, want: true},
		{name: "spec without enabled", server: map[string]interface{}{"spec": map[string]interface{}{}}, want: true},
		{name: "spec enabled true", server: map[string]interface{}{"spec": map[string]interface{}{"enabled": true}}, want: true},
		{name: "spec enabled false", server: map[string]interface{}{"spec": map[string]interface{}{"enabled": false}}, want: false},
		{name: "non-bool enabled is not a stored value", server: map[string]interface{}{"spec": map[string]interface{}{"enabled": "no"}}, want: true},
		{name: "malformed spec", server: map[string]interface{}{"spec": "rules: []"}, want: true},
		{
			name:   "load error",
			server: map[string]interface{}{"load_error": map[string]interface{}{"reason": "unknown keys"}},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdkPolicy := map[string]interface{}{}
			Copy(tt.server, sdkPolicy)
			if got := sdkPolicy[Key]; got != tt.want {
				t.Fatalf("enabled = %v, want %v", got, tt.want)
			}
		})
	}
}

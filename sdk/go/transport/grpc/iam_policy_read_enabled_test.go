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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// TestConvertProtoPolicyToSDK_ReportsStoredEnabled covers issue #1929: a
// policy read over gRPC reports the enabled state its stored document sets,
// and an absent spec.enabled means enabled.
func TestConvertProtoPolicyToSDK_ReportsStoredEnabled(t *testing.T) {
	tests := []struct {
		name   string
		policy *iampb.Policy
		want   bool
	}{
		{
			name:   "spec.enabled absent",
			policy: &iampb.Policy{Name: "p", SpecYaml: []byte("apiVersion: v1\nspec:\n  rules: []\n")},
			want:   true,
		},
		{
			name:   "spec.enabled true",
			policy: &iampb.Policy{Name: "p", SpecYaml: []byte("spec:\n  enabled: true\n")},
			want:   true,
		},
		{
			name:   "spec.enabled false",
			policy: &iampb.Policy{Name: "p", SpecYaml: []byte("spec:\n  enabled: false\n")},
			want:   false,
		},
		{
			name:   "no document",
			policy: &iampb.Policy{Name: "p"},
			want:   true,
		},
		{
			name:   "undecodable document",
			policy: &iampb.Policy{Name: "p", SpecYaml: []byte("spec: [unterminated")},
			want:   true,
		},
		{
			name: "load error is never applied",
			policy: &iampb.Policy{Name: "p", LoadError: &iampb.PolicyLoadError{
				Reason: `json: unknown field "bogus"`,
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertProtoPolicyToSDK(tt.policy)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Enabled)
		})
	}
}

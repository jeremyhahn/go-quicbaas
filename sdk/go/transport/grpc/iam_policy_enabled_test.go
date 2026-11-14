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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// TestConvertProtoPolicyToSDK_Enabled asserts a policy read over gRPC reports
// the stored spec.enabled, where an absent value means enabled.
func TestConvertProtoPolicyToSDK_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		specYAML string
		want     bool
	}{
		{name: "explicit true", specYAML: "spec:\n  enabled: true\n", want: true},
		{name: "explicit false", specYAML: "spec:\n  enabled: false\n", want: false},
		{name: "absent enabled", specYAML: "spec:\n  rules: []\n", want: true},
		{name: "empty spec", specYAML: "", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertProtoPolicyToSDK(&iampb.Policy{
				Name:     "p1",
				TenantId: "t1",
				SpecYaml: []byte(tt.specYAML),
			})
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Enabled)
		})
	}
}

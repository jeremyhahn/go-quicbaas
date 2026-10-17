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

func TestConvertProtoPolicyToSDK_LoadError(t *testing.T) {
	tests := []struct {
		name    string
		policy  *iampb.Policy
		flagged bool
	}{
		{
			name: "flagged policy carries load_error",
			policy: &iampb.Policy{
				Name:     "flagged",
				TenantId: "t1",
				LoadError: &iampb.PolicyLoadError{
					Reason:      `json: unknown field "bogus"`,
					UnknownKeys: []string{"spec.rules[0].bogus"},
				},
			},
			flagged: true,
		},
		{
			name:   "healthy policy has none",
			policy: &iampb.Policy{Name: "healthy", TenantId: "t1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertProtoPolicyToSDK(tt.policy)
			require.NotNil(t, got)
			assert.Equal(t, tt.policy.Name, got.Name)
			assert.Equal(t, "t1", got.TenantID)
			if !tt.flagged {
				assert.Nil(t, got.LoadError)
				return
			}
			require.NotNil(t, got.LoadError)
			assert.Equal(t, `json: unknown field "bogus"`, got.LoadError.Reason)
			assert.Equal(t, []string{"spec.rules[0].bogus"}, got.LoadError.UnknownKeys)
		})
	}
}

func TestConvertProtoPolicyToSDK_NilPolicy(t *testing.T) {
	assert.Nil(t, convertProtoPolicyToSDK(nil))
}

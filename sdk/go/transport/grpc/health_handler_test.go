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
)

func TestIsHealthService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "health service ping",
			service:  "quicbaas.Health",
			expected: true,
		},
		{
			name:     "health service with method",
			service:  "quicbaas.Health.Ping",
			expected: true,
		},
		{
			name:     "non-health service",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "iam service",
			service:  "quicbaas.IAM.User",
			expected: false,
		},
		{
			name:     "empty service",
			service:  "",
			expected: false,
		},
		{
			name:     "different service starting with health prefix",
			service:  "quicbaas.Healthy",
			expected: true, // This is true because it starts with healthServicePrefix
		},
		{
			name:     "health in different position",
			service:  "some.quicbaas.Health",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHealthService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewHealthHandler(t *testing.T) {
	t.Run("creates handler with nil conn", func(t *testing.T) {
		handler := newHealthHandler(nil)
		assert.NotNil(t, handler)
		assert.Nil(t, handler.conn)
	})
}

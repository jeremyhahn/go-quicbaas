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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func TestConvertProtoSealMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     pb.SealMode
		expected string
	}{
		{
			name:     "manual mode",
			mode:     pb.SealMode_SEAL_MODE_MANUAL,
			expected: "manual",
		},
		{
			name:     "auto mode",
			mode:     pb.SealMode_SEAL_MODE_AUTO,
			expected: "auto",
		},
		{
			name:     "crypto mode",
			mode:     pb.SealMode_SEAL_MODE_CRYPTO,
			expected: "crypto",
		},
		{
			name:     "unspecified mode",
			mode:     pb.SealMode_SEAL_MODE_UNSPECIFIED,
			expected: "unspecified",
		},
		{
			name:     "unknown mode value",
			mode:     pb.SealMode(999),
			expected: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoSealMode(tt.mode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

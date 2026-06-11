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

//go:build !integration

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// KMS Status Conversion Tests with grpc build tag
// =============================================================================

// TestConvertServiceStatusToProtoGRPC tests key status conversion with grpc build tag.
// Note: similar tests exist in handlers_coverage_boost2_test.go without the grpc build tag.
func TestConvertServiceStatusToProtoGRPC(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyStatus
		expected pb.KeyStatus
	}{
		{
			name:     "enabled_status",
			input:    service.KeyStatusEnabled,
			expected: pb.KeyStatus_KEY_STATUS_ENABLED,
		},
		{
			name:     "disabled_status",
			input:    service.KeyStatusDisabled,
			expected: pb.KeyStatus_KEY_STATUS_DISABLED,
		},
		{
			name:     "pending_deletion_status",
			input:    service.KeyStatusPendingDeletion,
			expected: pb.KeyStatus_KEY_STATUS_PENDING_DELETION,
		},
		{
			name:     "deleted_status",
			input:    service.KeyStatusDeleted,
			expected: pb.KeyStatus_KEY_STATUS_DELETED,
		},
		{
			name:     "unknown_defaults_to_enabled",
			input:    service.KeyStatus("unknown"),
			expected: pb.KeyStatus_KEY_STATUS_ENABLED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// =============================================================================
// convertServiceKeyUsageToProto Tests (proto <- service direction)
// =============================================================================

func TestConvertServiceKeyUsageToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    service.KeyUsage
		expected pb.KeyUsage
	}{
		{"encrypt", service.KeyUsageEncrypt, pb.KeyUsage_KEY_USAGE_ENCRYPT},
		{"decrypt", service.KeyUsageDecrypt, pb.KeyUsage_KEY_USAGE_DECRYPT},
		{"sign", service.KeyUsageSign, pb.KeyUsage_KEY_USAGE_SIGN},
		{"verify", service.KeyUsageVerify, pb.KeyUsage_KEY_USAGE_VERIFY},
		{"wrap_key", service.KeyUsageWrapKey, pb.KeyUsage_KEY_USAGE_WRAP_KEY},
		{"unknown_defaults_to_unspecified", service.KeyUsage("nope"), pb.KeyUsage_KEY_USAGE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertServiceKeyUsageToProto(tt.input))
		})
	}
}

// =============================================================================
// unixToTime Tests
// =============================================================================

func TestUnixToTime(t *testing.T) {
	t.Run("zero_returns_zero_time", func(t *testing.T) {
		assert.True(t, unixToTime(0).IsZero())
	})
	t.Run("non_zero_returns_unix_time", func(t *testing.T) {
		got := unixToTime(1700000000)
		assert.False(t, got.IsZero())
		assert.Equal(t, int64(1700000000), got.Unix())
	})
}

// =============================================================================
// protoKeyPolicyToService / serviceKeyPolicyToProto Tests
// =============================================================================

func TestProtoKeyPolicyToService(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		assert.Nil(t, protoKeyPolicyToService(nil))
	})

	t.Run("optional_fields_unset", func(t *testing.T) {
		// ExpiresAt == 0 and MaxUsageCount == 0 must leave pointers nil.
		in := &pb.KeyPolicy{
			KeyId:             "k1",
			Version:           2,
			AllowedOperations: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_SIGN},
			AllowedPrincipals: []string{"alice"},
			DeniedPrincipals:  []string{"mallory"},
			RotationRequired:  true,
			RequireMfa:        true,
		}
		out := protoKeyPolicyToService(in)
		assert.Equal(t, "k1", out.KeyID)
		assert.Equal(t, uint64(2), out.Version)
		assert.Equal(t, []service.KeyUsage{service.KeyUsageSign}, out.AllowedOperations)
		assert.Equal(t, []string{"alice"}, out.AllowedPrincipals)
		assert.Equal(t, []string{"mallory"}, out.DeniedPrincipals)
		assert.True(t, out.RotationRequired)
		assert.True(t, out.RequireMFA)
		assert.Nil(t, out.ExpiresAt)
		assert.Nil(t, out.MaxUsageCount)
		assert.True(t, out.UpdatedAt.IsZero())
	})

	t.Run("optional_fields_set", func(t *testing.T) {
		in := &pb.KeyPolicy{
			KeyId:         "k2",
			ExpiresAt:     1700000100,
			MaxUsageCount: 42,
			UpdatedAt:     1700000000,
		}
		out := protoKeyPolicyToService(in)
		assert.NotNil(t, out.ExpiresAt)
		assert.Equal(t, int64(1700000100), out.ExpiresAt.Unix())
		assert.NotNil(t, out.MaxUsageCount)
		assert.Equal(t, uint64(42), *out.MaxUsageCount)
		assert.Equal(t, int64(1700000000), out.UpdatedAt.Unix())
	})
}

func TestServiceKeyPolicyToProto(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		assert.Nil(t, serviceKeyPolicyToProto(nil))
	})

	t.Run("optional_fields_unset", func(t *testing.T) {
		in := &service.KeyPolicy{
			KeyID:             "k1",
			Version:           3,
			AllowedOperations: []service.KeyUsage{service.KeyUsageEncrypt},
			AllowedPrincipals: []string{"bob"},
			RotationRequired:  true,
			RequireMFA:        false,
		}
		out := serviceKeyPolicyToProto(in)
		assert.Equal(t, "k1", out.KeyId)
		assert.Equal(t, uint64(3), out.Version)
		assert.Equal(t, []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT}, out.AllowedOperations)
		assert.Equal(t, []string{"bob"}, out.AllowedPrincipals)
		assert.True(t, out.RotationRequired)
		assert.False(t, out.RequireMfa)
		assert.Equal(t, int64(0), out.ExpiresAt)
		assert.Equal(t, uint64(0), out.MaxUsageCount)
	})

	t.Run("roundtrip_with_optional_fields", func(t *testing.T) {
		expires := time.Unix(1700000100, 0)
		maxUse := uint64(99)
		orig := &service.KeyPolicy{
			KeyID:             "rt",
			Version:           7,
			AllowedOperations: []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
			AllowedPrincipals: []string{"a"},
			DeniedPrincipals:  []string{"d"},
			ExpiresAt:         &expires,
			RotationRequired:  true,
			MaxUsageCount:     &maxUse,
			RequireMFA:        true,
			UpdatedAt:         time.Unix(1700000000, 0),
		}
		proto := serviceKeyPolicyToProto(orig)
		back := protoKeyPolicyToService(proto)
		assert.Equal(t, orig.KeyID, back.KeyID)
		assert.Equal(t, orig.Version, back.Version)
		assert.Equal(t, orig.AllowedOperations, back.AllowedOperations)
		assert.Equal(t, orig.AllowedPrincipals, back.AllowedPrincipals)
		assert.Equal(t, orig.DeniedPrincipals, back.DeniedPrincipals)
		assert.Equal(t, orig.RotationRequired, back.RotationRequired)
		assert.Equal(t, orig.RequireMFA, back.RequireMFA)
		assert.Equal(t, expires.Unix(), back.ExpiresAt.Unix())
		assert.Equal(t, maxUse, *back.MaxUsageCount)
		assert.Equal(t, orig.UpdatedAt.Unix(), back.UpdatedAt.Unix())
	})
}

// =============================================================================
// protoToServiceRotationPolicy / serviceToProtoRotationPolicy Tests
// =============================================================================

func TestProtoToServiceRotationPolicy(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		assert.Nil(t, protoToServiceRotationPolicy(nil))
	})

	t.Run("without_next_rotation", func(t *testing.T) {
		in := &pb.RotationPolicy{
			Enabled:         true,
			IntervalSeconds: 3600,
			RetainVersions:  4,
			AutoRotate:      true,
		}
		out := protoToServiceRotationPolicy(in)
		assert.True(t, out.Enabled)
		assert.Equal(t, time.Hour, out.Interval)
		assert.Equal(t, 4, out.RetainVersions)
		assert.True(t, out.AutoRotate)
		assert.True(t, out.NextRotation.IsZero())
	})

	t.Run("with_next_rotation", func(t *testing.T) {
		in := &pb.RotationPolicy{
			Enabled:         true,
			IntervalSeconds: 60,
			NextRotation:    1700000000,
		}
		out := protoToServiceRotationPolicy(in)
		assert.False(t, out.NextRotation.IsZero())
		assert.Equal(t, int64(1700000000), out.NextRotation.Unix())
	})
}

func TestServiceToProtoRotationPolicy(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		out, err := serviceToProtoRotationPolicy(nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("retain_versions_out_of_range", func(t *testing.T) {
		out, err := serviceToProtoRotationPolicy(&service.RotationPolicy{RetainVersions: math.MaxInt32 + 1})
		var overflow *conv.ConversionOverflowError
		require.ErrorAs(t, err, &overflow)
		assert.Nil(t, out)
	})

	t.Run("roundtrip", func(t *testing.T) {
		orig := &service.RotationPolicy{
			Enabled:        true,
			Interval:       2 * time.Hour,
			NextRotation:   time.Unix(1700000000, 0),
			RetainVersions: 6,
			AutoRotate:     true,
		}
		proto, err := serviceToProtoRotationPolicy(orig)
		require.NoError(t, err)
		assert.Equal(t, int64(7200), proto.IntervalSeconds)
		assert.Equal(t, int32(6), proto.RetainVersions)
		assert.Equal(t, int64(1700000000), proto.NextRotation)

		back := protoToServiceRotationPolicy(proto)
		assert.Equal(t, orig.Interval, back.Interval)
		assert.Equal(t, orig.RetainVersions, back.RetainVersions)
		assert.Equal(t, orig.NextRotation.Unix(), back.NextRotation.Unix())
		assert.Equal(t, orig.AutoRotate, back.AutoRotate)
	})

	t.Run("zero_next_rotation_omitted", func(t *testing.T) {
		out, err := serviceToProtoRotationPolicy(&service.RotationPolicy{Enabled: false})
		require.NoError(t, err)
		assert.Equal(t, int64(0), out.NextRotation)
	})
}

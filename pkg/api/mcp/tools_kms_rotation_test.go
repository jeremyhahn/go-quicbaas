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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// rotationKMSService embeds testKMSService and overrides the rotation-policy
// methods so success and error paths can be exercised.
type rotationKMSService struct {
	*testKMSService
	setResp    *service.SetKeyRotationPolicyResponse
	setErr     error
	getResp    *service.GetKeyRotationPolicyResponse
	getErr     error
	deleteResp *service.DeleteKeyRotationPolicyResponse
	deleteErr  error
}

func (s *rotationKMSService) SetKeyRotationPolicy(_ context.Context, _ *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	return s.setResp, s.setErr
}

func (s *rotationKMSService) GetKeyRotationPolicy(_ context.Context, _ *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	return s.getResp, s.getErr
}

func (s *rotationKMSService) DeleteKeyRotationPolicy(_ context.Context, _ *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	return s.deleteResp, s.deleteErr
}

// rotationQuicBaaS exposes the rotationKMSService through KMS().
type rotationQuicBaaS struct {
	*MockQuicBaaS
	kms service.KMSService
}

func (m *rotationQuicBaaS) KMS() service.KMSService { return m.kms }

func newRotationRegistry(t *testing.T, kms service.KMSService) *ToolRegistry {
	t.Helper()
	return &ToolRegistry{
		service: &rotationQuicBaaS{MockQuicBaaS: &MockQuicBaaS{}, kms: kms},
		logger:  newTestLogger(t),
	}
}

// newNoKMSRegistry returns a registry whose KMS() yields nil so getKMSService
// returns the service-unavailable error.
func newNoKMSRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	return &ToolRegistry{
		service: &rotationQuicBaaS{MockQuicBaaS: &MockQuicBaaS{}, kms: nil},
		logger:  newTestLogger(t),
	}
}

func decodeMarshalled(t *testing.T, v interface{}) map[string]interface{} {
	t.Helper()
	out, ok := v.(map[string]interface{})
	require.True(t, ok, "marshalResult should return a JSON-compatible map")
	return out
}

// =============================================================================
// executeKMSSetRotationPolicy
// =============================================================================

func TestExecuteKMSSetRotationPolicy(t *testing.T) {
	now := time.Now()

	t.Run("missing_key_id", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{testKMSService: &testKMSService{}})
		_, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{})
		require.Error(t, err)
	})

	t.Run("auto_rotate_without_interval", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{testKMSService: &testKMSService{}})
		_, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{
			"key_id":      "k1",
			"auto_rotate": true,
		})
		require.Error(t, err)
	})

	t.Run("kms_unavailable", func(t *testing.T) {
		r := newNoKMSRegistry(t)
		_, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("service_error", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			setErr:         errors.New(errors.ErrForbidden, "SetKeyRotationPolicy", "denied"),
		})
		_, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("success_with_policy", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			setResp: &service.SetKeyRotationPolicyResponse{
				KeyID: "k1",
				Policy: &service.RotationPolicy{
					Enabled:        true,
					Interval:       time.Hour,
					NextRotation:   now,
					RetainVersions: 3,
					AutoRotate:     true,
				},
				Timestamp: now,
			},
		})
		res, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{
			"key_id":           "k1",
			"interval_seconds": float64(3600),
			"auto_rotate":      true,
			"retain_versions":  float64(3),
			"enabled":          true,
		})
		require.NoError(t, err)
		out := decodeMarshalled(t, res)
		assert.Equal(t, "k1", out["key_id"])
		policy := out["policy"].(map[string]interface{})
		assert.Equal(t, true, policy["enabled"])
		assert.Equal(t, float64(3600), policy["interval_seconds"])
		assert.Equal(t, float64(3), policy["retain_versions"])
	})

	t.Run("success_nil_policy", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			setResp:        &service.SetKeyRotationPolicyResponse{KeyID: "k1", Policy: nil, Timestamp: now},
		})
		res, err := r.executeKMSSetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.NoError(t, err)
		out := decodeMarshalled(t, res)
		assert.Equal(t, "k1", out["key_id"])
		assert.Nil(t, out["policy"])
	})
}

// =============================================================================
// executeKMSGetRotationPolicy
// =============================================================================

func TestExecuteKMSGetRotationPolicy(t *testing.T) {
	now := time.Now()

	t.Run("missing_key_id", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{testKMSService: &testKMSService{}})
		_, err := r.executeKMSGetRotationPolicy(context.Background(), map[string]interface{}{})
		require.Error(t, err)
	})

	t.Run("kms_unavailable", func(t *testing.T) {
		r := newNoKMSRegistry(t)
		_, err := r.executeKMSGetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("service_error", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			getErr:         errors.New(errors.ErrNotFound, "GetKeyRotationPolicy", "missing"),
		})
		_, err := r.executeKMSGetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("success_with_policy", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			getResp: &service.GetKeyRotationPolicyResponse{
				KeyID: "k1",
				Policy: &service.RotationPolicy{
					Enabled:        true,
					Interval:       2 * time.Hour,
					NextRotation:   now,
					RetainVersions: 5,
					AutoRotate:     false,
				},
				Timestamp: now,
			},
		})
		res, err := r.executeKMSGetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.NoError(t, err)
		out := decodeMarshalled(t, res)
		policy := out["policy"].(map[string]interface{})
		assert.Equal(t, float64(7200), policy["interval_seconds"])
		assert.Equal(t, float64(5), policy["retain_versions"])
	})

	t.Run("success_nil_policy", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			getResp:        &service.GetKeyRotationPolicyResponse{KeyID: "k1", Policy: nil, Timestamp: now},
		})
		res, err := r.executeKMSGetRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.NoError(t, err)
		out := decodeMarshalled(t, res)
		assert.Nil(t, out["policy"])
	})
}

// =============================================================================
// executeKMSDeleteRotationPolicy
// =============================================================================

func TestExecuteKMSDeleteRotationPolicy(t *testing.T) {
	now := time.Now()

	t.Run("missing_key_id", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{testKMSService: &testKMSService{}})
		_, err := r.executeKMSDeleteRotationPolicy(context.Background(), map[string]interface{}{})
		require.Error(t, err)
	})

	t.Run("kms_unavailable", func(t *testing.T) {
		r := newNoKMSRegistry(t)
		_, err := r.executeKMSDeleteRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("service_error", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			deleteErr:      errors.New(errors.ErrInternal, "DeleteKeyRotationPolicy", "boom"),
		})
		_, err := r.executeKMSDeleteRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		r := newRotationRegistry(t, &rotationKMSService{
			testKMSService: &testKMSService{},
			deleteResp:     &service.DeleteKeyRotationPolicyResponse{KeyID: "k1", Timestamp: now},
		})
		res, err := r.executeKMSDeleteRotationPolicy(context.Background(), map[string]interface{}{"key_id": "k1"})
		require.NoError(t, err)
		out := decodeMarshalled(t, res)
		assert.Equal(t, "k1", out["key_id"])
		assert.Equal(t, float64(now.Unix()), out["timestamp"])
	})
}

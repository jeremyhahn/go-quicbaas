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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// DeleteKeyPolicy
// =============================================================================

func TestKMSServer_DeleteKeyPolicy(t *testing.T) {
	log := newTestLogger()
	now := time.Now()

	t.Run("missing_key_id_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.DeleteKeyPolicy(context.Background(), &pb.DeleteKeyPolicyRequest{})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_service_unimplemented", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)
		_, err := server.DeleteKeyPolicy(context.Background(), &pb.DeleteKeyPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			DeleteKeyPolicyFunc: func(ctx context.Context, req *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "DeleteKeyPolicy", "no policy")
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		_, err := server.DeleteKeyPolicy(context.Background(), &pb.DeleteKeyPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("success", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			DeleteKeyPolicyFunc: func(ctx context.Context, req *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
				assert.Equal(t, "k1", req.KeyID)
				return &service.DeleteKeyPolicyResponse{KeyID: "k1", Timestamp: now}, nil
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		resp, err := server.DeleteKeyPolicy(context.Background(), &pb.DeleteKeyPolicyRequest{KeyId: "k1"})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyId)
		assert.Equal(t, now.Unix(), resp.Timestamp)
	})
}

// =============================================================================
// SetKeyRotationPolicy
// =============================================================================

func TestKMSServer_SetKeyRotationPolicy(t *testing.T) {
	log := newTestLogger()
	now := time.Now()

	t.Run("missing_key_id_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.SetKeyRotationPolicy(context.Background(), &pb.SetKeyRotationPolicyRequest{
			Policy: &pb.RotationPolicy{Enabled: true},
		})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_policy_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.SetKeyRotationPolicy(context.Background(), &pb.SetKeyRotationPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_service_unimplemented", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)
		_, err := server.SetKeyRotationPolicy(context.Background(), &pb.SetKeyRotationPolicyRequest{
			KeyId:  "k1",
			Policy: &pb.RotationPolicy{Enabled: true},
		})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			SetKeyRotationPolicyFunc: func(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrForbidden, "SetKeyRotationPolicy", "denied")
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		_, err := server.SetKeyRotationPolicy(context.Background(), &pb.SetKeyRotationPolicyRequest{
			KeyId:  "k1",
			Policy: &pb.RotationPolicy{Enabled: true},
		})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("success_converts_policy_both_directions", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			SetKeyRotationPolicyFunc: func(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
				require.NotNil(t, req.Policy)
				assert.True(t, req.Policy.Enabled)
				assert.Equal(t, time.Hour, req.Policy.Interval)
				assert.Equal(t, 3, req.Policy.RetainVersions)
				return &service.SetKeyRotationPolicyResponse{
					KeyID:     req.KeyID,
					Policy:    req.Policy,
					Timestamp: now,
				}, nil
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		resp, err := server.SetKeyRotationPolicy(context.Background(), &pb.SetKeyRotationPolicyRequest{
			KeyId: "k1",
			Policy: &pb.RotationPolicy{
				Enabled:         true,
				IntervalSeconds: 3600,
				RetainVersions:  3,
				AutoRotate:      true,
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyId)
		require.NotNil(t, resp.Policy)
		assert.True(t, resp.Policy.Enabled)
		assert.Equal(t, int64(3600), resp.Policy.IntervalSeconds)
		assert.Equal(t, now.Unix(), resp.Timestamp)
	})
}

// =============================================================================
// GetKeyRotationPolicy
// =============================================================================

func TestKMSServer_GetKeyRotationPolicy(t *testing.T) {
	log := newTestLogger()
	now := time.Now()

	t.Run("missing_key_id_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.GetKeyRotationPolicy(context.Background(), &pb.GetKeyRotationPolicyRequest{})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_service_unimplemented", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)
		_, err := server.GetKeyRotationPolicy(context.Background(), &pb.GetKeyRotationPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			GetKeyRotationPolicyFunc: func(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetKeyRotationPolicy", "missing")
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		_, err := server.GetKeyRotationPolicy(context.Background(), &pb.GetKeyRotationPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("success", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			GetKeyRotationPolicyFunc: func(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
				return &service.GetKeyRotationPolicyResponse{
					KeyID: "k1",
					Policy: &service.RotationPolicy{
						Enabled:        true,
						Interval:       2 * time.Hour,
						RetainVersions: 5,
						AutoRotate:     true,
					},
					Timestamp: now,
				}, nil
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		resp, err := server.GetKeyRotationPolicy(context.Background(), &pb.GetKeyRotationPolicyRequest{KeyId: "k1"})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyId)
		require.NotNil(t, resp.Policy)
		assert.Equal(t, int64(7200), resp.Policy.IntervalSeconds)
		assert.Equal(t, int32(5), resp.Policy.RetainVersions)
	})
}

// =============================================================================
// DeleteKeyRotationPolicy
// =============================================================================

func TestKMSServer_DeleteKeyRotationPolicy(t *testing.T) {
	log := newTestLogger()
	now := time.Now()

	t.Run("missing_key_id_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.DeleteKeyRotationPolicy(context.Background(), &pb.DeleteKeyRotationPolicyRequest{})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_service_unimplemented", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)
		_, err := server.DeleteKeyRotationPolicy(context.Background(), &pb.DeleteKeyRotationPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			DeleteKeyRotationPolicyFunc: func(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrInternal, "DeleteKeyRotationPolicy", "boom")
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		_, err := server.DeleteKeyRotationPolicy(context.Background(), &pb.DeleteKeyRotationPolicyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, st.Code())
	})

	t.Run("success", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			DeleteKeyRotationPolicyFunc: func(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
				assert.Equal(t, "k1", req.KeyID)
				return &service.DeleteKeyRotationPolicyResponse{KeyID: "k1", Timestamp: now}, nil
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		resp, err := server.DeleteKeyRotationPolicy(context.Background(), &pb.DeleteKeyRotationPolicyRequest{KeyId: "k1"})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyId)
		assert.Equal(t, now.Unix(), resp.Timestamp)
	})
}

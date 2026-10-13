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
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// TestKMSHandlers_ResponseOutOfRange proves that key metadata the server
// cannot represent on the wire, such as a negative version or a retention
// count beyond int32, becomes codes.Internal instead of a wrapped integer.
func TestKMSHandlers_ResponseOutOfRange(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	negativeVersion := func() *kms.KeyMetadata {
		meta := newMockKeyMetadata("k1", now)
		meta.Version = -1
		return meta
	}
	hugeRetention := func() *kms.KeyMetadata {
		meta := newMockKeyMetadata("k1", now)
		meta.RotationPolicy = &kms.RotationPolicy{RetainVersions: math.MaxInt32 + 1}
		return meta
	}
	getKey := func(meta func() *kms.KeyMetadata) *mockKMSManager {
		return &mockKMSManager{
			getKeyFunc: func(context.Context, string) (interface{}, error) { return meta(), nil },
		}
	}
	hugeServicePolicy := &service.RotationPolicy{RetainVersions: math.MaxInt32 + 1}

	tests := []struct {
		name  string
		field string
		mgr   *mockKMSManager
		svc   *mocks.MockKMSService
		call  func(srv *KMSServer) error
	}{
		{
			name:  "GetKey negative version",
			field: "version",
			mgr:   getKey(negativeVersion),
			call: func(srv *KMSServer) error {
				_, err := srv.GetKey(ctx, &pb.GetKeyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "GetKey retain versions",
			field: "retain_versions",
			mgr:   getKey(hugeRetention),
			call: func(srv *KMSServer) error {
				_, err := srv.GetKey(ctx, &pb.GetKeyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "GetKeyMetadata negative version",
			field: "version",
			mgr:   getKey(negativeVersion),
			call: func(srv *KMSServer) error {
				_, err := srv.GetKeyMetadata(ctx, &pb.GetKeyMetadataRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "ListKeyVersions negative version",
			field: "version",
			mgr:   getKey(negativeVersion),
			call: func(srv *KMSServer) error {
				_, err := srv.ListKeyVersions(ctx, &pb.ListKeyVersionsRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "RotateKey negative old version",
			field: "old_version",
			mgr:   getKey(negativeVersion),
			call: func(srv *KMSServer) error {
				_, err := srv.RotateKey(ctx, &pb.RotateKeyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "RotateKey negative new version",
			field: "new_version",
			mgr: &mockKMSManager{
				getKeyFunc: func(context.Context, string) (interface{}, error) {
					return newMockKeyMetadata("k1", now), nil
				},
				rotateKeyFunc: func(context.Context, string) (interface{}, error) { return negativeVersion(), nil },
			},
			call: func(srv *KMSServer) error {
				_, err := srv.RotateKey(ctx, &pb.RotateKeyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "ListKeys fallback negative version",
			field: "version",
			mgr: &mockKMSManager{
				listKeysFunc: func(context.Context, string) ([]interface{}, error) {
					return []interface{}{negativeVersion()}, nil
				},
			},
			call: func(srv *KMSServer) error {
				_, err := srv.ListKeys(ctx, &pb.ListKeysRequest{})
				return err
			},
		},
		{
			name:  "ListKeys service total count",
			field: "total_count",
			svc: &mocks.MockKMSService{
				ListKeysFunc: func(context.Context, *service.ListKeysRequest) (*service.ListKeysResponse, error) {
					return &service.ListKeysResponse{TotalCount: math.MaxInt32 + 1, Timestamp: now}, nil
				},
			},
			call: func(srv *KMSServer) error {
				_, err := srv.ListKeys(ctx, &pb.ListKeysRequest{})
				return err
			},
		},
		{
			name:  "GetKey service retain versions",
			field: "retain_versions",
			svc: &mocks.MockKMSService{
				GetKeyFunc: func(context.Context, *service.GetKeyRequest) (*service.GetKeyResponse, error) {
					return &service.GetKeyResponse{
						Key:       &service.KeyHandle{KeyID: "k1"},
						Metadata:  &service.KeyMetadata{KeyID: "k1", RotationPolicy: hugeServicePolicy},
						Timestamp: now,
					}, nil
				},
			},
			call: func(srv *KMSServer) error {
				_, err := srv.GetKey(ctx, &pb.GetKeyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "GetKeyRotationPolicy retain versions",
			field: "retain_versions",
			svc: &mocks.MockKMSService{
				GetKeyRotationPolicyFunc: func(context.Context, *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
					return &service.GetKeyRotationPolicyResponse{KeyID: "k1", Policy: hugeServicePolicy, Timestamp: now}, nil
				},
			},
			call: func(srv *KMSServer) error {
				_, err := srv.GetKeyRotationPolicy(ctx, &pb.GetKeyRotationPolicyRequest{KeyId: "k1"})
				return err
			},
		},
		{
			name:  "SetKeyRotationPolicy retain versions",
			field: "retain_versions",
			svc: &mocks.MockKMSService{
				SetKeyRotationPolicyFunc: func(context.Context, *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
					return &service.SetKeyRotationPolicyResponse{KeyID: "k1", Policy: hugeServicePolicy, Timestamp: now}, nil
				},
			},
			call: func(srv *KMSServer) error {
				_, err := srv.SetKeyRotationPolicy(ctx, &pb.SetKeyRotationPolicyRequest{
					KeyId:  "k1",
					Policy: &pb.RotationPolicy{Enabled: true, IntervalSeconds: 3600},
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := tt.mgr
			if mgr == nil {
				mgr = &mockKMSManager{}
			}
			srv := NewKMSServer(mgr, newTestLogger())
			if tt.svc != nil {
				srv = NewKMSServerWithService(mgr, tt.svc, newTestLogger())
			}
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}

// TestClientKMSSetKeyRotationPolicy_RetainVersionsOutOfRange proves that a
// caller's retention count beyond int32 is rejected with the typed conv error
// before the client touches the connection.
func TestClientKMSSetKeyRotationPolicy_RetainVersionsOutOfRange(t *testing.T) {
	client := &Client{}

	resp, err := client.KMSSetKeyRotationPolicy(context.Background(), &service.SetKeyRotationPolicyRequest{
		KeyID:  "k1",
		Policy: &service.RotationPolicy{RetainVersions: math.MaxInt32 + 1},
	})

	var overflow *conv.ConversionOverflowError
	require.ErrorAs(t, err, &overflow)
	assert.Nil(t, resp)
}

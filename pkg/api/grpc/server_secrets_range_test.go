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

	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestSecretsHandlers_ResponseOutOfRange proves that a secret count or
// retention setting too large for its int32 wire field becomes codes.Internal
// instead of a wrapped integer.
func TestSecretsHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	hugePolicy := &service.SecretRotationPolicy{RetainVersions: tooBig}

	tests := []struct {
		name  string
		field string
		svc   *mockSecretsService
		call  func(srv *SecretsServer) error
	}{
		{
			name:  "ListSecrets total count",
			field: "total_count",
			svc: &mockSecretsService{
				listSecretsFunc: func(context.Context, *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
					return &service.ListSecretsResponse{TotalCount: tooBig}, nil
				},
			},
			call: func(srv *SecretsServer) error {
				_, err := srv.ListSecrets(ctx, &pb.ListSecretsRequest{})
				return err
			},
		},
		{
			name:  "ListSecrets retain versions",
			field: "retain_versions",
			svc: &mockSecretsService{
				listSecretsFunc: func(context.Context, *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
					return &service.ListSecretsResponse{
						Secrets: []*service.Secret{{SecretID: "s1", RotationPolicy: hugePolicy}},
					}, nil
				},
			},
			call: func(srv *SecretsServer) error {
				_, err := srv.ListSecrets(ctx, &pb.ListSecretsRequest{})
				return err
			},
		},
		{
			name:  "GetSecretsByPrefix total count",
			field: "total_count",
			svc: &mockSecretsService{
				getSecretsByPrefixFunc: func(context.Context, *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
					return &service.GetSecretsByPrefixResponse{TotalCount: tooBig}, nil
				},
			},
			call: func(srv *SecretsServer) error {
				_, err := srv.GetSecretsByPrefix(ctx, &pb.GetSecretsByPrefixRequest{Prefix: "app/"})
				return err
			},
		},
		{
			name:  "SetRotationPolicy retain versions",
			field: "retain_versions",
			svc: &mockSecretsService{
				setRotationPolicyFunc: func(context.Context, *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
					return &service.SetRotationPolicyResponse{SecretID: "s1", Policy: hugePolicy}, nil
				},
			},
			call: func(srv *SecretsServer) error {
				_, err := srv.SetRotationPolicy(ctx, &pb.SetRotationPolicyRequest{
					SecretId: "s1",
					Policy:   &pb.SecretRotationPolicy{Enabled: true},
				})
				return err
			},
		},
		{
			name:  "GetRotationPolicy retain versions",
			field: "retain_versions",
			svc: &mockSecretsService{
				getRotationPolicyFunc: func(context.Context, *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
					return &service.GetRotationPolicyResponse{SecretID: "s1", Policy: hugePolicy}, nil
				},
			},
			call: func(srv *SecretsServer) error {
				_, err := srv.GetRotationPolicy(ctx, &pb.GetRotationPolicyRequest{SecretId: "s1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestSecretsServerForErrors(t, tt.svc)
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}

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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret is the gRPC entry point for the Vault-style
// generate-on-first-access primitive. If a secret at the given path already
// exists it is returned unchanged (generated=false). If it does not exist a
// fresh random value is generated, stored, and returned (generated=true).
// Exactly one concurrent caller in a cluster-wide race receives generated=true.
//
// The RPC is defined in api/proto/kms.proto (SecretsManagement.EnsureSecret)
// and the service-layer implementation lives in
// pkg/service/secrets.go (secretsService.EnsureSecret).
func (s *SecretsServer) EnsureSecret(ctx context.Context, req *pb.EnsureSecretRequest) (*pb.EnsureSecretResponse, error) {
	s.logger.Debug("Processing EnsureSecret request")

	if req == nil || req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "EnsureSecret: secret_id is required")
	}

	svcReq := &service.EnsureSecretRequest{
		SecretID: req.SecretId,
		Length:   int(req.Length),
		Encoding: req.Encoding,
		TenantID: req.TenantId,
	}

	resp, err := s.secretsSvc.EnsureSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.EnsureSecretResponse{
		SecretId:  resp.SecretID,
		Data:      resp.Data,
		Generated: resp.Generated,
		Version:   resp.Version,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

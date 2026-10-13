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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret is the gRPC client for the Vault-style generate-on-first-access
// primitive: it returns the secret if it exists, otherwise generates and stores
// it, returning the value. Mirrors the field mapping in
// server_secrets_ensure.go (the inverse direction).
func (c *Client) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if req == nil || req.SecretID == "" {
		return nil, status.Error(codes.InvalidArgument, "EnsureSecret: secret_id is required")
	}

	length, err := clientInt32("length", req.Length)
	if err != nil {
		return nil, err
	}

	secretsClient := pb.NewSecretsManagementClient(c.conn)

	pbResp, err := secretsClient.EnsureSecret(ctx, &pb.EnsureSecretRequest{
		SecretId: req.SecretID,
		Length:   length,
		Encoding: req.Encoding,
		TenantId: req.TenantID,
	})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.EnsureSecretResponse{
		SecretID:  pbResp.SecretId,
		Data:      pbResp.Data,
		Generated: pbResp.Generated,
		Version:   pbResp.Version,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

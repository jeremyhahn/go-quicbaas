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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret is the gRPC client stub for the Vault-style
// generate-on-first-access primitive. It currently returns a clear
// Unimplemented error pointing at the proto regeneration TODO in
// server_secrets_ensure.go. Once the proto is regenerated the body
// should follow the pattern in client_secrets.go:StoreSecret:
//
//  1. secretsClient := pb.NewSecretsManagementClient(c.conn)
//  2. pbReq := &pb.EnsureSecretRequest{ ... }
//  3. pbResp, err := secretsClient.EnsureSecret(ctx, pbReq)
//  4. return &service.EnsureSecretResponse{ ... }, nil
func (c *Client) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if req == nil || req.SecretID == "" {
		return nil, status.Error(codes.InvalidArgument, "EnsureSecret: secret_id is required")
	}
	return nil, status.Error(codes.Unimplemented,
		"grpc.Client.EnsureSecret: proto regeneration pending — see TODO in pkg/api/grpc/server_secrets_ensure.go")
}

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

// EnsureSecret is the gRPC entry point for the Vault-style
// generate-on-first-access primitive. The fully-wired service-layer
// logic already lives in service.SecretsService.EnsureSecret; what is
// intentionally NOT yet wired is the protobuf schema addition + stub
// regeneration. This method returns codes.Unimplemented until the
// proto is regenerated — see the TODO block below for the exact
// steps.
//
// TODO(gRPC proto regeneration) — blocking full wire support:
//
//  1. Add the following to api/proto/quicbaas.proto inside the
//     SecretsManagement service definition:
//
//     rpc EnsureSecret(EnsureSecretRequest) returns (EnsureSecretResponse);
//
//     message EnsureSecretRequest {
//     string secret_id  = 1;
//     int32  length     = 2;
//     string encoding   = 3;
//     string tenant_id  = 4;
//     }
//
//     message EnsureSecretResponse {
//     string secret_id = 1;
//     bytes  data      = 2;
//     bool   generated = 3;
//     uint64 version   = 4;
//     int64  timestamp = 5;
//     }
//
//  2. Regenerate with:
//     buf generate
//     (or: protoc --go_out=. --go-grpc_out=. api/proto/quicbaas.proto)
//
//  3. Replace the body below with the real wiring pattern used by
//     SecretsServer.StoreSecret (see server_secrets.go:44). It should
//     delegate to s.secretsSvc.EnsureSecret(ctx, svcReq).
//
// Until step 1-3 are done, gRPC clients receive a clear Unimplemented
// response that points them at this TODO.
func (s *SecretsServer) EnsureSecret(ctx context.Context, _ *struct{}) (*struct{}, error) {
	_ = service.EnsureSecretRequest{} // anchor the service-layer type so refactors surface here
	s.logger.Warn("EnsureSecret called but gRPC proto stubs are not regenerated; returning Unimplemented")
	return nil, status.Error(codes.Unimplemented,
		"EnsureSecret: proto regeneration pending — see TODO in pkg/api/grpc/server_secrets_ensure.go")
}

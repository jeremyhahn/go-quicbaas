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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret shim: the pre-existing mockSecretsService and
// mockSecretsServiceWithPrefix types in this package predate the
// addition of SecretsService.EnsureSecret to the interface. To keep
// the test binary compiling without invasively rewriting each mock
// file, we bolt on a no-op EnsureSecret here.
//
// The new Ensure-specific tests do NOT depend on these mocks — they
// operate on the gRPC Client stub directly (see
// client_secrets_ensure_test.go).

func (m *mockSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return &service.EnsureSecretResponse{SecretID: req.SecretID}, nil
}

func (m *mockSecretsServiceWithPrefix) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return &service.EnsureSecretResponse{SecretID: req.SecretID}, nil
}

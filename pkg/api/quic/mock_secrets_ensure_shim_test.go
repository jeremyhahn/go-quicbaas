//go:build quic

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

package quic

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret shims for pre-existing test mocks that predate the
// service.SecretsService.EnsureSecret interface addition. The new
// Ensure-specific tests do not depend on these shims — they operate
// on the QUIC Client stub directly (client_secrets_ensure_test.go).

func (s *e2eSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return &service.EnsureSecretResponse{SecretID: req.SecretID}, nil
}

func (s *securityTestSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return &service.EnsureSecretResponse{SecretID: req.SecretID}, nil
}

func (s *errorSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	return nil, context.Canceled
}

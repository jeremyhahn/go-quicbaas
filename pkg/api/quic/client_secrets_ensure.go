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
	"errors"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrEnsureSecretUnsupportedQUIC indicates the QUIC transport does not
// yet carry the EnsureSecret RPC because the single-byte opcode space
// in pkg/api/quic/protocol.go is fully allocated. Adding a new RPC
// requires one of:
//
//  1. Widening the wire format to a 2-byte opcode (invasive, touches
//     every handler and client — refused here per the implementation
//     rules).
//
//  2. Introducing a sub-dispatch opcode byte in the Secrets namespace
//     (mirroring the IAM sub-dispatch pattern in protocol_iam.go +
//     handler.go:1028) and routing EnsureSecret through a new
//     SecretsSubDispatch opcode. This is the recommended approach.
//
//  3. Repurposing an existing sparsely-used opcode (not recommended —
//     breaks backward compatibility for older clients).
//
// TODO(QUIC EnsureSecret wiring): follow approach (2). Steps:
//
//   - Add OpSecretsSubDispatch byte = 0x??  in protocol.go, documenting
//     the sub-opcode layout (payload[0] = sub-op, payload[1:] = encoded
//     request).
//   - Add OpSecretsSubEnsure byte = 0x01 as the first sub-op.
//   - In handler.go Dispatch, case OpSecretsSubDispatch: route to a new
//     handleSecretsSubDispatch(ctx, req) that reads payload[0] and maps
//     to the concrete sub-handler (e.g. handleSecretsEnsure).
//   - Create handler_secrets_ensure.go with the handleSecretsEnsure
//     body — decode service.EnsureSecretRequest, pre-validate spec,
//     call h.secretsService.EnsureSecret, encode response.
//   - Replace the body of Client.EnsureSecret below with a sendRequest
//     call that wraps the payload as [OpSecretsSubEnsure | encoded-req].
//
// Until that landing, callers receive a typed ErrEnsureSecretUnsupportedQUIC
// error so go-trusted-platform's multi-protocol adapter test suite can
// detect and skip the QUIC leg explicitly (no silent fallback).
var ErrEnsureSecretUnsupportedQUIC = errors.New("quic: EnsureSecret not yet wired — opcode space requires sub-dispatch (see TODO in client_secrets_ensure.go)")

// EnsureSecret returns ErrEnsureSecretUnsupportedQUIC until the
// sub-dispatch wiring described above lands.
func (c *Client) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.EnsureSecret", "request")
	}
	if req.SecretID == "" {
		return nil, api.NewInvalidInputError("Client.EnsureSecret: secret_id is required")
	}
	return nil, ErrEnsureSecretUnsupportedQUIC
}

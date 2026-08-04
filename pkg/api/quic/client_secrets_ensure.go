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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret implements the Vault-style generate-on-first-access primitive
// over QUIC using the OpSecretsSubDispatch (0x6F) / OpSecretsSubEnsure (0x01)
// sub-dispatch wire format introduced in protocol.go.
//
// Wire layout sent by this method:
//
//	opcode  = OpSecretsSubDispatch (0x6F)
//	payload = [OpSecretsSubEnsure (0x01)] | [JSON-encoded EnsureSecretRequest]
//
// The server-side handler_secrets_ensure.go decodes payload[0] as the
// sub-opcode, then decodes payload[1:] as service.EnsureSecretRequest and
// delegates to secretsService.EnsureSecret.
func (c *Client) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("Client.EnsureSecret", "request")
	}
	if req.SecretID == "" {
		return nil, api.NewInvalidInputError("Client.EnsureSecret: secret_id is required")
	}

	bodyBytes, err := EncodePayload(req)
	if err != nil {
		return nil, api.NewEncodeError("Client.EnsureSecret", "request", err)
	}

	// Prepend sub-opcode: payload = [subOp | body]
	payload := make([]byte, 1+len(bodyBytes))
	payload[0] = OpSecretsSubEnsure
	copy(payload[1:], bodyBytes)

	resp, err := c.sendRequest(ctx, OpSecretsSubDispatch, payload)
	if err != nil {
		return nil, err
	}

	var result service.EnsureSecretResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, api.NewDecodeError("Client.EnsureSecret", "response", err)
	}

	return &result, nil
}

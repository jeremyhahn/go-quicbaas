//go:build mcp

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

package mcp

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerSecretsEnsureTool installs the quicbaas_secrets_ensure tool.
// It is invoked from registerSecretsTools() via init-time wiring below.
func (r *ToolRegistry) registerSecretsEnsureTool() {
	r.addTool(ToolDefinition{
		Name: "quicbaas_secrets_ensure",
		Description: "Vault-style generate-on-first-access: generate a random secret " +
			"on first call, return the same stable value on subsequent calls. " +
			"Spec mismatch against an existing secret is a hard error.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Secret path / identifier",
				},
				"length": map[string]interface{}{
					"type":        "integer",
					"description": "Entropy length: bytes for base64/hex/raw, digits for numeric",
				},
				"encoding": map[string]interface{}{
					"type":        "string",
					"description": "Output encoding: base64 | hex | numeric | raw",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional tenant scope for tenant-keyed DEK selection",
				},
			},
			"required": []string{"path", "length", "encoding"},
		},
	})
}

// executeSecretsEnsure handles the quicbaas_secrets_ensure MCP tool
// call. It pre-validates the spec and then delegates to the
// service-layer EnsureSecret, preserving typed errors.
func (r *ToolRegistry) executeSecretsEnsure(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	secretsSvc := r.service.Secrets()
	if secretsSvc == nil {
		return nil, api.NewServiceUnavailableError("secrets")
	}

	path := getString(args, "path")
	if path == "" {
		return nil, api.NewInvalidInputError("path is required")
	}
	length := getInt(args, "length")
	encoding := getString(args, "encoding")

	spec := &secrets.SecretSpec{Length: length, Encoding: encoding}
	if err := spec.Validate(); err != nil {
		return nil, api.NewInvalidInputError(err.Error())
	}

	req := &service.EnsureSecretRequest{
		SecretID: path,
		Length:   length,
		Encoding: encoding,
		TenantID: getString(args, "tenant_id"),
	}

	resp, err := secretsSvc.EnsureSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

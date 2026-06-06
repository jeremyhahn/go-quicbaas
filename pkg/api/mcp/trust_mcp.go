//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package mcp implements Trust tool execution handlers for the Model
// Context Protocol. The handlers delegate to the service-layer
// TrustService, which composes the xkms TrustedCA. MCP rides the REST
// server surface; these handlers are the in-process equivalent.
package mcp

import (
	"context"
	"encoding/pem"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerTrustTools registers all Trust tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerTrustTools() {
	for _, tool := range getAllTrustTools() {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeTrustTool executes a Trust tool and returns the result.
func (r *ToolRegistry) executeTrustTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	trustSvc := r.service.Trust()
	if trustSvc == nil {
		return nil, api.NewServiceNotConfiguredError("trust")
	}

	switch name {
	case "quicbaas_trust_cross_signed_root_pool":
		return r.handleTrustCrossSignedRootPool(ctx, trustSvc)
	case "quicbaas_trust_auto_import_aia":
		return r.handleTrustAutoImportAIA(ctx, trustSvc, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleTrustCrossSignedRootPool handles quicbaas_trust_cross_signed_root_pool.
func (r *ToolRegistry) handleTrustCrossSignedRootPool(ctx context.Context, svc service.TrustService) (interface{}, error) {
	resp, err := svc.CrossSignedRootPool(ctx, &service.CrossSignedRootPoolRequest{})
	if err != nil {
		return nil, api.NewOperationFailedError("cross-signed root pool", err)
	}

	roots := make([]string, 0, len(resp.RootPEMs))
	for _, p := range resp.RootPEMs {
		roots = append(roots, string(p))
	}
	return map[string]interface{}{
		"root_pems": roots,
		"count":     len(roots),
	}, nil
}

// handleTrustAutoImportAIA handles quicbaas_trust_auto_import_aia. The
// chain_pem argument is a PEM bundle; each CERTIFICATE block is decoded to
// DER and forwarded to the service for AIA walking and import.
func (r *ToolRegistry) handleTrustAutoImportAIA(ctx context.Context, svc service.TrustService, args map[string]interface{}) (interface{}, error) {
	chainPEM := getString(args, "chain_pem")
	if chainPEM == "" {
		return nil, api.NewRequiredFieldError("chain_pem")
	}

	chainDER := decodePEMChainToDER([]byte(chainPEM))
	if len(chainDER) == 0 {
		return nil, api.NewRequiredFieldError("chain_pem")
	}

	resp, err := svc.AutoImportAIA(ctx, &service.AutoImportAIARequest{ChainDER: chainDER})
	if err != nil {
		return nil, api.NewOperationFailedError("auto import aia", err)
	}

	return map[string]interface{}{
		"imported": resp.Imported,
	}, nil
}

// decodePEMChainToDER extracts the DER bytes of every CERTIFICATE block in
// a PEM bundle, preserving order.
func decodePEMChainToDER(pemBytes []byte) [][]byte {
	var out [][]byte
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		der := make([]byte, len(block.Bytes))
		copy(der, block.Bytes)
		out = append(out, der)
	}
	return out
}

// isTrustTool returns true if the tool name is a Trust tool.
func isTrustTool(name string) bool {
	switch name {
	case "quicbaas_trust_cross_signed_root_pool",
		"quicbaas_trust_auto_import_aia":
		return true
	default:
		return false
	}
}

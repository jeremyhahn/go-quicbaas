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

// trust_handler.go adapts the Trust client's verbs to the server's
// quicbaas_trust_* MCP tools: AutoImportAIA sends its DER chain as the one
// PEM bundle the tool takes, PublishDANE sends its certificate as standard
// base64, and each result decodes into the client/trust type the SDK client
// type-asserts on.

package mcp

import (
	"encoding/base64"
	"encoding/pem"
	"strconv"

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPTrustService reports whether the service name targets Trust (the
// trust-anchor verbs), not TrustedCA.
func isMCPTrustService(service string) bool { return trimServiceName(service) == "Trust" }

// trustVerbs map the Trust verbs onto the server's quicbaas_trust_* tools.
// Keep in sync with pkg/api/mcp/trust_mcp.go.
var trustVerbs = map[string]toolVerb{
	"CrossSignedRootPool": {
		tool:   "quicbaas_trust_cross_signed_root_pool",
		args:   trustCrossSignedRootPoolArgs,
		decode: decodeTrustCrossSignedRootPool,
	},
	"AutoImportAIA": {
		tool:   "quicbaas_trust_auto_import_aia",
		args:   trustAutoImportAIAArgs,
		decode: decodeTrustAutoImportAIA,
	},
	"PublishDANE": {
		tool:   "quicbaas_trust_publish_dane",
		args:   trustPublishDANEArgs,
		decode: decodeTrustPublishDANE,
	},
}

func trustCrossSignedRootPoolArgs(req interface{}) (map[string]interface{}, error) {
	if req != nil {
		if _, ok := req.(*trustclient.CrossSignedRootPoolRequest); !ok {
			return nil, errors.NewInvalidInput("mcp.trust.CrossSignedRootPool",
				"invalid request type for CrossSignedRootPool")
		}
	}
	return map[string]interface{}{}, nil
}

func decodeTrustCrossSignedRootPool(m map[string]interface{}) interface{} {
	raw, _ := m["root_pems"].([]interface{})
	roots := make([][]byte, 0, len(raw))
	for _, entry := range raw {
		if s, ok := entry.(string); ok && s != "" {
			roots = append(roots, []byte(s))
		}
	}
	return &trustclient.CrossSignedRootPoolResponse{RootPEMs: roots}
}

func trustAutoImportAIAArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*trustclient.AutoImportAIARequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.trust.AutoImportAIA",
			"invalid request type for AutoImportAIA")
	}
	if len(r.ChainDER) == 0 {
		return nil, errors.NewInvalidInput("mcp.trust.AutoImportAIA",
			"chain_der is required")
	}
	var bundle []byte
	for i, der := range r.ChainDER {
		if len(der) == 0 {
			return nil, errors.NewInvalidInput("mcp.trust.AutoImportAIA",
				"chain_der entry is empty at index "+strconv.Itoa(i))
		}
		bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return map[string]interface{}{"chain_pem": string(bundle)}, nil
}

func decodeTrustAutoImportAIA(m map[string]interface{}) interface{} {
	return &trustclient.AutoImportAIAResponse{Imported: int(getResultInt64(m, "imported"))}
}

func trustPublishDANEArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*trustclient.PublishDANERequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.trust.PublishDANE",
			"invalid request type for PublishDANE")
	}
	switch {
	case r.Name == "":
		return nil, errors.NewInvalidInput("mcp.trust.PublishDANE", "name is required")
	case r.Port <= 0:
		return nil, errors.NewInvalidInput("mcp.trust.PublishDANE", "port must be positive")
	case len(r.CertDER) == 0:
		return nil, errors.NewInvalidInput("mcp.trust.PublishDANE", "cert_der is required")
	}
	return map[string]interface{}{
		"name":     r.Name,
		"port":     r.Port,
		"cert_der": base64.StdEncoding.EncodeToString(r.CertDER),
	}, nil
}

func decodeTrustPublishDANE(m map[string]interface{}) interface{} {
	return &trustclient.PublishDANEResponse{Published: int(getResultInt64(m, "published"))}
}

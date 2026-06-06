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

// Package mcp implements Trust tool definitions for the Model Context
// Protocol. The Trust tools compose the xkms TrustedCA cross-sign / AIA
// verbs surfaced under Security().Trust(). DANE publication is
// intentionally omitted (pending a separate DNS-provider hook design).
package mcp

var (
	// TrustCrossSignedRootPoolTool returns the active CA's cross-sign-aware
	// root set in PEM form.
	TrustCrossSignedRootPoolTool = Tool{
		Name: "quicbaas_trust_cross_signed_root_pool",
		Description: "Return the active CA's cross-sign-aware root certificate set " +
			"(native root plus any cross-signed roots) as PEM-encoded certificates",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}

	// TrustAutoImportAIATool walks the AIA chain of a PEM bundle and
	// imports the referenced issuing CAs.
	TrustAutoImportAIATool = Tool{
		Name: "quicbaas_trust_auto_import_aia",
		Description: "Walk the AuthorityInformationAccess (AIA) extension of each " +
			"certificate in the supplied PEM bundle and import the referenced " +
			"issuing CA certificates into the trust store",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"chain_pem": {
					Type:        "string",
					Description: "PEM bundle of one or more certificates whose AIA chains are walked and imported",
				},
			},
			Required: []string{"chain_pem"},
		},
	}
)

// getAllTrustTools returns all Trust tools (2 tools).
func getAllTrustTools() []Tool {
	return []Tool{
		TrustCrossSignedRootPoolTool,
		TrustAutoImportAIATool,
	}
}

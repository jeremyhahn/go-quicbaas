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

//go:build integration

package cli

import (
	"strconv"
	"time"
)

// =============================================================================
// Trust Commands
// =============================================================================
//
// Builders for the `quicbaas trust <verb>` tree defined in
// cmd/quicbaas/trust.go. Every verb routes through the SDK's
// Security().Trust() handle, which every network transport serves, so no
// builder excludes a transport.

// trustTimeout is the per-command timeout for trust operations.
const trustTimeout = 15 * time.Second

// TrustCrossSignedRootsCommand lists the cross-signed root pool as a table.
func TrustCrossSignedRootsCommand() CommandDef {
	return CommandDef{
		Name:           "trust cross-signed-roots",
		Args:           []string{"trust", "cross-signed-roots"},
		ExpectSuccess:  true,
		Timeout:        trustTimeout,
		RequiresUnseal: true,
	}
}

// TrustCrossSignedRootsJSONCommand lists the cross-signed root pool as JSON.
func TrustCrossSignedRootsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "trust cross-signed-roots (json)",
		Args:           []string{"trust", "--output", "json", "cross-signed-roots"},
		ExpectSuccess:  true,
		Timeout:        trustTimeout,
		RequiresUnseal: true,
	}
}

// TrustAutoImportAIACommand runs an AIA import of the PEM chain at chainPath.
func TrustAutoImportAIACommand(chainPath string) CommandDef {
	return CommandDef{
		Name:           "trust auto-import-aia",
		Args:           []string{"trust", "--output", "json", "auto-import-aia", "--chain", chainPath},
		ExpectSuccess:  true,
		Timeout:        trustTimeout,
		RequiresUnseal: true,
	}
}

// TrustPublishDANECommand publishes a TLSA record for the certificate at certPath.
func TrustPublishDANECommand(name string, port int, certPath string) CommandDef {
	return CommandDef{
		Name:           "trust publish-dane",
		Args:           []string{"trust", "publish-dane", "--name", name, "--port", strconv.Itoa(port), "--cert", certPath},
		ExpectSuccess:  false,
		Timeout:        trustTimeout,
		RequiresUnseal: true,
	}
}

// TrustAutoImportAIAMissingChainCommand omits the required --chain flag.
func TrustAutoImportAIAMissingChainCommand() CommandDef {
	return CommandDef{
		Name:          "trust auto-import-aia (missing --chain)",
		Args:          []string{"trust", "auto-import-aia"},
		ExpectSuccess: false,
		Timeout:       trustTimeout,
	}
}

// TrustPublishDANEBadPortCommand passes a --port outside 1-65535.
func TrustPublishDANEBadPortCommand(certPath string) CommandDef {
	return CommandDef{
		Name:          "trust publish-dane (bad --port)",
		Args:          []string{"trust", "publish-dane", "--name", "x.example", "--port", "70000", "--cert", certPath},
		ExpectSuccess: false,
		Timeout:       trustTimeout,
	}
}

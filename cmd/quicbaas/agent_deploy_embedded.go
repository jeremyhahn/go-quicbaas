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

//go:build desktop

package main

import (
	"sync/atomic"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// embeddedAgentCARef is an atomic pointer to the CA of whichever
// embedded quicbaasd is currently running under the desktop process.
// `installEmbeddedAgentCA` publishes the CA when the server starts;
// `uninstallEmbeddedAgentCA` clears it on Stop.
//
// The agent-deploy factory consults this pointer via the
// resolveAgentCertIssuer seam to mint daemon identities against the
// embedded CA. Invocations that happen before the embedded server is
// ready (or from a CLI process without an embedded server) get a typed
// error telling the operator where to run the command from.
var embeddedAgentCARef atomic.Pointer[xkmssdk.TrustedCA]

// init installs the embedded cert-issuer resolver so `quicbaas agent
// deploy` talks to the in-process CA when the desktop build is active.
// The CLI-only build preserves its default resolver (which returns a
// typed error when invoked).
func init() {
	resolveAgentCertIssuer = resolveEmbeddedAgentCertIssuer
}

// installEmbeddedAgentCA publishes the running server's CA into the
// process-wide seam. Called by EmbeddedServerBinding.Start after
// server.initializeCA has completed.
func installEmbeddedAgentCA(ca xkmssdk.TrustedCA) {
	embeddedAgentCARef.Store(&ca)
}

// uninstallEmbeddedAgentCA clears the process-wide seam. Called by
// EmbeddedServerBinding.Stop so stale references cannot outlive the
// server that owned them.
func uninstallEmbeddedAgentCA() {
	embeddedAgentCARef.Store(nil)
}

// resolveEmbeddedAgentCertIssuer wraps the currently-installed embedded
// CA in an XKMSAgentCertIssuer. Returns a typed error when no embedded
// server is running so the operator sees a clear diagnostic rather
// than a nil-pointer panic.
func resolveEmbeddedAgentCertIssuer() (service.AgentCertIssuer, error) {
	caPtr := embeddedAgentCARef.Load()
	if caPtr == nil || *caPtr == nil {
		return nil, &deployNoCertIssuerError{}
	}
	return service.NewXKMSAgentCertIssuer(*caPtr)
}

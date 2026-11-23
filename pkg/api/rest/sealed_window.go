//go:build rest

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

package rest

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// SetSealedWindowGate installs the sealed-window middleware at the top of the
// REST middleware chain and rebuilds the router. While probe reports sealed,
// every request whose path is not on the allowlist is rejected with 503 and
// qrdb's documented sealed envelope; allowlisted paths (barrier / ceremony /
// init / bootstrap / health) pass through so operators can drive the unseal
// flow. When the probe reports unsealed the middleware is a pass-through, so
// the gate stays installed for the daemon's whole lifetime — a post-boot
// barrier seal immediately re-narrows the surface.
//
// The middleware itself is go-qrdb's canonical REST sealed-window middleware
// (consumed via the SDK re-export qrdbsdk.SealedWindowMiddleware); quicbaas
// supplies only the allowlist, derived from the single canonical table in
// pkg/api/sealedwindow (sealedwindow.RESTAllowlist) so REST cannot drift from
// the gRPC / QUIC / MCP gates.
//
// The gate also derives the shared sealed-window authentication-bypass
// predicate (sealedwindow.AuthBypass) from the same probe. The authz
// middleware below the sealed gate consults it so that WHILE SEALED the
// allowlisted paths that pass the sealed gate are admitted WITHOUT
// authentication (audited, reason "sealed-window allowlist") — a sealed IAM
// plane cannot mint or validate tokens, so the allowlist is the entire
// control surface. Unsealed, the predicate is inert and enforcement is
// unchanged.
//
// operational is the ceremony-state probe that decides whether the
// pre-operational half of that exemption still applies (the self-authorizing
// ceremony claim handshakes and the ceremony status probe). It FAILS CLOSED:
// nil, or a probe reporting operational, withholds those exemptions so the
// claim routes require authentication on an already-initialized sealed daemon.
// The barrier / unseal surface is state-independent and is unaffected.
//
// Must be called before Start (it rebuilds the router, mirroring
// SetAuthorizationService). A nil probe leaves the gate uninstalled.
func (s *Server) SetSealedWindowGate(
	probe qrdbsdk.SealedWindowProbe,
	operational sealedwindow.OperationalProbe,
	allowlist []string,
) {
	s.sealedProbe = probe
	s.sealedAllowlist = allowlist

	if probe != nil {
		s.sealedBypass = sealedwindow.NewAuthBypass(
			probe, operational, sealedwindow.NewLoggerAuditSink(s.logger))
	} else {
		s.sealedBypass = nil
	}

	// Rebuild the router so the middleware is installed ahead of the authz
	// gates for every route group (setupRouter also propagates the bypass
	// predicate into the installed authz middleware).
	s.setupRouter()
}

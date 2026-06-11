//go:build rest

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

package rest

import (
	"github.com/go-chi/chi/v5"
)

// registerIAMRoutes registers IAM API routes and stores the IAMHandler on the
// server so lifecycle code can call SetMTLSAuthenticators after construction.
func (s *Server) registerIAMRoutes(r chi.Router) {
	h := NewIAMHandler(IAMHandlerDependencies{Server: s})
	h.RegisterRoutes(r)

	// Store so lifecycle_iam_mtls.go can wire mTLS authenticators after startup.
	s.iamHandler = h

	// Register the public agent registration endpoint at the root
	// router so it sits alongside /oauth2/token rather than inside the
	// authz-middleware-protected /api/v1 block. The endpoint is the
	// authentication bootstrap itself: the caller authenticates via
	// its mTLS client certificate on the TLS handshake, and the
	// handler mints OAuth2 client credentials in return.
	s.registerAgentRegistrationPublicRoutes(r)
}

// IAMHandler returns the IAMHandler registered on this server, or nil when no
// handler has been wired yet. Callers must type-assert to *IAMHandler.
func (s *Server) IAMHandler() interface{} {
	return s.iamHandler
}

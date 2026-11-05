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
	"context"
	"errors"
	"time"

	"github.com/go-chi/chi/v5"
)

// registerIAMRoutes registers IAM API routes and stores the IAMHandler on the
// server so lifecycle code can call SetMTLSAuthenticators after construction.
func (s *Server) registerIAMRoutes(r chi.Router) {
	h := NewIAMHandler(IAMHandlerDependencies{Server: s})
	h.RegisterRoutes(r)

	// Store so lifecycle_iam_mtls.go can wire mTLS authenticators after startup.
	s.iamHandler = h

	// Discard the self-registrations a restart left unfinished, by the expiry
	// recorded on each account. Sessions begun by this process are reaped by
	// their own timer, so this runs once, detached from any request. The
	// stores are resolved here so the detached sweep reads nothing shared.
	ctx := context.Background()
	tenants, tenantErr := h.getTenantManager(ctx)
	store, storeErr := h.getIdentityStore(ctx)
	if err := errors.Join(tenantErr, storeErr); err != nil {
		s.logger.WithError(err).Error("iam.self_registration: sweeping unfinished registrations failed")
	} else {
		logger := s.logger
		go func() {
			if err := sweepRegistrations(ctx, tenants, store, time.Now()); err != nil {
				logger.WithError(err).Error("iam.self_registration: sweeping unfinished registrations failed")
			}
		}()
	}

	// Register the public agent registration endpoint at the root
	// router so it sits alongside /oauth2/token rather than inside the
	// authz-middleware-protected /api/v1 block. The endpoint is the
	// authentication bootstrap itself: the caller authenticates via
	// its mTLS client certificate on the TLS handshake, and the
	// handler mints OAuth2 client credentials in return.
	s.registerFleetEnrollmentPublicRoutes(r)
}

// IAMHandler returns the IAMHandler registered on this server, or nil when no
// handler has been wired yet. Callers must type-assert to *IAMHandler.
func (s *Server) IAMHandler() interface{} {
	return s.iamHandler
}

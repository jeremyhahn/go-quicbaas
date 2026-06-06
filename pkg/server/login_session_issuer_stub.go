//go:build !iam

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

package server

import "github.com/jeremyhahn/go-quicbaas/pkg/service"

// buildLoginSessionIssuer is a no-op when IAM is not compiled: without the
// OIDC provider there is no session issuer to inject, so the composed qrdb
// node's login ceremony remains unwired (login returns the framework's
// missing-issuer error).
func (s *Server) buildLoginSessionIssuer() {}

// buildLoginFactorVerifiers is a no-op when IAM is not compiled: without the
// per-tenant WebAuthn services and CA trust roots there are no hardware factor
// verifiers to inject, so the composed qrdb node's login ceremony has no PIV /
// WebAuthn verification path (login of those factors returns the framework's
// unsupported-factor error).
func (s *Server) buildLoginFactorVerifiers() {}

// bindLoginCeremonyProvider is a no-op when IAM is not compiled.
func (s *Server) bindLoginCeremonyProvider(_ service.IAMComponents) {}

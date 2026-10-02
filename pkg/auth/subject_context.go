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

// Package auth carries the authenticated Subject across a request.
//
// It holds the context plumbing only. Identity itself — the user record, the
// authentication factors bound to it, and the tokens minted for it — belongs to
// go-qrdb and is reached through pkg/iam (IdentityStore for the record,
// iam.OIDCProvider for session tokens). This package deliberately has NO user
// type and NO user store: a second user struct here would be a second
// identifier space, which the locked architecture forbids. See go-quicbaas
// issue #905, which deleted the dead parallel model that used to live here.
package auth

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// subjectCtxKey is the unexported context key type for Subject.
// Using a package-local type prevents collisions with other packages.
type subjectCtxKey struct{}

// systemCtxKey is the unexported context key type for system call marker.
type systemCtxKey struct{}

// SubjectIntoContext returns a new context carrying the authenticated Subject.
// All authentication middleware (gRPC interceptor, REST, QUIC, MCP) must call
// this after verifying credentials so the service layer can perform per-key
// authorization without re-parsing the token.
func SubjectIntoContext(ctx context.Context, subject interfaces.Subject) context.Context {
	return context.WithValue(ctx, subjectCtxKey{}, subject)
}

// SubjectFromContext extracts the authenticated Subject from ctx.
// Returns the zero Subject (Authenticated == false) when no Subject has been
// injected, which causes the per-key authz gate to fail closed.
func SubjectFromContext(ctx context.Context) (interfaces.Subject, bool) {
	s, ok := ctx.Value(subjectCtxKey{}).(interfaces.Subject)
	return s, ok
}

// NewSystemContext returns a context derived from parent that is marked as an
// internal system call. Service-layer guards use IsSystemContext to bypass
// per-key authorization for scheduler-driven and daemon-internal operations.
//
// SECURITY: system contexts must NEVER be constructed from values that arrive
// over the network. Only intra-process code (schedulers, daemon startup,
// re-encryption jobs) should call this function.
//
// A parent with no tenant bound gets the system tenant (tenantctx.WithSystem):
// a system operation with no tenant of its own acts for the platform, and the
// services no longer read an unbound tenant as the system
// (go-quicbaas#1150). A tenant the parent already carries is kept, so a
// system operation run for one tenant stays scoped to it.
func NewSystemContext(parent context.Context) context.Context {
	ctx := context.WithValue(parent, systemCtxKey{}, true)
	if _, bound := tenantctx.Lookup(ctx); bound {
		return ctx
	}
	return tenantctx.WithSystem(ctx)
}

// IsSystemContext reports whether ctx was created by NewSystemContext.
// When true, the KMS service layer skips per-key authorization so that
// internal operations (grace-period disable, auto-rotation, re-encryption)
// can proceed without a network-authenticated principal.
func IsSystemContext(ctx context.Context) bool {
	v, _ := ctx.Value(systemCtxKey{}).(bool)
	return v
}

// PrincipalFromContext returns the ID of the authenticated Subject the
// transport middleware injected into ctx, or "" when the request carries no
// authenticated Subject.
//
// It is the value a server stamps into a record's created_by: the identity the
// server VERIFIED, never one the caller supplied in the request body. An empty
// result is an honest "no principal" (an in-process system call, a poller)
// rather than a guess.
func PrincipalFromContext(ctx context.Context) string {
	subject, ok := SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		return ""
	}
	return subject.ID
}

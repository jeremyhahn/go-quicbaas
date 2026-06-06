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

// Package auth provides authentication and authorization utilities.
package auth

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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
func NewSystemContext(parent context.Context) context.Context {
	return context.WithValue(parent, systemCtxKey{}, true)
}

// IsSystemContext reports whether ctx was created by NewSystemContext.
// When true, the KMS service layer skips per-key authorization so that
// internal operations (grace-period disable, auto-rotation, re-encryption)
// can proceed without a network-authenticated principal.
func IsSystemContext(ctx context.Context) bool {
	v, _ := ctx.Value(systemCtxKey{}).(bool)
	return v
}

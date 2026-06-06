//go:build iam

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

package iam

import "context"

// loginDPoPContextKey keys an optional DPoP proof presented with a unified login
// ceremony (the human-session sender-constraint path).
type loginDPoPContextKey struct{}

// LoginDPoP carries an RFC 9449 DPoP proof presented on a login-finish request,
// together with the htm/htu the issuer verifies it against. It rides on the
// request context from the transport login handler to the SessionIssuer so the
// issued session bearer can be sender-constrained (cnf.jkt) without widening the
// qrdb login-ceremony signature: qrdb passes the caller's context unchanged into
// SessionIssuer.Issue, which reads this value back out.
type LoginDPoP struct {
	// Proof is the DPoP proof JWT taken from the request DPoP header.
	Proof string
	// Method is the login-finish request method, the proof htm.
	Method string
	// URI is the absolute login-finish endpoint URI, the proof htu.
	URI string
}

// ContextWithLoginDPoP returns a context carrying a login DPoP proof. A nil or
// empty-proof value is a no-op (the context is returned unchanged) so callers
// need not branch on header presence.
func ContextWithLoginDPoP(ctx context.Context, d *LoginDPoP) context.Context {
	if d == nil || d.Proof == "" {
		return ctx
	}
	return context.WithValue(ctx, loginDPoPContextKey{}, d)
}

// LoginDPoPFromContext extracts a login DPoP proof from the context, or nil when
// none was presented.
func LoginDPoPFromContext(ctx context.Context) *LoginDPoP {
	if v := ctx.Value(loginDPoPContextKey{}); v != nil {
		if d, ok := v.(*LoginDPoP); ok {
			return d
		}
	}
	return nil
}

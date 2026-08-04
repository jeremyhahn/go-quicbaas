// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

package login

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"

	"github.com/stretchr/testify/require"
)

// TestUnifiedLogin_PasswordLoginUnsupported is SECURITY assertion (e): no login
// path sends an empty (or any) password. The legacy username/password
// AuthClient.Login verb is retired in the locked architecture; it fails closed
// with a typed PasswordLoginUnsupportedError directing callers to the unified
// ceremony. We assert this over every network transport so a regression that
// re-enables a password path on any single transport is caught.
//
// Note: ExtendedLoginRequest no longer even has a Password field — the type
// system itself forbids submitting a password. This test guards the runtime
// behavior of the retired verb.
func TestUnifiedLogin_PasswordLoginUnsupported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, tr := range allLoginTransports() {
		tr := tr
		t.Run(string(tr), func(t *testing.T) {
			c, cerr := newLoginClient(t, tr, mtlsTLSConfig(t))
			require.NoError(t, cerr, "[%s] build login client", tr)
			defer c.Close()

			_, lerr := c.Security().Auth().Login(ctx, &service.ExtendedLoginRequest{
				Username: adminSubject,
				TenantID: systemTenantID,
			})
			require.Error(t, lerr, "[%s] legacy password login must be unsupported", tr)
			switch tr {
			case transportQUIC, transportMCP:
				// On QUIC and MCP the legacy verb is removed at the WIRE level:
				// QUIC opcode 0x60 is permanently reserved ("former password
				// login", pkg/api/quic/protocol.go) and MCP registers only
				// quicbaas_login_begin / quicbaas_login_finish
				// (pkg/api/mcp/tools_login.go). The verb not existing at all is
				// a strictly stronger fail-closed posture than a typed
				// rejection, so accept either form.
				require.True(t, sdkerrors.IsPasswordLoginUnsupported(lerr) || isLegacyLoginVerbAbsent(lerr),
					"[%s] legacy Login must be rejected typed or absent at the wire level, got: %v", tr, lerr)
			default:
				require.True(t, sdkerrors.IsPasswordLoginUnsupported(lerr),
					"[%s] legacy Login must return PasswordLoginUnsupportedError, got: %v", tr, lerr)
			}
		})
	}
}

// isLegacyLoginVerbAbsent reports whether err is the wire-level "verb does
// not exist" rejection from a transport whose protocol surface no longer
// carries the retired password Login verb at all: the reserved QUIC opcode
// 0x60 or the unregistered MCP quicbaas_login tool. Both surface as
// invalid-input errors from the server's dispatch table.
func isLegacyLoginVerbAbsent(err error) bool {
	if err == nil || !sdkerrors.IsInvalidInput(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unknown opcode: 0x60") ||
		strings.Contains(msg, "unknown tool: quicbaas_login")
}

// TestUnifiedLogin_WebAuthnInContainerDeferred is SECURITY assertion (d),
// documented explicitly so there is NO silent gap.
//
// WHAT IS COVERED for WebAuthn/FIDO2:
//   - Server-side verification path: pkg/service/login_service_test.go proves
//     the LoginService facade forwards fido2 proofs UNCHANGED (no peer-cert
//     substitution) so the node's FactorVerifiers own verification; and
//     go-xkms pkg/auth_factors/webauthn.go (+ its unit tests) covers the
//     WebAuthn assertion verifier itself with a software authenticator.
//   - Client-side assertion plumbing: cmd/quicbaas/canonical_auth_login_fido2_test.go
//     drives the vendor-agnostic options-extraction, credential-id derivation,
//     and metadata assembly through a seam (canonicalFIDO2AssertFunc) without a
//     physical device.
//   - Concurrent distinct-session semantics: the session issuer mints a
//     per-login bearer keyed on the resolved identity + a fresh session id
//     (pkg/server/login_session_issuer.go); concurrent logins therefore yield
//     distinct bearers. This is asserted end-to-end for mtls/piv in
//     TestUnifiedLogin_ConcurrentLoginsMintDistinctBearers below, which
//     exercises the SAME session-issuance code path WebAuthn uses.
//
// WHAT IS DEFERRED (and why):
//   - A full BeginLogin(fido2) -> navigator.credentials.get() -> FinishLogin
//     round-trip in-container requires a VIRTUAL CTAP2 authenticator. The
//     integration image ships no virtual authenticator and go-xkms's
//     test/integration/fido2 harness (if present) is not wired into this
//     cluster image. Driving a real assertion here would require either USB
//     passthrough of a hardware key or a libfido2 software authenticator built
//     into the image — neither is available in the standard devcontainer.
//
// This test fails NEVER; it exists to make the coverage boundary auditable in
// the test log rather than leaving it implicit.
func TestUnifiedLogin_WebAuthnInContainerDeferred(t *testing.T) {
	t.Log("WebAuthn/FIDO2 end-to-end (virtual authenticator) is DEFERRED in-container.")
	t.Log("Covered elsewhere: server verifier (pkg/service/login_service_test.go, " +
		"go-xkms pkg/auth_factors/webauthn.go), client assertion plumbing " +
		"(cmd/quicbaas/canonical_auth_login_fido2_test.go), and distinct-session " +
		"semantics (TestUnifiedLogin_ConcurrentLoginsMintDistinctBearers).")
}

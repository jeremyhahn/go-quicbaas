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

//go:build integration && rest && iam

package login

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/stretchr/testify/require"
)

// adminSubject is the bootstrap admin username the integration cluster creates
// in the system tenant via go-qrdb first-boot (pkg/firstboot adminUsername =
// "admin"). The suite enrolls its mtls / piv factors against this user so the
// resolved login identity is deterministic and the same across transports.
const adminSubject = "admin"

// TestUnifiedLogin_MTLS_AcrossTransports is the headline mTLS test. It:
//
//  1. Enrolls an mtls factor (Credential = SHA-256(admin certDER)) against the
//     bootstrap admin user, writing to the qrdb UserAuthFactor store the login
//     resolver reads.
//  2. For every NETWORK transport (REST, gRPC, QUIC, Unix), builds an SDK
//     client that PRESENTS the admin client certificate on the TLS handshake
//     and runs BeginLogin(mtls) + FinishLogin.
//  3. Asserts a bearer is issued, the resolved (subject, tenant) is IDENTICAL
//     across every transport, the bearer's JWT claims encode that identity, and
//     the bearer validates against the live /oauth2/userinfo endpoint.
//
// The mTLS possession proof is the TLS handshake itself: the server derives the
// lookup key from the VERIFIED peer certificate, never from the request body.
func TestUnifiedLogin_MTLS_AcrossTransports(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Enroll the mtls factor once (idempotent at the suite level: a duplicate
	// enroll surfaces FactorAlreadyEnrolledError, which we tolerate so the
	// suite is re-runnable against a warm cluster).
	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err, "build enroll client")
	defer enrollClient.Close()

	if eerr := enrollMTLSFactor(ctx, enrollClient, adminSubject, adminLeaf); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr),
			"mtls factor enrollment must succeed or already exist, got: %v", eerr)
	}

	var (
		firstSubject string
		firstTenant  string
	)

	for _, tr := range allLoginTransports() {
		tr := tr
		t.Run(string(tr), func(t *testing.T) {
			c, cerr := newLoginClient(t, tr, mtlsTLSConfig(t))
			require.NoError(t, cerr, "[%s] build mTLS login client", tr)
			defer c.Close()

			res, lerr := runSDKLogin(ctx, c,
				qrdbtransport.FactorTypeMTLS,
				adminSubject,
				certSHA256(adminLeaf), // ignored server-side for mtls; peer cert wins
				nil,                   // no in-band signature for mtls
				nil)
			require.NoError(t, lerr, "[%s] mTLS unified login must succeed", tr)

			require.NotEmpty(t, res.bearer, "[%s] login must mint a bearer", tr)
			require.NotEmpty(t, res.subject, "[%s] login must resolve a subject", tr)
			require.Equal(t, systemTenantID, res.tenant, "[%s] resolved tenant must be the system tenant", tr)

			// Cross-transport identity parity.
			if firstSubject == "" {
				firstSubject = res.subject
				firstTenant = res.tenant
			} else {
				require.Equal(t, firstSubject, res.subject,
					"[%s] resolved subject must be identical across transports", tr)
				require.Equal(t, firstTenant, res.tenant,
					"[%s] resolved tenant must be identical across transports", tr)
			}

			// The bearer's JWT claims must encode the resolved identity.
			jwtSub, jwtTenant, jerr := jwtSubject(res.bearer)
			require.NoError(t, jerr, "[%s] bearer must be a decodable JWT", tr)
			require.Equal(t, res.subject, jwtSub,
				"[%s] JWT sub must match the resolved login subject", tr)
			if jwtTenant != "" {
				require.Equal(t, res.tenant, jwtTenant,
					"[%s] JWT tenant claim must match the resolved tenant", tr)
			}

			// The bearer must be a LIVE credential the server accepts.
			require.NoError(t, validateBearerOverREST(ctx, res.bearer),
				"[%s] issued bearer must validate against /oauth2/userinfo", tr)
		})
	}

	require.NotEmpty(t, firstSubject, "at least one transport must have completed an mTLS login")
}

// TestUnifiedLogin_MTLS_NoClientCertFailsClosed is SECURITY assertion (a): a
// network mTLS FinishLogin presented with NO client certificate must FAIL
// CLOSED. The ceremony's begin step does not require a cert (it only issues a
// challenge), but finish must reject the attempt because the server has no
// verified peer cert to derive the lookup key from — it must NOT fall through
// to trusting the client-supplied LookupKey.
func TestUnifiedLogin_MTLS_NoClientCertFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Drive the wire ceremony directly with a TLS config that presents NO
	// client certificate.
	wire := newRESTCeremonyClient(t, noCertTLSConfig(t))

	begin, status, berr := wire.begin(ctx, "mtls", adminSubject)
	require.NoError(t, berr, "begin (no client cert) should be reachable; begin only issues a challenge")
	require.Equal(t, http.StatusOK, status, "begin must succeed without a cert (challenge issuance only)")
	require.NotEmpty(t, begin.SessionID, "begin must return a session id")

	// Finish with the (correct) admin lookup key but NO presented client cert.
	// The server must fail closed: it has no verified peer cert and the request
	// is not embedded-trusted, so MTLSPeerCertRequiredError -> 401.
	finish, fstatus, ferr := wire.finish(ctx, map[string]any{
		"session_id": begin.SessionID,
		"type":       "mtls",
		"lookup_key": certSHA256(adminLeaf),
	})
	require.NoError(t, ferr, "finish transport call should complete")
	require.Equal(t, http.StatusUnauthorized, fstatus,
		"mTLS finish with NO client cert MUST fail closed (401); got finish=%+v", finish)
	require.Empty(t, finish.Bearer, "no bearer may be issued when the client cert is absent")
}

// TestUnifiedLogin_MTLS_SpoofedLookupKeyResolvesToPeerCert is SECURITY
// assertion (b): a network mTLS request whose claimed lookup_key differs from
// the PRESENTED peer certificate must resolve to the PEER CERT's user — the
// spoofed value is ignored. We prove this by:
//
//   - presenting the admin client cert on the handshake, and
//   - sending a bogus lookup_key in the finish body.
//
// The login must still succeed AND resolve to the admin (the peer cert's user),
// demonstrating the server overwrote the spoofed key with SHA-256(peer cert).
// A spoof that "wins" would either resolve to a different/empty subject or
// fail; neither is allowed.
func TestUnifiedLogin_MTLS_SpoofedLookupKeyResolvesToPeerCert(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Ensure the admin mtls factor is enrolled (peer cert -> admin).
	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err)
	defer enrollClient.Close()
	if eerr := enrollMTLSFactor(ctx, enrollClient, adminSubject, adminLeaf); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr), "enroll admin mtls factor: %v", eerr)
	}

	// First establish the legitimate resolved subject for comparison.
	legitClient, lerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, lerr)
	defer legitClient.Close()
	legit, lerr := runSDKLogin(ctx, legitClient, qrdbtransport.FactorTypeMTLS,
		adminSubject, certSHA256(adminLeaf), nil, nil)
	require.NoError(t, lerr, "baseline mTLS login must succeed")
	require.NotEmpty(t, legit.subject)

	// Now drive the wire ceremony PRESENTING the admin cert but SENDING a
	// spoofed lookup key in the body.
	wire := newRESTCeremonyClient(t, mtlsTLSConfig(t))
	begin, status, berr := wire.begin(ctx, "mtls", adminSubject)
	require.NoError(t, berr)
	require.Equal(t, http.StatusOK, status)

	spoofed := make([]byte, 32)
	for i := range spoofed {
		spoofed[i] = 0xAB // deliberately not the admin cert hash
	}
	require.NotEqual(t, certSHA256(adminLeaf), spoofed)

	finish, fstatus, ferr := wire.finish(ctx, map[string]any{
		"session_id": begin.SessionID,
		"type":       "mtls",
		"lookup_key": spoofed, // attacker-chosen victim key
	})
	require.NoError(t, ferr)
	require.Equal(t, http.StatusOK, fstatus,
		"mTLS finish with a presented peer cert must succeed (peer cert wins over the spoofed body)")
	require.NotNil(t, finish.Identity, "finish must carry a resolved identity")

	// The spoofed key must have been ignored: the resolved subject is the PEER
	// CERT's user (admin), identical to the legitimate login above.
	require.Equal(t, legit.subject, finish.Identity.Subject,
		"spoofed lookup key must be ignored; identity must resolve to the PEER CERT's user")
	require.Equal(t, systemTenantID, finish.Identity.TenantID,
		"resolved tenant must be the peer cert's tenant")
	require.NoError(t, validateBearerOverREST(ctx, finish.Bearer),
		"the peer-cert-resolved bearer must validate")
}

// isAlreadyEnrolled reports whether err is go-qrdb's FactorAlreadyEnrolledError
// (or carries a message indicating the factor already exists). The suite
// tolerates this so it is safely re-runnable against a warm cluster —
// re-enrollment of an identical credential is a no-op for the test's purposes.
func isAlreadyEnrolled(err error) bool {
	if err == nil {
		return false
	}
	var dup *qrdbtransport.FactorAlreadyEnrolledError
	if errors.As(err, &dup) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "already enrolled") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate")
}

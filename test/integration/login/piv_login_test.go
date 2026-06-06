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
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/stretchr/testify/require"
)

// TestUnifiedLogin_PIV_AcrossTransports is the headline PIV test. It mints a
// software PIV credential (P-256 key + a leaf certificate chained to the
// cluster CA), enrolls it as a piv factor against the bootstrap admin user
// (Credential = SHA-256(leaf DER), Metadata["cert_pem"] = the leaf PEM), then,
// for every NETWORK transport:
//
//  1. BeginLogin(piv) -> obtain the server nonce.
//  2. Sign SHA-256(nonce) with the P-256 key (ECDSA ASN.1) — exactly what a
//     real PIV slot produces.
//  3. FinishLogin with LookupKey = SHA-256(leaf DER) + the signature.
//
// The server-side xkms PIVHardwareFactorVerifier loads the cert from the
// factor metadata, validates the chain against the cluster CA, confirms the
// lookup key matches SHA-256(cert), and verifies the ECDSA signature over
// SHA-256(nonce). The crypto is REAL end-to-end — nothing is mocked.
//
// The test asserts a bearer is issued, the resolved (subject, tenant) is
// identical across transports, and the bearer validates.
func TestUnifiedLogin_PIV_AcrossTransports(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	cred := newSoftwarePIVCredential(t, "login-test-piv.admin@integration.test")

	// Enroll the piv factor against the admin user.
	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err, "build enroll client")
	defer enrollClient.Close()

	if eerr := enrollPIVFactor(ctx, enrollClient, adminSubject, cred); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr),
			"piv factor enrollment must succeed or already exist, got: %v", eerr)
	}

	var firstSubject, firstTenant string

	for _, tr := range allLoginTransports() {
		tr := tr
		t.Run(string(tr), func(t *testing.T) {
			// The PIV login client still presents the admin mTLS cert so the
			// network transport's TLS handshake succeeds; the PIV PROOF,
			// however, is the signature over the nonce, not the handshake.
			c, cerr := newLoginClient(t, tr, mtlsTLSConfig(t))
			require.NoError(t, cerr, "[%s] build PIV login client", tr)
			defer c.Close()

			res, lerr := runSDKLogin(ctx, c,
				qrdbtransport.FactorTypePIV,
				adminSubject,
				cred.leafHash, // piv lookup key = SHA-256(leaf DER)
				cred.signNonce,
				map[string]string{"slot": "9a"})
			require.NoError(t, lerr, "[%s] PIV unified login must succeed", tr)

			require.NotEmpty(t, res.bearer, "[%s] PIV login must mint a bearer", tr)
			require.NotEmpty(t, res.subject, "[%s] PIV login must resolve a subject", tr)
			require.Equal(t, systemTenantID, res.tenant, "[%s] resolved tenant must be the system tenant", tr)

			if firstSubject == "" {
				firstSubject = res.subject
				firstTenant = res.tenant
			} else {
				require.Equal(t, firstSubject, res.subject,
					"[%s] PIV-resolved subject must be identical across transports", tr)
				require.Equal(t, firstTenant, res.tenant,
					"[%s] PIV-resolved tenant must be identical across transports", tr)
			}

			jwtSub, _, jerr := jwtSubject(res.bearer)
			require.NoError(t, jerr, "[%s] PIV bearer must be a decodable JWT", tr)
			require.Equal(t, res.subject, jwtSub,
				"[%s] JWT sub must match the PIV-resolved subject", tr)

			require.NoError(t, validateBearerOverREST(ctx, res.bearer),
				"[%s] issued PIV bearer must validate against /oauth2/userinfo", tr)
		})
	}

	require.NotEmpty(t, firstSubject, "at least one transport must have completed a PIV login")
}

// TestUnifiedLogin_PIV_BadSignatureRejected is SECURITY assertion (c): a PIV
// FinishLogin carrying a FORGED signature must be rejected. The factor is
// enrolled correctly (valid cert, correct lookup key), but the signature is
// random garbage that cannot verify against the enrolled certificate's public
// key. The server must refuse to mint a bearer.
func TestUnifiedLogin_PIV_BadSignatureRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cred := newSoftwarePIVCredential(t, "login-test-piv-badsig.admin@integration.test")

	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err)
	defer enrollClient.Close()
	if eerr := enrollPIVFactor(ctx, enrollClient, adminSubject, cred); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr), "enroll piv factor: %v", eerr)
	}

	// Drive the wire ceremony so we control the exact (forged) signature bytes.
	wire := newRESTCeremonyClient(t, mtlsTLSConfig(t))
	begin, status, berr := wire.begin(ctx, "piv", adminSubject)
	require.NoError(t, berr)
	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, begin.Nonce, "PIV begin must return a server nonce to sign")

	// Forge a signature: random bytes that cannot verify against the cert.
	forged := make([]byte, 72) // plausible ECDSA-P256 ASN.1 length
	_, rerr := rand.Read(forged)
	require.NoError(t, rerr)

	finish, fstatus, ferr := wire.finish(ctx, map[string]any{
		"session_id": begin.SessionID,
		"type":       "piv",
		"lookup_key": cred.leafHash, // correct factor, forged proof
		"signature":  forged,
	})
	require.NoError(t, ferr, "finish transport call should complete")
	require.NotEqual(t, http.StatusOK, fstatus,
		"PIV finish with a FORGED signature MUST be rejected; got finish=%+v", finish)
	require.Empty(t, finish.Bearer, "no bearer may be issued for a forged PIV signature")
}

// TestUnifiedLogin_PIV_SignatureOverWrongNonceRejected proves replay/secondary
// protection: a signature computed over a DIFFERENT nonce than the one the
// server issued for this session must not verify. We sign a fresh random value
// (not begin.Nonce); the verifier hashes the SESSION nonce and the signature
// will not match.
func TestUnifiedLogin_PIV_SignatureOverWrongNonceRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cred := newSoftwarePIVCredential(t, "login-test-piv-replay.admin@integration.test")

	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err)
	defer enrollClient.Close()
	if eerr := enrollPIVFactor(ctx, enrollClient, adminSubject, cred); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr), "enroll piv factor: %v", eerr)
	}

	wire := newRESTCeremonyClient(t, mtlsTLSConfig(t))
	begin, status, berr := wire.begin(ctx, "piv", adminSubject)
	require.NoError(t, berr)
	require.Equal(t, http.StatusOK, status)

	// Sign a DIFFERENT nonce than the server issued.
	wrongNonce := make([]byte, 32)
	_, rerr := rand.Read(wrongNonce)
	require.NoError(t, rerr)
	sig, serr := cred.signNonce(wrongNonce)
	require.NoError(t, serr)

	finish, fstatus, ferr := wire.finish(ctx, map[string]any{
		"session_id": begin.SessionID,
		"type":       "piv",
		"lookup_key": cred.leafHash,
		"signature":  sig,
	})
	require.NoError(t, ferr)
	require.NotEqual(t, http.StatusOK, fstatus,
		"PIV finish with a signature over the WRONG nonce MUST be rejected; got finish=%+v", finish)
	require.Empty(t, finish.Bearer, "no bearer may be issued for a wrong-nonce PIV signature")
}

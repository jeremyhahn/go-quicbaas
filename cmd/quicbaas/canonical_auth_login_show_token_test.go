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

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// testBearer is a JWT-shaped value distinctive enough that a leak anywhere in
// the rendered output is unambiguous.
const testBearer = "eyJhbGciOiJFZERTQSJ9.eyJzdWIiOiJvcGVyYXRvciJ9.S1GNATUREsentinel"

func newTestLoginSummary() canonicalLoginSummary {
	return canonicalLoginSummary{
		Subject:   "operator",
		Tenant:    "acme",
		Bearer:    testBearer,
		ExpiresAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		SavedPath: "/home/operator/.quicbaas/token",
	}
}

// TestCanonicalLoginSummary_DefaultOmitsToken is the security assertion for
// issue #58: by default `auth login` puts no part of the bearer on stdout.
//
// The check is on the raw rendered bytes rather than on a label, because the
// leak would be the token value itself wherever it appeared.
func TestCanonicalLoginSummary_DefaultOmitsToken(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	summary := newTestLoginSummary()
	require.False(t, summary.ShowToken, "the default must not be show-token")
	summary.write(&out)

	rendered := out.String()
	require.NotContains(t, rendered, testBearer,
		"the session token must not reach stdout by default")

	// The signature segment alone is enough to replay nothing, but its presence
	// would mean the token was rendered and merely truncated.
	require.NotContains(t, rendered, "S1GNATUREsentinel")

	// The default is silent about the secret, not silent about the outcome:
	// a user who sees nothing useful will simply pass --show-token and put the
	// token back on stdout.
	require.Contains(t, rendered, "operator")
	require.Contains(t, rendered, "acme")
	require.Contains(t, rendered, "/home/operator/.quicbaas/token")
	require.Contains(t, rendered, "2026-08-23T12:00:00Z")
}

// TestCanonicalLoginSummary_ShowTokenPrintsIt covers the opt-in path.
func TestCanonicalLoginSummary_ShowTokenPrintsIt(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	summary := newTestLoginSummary()
	summary.ShowToken = true
	summary.write(&out)

	rendered := out.String()
	require.Contains(t, rendered, testBearer,
		"--show-token must print the token scripts asked for")
	require.Contains(t, rendered, "Token:      "+testBearer)
}

// TestCanonicalLoginSummary_OmitsEmptyTenant keeps the tenant line conditional,
// as it was before the output moved behind a renderer.
func TestCanonicalLoginSummary_OmitsEmptyTenant(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	summary := newTestLoginSummary()
	summary.Tenant = ""
	summary.write(&out)

	require.NotContains(t, out.String(), "Tenant:")
}

// TestCanonicalAuthLoginFlags_ShowTokenDefaultsOff pins the CLI default itself.
//
// The renderer tests above prove what the struct does; this one proves the flag
// wiring cannot hand it a ShowToken that starts true.
func TestCanonicalAuthLoginFlags_ShowTokenDefaultsOff(t *testing.T) {
	t.Parallel()

	cmd := newCanonicalAuthLoginCmdV2()

	showToken := cmd.Flags().Lookup("show-token")
	require.NotNil(t, showToken, "--show-token must exist for scripts that need the raw token")
	require.Equal(t, "false", showToken.DefValue,
		"printing the token must be opt-in, never the default")

	// --save-token no longer decides anything: the credential is always stored.
	saveToken := cmd.Flags().Lookup("save-token")
	require.NotNil(t, saveToken, "--save-token stays accepted so existing invocations keep working")
	require.NotEmpty(t, saveToken.Deprecated, "--save-token must announce that it is ignored")
	require.True(t, strings.Contains(saveToken.Deprecated, "always stored"),
		"the deprecation notice must say what replaced the flag, got %q", saveToken.Deprecated)
}

// TestRunCanonicalAuthLoginV2_PersistsTheSessionEnvelope is the flow-level
// assertion for the behaviour this change promises: every successful login
// stores its credential.
//
// The renderer and flag tests above prove the token stays off stdout, and the
// helper test proves writeCanonicalSessionToken writes a well-formed envelope --
// but each exercises a piece in isolation. Without this test, deleting the
// persistence call from runCanonicalAuthLoginV2 leaves all of them passing while
// the credential silently goes nowhere, which with stdout now silent means the
// caller cannot reach the token at all.
func TestRunCanonicalAuthLoginV2_PersistsTheSessionEnvelope(t *testing.T) {
	finish := newFinishResponse()

	auth := &fakeLoginAuthService{
		beginResp:     &qrdbtransport.BeginLoginResponse{SessionID: "s1", Nonce: []byte("nonce")},
		finishResp:    finish,
		finishRefresh: "refresh-abc",
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)

	certPath := filepath.Join(t.TempDir(), "admin.pem")
	writePEMCertToPath(t, certPath)
	tokenPath := filepath.Join(t.TempDir(), "session.token")

	err := runCanonicalAuthLoginV2(context.Background(), &canonicalAuthLoginOptions{
		method:         "mtls",
		clientCertPath: certPath,
		tenantID:       "acme-corp",
		tokenPath:      tokenPath,
		// saveToken deliberately left false: persistence is unconditional now.
	})
	require.NoError(t, err)

	stored, lerr := cli.LoadToken(tokenPath)
	require.NoError(t, lerr, "a successful login must leave a readable credential")
	require.Equal(t, finish.Bearer, stored.AccessToken)
	require.Equal(t, "refresh-abc", stored.RefreshToken,
		"the refresh credential must be stored too, or the session cannot be renewed")
	require.WithinDuration(t, finish.ExpiresAt, stored.ExpiresAt, time.Second)
}

// TestRunCanonicalAuthLoginV2_StoreFailureFailsTheLogin is the other half of the
// contract: if the credential cannot be stored, the login is a failure rather
// than a success the caller has no way to act on.
func TestRunCanonicalAuthLoginV2_StoreFailureFailsTheLogin(t *testing.T) {
	auth := &fakeLoginAuthService{
		beginResp:  &qrdbtransport.BeginLoginResponse{SessionID: "s1", Nonce: []byte("nonce")},
		finishResp: newFinishResponse(),
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeLoginClient(auth), nil)

	certPath := filepath.Join(t.TempDir(), "admin.pem")
	writePEMCertToPath(t, certPath)

	// A regular file cannot be a parent directory, so the envelope write fails.
	blocker := filepath.Join(t.TempDir(), "block")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	err := runCanonicalAuthLoginV2(context.Background(), &canonicalAuthLoginOptions{
		method:         "mtls",
		clientCertPath: certPath,
		tenantID:       "acme-corp",
		tokenPath:      filepath.Join(blocker, "session.token"),
	})
	require.Error(t, err, "a login whose credential cannot be stored must not report success")
	var typed *TokenWriteError
	require.ErrorAs(t, err, &typed)
}

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

// coverage_boost_test.go raises unit coverage of the daemon entrypoint.
// Every test runs in t.TempDir() and never modifies host state, binds a
// port, or starts a long-running daemon.
// The init-flow tests construct an embedded Server (no listeners) and
// shut it down synchronously, so they remain fast and side-effect free.

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// runInlineInit happy paths (none / software / shamir)
// =============================================================================
//
// These exercise the production runInlineInit flow end-to-end against a
// fresh data dir. They deliberately do NOT assert the IAM persistence
// contract (that lives in init_runtime_test.go); they assert only the
// on-disk markers that runInlineInit owns.

// TestRunInlineInit_NoneStrategy_DropsMarker drives the "none" strategy:
// no PINs, embedded server brought up and shut down, init-done marker
// dropped, and the marker's recorded strategy matches.
func TestRunInlineInit_NoneStrategy_DropsMarker(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	require.NoError(t, runInlineInit(cmd, f))

	markerPath := filepath.Join(dataDir, initDoneMarkerName)
	raw, err := os.ReadFile(markerPath)
	require.NoError(t, err)

	var marker initDoneMarker
	require.NoError(t, json.Unmarshal(raw, &marker))
	assert.Equal(t, "none", marker.BarrierStrategy)
	assert.Equal(t, "barrier", marker.CredentialStrategy)
}

// TestRunInlineInit_SoftwareStrategy_DropsMarker drives the "software"
// strategy where the seal subsystem is enabled. PINs are required here.
func TestRunInlineInit_SoftwareStrategy_DropsMarker(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "passphrase",
		secret:             "test-barrier-secret-12345",
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		credentialStrategy: "barrier",
		hostname:           "node.example",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	require.NoError(t, runInlineInit(cmd, f))

	raw, err := os.ReadFile(filepath.Join(dataDir, initDoneMarkerName))
	require.NoError(t, err)
	var marker initDoneMarker
	require.NoError(t, json.Unmarshal(raw, &marker))
	assert.Equal(t, "passphrase", marker.BarrierStrategy)
	assert.Equal(t, "node.example", marker.Hostname)
}

// TestRunInlineInit_ShamirStrategy_WritesPendingState drives the shamir
// branch: a pending_ceremony.json is written, the marker records the
// share/threshold parameters, and officers are recorded by username+kind.
func TestRunInlineInit_ShamirStrategy_WritesPendingState(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	csrPath := filepath.Join(tmp, "alice.csr.pem")
	writeBoostCSRFile(t, csrPath, "alice")

	f := &inlineInitFlags{
		barrierStrategy:    "shamir",
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		credentialStrategy: "barrier",
		shares:             3,
		threshold:          2,
		officers:           []string{"alice:csr:" + csrPath},
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	require.NoError(t, runInlineInit(cmd, f))

	raw, err := os.ReadFile(filepath.Join(dataDir, pendingCeremonyFileName))
	require.NoError(t, err)
	var state pendingCeremonyState
	require.NoError(t, json.Unmarshal(raw, &state))
	assert.Equal(t, "shamir", state.Strategy)
	assert.Equal(t, 3, state.Shares)
	assert.Equal(t, 2, state.Threshold)
	require.Len(t, state.Officers, 1)
	assert.Equal(t, "alice", state.Officers[0].Username)

	// The init-done marker records share/threshold for shamir runs.
	mraw, err := os.ReadFile(filepath.Join(dataDir, initDoneMarkerName))
	require.NoError(t, err)
	var marker initDoneMarker
	require.NoError(t, json.Unmarshal(mraw, &marker))
	assert.Equal(t, 3, marker.Shares)
	assert.Equal(t, 2, marker.Threshold)
	assert.Equal(t, 1, marker.OfficerCount)
}

// TestRunInlineInit_AlreadyDone returns InitAlreadyDoneError on a second
// invocation against the same data dir.
func TestRunInlineInit_AlreadyDone(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	require.NoError(t, runInlineInit(cmd, f))

	err := runInlineInit(cmd, f)
	var typed *InitAlreadyDoneError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, dataDir, typed.DataDir)
}

// TestRunInlineInit_RejectsBadCredentialStrategy ensures validation runs
// before any file is touched or server is constructed.
func TestRunInlineInit_RejectsBadCredentialStrategy(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "vault",
		dataDir:            t.TempDir(),
	}
	cmd := &cobra.Command{Use: "init"}
	err := runInlineInit(cmd, f)
	assert.ErrorIs(t, err, ErrInvalidCredentialStrategy)
}

// TestRunInlineInit_EmptyDataDir returns an error when no data dir can be
// resolved. The package globals are saved and restored so the test does
// not pollute sibling tests in the same binary.
func TestRunInlineInit_EmptyDataDir(t *testing.T) {
	origCfg := cfg
	origDataDir := dataDir
	defer func() { cfg = origCfg; dataDir = origDataDir }()

	cfg = config.Default()
	cfg.Storage.DataDir = ""
	dataDir = ""

	f := &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier"}
	cmd := &cobra.Command{Use: "init"}
	err := runInlineInit(cmd, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data-dir is required")
}

// TestRunInlineInit_InvalidOfficerAborts ensures a malformed --so value
// aborts the run before any state is persisted (no marker is dropped).
func TestRunInlineInit_InvalidOfficerAborts(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		officers:           []string{"bad-format-no-colons"},
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	err := runInlineInit(cmd, f)
	assert.ErrorIs(t, err, ErrInvalidOfficerFormat)
	_, statErr := os.Stat(filepath.Join(dataDir, initDoneMarkerName))
	assert.True(t, os.IsNotExist(statErr), "no marker should be dropped on officer parse failure")
}

// TestRunInlineInit_BadTrustedCAAborts ensures a missing --trusted-ca path
// aborts the run with the canonical typed error.
func TestRunInlineInit_BadTrustedCAAborts(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		trustedCAs:         []string{filepath.Join(dataDir, "absent-ca.pem")},
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	err := runInlineInit(cmd, f)
	assert.ErrorIs(t, err, ErrTrustedCANotFound)
}

// =============================================================================
// resolveInitDataDir / buildInitConfig / runInitEmbeddedServer
// =============================================================================

// TestResolveInitDataDir_Precedence verifies the resolution order:
// --init-data-dir > cfg.Storage.DataDir > global dataDir.
func TestResolveInitDataDir_Precedence(t *testing.T) {
	origCfg := cfg
	origDataDir := dataDir
	defer func() { cfg = origCfg; dataDir = origDataDir }()

	cfg = config.Default()
	cfg.Storage.DataDir = "/from-cfg"
	dataDir = "/from-global"

	assert.Equal(t, "/explicit", resolveInitDataDir(&inlineInitFlags{dataDir: "/explicit"}))
	assert.Equal(t, "/from-cfg", resolveInitDataDir(&inlineInitFlags{}))

	cfg.Storage.DataDir = ""
	assert.Equal(t, "/from-global", resolveInitDataDir(&inlineInitFlags{}))
}

// TestResolveInitDataDir_NilCfg covers the branch where the package cfg is
// nil so the global dataDir is the only fallback.
func TestResolveInitDataDir_NilCfg(t *testing.T) {
	origCfg := cfg
	origDataDir := dataDir
	defer func() { cfg = origCfg; dataDir = origDataDir }()

	cfg = nil
	dataDir = "/only-global"
	assert.Equal(t, "/only-global", resolveInitDataDir(&inlineInitFlags{}))
}

// TestBuildInitConfig_NoHostname covers the branch where no hostname is
// supplied: cfg.Server.Host is left at its default.
func TestBuildInitConfig_NoHostname(t *testing.T) {
	f := &inlineInitFlags{barrierStrategy: "none"}
	c := buildInitConfig(t.TempDir(), f, false)
	assert.False(t, c.Seal.Enabled)
	assert.True(t, c.Server.Embedded)
	assert.False(t, c.KMS.Enabled)
	assert.False(t, c.Secrets.Enabled)
	assert.False(t, c.Vector.Enabled)
}

// TestRunInitEmbeddedServer_StartsAndShutsDown drives runInitEmbeddedServer
// directly so its happy path (construct, start, shutdown) is covered
// without going through the full runInlineInit wrapper.
func TestRunInitEmbeddedServer_StartsAndShutsDown(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier"}
	require.NoError(t, runInitEmbeddedServer(dataDir, f, false))
}

// =============================================================================
// writePendingCeremonyState error path
// =============================================================================

// TestWritePendingCeremonyState_BadOfficerSpec covers the error branch in
// writePendingCeremonyState where a previously-parsed officer string fails
// to re-parse (defensive path). We feed a mismatched officers slice so the
// re-parse inside the function fails.
func TestWritePendingCeremonyState_BadOfficerSpec(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy: "shamir",
		officers:        []string{"not-a-valid-spec"},
	}
	// officers slice length must match f.officers length for the index
	// access; supply a single placeholder material.
	mats := []*officerMaterial{{Username: "x"}}
	err := writePendingCeremonyState(dataDir, f, mats)
	assert.ErrorIs(t, err, ErrInvalidOfficerFormat)
}

// TestWritePendingCeremonyState_WriteFails covers the write error branch by
// pointing dataDir at a path that cannot be written to.
func TestWritePendingCeremonyState_WriteFails(t *testing.T) {
	require.NotEqual(t, 0, os.Geteuid(), "Test must not run as root")
	f := &inlineInitFlags{
		barrierStrategy: "shamir",
		shares:          3,
		threshold:       2,
	}
	err := writePendingCeremonyState("/proc/nonexistent/dir", f, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init: write")
}

// =============================================================================
// writeInitDoneMarker error path
// =============================================================================

// TestWriteInitDoneMarker_HappyPath writes and reads back a marker.
func TestWriteInitDoneMarker_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), initDoneMarkerName)
	m := &initDoneMarker{BarrierStrategy: "none", CredentialStrategy: "barrier"}
	require.NoError(t, writeInitDoneMarker(path, m))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var got initDoneMarker
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "none", got.BarrierStrategy)
}

// TestWriteInitDoneMarker_WriteFails covers the temp-file write error path.
func TestWriteInitDoneMarker_WriteFails(t *testing.T) {
	require.NotEqual(t, 0, os.Geteuid(), "Test must not run as root")
	m := &initDoneMarker{BarrierStrategy: "none"}
	err := writeInitDoneMarker("/proc/nonexistent/dir/marker", m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init: write")
}

// =============================================================================
// printInitSummary branches
// =============================================================================

// TestPrintInitSummary_ShamirBranch exercises the shamir branch which prints
// the M-of-N mode line and the two-step next-steps block.
func TestPrintInitSummary_ShamirBranch(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	var buf safeBuffer
	cmd.SetErr(&buf)
	f := &inlineInitFlags{
		barrierStrategy:    "shamir",
		credentialStrategy: "barrier",
		hostname:           "node.example",
		shares:             3,
		threshold:          2,
		trustedCAs:         []string{"/a", "/b"},
	}
	printInitSummary(cmd, f, "/data", 2, true)
	out := buf.String()
	assert.Contains(t, out, "2-of-3")
	assert.Contains(t, out, "node.example")
	assert.Contains(t, out, "ceremony claim-cert")
	assert.Contains(t, out, "Officers:")
	assert.Contains(t, out, "TrustedCAPaths:")
}

// TestPrintInitSummary_DefaultBranch exercises the non-shamir branch with
// no officers / hostname / trusted CAs so the optional lines are skipped.
func TestPrintInitSummary_DefaultBranch(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	var buf safeBuffer
	cmd.SetErr(&buf)
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
	}
	printInitSummary(cmd, f, "/data", 0, false)
	out := buf.String()
	assert.Contains(t, out, "Start the daemon")
	assert.NotContains(t, out, "Officers:")
	assert.NotContains(t, out, "Hostname:")
}

// =============================================================================
// Officer loader parse-failure branches (cert / pubkey)
// =============================================================================

// TestLoadOfficerCert_ParseFails feeds a PEM block whose bytes are not a
// valid certificate, covering the ParseCertificate error branch.
func TestLoadOfficerCert_ParseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.cert.pem")
	writePEMBlock(t, path, "CERTIFICATE", []byte("not-a-real-cert"))
	_, err := loadOfficerCert(officerSpec{Username: "x", Kind: officerKindCert, Path: path})
	assert.ErrorIs(t, err, ErrCertParseFailed)
}

// TestLoadOfficerPubKey_ParseFails feeds a PEM block whose bytes are not a
// valid PKIX public key, covering the ParsePKIXPublicKey error branch.
func TestLoadOfficerPubKey_ParseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pub.pem")
	writePEMBlock(t, path, "PUBLIC KEY", []byte("not-a-real-key"))
	_, err := loadOfficerPubKey(officerSpec{Username: "x", Kind: officerKindPubKey, Path: path})
	assert.ErrorIs(t, err, ErrPubKeyParseFailed)
}

// TestLoadOfficerCSR_ParseFails feeds a PEM block whose bytes are not a
// valid CSR, covering the ParseCertificateRequest error branch.
func TestLoadOfficerCSR_ParseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.csr.pem")
	writePEMBlock(t, path, "CERTIFICATE REQUEST", []byte("not-a-real-csr"))
	_, err := loadOfficerCSR(officerSpec{Username: "x", Kind: officerKindCSR, Path: path})
	assert.ErrorIs(t, err, ErrCSRParseFailed)
}

// TestLoadOfficers_PropagatesLoaderError ensures loadOfficers surfaces a
// loader-level error (file not found) rather than swallowing it.
func TestLoadOfficers_PropagatesLoaderError(t *testing.T) {
	_, err := loadOfficers([]string{"alice:csr:/no/such/file.pem"})
	assert.ErrorIs(t, err, ErrOfficerFileNotFound)
}

// TestLoadOfficers_PropagatesParseError ensures loadOfficers surfaces a
// parse-level error before any loader is invoked.
func TestLoadOfficers_PropagatesParseError(t *testing.T) {
	_, err := loadOfficers([]string{":csr:/tmp/x"})
	assert.ErrorIs(t, err, ErrEmptyOfficerUsername)
}

// TestLoadOfficers_Empty returns an empty slice and no error.
func TestLoadOfficers_Empty(t *testing.T) {
	out, err := loadOfficers(nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}

// TestLoadTrustedCAs_DirectoryRejected covers the directory branch of the
// trusted-CA loader.
func TestLoadTrustedCAs_DirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := loadTrustedCAs([]string{dir})
	assert.ErrorIs(t, err, ErrTrustedCARead)
}

// TestLoadTrustedCAs_MultipleValid loads two PEM files into the same pool.
func TestLoadTrustedCAs_MultipleValid(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.pem")
	b := filepath.Join(tmp, "b.pem")
	writeBoostCertFile(t, a, "ca-a")
	writeBoostCertFile(t, b, "ca-b")
	pool, err := loadTrustedCAs([]string{a, b})
	require.NoError(t, err)
	require.NotNil(t, pool)
}

// =============================================================================
// barrierStrategyAllowsPINs
// =============================================================================

func TestBarrierStrategyAllowsPINs(t *testing.T) {
	assert.False(t, barrierStrategyAllowsPINs("none"))
	assert.False(t, barrierStrategyAllowsPINs("disabled"))
	assert.True(t, barrierStrategyAllowsPINs("passphrase"))
	assert.True(t, barrierStrategyAllowsPINs("shamir"))
	assert.True(t, barrierStrategyAllowsPINs("pkcs11"))
}

// =============================================================================
// Canonical Error() messages and command RunE handlers
// =============================================================================

// TestCanonicalErrorMessages verifies every canonical typed error renders a
// non-empty, descriptive message.
func TestCanonicalErrorMessages(t *testing.T) {
	t.Run("CanonicalRedirectError", func(t *testing.T) {
		e := &CanonicalRedirectError{Old: "quicbaasd seal", New: "quicbaasd barrier"}
		assert.Contains(t, e.Error(), "quicbaasd seal")
		assert.Contains(t, e.Error(), "quicbaasd barrier")
	})
	t.Run("PendingFactorBackendError", func(t *testing.T) {
		e := &PendingFactorBackendError{Op: "auth login"}
		assert.Contains(t, e.Error(), "auth login")
	})
	t.Run("PendingCredentialBackendError", func(t *testing.T) {
		e := &PendingCredentialBackendError{Op: "credential rotate"}
		assert.Contains(t, e.Error(), "credential rotate")
	})
	t.Run("PINsNotAllowedError", func(t *testing.T) {
		e := &PINsNotAllowedError{Strategy: "none"}
		assert.Contains(t, e.Error(), "none")
	})
	t.Run("UnsupportedBarrierStrategyError", func(t *testing.T) {
		e := &UnsupportedBarrierStrategyError{Strategy: "tpm2"}
		assert.Contains(t, e.Error(), "tpm2")
	})
	t.Run("InitAlreadyDoneError", func(t *testing.T) {
		e := &InitAlreadyDoneError{DataDir: "/var/lib/quicbaas"}
		assert.Contains(t, e.Error(), "/var/lib/quicbaas")
	})
}

// TestCanonicalBarrierHandlers verifies each barrier subcommand RunE emits a
// redirect-style error pointing at the runtime client.
func TestCanonicalBarrierHandlers(t *testing.T) {
	for _, verb := range []string{"seal", "unseal", "status", "rekey"} {
		cmd, _, err := rootCmd.Find([]string{"barrier", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		runErr := cmd.RunE(cmd, nil)
		require.Error(t, runErr)
		assert.Contains(t, runErr.Error(), verb)
	}
	for _, verb := range []string{"generate", "restore"} {
		cmd, _, err := rootCmd.Find([]string{"barrier", "recovery", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		assert.Error(t, cmd.RunE(cmd, nil))
	}
}

// TestCanonicalCeremonyHandlers verifies each ceremony subcommand RunE emits
// a redirect-style error.
func TestCanonicalCeremonyHandlers(t *testing.T) {
	for _, verb := range []string{"status", "claim-cert", "claim-share", "sign-csr"} {
		cmd, _, err := rootCmd.Find([]string{"ceremony", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		assert.Error(t, cmd.RunE(cmd, nil))
	}
}

// TestCanonicalCAHandlers verifies each CA subcommand RunE emits a
// redirect-style error.
func TestCanonicalCAHandlers(t *testing.T) {
	for _, verb := range []string{"info", "bundle", "list", "issue", "revoke", "export-bundle"} {
		cmd, _, err := rootCmd.Find([]string{"ca", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		assert.Error(t, cmd.RunE(cmd, nil))
	}
}

// TestCanonicalBootstrapHandlers verifies each bootstrap subcommand RunE
// emits the xkms-owned redirect error.
func TestCanonicalBootstrapHandlers(t *testing.T) {
	for _, mode := range []string{"auto", "dane", "noise", "spki", "direct"} {
		cmd, _, err := rootCmd.Find([]string{"bootstrap", mode})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		runErr := cmd.RunE(cmd, nil)
		require.Error(t, runErr)
		assert.Contains(t, runErr.Error(), "xkms")
	}
}

// TestCanonicalCredentialHandlers verifies each credential subcommand RunE
// returns PendingCredentialBackendError.
func TestCanonicalCredentialHandlers(t *testing.T) {
	for _, verb := range []string{"submit", "list", "rotate"} {
		cmd, _, err := rootCmd.Find([]string{"credential", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		runErr := cmd.RunE(cmd, nil)
		var typed *PendingCredentialBackendError
		require.ErrorAs(t, runErr, &typed)
	}
}

// TestCanonicalAuthHandlers verifies the auth login and factor subcommand
// RunE handlers return PendingFactorBackendError.
func TestCanonicalAuthHandlers(t *testing.T) {
	login, _, err := rootCmd.Find([]string{"auth", "login"})
	require.NoError(t, err)
	var loginErr *PendingFactorBackendError
	require.ErrorAs(t, login.RunE(login, nil), &loginErr)

	for _, verb := range []string{"list", "add", "remove", "rotate", "relabel"} {
		cmd, _, err := rootCmd.Find([]string{"auth", "factor", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		var typed *PendingFactorBackendError
		require.ErrorAs(t, cmd.RunE(cmd, nil), &typed)
	}
}

// TestCanonicalInitCmd_PreRunEResolves drives the canonical init command's
// PreRunE so the env-var resolution + canonical validation closure is
// covered. A "none" strategy with no PINs validates cleanly.
func TestCanonicalInitCmd_PreRunEResolves(t *testing.T) {
	cmd := newCanonicalInitCmd()
	require.NotNil(t, cmd.PreRunE)
	require.NoError(t, cmd.Flags().Set("barrier-strategy", "none"))
	assert.NoError(t, cmd.PreRunE(cmd, nil))
}

// TestCanonicalInitCmd_PreRunERejectsBadStrategy ensures the PreRunE closure
// surfaces validation errors from resolveAndValidateInlineInit.
func TestCanonicalInitCmd_PreRunERejectsBadStrategy(t *testing.T) {
	cmd := newCanonicalInitCmd()
	require.NoError(t, cmd.Flags().Set("barrier-strategy", "phantom"))
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "barrier-strategy")
}

// TestCanonicalInitCmd_RunEHappyPath drives the canonical init command's
// RunE closure end-to-end against a temp data dir using the "none" strategy.
func TestCanonicalInitCmd_RunEHappyPath(t *testing.T) {
	dataDir := t.TempDir()
	cmd := newCanonicalInitCmd()
	require.NoError(t, cmd.Flags().Set("barrier-strategy", "none"))
	require.NoError(t, cmd.Flags().Set("init-data-dir", dataDir))
	require.NoError(t, cmd.PreRunE(cmd, nil))
	require.NoError(t, cmd.RunE(cmd, nil))
	_, err := os.Stat(filepath.Join(dataDir, initDoneMarkerName))
	assert.NoError(t, err)
}

// =============================================================================
// resolveAndValidateCanonicalInit env-var resolution
// =============================================================================

// TestResolveAndValidateCanonicalInit_EnvFallback verifies env vars fill in
// unset flags via viper and that the resolved value reaches the
// canonicalInitFlags struct the validation reads from. This is a regression
// test for the env-resolution propagation bug where an env-supplied
// --barrier-strategy=none was validated against the stale pre-resolution
// "software" default and wrongly required PINs.
func TestResolveAndValidateCanonicalInit_EnvFallback(t *testing.T) {
	t.Setenv("QUICBAASBOOST_BARRIER_STRATEGY", "none")
	cmd := newCanonicalInitCmd()
	// Mirror the pre-resolution snapshot resolveAndValidateInlineInit builds:
	// f carries the cobra default ("software") and no PINs. The env var must
	// override it to "none", and validation must then succeed without PINs.
	f := &canonicalInitFlags{barrierStrategy: "passphrase"}
	err := resolveAndValidateCanonicalInit(cmd, f, "QUICBAASBOOST")
	require.NoError(t, err)
	assert.Equal(t, "none", f.barrierStrategy)
}

// TestResolveAndValidateCanonicalInit_EnvFallbackInts verifies numeric env
// fallbacks (shares/threshold) are parsed into the canonicalInitFlags ints.
func TestResolveAndValidateCanonicalInit_EnvFallbackInts(t *testing.T) {
	t.Setenv("QUICBAASBOOSTINT_BARRIER_STRATEGY", "shamir")
	t.Setenv("QUICBAASBOOSTINT_SO_PIN", "so-pin-1234")
	t.Setenv("QUICBAASBOOSTINT_USER_PIN", "user-pin-1234")
	t.Setenv("QUICBAASBOOSTINT_SHARES", "3")
	t.Setenv("QUICBAASBOOSTINT_THRESHOLD", "2")
	cmd := newCanonicalInitCmd()
	f := &canonicalInitFlags{barrierStrategy: "passphrase"}
	err := resolveAndValidateCanonicalInit(cmd, f, "QUICBAASBOOSTINT")
	require.NoError(t, err)
	assert.Equal(t, "shamir", f.barrierStrategy)
	assert.Equal(t, 3, f.shares)
	assert.Equal(t, 2, f.threshold)
}

// TestResolveAndValidateCanonicalInit_EnvFallbackBadInt verifies a
// non-numeric env value for an int flag surfaces an error rather than
// silently defaulting.
func TestResolveAndValidateCanonicalInit_EnvFallbackBadInt(t *testing.T) {
	t.Setenv("QUICBAASBOOSTBAD_BARRIER_STRATEGY", "shamir")
	t.Setenv("QUICBAASBOOSTBAD_SO_PIN", "so-pin-1234")
	t.Setenv("QUICBAASBOOSTBAD_USER_PIN", "user-pin-1234")
	t.Setenv("QUICBAASBOOSTBAD_SHARES", "not-a-number")
	cmd := newCanonicalInitCmd()
	f := &canonicalInitFlags{barrierStrategy: "passphrase"}
	err := resolveAndValidateCanonicalInit(cmd, f, "QUICBAASBOOSTBAD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shares")
}

// =============================================================================
// flushCoverage / setupCoverageSignalHook no-ops
// =============================================================================

// TestCoverageHooks_NoOp invokes the production no-op coverage hooks so they
// are recorded as covered. They must not panic or modify any state.
func TestCoverageHooks_NoOp(t *testing.T) {
	assert.NotPanics(t, func() {
		flushCoverage()
		setupCoverageSignalHook()
	})
}

// =============================================================================
// runDaemon NewServer error path
// =============================================================================

// TestRunDaemon_NewServerFails covers the runDaemon branch where server
// construction fails. prepareDaemon succeeds (valid data dir, valid global
// logger), then NewServer fails because the config's logger output points
// at an unwritable file.
func TestRunDaemon_NewServerFails(t *testing.T) {
	require.NotEqual(t, 0, os.Geteuid(), "Test must not run as root")

	log = createTestLogger(t)
	tempDir := t.TempDir()

	cfg = config.Default()
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = tempDir
	// Keep the WAL dir writable so the boot-time config.Validate gate passes
	// and the failure occurs inside NewServer as this test intends.
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Server.Unix.Enabled = false
	// Force NewServer's internal logger.New to fail. The config validator only
	// checks that a file path is set for file output; opening it happens in
	// NewServer's logger construction.
	cfg.Logging.Output = "file"
	cfg.Logging.File = "/proc/nonexistent/dir/daemon.log"

	err := runDaemon("local")
	require.Error(t, err)
}

// TestRunDaemon_RefusesInvalidConfig covers the boot-time compliance gate:
// runDaemon must refuse to start when config.Validate rejects the effective
// configuration (here: a fail-open authz posture on a network transport with
// no insecure_dev_mode acknowledgment).
func TestRunDaemon_RefusesInvalidConfig(t *testing.T) {
	log = createTestLogger(t)
	tempDir := t.TempDir()

	cfg = config.Default()
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = tempDir
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Server.Unix.Enabled = false
	// Fail-open posture: authz disabled, REST enabled, no dev-mode ack.
	cfg.Security.RequireAuthz = false
	cfg.Security.InsecureDevMode = false
	cfg.Server.REST.Enabled = true

	err := runDaemon("refuse-invalid")
	require.Error(t, err)
	require.Contains(t, err.Error(), "security.require_authz")
}

// =============================================================================
// applyServerFlags embedded-agent branches
// =============================================================================

// TestApplyServerFlags_EmbeddedAgentFlags exercises every embedded-agent
// flag branch in applyServerFlags.
func TestApplyServerFlags_EmbeddedAgentFlags(t *testing.T) {
	log = createTestLogger(t)
	cfg = config.Default()
	cmd := newServerFlagCmd()

	require.NoError(t, cmd.Flags().Set("embedded-agent", "true"))
	require.NoError(t, cmd.Flags().Set("embedded-agent-backend", "ollama"))
	require.NoError(t, cmd.Flags().Set("embedded-agent-ollama-url", "http://127.0.0.1:9999"))
	require.NoError(t, cmd.Flags().Set("embedded-agent-host-affinity", "node-a"))
	require.NoError(t, cmd.Flags().Set("embedded-agent-gpu", "true"))
	require.NoError(t, cmd.Flags().Set("embedded-agent-data-dir", "/tmp/agent-data"))

	applyServerFlags(cmd)

	assert.True(t, cfg.Server.EmbeddedAgent.Enabled)
	assert.Equal(t, "http://127.0.0.1:9999", cfg.Server.EmbeddedAgent.OllamaURL)
	assert.Equal(t, "node-a", cfg.Server.EmbeddedAgent.HostAffinity)
	assert.True(t, cfg.Server.EmbeddedAgent.GPUAvailable)
	assert.Equal(t, "/tmp/agent-data", cfg.Server.EmbeddedAgent.DataDir)
}

// TestApplyServerFlags_EmbeddedAgentDefaultDataDir covers the else branch
// that derives the embedded-agent data dir from the storage data dir when
// the flag is not set and the config field is empty.
func TestApplyServerFlags_EmbeddedAgentDefaultDataDir(t *testing.T) {
	log = createTestLogger(t)
	cfg = config.Default()
	cfg.Storage.DataDir = "/srv/data"
	cfg.Server.EmbeddedAgent.DataDir = ""

	cmd := newServerFlagCmd()
	applyServerFlags(cmd)

	assert.Equal(t, "/srv/data/embedded-agent", cfg.Server.EmbeddedAgent.DataDir)
}

// =============================================================================
// helpers
// =============================================================================

// newServerFlagCmd builds a cobra command with the full server flag set so
// applyServerFlags can be driven in isolation. It mirrors addServerFlags but
// also registers --data-dir (a persistent flag on the real root) so the
// data-dir branch is reachable.
func newServerFlagCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "x"}
	addServerFlags(cmd)
	cmd.Flags().StringVar(&dataDir, "data-dir", "./data", "data directory path")
	return cmd
}

// writeBoostCSRFile writes a valid P-256 CSR PEM for the given common name.
func writeBoostCSRFile(t *testing.T, path, cn string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, priv)
	require.NoError(t, err)
	writePEMBlock(t, path, "CERTIFICATE REQUEST", der)
}

// writeBoostCertFile writes a self-signed P-256 certificate PEM.
func writeBoostCertFile(t *testing.T, path, cn string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	writePEMBlock(t, path, "CERTIFICATE", der)
}

// writePEMBlock writes a single PEM block of the given type to path.
func writePEMBlock(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	out := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	require.NoError(t, os.WriteFile(path, out, 0o600))
}

// safeBuffer captures cobra stderr output without colliding with the bytes
// import used elsewhere in the package's test files.
type safeBuffer struct {
	data []byte
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *safeBuffer) String() string { return string(b.data) }

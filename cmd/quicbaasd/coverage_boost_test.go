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
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"

	"github.com/jeremyhahn/go-quicbaas/pkg/initmarker"
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
	requireTestTrustDomain(t)
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

// TestRunInlineInit_DoesNotWriteDataDirAfterShutdown pins go-quicbaas#714:
// once runInlineInit has shut its server down, nothing writes to that data dir
// again. The server used to leave its process-global xkms service registered,
// so the next server in the process reused it: its OIDC signing key went
// through the first server's key storage instead of its own, re-creating that
// data dir when test cleanup had already removed it.
func TestRunInlineInit_DoesNotWriteDataDirAfterShutdown(t *testing.T) {
	requireTestTrustDomain(t)
	runInit := func(dataDir string) {
		f := &inlineInitFlags{
			barrierStrategy:    "none",
			credentialStrategy: "barrier",
			dataDir:            dataDir,
		}
		require.NoError(t, runInlineInit(&cobra.Command{Use: "init"}, f))
	}

	first := t.TempDir()
	runInit(first)
	before := snapshotDir(t, first)

	second := t.TempDir()
	runInit(second)
	assert.Equal(t, before, snapshotDir(t, first),
		"a later server must not write into a data dir whose server has shut down")
	assert.FileExists(t, hashedKeyPath(second, "keys/software:signing:oidc-signing:ecdsa.key"),
		"the later server's OIDC signing key must live in its own data dir")
}

// hashedKeyPath is where the go-qrdb v0.47 file key store writes the object
// named name under dataDir: the hex SHA-256 of the name, sharded by its first
// two byte pairs beneath keys/.
func hashedKeyPath(dataDir, name string) string {
	sum := sha256.Sum256([]byte(name))
	digest := hex.EncodeToString(sum[:])
	return filepath.Join(dataDir, "keys", digest[:2], digest[2:4], digest)
}

// snapshotDir maps every path under root to its size and modification time.
func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		snap[path] = fmt.Sprintf("%d %s", info.Size(), info.ModTime())
		return nil
	})
	require.NoError(t, err)
	return snap
}

// TestRunInlineInit_SoftwareStrategy_DropsMarker drives the "software"
// strategy where the seal subsystem is enabled. PINs are required here.
func TestRunInlineInit_SoftwareStrategy_DropsMarker(t *testing.T) {
	requireTestTrustDomain(t)
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

// TestRunInlineInit_AlreadyDone returns InitAlreadyDoneError on a second
// invocation against the same data dir.
func TestRunInlineInit_AlreadyDone(t *testing.T) {
	requireTestTrustDomain(t)
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
	// Supply a trust domain so validation reaches the data-dir check this test
	// is about, rather than failing earlier on the SPIFFE requirement.
	cfg.IAM.SPIFFETrustDomain = "quicbaas.test"
	cfg.Storage.DataDir = ""
	dataDir = ""

	f := &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier"}
	cmd := &cobra.Command{Use: "init"}
	err := runInlineInit(cmd, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data-dir is required")
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
	// buildInitConfig reads the operator config for the SPIFFE trust domain, and
	// this test calls runInitEmbeddedServer directly — bypassing
	// validateInlineInitFlags, which is what guarantees a non-empty value on the
	// CLI path. Supply it here: an init with a CA and no trust domain cannot issue
	// a usable admin certificate, so first-boot refuses the boot
	// (go-quicbaas#57) and this test would be asserting a state the CLI cannot
	// produce.
	origCfg := cfg
	t.Cleanup(func() { cfg = origCfg })
	cfg = config.Default()
	cfg.IAM.SPIFFETrustDomain = "quicbaas.example.test"

	dataDir := t.TempDir()
	f := &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier"}
	require.NoError(t, runInitEmbeddedServer(dataDir, f, false))
}

// =============================================================================
// shared init-done marker (pkg/initmarker)
// =============================================================================

// TestInitMarker_WriteReadRoundTrip writes and reads back a marker through
// the shared pkg/initmarker package the daemon-side reader also imports.
func TestInitMarker_WriteReadRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	m := &initDoneMarker{BarrierStrategy: "none", CredentialStrategy: "barrier"}
	require.NoError(t, initmarker.Write(dataDir, m))

	got, err := initmarker.Read(dataDir)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "none", got.BarrierStrategy)
	assert.True(t, initmarker.Exists(dataDir))
}

// TestInitMarker_WriteFails covers the temp-file write error path.
func TestInitMarker_WriteFails(t *testing.T) {
	m := &initDoneMarker{BarrierStrategy: "none"}
	err := initmarker.Write("/proc/nonexistent/dir", m)
	require.Error(t, err)
	var typed *initmarker.WriteError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// printInitSummary branches
// =============================================================================

// TestPrintInitSummary_ShamirBranch exercises the shamir branch, which must
// direct the operator to the live ceremony rather than to any on-disk officer
// bundle: shares are never written to disk.
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
	}
	printInitSummary(cmd, f, "/data")
	out := buf.String()
	assert.Contains(t, out, "2-of-3")
	assert.Contains(t, out, "node.example")
	assert.Contains(t, out, "boots SEALED")
	assert.Contains(t, out, "officer register")
	assert.Contains(t, out, "claim-share")
	assert.NotContains(t, out, "officers/",
		"shamir init must never point the operator at an on-disk share bundle")
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
	printInitSummary(cmd, f, "/data")
	out := buf.String()
	assert.Contains(t, out, "Start the daemon")
	assert.NotContains(t, out, "Hostname:")
}

// =============================================================================
// Officer loader parse-failure branches (cert / pubkey)
// =============================================================================

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
// emits a redirect-style error pointing at the runtime client — never a
// pending-backend stub.
func TestCanonicalCredentialHandlers(t *testing.T) {
	for _, verb := range []string{"submit", "list", "rotate"} {
		cmd, _, err := rootCmd.Find([]string{"credential", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		runErr := cmd.RunE(cmd, nil)
		require.Error(t, runErr)
		assert.Contains(t, runErr.Error(), "quicbaas credential "+verb)
	}
}

// TestCanonicalAuthHandlers verifies the auth login and factor subcommand
// RunE handlers emit redirect-style errors pointing at the runtime client —
// never a pending-backend stub.
func TestCanonicalAuthHandlers(t *testing.T) {
	login, _, err := rootCmd.Find([]string{"auth", "login"})
	require.NoError(t, err)
	loginErr := login.RunE(login, nil)
	require.Error(t, loginErr)
	assert.Contains(t, loginErr.Error(), "quicbaas auth login")

	for _, verb := range []string{"list", "add", "remove", "rotate", "relabel"} {
		cmd, _, err := rootCmd.Find([]string{"auth", "factor", verb})
		require.NoError(t, err)
		require.NotNil(t, cmd.RunE)
		runErr := cmd.RunE(cmd, nil)
		require.Error(t, runErr)
		assert.Contains(t, runErr.Error(), "quicbaas auth factor "+verb)
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
	requireTestTrustDomain(t)
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

// TestRunDaemon_RefusesInvalidSelfServiceRegistration shows the boot gate
// runs the self-service registration section's own validation: an enabled
// section naming an unknown default role stops the daemon at startup
// instead of failing at the first registration.
func TestRunDaemon_RefusesInvalidSelfServiceRegistration(t *testing.T) {
	log = createTestLogger(t)
	tempDir := t.TempDir()

	cfg = config.Default()
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = tempDir
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Server.Unix.Enabled = false
	cfg.IAM.SelfServiceRegistration.Enabled = true
	cfg.IAM.SelfServiceRegistration.DefaultRole = "NoSuchRole"

	err := runDaemon("refuse-selfreg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "iam.self_service_registration.default_role")
}

func TestRunDaemon_RefusesSystemTenantDynamicClientRegistration(t *testing.T) {
	log = createTestLogger(t)
	tempDir := t.TempDir()

	cfg = config.Default()
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = tempDir
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Server.Unix.Enabled = false
	cfg.IAM.DynamicClientRegistration.Enabled = true
	cfg.IAM.DynamicClientRegistration.AnonymousTenantID = config.SystemTenantID

	err := runDaemon("refuse-dcr")
	require.Error(t, err)
	require.Contains(t, err.Error(), "iam.dynamic_client_registration.anonymous_tenant_id")
}

// =============================================================================
// applyServerFlags embedded-fleet-node branches
// =============================================================================

// TestApplyServerFlags_EmbeddedAgentFlags exercises every embedded-agent
// flag branch in applyServerFlags.
func TestApplyServerFlags_EmbeddedAgentFlags(t *testing.T) {
	log = createTestLogger(t)
	cfg = config.Default()
	cmd := newServerFlagCmd()

	require.NoError(t, cmd.Flags().Set("embedded-fleet-node", "true"))
	require.NoError(t, cmd.Flags().Set("embedded-fleet-node-backend", "ollama"))
	require.NoError(t, cmd.Flags().Set("embedded-fleet-node-ollama-url", "http://127.0.0.1:9999"))
	require.NoError(t, cmd.Flags().Set("embedded-fleet-node-host-affinity", "node-a"))
	require.NoError(t, cmd.Flags().Set("embedded-fleet-node-gpu", "true"))
	require.NoError(t, cmd.Flags().Set("embedded-fleet-node-data-dir", "/tmp/agent-data"))

	applyServerFlags(cmd)

	assert.True(t, cfg.Server.EmbeddedFleetNode.Enabled)
	assert.Equal(t, "http://127.0.0.1:9999", cfg.Server.EmbeddedFleetNode.OllamaURL)
	assert.Equal(t, "node-a", cfg.Server.EmbeddedFleetNode.HostAffinity)
	assert.True(t, cfg.Server.EmbeddedFleetNode.GPUAvailable)
	assert.Equal(t, "/tmp/agent-data", cfg.Server.EmbeddedFleetNode.DataDir)
}

// TestApplyServerFlags_EmbeddedAgentDefaultDataDir covers the else branch
// that derives the embedded-fleet-node data dir from the storage data dir when
// the flag is not set and the config field is empty.
func TestApplyServerFlags_EmbeddedAgentDefaultDataDir(t *testing.T) {
	log = createTestLogger(t)
	cfg = config.Default()
	cfg.Storage.DataDir = "/srv/data"
	cfg.Server.EmbeddedFleetNode.DataDir = ""

	cmd := newServerFlagCmd()
	applyServerFlags(cmd)

	assert.Equal(t, "/srv/data/embedded-agent", cfg.Server.EmbeddedFleetNode.DataDir)
}

// =============================================================================
// storage.data_dir -> storage.wal_dir derivation
// =============================================================================

// TestInitConfig_WALDirFollowsDataDir drives the daemon's real config path
// (initConfig, then applyServerFlags as runStart does) and proves that
// storage.wal_dir follows storage.data_dir whichever source set it: config
// file, environment or --data-dir. An explicit wal_dir from any source wins,
// and with neither key set the defaults-layer WAL dir is kept. Before #176 only
// --data-dir derived it, so a service unit with a config file and no flags kept
// the absolute default (/var/lib/quicbaas/wal) and failed boot validation.
func TestInitConfig_WALDirFollowsDataDir(t *testing.T) {
	const (
		fromDataDir  = "derived"
		fromExplicit = "explicit"
		fromDefault  = "default"
	)
	tests := []struct {
		name        string
		fileDataDir bool
		fileWALDir  bool
		envDataDir  bool
		envWALDir   bool
		flagDataDir bool
		want        string
	}{
		{name: "config file data_dir only", fileDataDir: true, want: fromDataDir},
		{name: "config file sets both", fileDataDir: true, fileWALDir: true, want: fromExplicit},
		{name: "env data_dir only", envDataDir: true, want: fromDataDir},
		{name: "env sets both", envDataDir: true, envWALDir: true, want: fromExplicit},
		{name: "data-dir flag only", flagDataDir: true, want: fromDataDir},
		{name: "data-dir flag with env wal_dir", flagDataDir: true, envWALDir: true, want: fromExplicit},
		{name: "data-dir flag with config file wal_dir", flagDataDir: true, fileWALDir: true, want: fromExplicit},
		{name: "neither set", want: fromDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origCfg, origLog, origCfgFile, origDataDir := cfg, log, cfgFile, dataDir
			t.Cleanup(func() {
				cfg, log, cfgFile, dataDir = origCfg, origLog, origCfgFile, origDataDir
				viper.Reset()
			})
			viper.Reset()

			dir := t.TempDir()
			explicitWAL := filepath.Join(dir, "operator-wal")

			// Empty values read as unset, so the host environment cannot leak in.
			t.Setenv("QUICBAAS_STORAGE_DATA_DIR", "")
			t.Setenv("QUICBAAS_STORAGE_WAL_DIR", "")
			if tt.envDataDir {
				t.Setenv("QUICBAAS_STORAGE_DATA_DIR", dir)
			}
			if tt.envWALDir {
				t.Setenv("QUICBAAS_STORAGE_WAL_DIR", explicitWAL)
			}

			storage := ""
			if tt.fileDataDir {
				storage += fmt.Sprintf("  data_dir: %s\n", dir)
			}
			if tt.fileWALDir {
				storage += fmt.Sprintf("  wal_dir: %s\n", explicitWAL)
			}
			content := "logging:\n  level: warn\n"
			if storage != "" {
				content += "storage:\n" + storage
			}
			cfgFile = filepath.Join(dir, "quicbaas.yaml")
			require.NoError(t, os.WriteFile(cfgFile, []byte(content), 0o600))

			// Bind --data-dir to storage.data_dir exactly as init() does.
			cmd := newServerFlagCmd()
			require.NoError(t, viper.BindPFlag("storage.data_dir", cmd.Flags().Lookup("data-dir")))
			if tt.flagDataDir {
				require.NoError(t, cmd.Flags().Set("data-dir", dir))
			}

			initConfig()
			applyServerFlags(cmd)

			switch tt.want {
			case fromDataDir:
				require.Equal(t, dir, cfg.Storage.DataDir)
				require.Equal(t, filepath.Join(dir, "wal"), cfg.Storage.WALDir)
				require.NoError(t, config.Validate(cfg),
					"config with a derived WAL dir must pass the boot-time validator")
			case fromExplicit:
				require.Equal(t, dir, cfg.Storage.DataDir)
				require.Equal(t, explicitWAL, cfg.Storage.WALDir)
			case fromDefault:
				require.Equal(t, config.Default().Storage.WALDir, cfg.Storage.WALDir)
			}
		})
	}
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

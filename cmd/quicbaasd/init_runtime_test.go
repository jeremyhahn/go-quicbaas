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

// init_runtime_test.go exercises the real init flow against an embedded
// Server, asserting the persistence contract (admin assignment + invite +
// roles surviving a daemon restart). All tests use t.TempDir() so they leave
// no host-side state behind.

package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/initmarker"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// TestRunInlineInit_NoneStrategy_WorksWithoutPINs verifies that
// "--barrier-strategy none" does not require --so-pin / --user-pin and
// completes successfully against a fresh data dir.
func TestRunInlineInit_NoneStrategy_WorksWithoutPINs(t *testing.T) {
	requireTestTrustDomain(t)
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit(none) error: %v", err)
	}
	markerPath := filepath.Join(dataDir, initDoneMarkerName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected init-done marker at %s: %v", markerPath, err)
	}
}

// TestRunInlineInit_NoneRejectsPINs verifies that supplying --so-pin or
// --user-pin under "--barrier-strategy none" surfaces PINsNotAllowedError.
// Silent ignoring would let operators believe their SO credential is in
// effect when the daemon has no barrier at all.
func TestRunInlineInit_NoneRejectsPINs(t *testing.T) {
	cases := []struct {
		name string
		f    *inlineInitFlags
	}{
		{"so-pin only", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", soPin: "x"}},
		{"user-pin only", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", userPin: "y"}},
		{"both pins", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", soPin: "x", userPin: "y"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.f.dataDir = t.TempDir()
			cmd := &cobra.Command{Use: "init"}
			// runInlineInit invokes validateInlineInitFlags which only
			// runs credential strategy validation. The PIN rejection lives
			// in resolveAndValidateCanonicalInit so we drive that path
			// directly through resolveAndValidateInlineInit.
			err := resolveAndValidateInlineInit(cmd, c.f, "QUICBAAS_TEST_NONE")
			if err == nil {
				t.Fatal("expected PINsNotAllowedError")
			}
			var typed *PINsNotAllowedError
			if !errors.As(err, &typed) {
				t.Fatalf("got %T (%v), want *PINsNotAllowedError", err, err)
			}
			if typed.Strategy != "none" {
				t.Errorf("Strategy: got %q, want %q", typed.Strategy, "none")
			}
		})
	}
}

// TestRunInlineInit_DisabledRejectsPINs mirrors the none-rejects-PINs
// check for the legacy "disabled" synonym so operators upgrading from
// older builds see the same explicit error.
func TestRunInlineInit_DisabledRejectsPINs(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy:    "disabled",
		credentialStrategy: "barrier",
		soPin:              "supplied",
		dataDir:            t.TempDir(),
	}
	cmd := &cobra.Command{Use: "init"}
	err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_DISABLED")
	if err == nil {
		t.Fatal("expected PINsNotAllowedError")
	}
	var typed *PINsNotAllowedError
	if !errors.As(err, &typed) {
		t.Fatalf("got %T, want *PINsNotAllowedError", err)
	}
}

// TestRunInlineInit_HardwareStrategiesRejected verifies that every
// hardware-backed strategy is recognised on the CLI surface but rejected
// with UnsupportedBarrierStrategyError until its factory wiring lands.
func TestRunInlineInit_HardwareStrategiesRejected(t *testing.T) {
	hardware := []string{"pkcs11", "tpm2", "awskms", "gcpkms", "azurekv", "vault"}
	for _, strategy := range hardware {
		t.Run(strategy, func(t *testing.T) {
			f := &inlineInitFlags{
				barrierStrategy:    strategy,
				credentialStrategy: "barrier",
				soPin:              "so-pin-1234",
				userPin:            "user-pin-1234",
				dataDir:            t.TempDir(),
			}
			cmd := &cobra.Command{Use: "init"}
			err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_HW")
			if err == nil {
				t.Fatalf("expected UnsupportedBarrierStrategyError for %s", strategy)
			}
			var typed *UnsupportedBarrierStrategyError
			if !errors.As(err, &typed) {
				t.Fatalf("got %T (%v), want *UnsupportedBarrierStrategyError", err, err)
			}
			if typed.Strategy != strategy {
				t.Errorf("Strategy: got %q, want %q", typed.Strategy, strategy)
			}
		})
	}
}

// TestRunInlineInit_InvalidBarrierStrategyRejected verifies that a value
// outside the validBarrierStrategies map surfaces the canonical
// "invalid --barrier-strategy" error rather than the typed
// UnsupportedBarrierStrategyError. The two errors mean different things:
// invalid = "this is not a recognized name", unsupported = "name is known
// but factory wiring has not landed".
func TestRunInlineInit_InvalidBarrierStrategyRejected(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy:    "phantom-strategy",
		credentialStrategy: "barrier",
	}
	cmd := &cobra.Command{Use: "init"}
	err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_BAD")
	if err == nil {
		t.Fatal("expected invalid --barrier-strategy error")
	}
	var typed *UnsupportedBarrierStrategyError
	if errors.As(err, &typed) {
		t.Fatalf("expected invalid-strategy error, got UnsupportedBarrierStrategyError")
	}
}

// TestRunInlineInit_InitDoneMarkerIdempotency drives the canonical
// idempotency contract: a second init against an already-initialised data
// dir returns InitAlreadyDoneError. The error references the data dir so
// operators have a single message that names the offending path.
func TestRunInlineInit_InitDoneMarkerIdempotency(t *testing.T) {
	requireTestTrustDomain(t)
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("first runInlineInit error: %v", err)
	}
	err := runInlineInit(cmd, f)
	if err == nil {
		t.Fatal("expected InitAlreadyDoneError on second invocation")
	}
	var typed *InitAlreadyDoneError
	if !errors.As(err, &typed) {
		t.Fatalf("got %T (%v), want *InitAlreadyDoneError", err, err)
	}
	if typed.DataDir != dataDir {
		t.Errorf("DataDir: got %q, want %q", typed.DataDir, dataDir)
	}
}

// TestRunInlineInit_PersistenceRoundTrip verifies the end-to-end
// init → embedded server start → no-rebootstrap contract. After
// runInlineInit completes against a fresh data dir, opening the same
// data dir with a regular embedded Server start MUST NOT create a
// second bootstrap admin assignment.
//
// The first invocation runs through runInlineInit which constructs its
// own embedded Server, runs the IAM bootstrap, and shuts it down. The
// second invocation opens the same data dir with a fresh embedded Server
// (mirroring "quicbaasd start --data-dir ..."). The admin assignment
// count must remain at exactly one after the second start.
func TestRunInlineInit_PersistenceRoundTrip(t *testing.T) {
	requireTestTrustDomain(t)
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "passphrase",
		secret:             "test-barrier-secret-12345",
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit error: %v", err)
	}

	// Open the same data dir as a regular "start" would. The embedded
	// Server runs initializeIAMComponents which in turn runs
	// firstBootBootstrap; the admin assignment from the init phase
	// MUST be discovered so the second bootstrap is skipped.
	cfg := buildInitConfig(dataDir, f, true)
	// A real "start" against a passphrase barrier supplies the operator
	// passphrase via WithSealPassphrase so the qrdb node auto-unseals on
	// boot (matching runInlineInit's own server build). Without it the node
	// stays sealed and node.Client() is nil.
	srv, err := server.NewServer(cfg, server.WithSealPassphrase([]byte(f.secret)))
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	// Reach into the persistent admin storage directly. We do NOT trust
	// the in-memory iamComponents because the contract under test is the
	// on-disk one: any future change that breaks persistence (e.g.
	// switching back to MemoryAdminStorage) MUST fail this assertion.
	//
	// The store must be the TENANT-ROUTED one, which is what IAM writes
	// through. Reading the platform shard raw was equivalent only while
	// seal-disabled boots installed no StoreResolver; since go-qrdb
	// v0.10.0-alpha one is always installed, so the bootstrap admin lives
	// under the system tenant's namespace and a raw shard read returns an
	// empty list — indistinguishable from a genuine persistence failure.
	// This is still the durable store resolved fresh from the restarted
	// server, not a cached component, so the on-disk contract is intact.
	adminStorage := iam.NewBackendAdminStorage(srv.TenantRoutedKVStore())
	assignments, err := adminStorage.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Len(t, assignments, 1,
		"exactly one bootstrap admin assignment must exist after init+start; got %d (re-bootstrap fired)",
		len(assignments),
	)

	// The bootstrap admin lives in qrdb's canonical system tenant.
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
}

// TestRunInlineInit_ShamirRequiresThresholdAndShares verifies that
// supplying --barrier-strategy=shamir without valid --shares / --threshold
// fails at the canonical validation layer.
func TestRunInlineInit_ShamirRequiresThresholdAndShares(t *testing.T) {
	cases := []struct {
		name   string
		shares int
		thresh int
	}{
		{"missing shares", 0, 0},
		{"shares without threshold", 3, 0},
		{"threshold above shares", 3, 5},
		{"shares below 2", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &inlineInitFlags{
				barrierStrategy:    "shamir",
				soPin:              "so-pin-1234",
				userPin:            "user-pin-1234",
				credentialStrategy: "barrier",
				shares:             c.shares,
				threshold:          c.thresh,
			}
			cmd := &cobra.Command{Use: "init"}
			if err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_SHAMIR"); err == nil {
				t.Fatal("expected shamir validation error")
			}
		})
	}
}

// TestPINsNotAllowedError_Message ensures the typed error includes the
// offending strategy so operators see precisely which mode rejected
// their PIN.
func TestPINsNotAllowedError_Message(t *testing.T) {
	e := &PINsNotAllowedError{Strategy: "none"}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	assert.Contains(t, msg, "none")
	assert.Contains(t, msg, "pin")
}

// TestUnsupportedBarrierStrategyError_Message ensures the typed error
// names the strategy and lists the supported alternatives.
func TestUnsupportedBarrierStrategyError_Message(t *testing.T) {
	e := &UnsupportedBarrierStrategyError{Strategy: "pkcs11"}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	assert.Contains(t, msg, "pkcs11")
	assert.Contains(t, msg, "passphrase")
	assert.Contains(t, msg, "shamir")
}

// TestInitAlreadyDoneError_Message ensures the error names the data dir
// and a recovery command.
func TestInitAlreadyDoneError_Message(t *testing.T) {
	e := &InitAlreadyDoneError{DataDir: "/var/lib/quicbaas"}
	msg := e.Error()
	assert.Contains(t, msg, "/var/lib/quicbaas")
	assert.Contains(t, msg, "quicbaasd start")
}

// TestBuildInitConfig_DisablesAllListeners verifies the config returned
// by buildInitConfig has every transport listener turned off so init
// cannot accidentally bind a public port.
func TestBuildInitConfig_DisablesAllListeners(t *testing.T) {
	f := &inlineInitFlags{barrierStrategy: "none"}
	cfg := buildInitConfig(t.TempDir(), f, false)

	assert.True(t, cfg.Server.Embedded, "init config must run in embedded mode")
	assert.False(t, cfg.Server.GRPC.Enabled, "init must not enable gRPC listener")
	assert.False(t, cfg.Server.REST.Enabled, "init must not enable REST listener")
	assert.False(t, cfg.Server.MCP.Enabled, "init must not enable MCP listener")
	assert.False(t, cfg.Server.Unix.Enabled, "init must not enable Unix listener")
	// TLS is ON even though nothing listens: initializeCA early-returns when
	// TLS is disabled, and init needs the CA to issue the platform admin's
	// mTLS certificate and to sign officer CSRs.
	assert.True(t, cfg.Security.TLSEnabled,
		"init must bootstrap the CA, which requires TLSEnabled")
	assert.Equal(t, config.ServerModeStandalone, cfg.Server.Mode,
		"init must run in standalone mode")
}

// TestBuildInitConfig_BarrierFlagTogglesSeal verifies the barrierEnabled
// argument flows through to cfg.Seal.Enabled. The "none" / "disabled"
// strategies must leave seal off; "software" must turn it on.
func TestBuildInitConfig_BarrierFlagTogglesSeal(t *testing.T) {
	f := &inlineInitFlags{barrierStrategy: "passphrase"}
	cfgOn := buildInitConfig(t.TempDir(), f, true)
	cfgOff := buildInitConfig(t.TempDir(), f, false)

	assert.True(t, cfgOn.Seal.Enabled, "barrierEnabled=true must enable seal")
	assert.False(t, cfgOff.Seal.Enabled, "barrierEnabled=false must disable seal")
}

// TestResolveInitDataDir_PrefersInitDataDir verifies the resolution
// precedence: --init-data-dir > package cfg.Storage.DataDir > global
// --data-dir variable.
func TestResolveInitDataDir_PrefersInitDataDir(t *testing.T) {
	f := &inlineInitFlags{dataDir: "/explicit"}
	// Save / restore the package globals so the test does not pollute
	// state for other tests in the same binary.
	origCfg := cfg
	origDataDir := dataDir
	defer func() { cfg = origCfg; dataDir = origDataDir }()

	cfg = config.Default()
	cfg.Storage.DataDir = "/from-cfg"
	dataDir = "/from-global"

	assert.Equal(t, "/explicit", resolveInitDataDir(f))

	f.dataDir = ""
	assert.Equal(t, "/from-cfg", resolveInitDataDir(f))

	cfg.Storage.DataDir = ""
	assert.Equal(t, "/from-global", resolveInitDataDir(f))
}

// ---------------------------------------------------------------------------
// §08 invariant 5: init leaves identical IAM state for every barrier strategy
// ---------------------------------------------------------------------------

// TestRunInlineInit_EveryStrategyRunsFirstBoot is the regression test for the
// strategy-dependent bootstrap: "shamir" used to skip the embedded server
// entirely and only drop a pending_ceremony.json, so a shamir cluster came up
// with NO system tenant, NO built-in roles and NO platform admin, while "none"
// and "passphrase" got all three. §08 invariant 5 requires init to leave
// identical IAM state whichever way the barrier is sealed.
//
// It doubles as the coverage for the CA bootstrap: init used to force
// TLSEnabled=false, which made initializeCA early-return and left xkmsCA nil,
// so first-boot could not issue the admin's mTLS certificate on the init path.
func TestRunInlineInit_EveryStrategyRunsFirstBoot(t *testing.T) {
	requireTestTrustDomain(t)
	cases := []struct {
		name           string
		flags          func(dataDir string) *inlineInitFlags
		wantPendingCer bool
	}{
		{
			name: "none",
			flags: func(dataDir string) *inlineInitFlags {
				return &inlineInitFlags{
					barrierStrategy:    "none",
					credentialStrategy: "barrier",
					dataDir:            dataDir,
				}
			},
		},
		{
			name: "passphrase",
			flags: func(dataDir string) *inlineInitFlags {
				return &inlineInitFlags{
					barrierStrategy:    "passphrase",
					credentialStrategy: "barrier",
					dataDir:            dataDir,
					secret:             "init-passphrase-1234567890",
					soPin:              "so-pin-12345678",
					userPin:            "user-pin-12345678",
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			cmd := &cobra.Command{Use: "init"}
			require.NoError(t, runInlineInit(cmd, tc.flags(dataDir)))

			// The init-done marker proves the run completed.
			require.FileExists(t, filepath.Join(dataDir, initDoneMarkerName))

			// First-boot ran: the platform admin's mTLS credential exists.
			// This is only reachable when the embedded server started AND the
			// CA was bootstrapped, so it covers items 4 and 6 together.
			certPath := filepath.Join(dataDir, "admin", "admin.crt")
			certPEM, err := os.ReadFile(certPath)
			require.NoError(t, err,
				"%s init must run first-boot and issue the admin certificate", tc.name)

			block, _ := pem.Decode(certPEM)
			require.NotNil(t, block, "admin certificate must be valid PEM")
			cert, err := x509.ParseCertificate(block.Bytes)
			require.NoError(t, err)
			assert.NotEmpty(t, qrdbsdk.ComputeSPKIThumbprint(cert),
				"the issued admin certificate must have a resolvable SPKI thumbprint")

			// The private key is written alongside it for the operator to
			// collect (generate-and-distribute; no --bootstrap-admin-csr here).
			require.FileExists(t, filepath.Join(dataDir, "admin", "admin.key"))

			// The dead pending-ceremony mechanism must never resurface.
			assert.NoFileExists(t, filepath.Join(dataDir, "pending_ceremony.json"),
				"%s must not write pending-ceremony state", tc.name)
		})
	}
}

// TestRunInlineInit_ShamirDefersToCeremony pins the locked deferred-init
// architecture at the init boundary. go-qrdb's bootstrap orchestrator states
// that "Shamir barrier initialization is the ceremony's job (multiple SOs),
// never inline", so `quicbaasd init --barrier-strategy shamir` must start no
// server, run no first-boot, split no secret, and above all write no share to
// disk. It records the operator's chosen M-of-N parameters and stops; the
// daemon then boots SEALED and the ceremony runs against it.
func TestRunInlineInit_ShamirDefersToCeremony(t *testing.T) {
	requireTestTrustDomain(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	f := &inlineInitFlags{
		barrierStrategy:    "shamir",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
		shares:             3,
		threshold:          2,
		soPin:              "so-pin-12345678",
		userPin:            "user-pin-12345678",
	}
	cmd := &cobra.Command{Use: "init"}
	require.NoError(t, runInlineInit(cmd, f))

	// The run completed and recorded the M-of-N parameters the daemon boots on.
	marker, err := initmarker.Read(dataDir)
	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "shamir", marker.BarrierStrategy)
	assert.Equal(t, 2, marker.Threshold)
	assert.Equal(t, 3, marker.Shares)

	// No share material of any kind reached the filesystem. This is the
	// security invariant: shares live only in memguard enclaves inside the
	// running daemon until each officer claims theirs over their own mTLS
	// certificate, exactly once.
	assert.NoDirExists(t, filepath.Join(dataDir, "officers"),
		"shamir init must never write per-officer share bundles")
	var stray []string
	_ = filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // a walk error must not fail the scan
		}
		if strings.Contains(d.Name(), "share") {
			stray = append(stray, path)
		}
		return nil
	})
	assert.Empty(t, stray, "no file named for a share may exist under the data dir")

	// First-boot did NOT run: it follows the ceremony's unseal, not init.
	assert.NoFileExists(t, filepath.Join(dataDir, "admin", "admin.crt"),
		"shamir first-boot must wait for the ceremony to unseal the barrier")
}

// TestBuildInitConfig_EnablesCAForEveryStrategy pins the item-6 fix at the
// config layer: init must turn TLS on (which is what gates initializeCA) while
// still binding no listener, for every supported strategy.
func TestBuildInitConfig_EnablesCAForEveryStrategy(t *testing.T) {
	for _, strategy := range []string{"none", "disabled", "passphrase", "shamir"} {
		t.Run(strategy, func(t *testing.T) {
			f := &inlineInitFlags{barrierStrategy: strategy}
			cfg := buildInitConfig(t.TempDir(), f, isPassphraseStrategy(strategy))

			assert.True(t, cfg.Security.TLSEnabled,
				"initializeCA early-returns when TLS is off, leaving no CA to issue the admin cert")
			assert.True(t, cfg.Server.Embedded)
			assert.False(t, cfg.Server.REST.Enabled, "init must still bind no listener")
			assert.False(t, cfg.Server.GRPC.Enabled)
		})
	}
}

// TestBuildInitConfig_PropagatesSPIFFETrustDomain is the regression test for the
// "no headless path from init to an authenticated CLI" defect at its root cause.
//
// buildInitConfig starts from config.Default(), so it silently DISCARDED the
// operator's iam.spiffe_trust_domain. With no trust domain the xkms CAAdapter
// skips SPIFFE SAN stamping, the init-issued admin certificate comes out as
// CN=admin with no SANs, and REST login fails closed with "login tenant could
// not be resolved" — the operator cannot use the credential init handed them.
func TestBuildInitConfig_PropagatesSPIFFETrustDomain(t *testing.T) {
	const trustDomain = "quicbaas.example.test"

	// Stand in for the daemon's loaded configuration.
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = config.Default()
	cfg.IAM.SPIFFETrustDomain = trustDomain

	f := &inlineInitFlags{barrierStrategy: "none"}
	got := buildInitConfig(t.TempDir(), f, false)

	assert.Equal(t, trustDomain, got.IAM.SPIFFETrustDomain,
		"init must carry the operator's SPIFFE trust domain so the admin cert gets a tenant SAN")
}

// TestBuildInitConfig_NoOperatorTrustDomainLeavesItEmpty covers the other
// branch: with nothing configured, init must not invent a trust domain. A
// synthesized value would be baked into every issued certificate and PINNED by
// login, so a later real configuration would invalidate them all.
func TestBuildInitConfig_NoOperatorTrustDomainLeavesItEmpty(t *testing.T) {
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = config.Default()
	cfg.IAM.SPIFFETrustDomain = ""

	f := &inlineInitFlags{barrierStrategy: "none"}
	got := buildInitConfig(t.TempDir(), f, false)

	assert.Empty(t, got.IAM.SPIFFETrustDomain,
		"init must not synthesize a SPIFFE trust domain")
}

// requireTestTrustDomain sets the SPIFFE trust domain on the daemon's loaded
// configuration for the duration of the test.
//
// iam.spiffe_trust_domain is a HARD requirement for `quicbaasd init`: it is
// what makes the issued admin certificate carry the tenant SPIFFE SAN that
// login resolves the tenant from. Every test that expects init to succeed must
// therefore supply one, exactly as a real operator must.
func requireTestTrustDomain(t *testing.T) {
	t.Helper()
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	if cfg == nil {
		cfg = config.Default()
	} else {
		clone := *cfg
		cfg = &clone
	}
	cfg.IAM.SPIFFETrustDomain = "quicbaas.test"
}

// TestRunInlineInit_RequiresSPIFFETrustDomain is the fail-closed regression
// test for the owner decision: init must REFUSE when iam.spiffe_trust_domain is
// unset, rather than issuing an admin certificate that cannot authenticate.
//
// It also pins the "before any side effect" requirement: no data dir, no CA and
// no init marker may exist after the refusal.
//
// No revert experiment is needed to prove this test is non-vacuous: it asserts
// the TYPED error, the exact config key, the example value, AND the absence of
// the data dir, marker and admin certificate. A missing guard cannot produce
// that combination — init would instead succeed and leave those artifacts
// behind, failing every one of the assertions below.
func TestRunInlineInit_RequiresSPIFFETrustDomain(t *testing.T) {
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = config.Default()
	cfg.IAM.SPIFFETrustDomain = ""

	dataDir := filepath.Join(t.TempDir(), "not-yet-created")
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}

	err := runInlineInit(&cobra.Command{Use: "init"}, f)
	require.Error(t, err, "init must refuse without a SPIFFE trust domain")

	var typed *SPIFFETrustDomainRequiredError
	require.ErrorAs(t, err, &typed, "got %T (%v)", err, err)

	// The message must name the exact key and show an example value.
	assert.Contains(t, err.Error(), "iam.spiffe_trust_domain")
	assert.Contains(t, err.Error(), "spiffe_trust_domain: quicbaas.example.test")

	// Fail BEFORE any side effect.
	assert.NoDirExists(t, dataDir, "init must not create the data dir before validating")
	assert.NoFileExists(t, filepath.Join(dataDir, initDoneMarkerName))
	assert.NoFileExists(t, filepath.Join(dataDir, "admin", "admin.crt"))
}

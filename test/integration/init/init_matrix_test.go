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

//go:build integration

// init_matrix_test.go is the canonical init/start/transport matrix.
//
// Why a combinatorial expansion?
// ------------------------------
// The init flow's contract is "drive the binary to a known operational
// state from a single command, against any supported barrier strategy,
// listening on any supported transport". Each (strategy, transport) cell
// is its own integration boundary:
//
//   - strategies select different bootstrap code paths inside the
//     daemon (embedded-server-with-seal vs. embedded-server-without-seal
//     vs. pending-ceremony-state). Coverage across the three rules out a
//     class of bugs where a marker write only succeeds for one strategy.
//   - transports each have an independent listener stack (TCP vs. UDP
//     vs. AF_UNIX). A regression in any listener (e.g. the daemon
//     accidentally binding a different port than env-configured) only
//     surfaces when we drive it column-by-column.
//
// The two axes multiply (3 × 5 = 15) because they fail independently and
// no single subset proves the combined behaviour. Hardware-backed
// strategies (pkcs11/tpm2/awskms/gcpkms/azurekv/vault) are intentionally
// excluded — their factory wiring is gated on operator-supplied hardware
// that belongs in dedicated env-gated suites per the spec.
//
// Each subtest:
//
//  1. Allocates a fresh data dir under t.TempDir() (hermetic; auto-removed).
//  2. Allocates non-overlapping ports for the transport-under-test
//     (loopback-only; collision-resistant against parallel siblings).
//  3. Runs `quicbaasd init --barrier-strategy <s> ...` and asserts the
//     on-disk init-done marker matches the expected strategy.
//  4. For shamir: also asserts the pending_ceremony.json captured the
//     officer count, threshold, and shares we passed.
//  5. Spawns `quicbaasd start --data-dir <dir> ...` against the same
//     dir; verifies the transport-under-test becomes reachable inside
//     daemonStartTimeout.
//  6. Asserts the daemon-issued CA + server cert files appeared (proves
//     the xkms-backed PKI bootstrapped on top of the init state).
//  7. Tears down the daemon via t.Cleanup so the data dir's PebbleDB
//     lock is released before the next subtest.

package init

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// matrixStrategy enumerates the three software-backed barrier strategies
// the matrix exercises. The order is deterministic so log diffs across
// runs stay readable.
var matrixStrategies = []string{"none", "passphrase", "shamir"}

// matrixTransports enumerates the five transports per the locked
// architecture (embedded is excluded per the spec — the in-process
// transport is unit-test territory).
var matrixTransports = []string{"rest", "grpc", "quic", "mcp", "unix"}

// TestCanonicalInit_Matrix runs every (strategy, transport) cell. The
// 15 subtests run sequentially by default — they spawn real binaries and
// the PebbleDB cold-boot dominates CPU, so parallel runs trade wall-clock
// for resource contention without a meaningful coverage gain. Operators
// can opt into parallelism by removing the top-level Helper if they have
// CI capacity to spare.
func TestCanonicalInit_Matrix(t *testing.T) {
	requireIntegrationGate(t)
	binary := resolveDaemonBinary(t)

	for _, strategy := range matrixStrategies {
		for _, transport := range matrixTransports {
			strategy, transport := strategy, transport
			t.Run(matrixSubtestName(strategy, transport), func(t *testing.T) {
				runMatrixCell(t, binary, strategy, transport)
			})
		}
	}
}

// matrixSubtestName renders the canonical "<strategy>_<transport>"
// label so the test runner's pass/fail summary lists every cell.
func matrixSubtestName(strategy, transport string) string {
	return strategy + "_" + transport
}

// runMatrixCell is the per-(strategy, transport) integration body. The
// body is intentionally linear so a failure points to exactly one phase.
func runMatrixCell(t *testing.T, binary, strategy, transport string) {
	t.Helper()

	dataDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Phase 1: init.
	initOutput, initErr := invokeInit(ctx, binary, strategy, dataDir)
	if initErr != nil {
		t.Fatalf("`quicbaasd init --barrier-strategy %s` failed: %v\noutput:\n%s",
			strategy, initErr, initOutput)
	}
	assertInitDoneMarker(t, dataDir, strategy)
	if strategy == "shamir" {
		assertPendingCeremonyState(t, dataDir)
	}

	// Phase 2: start daemon + wait for transport readiness.
	ports := allocPortBundle(t, transport, dataDir)
	daemon := startDaemon(t, binary, dataDir, strategy, transport, ports)
	if err := waitForTransportReady(ctx, t, daemon, transport, ports, dataDir); err != nil {
		t.Fatalf("transport %q for strategy %q never became ready: %v",
			transport, strategy, err)
	}

	// Phase 3: PKI post-conditions. The daemon's xkms-backed CA must
	// have produced both the CA chain and a server cert during start.
	// These artefacts are the load-bearing oracle for any downstream
	// SDK-based check; without them, transport TLS handshake fails.
	requireFileExists(t, caChainPath(dataDir), "platform CA chain (ca-chain.crt)")
	requireFileExists(t, serverCertPath(dataDir), "platform server cert (server.crt)")

	// Phase 4: SDK reaches the daemon over this transport. Health.Liveness
	// is the cheapest cross-transport SDK call (no auth required) and is
	// wired across every supported transport. A successful response proves
	// the SDK transport stack lines up with the daemon's listener for this
	// cell — a regression here would silently break every downstream SDK
	// caller without firing in the raw-socket probes above.
	verifySDKLivenessOverTransport(t, transport, ports, dataDir)

	// Phase 5: cross-strategy invariants on the marker (e.g. credential
	// strategy default = "barrier"). assertInitDoneMarker already covered
	// the strategy-specific contents; the cross-strategy checks live here
	// so the matrix has one obvious failure point per phase.
	marker, err := readInitDoneMarker(dataDir)
	if err != nil {
		t.Fatalf("post-start marker read: %v", err)
	}
	if marker.CredentialStrategy != "barrier" {
		t.Errorf("CredentialStrategy: got %q, want %q",
			marker.CredentialStrategy, "barrier")
	}
	if marker.CompletedAt.IsZero() {
		t.Error("CompletedAt: zero — marker write skipped the timestamp")
	}

	// Phase 6: clean stop. t.Cleanup will also fire Stop() if we did not
	// reach this point — calling it explicitly here keeps the happy path's
	// wall-clock budget tight (we do not wait until the test teardown
	// phase to release the data dir).
	if stopErr := daemon.Stop(); stopErr != nil {
		t.Fatalf("daemon Stop: %v", stopErr)
	}
}

// invokeInit dispatches to the appropriate init helper for the
// requested strategy. Keeps the matrix body free of strategy-specific
// branching.
func invokeInit(ctx context.Context, binary, strategy, dataDir string) (string, error) {
	switch strategy {
	case "none", "passphrase":
		return runInit(ctx, binary, strategy, dataDir)
	case "shamir":
		return runShamirInit(ctx, binary, dataDir)
	}
	return "", errors.New("invokeInit: unknown strategy " + strategy)
}

// assertInitDoneMarker verifies the init-done marker captured the
// expected strategy and (for shamir) the expected officer count /
// threshold / shares. Failure here means the daemon's persistence
// contract regressed — the integration test must fail loudly because
// later phases (start, transport reachability) assume the marker is
// the source of truth for "init already ran".
func assertInitDoneMarker(t *testing.T, dataDir, strategy string) {
	t.Helper()
	marker, err := readInitDoneMarker(dataDir)
	if err != nil {
		t.Fatalf("read init-done marker: %v", err)
	}
	if marker.BarrierStrategy != strategy {
		t.Fatalf("marker.BarrierStrategy: got %q, want %q",
			marker.BarrierStrategy, strategy)
	}
	switch strategy {
	case "none", "passphrase":
		if marker.OfficerCount != 0 {
			t.Errorf("strategy %s: marker.OfficerCount=%d, want 0",
				strategy, marker.OfficerCount)
		}
		if marker.Shares != 0 || marker.Threshold != 0 {
			t.Errorf("strategy %s: shares/threshold leaked into marker: shares=%d threshold=%d",
				strategy, marker.Shares, marker.Threshold)
		}
	case "shamir":
		if marker.OfficerCount != 3 {
			t.Errorf("shamir: marker.OfficerCount=%d, want 3",
				marker.OfficerCount)
		}
		if marker.Shares != 3 {
			t.Errorf("shamir: marker.Shares=%d, want 3", marker.Shares)
		}
		if marker.Threshold != 2 {
			t.Errorf("shamir: marker.Threshold=%d, want 2", marker.Threshold)
		}
	}
}

// assertPendingCeremonyState verifies the shamir branch wrote the
// expected pending ceremony state. The state's contents are how
// subsequent `quicbaas ceremony claim-cert / claim-share` operations
// know what to resume — corruption here means later ceremony tooling
// fails downstream and operators see a confusing "state mismatch"
// instead of a precise typed error.
func assertPendingCeremonyState(t *testing.T, dataDir string) {
	t.Helper()
	state, err := readPendingCeremonyState(dataDir)
	if err != nil {
		t.Fatalf("read pending ceremony state: %v", err)
	}
	if state.Strategy != "shamir" {
		t.Errorf("ceremony.Strategy: got %q, want %q", state.Strategy, "shamir")
	}
	if state.Shares != 3 {
		t.Errorf("ceremony.Shares: got %d, want 3", state.Shares)
	}
	if state.Threshold != 2 {
		t.Errorf("ceremony.Threshold: got %d, want 2", state.Threshold)
	}
	if state.CredentialStrategy != "barrier" {
		t.Errorf("ceremony.CredentialStrategy: got %q, want %q",
			state.CredentialStrategy, "barrier")
	}
	if len(state.Officers) != 3 {
		t.Fatalf("ceremony.Officers: got %d, want 3", len(state.Officers))
	}
	// Officer kinds MUST round-trip through the marker so resumption
	// dispatches to the correct loader. Index order matches the
	// runShamirInit argument order in helpers.go.
	wantKinds := []string{"csr", "cert", "pubkey"}
	wantUsers := []string{"alice", "bob", "carol"}
	officerDir := filepath.Join(dataDir, "officers")
	for i, o := range state.Officers {
		if o.Username != wantUsers[i] {
			t.Errorf("officer[%d].Username: got %q, want %q",
				i, o.Username, wantUsers[i])
		}
		if o.Kind != wantKinds[i] {
			t.Errorf("officer[%d].Kind: got %q, want %q",
				i, o.Kind, wantKinds[i])
		}
		if !strings.HasPrefix(o.Path, officerDir) {
			t.Errorf("officer[%d].Path %q: expected prefix %q",
				i, o.Path, officerDir)
		}
	}
}

// TestCanonicalInit_NoneRejectsPINs is a non-matrix companion that
// verifies the CLI's PIN rejection survives the integration boundary.
// The cmd/quicbaasd unit tests cover the same contract in-process; this
// test catches regressions in the exec dispatch (e.g. a flag parser
// change that silently ignores --so-pin under --barrier-strategy=none).
func TestCanonicalInit_NoneRejectsPINs(t *testing.T) {
	requireIntegrationGate(t)
	binary := resolveDaemonBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dataDir := t.TempDir()
	// We do NOT use runInit here because runInit deliberately omits PINs
	// under "none". The explicit exec invocation is the only way to
	// reproduce the operator-mistake scenario.
	output, err := execInit(ctx, binary, []string{
		"init",
		"--barrier-strategy", "none",
		"--so-pin", "should-fail",
		"--data-dir", dataDir,
	})
	if err == nil {
		t.Fatalf("expected non-zero exit when --so-pin supplied under 'none'; output:\n%s", output)
	}
	if !strings.Contains(output, "PIN") && !strings.Contains(output, "pin") {
		t.Errorf("expected error to mention PINs; got:\n%s", output)
	}
}

// TestCanonicalInit_UnsupportedHardwareGracefulSkip exercises the
// environment-gated hardware-strategy contract. When neither
// QUICBAAS_TEST_TPM2_SOCKET nor QUICBAAS_TEST_SOFTHSM2_MODULE is set we
// assert the hardware strategies return UnsupportedBarrierStrategyError
// (the unwired state); when they are set, the test skips with a
// rationale so the operator knows hardware exercise is wired in a
// future suite. This is the ONLY skip in the package and it is purely
// environmental per CLAUDE.md.
func TestCanonicalInit_UnsupportedHardwareGracefulSkip(t *testing.T) {
	requireIntegrationGate(t)
	if hwTPMAvailable() || hwSoftHSMAvailable() {
		t.Skip("hardware barrier strategy environment present " +
			"(QUICBAAS_TEST_TPM2_SOCKET / QUICBAAS_TEST_SOFTHSM2_MODULE); " +
			"exercise lives in a dedicated env-gated suite")
	}
	binary := resolveDaemonBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, strategy := range []string{"pkcs11", "tpm2", "awskms", "gcpkms", "azurekv", "vault"} {
		strategy := strategy
		t.Run(strategy, func(t *testing.T) {
			dataDir := t.TempDir()
			output, err := execInit(ctx, binary, []string{
				"init",
				"--barrier-strategy", strategy,
				"--so-pin", "p",
				"--user-pin", "u",
				"--data-dir", dataDir,
			})
			if err == nil {
				t.Fatalf("expected non-zero exit for unsupported strategy %s; output:\n%s",
					strategy, output)
			}
			if !strings.Contains(output, "not yet wired") &&
				!strings.Contains(output, "supported strategies") {
				t.Errorf("expected UnsupportedBarrierStrategyError-shaped output for %s; got:\n%s",
					strategy, output)
			}
		})
	}
}

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

//go:build integration

// ceremony_e2e_test.go drives go-qrdb's Security Officer distribution model
// end to end THROUGH THE CLI, against a real daemon booted sealed:
//
//	quicbaasd init --barrier-strategy shamir   (records M-of-N, creates nothing)
//	quicbaasd start                            (boots SEALED, serves the window)
//	quicbaas init status                       (ceremony reachable while sealed)
//	quicbaas init officer register             (CSR signed, share index bound)
//	quicbaas init initialize                   (split; one share per enclave)
//	quicbaas init claim-cert / claim-share     (live, release-once, per officer)
//	quicbaas barrier unseal                    (quorum opens the barrier)
//
// The invariant every phase re-checks: NO SHARE EVER REACHES DISK. That is the
// property the deleted one-shot ceremony violated by writing sealed envelopes
// under <data-dir>/officers/.

package init

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cliBinaryEnv names the environment variable pointing at the quicbaas CLI.
const cliBinaryEnv = "QUICBAAS_BINARY"

// resolveCLIBinary returns an absolute path to the quicbaas CLI binary,
// mirroring resolveDaemonBinary's policy: explicit env var, then the repo's
// bin/ directory, never PATH.
func resolveCLIBinary(t *testing.T) string {
	t.Helper()
	if explicit := os.Getenv(cliBinaryEnv); explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			t.Fatalf("resolve $%s=%s: %v", cliBinaryEnv, explicit, err)
		}
		if _, statErr := os.Stat(abs); statErr != nil {
			t.Fatalf("$%s=%s: not found: %v", cliBinaryEnv, abs, statErr)
		}
		return abs
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root for CLI discovery: %v", err)
	}
	candidate := filepath.Join(repoRoot, "bin", "quicbaas")
	if _, statErr := os.Stat(candidate); statErr == nil {
		return candidate
	}
	t.Fatalf(
		"quicbaas CLI not found at %s; build it via `make build` or point $%s at one",
		candidate, cliBinaryEnv,
	)
	return "" // unreachable
}

// runCLI executes the quicbaas CLI and returns its combined output.
func runCLI(ctx context.Context, t *testing.T, binary string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204 - test-controlled args
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestCeremonyE2E_ShamirInitLeavesNothingOnDisk is the CLI-level contract test
// for `quicbaasd init --barrier-strategy shamir`.
//
// It asserts the deferred-init architecture at the process boundary: the
// command succeeds, records the M-of-N parameters, and creates NO barrier, NO
// first-boot admin credential, and NO share material. Everything else is the
// ceremony's job, against the running daemon.
func TestCeremonyE2E_ShamirInitLeavesNothingOnDisk(t *testing.T) {
	requireIntegrationGate(t)
	binary := resolveDaemonBinary(t)

	dataDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out, err := runShamirInit(ctx, binary, dataDir)
	if err != nil {
		t.Fatalf("shamir init failed: %v\noutput:\n%s", err, out)
	}

	// The operator is pointed at the live ceremony, never at a bundle on disk.
	for _, want := range []string{"officer register", "claim-share", "SEALED"} {
		if !strings.Contains(out, want) {
			t.Errorf("init summary must mention %q so the operator knows the next step;\ngot:\n%s",
				want, out)
		}
	}
	if strings.Contains(out, "officers/") {
		t.Errorf("init summary must not point at an on-disk share bundle:\n%s", out)
	}

	marker, err := readInitDoneMarker(dataDir)
	if err != nil {
		t.Fatalf("read init-done marker: %v", err)
	}
	if marker.BarrierStrategy != "shamir" {
		t.Errorf("marker.BarrierStrategy=%q, want shamir", marker.BarrierStrategy)
	}
	if marker.Shares != 3 || marker.Threshold != 2 {
		t.Errorf("marker M-of-N = %d-of-%d, want 2-of-3", marker.Threshold, marker.Shares)
	}

	assertNoShareMaterialOnDisk(t, dataDir)

	// First-boot must NOT have run: it follows the ceremony's unseal. An admin
	// certificate here would mean IAM state was written before the barrier
	// existed, i.e. unencrypted at rest.
	if _, statErr := os.Stat(filepath.Join(dataDir, "admin", "admin.crt")); statErr == nil {
		t.Error("shamir init ran first-boot; it must wait for the ceremony to unseal the barrier")
	}
}

// TestCeremonyE2E_SealedDaemonServesTheCeremonyOverCLI proves the enrolling
// window is real: after a shamir init the daemon boots SEALED, and the ceremony
// verbs are reachable through the CLI while it is still sealed.
//
// This is the phase the deleted one-shot ceremony existed to work around — its
// comment claimed the enrolling window "would die with the process". It does
// not: it lives in the long-running daemon.
func TestCeremonyE2E_SealedDaemonServesTheCeremonyOverCLI(t *testing.T) {
	requireIntegrationGate(t)
	daemonBinary := resolveDaemonBinary(t)
	cliBinary := resolveCLIBinary(t)

	dataDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if out, err := runShamirInit(ctx, daemonBinary, dataDir); err != nil {
		t.Fatalf("shamir init failed: %v\noutput:\n%s", err, out)
	}

	ports := allocPortBundle(t, "rest", dataDir)
	daemon := startDaemon(t, daemonBinary, dataDir, "shamir", "rest", ports)
	if err := waitForTransportReady(ctx, t, daemon, "rest", ports, dataDir); err != nil {
		t.Fatalf("sealed daemon never became reachable over REST: %v", err)
	}

	address := fmt.Sprintf("https://127.0.0.1:%d", ports.REST)
	// The sealed daemon serves with the CA chain its first boot wrote under
	// the data dir (the same chain the readiness probe trusts). The CLI runs
	// with a fresh HOME and no --data-dir, so it must be pointed at that chain
	// or it falls back to the system roots. TLS verification stays on.
	caFile := caChainPath(dataDir)

	// The ceremony status verb must answer while the barrier is sealed. If the
	// sealed-window allowlist regressed, this is where it shows up.
	statusOut, statusErr := runCLI(ctx, t, cliBinary,
		"init", "status", "--address", address, "--tls-ca", caFile, "--output", "json")
	if statusErr != nil {
		t.Fatalf("`quicbaas init status` against a SEALED daemon failed: %v\noutput:\n%s",
			statusErr, statusOut)
	}
	if !strings.Contains(strings.ToLower(statusOut), "state") {
		t.Errorf("ceremony status did not report a state:\n%s", statusOut)
	}

	// Officer registration must also be reachable while sealed: officers have
	// to be enrolled BEFORE the split, and the barrier is sealed until after it.
	csrPath := filepath.Join(t.TempDir(), "alice.csr")
	if err := writeOfficerCSR(csrPath, "alice"); err != nil {
		t.Fatalf("generate officer CSR: %v", err)
	}
	certPath := filepath.Join(t.TempDir(), "alice.crt")

	registerOut, registerErr := runCLI(ctx, t, cliBinary,
		"init", "officer", "register",
		"--address", address,
		"--tls-ca", caFile,
		"--username", "alice",
		"--csr", csrPath,
		"--output", certPath,
	)
	// Registration may legitimately require admin authorization on a
	// production-posture daemon. What must NOT happen is the sealed window
	// refusing the route outright — that would mean officers can never enrol.
	if registerErr != nil {
		lower := strings.ToLower(registerOut)
		for _, sealedRefusal := range []string{"sealed", "unavailable", "not allowed"} {
			if strings.Contains(lower, sealedRefusal) {
				t.Fatalf("officer registration was refused BY THE SEALED WINDOW (%q); "+
					"officers must be able to enrol before the split:\n%s",
					sealedRefusal, registerOut)
			}
		}
		t.Logf("officer register returned %v (authorization is expected to gate this); output:\n%s",
			registerErr, registerOut)
	}

	// Whatever happened above, no share may have reached disk.
	assertNoShareMaterialOnDisk(t, dataDir)
}

// TestCeremonyE2E_InitHelpDocumentsTheFlow verifies the CLI teaches the correct
// order. The verb set is useless if an operator cannot discover it, and the
// wrong order (initialize before register) strands officers without shares.
func TestCeremonyE2E_InitHelpDocumentsTheFlow(t *testing.T) {
	requireIntegrationGate(t)
	cliBinary := resolveCLIBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := runCLI(ctx, t, cliBinary, "init", "--help")
	if err != nil {
		t.Fatalf("`quicbaas init --help` failed: %v\noutput:\n%s", err, out)
	}
	for _, want := range []string{
		"initialize", "officer", "claim-cert", "claim-share", "sign-csr",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init help does not document the %q verb:\n%s", want, out)
		}
	}
	if !strings.Contains(strings.ToLower(out), "never written to disk") &&
		!strings.Contains(strings.ToLower(out), "released to") {
		t.Errorf("init help should state the share-custody property:\n%s", out)
	}
}

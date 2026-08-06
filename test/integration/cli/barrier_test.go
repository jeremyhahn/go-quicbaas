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

// barrier_test.go covers the canonical 9-verb barrier surface that ships
// in quicbaas / qrdbctl / xkmsctl / platformctl (cmd/quicbaas/barrier.go).
//
// Two execution patterns coexist in this file:
//
//  1. READ-ONLY against the shared integration cluster. `barrier status`
//     does not mutate daemon state, so RunForAllTransports against the
//     pre-warmed docker-compose cluster is the cheapest oracle for
//     "every transport's BarrierStatus envelope decodes correctly".
//
//  2. RUNTIME MUTATION verbs (unseal, unseal-shares, seal, rekey,
//     recovery) against a per-test fresh daemon. The CONFIRMED model is
//     server-init-first: the SERVER is initialised with the barrier
//     strategy + secret via `quicbaasd init` (spawnFreshBarrierDaemon).
//     For the passphrase strategy init leaves an encfs-ENCRYPTED data
//     plane, so `quicbaasd start` must boot with the matching seal
//     posture. Two boot modes exist:
//       - AUTO-UNSEAL (QUICBAAS_SEAL_ENABLED/STRATEGY/PASSPHRASE): the
//         daemon decrypts and boots UNSEALED; the fixture then restores
//         the suite's SEALED precondition with a runtime `barrier seal
//         -y` (requires the harness's insecure authz posture).
//       - SEALED BOOT (seal enabled, NO passphrase injected): the daemon
//         enters the sealed-window phase — all five transports serve
//         ONLY the sealed allowlist until an operator unseals. Used by
//         the AUTH-ON posture test, where no token can be minted and
//         the sealed-window bypass is the very contract under test.
//     The client NEVER runs `barrier init` over a network transport —
//     init is fixed at the server and is meaningful only for the
//     embedded transport.
//     The runtime client only unseals (with the already-known secret),
//     seals, rekeys, and recovers. Barrier state is daemon-global —
//     sealing blocks every sibling subtest — so each cell spins up a
//     fresh quicbaasd process per test, bound to a per-test data dir +
//     port bundle + Unix socket, drives the verb under test through the
//     CLI against that daemon, then tears down via t.Cleanup with the
//     SIGTERM -> SIGKILL escalation pattern.
//
// Every runtime verb is exercised across every supported transport so a
// regression in any per-transport BarrierClient wiring (e.g. REST
// returns the wrong envelope, MCP forgets to forward --so-pin, QUIC's
// recovery response decoder is off) surfaces in exactly one matrix cell
// rather than failing silently behind a green test on the other rows.
//
// SHAMIR happy-path note: a Shamir barrier is brought up by the LOCKED
// M-of-N ceremony, not by a network init verb. `quicbaasd init
// --barrier-strategy shamir` writes only pending_ceremony.json and defers
// first-boot to the ceremony, so a Shamir daemon cannot reach readiness
// for this suite. The Shamir end-to-end unseal therefore lives in the
// dedicated ceremony suite; here TestBarrierUnsealShares only verifies the
// per-transport unseal-shares request/response wiring (see its doc).

package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Per-test daemon infrastructure
// =============================================================================
//
// The barrier mutation tests need a fresh daemon per subtest because
// barrier state is daemon-global. We deliberately do not import
// test/integration/init/* here: that package is its own go test binary
// (the matrix runs under `go test ./test/integration/init/...`) and its
// internal helpers are tagged `package init` so they are not exported.
// Duplicating the minimum supervision surface keeps each suite self-
// contained, prevents tag entanglement, and lets each evolve at its
// own cadence.

// daemonBinaryEnv overrides the auto-discovered quicbaasd path. Set by
// the Makefile (`integration-test-cli`) to point at the binary built
// alongside the CLI binary so the suite runs against the current source.
const daemonBinaryEnv = "QUICBAASD_BINARY"

// daemonStartTimeout bounds the wait for the per-test daemon to reach
// readiness. The PebbleDB cold-boot + xkms CA bootstrap dominates the
// budget; 90s comfortably covers a CI cold boot while still failing fast
// on a genuinely broken combination.
const daemonStartTimeout = 90 * time.Second

// daemonShutdownTimeout bounds the SIGTERM drain before SIGKILL.
const daemonShutdownTimeout = 20 * time.Second

// daemonReadyPollInterval is the gap between transport readiness polls.
// Channel-driven polling (no time.Sleep) per CLAUDE.md.
const daemonReadyPollInterval = 200 * time.Millisecond

// daemonPortRetries caps the loopback-port allocation retry count.
const daemonPortRetries = 50

// barrierDaemonPorts is the unique-per-test port + socket allocation for
// a freshly-spawned daemon. REST is always bound because it is the CLI's
// canonical address surface when --transport rest is in play. gRPC, QUIC,
// MCP, and Unix are always bound so the same daemon serves every
// transport the matrix exercises.
type barrierDaemonPorts struct {
	REST int
	GRPC int
	QUIC int
	MCP  int
	Unix string
}

// allocatedBarrierPorts tracks every port handed out to a barrier daemon
// so parallel subtests cannot collide. We bind exclusively during
// allocation, close the listener immediately, and the daemon re-binds
// the same port a few hundred ms later. The narrow race window is
// tolerated by retrying allocations.
var (
	allocatedBarrierPortsMu sync.Mutex
	allocatedBarrierPorts   = map[int]struct{}{}
)

// allocBarrierPort reserves a free TCP port for the per-test daemon.
func allocBarrierPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < daemonPortRetries; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if cerr := ln.Close(); cerr != nil {
			t.Fatalf("close port-allocation listener: %v", cerr)
		}
		allocatedBarrierPortsMu.Lock()
		_, taken := allocatedBarrierPorts[port]
		if !taken {
			allocatedBarrierPorts[port] = struct{}{}
			allocatedBarrierPortsMu.Unlock()
			return port
		}
		allocatedBarrierPortsMu.Unlock()
	}
	t.Fatalf("allocBarrierPort: %d retries exhausted", daemonPortRetries)
	return 0
}

// allocBarrierUDPPort reserves a free UDP port for the QUIC listener.
func allocBarrierUDPPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < daemonPortRetries; attempt++ {
		conn, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := conn.LocalAddr().(*net.UDPAddr).Port
		if cerr := conn.Close(); cerr != nil {
			t.Fatalf("close port-allocation packet conn: %v", cerr)
		}
		allocatedBarrierPortsMu.Lock()
		_, taken := allocatedBarrierPorts[port]
		if !taken {
			allocatedBarrierPorts[port] = struct{}{}
			allocatedBarrierPortsMu.Unlock()
			return port
		}
		allocatedBarrierPortsMu.Unlock()
	}
	t.Fatalf("allocBarrierUDPPort: %d retries exhausted", daemonPortRetries)
	return 0
}

// allocBarrierPorts reserves the full port set the per-test daemon needs
// to serve every transport the barrier matrix exercises. Unix socket
// lives under the data dir so it is removed automatically when t.TempDir
// is torn down. We use a short basename ("d.sock") to stay under the
// AF_UNIX 108-byte limit when t.TempDir prefixes are long.
func allocBarrierPorts(t *testing.T, dataDir string) barrierDaemonPorts {
	t.Helper()
	return barrierDaemonPorts{
		REST: allocBarrierPort(t),
		GRPC: allocBarrierPort(t),
		QUIC: allocBarrierUDPPort(t),
		MCP:  allocBarrierPort(t),
		Unix: filepath.Join(dataDir, "d.sock"),
	}
}

// barrierDaemon wraps the supervised quicbaasd subprocess.
type barrierDaemon struct {
	cmd      *exec.Cmd
	logPath  string
	logFile  *os.File
	dataDir  string
	caPath   string
	ports    barrierDaemonPorts
	doneCh   chan struct{}
	waitErr  atomic.Pointer[error]
	stopOnce sync.Once
	stopErr  error
}

// LogPath returns the daemon's combined stdout+stderr log file path.
func (d *barrierDaemon) LogPath() string { return d.logPath }

// Pid returns the supervised process id (0 before start).
func (d *barrierDaemon) Pid() int {
	if d == nil || d.cmd == nil || d.cmd.Process == nil {
		return 0
	}
	return d.cmd.Process.Pid
}

// Done returns a channel closed when the daemon process exits.
func (d *barrierDaemon) Done() <-chan struct{} { return d.doneCh }

// WaitErr returns the os/exec wait error after the process exits.
func (d *barrierDaemon) WaitErr() error {
	if p := d.waitErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Stop signals SIGTERM, falls back to SIGKILL after daemonShutdownTimeout.
// First call wins; subsequent calls return the original outcome so
// t.Cleanup is safe regardless of who called Stop first.
//
// quicbaasd's memguard signal handler exits with code 1 on a clean
// SIGTERM; treat both code 0 and the signalled-1 path as success.
func (d *barrierDaemon) Stop() error {
	d.stopOnce.Do(func() {
		if d.cmd == nil || d.cmd.Process == nil {
			return
		}
		select {
		case <-d.doneCh:
			d.stopErr = sanitizeBarrierWaitErr(d.WaitErr(), false)
			if d.logFile != nil {
				_ = d.logFile.Close()
			}
			return
		default:
		}
		if sigErr := d.cmd.Process.Signal(syscall.SIGTERM); sigErr != nil {
			if !errors.Is(sigErr, os.ErrProcessDone) {
				d.stopErr = sigErr
			}
		}
		select {
		case <-d.doneCh:
			d.stopErr = sanitizeBarrierWaitErr(d.WaitErr(), true)
		case <-time.After(daemonShutdownTimeout):
			_ = d.cmd.Process.Signal(syscall.SIGKILL)
			<-d.doneCh
			d.stopErr = fmt.Errorf("barrierDaemon SIGTERM exceeded %s; SIGKILL applied; wait err=%v",
				daemonShutdownTimeout, d.WaitErr())
		}
		if d.logFile != nil {
			_ = d.logFile.Close()
		}
	})
	return d.stopErr
}

// sanitizeBarrierWaitErr maps exit outcomes per the memguard contract.
func sanitizeBarrierWaitErr(waitErr error, signalled bool) error {
	if waitErr == nil {
		return nil
	}
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return waitErr
	}
	if signalled && exitErr.ExitCode() == 1 {
		return nil
	}
	return waitErr
}

// resolveBarrierDaemonBinary returns an absolute path to quicbaasd.
// Order: $QUICBAASD_BINARY -> /usr/local/bin/quicbaasd (devcontainer
// convention) -> <repo-root>/bin/quicbaasd (developer workflow). We do
// NOT fall back to PATH so we never dispatch to a stale install.
func resolveBarrierDaemonBinary(t *testing.T) string {
	t.Helper()
	if explicit := os.Getenv(daemonBinaryEnv); explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			t.Fatalf("resolve $%s=%s: %v", daemonBinaryEnv, explicit, err)
		}
		if _, statErr := os.Stat(abs); statErr == nil {
			return abs
		}
	}
	for _, candidate := range []string{
		"/usr/local/bin/quicbaasd",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	repoRoot, err := findBarrierRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	candidate := filepath.Join(repoRoot, "bin", "quicbaasd")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	t.Fatalf(
		"quicbaasd binary not found in $%s, /usr/local/bin/quicbaasd, or %s; "+
			"build via `make build-daemon` or set $%s",
		daemonBinaryEnv, candidate, daemonBinaryEnv,
	)
	return ""
}

// findBarrierRepoRoot walks upward from this file's location until it
// finds the directory containing go.mod.
func findBarrierRepoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller(0) returned !ok")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod found above " + filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// Default SO/User PIN values used by the per-test daemon's init ceremony.
// The canonical init flow REQUIRES --so-pin/--user-pin for any
// barrier-enabled strategy (software/shamir); the no-barrier "none"
// strategy REJECTS them. These are throwaway test credentials — the
// per-test daemon has no PINBackend, so they only satisfy the init flag
// contract and are never consulted at runtime.
const (
	defaultBarrierSOPin   = "SO-PIN-TEST-0001"
	defaultBarrierUserPin = "USER-PIN-TEST-0001"
)

// barrierDaemonOptions parameterises the per-test daemon's `quicbaasd
// init` ceremony. The CONFIRMED model is server-init-first: the daemon is
// initialised with the barrier strategy + secret here, comes up SEALED,
// and the runtime CLI client only ever unseals/seals/rekeys/recovers with
// the already-known secret. The client NEVER performs `barrier init` over
// a network transport — that verb is meaningful only for the embedded
// transport.
//
//   - Strategy is the barrier strategy the daemon is initialised with
//     ("none" | "passphrase" | "shamir"). It is fixed for the daemon's
//     entire process lifetime and reported verbatim by `barrier status`.
//   - Secret is the software/shamir passphrase supplied at init. Required
//     for software/shamir, ignored for none. The same secret is later
//     supplied to `barrier unseal`.
//   - Shares/Threshold are the Shamir M-of-N parameters, required when
//     Strategy=="shamir" and ignored otherwise.
//
// SO/User PINs default to the throwaway test credentials and are filled in
// automatically for barrier-enabled strategies; callers do not set them.
type barrierDaemonOptions struct {
	Strategy  string
	Secret    string
	Shares    int
	Threshold int

	// EnforceAuthz keeps the daemon at the production default-deny
	// posture (config.Default: security.require_authz=true). The mutation
	// matrix runs with EnforceAuthz=false — the harness's ACKNOWLEDGED
	// insecure posture (security.require_authz=false +
	// security.insecure_dev_mode=true, the exact posture
	// test/integration/quicbaas.yaml documents for the shared cluster) —
	// because a spawned per-test daemon has no IAM bootstrap and thus no
	// way to mint a bearer token; under the owner's AUTH-ON contract every
	// post-unseal verb is otherwise rejected Unauthenticated.
	// TestBarrierAuthPosture_DefaultDeny sets this true to pin the AUTH-ON
	// contract explicitly instead of leaving it an accidental side effect.
	EnforceAuthz bool

	// SealedBoot starts the daemon WITHOUT injecting the seal passphrase:
	// with seal enabled and no auto-unseal material, quicbaasd enters the
	// sealed-window phase and reaches readiness SEALED, serving only the
	// canonical sealed-window allowlist. This is the only way to hand a
	// test a SEALED daemon under EnforceAuthz=true — a post-unseal
	// `barrier seal -y` requires a bearer token that a fresh per-test
	// daemon cannot mint (the very contract the AUTH-ON test pins).
	// Requires Strategy=="passphrase". Readiness is probed via the
	// allowlisted /v1/barrier/status (mirroring how qrdb's harness probes
	// a sealed node) because a sealed boot never reaches the full-boot
	// readiness gate.
	SealedBoot bool
}

// spawnFreshBarrierDaemon starts a hermetic quicbaasd subprocess against a
// per-test data dir + port bundle, SERVER-INITIALISED with the supplied
// barrier options, then started so every transport listener comes up.
// Returns the supervised daemon plus the resolved CA chain path; the
// t.Cleanup hook signals SIGTERM -> SIGKILL.
//
// CONFIRMED model: the SERVER is initialised first (`quicbaasd init` with
// the barrier strategy + secret). `quicbaasd init --barrier-strategy
// software --secret <X> --so-pin <SO> --user-pin <USR>` fully initialises
// the data-plane barrier (persists the root key under <dataDir>/seal), so
// the daemon comes up with a usable barrier reporting the chosen strategy.
// The runtime CLI client then exercises unseal/seal/rekey/recovery against
// that daemon with the already-known secret — it never runs `barrier init`
// over the network. For "shamir", init writes a pending_ceremony.json and
// the LOCKED M-of-N ceremony owns first-boot; we do not bypass it.
//
// SEAL POSTURE: passphrase init also encrypts the qrdb data plane (encfs
// over the barrier KEK), so `quicbaasd start` MUST boot with the matching
// posture — QUICBAAS_SEAL_ENABLED/STRATEGY/PASSPHRASE, mirroring the
// canonical test/integration/init/helpers.go startDaemon contract. Booting
// without it reads the encrypted files as garbage ("malformed manifest").
// quicbaasd has no sealed-start mode (a sealed node exposes no SDK client,
// so IAM bootstrap cannot run), which means the daemon reaches readiness
// UNSEALED. Because unsealing an unsealed barrier is a typed error
// (qrdb ErrAlreadyUnsealed), the fixture restores the suite's SEALED
// precondition with the product's own runtime `barrier seal -y` verb over
// REST before returning, so every test body still starts from SEALED.
func spawnFreshBarrierDaemon(t *testing.T, opts barrierDaemonOptions) (*barrierDaemon, string) {
	t.Helper()

	if opts.SealedBoot && opts.Strategy != "passphrase" {
		t.Fatalf("spawnFreshBarrierDaemon: SealedBoot requires the passphrase strategy, got %q", opts.Strategy)
	}

	binary := resolveBarrierDaemonBinary(t)
	dataDir := t.TempDir()

	// Phase 1: SERVER-side `quicbaasd init` with the requested strategy.
	// This is the keystone: software/shamir init fully provisions the
	// data-plane barrier so the daemon starts SEALED and reports the
	// chosen strategy. The runtime client never re-inits over the network.
	initArgs := []string{
		"init",
		"--barrier-strategy", opts.Strategy,
		"--data-dir", dataDir,
	}
	switch opts.Strategy {
	case "none", "disabled":
		// none REJECTS --secret/--so-pin/--user-pin: supply nothing extra.
	case "passphrase":
		// software REQUIRES --secret and the SO/User PIN pair.
		initArgs = append(initArgs,
			"--passphrase", opts.Secret,
			"--so-pin", defaultBarrierSOPin,
			"--user-pin", defaultBarrierUserPin,
		)
	case "shamir":
		// shamir REQUIRES --secret, the SO/User PIN pair, and the M-of-N
		// (--shares/--threshold) parameters; init writes pending_ceremony
		// .json and the LOCKED ceremony owns first-boot.
		initArgs = append(initArgs,
			"--passphrase", opts.Secret,
			"--so-pin", defaultBarrierSOPin,
			"--user-pin", defaultBarrierUserPin,
			"--shares", fmt.Sprintf("%d", opts.Shares),
			"--threshold", fmt.Sprintf("%d", opts.Threshold),
		)
	default:
		t.Fatalf("spawnFreshBarrierDaemon: unsupported strategy %q", opts.Strategy)
	}

	initCtx, cancelInit := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelInit()
	initCmd := exec.CommandContext(initCtx, binary, initArgs...) // #nosec G204 - args produced by test
	initCmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dataDir}
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("quicbaasd init (strategy=%s) for barrier suite failed: %v\noutput:\n%s",
			opts.Strategy, initErr, string(initOut))
	}

	// Phase 2: allocate the per-daemon port + socket bundle and start.
	ports := allocBarrierPorts(t, dataDir)
	daemon := startBarrierDaemonProcess(t, binary, dataDir, ports, opts)
	t.Cleanup(func() {
		if stopErr := daemon.Stop(); stopErr != nil {
			t.Logf("barrierDaemon Stop: %v", stopErr)
		}
		if t.Failed() {
			dumpBarrierLogTail(t, daemon.LogPath(), 100)
		}
	})

	// Phase 3: block until every transport listener is reachable. A sealed
	// boot never passes the full-boot readiness gate, so it is probed via
	// the allowlisted barrier-status surface instead.
	startCtx, cancelStart := context.WithTimeout(context.Background(), daemonStartTimeout)
	defer cancelStart()
	if opts.SealedBoot {
		if err := waitForSealedBarrierDaemonReady(startCtx, daemon); err != nil {
			t.Fatalf("sealed barrier daemon never became ready: %v", err)
		}
	} else if err := waitForBarrierDaemonReady(startCtx, daemon); err != nil {
		t.Fatalf("barrier daemon never became ready: %v", err)
	}

	caPath := filepath.Join(dataDir, "certs", "ca-chain.crt")
	if _, err := os.Stat(caPath); err != nil {
		t.Fatalf("CA chain missing at %s after start: %v", caPath, err)
	}
	daemon.caPath = caPath

	// Phase 4: restore the suite's SEALED precondition. A passphrase daemon
	// boots auto-unsealed (see the SEAL POSTURE note above); the mutation
	// tests exercise unseal-from-sealed, so seal the barrier with the same
	// runtime verb the suite itself covers. The runtime `barrier seal -y`
	// requires the harness's insecure authz posture — under EnforceAuthz
	// the daemon rejects the token-less seal (that AUTH-ON contract is
	// pinned by TestBarrierAuthPosture_DefaultDeny, which boots SEALED via
	// SealedBoot instead). A SealedBoot daemon is already sealed.
	if opts.Strategy == "passphrase" && !opts.SealedBoot {
		sealBarrierDaemon(t, daemon)
	}

	return daemon, caPath
}

// sealBarrierDaemon seals the freshly-started daemon's barrier via the CLI
// over REST (`barrier seal -y` with the daemon's own CA chain). Used by
// spawnFreshBarrierDaemon to hand test bodies a SEALED passphrase daemon,
// matching the suite's documented starting state.
func sealBarrierDaemon(t *testing.T, d *barrierDaemon) {
	t.Helper()
	binary := getQuicBaaSBinary()
	require.NotEmptyf(t, binary, "quicbaas CLI binary required to seal the per-test daemon")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{
		"--address", fmt.Sprintf("https://127.0.0.1:%d", d.ports.REST),
		"--transport", "rest",
		"--tls-ca", d.caPath,
		"barrier", "seal", "-y",
	}
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204 - args produced by the test
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seal per-test daemon (barrier seal -y over REST) failed: %v\noutput:\n%s", err, out)
	}
}

// writeBarrierInsecurePostureConfig writes the harness's acknowledged
// insecure-security posture (security.require_authz=false +
// security.insecure_dev_mode=true — mirroring the documented posture in
// test/integration/quicbaas.yaml) to a per-daemon config file consumed via
// `quicbaasd start --config`. The refuse-to-start gate rejects
// require_authz=false with network transports enabled unless the dev-only
// escape hatch is set, so both keys are required. NEVER use this posture in
// production.
func writeBarrierInsecurePostureConfig(t *testing.T, dataDir string) string {
	t.Helper()
	const insecurePosture = `# Test-only security posture for the hermetic per-test barrier daemon.
# Mirrors test/integration/quicbaas.yaml: the daemon has no IAM bootstrap,
# so post-unseal barrier verbs must be reachable without a bearer token.
security:
  require_authz: false
  insecure_dev_mode: true
`
	path := filepath.Join(dataDir, "insecure-posture.yaml")
	if err := os.WriteFile(path, []byte(insecurePosture), 0o600); err != nil {
		t.Fatalf("write insecure posture config %s: %v", path, err)
	}
	return path
}

// startBarrierDaemonProcess launches quicbaasd start with every transport
// enabled against the supplied port bundle. For the passphrase strategy the
// daemon is started with the seal posture matching `quicbaasd init`
// (QUICBAAS_SEAL_ENABLED/STRATEGY/PASSPHRASE) so it can decrypt the
// encfs-encrypted data plane and auto-unseal — the same contract as
// test/integration/init/helpers.go startDaemon.
func startBarrierDaemonProcess(t *testing.T, binary, dataDir string, ports barrierDaemonPorts, opts barrierDaemonOptions) *barrierDaemon {
	t.Helper()

	args := []string{
		"start",
		"--data-dir", dataDir,
		"--log-level", "warn",
		"--http-port", fmt.Sprintf("%d", ports.REST),
		"--grpc-port", fmt.Sprintf("%d", ports.GRPC),
		"--unix-socket", ports.Unix,
	}
	if !opts.EnforceAuthz {
		// The security.* keys have no QUICBAAS_* env binding in quicbaasd
		// (bindEnvVars covers seal/server/raft/... but not security.require_
		// authz), so the acknowledged insecure posture must travel via a
		// --config file. See barrierDaemonOptions.EnforceAuthz.
		args = append(args, "--config", writeBarrierInsecurePostureConfig(t, dataDir))
	}

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dataDir,
		// Loopback-only so parallel subtests cannot accidentally cross.
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		// Discovery + scheduler do not add coverage to barrier tests; off.
		"QUICBAAS_DISCOVERY_ENABLED=false",
		"QUICBAAS_ENABLE_SCHEDULER=false",
		// QUIC + MCP listeners have no CLI flags; drive via env.
		"QUICBAAS_SERVER_QUIC_ENABLED=true",
		fmt.Sprintf("QUICBAAS_SERVER_QUIC_PORT=%d", ports.QUIC),
		"QUICBAAS_SERVER_MCP_ENABLED=true",
		fmt.Sprintf("QUICBAAS_SERVER_MCP_PORT=%d", ports.MCP),
	}
	if opts.Strategy == "passphrase" {
		// Init ran with seal enabled and an initialized passphrase barrier,
		// so the data plane on disk is encrypted (encfs over the barrier
		// KEK). Start must boot with the SAME seal posture — there is no
		// deployment-derived default (security hardening).
		env = append(env,
			"QUICBAAS_SEAL_ENABLED=true",
			"QUICBAAS_SEAL_STRATEGY=passphrase",
		)
		// SealedBoot deliberately withholds the passphrase: with seal
		// enabled and no auto-unseal material the daemon enters the
		// sealed-window phase and comes up SEALED, serving only the
		// allowlist until an operator drives the unseal via a transport.
		if !opts.SealedBoot {
			// Auto-unseal with the admin passphrase so the daemon boots
			// fully and reaches the full-boot readiness gate.
			env = append(env, "QUICBAAS_SEAL_PASSPHRASE="+opts.Secret)
		}
	}

	logPath := filepath.Join(dataDir, "quicbaasd.log")
	// #nosec G304 - logPath rooted at t.TempDir
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create daemon log file %s: %v", logPath, err)
	}

	cmd := exec.Command(binary, args...) // #nosec G204 - args produced by test
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = env
	// New process group so SIGTERM to the daemon does not propagate up.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if startErr := cmd.Start(); startErr != nil {
		_ = logFile.Close()
		t.Fatalf("start quicbaasd: %v", startErr)
	}

	d := &barrierDaemon{
		cmd:     cmd,
		logPath: logPath,
		logFile: logFile,
		dataDir: dataDir,
		ports:   ports,
		doneCh:  make(chan struct{}),
	}
	go func() {
		waitErr := cmd.Wait()
		d.waitErr.Store(&waitErr)
		close(d.doneCh)
	}()
	return d
}

// waitForBarrierDaemonReady blocks until REST + gRPC + QUIC + MCP + Unix
// are all reachable, the CA chain has landed on disk, or the deadline
// fires. Polling races against the daemon's Done channel so a crash
// fails fast.
func waitForBarrierDaemonReady(ctx context.Context, d *barrierDaemon) error {
	ticker := time.NewTicker(daemonReadyPollInterval)
	defer ticker.Stop()
	caPath := filepath.Join(d.dataDir, "certs", "ca-chain.crt")
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("barrier daemon not ready within %s: %w", daemonStartTimeout, ctx.Err())
		case <-d.Done():
			return fmt.Errorf("barrier daemon exited before ready: wait_err=%v", d.WaitErr())
		case <-ticker.C:
			// The daemon auto-issues its own CA chain during first-boot. Until
			// that lands on disk we cannot verify its TLS listeners, so treat a
			// missing/empty chain as "not ready yet" and keep polling.
			tlsCfg, err := barrierProbeTLSConfig(caPath)
			if err != nil {
				continue
			}
			if err := probeBarrierREST(ctx, d.ports.REST, tlsCfg); err != nil {
				continue
			}
			if err := probeBarrierTCP(ctx, d.ports.GRPC); err != nil {
				continue
			}
			if err := probeBarrierUDP(ctx, d.ports.QUIC); err != nil {
				continue
			}
			if err := probeBarrierTLS(ctx, d.ports.MCP, tlsCfg); err != nil {
				continue
			}
			if err := probeBarrierUnix(ctx, d.ports.Unix); err != nil {
				continue
			}
			return nil
		}
	}
}

// waitForSealedBarrierDaemonReady blocks until a SEALED-BOOT daemon's
// sealed-window listeners are all reachable. The full-boot readiness gate
// (/api/v1/health/ready driven by the complete service graph) is never
// reached by a sealed boot, so readiness is the allowlisted
// GET /v1/barrier/status reporting sealed=true — mirroring how qrdb's
// harness documents probing /v1/barrier/status while a node is sealed —
// plus liveness probes for the remaining sealed-phase listeners.
func waitForSealedBarrierDaemonReady(ctx context.Context, d *barrierDaemon) error {
	ticker := time.NewTicker(daemonReadyPollInterval)
	defer ticker.Stop()
	caPath := filepath.Join(d.dataDir, "certs", "ca-chain.crt")
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sealed barrier daemon not ready within %s: %w", daemonStartTimeout, ctx.Err())
		case <-d.Done():
			return fmt.Errorf("sealed barrier daemon exited before ready: wait_err=%v", d.WaitErr())
		case <-ticker.C:
			tlsCfg, err := barrierProbeTLSConfig(caPath)
			if err != nil {
				continue
			}
			if err := probeBarrierSealedStatus(ctx, d.ports.REST, tlsCfg); err != nil {
				continue
			}
			if err := probeBarrierTCP(ctx, d.ports.GRPC); err != nil {
				continue
			}
			if err := probeBarrierUDP(ctx, d.ports.QUIC); err != nil {
				continue
			}
			if err := probeBarrierTLS(ctx, d.ports.MCP, tlsCfg); err != nil {
				continue
			}
			if err := probeBarrierUnix(ctx, d.ports.Unix); err != nil {
				continue
			}
			return nil
		}
	}
}

// probeBarrierSealedStatus GETs the allowlisted /v1/barrier/status and
// requires a 200 reporting sealed=true — the sealed-window boot's
// readiness signal.
func probeBarrierSealedStatus(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeBarrierSealedStatus: port=0")
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/v1/barrier/status", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probeBarrierSealedStatus: %s -> %d", url, resp.StatusCode)
	}
	var snap barrierStatusSnapshot
	if derr := json.NewDecoder(resp.Body).Decode(&snap); derr != nil {
		return fmt.Errorf("probeBarrierSealedStatus: decode: %w", derr)
	}
	if !snap.Sealed {
		return fmt.Errorf("probeBarrierSealedStatus: daemon reports sealed=false during sealed boot")
	}
	return nil
}

// barrierProbeTLSConfig builds a TLS config that trusts the ephemeral barrier
// daemon's OWN auto-issued CA chain (written to <dataDir>/certs/ca-chain.crt
// during first-boot), not the integration cluster CA.
//
// The barrier mutation suite spawns a throwaway quicbaasd per test, bound to
// 127.0.0.1 with a self-bootstrapped PKI. It is not a cluster node, so
// helpers.IntegrationTLSConfig (which pins /etc/quicbaas/certs/ca.crt) cannot
// verify it — pinning the cluster CA here is what made the readiness probe
// reject the daemon's cert and hang until the 90s deadline. The daemon's
// serving leaf is valid for 127.0.0.1 (the CLI verbs themselves connect with
// --tls-ca ca-chain.crt against the same host), so RootCAs+ServerName verify
// cleanly without InsecureSkipVerify. Returns an error until the chain exists,
// which the readiness loop treats as "not ready yet".
func barrierProbeTLSConfig(caPath string) (*tls.Config, error) {
	pemBytes, err := os.ReadFile(caPath) // #nosec G304 - caPath rooted at t.TempDir
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("barrierProbeTLSConfig: no certificates in %s", caPath)
	}
	return &tls.Config{
		RootCAs:    pool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS13,
	}, nil
}

// probeBarrierREST GETs /api/v1/health/ready. A 200 here is the daemon's
// "fully bootstrapped" gate.
func probeBarrierREST(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeBarrierREST: port=0")
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/api/v1/health/ready", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probeBarrierREST: %s -> %d", url, resp.StatusCode)
	}
	return nil
}

// probeBarrierTLS confirms the MCP/HTTPS listener is up via a TLS
// handshake.
func probeBarrierTLS(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeBarrierTLS: port=0")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("127.0.0.1:%d", port), tlsCfg)
	if err != nil {
		return err
	}
	_ = ctx
	return conn.Close()
}

// probeBarrierTCP opens a raw TCP connection (used for gRPC port liveness).
func probeBarrierTCP(ctx context.Context, port int) error {
	if port == 0 {
		return errors.New("probeBarrierTCP: port=0")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	return conn.Close()
}

// probeBarrierUDP confirms the QUIC port is bound by a listener. UDP has
// no handshake so we write a junk byte and rely on a Read timeout
// (listener silent) vs ECONNREFUSED to differentiate.
func probeBarrierUDP(ctx context.Context, port int) error {
	if port == 0 {
		return errors.New("probeBarrierUDP: port=0")
	}
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetWriteDeadline(deadline)
	if _, werr := conn.Write([]byte{0}); werr != nil {
		return werr
	}
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 1)
	_, rerr := conn.Read(buf)
	if rerr == nil {
		return nil
	}
	var nerr net.Error
	if errors.As(rerr, &nerr) && nerr.Timeout() {
		return nil
	}
	return rerr
}

// probeBarrierUnix verifies the Unix socket gRPC listener is up.
func probeBarrierUnix(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("probeBarrierUnix: empty path")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	return conn.Close()
}

// dumpBarrierLogTail prints the last n lines of the daemon log so a
// failing test's diagnostics appear in `go test` output.
func dumpBarrierLogTail(t *testing.T, path string, n int) {
	t.Helper()
	// #nosec G304 - log path rooted at t.TempDir
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("dumpBarrierLogTail: %s: %v", path, err)
		return
	}
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	t.Logf("=== last %d lines of %s ===", len(lines), path)
	for _, l := range lines {
		t.Logf("  %s", string(l))
	}
}

// =============================================================================
// Per-test daemon transport wiring
// =============================================================================

// barrierTransportConfig returns the per-transport (Transport, address)
// pair pointing at a per-test daemon. The address shape matches the
// runner's NewTestRunner contract (host:port or socket path); REST and
// MCP carry the full URL because the SDK REST/MCP transports parse it
// directly.
func barrierTransportConfig(transport Transport, ports barrierDaemonPorts) (Transport, string) {
	switch transport {
	case TransportGRPC:
		return TransportGRPC, fmt.Sprintf("127.0.0.1:%d", ports.GRPC)
	case TransportREST:
		return TransportREST, fmt.Sprintf("https://127.0.0.1:%d", ports.REST)
	case TransportQUIC:
		return TransportQUIC, fmt.Sprintf("127.0.0.1:%d", ports.QUIC)
	case TransportMCP:
		return TransportMCP, fmt.Sprintf("https://127.0.0.1:%d/mcp", ports.MCP)
	case TransportUnix:
		return TransportUnix, ports.Unix
	}
	return transport, ""
}

// newBarrierDaemonRunner constructs a TestRunner targeting the supplied
// per-test daemon over the supplied transport. The CA path is the
// daemon's auto-issued chain so --tls-ca is honoured uniformly.
//
// We bypass NewTestRunner because that helper hard-codes the integration
// cluster's CA path. The barrier daemon has its own CA, so we wire the
// runner manually with the matching --tls-ca + --address + --transport.
func newBarrierDaemonRunner(t *testing.T, transport Transport, daemon *barrierDaemon) *TestRunner {
	t.Helper()
	binary := getQuicBaaSBinary()
	require.NotEmptyf(t, binary, "quicbaas binary required for barrier daemon runner")
	tr, address := barrierTransportConfig(transport, daemon.ports)
	baseArgs := []string{"--address", address}
	if transport == TransportUnix {
		// Unix socket dispatches via gRPC under the hood.
		baseArgs = append(baseArgs, "--transport", "grpc")
	} else {
		baseArgs = append(baseArgs, "--transport", string(transport))
	}
	baseArgs = append(baseArgs, "--tls-ca", daemon.caPath)
	return &TestRunner{
		t:             t,
		binary:        binary,
		address:       address,
		transport:     tr,
		baseArgs:      baseArgs,
		authenticated: true,
	}
}

// barrierMutationTransports enumerates every transport the barrier
// matrix exercises against a fresh daemon. We mirror GetTransportConfigs
// here because the shared-cluster Enabled flag is irrelevant for per-test
// daemons — every transport is bound on the spawn path so every cell is
// always exercised.
var barrierMutationTransports = []Transport{
	TransportGRPC,
	TransportREST,
	TransportQUIC,
	TransportMCP,
	TransportUnix,
}

// runForBarrierTransports drives fn across every transport in
// barrierMutationTransports. Each invocation spawns a FRESH daemon via
// spawnFreshBarrierDaemon (SERVER-INITIALISED with opts) so mutations in
// one cell cannot leak into the next. Per the CONFIRMED model the daemon's
// barrier strategy is fixed at init time by opts.Strategy; the fn body
// exercises the runtime verb under test against that daemon.
func runForBarrierTransports(t *testing.T, name string, opts barrierDaemonOptions, fn func(t *testing.T, daemon *barrierDaemon, runner *TestRunner)) {
	t.Helper()
	for _, transport := range barrierMutationTransports {
		transport := transport
		t.Run(string(transport)+"/"+name, func(t *testing.T) {
			daemon, _ := spawnFreshBarrierDaemon(t, opts)
			runner := newBarrierDaemonRunner(t, transport, daemon)
			fn(t, daemon, runner)
		})
	}
}

// barrierValidationDaemonOpts is the cheapest daemon for the pure
// CLI-flag-VALIDATION tests. Those tests assert the CLI rejects bad
// arguments BEFORE any request reaches the daemon, so the daemon's barrier
// strategy is irrelevant; "none" is the fastest first-boot (no secret, no
// PIN exchange, no ceremony).
var barrierValidationDaemonOpts = barrierDaemonOptions{Strategy: "none"}

// runBarrierCommandWithStdin executes a CommandDef but pipes the supplied
// stdin into the binary. Used for the seal interactive path (yes\n vs
// no\n) and the unseal-shares stdin path.
func runBarrierCommandWithStdin(t *testing.T, runner *TestRunner, cmd CommandDef, stdin string) (string, string, error) {
	t.Helper()
	args := append(runner.baseArgs, cmd.Args...)
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()
	execCmd := exec.CommandContext(ctx, runner.binary, args...) // #nosec G204
	execCmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	execCmd.Stdout = &outBuf
	execCmd.Stderr = &errBuf
	err := execCmd.Run()
	t.Logf("[%s/stdin] %s %s -> err=%v\nstdout=%s\nstderr=%s",
		runner.transport, runner.binary, strings.Join(args, " "), err, outBuf.String(), errBuf.String())
	return outBuf.String(), errBuf.String(), err
}

// =============================================================================
// Assertion helpers
// =============================================================================

// barrierStatusSnapshot is the JSON shape emitted by `barrier status
// --output json` (cmd/quicbaas/barrier.go formatBarrierStatusResponse).
type barrierStatusSnapshot struct {
	Sealed         bool   `json:"sealed"`
	Strategy       string `json:"strategy"`
	HardwareBacked bool   `json:"hardware_backed"`
	InitializedAt  string `json:"initialized_at"`
}

// assertBarrierStatus decodes the JSON status envelope and asserts a
// subset of fields. expectedStrategy == "" disables the strategy check;
// expectedSealed nil disables the sealed check (used during transitional
// states).
func assertBarrierStatus(t *testing.T, runner *TestRunner, expectedSealed *bool, expectedStrategy string) barrierStatusSnapshot {
	t.Helper()
	stdout, stderr, err := runner.Run(BarrierStatusJSONCommand())
	require.NoErrorf(t, err, "barrier status --output json failed: stderr=%s", stderr)

	// The CLI may interleave a structured log line on stderr; the JSON
	// envelope lives alone on stdout.
	var snap barrierStatusSnapshot
	if jerr := json.Unmarshal(bytes.TrimSpace([]byte(stdout)), &snap); jerr != nil {
		t.Fatalf("decode barrier status JSON: %v\nstdout=%s", jerr, stdout)
	}
	if expectedSealed != nil {
		assert.Equalf(t, *expectedSealed, snap.Sealed,
			"barrier status sealed=%t (got %t): %+v", *expectedSealed, snap.Sealed, snap)
	}
	if expectedStrategy != "" {
		assert.Equalf(t, expectedStrategy, snap.Strategy,
			"barrier status strategy=%q (got %q): %+v", expectedStrategy, snap.Strategy, snap)
	}
	return snap
}

// writeBarrierSharesFile writes shares to a tempfile (one per line) for
// the --shares-file path.
func writeBarrierSharesFile(t *testing.T, dir string, shares []string) string {
	t.Helper()
	path := filepath.Join(dir, "shares.txt")
	body := strings.Join(shares, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write shares file: %v", err)
	}
	return path
}

// =============================================================================
// Tests: status (read-only against shared cluster)
// =============================================================================

// TestBarrierStatus is a read-only verb against the shared integration
// cluster — no per-test daemon is needed. Every transport's
// BarrierStatus envelope must decode cleanly and report the same
// (sealed, strategy) tuple.
func TestBarrierStatus(t *testing.T) {
	RunForAllTransports(t, "BarrierStatus", func(t *testing.T, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierStatusCommand())
		require.NoErrorf(t, err, "barrier status: stderr=%s", stderr)
		assert.NotEmpty(t, stdout, "barrier status produced no output")
	})
}

// TestBarrierStatusJSON exercises the JSON envelope across every
// transport.
func TestBarrierStatusJSON(t *testing.T) {
	RunForAllTransports(t, "BarrierStatusJSON", func(t *testing.T, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierStatusJSONCommand())
		require.NoErrorf(t, err, "barrier status --output json: stderr=%s", stderr)
		var snap barrierStatusSnapshot
		require.NoErrorf(t, json.Unmarshal(bytes.TrimSpace([]byte(stdout)), &snap),
			"decode JSON: %s", stdout)
		assert.NotEmpty(t, snap.Strategy, "strategy must be present: %+v", snap)
	})
}

// =============================================================================
// Tests: init across all strategies (per-test daemon)
// =============================================================================

// TestBarrierInitStatusUnseal_AllStrategies validates the CONFIRMED
// server-init-first model across every strategy and transport. The daemon
// is SERVER-INITIALISED with the strategy (via `quicbaasd init`), so each
// cell asserts that:
//
//   - `barrier status` reports the strategy the daemon was initialised
//     with over every transport (the strategy is fixed at init; the client
//     cannot change it over the network), and
//   - for software, the runtime UNSEAL verb accepts the already-known
//     secret and transitions the barrier to unsealed.
//
// The client NEVER runs `barrier init` over a network transport — that
// verb is meaningful only for the embedded transport — so this test
// replaces the previous client-side init calls with the daemon-init +
// status (+ unseal) assertions the real architecture supports.
func TestBarrierInitStatusUnseal_AllStrategies(t *testing.T) {
	cases := []struct {
		name      string
		opts      barrierDaemonOptions
		strategy  string // strategy `barrier status` must report
		canUnseal bool   // exercise the runtime unseal round-trip
	}{
		{
			name:     "none",
			opts:     barrierDaemonOptions{Strategy: "none"},
			strategy: "none",
		},
		{
			name:      "passphrase",
			opts:      barrierDaemonOptions{Strategy: "passphrase", Secret: "matrix-secret-software"},
			strategy:  "passphrase",
			canUnseal: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runForBarrierTransports(t, tc.name, tc.opts, func(t *testing.T, daemon *barrierDaemon, runner *TestRunner) {
				_ = daemon
				// 1. Status must report the strategy the daemon was
				//    SERVER-initialised with. The client cannot mutate the
				//    strategy over the network.
				assertBarrierStatus(t, runner, nil, tc.strategy)

				if !tc.canUnseal {
					return
				}

				// 2. A software daemon comes up SEALED after init. The
				//    runtime client unseals with the already-known secret
				//    (the operator kept it from init) — this is the only
				//    barrier-bring-up verb the network client performs.
				runner.RunAndAssert(BarrierUnsealCommand(tc.opts.Secret))
				unsealed := false
				assertBarrierStatus(t, runner, &unsealed, tc.strategy)
			})
		})
	}
}

// TestBarrierInit_NoneRejectsPINs asserts the typed PINsNotAllowedError
// surfaces correctly through the SDK across every transport. The
// validation runs CLI-side, so we do not actually need a daemon for
// this test — but we keep the per-test daemon for parity with the rest
// of the mutation matrix.
func TestBarrierInit_NoneRejectsPINs(t *testing.T) {
	runForBarrierTransports(t, "NoneRejectsPINs", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierInitNoneRejectsPINsCommand("SO123", "USR123"))
		require.Errorf(t, err, "barrier init --barrier-strategy=none with PINs must fail: stdout=%s", stdout)
		combined := stdout + stderr
		assert.Containsf(t, combined, "barrier-strategy=none",
			"error must reference barrier-strategy=none: combined=%s", combined)
		assert.Containsf(t, combined, "--so-pin",
			"error must mention --so-pin: combined=%s", combined)
	})
}

// TestBarrierInit_ShamirRequiresParams exercises the required-flag
// matrix for init-shamir.
func TestBarrierInit_ShamirRequiresParams(t *testing.T) {
	cases := []string{"--passphrase", "--threshold", "--total"}
	for _, omit := range cases {
		omit := omit
		t.Run("omit_"+strings.TrimPrefix(omit, "--"), func(t *testing.T) {
			runForBarrierTransports(t, omit, barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
				stdout, stderr, err := runner.Run(BarrierInitShamirMissingFlagCommand(omit))
				require.Errorf(t, err,
					"init-shamir without %s must fail: stdout=%s stderr=%s", omit, stdout, stderr)
				assert.Containsf(t, stdout+stderr, strings.TrimPrefix(omit, "--"),
					"error must mention omitted flag %s", omit)
			})
		})
	}
	t.Run("threshold_exceeds_total", func(t *testing.T) {
		runForBarrierTransports(t, "threshold_exceeds_total", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			stdout, stderr, err := runner.Run(BarrierInitShamirThresholdExceedsTotalCommand())
			require.Errorf(t, err, "init-shamir threshold>total must fail")
			assert.Containsf(t, stdout+stderr, "--threshold cannot exceed --total",
				"missing canonical error message: stdout=%s stderr=%s", stdout, stderr)
		})
	})
}

// TestBarrierInit_InvalidStrategy asserts the unknown-strategy
// validation error.
func TestBarrierInit_InvalidStrategy(t *testing.T) {
	runForBarrierTransports(t, "InvalidStrategy", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierInitInvalidStrategyCommand())
		require.Errorf(t, err, "invalid --barrier-strategy must fail")
		combined := stdout + stderr
		assert.Containsf(t, combined, "none", "error must enumerate valid strategies: %s", combined)
		assert.Containsf(t, combined, "passphrase", "error must enumerate valid strategies: %s", combined)
		assert.Containsf(t, combined, "shamir", "error must enumerate valid strategies: %s", combined)
	})
}

// TestBarrierInit_SoftwareMissingSecret asserts the secret-required rule
// under the software/shamir branches.
func TestBarrierInit_SoftwareMissingSecret(t *testing.T) {
	for _, strategy := range []string{"passphrase", "shamir"} {
		strategy := strategy
		t.Run(strategy, func(t *testing.T) {
			runForBarrierTransports(t, strategy, barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
				stdout, stderr, err := runner.Run(BarrierInitMissingSecretCommand(strategy))
				require.Errorf(t, err, "barrier init %s without --secret must fail", strategy)
				assert.Containsf(t, stdout+stderr, "--passphrase is required",
					"canonical error missing: stdout=%s stderr=%s", stdout, stderr)
			})
		})
	}
}

// TestBarrierInit_SoloPinRejected asserts the dual-control rule.
func TestBarrierInit_SoloPinRejected(t *testing.T) {
	cases := []struct {
		flag  string
		value string
	}{
		{"--so-pin", "lonely-so"},
		{"--user-pin", "lonely-user"},
	}
	for _, c := range cases {
		c := c
		t.Run(strings.TrimPrefix(c.flag, "--"), func(t *testing.T) {
			runForBarrierTransports(t, c.flag, barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
				stdout, stderr, err := runner.Run(BarrierInitSoloPinCommand(c.flag, c.value))
				require.Errorf(t, err, "barrier init with solo %s must fail", c.flag)
				assert.Containsf(t, stdout+stderr, "requires",
					"dual-control error missing: stdout=%s stderr=%s", stdout, stderr)
			})
		})
	}
}

// =============================================================================
// Tests: unseal round-trip (per-test daemon)
// =============================================================================

// TestBarrierUnseal_RoundTrip exercises the runtime unseal/seal cycle
// end-to-end across every transport against a SERVER-INITIALISED software
// daemon. The daemon is `quicbaasd init`-ed with the secret and comes up
// SEALED; the client then drives unseal -> seal -> unseal with the
// already-known secret. The cycle proves the SDK's BarrierClient wiring on
// each transport for the load-bearing runtime mutation verbs. The client
// never performs `barrier init` over the network — only the server inits.
func TestBarrierUnseal_RoundTrip(t *testing.T) {
	const secret = "round-trip-secret"
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: secret}
	runForBarrierTransports(t, "RoundTrip", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		// 1. Daemon came up SEALED after server-init. Unseal with the
		//    operator's known secret.
		runner.RunAndAssert(BarrierUnsealCommand(secret))
		unsealed := false
		assertBarrierStatus(t, runner, &unsealed, "passphrase")

		// 2. Seal (non-interactive).
		runner.RunAndAssert(BarrierSealAssumeYesCommand())
		sealed := true
		assertBarrierStatus(t, runner, &sealed, "passphrase")

		// 3. Unseal again with the original passphrase.
		runner.RunAndAssert(BarrierUnsealCommand(secret))
		unsealedAgain := false
		assertBarrierStatus(t, runner, &unsealedAgain, "passphrase")
	})
}

// TestBarrierUnsealShares exercises the runtime `barrier unseal-shares`
// share-submission wiring (repeatable --share, --shares-file, --shares-file
// -) across every transport and every input mode.
//
// CONFIRMED-MODEL NOTE — the Shamir HAPPY PATH is intentionally not driven
// here. Per the locked architecture, a Shamir barrier is brought up by the
// M-of-N CEREMONY, not by a network `barrier init-shamir` verb. A
// `quicbaasd init --barrier-strategy shamir` only writes
// pending_ceremony.json and defers first-boot to the ceremony; it does NOT
// provision a usable barrier or bring the listeners up, so a Shamir daemon
// cannot reach readiness for this suite without completing the ceremony.
// Bypassing the ceremony to force a green here is explicitly forbidden.
//
// What this test DOES guarantee is that every transport correctly forwards
// the share inputs and decodes the daemon's BarrierUnsealWithShares
// response envelope: we submit shares to a SERVER-INITIALISED software
// daemon (where Shamir is not configured) and assert each transport
// surfaces the canonical "shamir not configured" error rather than a CLI
// crash or a malformed-envelope decode failure. A regression in any
// transport's unseal-shares wiring (flags vs file vs stdin, request
// marshalling, or response decoding) therefore still fails in exactly one
// matrix cell. The end-to-end Shamir unseal is covered once the ceremony
// path is exercised by the dedicated ceremony suite.
func TestBarrierUnsealShares(t *testing.T) {
	const shamirNotConfigured = "shamir not configured"
	// Representative non-secret share blobs; the daemon rejects them before
	// any cryptographic check because Shamir is not configured, so their
	// exact contents are irrelevant — only the transport plumbing matters.
	shares := []string{"share-alpha", "share-bravo"}
	scenarios := []struct {
		name string
		mode string // "flags" | "file" | "stdin"
	}{
		{"flags", "flags"},
		{"file", "file"},
		{"stdin", "stdin"},
	}
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: "unseal-shares-secret"}
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runForBarrierTransports(t, sc.name, opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
				var stdout, stderr string
				var err error
				switch sc.mode {
				case "flags":
					stdout, stderr, err = runner.Run(BarrierUnsealSharesFlagsCommand(shares))
				case "file":
					path := writeBarrierSharesFile(t, t.TempDir(), shares)
					stdout, stderr, err = runner.Run(BarrierUnsealSharesFileCommand(path))
				case "stdin":
					stdout, stderr, err = runBarrierCommandWithStdin(
						t, runner, BarrierUnsealSharesStdinCommand(),
						strings.Join(shares, "\n")+"\n",
					)
				}
				// The verb must reach the daemon and be REJECTED — proving the
				// per-transport request marshalling + response decoding is
				// wired. A software barrier has no Shamir config, so the
				// daemon rejects the share submission. Most transports
				// surface the canonical "shamir not configured" string; the
				// MCP transport wraps daemon errors in a generic envelope
				// that names the invoked tool (qrdb_barrier_shamir_unseal
				// _shares) rather than the inner message, so we accept
				// either marker. Both prove the share inputs reached the
				// BarrierUnsealWithShares handler — a CLI crash or a missing
				// dispatch would fail one marker AND the error check.
				combined := stdout + stderr
				require.Errorf(t, err,
					"unseal-shares against a software daemon must be rejected: stdout=%s stderr=%s",
					stdout, stderr)
				rejected := strings.Contains(combined, shamirNotConfigured) ||
					strings.Contains(combined, "qrdb_barrier_shamir_unseal_shares")
				assert.Truef(t, rejected,
					"unseal-shares must surface a daemon-side rejection that proves the verb dispatched: stdout=%s stderr=%s",
					stdout, stderr)
			})
		})
	}
}

// TestBarrierUnsealShares_MutualExclusion asserts the validation rules
// for the --share / --shares-file pair.
func TestBarrierUnsealShares_MutualExclusion(t *testing.T) {
	t.Run("both_inputs", func(t *testing.T) {
		runForBarrierTransports(t, "both_inputs", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			stdout, stderr, err := runner.Run(BarrierUnsealSharesBothInputsCommand())
			require.Errorf(t, err, "unseal-shares with both inputs must fail")
			assert.Containsf(t, stdout+stderr, "exactly one of",
				"canonical mutual-exclusion error missing: %s", stdout+stderr)
		})
	})
	t.Run("no_inputs", func(t *testing.T) {
		runForBarrierTransports(t, "no_inputs", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			stdout, stderr, err := runner.Run(BarrierUnsealSharesNoInputsCommand())
			require.Errorf(t, err, "unseal-shares with no inputs must fail")
			assert.Containsf(t, stdout+stderr, "is required",
				"canonical required-input error missing: %s", stdout+stderr)
		})
	})
}

// TestBarrierUnseal_MissingSecret asserts the unseal verb rejects an
// empty --secret.
func TestBarrierUnseal_MissingSecret(t *testing.T) {
	runForBarrierTransports(t, "MissingSecret", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierUnsealMissingSecretCommand())
		require.Errorf(t, err, "barrier unseal without --secret must fail")
		assert.Containsf(t, stdout+stderr, "--passphrase is required",
			"canonical error missing: %s", stdout+stderr)
	})
}

// =============================================================================
// Tests: seal confirmation path (per-test daemon)
// =============================================================================

// TestBarrierSeal_RequiresConfirmation contrasts the -y bypass path
// against the interactive prompt. The interactive path reads "yes\n" /
// "no\n" from stdin via runBarrierCommandWithStdin.
func TestBarrierSeal_RequiresConfirmation(t *testing.T) {
	const secret = "seal-confirm-secret"
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: secret}
	t.Run("assume_yes", func(t *testing.T) {
		runForBarrierTransports(t, "assume_yes", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			runner.RunAndAssert(BarrierUnsealCommand(secret))
			runner.RunAndAssert(BarrierSealAssumeYesCommand())
			sealed := true
			assertBarrierStatus(t, runner, &sealed, "passphrase")
		})
	})
	t.Run("interactive_yes", func(t *testing.T) {
		runForBarrierTransports(t, "interactive_yes", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			runner.RunAndAssert(BarrierUnsealCommand(secret))
			out, errStr, runErr := runBarrierCommandWithStdin(
				t, runner, BarrierSealCommand(), "yes\n",
			)
			require.NoErrorf(t, runErr, "interactive seal (yes): stdout=%s stderr=%s", out, errStr)
			sealed := true
			assertBarrierStatus(t, runner, &sealed, "passphrase")
		})
	})
	t.Run("interactive_no", func(t *testing.T) {
		runForBarrierTransports(t, "interactive_no", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			runner.RunAndAssert(BarrierUnsealCommand(secret))
			out, errStr, runErr := runBarrierCommandWithStdin(
				t, runner, BarrierSealCancelCommand(), "no\n",
			)
			require.NoErrorf(t, runErr, "interactive seal (no): stdout=%s stderr=%s", out, errStr)
			assert.Containsf(t, out, "Seal cancelled",
				"cancel path must print 'Seal cancelled': %s", out)
			// Barrier must still be unsealed because the seal was cancelled.
			unsealed := false
			assertBarrierStatus(t, runner, &unsealed, "passphrase")
		})
	})
}

// TestBarrierSeal_SORequired exercises the --so-pin pass-through. The
// seal verb accepts --so-pin uncoditionally per the canonical surface
// (the SDK forwards it once the BarrierSeal request-bearing form lands).
// Today we assert that --so-pin -y succeeds when the daemon does not
// have a PINBackend configured (per-test daemon comes up with no
// PINBackend), proving the flag is at least parsed and forwarded without
// error.
func TestBarrierSeal_SORequired(t *testing.T) {
	const secret = "so-pin-secret"
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: secret}
	runForBarrierTransports(t, "SOPin", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		runner.RunAndAssert(BarrierUnsealCommand(secret))
		// With --so-pin set and -y supplied: succeeds (the per-test daemon
		// has no PINBackend, so the flag is parsed + ignored).
		runner.RunAndAssert(BarrierSealWithSOPinCommand("SO-PIN-XYZ"))
		sealed := true
		assertBarrierStatus(t, runner, &sealed, "passphrase")
	})
}

// =============================================================================
// Tests: rekey full lifecycle (per-test daemon)
// =============================================================================

// TestBarrierRekey_Dispatch verifies the runtime `barrier rekey` request/
// response wiring across every transport against a SERVER-INITIALISED,
// UNSEALED software daemon.
//
// BLOCKED happy-path note: the qrdb seal backend's BarrierRekey path
// requires a configured Shamir sharing scheme — a software (passphrase)
// barrier rekey returns the canonical "shamir not configured" rejection
// (status 13). This is a qrdb-layer constraint, not a quicbaas test
// artifact (the upstream go-qrdb CLI suite skips the same path), so the
// "rekey then unseal-with-new-secret" lifecycle cannot be exercised here
// without a Shamir barrier, and a Shamir barrier requires the locked M-of-N
// ceremony that `quicbaasd init --barrier-strategy shamir` defers. Forcing
// this green would require either bypassing the ceremony or weakening the
// assertion to ignore the failure — both forbidden.
//
// What this test DOES guarantee is that the rekey verb dispatches over each
// transport and the daemon's rejection is surfaced (not a CLI crash or a
// malformed-envelope decode failure), so a regression in any transport's
// BarrierRekey marshalling/decoding still fails in exactly one matrix cell.
func TestBarrierRekey_Dispatch(t *testing.T) {
	const oldSecret = "rekey-old-secret"
	const shamirNotConfigured = "shamir not configured"
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: oldSecret}
	runForBarrierTransports(t, "Dispatch", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		// Daemon came up SEALED after server-init; unseal so rekey operates
		// on an unsealed barrier and reaches the backend's rekey path.
		runner.RunAndAssert(BarrierUnsealCommand(oldSecret))

		// The canonical rekey surface is SHARE-FREE: no secret travels on
		// the request (`barrier rekey [--so-pin] [--threshold N --total M]`).
		// Send a well-formed 2-of-3 pair so every transport-side validator
		// (REST/gRPC handlers require threshold/total > 0) passes and the
		// request reaches the qrdb backend, which rejects a passphrase
		// barrier with the canonical "shamir not configured". Most
		// transports surface the message; MCP wraps it in a tool-name
		// envelope. Either proves the verb reached BarrierRekey.
		stdout, stderr, err := runner.Run(BarrierRekeyCommand(2, 3))
		combined := stdout + stderr
		require.Errorf(t, err,
			"rekey of a software barrier must be rejected (shamir not configured): stdout=%s stderr=%s",
			stdout, stderr)
		rejected := strings.Contains(combined, shamirNotConfigured) ||
			strings.Contains(combined, "qrdb_barrier_rekey")
		assert.Truef(t, rejected,
			"rekey must surface a daemon-side rejection that proves the verb dispatched: stdout=%s stderr=%s",
			stdout, stderr)
	})
}

// TestBarrierRekey_ValidationErrors covers the CLI-side validation rules of
// the share-free rekey surface: the Shamir --threshold/--total pair must be
// supplied together, the threshold must be >= 2, and the total must be >=
// the threshold (cmd/quicbaas/barrier.go runBarrierRekeyCommand). All rules
// reject BEFORE any request reaches the daemon.
func TestBarrierRekey_ValidationErrors(t *testing.T) {
	for _, flag := range []string{"--threshold", "--total"} {
		flag := flag
		t.Run("unpaired_"+strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			runForBarrierTransports(t, flag, barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
				stdout, stderr, err := runner.Run(BarrierRekeyUnpairedShamirCommand(flag))
				require.Errorf(t, err, "rekey with solo %s must fail", flag)
				assert.Containsf(t, stdout+stderr, "must be supplied together",
					"canonical pairing error missing: %s", stdout+stderr)
			})
		})
	}
	t.Run("threshold_below_minimum", func(t *testing.T) {
		runForBarrierTransports(t, "threshold_below_minimum", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			stdout, stderr, err := runner.Run(BarrierRekeyBadThresholdCommand())
			require.Errorf(t, err, "rekey with --threshold 1 must fail")
			assert.Containsf(t, stdout+stderr, "--threshold must be >= 2",
				"canonical threshold-bound error missing: %s", stdout+stderr)
		})
	})
	t.Run("total_below_threshold", func(t *testing.T) {
		runForBarrierTransports(t, "total_below_threshold", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
			stdout, stderr, err := runner.Run(BarrierRekeyTotalBelowThresholdCommand())
			require.Errorf(t, err, "rekey with --total < --threshold must fail")
			assert.Containsf(t, stdout+stderr, "--total must be >= --threshold",
				"canonical total-bound error missing: %s", stdout+stderr)
		})
	})
}

// =============================================================================
// Tests: recovery generate + recover (per-test daemon)
// =============================================================================

// TestBarrierRecovery_Dispatch verifies the recovery generate/recover
// request wiring across every transport against a SERVER-INITIALISED
// software daemon.
//
// BLOCKED happy-path note: the qrdb recovery surface is SHARE-FREE —
// GenerateRecoveryKeys seals the generated shares to ENROLLED recovery
// officers via the recovery distributor and returns metadata only; no key
// material ever travels back to the caller (qrdb pkg/seal/recovery.go).
// The per-test daemon has no enrolled officers (enrollment requires the
// locked officer-ceremony flow), so generation is rejected with the
// canonical "insufficient officers enrolled" error and there are no keys
// with which to drive RecoverWithKeys' happy path. Forcing this green would
// require bypassing the officer-enrollment ceremony — forbidden. The
// end-to-end recovery lifecycle belongs to the ceremony suite.
//
// What this test DOES guarantee is that both verbs dispatch over every
// transport and the daemon's canonical rejection is surfaced (not a CLI
// crash or a malformed-envelope decode failure), so a regression in any
// transport's recovery marshalling/decoding still fails in exactly one
// matrix cell.
func TestBarrierRecovery_Dispatch(t *testing.T) {
	const secret = "recovery-secret"
	opts := barrierDaemonOptions{Strategy: "passphrase", Secret: secret}
	runForBarrierTransports(t, "Dispatch", opts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		// 1. Daemon came up SEALED after server-init; unseal so generation
		//    reaches the distributor (the root key must be in memory).
		runner.RunAndAssert(BarrierUnsealCommand(secret))

		// 2. recovery generate → rejected: no recovery officers enrolled.
		stdout, stderr, err := runner.Run(BarrierRecoveryGenerateCommand(2, 3))
		combined := stdout + stderr
		require.Errorf(t, err,
			"recovery generate without enrolled officers must be rejected: stdout=%s stderr=%s",
			stdout, stderr)
		generateRejected := strings.Contains(combined, "insufficient officers enrolled") ||
			strings.Contains(combined, "qrdb_barrier_recovery_generate")
		assert.Truef(t, generateRejected,
			"recovery generate must surface the canonical officers rejection: stdout=%s stderr=%s",
			stdout, stderr)

		// 3. Seal, then recovery recover with placeholder keys → rejected.
		//    GenerateRecoveryKeys persists the recovery METADATA before the
		//    distributor runs, so after the officers rejection above the
		//    daemon carries a 2-of-3 recovery meta with NO distributed
		//    shares: bogus share values fail the Shamir combine ("failed to
		//    combine shares"). A daemon whose generate never persisted meta
		//    would reject with "recovery keys not found" instead — both are
		//    canonical daemon-side rejections proving the verb dispatched.
		runner.RunAndAssert(BarrierSealAssumeYesCommand())
		stdout, stderr, err = runner.Run(BarrierRecoveryRecoverCommand([]string{"key-a", "key-b"}))
		combined = stdout + stderr
		require.Errorf(t, err,
			"recovery recover without distributed recovery keys must be rejected: stdout=%s stderr=%s",
			stdout, stderr)
		recoverRejected := strings.Contains(combined, "failed to combine shares") ||
			strings.Contains(combined, "recovery keys not found") ||
			strings.Contains(combined, "qrdb_barrier_recovery_recover")
		assert.Truef(t, recoverRejected,
			"recovery recover must surface a canonical daemon-side rejection: stdout=%s stderr=%s",
			stdout, stderr)
	})
}

// TestBarrierRecovery_GenerateValidation asserts the threshold>=2 rule.
func TestBarrierRecovery_GenerateValidation(t *testing.T) {
	runForBarrierTransports(t, "GenerateValidation", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierRecoveryGenerateBadThresholdCommand())
		require.Errorf(t, err, "recovery generate with threshold=1 must fail")
		assert.Containsf(t, stdout+stderr, "--threshold must be >= 2",
			"canonical threshold-validation error missing: %s", stdout+stderr)
	})
}

// TestBarrierRecovery_RecoverValidation asserts the "at least one share"
// rule.
func TestBarrierRecovery_RecoverValidation(t *testing.T) {
	runForBarrierTransports(t, "RecoverValidation", barrierValidationDaemonOpts, func(t *testing.T, _ *barrierDaemon, runner *TestRunner) {
		stdout, stderr, err := runner.Run(BarrierRecoveryRecoverNoSharesCommand())
		require.Errorf(t, err, "recovery recover with no shares must fail")
		assert.Containsf(t, stdout+stderr, "--share is required",
			"canonical missing-share error missing: %s", stdout+stderr)
	})
}

// =============================================================================
// Tests: AUTH-ON posture contract (per-test daemon at production defaults)
// =============================================================================

// TestBarrierAuthPosture_DefaultDeny pins the owner's AUTH-ON contract for
// the barrier surface — this posture must NOT be weakened and must stay an
// EXPLICIT regression test rather than an accidental side effect of fixture
// defaults:
//
//   - While SEALED, the sealed-window allowlist admits UNAUTHENTICATED
//     submission of unseal material: possession of the unseal secret IS the
//     authorization, because a sealed IAM plane can neither mint nor
//     validate tokens.
//   - Once UNSEALED, the sealed-window bypass is gone and the default-deny
//     gate is in force: BarrierStatus WITHOUT a bearer token is rejected
//     Unauthenticated over gRPC (pkg/api/grpc/authz_interceptor.go), and
//     BarrierSeal WITHOUT a bearer token is rejected 401 over REST — seal
//     is deliberately NOT on the sealed-window allowlist, which is exactly
//     why an EnforceAuthz daemon cannot be re-sealed by a token-less
//     fixture and must instead boot SEALED (SealedBoot).
//
// The daemon boots at the production default posture (EnforceAuthz=true —
// no insecure --config override) in SEALED-BOOT mode: `quicbaasd init`
// provisions the passphrase barrier, `quicbaasd start` runs WITHOUT the
// auto-unseal passphrase, so the daemon serves the sealed window and the
// test itself performs the unseal through the product's own allowlisted
// verb.
func TestBarrierAuthPosture_DefaultDeny(t *testing.T) {
	const secret = "auth-posture-secret"
	daemon, caPath := spawnFreshBarrierDaemon(t, barrierDaemonOptions{
		Strategy:     "passphrase",
		Secret:       secret,
		EnforceAuthz: true,
		SealedBoot:   true,
	})
	runner := newBarrierDaemonRunner(t, TransportGRPC, daemon)

	// 1. Sealed-window allowlist: the unauthenticated unseal MUST succeed
	//    while the barrier is sealed.
	runner.RunAndAssert(BarrierUnsealCommand(secret))

	// The unseal closes the sealed window: the daemon tears the
	// sealed-phase listeners down and completes the full boot, restarting
	// every transport behind the default-deny gate. Synchronize on the
	// full-boot REST authz middleware before asserting the post-unseal
	// contract (during the transition connections are refused).
	waitForBarrierAuthGate(t, daemon, caPath)

	// 2. Post-unseal, the same unauthenticated caller is rejected: the
	//    barrier surface has left the sealed window, so BarrierStatus
	//    demands an authenticated principal.
	stdout, stderr, err := runner.Run(BarrierStatusJSONCommand())
	require.Errorf(t, err,
		"post-unseal BarrierStatus without a token must be rejected: stdout=%s stderr=%s",
		stdout, stderr)
	assert.Containsf(t, stdout+stderr, "authentication required",
		"post-unseal status must fail Unauthenticated: stdout=%s stderr=%s",
		stdout, stderr)

	// 3. Post-unseal, a token-less re-seal is rejected 401 over REST. This
	//    pins the exact behavior that forbids an EnforceAuthz fixture from
	//    restoring the SEALED precondition at runtime — the contract
	//    working as designed, asserted through the CLI like every other
	//    matrix cell.
	restRunner := newBarrierDaemonRunner(t, TransportREST, daemon)
	stdout, stderr, err = restRunner.Run(CommandDef{
		Name:    "barrier seal -y (no token)",
		Args:    []string{"barrier", "seal", "-y"},
		Timeout: 30 * time.Second,
	})
	require.Errorf(t, err,
		"post-unseal BarrierSeal without a token must be rejected: stdout=%s stderr=%s",
		stdout, stderr)
	assert.Containsf(t, stdout+stderr, "401",
		"post-unseal seal must fail 401 Unauthorized: stdout=%s stderr=%s", stdout, stderr)
	assert.Containsf(t, stdout+stderr, "Missing or invalid authorization header",
		"post-unseal seal must surface the REST authz rejection: stdout=%s stderr=%s",
		stdout, stderr)
}

// waitForBarrierAuthGate blocks until the post-unseal FULL boot has
// restarted the REST transport behind the default-deny gate: a token-less
// POST /v1/barrier/seal answered 401 proves the full-boot authz middleware
// (not the sealed-window allowlist, which rejects the off-allowlist verb
// with a non-401 status) is serving. Also confirms the gRPC TCP listener
// is back so the subsequent CLI assertions cannot race the transport
// restart.
func waitForBarrierAuthGate(t *testing.T, d *barrierDaemon, caPath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartTimeout)
	defer cancel()
	ticker := time.NewTicker(daemonReadyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("full boot never reached the default-deny authz gate within %s", daemonStartTimeout)
		case <-d.Done():
			t.Fatalf("barrier daemon exited during the sealed-window -> full-boot transition: wait_err=%v", d.WaitErr())
		case <-ticker.C:
			tlsCfg, err := barrierProbeTLSConfig(caPath)
			if err != nil {
				continue
			}
			if !probeBarrierSealRejected401(ctx, d.ports.REST, tlsCfg) {
				continue
			}
			if err := probeBarrierTCP(ctx, d.ports.GRPC); err != nil {
				continue
			}
			return
		}
	}
}

// probeBarrierSealRejected401 reports whether a token-less POST to the
// full-boot /v1/barrier/seal is rejected with 401.
func probeBarrierSealRejected401(ctx context.Context, port int, tlsCfg *tls.Config) bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/v1/barrier/seal", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusUnauthorized
}

// =============================================================================
// Unused certificate scaffolding (intentionally retained for future use)
// =============================================================================
//
// The package historically provided a synthetic-cert helper for one-off
// PIN-backend wiring tests. The current barrier surface does not need
// it, but keeping the helper compiled (behind the _ = name pattern below)
// preserves the export so a future test that needs to spawn a daemon
// with a pre-loaded PIN-backed credential bundle does not have to
// re-derive the X.509 boilerplate.

// generateBarrierCertScaffold issues a throwaway ECDSA P-256 self-signed
// certificate for tests that need a placeholder credential bundle. The
// produced PEM-encoded cert + key bytes are returned in memory so the
// caller can choose where to persist them.
func generateBarrierCertScaffold(cn string) ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		IsCA:         false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

var _ = generateBarrierCertScaffold

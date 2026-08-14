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

// Package init provides production-grade integration tests covering the
// canonical "quicbaasd init + start + verify" matrix per spec
// docs/08-bootstrap-auth-architecture.md §6 and the locked architecture
// (qrdb -> xkms -> quicbaas composition; quicbaasd-only matrix; no
// embedded transport).
//
// The matrix exercises every combination of:
//
//	{ none, software, shamir }   barrier strategy
//	            ×
//	{ rest, grpc, quic, mcp, unix }   transport under test
//
// for a total of 15 software-only combinations. Hardware-backed
// strategies (pkcs11/tpm2/awskms/gcpkms/azurekv/vault) are NOT exercised
// here — their factory wiring is gated on operator-supplied hardware that
// belongs in dedicated env-gated suites.
//
// The matrix tests spawn the real quicbaasd binary via exec.Command
// against per-subtest temp data directories. Every subtest runs
// hermetically — distinct data dir, distinct port range, distinct PID,
// torn down via t.Cleanup so process leaks cannot poison sibling subtests
// even on panic / timeout paths.
package init

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Environment + binary discovery
// ---------------------------------------------------------------------------

// integrationGateEnv is the canonical environment variable that opts into
// the integration test suite per the project's CLAUDE.md convention. The
// matrix tests refuse to run unless it is set to a truthy value.
const integrationGateEnv = "INTEGRATION_TEST"

// daemonBinaryEnv overrides the auto-discovered quicbaasd path. Used by
// container-based runners that ship the binary at a fixed location.
const daemonBinaryEnv = "QUICBAASD_BINARY"

// daemonStartTimeout bounds the wait for the daemon's selected transport
// to become reachable. The PebbleDB open + xkms CA bootstrap dominates
// cold-boot latency; 90s comfortably covers a CI cold boot while still
// failing fast on a genuinely broken combination.
const daemonStartTimeout = 90 * time.Second

// daemonShutdownTimeout bounds the wait for a SIGTERM to drain the
// daemon's listeners and release its PebbleDB lock. Exceeding this
// triggers SIGKILL so a hung daemon cannot wedge sibling subtests.
const daemonShutdownTimeout = 20 * time.Second

// transportProbeInterval is the gap between transport-reachability polls
// while waiting for the daemon to come up. Channel-driven polling avoids
// time.Sleep loops per CLAUDE.md.
const transportProbeInterval = 200 * time.Millisecond

// requireIntegrationGate enforces the INTEGRATION_TEST gate convention.
// We fail (not skip) when unset so accidental local runs surface as
// FAILED rather than mysteriously passing. The integration build tag
// already excludes the file from default unit-test runs; the env gate is
// a second-layer safety net for runs that explicitly pass -tags=integration.
func requireIntegrationGate(t *testing.T) {
	t.Helper()
	if v := os.Getenv(integrationGateEnv); v != "" {
		return
	}
	t.Fatalf(
		"INTEGRATION_TEST gate is not set; run via `INTEGRATION_TEST=1 make integration-test-init` " +
			"(see test/integration/init/README.md). Build tag '-tags=integration' alone is not sufficient.",
	)
}

// resolveDaemonBinary returns an absolute path to the quicbaasd binary.
// Priority:
//  1. $QUICBAASD_BINARY when set (CI / container convention).
//  2. <repo-root>/bin/quicbaasd when present (developer workflow).
//
// We deliberately do NOT fall back to PATH — the matrix tests must run
// against the binary built from the current source tree. A missing binary
// fails the test rather than silently dispatching to a stale install.
func resolveDaemonBinary(t *testing.T) string {
	t.Helper()
	if explicit := os.Getenv(daemonBinaryEnv); explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			t.Fatalf("resolve $%s=%s: %v", daemonBinaryEnv, explicit, err)
		}
		if _, statErr := os.Stat(abs); statErr != nil {
			t.Fatalf("$%s=%s: not found: %v", daemonBinaryEnv, abs, statErr)
		}
		return abs
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root for default binary discovery: %v", err)
	}
	candidate := filepath.Join(repoRoot, "bin", "quicbaasd")
	if _, statErr := os.Stat(candidate); statErr == nil {
		return candidate
	}
	t.Fatalf(
		"quicbaasd binary not found at %s; build it first via `make build-daemon` "+
			"or point $%s at an existing binary",
		candidate, daemonBinaryEnv,
	)
	return "" // unreachable
}

// findRepoRoot walks upward from the test file's location to find the
// directory containing go.mod. This makes the helper independent of the
// caller's working directory (matters for `go test` invocations that may
// chdir into the package directory).
func findRepoRoot() (string, error) {
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
			return "", errors.New("no go.mod found walking upward from test file")
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Port allocator
// ---------------------------------------------------------------------------

// allocatedPorts tracks every port handed out to a subtest. The global is
// guarded by a mutex so parallel subtests cannot collide. We bind the
// listener exclusively during allocation and close it immediately; the
// daemon re-binds the same port a few hundred ms later. Race-window
// collisions are tolerated by re-allocating up to maxPortRetries times.
var (
	allocatedPortsMu sync.Mutex
	allocatedPorts   = map[int]struct{}{}
)

const maxPortRetries = 50

// PortBundle holds every port the daemon needs to bring up the requested
// transport. Each field is unique per subtest. Zero indicates "the
// transport is disabled for this combination".
//
// Unix socket transport replaces tcpListener-based ports with a tempdir-
// rooted socket path that lives until t.Cleanup removes the data dir.
type PortBundle struct {
	REST int
	GRPC int
	QUIC int
	MCP  int
	Unix string
}

// allocPort reserves a free TCP port and remembers it so subsequent
// allocations cannot return the same value while a subtest still holds
// the daemon process that bound to it.
func allocPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < maxPortRetries; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if cerr := ln.Close(); cerr != nil {
			t.Fatalf("close port-allocation listener: %v", cerr)
		}
		allocatedPortsMu.Lock()
		_, already := allocatedPorts[port]
		if !already {
			allocatedPorts[port] = struct{}{}
			allocatedPortsMu.Unlock()
			return port
		}
		allocatedPortsMu.Unlock()
	}
	t.Fatalf("failed to allocate a unique TCP port after %d retries", maxPortRetries)
	return 0
}

// allocUDPPort reserves a free UDP port for the QUIC listener. Mirrors
// allocPort but binds UDP because the kernel keeps TCP and UDP port
// spaces distinct on the same address.
func allocUDPPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < maxPortRetries; attempt++ {
		conn, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := conn.LocalAddr().(*net.UDPAddr).Port
		if cerr := conn.Close(); cerr != nil {
			t.Fatalf("close port-allocation packet conn: %v", cerr)
		}
		allocatedPortsMu.Lock()
		_, already := allocatedPorts[port]
		if !already {
			allocatedPorts[port] = struct{}{}
			allocatedPortsMu.Unlock()
			return port
		}
		allocatedPortsMu.Unlock()
	}
	t.Fatalf("failed to allocate a unique UDP port after %d retries", maxPortRetries)
	return 0
}

// allocPortBundle reserves every port the daemon listener stack needs
// for a given transport-under-test. The transport-under-test plus REST
// are always enabled — REST is the matrix's uniform health-probe
// surface, and the transport-under-test is exercised by the per-transport
// liveness probe. Other transports are disabled (port == 0) so the
// daemon does not bind unrelated ports.
//
// The Unix socket is ALWAYS allocated regardless of the transport
// under test. The daemon's startGRPCServer refuses to come up unless at
// least one of {gRPC TCP, Unix socket} is bound; making unix
// unconditional gives every row a deterministic gRPC anchor without the
// caller having to remember which transports carry gRPC natively.
//
// Unix socket lives under the per-subtest tmpdir so cleanup removes it
// transparently when the data dir is torn down. We use a short basename
// ("d.sock") to stay under the AF_UNIX 108-byte path limit on Linux
// when t.TempDir() prefixes are long.
func allocPortBundle(t *testing.T, transport string, dataDir string) PortBundle {
	t.Helper()
	bundle := PortBundle{
		REST: allocPort(t),
		Unix: filepath.Join(dataDir, "d.sock"),
	}
	switch transport {
	case "rest", "unix":
		// Both already covered: REST + unix anchor are unconditional.
	case "grpc":
		bundle.GRPC = allocPort(t)
	case "quic":
		bundle.QUIC = allocUDPPort(t)
	case "mcp":
		bundle.MCP = allocPort(t)
	default:
		t.Fatalf("allocPortBundle: unknown transport %q", transport)
	}
	return bundle
}

// ---------------------------------------------------------------------------
// Daemon process supervision
// ---------------------------------------------------------------------------

// DaemonProcess wraps an `exec.Cmd` spawning quicbaasd with the lifecycle
// guarantees the matrix requires:
//
//   - Stop is idempotent and signal-safe so t.Cleanup can call it without
//     worrying about earlier panics.
//   - LogPath surfaces the daemon's combined stderr for failure diagnostics.
//   - Done returns when the underlying process exits (used by the readiness
//     wait to fail fast on a crashed daemon rather than hanging).
type DaemonProcess struct {
	cmd     *exec.Cmd
	logPath string
	logFile *os.File
	dataDir string

	doneCh    chan struct{}
	waitErr   atomic.Pointer[error]
	stopOnce  sync.Once
	stopErr   error
	stoppedAt time.Time
}

// LogPath returns the absolute path of the file capturing the daemon's
// combined stderr + stdout. Test failure paths print or tail it for
// post-mortem diagnostics.
func (d *DaemonProcess) LogPath() string { return d.logPath }

// Pid returns the daemon's process ID, or 0 if it has not started.
func (d *DaemonProcess) Pid() int {
	if d == nil || d.cmd == nil || d.cmd.Process == nil {
		return 0
	}
	return d.cmd.Process.Pid
}

// Done returns a channel closed when the daemon process exits. Callers
// use this in select{} alongside readiness polls to fail fast on a crash.
func (d *DaemonProcess) Done() <-chan struct{} { return d.doneCh }

// WaitErr returns the os/exec wait error, or nil if the process exited
// cleanly. Only meaningful after Done() fires.
func (d *DaemonProcess) WaitErr() error {
	if p := d.waitErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Stop terminates the daemon with SIGTERM, falling back to SIGKILL when
// the graceful drain exceeds daemonShutdownTimeout. The first call wins;
// subsequent calls return the original outcome so t.Cleanup is always
// safe to invoke from multiple call sites.
//
// quicbaasd installs memguard.CatchSignal in main(), so a clean SIGTERM
// shutdown produces process exit code 1 (not 0). We treat both 0 and
// signal-driven exit 1 as success; only an unexpected non-zero exit
// surfaces as a Stop error.
func (d *DaemonProcess) Stop() error {
	d.stopOnce.Do(func() {
		defer func() { d.stoppedAt = time.Now() }()
		if d.cmd == nil || d.cmd.Process == nil {
			return
		}
		// Already exited?
		select {
		case <-d.doneCh:
			d.stopErr = sanitizeWaitErr(d.WaitErr(), false)
			if d.logFile != nil {
				_ = d.logFile.Close()
			}
			return
		default:
		}
		// Try graceful shutdown first.
		if sigErr := d.cmd.Process.Signal(syscall.SIGTERM); sigErr != nil {
			// EPERM / ESRCH means the process already exited.
			if !errors.Is(sigErr, os.ErrProcessDone) {
				d.stopErr = sigErr
			}
		}
		select {
		case <-d.doneCh:
			d.stopErr = sanitizeWaitErr(d.WaitErr(), true)
		case <-time.After(daemonShutdownTimeout):
			// Graceful drain timed out; force-kill.
			_ = d.cmd.Process.Signal(syscall.SIGKILL)
			<-d.doneCh
			d.stopErr = fmt.Errorf("daemon SIGTERM exceeded %s; SIGKILL applied; wait err=%v",
				daemonShutdownTimeout, d.WaitErr())
		}
		if d.logFile != nil {
			_ = d.logFile.Close()
		}
	})
	return d.stopErr
}

// sanitizeWaitErr maps process exit outcomes to Stop's semantics. The
// daemon's memguard signal handler exits with code 1 on SIGTERM; that
// is the normal path and MUST NOT propagate as an error. Any non-zero
// exit produced WITHOUT a prior SIGTERM (signalled=false) reflects a
// daemon crash and IS surfaced to the caller.
func sanitizeWaitErr(waitErr error, signalled bool) error {
	if waitErr == nil {
		return nil
	}
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return waitErr
	}
	if signalled && exitErr.ExitCode() == 1 {
		// Expected: memguard-driven SIGTERM exit.
		return nil
	}
	return waitErr
}

// spawnDaemon starts a quicbaasd subprocess with the supplied args + env.
// The returned DaemonProcess is registered with t.Cleanup so the binary
// is signalled at the end of the subtest even on panic / fatal-error
// paths.
//
// The daemon's combined stdout+stderr is captured to a per-subtest log
// file under the data dir; a failed test prints the tail of that file
// via dumpLogTail so the operator sees the daemon's last words without
// staring at a wall of structured JSON.
func spawnDaemon(t *testing.T, binary string, dataDir string, args []string, env []string) *DaemonProcess {
	t.Helper()
	logPath := filepath.Join(dataDir, "quicbaasd.log")
	// #nosec G304 - logPath is rooted at t.TempDir()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create daemon log file %s: %v", logPath, err)
	}

	cmd := exec.Command(binary, args...) // #nosec G204 - args are produced by the test
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Inherit PATH only; everything else is explicit.
	mergedEnv := append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	cmd.Env = mergedEnv
	// New process group so SIGTERM to the daemon does not propagate to
	// our test binary if the daemon spawns helpers that mis-route signals.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if startErr := cmd.Start(); startErr != nil {
		_ = logFile.Close()
		t.Fatalf("start quicbaasd: %v (binary=%s args=%v)", startErr, binary, args)
	}

	d := &DaemonProcess{
		cmd:     cmd,
		logPath: logPath,
		logFile: logFile,
		dataDir: dataDir,
		doneCh:  make(chan struct{}),
	}
	go func() {
		waitErr := cmd.Wait()
		d.waitErr.Store(&waitErr)
		close(d.doneCh)
	}()
	t.Cleanup(func() {
		if stopErr := d.Stop(); stopErr != nil {
			t.Logf("daemon Stop returned: %v", stopErr)
		}
		if t.Failed() {
			dumpLogTail(t, d.logPath, 100)
		}
	})
	return d
}

// dumpLogTail prints the last n lines of path to t.Logf so a failing
// matrix combination's diagnostic log is visible in the test runner
// output without forcing operators to chase the file out of t.TempDir().
func dumpLogTail(t *testing.T, path string, n int) {
	t.Helper()
	// #nosec G304 - log path is rooted at t.TempDir()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("dumpLogTail: read %s: %v", path, err)
		return
	}
	lines := splitLines(data)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	t.Logf("=== last %d lines of %s ===", len(lines), path)
	for _, line := range lines {
		t.Logf("  %s", line)
	}
}

// splitLines splits raw daemon output into newline-terminated lines for
// dumpLogTail. We avoid bufio.Scanner here because daemon log lines may
// exceed the default 64KB token limit on a high-load run.
func splitLines(data []byte) []string {
	var (
		out   []string
		start int
	)
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			out = append(out, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, string(data[start:]))
	}
	return out
}

// ---------------------------------------------------------------------------
// Init invocation
// ---------------------------------------------------------------------------

// runInit invokes `quicbaasd init` with the given strategy + data dir.
// PINs are supplied for strategies that require them; absent for "none".
// The function blocks until the init subcommand exits and returns the
// process's combined output for assertion / diagnostic use.
//
// We deliberately use a fresh exec.Cmd (not spawnDaemon) because init is
// short-lived and must exit cleanly before the test moves to start.
// matrixPassphrase is the admin-supplied barrier passphrase shared between
// the init phase (--passphrase) and the start phase
// (QUICBAAS_SEAL_PASSPHRASE auto-unseal) of the passphrase matrix cells.
const matrixPassphrase = "init-matrix-passphrase-2026"

// matrixTrustDomain is the SPIFFE trust domain every matrix init runs under.
// It is mandatory: the CA stamps spiffe://<trust-domain>/tenant/system/user/admin
// onto the admin certificate and login resolves the tenant from that SAN, so
// init fails closed without it.
const matrixTrustDomain = "quicbaas.integration.test"

// writeInitConfig writes the minimal config file `quicbaasd init` needs and
// returns its path.
//
// The trust domain has to travel in a CONFIG FILE rather than an environment
// variable: viper's AutomaticEnv only resolves keys it already knows from a
// default or a config file, so QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN alone is
// silently ignored. Passing it here keeps the init helpers hermetic while
// still exercising the real operator path.
func writeInitConfig(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "init-config.yaml")
	body := "iam:\n  spiffe_trust_domain: " + matrixTrustDomain + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("writeInitConfig: %w", err)
	}
	return path, nil
}

func runInit(ctx context.Context, binary string, strategy string, dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("runInit: mkdir data dir: %w", err)
	}
	cfgPath, cfgErr := writeInitConfig(dataDir)
	if cfgErr != nil {
		return "", cfgErr
	}
	args := []string{"init", "--config", cfgPath, "--barrier-strategy", strategy, "--data-dir", dataDir}
	switch strategy {
	case "none":
		// PINs intentionally omitted — the daemon rejects them under "none".
	case "passphrase":
		// The passphrase barrier is sealed with an admin-supplied
		// passphrase at init (security hardening removed the
		// deployment-derived default), so --passphrase is REQUIRED.
		args = append(args, "--so-pin", "so-pin-1234", "--user-pin", "user-pin-1234",
			"--passphrase", matrixPassphrase)
	case "shamir":
		// For shamir we expect officers + threshold/shares. The matrix
		// drives this through the dedicated runShamirInit helper below
		// because the officer-material files need to be generated first.
		return "", fmt.Errorf("runInit: shamir requires runShamirInit; not handled here")
	default:
		return "", fmt.Errorf("runInit: unknown strategy %q", strategy)
	}
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, "HOME="+dataDir)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// runShamirInit invokes `quicbaasd init --barrier-strategy shamir` with a
// 3-of-2 threshold and three synthetic officers covering all three
// officer kinds (csr / cert / pubkey). The officer material is generated
// hermetically per call in dataDir so no host state is left behind.
func runShamirInit(ctx context.Context, binary string, dataDir string) (string, error) {
	// No officer material is supplied here, deliberately. Under go-qrdb's
	// Security Officer distribution model officers register against the
	// RUNNING daemon (barrier officer-register, reachable from the sealed
	// window), and the M-of-N barrier is created by the ceremony rather than
	// inline at init. `quicbaasd init --barrier-strategy shamir` therefore
	// only records the operator's chosen M-of-N parameters.
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("runShamirInit: mkdir data dir: %w", err)
	}
	cfgPath, cfgErr := writeInitConfig(dataDir)
	if cfgErr != nil {
		return "", cfgErr
	}
	args := []string{
		"init",
		"--config", cfgPath,
		"--barrier-strategy", "shamir",
		"--so-pin", "so-pin-1234",
		"--user-pin", "user-pin-1234",
		"--shares", "3",
		"--threshold", "2",
		"--data-dir", dataDir,
	}
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, "HOME="+dataDir)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// ---------------------------------------------------------------------------
// Daemon start + transport readiness
// ---------------------------------------------------------------------------

// startDaemon spawns `quicbaasd start` against an already-initialised
// data dir with the supplied port bundle. Ports that map to 0 disable
// the corresponding transport via env-var overrides; only the
// transport-under-test plus REST (the always-on health-probe channel)
// are enabled.
//
// gRPC and Unix have an unusual coupling in the daemon: at least one of
// the two MUST be enabled or startGRPCServer refuses to start. We
// explicitly disable the Unix socket only when our target transport is
// gRPC or none of the gRPC-bearing transports; otherwise unix lives on a
// per-subtest path under the data dir.
func startDaemon(t *testing.T, binary string, dataDir string, strategy string, transport string, ports PortBundle) *DaemonProcess {
	t.Helper()

	args := []string{
		"start",
		"--data-dir", dataDir,
		"--log-level", "warn",
		"--http-port", fmt.Sprintf("%d", ports.REST),
	}

	// Unix socket is the unconditional gRPC anchor. gRPC TCP is opt-in
	// for the "grpc" row only — every other row exercises a non-gRPC
	// transport and we do not want the daemon binding a stray TCP port.
	switch transport {
	case "grpc":
		args = append(args, "--grpc-port", fmt.Sprintf("%d", ports.GRPC), "--unix-socket", ports.Unix)
	case "rest", "unix", "quic", "mcp":
		args = append(args, "--grpc-port", "0", "--unix-socket", ports.Unix)
	default:
		t.Fatalf("startDaemon: unknown transport %q", transport)
	}

	// Build the env-var override set that turns each transport on/off
	// at the listener layer. The CLI flag set covers REST + gRPC + unix;
	// QUIC and MCP have no CLI flags so we drive them through the
	// canonical env-var contract (pkg/config/loader.go applyEnvironmentOverrides).
	env := []string{
		// HOME isolates xkms key material under the data dir.
		"HOME=" + dataDir,
	}
	if strategy == "passphrase" {
		// Init ran with seal enabled and an initialized passphrase barrier,
		// so the data plane on disk is encrypted (encfs over the barrier
		// KEK). Start must boot with the SAME seal posture and auto-unseal
		// with the admin passphrase — there is no deployment-derived
		// default (security hardening).
		env = append(env,
			"QUICBAAS_SEAL_ENABLED=true",
			"QUICBAAS_SEAL_STRATEGY=passphrase",
			"QUICBAAS_SEAL_PASSPHRASE="+matrixPassphrase,
		)
	}
	if transport == "quic" {
		env = append(env,
			"QUICBAAS_SERVER_QUIC_ENABLED=true",
			fmt.Sprintf("QUICBAAS_SERVER_QUIC_PORT=%d", ports.QUIC),
		)
	} else {
		env = append(env, "QUICBAAS_SERVER_QUIC_ENABLED=false")
	}
	if transport == "mcp" {
		env = append(env,
			"QUICBAAS_SERVER_MCP_ENABLED=true",
			fmt.Sprintf("QUICBAAS_SERVER_MCP_PORT=%d", ports.MCP),
		)
	} else {
		env = append(env, "QUICBAAS_SERVER_MCP_ENABLED=false")
	}
	// Bind every TCP listener loopback-only; tests must not advertise on
	// 0.0.0.0 or they will collide with sibling subtests on shared hosts.
	env = append(env,
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		// Disable discovery + scheduler so we do not contend on the LAN
		// multicast group or spin background goroutines we do not use.
		"QUICBAAS_DISCOVERY_ENABLED=false",
		"QUICBAAS_ENABLE_SCHEDULER=false",
	)
	return spawnDaemon(t, binary, dataDir, args, env)
}

// waitForTransportReady blocks until both readiness gates fire or the
// deadline expires. The two gates are layered so the matrix never asserts
// the transport probe against a half-started daemon:
//
//  1. REST + /api/v1/health/ready (the daemon's canonical "IAM bootstrap
//     finished, all subsystems are up" oracle). Always probed because
//     REST is always enabled, even when REST is not the transport under
//     test. A 200 here proves PKI + IAM + the configured listeners all
//     came up cleanly.
//
//  2. The transport-under-test's own listener probe (raw TCP/UDP for
//     QUIC, HTTPS GET for MCP, gRPC dial for gRPC, Unix socket connect
//     for unix). When transport == "rest" this collapses to a single
//     readiness check.
//
// Each poll races against the daemon's Done channel so a crashed daemon
// fails the test immediately rather than after the full timeout elapses.
func waitForTransportReady(ctx context.Context, t *testing.T, d *DaemonProcess, transport string, ports PortBundle, dataDir string) error {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, daemonStartTimeout)
	defer cancel()

	ticker := time.NewTicker(transportProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.Done():
			return fmt.Errorf("transport %q not ready within %s: %w", transport, daemonStartTimeout, deadline.Err())
		case <-d.Done():
			return fmt.Errorf("daemon exited while waiting for transport %q: wait_err=%v", transport, d.WaitErr())
		case <-ticker.C:
			// The daemon auto-issues its OWN CA chain during first-boot. Until
			// that lands on disk we cannot verify its TLS listeners, so a
			// missing/empty chain is the same readiness signal as the old
			// probeCAChainPresent gate: keep polling.
			tlsCfg, err := daemonProbeTLSConfig(dataDir)
			if err != nil {
				continue
			}
			if err := probeREST_Ready(deadline, ports.REST, tlsCfg); err != nil {
				continue
			}
			if err := transportProbe(transport, ports, tlsCfg)(deadline); err != nil {
				continue
			}
			return nil
		}
	}
}

// daemonProbeTLSConfig builds a TLS config that trusts the ephemeral daemon's
// OWN auto-issued CA chain (caChainPath(dataDir)), not the integration cluster
// CA. Each init test spawns a throwaway quicbaasd bound to 127.0.0.1 with a
// self-bootstrapped PKI, so helpers.IntegrationTLSConfig (which pins the cluster
// CA at /etc/quicbaas/certs/ca.crt) cannot verify it — pinning the cluster CA
// is what made the readiness probes reject the daemon's cert and hang until the
// start deadline. The daemon's serving leaf is valid for 127.0.0.1, so
// RootCAs+ServerName verify cleanly without InsecureSkipVerify. Returns an error
// until the chain exists, which the readiness loop treats as "not ready yet".
func daemonProbeTLSConfig(dataDir string) (*tls.Config, error) {
	path := caChainPath(dataDir)
	pemBytes, err := os.ReadFile(path) // #nosec G304 - path rooted at t.TempDir
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("daemonProbeTLSConfig: no certificates in %s", path)
	}
	return &tls.Config{
		RootCAs:    pool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS13,
	}, nil
}

// probeREST_Ready GETs /api/v1/health/ready. Ready (not live) is the
// daemon's "fully bootstrapped" gate — by the time it returns 200 the
// IAM bootstrap, PKI bootstrap, and every configured listener are up.
func probeREST_Ready(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeREST_Ready: port=0")
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
		return fmt.Errorf("probeREST_Ready: %s -> %d", url, resp.StatusCode)
	}
	return nil
}

// transportProbe returns a probe function for the named transport. Each
// probe verifies the transport's listener is accepting connections —
// stronger than a simple TCP open because we also exercise the
// transport's protocol handshake where cheap (HTTP GET, gRPC dial). The
// tlsCfg trusts the daemon's own auto-issued CA chain and is used by the
// HTTPS-bearing probes (rest, mcp); the raw socket probes ignore it.
func transportProbe(transport string, ports PortBundle, tlsCfg *tls.Config) func(ctx context.Context) error {
	switch transport {
	case "rest":
		return func(ctx context.Context) error { return probeREST(ctx, ports.REST, tlsCfg) }
	case "grpc":
		return func(ctx context.Context) error { return probeTCP(ctx, ports.GRPC) }
	case "quic":
		return func(ctx context.Context) error { return probeUDP(ctx, ports.QUIC) }
	case "mcp":
		// MCP serves https; the JSON-RPC envelope requires POST. A bare
		// TCP open + TLS handshake is enough to confirm the listener is
		// up without paying the auth cost of a real RPC.
		return func(ctx context.Context) error { return probeTLS(ctx, ports.MCP, tlsCfg) }
	case "unix":
		return func(ctx context.Context) error { return probeUnix(ctx, ports.Unix) }
	}
	return func(_ context.Context) error { return fmt.Errorf("unknown transport %q", transport) }
}

// probeREST GETs /api/v1/health/live over HTTPS. The daemon auto-issues
// a self-signed cert via xkms; we cannot trust it via the system pool so
// we skip verification — the goal is reachability, not authenticity. The
// dedicated SDK-level liveness probe in the matrix asserts the
// authenticated end-to-end semantics afterwards.
func probeREST(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeREST: port=0")
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/api/v1/health/live", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("probeREST: %s -> %d", url, resp.StatusCode)
	}
	return nil
}

// probeTLS opens a TLS handshake against the listener. Skips cert
// verification — see probeREST commentary. Used for MCP whose only
// uniformly available reachability oracle is the TLS port itself.
func probeTLS(ctx context.Context, port int, tlsCfg *tls.Config) error {
	if port == 0 {
		return errors.New("probeTLS: port=0")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("127.0.0.1:%d", port), tlsCfg)
	if err != nil {
		return err
	}
	_ = conn.Close()
	_ = ctx
	return nil
}

// probeTCP opens a plain TCP connection to confirm the listener is
// accepting connections. Used by the gRPC probe because the daemon's
// gRPC port speaks TLS — a TCP open is the cheapest indication that the
// listener bound the port without paying the full mTLS handshake cost.
func probeTCP(ctx context.Context, port int) error {
	if port == 0 {
		return errors.New("probeTCP: port=0")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	return conn.Close()
}

// probeUDP confirms the QUIC port is actually bound by a listener. UDP
// has no handshake, so the kernel happily accepts sendto on an
// unbound port — only a follow-up read surfaces the ICMP "port
// unreachable" the kernel queues. The probe pattern:
//
//  1. Dial UDP to 127.0.0.1:port.
//  2. Write a 1-byte payload (junk QUIC datagram).
//  3. Read with a short deadline. A listener accepts the datagram and
//     stays silent; an unbound port replies ICMP "unreachable" which
//     surfaces as a "connection refused" Read error.
//
// The probe returns nil for "no read error AND timeout fired" (listener
// silent) OR "read returned data" (listener replied). It returns an
// error for "ECONNREFUSED" (no listener) or any other unexpected error.
//
// QUIC datagrams are big enough that QUIC servers will not reply to a
// random 1-byte payload; a Read timeout is the expected path.
func probeUDP(ctx context.Context, port int) error {
	if port == 0 {
		return errors.New("probeUDP: port=0")
	}
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer conn.Close()
	writeDeadline := time.Now().Add(500 * time.Millisecond)
	readDeadline := time.Now().Add(500 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok {
		if d.Before(writeDeadline) {
			writeDeadline = d
		}
		if d.Before(readDeadline) {
			readDeadline = d
		}
	}
	_ = conn.SetWriteDeadline(writeDeadline)
	if _, werr := conn.Write([]byte{0x00}); werr != nil {
		return werr
	}
	_ = conn.SetReadDeadline(readDeadline)
	buf := make([]byte, 1)
	_, rerr := conn.Read(buf)
	if rerr == nil {
		// Listener replied — unambiguously bound.
		return nil
	}
	var nerr net.Error
	if errors.As(rerr, &nerr) && nerr.Timeout() {
		// Listener silent — that is the expected QUIC behaviour for an
		// invalid handshake. Listener is up.
		return nil
	}
	// Anything else (e.g. "connection refused" via ICMP) means the
	// port is not bound by a UDP listener.
	return rerr
}

// probeUnix connects to the Unix Domain Socket the daemon's gRPC server
// binds when --unix-socket is set. A successful connect proves the
// listener is up.
func probeUnix(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("probeUnix: empty path")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	return conn.Close()
}

// ---------------------------------------------------------------------------
// On-disk post-condition verification
// ---------------------------------------------------------------------------

// initDoneMarkerName mirrors cmd/quicbaasd/init.go's constant. The matrix
// reads the marker to assert the init flow recorded the exact strategy /
// officer-count tuple the test invoked. We cannot import the const
// directly without inheriting the daemon's full build-tag matrix; the
// duplication is one line and is covered by both the unit test in
// cmd/quicbaasd and the integration tests below.
const initDoneMarkerName = ".quicbaasd_init_done"

// onDiskInitDoneMarker is the subset of cmd/quicbaasd/init.go's
// initDoneMarker struct the matrix needs to assert. Keeping the struct
// local makes the test self-contained and avoids a circular dependency
// against the daemon binary's main package.
type onDiskInitDoneMarker struct {
	BarrierStrategy    string    `json:"barrier_strategy"`
	CredentialStrategy string    `json:"credential_strategy"`
	Hostname           string    `json:"hostname,omitempty"`
	Shares             int       `json:"shares,omitempty"`
	Threshold          int       `json:"threshold,omitempty"`
	CompletedAt        time.Time `json:"completed_at"`
}

// readInitDoneMarker parses the marker file written by quicbaasd init.
// Missing / malformed markers return typed errors so the matrix can
// distinguish "init never ran" from "init recorded the wrong fields".
func readInitDoneMarker(dataDir string) (*onDiskInitDoneMarker, error) {
	path := filepath.Join(dataDir, initDoneMarkerName)
	// #nosec G304 - path rooted at t.TempDir()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &MarkerReadError{Path: path, Cause: err}
	}
	var m onDiskInitDoneMarker
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, &MarkerParseError{Path: path, Cause: err}
	}
	return &m, nil
}

// caChainPath returns the path quicbaasd writes the platform CA chain
// to after a successful start. The presence of the file proves the
// daemon's xkms-backed CA bootstrap ran and persisted the platform trust
// material — the necessary precursor for issuing the admin client cert.
func caChainPath(dataDir string) string {
	return filepath.Join(dataDir, "certs", "ca-chain.crt")
}

// serverCertPath returns the path quicbaasd writes its TLS server cert
// to after a successful start. Presence indicates the daemon issued at
// least one cert via its CA — the load-bearing signal that the PKI is
// operational.
func serverCertPath(dataDir string) string {
	return filepath.Join(dataDir, "certs", "server.crt")
}

// requireFileExists fails the test when path is missing. Helper for the
// post-start CA / cert assertions that all matrix combinations share.
func requireFileExists(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s missing at %s: %v", label, path, err)
	}
}

// ---------------------------------------------------------------------------
// Typed errors
// ---------------------------------------------------------------------------

// MarkerReadError indicates the init-done marker could not be read.
type MarkerReadError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *MarkerReadError) Error() string {
	return "init-done marker " + e.Path + ": read: " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *MarkerReadError) Unwrap() error { return e.Cause }

// MarkerParseError indicates the init-done marker was unparseable JSON.
type MarkerParseError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *MarkerParseError) Error() string {
	return "init-done marker " + e.Path + ": parse: " + e.Cause.Error()
}

// Unwrap exposes the underlying cause.
func (e *MarkerParseError) Unwrap() error { return e.Cause }

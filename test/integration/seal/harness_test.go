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

// Package seal contains end-to-end integration tests for the cloud- and
// hardware-backed barrier sealing strategies that go-xkms supplies and
// quicbaas composes through the build-tagged seal factories
// (pkg/server/seal_factory_<backend>.go).
//
// Unlike the cli barrier suite (which exercises the pure-qrdb software /
// shamir / none strategies against a fresh daemon), this suite spins up a
// daemon configured with a CLOUD strategy (awskms, vault, ...) pointed at a
// local emulator (LocalStack KMS, a Vault dev server) and drives the full
//
//	barrier init -> seal -> unseal
//
// round-trip through the CLI. The round-trip proves that the seal factory
// constructed a working go-xkms backend, that the data-plane StorageBarrier
// registered the cloud strategy, and that the cloud KMS sealed and recovered
// the barrier root key.
//
// Each cloud backend is gated by its own go build tag (awskms, vault, ...)
// so the suite only compiles the factory + test for a backend when the
// daemon binary under test was built with the matching tag. The harness in
// this file is tag-free; the per-backend tests live in awskms_test.go,
// vault_test.go, etc. behind their build tags.
//
// The harness deliberately mirrors the per-test daemon supervision pattern
// from test/integration/cli/barrier_test.go (SIGTERM -> SIGKILL escalation,
// channel-driven readiness polling, no time.Sleep) but is fully self-
// contained so this package carries no dependency on package cli internals.
package seal

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
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
)

// daemonBinaryEnv overrides the auto-discovered quicbaasd path. The seal
// Makefile target sets it to the cloud-seal-tag-built daemon so the suite
// runs against a binary that actually has the awskms/vault factories
// compiled in.
const daemonBinaryEnv = "QUICBAASD_BINARY"

// cliBinaryEnv overrides the auto-discovered quicbaas CLI path.
const cliBinaryEnv = "QUICBAAS_BINARY"

// daemonStartTimeout bounds the wait for a per-test daemon to reach
// readiness. Cloud-strategy boot adds the seal-factory backend construction
// (and, for vault, a TLS handshake to the dev server) on top of the
// PebbleDB cold boot; 120s comfortably covers a CI cold boot while still
// failing fast on a genuinely broken combination.
const daemonStartTimeout = 120 * time.Second

// daemonShutdownTimeout bounds the SIGTERM drain before SIGKILL.
const daemonShutdownTimeout = 20 * time.Second

// daemonReadyPollInterval is the gap between readiness polls. Channel-driven
// polling (no time.Sleep) per CLAUDE.md.
const daemonReadyPollInterval = 250 * time.Millisecond

// portRetries caps the loopback-port allocation retry count.
const portRetries = 50

// commandTimeout bounds a single CLI verb invocation.
const commandTimeout = 45 * time.Second

// allocatedPortsMu guards the cross-subtest port reservation set so parallel
// daemons cannot collide on a freshly-freed loopback port.
var (
	allocatedPortsMu sync.Mutex
	allocatedPorts   = map[int]struct{}{}
)

// sealDaemonPorts is the per-test port + socket allocation. The seal suite
// drives the barrier over gRPC + REST only (the seal round-trip is transport-
// agnostic on the server side; one TCP + one UDS transport is sufficient to
// prove the strategy works without re-paying the full five-transport boot
// cost per subtest). QUIC/MCP listeners are left disabled to keep boot lean.
type sealDaemonPorts struct {
	REST int
	GRPC int
	Unix string
}

// allocTCPPort reserves a free loopback TCP port for the per-test daemon.
func allocTCPPort(t *testing.T) int {
	t.Helper()
	for attempt := 0; attempt < portRetries; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if cerr := ln.Close(); cerr != nil {
			t.Fatalf("close port-allocation listener: %v", cerr)
		}
		allocatedPortsMu.Lock()
		_, taken := allocatedPorts[port]
		if !taken {
			allocatedPorts[port] = struct{}{}
			allocatedPortsMu.Unlock()
			return port
		}
		allocatedPortsMu.Unlock()
	}
	t.Fatalf("allocTCPPort: %d retries exhausted", portRetries)
	return 0
}

// allocPorts reserves the REST + gRPC ports and a Unix socket path under the
// per-test data dir (auto-removed with t.TempDir).
func allocPorts(t *testing.T, dataDir string) sealDaemonPorts {
	t.Helper()
	return sealDaemonPorts{
		REST: allocTCPPort(t),
		GRPC: allocTCPPort(t),
		Unix: filepath.Join(dataDir, "d.sock"),
	}
}

// sealDaemon wraps a supervised quicbaasd subprocess configured with a cloud
// sealing strategy.
type sealDaemon struct {
	cmd      *exec.Cmd
	logPath  string
	logFile  *os.File
	dataDir  string
	caPath   string
	ports    sealDaemonPorts
	doneCh   chan struct{}
	waitErr  atomic.Pointer[error]
	stopOnce sync.Once
	stopErr  error
}

// LogPath returns the daemon's combined stdout+stderr log path.
func (d *sealDaemon) LogPath() string { return d.logPath }

// Done returns a channel closed when the daemon process exits.
func (d *sealDaemon) Done() <-chan struct{} { return d.doneCh }

// WaitErr returns the os/exec wait error after the process exits.
func (d *sealDaemon) WaitErr() error {
	if p := d.waitErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Stop signals SIGTERM, escalating to SIGKILL after daemonShutdownTimeout.
// First call wins; the memguard signal handler exits with code 1 on a clean
// SIGTERM, so both code 0 and the signalled-1 path are treated as success.
func (d *sealDaemon) Stop() error {
	d.stopOnce.Do(func() {
		if d.cmd == nil || d.cmd.Process == nil {
			return
		}
		select {
		case <-d.doneCh:
			d.stopErr = sanitizeWaitErr(d.WaitErr(), false)
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
			d.stopErr = sanitizeWaitErr(d.WaitErr(), true)
		case <-time.After(daemonShutdownTimeout):
			_ = d.cmd.Process.Signal(syscall.SIGKILL)
			<-d.doneCh
			d.stopErr = fmt.Errorf("sealDaemon SIGTERM exceeded %s; SIGKILL applied; wait err=%v",
				daemonShutdownTimeout, d.WaitErr())
		}
		if d.logFile != nil {
			_ = d.logFile.Close()
		}
	})
	return d.stopErr
}

// sanitizeWaitErr maps exit outcomes per the memguard SIGTERM contract.
func sanitizeWaitErr(waitErr error, signalled bool) error {
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

// resolveDaemonBinary returns an absolute path to the cloud-seal-tag-built
// quicbaasd. Order: $QUICBAASD_BINARY -> /usr/local/bin/quicbaasd ->
// <repo-root>/bin/quicbaasd. We do NOT fall back to PATH.
func resolveDaemonBinary(t *testing.T) string {
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
	if _, err := os.Stat("/usr/local/bin/quicbaasd"); err == nil {
		return "/usr/local/bin/quicbaasd"
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	candidate := filepath.Join(repoRoot, "bin", "quicbaasd")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	t.Fatalf("quicbaasd binary not found in $%s, /usr/local/bin, or %s; "+
		"build with the cloud seal tags or set $%s", daemonBinaryEnv, candidate, daemonBinaryEnv)
	return ""
}

// resolveCLIBinary returns an absolute path to the quicbaas CLI.
func resolveCLIBinary(t *testing.T) string {
	t.Helper()
	if explicit := os.Getenv(cliBinaryEnv); explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
	}
	if _, err := os.Stat("/app/bin/quicbaas"); err == nil {
		return "/app/bin/quicbaas"
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	candidate := filepath.Join(repoRoot, "bin", "quicbaas")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	t.Fatalf("quicbaas CLI not found in $%s, /app/bin, or %s; set $%s",
		cliBinaryEnv, candidate, cliBinaryEnv)
	return ""
}

// findRepoRoot walks upward from this file until it finds go.mod.
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
			return "", errors.New("no go.mod found above " + filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// spawnSealDaemon boots a quicbaasd configured with the supplied cloud
// sealing strategy. cfgYAML is the seal-config YAML rendered for the chosen
// backend (it carries seal.strategy + the backend sub-config such as
// seal.vault.address / seal.awskms.endpoint). The QUICBAAS_SEAL_STRATEGY env
// flips cfg.Seal.StrategyExplicit on so the configured cloud strategy wins
// over the "none" init marker written by the daemon-side init below.
//
// Boot sequence (mirrors the barrier suite's two-phase pattern):
//
//  1. quicbaasd init --barrier-strategy none — cheapest first-boot bootstrap;
//     populates the data dir + CA without paying a Shamir/PIN ceremony.
//  2. quicbaasd start --config <cfgYAML> with QUICBAAS_SEAL_STRATEGY=<id> —
//     the data-plane barrier is rebuilt with the cloud strategy + backend
//     sub-config, and the listeners come up.
func spawnSealDaemon(t *testing.T, strategyID, cfgYAML string) *sealDaemon {
	return spawnSealDaemonWithEnv(t, strategyID, cfgYAML, nil)
}

// spawnSealDaemonWithEnv is spawnSealDaemon extended with extra env vars
// injected into the daemon start process. extraEnv entries are appended after
// the base env and take precedence over any base-env entries with the same key.
// This is used by hardware-strategy tests (e.g. SOFTHSM2_CONF for pkcs11) that
// need the daemon to find a per-test resource the base env does not carry.
func spawnSealDaemonWithEnv(t *testing.T, strategyID, cfgYAML string, extraEnv []string) *sealDaemon {
	t.Helper()

	binary := resolveDaemonBinary(t)
	dataDir := t.TempDir()

	// Phase 1: daemon-side init to populate the data dir + CA.
	initCtx, cancelInit := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelInit()
	initCmd := exec.CommandContext(initCtx, binary,
		"init",
		"--barrier-strategy", "none",
		"--data-dir", dataDir,
	) // #nosec G204 - args produced by test
	initCmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dataDir}
	if initOut, initErr := initCmd.CombinedOutput(); initErr != nil {
		t.Fatalf("quicbaasd init for seal suite failed: %v\noutput:\n%s", initErr, string(initOut))
	}

	// Render the seal config YAML to a file under the data dir.
	cfgPath := filepath.Join(dataDir, "seal.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write seal config: %v", err)
	}

	// Phase 2: allocate ports and start with the cloud strategy.
	ports := allocPorts(t, dataDir)
	daemon := startSealDaemonProcess(t, binary, dataDir, cfgPath, strategyID, ports, extraEnv)
	t.Cleanup(func() {
		if stopErr := daemon.Stop(); stopErr != nil {
			t.Logf("sealDaemon Stop: %v", stopErr)
		}
		if t.Failed() {
			dumpLogTail(t, daemon.LogPath(), 120)
		}
	})

	// Phase 3: block until REST + gRPC + CA chain are all reachable.
	startCtx, cancelStart := context.WithTimeout(context.Background(), daemonStartTimeout)
	defer cancelStart()
	if err := waitForDaemonReady(startCtx, daemon); err != nil {
		t.Fatalf("seal daemon never became ready: %v", err)
	}

	caPath := filepath.Join(dataDir, "certs", "ca-chain.crt")
	if _, err := os.Stat(caPath); err != nil {
		t.Fatalf("CA chain missing at %s after start: %v", caPath, err)
	}
	daemon.caPath = caPath
	return daemon
}

// startSealDaemonProcess launches `quicbaasd start --config <cfgPath>` with
// REST + gRPC + Unix enabled and the cloud seal strategy override in the env.
// extraEnv is an optional slice of "KEY=VALUE" strings appended after the base
// env; it is used by hardware-strategy tests (e.g. SOFTHSM2_CONF) that need
// the daemon to find a per-test resource the base env does not carry.
func startSealDaemonProcess(t *testing.T, binary, dataDir, cfgPath, strategyID string, ports sealDaemonPorts, extraEnv []string) *sealDaemon {
	t.Helper()

	args := []string{
		"start",
		"--config", cfgPath,
		"--data-dir", dataDir,
		"--log-level", "info",
		"--http-port", fmt.Sprintf("%d", ports.REST),
		"--grpc-port", fmt.Sprintf("%d", ports.GRPC),
		"--unix-socket", ports.Unix,
	}

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dataDir,
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		"QUICBAAS_DISCOVERY_ENABLED=false",
		"QUICBAAS_ENABLE_SCHEDULER=false",
		"QUICBAAS_SERVER_QUIC_ENABLED=false",
		"QUICBAAS_SERVER_MCP_ENABLED=false",
		// Flip StrategyExplicit on so the configured cloud strategy wins over
		// the "none" init marker. The value must match the YAML's
		// seal.strategy (viper.IsSet("seal.strategy") is what flips the flag).
		"QUICBAAS_SEAL_STRATEGY=" + strategyID,
	}
	env = append(env, extraEnv...)

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if startErr := cmd.Start(); startErr != nil {
		_ = logFile.Close()
		t.Fatalf("start quicbaasd: %v", startErr)
	}

	d := &sealDaemon{
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

// waitForDaemonReady blocks until the per-test CA chain lands on disk, the
// REST readiness probe passes (using that per-test CA), and the gRPC port
// accepts a TCP connection — or the deadline fires, or the daemon crashes.
//
// The probe order matters: the CA file is written by the daemon as part of TLS
// bootstrap, which happens before the REST listener opens. We therefore wait for
// the CA file first, build a per-test cert pool from it, then probe REST. This
// avoids falling back to the shared cluster CA (helpers.IntegrationTLSConfig)
// which resolves /etc/quicbaas/certs/ca.crt — a path only valid inside the
// Docker integration cluster, not on the host where the seal suite runs.
func waitForDaemonReady(ctx context.Context, d *sealDaemon) error {
	caPath := filepath.Join(d.dataDir, "certs", "ca-chain.crt")
	ticker := time.NewTicker(daemonReadyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("seal daemon not ready within %s: %w", daemonStartTimeout, ctx.Err())
		case <-d.Done():
			return fmt.Errorf("seal daemon exited before ready: wait_err=%v", d.WaitErr())
		case <-ticker.C:
			// Wait for the CA chain to land on disk before attempting TLS probes.
			if _, err := os.Stat(caPath); err != nil {
				continue
			}
			if err := probeREST(ctx, d.ports.REST, caPath); err != nil {
				continue
			}
			if err := probeTCP(ctx, d.ports.GRPC); err != nil {
				continue
			}
			return nil
		}
	}
}

// probeREST GETs /api/v1/health/ready using the per-test CA at caPath. A 200
// is the daemon's fully-bootstrapped gate. caPath must not be empty; the seal
// suite always provides the daemon's own CA chain so the TLS handshake trusts
// the server certificate without InsecureSkipVerify.
func probeREST(ctx context.Context, port int, caPath string) error {
	if port == 0 {
		return errors.New("probeREST: port=0")
	}
	if caPath == "" {
		return errors.New("probeREST: caPath is empty")
	}

	// Build a cert pool from the per-test CA chain so the probe trusts exactly
	// this daemon's self-signed CA without touching the system cert store.
	pemBytes, err := os.ReadFile(caPath) // #nosec G304 — caPath rooted at t.TempDir
	if err != nil {
		return fmt.Errorf("probeREST: read CA %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return fmt.Errorf("probeREST: no PEM certificates in %s", caPath)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS13,
			},
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
		return fmt.Errorf("probeREST: %s -> %d", url, resp.StatusCode)
	}
	return nil
}

// probeTCP opens a raw TCP connection (gRPC port liveness).
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

// dumpLogTail prints the last n lines of the daemon log so a failing test's
// diagnostics appear in `go test` output.
func dumpLogTail(t *testing.T, path string, n int) {
	t.Helper()
	// #nosec G304 - log path rooted at t.TempDir
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("dumpLogTail: %s: %v", path, err)
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

// runCLI executes a quicbaas CLI verb over gRPC against the supplied daemon
// and returns combined output + error. gRPC is used uniformly: the seal
// round-trip is transport-agnostic on the server side, so one transport is
// sufficient to prove the cloud strategy works. The CA path is the daemon's
// auto-issued chain so --tls-ca is honoured.
func runCLI(t *testing.T, daemon *sealDaemon, args ...string) (string, error) {
	t.Helper()
	cli := resolveCLIBinary(t)
	full := []string{
		"--address", fmt.Sprintf("127.0.0.1:%d", daemon.ports.GRPC),
		"--transport", "grpc",
		"--tls-ca", daemon.caPath,
	}
	full = append(full, args...)

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, full...) // #nosec G204 - args produced by test
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	t.Logf("[grpc] %s %s -> err=%v\n%s", cli, strings.Join(full, " "), err, out)
	return out, err
}

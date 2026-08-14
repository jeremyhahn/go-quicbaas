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

package durability

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// daemonBinaryEnv overrides the auto-discovered quicbaasd path, matching the
// convention used by test/integration/init and test/integration/seal.
const daemonBinaryEnv = "QUICBAASD_BINARY"

// daemonReadyTimeout bounds the cold-boot wait. PebbleDB open plus the xkms CA
// bootstrap dominate first-boot latency.
const daemonReadyTimeout = 90 * time.Second

// daemonExitTimeout bounds the graceful shutdown that SIGTERM must trigger.
const daemonExitTimeout = 60 * time.Second

// readyProbeInterval is the gap between readiness polls. Ticker-driven polling
// avoids time.Sleep loops.
const readyProbeInterval = 200 * time.Millisecond

// gracefulShutdownMarker is the first line runDaemon logs once its own SIGTERM
// handler wins the race. Its absence means something exited the process from
// inside a signal handler.
const gracefulShutdownMarker = "Performing graceful shutdown..."

// nodeCloseMarker is logged by Server.Shutdown after the embedded qrdb node
// (and therefore its PebbleDB) has been closed. This is the exact step that was
// skipped when memguard hard-exited the process, and skipping it is what
// discarded buffered writes.
const nodeCloseMarker = "Embedded qrdb node shutdown complete"

// resolveDaemonBinary returns an absolute path to the quicbaasd binary built
// from the current tree. PATH is deliberately not consulted so the test can
// never silently validate a stale install.
func resolveDaemonBinary(t *testing.T) string {
	t.Helper()

	if explicit := os.Getenv(daemonBinaryEnv); explicit != "" {
		abs, err := filepath.Abs(explicit)
		require.NoError(t, err, "resolve $%s=%s", daemonBinaryEnv, explicit)
		_, statErr := os.Stat(abs)
		require.NoError(t, statErr, "$%s=%s not found", daemonBinaryEnv, abs)
		return abs
	}

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) must resolve the test file path")
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod found walking upward from %s", thisFile)
		dir = parent
	}

	candidate := filepath.Join(dir, "bin", "quicbaasd")
	_, err := os.Stat(candidate)
	require.NoError(t, err,
		"quicbaasd not found at %s; run `make build-daemon` or set $%s", candidate, daemonBinaryEnv)
	return candidate
}

// reservePort binds an ephemeral port and releases it, returning the number.
func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "reserve ephemeral port")
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// waitForREST waits until the daemon's HTTPS listener is accepting TLS
// connections, or the deadline expires. The daemon mints its own CA during
// first boot, so this probe cannot chain-verify the presented certificate --
// and QuicBaaS bans InsecureSkipVerify outright, so verification is never
// disabled. Instead the probe performs a real handshake and treats a
// *certificate verification* failure as proof that the listener answered.
// Connection-level errors (refused, reset, timeout) mean the daemon is not up
// yet, so the probe keeps polling. Reachability is the only signal needed.
func waitForREST(t *testing.T, port int, exited <-chan struct{}) error {
	t.Helper()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	dialer := &tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS13}}

	ctx, cancel := context.WithTimeout(context.Background(), daemonReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(readyProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &qberrors.MessageError{Message: "daemon REST endpoint not ready within " + daemonReadyTimeout.String()}
		case <-exited:
			return &qberrors.MessageError{Message: "daemon exited before becoming ready"}
		case <-ticker.C:
			probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
			conn, err := dialer.DialContext(probeCtx, "tcp", addr)
			probeCancel()
			if err == nil {
				conn.Close()
				return nil
			}
			// The listener answered with a certificate this client has no
			// reason to trust: still proof the daemon is serving TLS.
			var verifyErr *tls.CertificateVerificationError
			if errors.As(err, &verifyErr) {
				return nil
			}
		}
	}
}

// TestDaemonSIGTERMRunsGracefulShutdown is the end-to-end regression test for
// the reported data loss. It spawns the real quicbaasd binary, sends SIGTERM,
// and asserts the daemon shut down gracefully.
//
// Under the old wiring main() called secure.CatchInterrupt(), whose memguard
// handler purged memory and then os.Exit(1)ed from inside the signal handler.
// That beat runDaemon's own handler every time, so:
//
//	exit code       1 (memguard) instead of 0 (graceful)
//	shutdown log    empty — Server.Shutdown never ran
//	embedded node   never closed, so buffered PebbleDB writes were discarded
//
// All three are asserted below; each one alone fails on the old binary.
func TestDaemonSIGTERMRunsGracefulShutdown(t *testing.T) {
	binary := resolveDaemonBinary(t)
	dataDir := t.TempDir()
	restPort := reservePort(t)
	logPath := filepath.Join(dataDir, "daemon.log")

	logFile, err := os.Create(logPath) //nolint:gosec // G304: path is test-owned under t.TempDir()
	require.NoError(t, err, "create daemon log file")
	defer logFile.Close()

	cmd := exec.Command(binary, //nolint:gosec // G204: arguments are produced by this test
		"start",
		"--data-dir", dataDir,
		"--log-level", "info",
		"--http-port", strconv.Itoa(restPort),
		"--grpc-port", "0",
		"--unix-socket", filepath.Join(dataDir, "q.sock"),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		// HOME isolates xkms key material under the data dir.
		"HOME="+dataDir,
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		"QUICBAAS_SERVER_QUIC_ENABLED=false",
		"QUICBAAS_SERVER_MCP_ENABLED=false",
		"QUICBAAS_DISCOVERY_ENABLED=false",
		"QUICBAAS_ENABLE_SCHEDULER=false",
	)

	require.NoError(t, cmd.Start(), "spawn quicbaasd")

	exited := make(chan struct{})
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
		close(exited)
	}()

	// Guarantee the daemon is reaped even if an assertion below fails early.
	defer func() {
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			<-exited
		}
	}()

	require.NoError(t, waitForREST(t, restPort, exited), "daemon must become ready")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM), "deliver SIGTERM")

	var runErr error
	select {
	case runErr = <-waitErr:
	case <-time.After(daemonExitTimeout):
		t.Fatalf("daemon did not exit within %s of SIGTERM", daemonExitTimeout)
	}

	assert.NoError(t, runErr,
		"SIGTERM must exit 0 via the graceful path; a non-zero status means a signal "+
			"handler hard-exited the process before Server.Shutdown could close the store")

	logBytes, readErr := os.ReadFile(logPath) //nolint:gosec // G304: path is test-owned under t.TempDir()
	require.NoError(t, readErr, "read daemon log")
	logText := string(logBytes)

	assert.True(t, strings.Contains(logText, gracefulShutdownMarker),
		"daemon log must contain %q — its absence means SIGTERM never reached runDaemon's handler",
		gracefulShutdownMarker)
	assert.True(t, strings.Contains(logText, nodeCloseMarker),
		"daemon log must contain %q — the embedded qrdb node (and its PebbleDB) must be "+
			"closed on shutdown or buffered writes are lost",
		nodeCloseMarker)
}

// TestDaemonSIGINTRunsGracefulShutdown covers the sibling signal. Ctrl-C from
// an interactive operator must be as safe for on-disk state as `systemctl
// stop`: memguard's handler was registered for SIGINT as well, so the same
// data loss occurred on interactive shutdown.
func TestDaemonSIGINTRunsGracefulShutdown(t *testing.T) {
	binary := resolveDaemonBinary(t)
	dataDir := t.TempDir()
	restPort := reservePort(t)
	logPath := filepath.Join(dataDir, "daemon.log")

	logFile, err := os.Create(logPath) //nolint:gosec // G304: path is test-owned under t.TempDir()
	require.NoError(t, err, "create daemon log file")
	defer logFile.Close()

	cmd := exec.Command(binary, //nolint:gosec // G204: arguments are produced by this test
		"start",
		"--data-dir", dataDir,
		"--log-level", "info",
		"--http-port", strconv.Itoa(restPort),
		"--grpc-port", "0",
		"--unix-socket", filepath.Join(dataDir, "q.sock"),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"HOME="+dataDir,
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		"QUICBAAS_SERVER_QUIC_ENABLED=false",
		"QUICBAAS_SERVER_MCP_ENABLED=false",
		"QUICBAAS_DISCOVERY_ENABLED=false",
		"QUICBAAS_ENABLE_SCHEDULER=false",
	)

	require.NoError(t, cmd.Start(), "spawn quicbaasd")

	exited := make(chan struct{})
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
		close(exited)
	}()
	defer func() {
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			<-exited
		}
	}()

	require.NoError(t, waitForREST(t, restPort, exited), "daemon must become ready")
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT), "deliver SIGINT")

	select {
	case runErr := <-waitErr:
		assert.NoError(t, runErr, "SIGINT must exit 0 via the graceful path")
	case <-time.After(daemonExitTimeout):
		t.Fatalf("daemon did not exit within %s of SIGINT", daemonExitTimeout)
	}

	logFile.Sync()
	f, openErr := os.Open(logPath) //nolint:gosec // G304: path is test-owned under t.TempDir()
	require.NoError(t, openErr, "open daemon log")
	defer f.Close()

	sawNodeClose := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), nodeCloseMarker) {
			sawNodeClose = true
			break
		}
	}
	require.NoError(t, scanner.Err(), "scan daemon log")
	assert.True(t, sawNodeClose,
		"SIGINT must close the embedded qrdb node so PebbleDB writes are flushed")
}

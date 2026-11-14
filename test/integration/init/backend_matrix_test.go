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

// backend_matrix_test.go adds the STORAGE BACKEND axis to the init matrix.
//
// The existing matrix covers barrier strategy x transport. What it never
// exercised is the requirement that a deployment can select its qrdb storage
// engine — memory, file, or pebble — with only a config change, and have the
// whole stack come up and serve data on it. That axis had no coverage
// anywhere, which is how `quicbaasd init` came to hard-code pebble: init wrote
// first-boot IAM state to one engine while the daemon read from another, and
// nothing failed until an operator tried to log in.
//
// Each cell therefore proves the engine end to end, through the CLI:
//
//	init (on this backend) -> start -> data put -> data get
//
// A liveness probe alone would not catch the split-brain above, because the
// daemon comes up healthy either way. Only a write-then-read round trip
// through the selected engine does.

package init

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// matrixBackends enumerates the qrdb storage engines a deployment may select.
//
// "raft" is deliberately absent: it is a cluster MODE, not a storage engine
// (see config.StorageBackend* vs config.ServerMode*). Clustered coverage lives
// in test/integration/cluster, and pairing it with the non-durable engines
// would assert a deployment nobody runs.
var matrixBackends = []string{"memory", "file", "pebble"}

// backendTransports is the transport set each backend is exercised over.
// The backend is orthogonal to the wire protocol, so every combination is
// meaningful: a serialization path that works over REST and breaks over QUIC
// is exactly the kind of drift this matrix exists to catch.
var backendTransports = []string{"rest", "grpc", "quic", "mcp", "unix"}

// TestBackendMatrix runs every storage backend across every transport.
func TestBackendMatrix(t *testing.T) {
	requireIntegrationGate(t)
	daemonBinary := resolveDaemonBinary(t)
	cliBinary := resolveCLIBinary(t)

	for _, backend := range matrixBackends {
		for _, transport := range backendTransports {
			backend, transport := backend, transport
			t.Run(backend+"_"+transport, func(t *testing.T) {
				runBackendCell(t, daemonBinary, cliBinary, backend, transport)
			})
		}
	}
}

// runBackendCell is the per-(backend, transport) body. It is linear so a
// failure points at exactly one phase.
func runBackendCell(t *testing.T, daemonBinary, cliBinary, backend, transport string) {
	t.Helper()

	dataDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Phase 1: init ON THIS BACKEND. The backend travels in the config file,
	// which is the operator-facing contract: selecting an engine must require
	// only a config change.
	initOut, initErr := runInitOnBackend(ctx, daemonBinary, dataDir, backend)
	if initErr != nil {
		t.Fatalf("init on backend %q failed: %v\noutput:\n%s", backend, initErr, initOut)
	}

	marker, err := readInitDoneMarker(dataDir)
	if err != nil {
		t.Fatalf("read init-done marker: %v", err)
	}
	if marker.BarrierStrategy != "none" {
		t.Errorf("marker.BarrierStrategy = %q, want none", marker.BarrierStrategy)
	}

	// Phase 2: start the daemon on the SAME backend and wait for the
	// transport under test.
	ports := allocPortBundle(t, transport, dataDir)
	daemon := startDaemonOnBackend(t, daemonBinary, dataDir, transport, ports)
	if err := waitForTransportReady(ctx, t, daemon, transport, ports, dataDir); err != nil {
		t.Fatalf("backend %q over %q never became ready: %v", backend, transport, err)
	}

	// Phase 3: log in as the administrator first boot issued. On the memory
	// engine `quicbaasd start` runs first boot again in the serving process
	// (the init process's state is not durable) and delivers a fresh admin
	// bundle the same way, so every engine logs in identically.
	token := loginFirstBootAdmin(ctx, t, cliBinary, transport, ports, dataDir, backend)

	// Phase 4: the SDK reaches the daemon over this transport, with that
	// administrator's session (MCP refuses an uncredentialed initialize).
	verifySDKLivenessOverTransport(t, transport, ports, dataDir, token)

	// Phase 5: a real round trip through the engine. This is the assertion
	// that distinguishes a working backend from a daemon that merely booted.
	// Every engine, memory included, authenticates with the administrator
	// first boot issued: no backend is exempt.
	verifyDataRoundTrip(ctx, t, cliBinary, transport, ports, dataDir, backend)

	if stopErr := daemon.Stop(); stopErr != nil {
		t.Fatalf("daemon Stop: %v", stopErr)
	}
}

// runInitOnBackend invokes `quicbaasd init` with the storage backend selected
// in the config file.
//
// The barrier strategy is "none" so each cell isolates the STORAGE variable:
// barrier behaviour already has its own matrix, and combining the two would
// multiply runtime without testing anything the pair does not test apart.
func runInitOnBackend(ctx context.Context, binary, dataDir, backend string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("runInitOnBackend: mkdir data dir: %w", err)
	}
	cfgPath, err := writeBackendConfig(dataDir, backend)
	if err != nil {
		return "", err
	}

	args := []string{
		"init",
		"--config", cfgPath,
		"--barrier-strategy", "none",
		"--data-dir", dataDir,
	}
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, "HOME="+dataDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startDaemonOnBackend starts the daemon on the config file runInitOnBackend
// wrote, so start and init agree on the engine and the trust domain. The
// file is the only channel: spawnDaemon passes the child no environment
// beyond what the harness sets, and the trust domain has no env binding.
func startDaemonOnBackend(
	t *testing.T,
	binary, dataDir, transport string,
	ports PortBundle,
) *DaemonProcess {
	t.Helper()
	return startDaemonWithConfig(t, binary, dataDir, backendConfigPath(dataDir), "none", transport, ports)
}

// verifyDataRoundTrip writes a key through the CLI and reads it back over the
// same transport, proving the selected engine actually stores and serves.
//
// The CLI is the subject deliberately: it is the surface an operator uses, and
// driving it end to end catches contract breaks between CLI, SDK, transport,
// and engine that a direct service-layer call would step over.
// loginFirstBootAdmin logs in, over the transport under test, as the
// administrator first boot issued, and returns the session's access token.
//
// The data plane is fail-closed by default: every transport gates on a
// platform token whose scp claim carries the required resource:action, so a
// client certificate alone is not enough, and the MCP server refuses an
// uncredentialed initialize (CA ruling 571). The CLI caches the session under
// HOME, which the harness points at this cell's data dir, so each cell's
// session stays isolated; the token is also handed back for SDK clients.
func loginFirstBootAdmin(
	ctx context.Context,
	t *testing.T,
	cliBinary, transport string,
	ports PortBundle,
	dataDir, label string,
) string {
	t.Helper()

	// Over MCP the login is made on REST, which every cell serves: the MCP
	// login tools carry no permission registration and an MCP session needs a
	// credential to begin with, so MCP cannot be where the credential is
	// obtained. The session the REST login issues is then used over MCP.
	loginTransport := transport
	if transport == "mcp" {
		loginTransport = "rest"
	}
	loginOut, loginErr := runCLIAgainst(ctx, t, cliBinary, loginTransport, ports, dataDir,
		"auth", "login", "--method", "mtls",
		"--client-cert", filepath.Join(dataDir, "admin", "admin.crt"),
		"--username", "admin")
	if loginErr != nil {
		t.Fatalf("admin mTLS login (%s) over %q failed: %v\noutput:\n%s",
			label, transport, loginErr, loginOut)
	}
	// Log the outcome even on success: a login that exits 0 without issuing a
	// usable token looks identical to a working one until the next call is
	// refused, which is exactly the failure mode this matrix must not hide.
	t.Logf("admin login (%s/%s) output:\n%s", label, transport, loginOut)

	session, err := cli.LoadToken(filepath.Join(dataDir, ".quicbaas", "token"))
	if err != nil {
		t.Fatalf("admin login (%s) over %q stored no session: %v", label, transport, err)
	}
	if session.AccessToken == "" {
		t.Fatalf("admin login (%s) over %q stored an empty access token", label, transport)
	}
	return session.AccessToken
}

func verifyDataRoundTrip(
	ctx context.Context,
	t *testing.T,
	cliBinary, transport string,
	ports PortBundle,
	dataDir, backend string,
) {
	t.Helper()

	// The CLI session loginFirstBootAdmin cached under HOME (this cell's data
	// dir) authenticates every call below: the data plane is fail-closed.

	// data put/get take a COLLECTION and a key, not a single path.
	collection := "backendmatrix"
	key := backend + "-" + transport
	value := `{"backend":"` + backend + `","transport":"` + transport + `"}`

	putOut, putErr := runCLIAgainst(ctx, t, cliBinary, transport, ports, dataDir,
		"data", "put", collection, key, value)
	if putErr != nil {
		// A backend that cannot accept a write is the failure this matrix
		// exists to surface; report the engine and the transport together so
		// the cell is identifiable from the log alone.
		t.Fatalf("data put on backend %q over %q failed: %v\noutput:\n%s",
			backend, transport, putErr, putOut)
	}

	getOut, getErr := runCLIAgainst(ctx, t, cliBinary, transport, ports, dataDir,
		"data", "get", collection, key)
	if getErr != nil {
		t.Fatalf("data get on backend %q over %q failed: %v\noutput:\n%s",
			backend, transport, getErr, getOut)
	}

	// Match on the payload rather than the exact envelope: the assertion is
	// that the ENGINE returned what was written, not that a particular output
	// format was used.
	if !strings.Contains(getOut, backend) || !strings.Contains(getOut, transport) {
		t.Errorf("backend %q over %q: round trip lost the value.\nwrote %s\nread back:\n%s",
			backend, transport, value, getOut)
	}
}

// runCLIAgainst invokes the quicbaas CLI against the running daemon over the
// requested transport.
func runCLIAgainst(
	ctx context.Context,
	t *testing.T,
	binary, transport string,
	ports PortBundle,
	dataDir string,
	args ...string,
) (string, error) {
	t.Helper()

	full := append([]string{"--transport", transport}, args...)
	switch transport {
	case "unix":
		full = append(full, "--address", ports.Unix)
	case "grpc":
		full = append(full, "--address", fmt.Sprintf("127.0.0.1:%d", ports.GRPC))
	case "quic":
		full = append(full, "--address", fmt.Sprintf("127.0.0.1:%d", ports.QUIC))
	case "mcp":
		full = append(full, "--address", fmt.Sprintf("https://127.0.0.1:%d", ports.MCP))
	default: // rest
		full = append(full, "--address", fmt.Sprintf("https://127.0.0.1:%d", ports.REST))
	}
	// The daemon's CA chain is written under the data dir; point the client at
	// it rather than disabling verification. TLS is never turned off.
	full = append(full, "--tls-ca", caChainPath(dataDir))

	// Present the administrator's mTLS credential. The data plane is
	// fail-closed by default (RequireAuthz is a secure default on every config
	// variant, embedded included), so an unauthenticated round trip is
	// correctly refused. Using the certificate first-boot issued also makes
	// each cell prove that credential works over this transport, not just that
	// the storage engine does.
	full = append(full,
		"--tls-cert", filepath.Join(dataDir, "admin", "admin.crt"),
		"--tls-key", filepath.Join(dataDir, "admin", "admin.key"),
	)

	cmd := exec.CommandContext(ctx, binary, full...) // #nosec G204 - test-controlled
	cmd.Env = append(os.Environ(), "HOME="+dataDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestBackendMatrix_InitHonoursConfiguredBackend is the focused regression for
// the split-brain that had no coverage: `quicbaasd init` must write its state
// to the engine the operator configured.
//
// It hard-coded pebble, so an operator selecting memory or file got an init
// that appeared to succeed while the daemon later read from a different store
// and came up with no admin at all.
func TestBackendMatrix_InitHonoursConfiguredBackend(t *testing.T) {
	requireIntegrationGate(t)
	binary := resolveDaemonBinary(t)

	for _, backend := range matrixBackends {
		backend := backend
		t.Run(backend, func(t *testing.T) {
			dataDir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			out, err := runInitOnBackend(ctx, binary, dataDir, backend)
			if err != nil {
				t.Fatalf("init on backend %q failed: %v\noutput:\n%s", backend, err, out)
			}

			// The durable engines must leave state on disk; memory must not
			// pretend to. Either way init has to complete and record itself.
			if _, err := readInitDoneMarker(dataDir); err != nil {
				t.Fatalf("backend %q: init-done marker missing: %v", backend, err)
			}

			if backend == "memory" {
				return
			}
			entries, err := os.ReadDir(dataDir)
			if err != nil {
				t.Fatalf("read data dir: %v", err)
			}
			if len(entries) <= 1 {
				t.Errorf("backend %q: init left only %d entries in the data dir; "+
					"a durable engine must persist first-boot state", backend, len(entries))
			}
		})
	}
}

// decodeJSONOutput is a small helper for cells that need to inspect structured
// CLI output rather than match a substring.
func decodeJSONOutput(t *testing.T, out string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("decode CLI JSON output: %v\nraw:\n%s", err, out)
	}
	return m
}

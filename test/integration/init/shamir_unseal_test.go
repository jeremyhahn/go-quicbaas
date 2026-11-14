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

// shamir_unseal.go drives a sealed shamir node through its officer ceremony
// to unsealed, the way an operator does: over REST with the quicbaas CLI.

package init

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shamirCeremonyOfficers are the officers the matrix's shamir init sized the
// barrier for (--shares 3 --threshold 2 in runShamirInit).
var shamirCeremonyOfficers = []string{"alice", "bob", "carol"}

// shamirCeremonyThreshold is the M of runShamirInit's M-of-N.
const shamirCeremonyThreshold = 2

// requireMCPRefusedWhileSealed pins CA ruling 576: a sealed node does not
// serve MCP at all. Nothing listens on the MCP port, so the dial is refused
// outright; a timeout or an accepted connection both fail the assertion.
func requireMCPRefusedWhileSealed(t *testing.T, ports PortBundle) {
	t.Helper()
	address := fmt.Sprintf("127.0.0.1:%d", ports.MCP)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("a sealed node accepted a connection on its MCP port %s; it must not serve MCP until unsealed", address)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("dial of the sealed node's MCP port %s: got %v, want the connection refused", address, err)
	}
}

// unsealShamirByCeremony runs the M-of-N ceremony against the sealed node's
// REST listener: the barrier is split with one share sealed per officer,
// threshold officers claim their certificate and then their share, and the
// shares unseal the barrier. Nothing in it bypasses the server: every step
// is the CLI an operator runs.
func unsealShamirByCeremony(ctx context.Context, t *testing.T, cliBinary string, ports PortBundle, dataDir string) {
	t.Helper()

	officerDir := t.TempDir()
	keyPath := func(user string) string { return filepath.Join(officerDir, user+".key") }

	certPath := func(user string) string { return filepath.Join(officerDir, user+".crt") }

	// Officers enrol first: the sealed window serves officer registration,
	// and the barrier's split seals one share per registered officer.
	for _, user := range shamirCeremonyOfficers {
		csrPath := filepath.Join(officerDir, user+".csr")
		signer, err := writeOfficerCSR(csrPath, user)
		if err != nil {
			t.Fatalf("officer %s CSR: %v", user, err)
		}
		if err := writeOfficerKey(keyPath(user), user, signer); err != nil {
			t.Fatalf("officer %s key: %v", user, err)
		}
		runCeremonyCLI(ctx, t, cliBinary, ports, dataDir, "officer register "+user,
			"init", "officer", "register", "--username", user, "--csr", csrPath, "--output", certPath(user))
	}

	runCeremonyCLI(ctx, t, cliBinary, ports, dataDir, "barrier init-shamir",
		"barrier", "init-shamir",
		"--passphrase", "shamir-matrix-master-secret",
		"--threshold", fmt.Sprint(shamirCeremonyThreshold),
		"--total", fmt.Sprint(len(shamirCeremonyOfficers)),
		"--so-pin", "so-pin-1234",
		"--user-pin", "user-pin-1234")

	shares := make([]string, 0, shamirCeremonyThreshold)
	for _, user := range shamirCeremonyOfficers[:shamirCeremonyThreshold] {
		certPath := certPath(user)
		sharePath := filepath.Join(officerDir, user+".share")
		runCeremonyCLI(ctx, t, cliBinary, ports, dataDir, "claim-share "+user,
			"init", "claim-share", "--username", user,
			"--tls-cert", certPath, "--tls-key", keyPath(user), "--output", sharePath)
		share, err := os.ReadFile(sharePath) // #nosec G304 - test-controlled path
		if err != nil {
			t.Fatalf("read officer %s's claimed share: %v", user, err)
		}
		shares = append(shares, strings.TrimSpace(string(share)))
	}

	sharesFile := filepath.Join(officerDir, "shares.txt")
	if err := os.WriteFile(sharesFile, []byte(strings.Join(shares, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write the shares file: %v", err)
	}
	runCeremonyCLI(ctx, t, cliBinary, ports, dataDir, "barrier unseal-shares",
		"barrier", "unseal-shares", "--shares-file", sharesFile)
}

// runCeremonyCLI runs one ceremony step over REST, trusting the CA chain the
// node wrote under its data dir, and fails the test with the step's output
// when it does not succeed.
func runCeremonyCLI(ctx context.Context, t *testing.T, cliBinary string, ports PortBundle, dataDir, step string, args ...string) {
	t.Helper()
	full := append([]string{
		"--transport", "unix",
		"--address", ports.Unix,
		"--tls-ca", caChainPath(dataDir),
	}, args...)
	out, err := runCLI(ctx, t, cliBinary, full...)
	if err != nil {
		t.Fatalf("shamir ceremony step %q failed: %v\noutput:\n%s", step, err, out)
	}
	t.Logf("shamir ceremony step %q:\n%s", step, out)
}

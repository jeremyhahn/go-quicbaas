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

//go:build swtpm

// seal_autodetect_swtpm_test.go is the empirical hardware proof for the P4 seal
// auto-detect path. It connects to a swtpm software-TPM emulator over TCP and
// drives the PRODUCTION code path end-to-end:
//
//	seal.strategy: auto  ->  buildHardwareSealStrategies  ->  buildTPM2AutoSealStrategy
//	(SINGLE non-provisioning TPM open)  ->  tpm2 strategy  ->  SealRootKey /
//	UnsealRootKey round-trip against the TPM SRK hierarchy.
//
// It deliberately does NOT boot a daemon — it exercises the same composer
// functions the daemon calls, isolating the auto-detect + seal wiring.
//
// A swtpm command channel is single-session (empirically the emulator HANGS on a
// second open within a process), so the test performs EXACTLY ONE TPM open: the
// one buildHardwareSealStrategies makes. The swtpm must already be provisioned
// (SRK present) — the integration harness provisions it out-of-band, mirroring
// test/integration/seal/tpm2_test.go. When the TPM is not reachable or not
// provisioned, auto-detect correctly declines TPM and the test SKIPs (the policy
// itself is covered by the hardware-free unit tests).
//
// Gating: compiled only with `-tags swtpm` (plus `codec_json`, required by the
// go-xkms TPM blob store). Address from SWTPM_HOST / SWTPM_PORT (default
// 127.0.0.1:2321).
//
// Run:
//
//	go test -tags 'swtpm codec_json' -run TestSealAutoDetect_Swtpm ./pkg/server/
package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// swtpmAddr resolves the swtpm command-channel address from the environment,
// defaulting to the devcontainer's 127.0.0.1:2321.
func swtpmAddr() (host string, port int) {
	host = os.Getenv("SWTPM_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port = 2321
	if p := os.Getenv("SWTPM_PORT"); p != "" {
		var parsed int
		if _, err := fmt.Sscanf(p, "%d", &parsed); err == nil && parsed > 0 {
			port = parsed
		}
	}
	return host, port
}

// skipUnlessSwtpm skips the test unless the swtpm command channel accepts a TCP
// connection, mirroring the integration suite's reachability guard.
func skipUnlessSwtpm(t *testing.T, host string, port int) {
	t.Helper()
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("swtpm not reachable at %s (%v); skipping hardware seal test", addr, err)
	}
	_ = conn.Close()
}

// TestSealAutoDetect_Swtpm is the end-to-end empirical proof that the P4
// auto-detect path selects TPM2 from a single non-provisioning open and that the
// resulting production seal strategy seals and unseals the barrier root key
// against a real (software) TPM.
func TestSealAutoDetect_Swtpm(t *testing.T) {
	host, port := swtpmAddr()
	t.Logf("swtpm address: %s:%d", host, port)
	skipUnlessSwtpm(t, host, port)

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(config.BackendAuto) // OPT-IN: auto-detect
	cfg.Seal.TPM2.UseSimulator = true
	cfg.Seal.TPM2.SimulatorHost = host
	cfg.Seal.TPM2.SimulatorPort = port
	cfg.Seal.TPM2.KeyDir = t.TempDir()

	// SINGLE TPM open: the production data-plane dispatch under "auto".
	strategies, err := buildHardwareSealStrategies(cfg)
	if err != nil {
		t.Fatalf("buildHardwareSealStrategies (auto): %v", err)
	}
	if strategies == nil {
		t.Skip("swtpm reachable but not provisioned/seal-capable; auto-detect correctly declined TPM " +
			"(provision the swtpm out-of-band to exercise the seal round-trip)")
	}
	if len(strategies) != 1 || strategies[0].ID() != xkmssdk.StrategyTPM2 {
		t.Fatalf("want exactly one tpm2 strategy, got %d (%v)", len(strategies), strategies)
	}
	st := strategies[0]
	defer func() { _ = st.Close() }()
	if !st.HardwareBacked() {
		t.Fatal("auto-selected tpm2 strategy reports HardwareBacked()=false")
	}
	if !st.Available() {
		t.Fatal("auto-selected tpm2 strategy reports Available()=false over a seal-capable backend")
	}

	// Full root-key seal -> unseal round-trip: the KEK wrap (root key sealed under
	// the TPM SRK hierarchy) must recover the key byte-for-byte.
	ctx := context.Background()
	rootKey := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := st.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey against swtpm FAILED: %v", err)
	}
	recovered, err := st.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey against swtpm FAILED: %v", err)
	}
	if string(recovered) != string(rootKey) {
		t.Fatalf("root key mismatch after TPM seal round-trip: got %x want %x", recovered, rootKey)
	}
	t.Log("auto-detect -> tpm2 single-open seal/unseal round-trip succeeded against swtpm")
}

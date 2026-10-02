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

//go:build integration && tpm2

// tpm2_test.go exercises the TPM 2.0 barrier sealing strategy end-to-end
// against a swtpm software TPM emulator. It is compiled only when both the
// `integration` and `tpm2` build tags are present.
//
// The swtpm container exposes the TPM command channel on
// SWTPM_HOST:SWTPM_PORT (defaults: swtpm1:2321). The test boots a daemon
// whose seal.tpm2.use_simulator=true, seal.tpm2.simulator_host, and
// seal.tpm2.simulator_port point at that container, and drives the
// init -> seal -> unseal round-trip.
//
// Unlike cloud strategies (awskms, vault), the TPM2 strategy seals root key
// material under the TPM's storage hierarchy — no wrapping key ID or token
// pre-provisioning is required; the backend manages the SRK hierarchy
// internally. The swtpm image is pre-initialized with --flags startup-clear
// so it is ready immediately after the TCP port opens.
package seal

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	pkgtpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"
	tpmstore "github.com/jeremyhahn/go-xkms/pkg/tpm2/store"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
)

// swtpmHostEnv / swtpmPortEnv name the env vars carrying the swtpm address.
// The hardware-seal compose sets SWTPM_HOST=swtpm1, SWTPM_PORT=2321.
const (
	swtpmHostEnv = "SWTPM_HOST"
	swtpmPortEnv = "SWTPM_PORT"
)

// resolveSwtpm returns (host, port) for the swtpm emulator, falling back to
// the compose defaults when the env is partially set.
func resolveSwtpm() (host string, port int) {
	host = os.Getenv(swtpmHostEnv)
	if host == "" {
		host = "swtpm1"
	}
	portStr := os.Getenv(swtpmPortEnv)
	if portStr == "" {
		return host, 2321
	}
	var p int
	if _, err := fmt.Sscanf(portStr, "%d", &p); err != nil || p <= 0 {
		return host, 2321
	}
	return host, p
}

// waitForSwtpm polls the swtpm TCP command port until it accepts a connection
// or the deadline fires. Channel-driven readiness (no time.Sleep) per CLAUDE.md.
func waitForSwtpm(t *testing.T, host string, port int) {
	t.Helper()

	addr := fmt.Sprintf("%s:%d", host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("swtpm at %s never became reachable: %v", addr, ctx.Err())
		case <-ticker.C:
			dialer := &net.Dialer{Timeout: 2 * time.Second}
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				continue
			}
			_ = conn.Close()
			return
		}
	}
}

// provisionSwtpm provisions the swtpm at host:port so the daemon's
// buildTPM2Strategy succeeds immediately without a first-boot provisioning
// step. Provisioning is idempotent: if the swtpm is already provisioned
// (state persisted from a previous run), NewTPM2 returns without error and
// this function returns immediately.
//
// The provisioning uses go-xkms's canonical recipe (pkgtpm2.NewTPM2 +
// Provision) with minimal key configuration that matches the defaults applied
// by provisionTPM2Backend in pkg/server/seal_factories.go.
func provisionSwtpm(t *testing.T, host string, port int) {
	t.Helper()

	keyDir := t.TempDir()
	sf, err := tpmstore.NewStorageFactory(nil, keyDir)
	if err != nil {
		t.Fatalf("swtpm provision: NewStorageFactory: %v", err)
	}

	ekAuth, err := xkmstypes.NewLiteralSecretValue([]byte(tpmstore.DEFAULT_PASSWORD))
	if err != nil {
		t.Fatalf("swtpm provision: seal EK hierarchy auth: %v", err)
	}
	srkAuth, err := xkmstypes.NewLiteralSecretValue([]byte(tpmstore.DEFAULT_PASSWORD))
	if err != nil {
		t.Fatalf("swtpm provision: seal SRK hierarchy auth: %v", err)
	}
	cfg := &pkgtpm2.Config{
		CommandAddress:               fmt.Sprintf("%s:%d", host, port),
		PlatformAddress:              fmt.Sprintf("%s:%d", host, port+1),
		Hash:                         "SHA-256",
		PlatformPCRBank:              pkgtpm2.PCRBankSHA256,
		IdentityProvisioningStrategy: string(pkgtpm2.EnrollmentStrategyIAK),
		EK: &pkgtpm2.EKConfig{
			Handle:        0x81010001,
			CertHandle:    0x01C00002,
			HierarchyAuth: ekAuth,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &tpmstore.RSAConfig{KeySize: 2048},
		},
		SSRK: &pkgtpm2.SRKConfig{
			Handle:        0x81000001,
			HierarchyAuth: srkAuth,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &tpmstore.RSAConfig{KeySize: 2048},
		},
		IAK: &pkgtpm2.IAKConfig{
			CN:                 "quicbaas-iak",
			Hash:               "SHA-256",
			Handle:             0x81010002,
			KeyAlgorithm:       x509.RSA.String(),
			RSAConfig:          &tpmstore.RSAConfig{KeySize: 2048},
			SignatureAlgorithm: x509.SHA256WithRSAPSS.String(),
		},
	}
	params := &pkgtpm2.Params{
		Config:    cfg,
		BlobStore: sf.BlobStore(),
		Backend:   sf.KeyBackend(),
	}

	provCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tpm, err := pkgtpm2.NewTPM2(params)
	if err != nil {
		if !errors.Is(err, pkgtpm2.ErrNotInitialized) || tpm == nil {
			t.Fatalf("swtpm provision: NewTPM2: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- tpm.Provision(nil) }()
		select {
		case provErr := <-done:
			if provErr != nil {
				_ = tpm.Close()
				t.Fatalf("swtpm provision: Provision: %v", provErr)
			}
			t.Logf("swtpm provisioned successfully")
		case <-provCtx.Done():
			_ = tpm.Close()
			t.Fatalf("swtpm provision: timed out: %v", provCtx.Err())
		}
	} else {
		t.Logf("swtpm already provisioned (state persisted)")
	}
	_ = tpm.Close()
}

// tpm2ConfigYAML renders the daemon seal config for the tpm2 strategy. The
// use_simulator flag tells the go-xkms tpm2 backend to connect to an external
// software TPM over TCP (simulator_host:simulator_port) rather than opening a
// hardware /dev/tpmrm0 device.
func tpm2ConfigYAML(host string, port int) string {
	return fmt.Sprintf(`seal:
  enabled: true
  strategy: tpm2
  tpm2:
    use_simulator: true
    simulator_host: %q
    simulator_port: %d
    key_dir: /tmp/tpm2-seal-keys
`, host, port)
}

// TestTPM2SealRoundTrip boots a daemon configured with the tpm2 strategy
// pointed at a swtpm emulator and asserts the init -> seal -> unseal
// round-trip recovers the barrier root key.
//
// The test pre-provisions the swtpm before starting the daemon so that
// the daemon's buildTPM2Strategy finds an already-provisioned TPM and can
// construct the backend without a first-boot provisioning step. This avoids
// a startup-time timeout on fresh swtpm instances (provisioning with IAK key
// generation can take tens of seconds on resource-constrained emulators).
func TestTPM2SealRoundTrip(t *testing.T) {
	host, port := resolveSwtpm()
	t.Logf("swtpm address: %s:%d", host, port)

	waitForSwtpm(t, host, port)
	t.Logf("swtpm is reachable at %s:%d", host, port)

	provisionSwtpm(t, host, port)

	cfgYAML := tpm2ConfigYAML(host, port)
	runCloudSealRoundTrip(t, "tpm2", cfgYAML)
}

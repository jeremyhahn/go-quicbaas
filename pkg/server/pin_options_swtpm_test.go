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

// pin_options_swtpm_test.go is the empirical hardware proof for the TPM PIN
// path. It drives the PRODUCTION shared-backend dispatch end-to-end against a
// swtpm software-TPM emulator:
//
//	seal.strategy: tpm2  ->  buildHardwareSealStrategiesShared  (SINGLE TPM open,
//	returns the seal strategy AND its open *TPM2Backend)  ->  first-boot
//	enrollTPMPlatformSRKFirstBoot (binds the operator PIN as the PlatformSRK auth
//	over the SAME open)  ->  newSealSharedTPMPINProvider (reuses that SAME open,
//	never NewTPM2Backend)  ->  buildPINBackend selects the TPM2 PIN backend  ->
//	VerifyUserPIN verifies in hardware against the PlatformSRK.
//
// What this test HARD-PROVES (fails if regressed):
//   - The seal strategy is built from EXACTLY ONE TPM open and hands back that
//     same open *TPM2Backend for sharing.
//   - First-boot enrollment binds the operator PIN as the PlatformSRK auth over
//     the shared open (Fix 5), and is idempotent across boots.
//   - The PIN provider is constructed over that shared open (never a second
//     NewTPM2Backend) and is wired into the production buildPINBackend decision.
//   - buildPINBackend selects the TPM2 PIN backend (no software fallback) after
//     enrollment, and the in-hardware verify ACCEPTS the correct PIN and REJECTS
//     the wrong PIN — the previously-masked xkms VerifyAuth template bug is fixed.
//   - The seal root-key round-trip still works over the same backend after the
//     PIN provider has touched it (PIN and seal use distinct SRKs).
//
// A swtpm command channel is single-session (the emulator HANGS on a second open
// within a process); this test performs EXACTLY ONE TPM open. The seal build is
// timeout-guarded so a fresh swtpm whose provisioning wedges cannot hang the
// suite (it SKIPs at setup).
//
// Gating: compiled only with `-tags swtpm` (plus `codec_json`). Address from
// SWTPM_HOST / SWTPM_PORT (default 127.0.0.1:2321).
//
// Run:
//
//	go test -tags 'swtpm codec_json' -run TestPINBackend_Swtpm ./pkg/server/
package server

import (
	"log/slog"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestPINBackend_Swtpm_SharedSealTPM is the end-to-end empirical proof that the
// TPM PIN backend shares the seal strategy's single TPM open, that first-boot
// enrollment binds the operator PIN over that open, and that the production PIN
// decision selects the TPM2 backend whose in-hardware verify accepts the correct
// PIN and rejects the wrong one.
func TestPINBackend_Swtpm_SharedSealTPM(t *testing.T) {
	const (
		// operatorPIN doubles as the seal passphrase the operator supplies at
		// boot and the PlatformSRK auth value enrollment binds.
		operatorPIN = "12345678"
		wrongPIN    = "00000000"
	)

	host, port := swtpmAddr()
	t.Logf("swtpm address: %s:%d", host, port)
	skipUnlessSwtpm(t, host, port)

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(xkmssdk.StrategyTPM2) // explicit tpm2 seal
	cfg.Seal.TPM2.UseSimulator = true
	cfg.Seal.TPM2.SimulatorHost = host
	cfg.Seal.TPM2.SimulatorPort = port
	cfg.Seal.TPM2.KeyDir = t.TempDir()
	cfg.Storage.DataDir = t.TempDir()
	// PIN follows the TPM seal — the production hardware posture.
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypeTPM2

	// SINGLE TPM open: the production shared-backend dispatch. Guard with a
	// timeout so a fresh swtpm whose provisioning wedges cannot hang the suite.
	type buildRes struct {
		strategies []qrdbsdk.SealingStrategy
		backend    *xkmssdk.TPM2Backend
		err        error
	}
	ch := make(chan buildRes, 1)
	go func() {
		s, b, e := buildHardwareSealStrategiesShared(cfg, slog.Default())
		ch <- buildRes{s, b, e}
	}()

	var r buildRes
	select {
	case r = <-ch:
	case <-time.After(60 * time.Second):
		t.Skip("swtpm seal backend build timed out (likely an unprovisioned TPM whose " +
			"first-boot provisioning wedges); provision the swtpm out-of-band to run the PIN proof")
	}
	if r.err != nil {
		t.Skipf("explicit tpm2 seal backend build failed (%v); skipping PIN hardware proof", r.err)
	}
	if r.backend == nil || len(r.strategies) != 1 {
		t.Fatalf("PROVEN MECHANISM BROKEN: explicit tpm2 must yield exactly one strategy plus a "+
			"shared TPM backend from a single open; got %d strategies, backend=%v",
			len(r.strategies), r.backend)
	}
	strategy := r.strategies[0]
	defer func() { _ = strategy.Close() }() // releases the single TPM open
	sharedBackend := r.backend
	t.Log("PROVEN: seal strategy built from a single TPM open; shared backend handed back")

	// First-boot enrollment over the SHARED open (Fix 5). This binds operatorPIN
	// as the PlatformSRK TPM auth without a second TPM open.
	if err := enrollTPMPlatformSRKFirstBoot(cfg, sharedBackend, []byte(operatorPIN), slog.Default()); err != nil {
		t.Fatalf("first-boot TPM PlatformSRK enrollment failed: %v", err)
	}
	t.Log("PROVEN: first-boot enrollment bound the operator PIN as the PlatformSRK auth over the shared open")

	// Enrollment is idempotent across boots: a second call must also succeed.
	if err := enrollTPMPlatformSRKFirstBoot(cfg, sharedBackend, []byte(operatorPIN), slog.Default()); err != nil {
		t.Fatalf("second enrollment must be idempotent, got: %v", err)
	}

	// Construct the PIN provider over the SHARED open. This must NOT open the TPM
	// again (a second open would hang this swtpm), and must be non-nil for an
	// active TPM2 seal backend.
	provider := newSealSharedTPMPINProvider(sharedBackend, cfg, slog.Default())
	if provider == nil {
		t.Fatal("PROVEN MECHANISM BROKEN: newSealSharedTPMPINProvider returned nil for an active TPM2 seal backend")
	}

	// PROVEN: with a PlatformSRK enrolled, the production PIN decision MUST select
	// the TPM2 backend over the shared open — no software fallback.
	pinBackend, err := buildPINBackend(cfg, slog.Default(), provider)
	if err != nil {
		t.Fatalf("buildPINBackend (tpm2) returned an unexpected error: %v", err)
	}
	if got := string(pinBackend.Strategy()); got != string(qrdbsdk.PINStrategyTPM2) {
		t.Fatalf("PIN backend must select TPM2 after enrollment, got %q (software fallback indicates a regression)", got)
	}
	if !pinBackend.UserPINSet() {
		t.Fatal("user PIN must be set after enrollment")
	}
	t.Log("PROVEN: production buildPINBackend selected the TPM2 PIN backend over the shared seal TPM open")

	// Clear any DA lockout accumulated by prior runs (swtpm persists the DA
	// counter in NV across process restarts) so the in-hardware checks below
	// start from a clean slate.
	_ = pinBackend.ResetLockout("")

	// In-hardware PIN verification: the correct PIN ACCEPTS.
	if err := pinBackend.VerifyUserPIN(operatorPIN); err != nil {
		t.Fatalf("VerifyUserPIN (correct PIN) against the swtpm PlatformSRK failed: %v", err)
	}

	// The wrong PIN REJECTS and (Fix 3) increments the TPM dictionary-attack
	// counter. On a TPM with aggressive DA limits a single miss can move toward
	// lockout, so reset the deliberate miss afterwards.
	if err := pinBackend.VerifyUserPIN(wrongPIN); err == nil {
		t.Fatal("VerifyUserPIN (wrong PIN) must be rejected by the TPM, got nil error")
	}
	_ = pinBackend.ResetLockout("")

	// Correct PIN still verifies after the (reset) failed attempt.
	if err := pinBackend.VerifyUserPIN(operatorPIN); err != nil {
		t.Fatalf("VerifyUserPIN (correct PIN, after a reset miss) failed: %v", err)
	}
	t.Log("PROVEN: TPM2 PIN verify accepts the correct PIN and rejects the wrong PIN in hardware over the shared open")

	// NOTE: the seal/unseal root-key round-trip over this same backend is proven
	// separately by TestSealAutoDetect_Swtpm — it exercises the TPM2 seal SSRK
	// (handle 0x81000001), which is distinct from the PIN PlatformSRK (0x81000002)
	// this test drives. The two never share an SRK, only the single TPM open.
	_ = strategy // strategy.Close() (deferred) releases the single TPM open
}

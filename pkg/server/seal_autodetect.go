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

package server

import (
	"log/slog"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file implements the P4 seal-strategy auto-detect policy: when the
// operator opts in with `seal.strategy: auto`, prefer a TPM 2.0 seal strategy IF
// — and only if — a TPM that is already provisioned and seal-capable is present.
// Otherwise fall back to the software passphrase strategy.
//
// The policy is deliberately conservative, mirroring the P1 CA-path lesson that
// a blind auto -> hardware default broke boot on a TPM host:
//
//   - It is OPT-IN. The empty string and the "passphrase" default are returned
//     unchanged; only the explicit "auto" sentinel triggers detection. Existing
//     deployments and the default posture are byte-for-byte unaffected, and the
//     unit-test path never touches a TPM.
//   - Auto-detect NEVER provisions. Empirically (swtpm 0.7.3), boot-time TPM
//     provisioning is heavy and unreliable: a fresh device's provisioning hangs
//     for >120s and fails with TPM_RC_RETRY. Auto-detect therefore selects tpm2
//     only when the TPM backend opens quickly AND reports CanSeal() — i.e. the
//     SRK hierarchy is already present. An unprovisioned, inaccessible, or
//     slow/wedged TPM falls back to passphrase cleanly. An operator who wants
//     boot-time provisioning must name "tpm2" explicitly, accepting the cost.
//   - The TPM is opened EXACTLY ONCE. The single open's seal-capable backend is
//     retained inside the returned TPM2Strategy and reused for the barrier's
//     whole life — there is no probe-then-reopen. This matters because a TPM
//     command channel is a single session: empirically a swtpm emulator HANGS on
//     the second open within a process (the kernel /dev/tpmrm0 resource manager
//     multiplexes, but never relying on that keeps the path emulator-safe too).

// sealAutoProbeTimeout bounds the single auto-detect TPM open so a slow or
// wedged TPM can never stall daemon boot. It only opens and checks seal
// capability; it never provisions.
const sealAutoProbeTimeout = 3 * time.Second

// tpm2AutoSealStrategy is the seam buildHardwareSealStrategies uses to obtain a
// TPM2 seal strategy under the "auto" policy. It is a package var so unit tests
// can stub it deterministically without touching hardware; the production value
// is buildTPM2AutoSealStrategy, exercised end-to-end by the swtpm hardware test.
//
// It returns (nil, nil) when no provisioned, seal-capable TPM is present. On
// success it returns BOTH the strategy AND the open TPM2 backend the strategy
// wraps, so the PIN backend (P3) can share that single TPM open rather than
// opening the device a second time (which a single-session swtpm emulator would
// hang on). The returned backend is the SAME instance held inside the strategy;
// it must not be closed independently — closing the strategy releases it.
var tpm2AutoSealStrategy = buildTPM2AutoSealStrategy

// resolveEffectiveSealStrategy maps the configured seal strategy to a concrete
// software-side StrategyID. It is a pure string normalizer: "auto", the empty
// string, and the "passphrase" default all map to passphrase; every explicit
// value (none/shamir/tpm2/cloud) is returned unchanged.
//
// Auto-detect's HARDWARE decision deliberately does NOT live here — it lives in
// buildHardwareSealStrategies, which opens the TPM exactly once. Mapping "auto"
// to passphrase here gives the correct software FALLBACK for the data-plane:
// when no usable TPM is present, buildHardwareSealStrategies returns nil and the
// caller builds the passphrase strategy this returns. The logger parameter is
// retained for signature symmetry with the rest of the seal wiring.
func resolveEffectiveSealStrategy(cfg *config.Config, _ *slog.Logger) qrdbsdk.StrategyID {
	if cfg == nil {
		return qrdbsdk.StrategyPassphrase
	}
	raw := cfg.Seal.Strategy
	if raw == "" || raw == string(config.BackendAuto) {
		return qrdbsdk.StrategyPassphrase
	}
	return qrdbsdk.StrategyID(raw)
}

// buildTPM2AutoSealStrategy delegates to the canonical go-xkms SDK autodetect
// (xkmssdk.BuildBestSealStrategy) — the single stack-wide implementation of the
// non-provisioning, time-bounded, single-open TPM probe — and adapts its result
// to this call site: a seal-capable TPM2Strategy plus the concrete open backend
// so the PIN path can share the one open (never reopened; a single-session
// swtpm emulator hangs on a second open). Returns (nil, nil) when no
// provisioned, seal-capable TPM is present, and the caller falls back to
// software. All the probe hardening (timeout, drain-on-timeout,
// unprovisioned-device handling) now lives in the SDK helper.
func buildTPM2AutoSealStrategy(cfg *config.Config, logger *slog.Logger) (qrdbsdk.SealingStrategy, *xkmssdk.TPM2Backend) {
	if cfg == nil {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	tpmCfg := cfg.Seal.TPM2
	res := xkmssdk.BuildBestSealStrategy(xkmssdk.SealAutodetectOptions{
		TPM2: &xkmssdk.TPM2ProbeConfig{
			Device:          tpmCfg.Device,
			KeyDir:          tpmCfg.KeyDir,
			UseSimulator:    tpmCfg.UseSimulator,
			SimulatorHost:   tpmCfg.SimulatorHost,
			SimulatorPort:   tpmCfg.SimulatorPort,
			SRKHandle:       tpmCfg.SRKHandle,
			PlatformPCR:     tpmCfg.PCR,
			PlatformPCRBank: tpmCfg.PCRBank,
		},
		ProbeTimeout: sealAutoProbeTimeout,
		Logger:       logger,
	})
	if !res.HardwareAvailable() {
		return nil, nil
	}
	// res.Sealer is the single open TPM backend the strategy wraps; recover the
	// concrete type so the PIN path shares that one open.
	backend, _ := res.Sealer.(*xkmssdk.TPM2Backend)
	return res.Strategy, backend
}

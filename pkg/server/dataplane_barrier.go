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
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// strategyAwareBarrierService enriches the qrdb BarrierService status with the
// CONFIGURED sealing strategy when the underlying barrier reports none.
//
// qrdb's StorageBarrier.Status reports the ACTIVE strategy, which is only
// populated once the barrier is initialised/unsealed (activeStrategy is set by
// the init/unseal path's bestStrategy selection). A sealed, not-yet-initialised
// barrier therefore reports an empty strategy. Operators — and the canonical
// `barrier status` CLI, mirroring Vault's "Seal Type" being shown on a sealed
// vault — expect the status to always name the strategy that protects (or will
// protect) the root key. This decorator fills that gap in the quicbaas
// composition layer WITHOUT modifying qrdb: when the active strategy is empty it
// substitutes the configured strategy resolved at construction. Once the barrier
// is initialised, qrdb's active strategy is authoritative and is passed through
// unchanged.
type strategyAwareBarrierService struct {
	BaasBarrierService
	configuredStrategy string
}

// BarrierStatus delegates to the wrapped service and substitutes the configured
// strategy when the barrier has not yet activated one (sealed/uninitialised).
func (s *strategyAwareBarrierService) BarrierStatus(ctx context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	info, err := s.BaasBarrierService.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	if info != nil && info.Strategy == "" {
		info.Strategy = s.configuredStrategy
	}
	return info, nil
}

// DataPlaneBarrierError describes a failure constructing the data-plane
// StorageBarrier. Stage names the construction step that failed; Cause is
// the underlying error.
type DataPlaneBarrierError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *DataPlaneBarrierError) Error() string {
	if e.Cause == nil {
		return "data-plane barrier: " + e.Stage + " failed"
	}
	return "data-plane barrier: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *DataPlaneBarrierError) Unwrap() error { return e.Cause }

// buildDataPlaneBarrierService constructs the canonical data-plane
// StorageBarrier for the embedded quicbaasd server.
//
// The barrier is the qrdb StorageBarrier (go-qrdb/pkg/seal) composed
// according to the locked architecture (docs/05-seal-barrier-architecture.md):
//
//   - Storage backend: PebbleDB at <dataDir>/seal/ — persistent across
//     daemon restarts.
//   - Strategy: resolved from cfg.Seal.Strategy ("none", "passphrase",
//     "shamir"). Hardware strategies (tpm2/pkcs11/cloud KMS) require the
//     corresponding go-xkms strategy to be injected via
//     XKMSCryptoOptions; support for those is wired in a follow-up
//     when hardware-backed deployments are configured.
//   - BarrierConfig.RootKeyPath: "root-key" — canonical location that
//     matches the qrdb embedded server's own setupBarrier path.
//
// The returned BaasBarrierService is the qrdbsdk.BarrierService wrapper
// around the concrete StorageBarrier, wired to a persistent-barrier ceremony
// service exactly the way qrdbd wires its own (NewBarrierServiceWithCeremony:
// the ceremony service is installed as the barrier's CeremonyDistributor and
// RecoveryDistributor, so the officer / share-claim / recovery verbs work
// instead of failing typed "ceremony service not configured"). The second
// return value is the raw *qrdbsdk.StorageBarrier so callers can build an
// atrest.Encryptor from it. The third is the ceremony core so composition
// stages can attach the platform CA (SetCA) and the init ceremony can drive
// officer enrollment. The returned io.Closer closes the underlying PebbleDB
// instance; the caller must call Close when done.
//
// The data-plane barrier is constructed whenever a persistent data directory
// is available, INDEPENDENT of cfg.Seal.Enabled. cfg.Seal.Enabled only governs
// whether the daemon auto-initializes/encrypts the data plane at startup; the
// barrier SERVICE that backs the `barrier` CLI verbs (init/seal/unseal/status/
// rekey/recovery) must always be the real qrdb StorageBarrier so operators can
// drive the barrier lifecycle regardless of the auto-init setting. Only when
// cfg.Storage.DataDir is empty (no persistent backend) is (nil, nil, nil, nil)
// returned, and the caller falls back to the in-memory bootstrap barrier.
//
// buildDataPlaneBarrierService is idempotent with respect to the PebbleDB
// state: it opens the existing seal sub-store on subsequent starts and lets
// the operator issue a BarrierUnseal call to restore the unsealed state.
func buildDataPlaneBarrierService(
	cfg *config.Config,
	logger *slog.Logger,
) (BaasBarrierService, *qrdbsdk.StorageBarrier, *qrdbsdk.CeremonyService, io.Closer, error) {
	if cfg == nil || cfg.Storage.DataDir == "" {
		return nil, nil, nil, nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	// ------------------------------------------------------------------ //
	// 1. Open the persistent PebbleDB seal sub-store.
	// ------------------------------------------------------------------ //
	sealDir := filepath.Join(cfg.Storage.DataDir, "seal")
	if err := os.MkdirAll(sealDir, 0o750); err != nil {
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "mkdir seal dir", Cause: err}
	}
	pebble, err := qrdbsdk.NewPebbleStorage(qrdbsdk.DefaultPebbleConfig(sealDir))
	if err != nil {
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "open pebble seal store", Cause: err}
	}

	// ------------------------------------------------------------------ //
	// 2. Wrap the PebbleStorage as a seal.StorageBackend via the qrdb
	//    StorageAdapter (LegacyStorage → StorageBackend bridge).
	// ------------------------------------------------------------------ //
	backend := qrdbsdk.NewStorageAdapter(pebble)

	// ------------------------------------------------------------------ //
	// 3. Resolve the sealing strategy from the config.
	//
	//    Software and None are pure qrdb strategies (no xkms dependency).
	//    Hardware strategies (tpm2/pkcs11/cloud KMS) require go-xkms
	//    injection; they are registered via XKMSCryptoOptions and
	//    will be added here in the hardware-strategy wiring follow-up.
	// ------------------------------------------------------------------ //
	// Resolve the effective strategy honoring the precedence: explicit
	// operator override (cfg.Seal.StrategyExplicit) > persisted init marker in
	// the data dir > config default. This is what makes `quicbaasd init
	// --barrier-strategy none` followed by `quicbaasd start` come up with the
	// none strategy instead of the "passphrase" config default. For shamir the
	// persisted threshold/total are returned too so the barrier registers the
	// same M-of-N the init ceremony recorded.
	strategy, threshold, totalShares, _, err := resolveBarrierStrategy(cfg, cfg.Seal.StrategyExplicit)
	if err != nil {
		_ = pebble.Close()
		return nil, nil, nil, nil, err
	}

	// Hardware/cloud strategies (tpm2/pkcs11/awskms/gcpkms/azurekv/vault) are
	// supplied by go-xkms via the hardware factory registry
	// (seal_factories.go). Software/none/shamir fall through to the pure-qrdb
	// strategy builder below. buildHardwareSealStrategies returns (nil, nil)
	// for the non-hardware IDs, which is the signal to use buildSealStrategies.
	strategies, err := buildHardwareSealStrategies(cfg)
	if err != nil {
		_ = pebble.Close()
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "build hardware strategies", Cause: err}
	}
	if strategies == nil {
		strategies, err = buildSealStrategies(strategy, threshold, totalShares)
		if err != nil {
			_ = pebble.Close()
			return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "build strategies", Cause: err}
		}
	}

	// ------------------------------------------------------------------ //
	// 4. Construct the StorageBarrier.
	// ------------------------------------------------------------------ //
	barrierCfg := qrdbsdk.BarrierConfig{
		RootKeyPath: "root-key",
	}
	if strategy == qrdbsdk.StrategyShamir {
		barrierCfg.Shamir = &qrdbsdk.ShamirConfig{
			Threshold:   threshold,
			TotalShares: totalShares,
		}
	}
	// Explicit strategy selection (no implicit preference-order default): the
	// preference order is the strategy chain order (primary first).
	for _, s := range strategies {
		barrierCfg.PreferenceOrder = append(barrierCfg.PreferenceOrder, s.ID())
	}

	// The actually-built primary strategy is authoritative for status/logging.
	// Under `seal.strategy: auto` this surfaces the auto-selected tpm2 (or the
	// passphrase fallback) rather than the literal "auto" sentinel.
	effectiveStrategy := strategy
	if len(strategies) > 0 {
		effectiveStrategy = strategies[0].ID()
	}

	barrier, err := qrdbsdk.NewStorageBarrier(logger, backend, barrierCfg, strategies...)
	if err != nil {
		_ = pebble.Close()
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "construct storage barrier", Cause: err}
	}

	logger.Info("data-plane StorageBarrier constructed",
		slog.String("strategy", string(effectiveStrategy)),
		slog.String("seal_dir", sealDir),
	)

	// Wire the ceremony service over the SAME persistent barrier, mirroring
	// qrdbd's buildSealServices: a barrier-sealed credential service feeds a
	// ceremony core whose DataDir is the seal sub-store, and
	// NewBarrierServiceWithCeremony installs the ceremony core as the
	// barrier's CeremonyDistributor and RecoveryDistributor. Without this the
	// officer / share / recovery verbs on the data-plane barrier fail with the
	// typed "ceremony service not configured" error.
	credCore, err := qrdbsdk.NewVersionedCredentialService(
		&qrdbsdk.CredentialConfig{Strategy: qrdbsdk.CredentialStrategyBarrier},
		barrier,
		logger,
	)
	if err != nil {
		_ = pebble.Close()
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "construct credential service", Cause: err}
	}
	ceremonyCore, err := qrdbsdk.NewCeremonyService(
		&qrdbsdk.CeremonyConfig{DataDir: sealDir},
		barrier,
		credCore,
		logger,
	)
	if err != nil {
		_ = pebble.Close()
		return nil, nil, nil, nil, &DataPlaneBarrierError{Stage: "construct ceremony service", Cause: err}
	}

	// Wrap the qrdb BarrierService so `barrier status` reports the configured
	// strategy even while the barrier is sealed/uninitialised (qrdb only reports
	// the ACTIVE strategy, which is empty until init/unseal). See
	// strategyAwareBarrierService.
	svc := &strategyAwareBarrierService{
		BaasBarrierService: qrdbsdk.NewBarrierServiceWithCeremony(barrier, ceremonyCore),
		configuredStrategy: string(effectiveStrategy),
	}
	return svc, barrier, ceremonyCore, pebble, nil
}

// buildSealStrategies instantiates the concrete SealingStrategy slice for
// the given strategy ID. For StrategyNone a single NoneStrategy is returned.
// For StrategyPassphrase a single DeferredSoftwareStrategy is returned
// (passphrase arrives via BarrierInitialize/BarrierUnseal at runtime). For
// StrategyShamir a ShamirStrategy is constructed; its shares are returned to
// the operator at initialize time and are never persisted.
//
// Hardware strategies (tpm2/pkcs11/awskms/gcpkms/azurekv/vault) are not
// instantiated here — they require go-xkms injection via
// XKMSCryptoOptions and are handled at server-composition time.
func buildSealStrategies(
	strategy qrdbsdk.StrategyID,
	threshold, totalShares int,
) ([]qrdbsdk.SealingStrategy, error) {
	switch strategy {
	case qrdbsdk.StrategyNone:
		return []qrdbsdk.SealingStrategy{qrdbsdk.NewNoneStrategy()}, nil

	case qrdbsdk.StrategyShamir:
		// ShamirStrategy splits the root key into shares that are returned
		// to the operator at initialize time and never persisted.
		shamirStrat, err := qrdbsdk.NewShamirStrategy(threshold, totalShares)
		if err != nil {
			return nil, &DataPlaneBarrierError{Stage: "construct shamir strategy", Cause: err}
		}
		return []qrdbsdk.SealingStrategy{shamirStrat}, nil

	default:
		// StrategyPassphrase and any unrecognised strategy fall through to
		// deferred software so quicbaasd can start without a passphrase and
		// accept one later via BarrierInitialize or BarrierUnseal.
		return []qrdbsdk.SealingStrategy{qrdbsdk.NewDeferredSoftwareStrategy()}, nil
	}
}

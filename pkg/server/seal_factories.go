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
	"crypto/x509"
	"errors"
	"os"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file is the registry and dispatch layer that wires go-xkms hardware-
// and cloud-backed sealing strategies into the quicbaas barrier composer.
//
// The build-tag-free TPM2 strategy is registered here directly (see init()).
// The five build-tagged strategies — awskms, gcpkms, azurekv, vault, pkcs11 —
// live in their own files (e.g. seal_factory_awskms.go) guarded by a build
// tag matching their strategy ID. Each such file registers its factory into
// hardwareStrategyFactories from its own init() via
// registerHardwareStrategyFactory. When the matching tag is not compiled, the
// factory is simply absent from the map and buildHardwareSealStrategies
// returns a StrategyNotCompiledError that tells the operator to rebuild with
// the appropriate -tags value.
//
// The crypto is never reimplemented here: each factory constructs a go-xkms
// backend from the operator config and wraps it with the corresponding
// go-xkms SealingStrategy (qrdbsdk.SealingStrategy alias).

// hardwareStrategyFactory constructs a single hardware/cloud SealingStrategy
// from the full server config. The factory is responsible for reading its own
// sub-config (e.g. cfg.Seal.TPM2) and building the go-xkms backend + strategy.
type hardwareStrategyFactory func(cfg *config.Config) (qrdbsdk.SealingStrategy, error)

// hardwareStrategyFactories maps a hardware/cloud strategy ID to its factory.
// It is populated at package-init time: the TPM2 factory is registered by this
// file's init(); each build-tagged backend registers its own factory from a
// guarded file's init(). Reads happen only after all init() functions have
// run, so the map needs no synchronization.
var hardwareStrategyFactories = map[qrdbsdk.StrategyID]hardwareStrategyFactory{}

// hardwareStrategyIDs is the set of strategy IDs that are hardware- or
// cloud-backed and therefore dispatched through hardwareStrategyFactories
// rather than buildSealStrategies. A strategy in this set with no registered
// factory yields a StrategyNotCompiledError.
var hardwareStrategyIDs = map[qrdbsdk.StrategyID]struct{}{
	xkmssdk.StrategyTPM2:    {},
	xkmssdk.StrategyPKCS11:  {},
	xkmssdk.StrategyAWSKMS:  {},
	xkmssdk.StrategyGCPKMS:  {},
	xkmssdk.StrategyAzureKV: {},
	xkmssdk.StrategyVault:   {},
}

// registerHardwareStrategyFactory installs factory under id. It is called from
// the init() of each strategy file (including this one for TPM2). Re-
// registering the same id panics: a duplicate registration is a programming
// error (two files claiming the same strategy), not a runtime condition.
func registerHardwareStrategyFactory(id qrdbsdk.StrategyID, factory hardwareStrategyFactory) {
	if factory == nil {
		panic("server: nil hardware strategy factory for " + string(id))
	}
	if _, exists := hardwareStrategyFactories[id]; exists {
		panic("server: duplicate hardware strategy factory for " + string(id))
	}
	hardwareStrategyFactories[id] = factory
}

// isHardwareStrategy reports whether id is dispatched through the hardware
// factory registry rather than the software/none/shamir path.
func isHardwareStrategy(id qrdbsdk.StrategyID) bool {
	_, ok := hardwareStrategyIDs[id]
	return ok
}

// StrategyBuildError reports a failure constructing a hardware/cloud sealing
// strategy from its backend config. Backend names the go-xkms backend that
// failed; Cause is the underlying error.
type StrategyBuildError struct {
	Backend string
	Cause   error
}

// Error implements the error interface.
func (e *StrategyBuildError) Error() string {
	if e.Cause == nil {
		return "seal strategy: building " + e.Backend + " backend failed"
	}
	return "seal strategy: building " + e.Backend + " backend failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *StrategyBuildError) Unwrap() error { return e.Cause }

// StrategyNotCompiledError reports that a hardware/cloud strategy was selected
// in the config but its factory is not registered — i.e. the binary was built
// without the build tag that compiles that backend in. The error message names
// the -tags value needed to enable it.
type StrategyNotCompiledError struct {
	Strategy qrdbsdk.StrategyID
}

// Error implements the error interface.
func (e *StrategyNotCompiledError) Error() string {
	return "seal strategy: " + string(e.Strategy) +
		" was selected but is not compiled into this binary; rebuild with -tags " +
		string(e.Strategy)
}

// buildHardwareSealStrategies resolves the hardware/cloud sealing strategy
// selected by cfg.Seal.Strategy.
//
//   - For software/none/shamir (and the empty default), it returns (nil, nil):
//     those are handled by buildSealStrategies and must not be dispatched here.
//   - For a hardware/cloud ID with a registered factory, it invokes the factory
//     and returns a single-element slice.
//   - For a hardware/cloud ID with NO registered factory (its build tag was not
//     compiled), it returns a StrategyNotCompiledError.
func buildHardwareSealStrategies(cfg *config.Config) ([]qrdbsdk.SealingStrategy, error) {
	if cfg == nil {
		return nil, nil
	}

	strategyStr := cfg.Seal.Strategy
	if strategyStr == "" {
		strategyStr = string(qrdbsdk.StrategyPassphrase)
	}
	id := qrdbsdk.StrategyID(strategyStr)

	if !isHardwareStrategy(id) {
		// software / none / shamir — not our concern.
		return nil, nil
	}

	factory, ok := hardwareStrategyFactories[id]
	if !ok {
		return nil, &StrategyNotCompiledError{Strategy: id}
	}

	strategy, err := factory(cfg)
	if err != nil {
		return nil, err
	}
	return []qrdbsdk.SealingStrategy{strategy}, nil
}

// buildTPM2Strategy constructs the TPM 2.0 sealing strategy from cfg.Seal.TPM2.
//
// It composes go-xkms: a tpm2backend.Backend (which implements types.Sealer) is
// built from the operator config and wrapped by xkmssdk.NewTPM2Strategy. No
// TPM crypto is reimplemented here — the backend handles SRK hierarchy, PCR
// binding, and seal/unseal.
//
// First-boot provisioning: when the TPM reports ErrNotInitialized (typical of
// a freshly-reset TPM, software TPM emulator, or an operator-reset device),
// this factory provisions it via provisionTPM2Backend and rebuilds the backend.
// Provisioning is idempotent: on a real platform the TPM is provisioned exactly
// once; subsequent starts find a pre-provisioned TPM and NewBackend succeeds
// without reaching the provisioning path. A genuinely unrecoverable TPM failure
// is wrapped in a StrategyBuildError.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then falls back to a software DEK derived from the TPM-protected
// root key (see TPM2Strategy.BarrierEncryptor), which keeps the root key
// hardware-bound while avoiding a second TPM round-trip per data operation.
func buildTPM2Strategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	tpmCfg := cfg.Seal.TPM2

	backendCfg := &xkmssdk.TPM2BackendConfig{
		Device:          tpmCfg.Device,
		KeyDir:          tpmCfg.KeyDir,
		UseSimulator:    tpmCfg.UseSimulator,
		SimulatorHost:   tpmCfg.SimulatorHost,
		SimulatorPort:   tpmCfg.SimulatorPort,
		SRKHandle:       tpmCfg.SRKHandle,
		PlatformPolicy:  tpmCfg.PlatformPolicy,
		PlatformPCR:     tpmCfg.PCR,
		PlatformPCRBank: tpmCfg.PCRBank,
	}

	backend, err := xkmssdk.NewTPM2Backend(backendCfg)
	if err == nil {
		return xkmssdk.NewTPM2Strategy(backend, nil), nil
	}

	// First-boot path: provision the TPM when it reports uninitialized and retry.
	if !errors.Is(err, xkmssdk.ErrTPM2BackendNotInitialized) {
		return nil, &StrategyBuildError{Backend: "tpm2", Cause: err}
	}

	backend, err = provisionTPM2Backend(backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "tpm2", Cause: err}
	}

	return xkmssdk.NewTPM2Strategy(backend, nil), nil
}

// provisionTPM2Backend provisions an uninitialized TPM (fresh device, emulator,
// or operator-reset hardware) and returns a ready-to-use Backend.
//
// It mirrors the provisioning recipe used by the go-xkms tpm2 test suite
// (pkg/tpm2 seal round-trip tests): pkgtpm2.NewTPM2 + Provision + NewBackendWithTPM.
// The resulting backend owns the TPM connection (externalTPM=false); the caller
// must not close the underlying TPM separately.
//
// provisionTPM2Backend is called at most once per daemon boot (subsequent starts
// find a provisioned TPM and NewBackend succeeds without reaching this path).
func provisionTPM2Backend(cfg *xkmssdk.TPM2BackendConfig) (*xkmssdk.TPM2Backend, error) {
	if cfg.KeyDir == "" {
		cfg.KeyDir = "./tpm2-keys"
	}
	if err := os.MkdirAll(cfg.KeyDir, 0o700); err != nil {
		return nil, err
	}

	storageFactory, err := xkmssdk.NewTPMStorageFactory(nil, cfg.KeyDir)
	if err != nil {
		return nil, err
	}

	tpmConfig := cfg.ToTPMConfig()
	if tpmConfig.Hash == "" {
		tpmConfig.Hash = "SHA-256"
	}
	if tpmConfig.PlatformPCRBank == "" {
		tpmConfig.PlatformPCRBank = xkmssdk.PCRBankSHA256
	}
	if tpmConfig.EK == nil {
		tpmConfig.EK = &xkmssdk.EKConfig{
			Handle:        0x81010001,
			CertHandle:    0x01C00002,
			HierarchyAuth: xkmssdk.TPM_DEFAULT_PASSWORD,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &xkmssdk.RSAConfig{KeySize: 2048},
		}
	}
	if tpmConfig.SSRK == nil {
		tpmConfig.SSRK = &xkmssdk.SRKConfig{
			Handle:        0x81000001,
			HierarchyAuth: xkmssdk.TPM_DEFAULT_PASSWORD,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &xkmssdk.RSAConfig{KeySize: 2048},
		}
	}
	// IAK config is required by pkgtpm2.Provision — a nil IAK causes a nil
	// pointer dereference inside IAKAttributesFromConfig. Provision creates an
	// Initial Attestation Key as part of platform initialisation; the go-xkms
	// provisioner expects the config to be fully populated. The identity
	// provisioning strategy IAK_IDEVID_SINGLE_PASS is the go-xkms default and
	// is suitable for software TPMs and basic hardware deployments.
	if tpmConfig.IAK == nil {
		tpmConfig.IAK = &xkmssdk.IAKConfig{
			CN:                 "quicbaas-iak",
			Hash:               "SHA-256",
			Handle:             0x81010002,
			KeyAlgorithm:       x509.RSA.String(),
			RSAConfig:          &xkmssdk.RSAConfig{KeySize: 2048},
			SignatureAlgorithm: x509.SHA256WithRSAPSS.String(),
		}
	}
	if tpmConfig.IdentityProvisioningStrategy == "" {
		tpmConfig.IdentityProvisioningStrategy = string(xkmssdk.TPMEnrollmentStrategyIAK)
	}

	params := &xkmssdk.TPMParams{
		Config:    tpmConfig,
		BlobStore: storageFactory.BlobStore(),
		Backend:   storageFactory.KeyBackend(),
	}

	tpm, newErr := xkmssdk.NewTPM2(params)
	if newErr != nil {
		if !errors.Is(newErr, xkmssdk.ErrTPMNotInitialized) || tpm == nil {
			return nil, newErr
		}
		if provErr := tpm.Provision(nil); provErr != nil {
			_ = tpm.Close()
			return nil, provErr
		}
	}

	backend, err := xkmssdk.NewTPM2BackendWithTPM(&xkmssdk.TPM2BackendExternalConfig{
		TPM:        tpm,
		KeyBackend: storageFactory.KeyBackend(),
	})
	if err != nil {
		_ = tpm.Close()
		return nil, err
	}
	return backend, nil
}

// init registers the build-tag-free TPM2 strategy factory. The build-tagged
// cloud/HSM factories register themselves from their own guarded files.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyTPM2, buildTPM2Strategy)
}

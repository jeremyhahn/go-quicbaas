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

// Package resolver re-exports go-xkms's canonical trust resolver so downstream
// callers (go-trusted-platform's pkg/trust, the quicbaasd trust: config section,
// the xkmsctl CLI when reached through the SDK) resolve outbound TLS trust
// through the SDK path rather than reaching into go-xkms directly.
//
// All symbols are pure aliases of `xkms/sdk/go/trust/resolver`. There is no
// transport, no client, no shimming — the package exists solely so the platform
// repo can write
//
//	import xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"
//
// and stay on the SDK-only contract documented in platform/CLAUDE.md
// §"SDK Consumption Invariant".
package resolver

import xkmsresolver "github.com/jeremyhahn/go-xkms/sdk/go/trust/resolver"

// =============================================================================
// Strategy type + constants
// =============================================================================

// Strategy identifies a single trust-establishment method within a scope's
// ordered chain.
type Strategy = xkmsresolver.Strategy

const (
	// StrategyDANE verifies the peer via DNSSEC-validated TLSA records per RFC 7671.
	StrategyDANE = xkmsresolver.StrategyDANE

	// StrategyNoise fetches the CA bundle over a Noise_NK channel authenticated
	// by the server's static public key.
	StrategyNoise = xkmsresolver.StrategyNoise

	// StrategySPKI verifies the peer leaf certificate against a pinned SHA-256
	// SubjectPublicKeyInfo digest (hex encoded).
	StrategySPKI = xkmsresolver.StrategySPKI

	// StrategyEmbedded trusts the operating system trust store. No network fetch
	// is performed.
	StrategyEmbedded = xkmsresolver.StrategyEmbedded

	// StrategyCAFile trusts a CA bundle PEM file on local disk.
	StrategyCAFile = xkmsresolver.StrategyCAFile

	// StrategyProviderCA trusts the CA bundle supplied by an injected
	// CABundleProvider (e.g. the platform's own certificate authority).
	StrategyProviderCA = xkmsresolver.StrategyProviderCA

	// StrategyTOFU uses trust-on-first-use SPKI pinning. On the first connection
	// the server's leaf certificate SPKI pin is captured and persisted via the
	// injected TOFUPinStore. Subsequent connections verify the stored pin exactly
	// like StrategySPKI. Requires WithTOFUPinStore to be passed at construction
	// time.
	StrategyTOFU = xkmsresolver.StrategyTOFU
)

// =============================================================================
// Config types
// =============================================================================

// Re-exported config types.
type (
	// Config is the top-level trust: configuration. Each scope is optional;
	// an unconfigured scope fails resolution with ErrScopeNotConfigured.
	Config = xkmsresolver.Config

	// ScopeConfig configures a single trust scope: the ordered strategy chain
	// plus per-strategy inputs.
	ScopeConfig = xkmsresolver.ScopeConfig

	// DANEConfig configures the DANE/TLSA resolver used by the dane strategy.
	DANEConfig = xkmsresolver.DANEConfig

	// NoiseConfig configures the Noise_NK client used by the noise strategy.
	NoiseConfig = xkmsresolver.NoiseConfig

	// SPKIConfig configures the SPKI pin used by the spki strategy.
	SPKIConfig = xkmsresolver.SPKIConfig

	// TOFUScopeConfig holds per-scope TOFU knobs. The PinStore itself is
	// injected at resolver construction time via WithTOFUPinStore.
	TOFUScopeConfig = xkmsresolver.TOFUScopeConfig
)

// =============================================================================
// Resolver interface + collaborator interfaces
// =============================================================================

// Re-exported interface types.
type (
	// Resolver resolves trust material for a (scope, endpoint) pair by walking
	// the scope's ordered strategy chain and returning the first success.
	Resolver = xkmsresolver.Resolver

	// PinStore supplies per-endpoint SPKI pins captured during enrollment. A
	// store with no pin for the endpoint returns *ErrPinNotFound so the resolver
	// can fall back to the scope's static pin.
	PinStore = xkmsresolver.PinStore

	// CABundleProvider supplies a CA bundle PEM for the provider-ca strategy.
	CABundleProvider = xkmsresolver.CABundleProvider

	// Option configures optional resolver collaborators.
	Option = xkmsresolver.Option

	// Attempt records the outcome of a single strategy attempt within a
	// resolution chain.
	Attempt = xkmsresolver.Attempt

	// TOFUPinStore is the truststrap PinStore used by the tofu strategy.
	// Use bootstrap.NewStoragePinStore to construct a storage-backed instance.
	TOFUPinStore = xkmsresolver.TOFUPinStore

	// TOFUPin is the truststrap Pin record used by the tofu strategy.
	TOFUPin = xkmsresolver.TOFUPin
)

// =============================================================================
// Typed errors
// =============================================================================

// Re-exported typed errors.
type (
	// ErrNilConfig indicates a nil *Config was passed to New.
	ErrNilConfig = xkmsresolver.ErrNilConfig

	// ErrScopeNotConfigured indicates the requested scope has no ScopeConfig.
	ErrScopeNotConfigured = xkmsresolver.ErrScopeNotConfigured

	// ErrInvalidConfig indicates a scope configuration field is missing or
	// malformed.
	ErrInvalidConfig = xkmsresolver.ErrInvalidConfig

	// ErrFetchFailed indicates a CA bundle fetch strategy failed.
	ErrFetchFailed = xkmsresolver.ErrFetchFailed

	// ErrCABundleEmpty indicates a fetched or provided CA bundle contained no
	// data.
	ErrCABundleEmpty = xkmsresolver.ErrCABundleEmpty

	// ErrCABundleParse indicates a CA bundle contained no parseable PEM
	// certificates.
	ErrCABundleParse = xkmsresolver.ErrCABundleParse

	// ErrWriteFailed indicates a filesystem write failed while installing a CA
	// bundle.
	ErrWriteFailed = xkmsresolver.ErrWriteFailed

	// ErrProviderCAUnavailable indicates the provider-ca strategy was requested
	// but no CABundleProvider was injected.
	ErrProviderCAUnavailable = xkmsresolver.ErrProviderCAUnavailable

	// ErrPinNotFound indicates no SPKI pin is available for the endpoint: the
	// PinStore has no entry and the scope carries no static pin.
	ErrPinNotFound = xkmsresolver.ErrPinNotFound

	// ErrCAFileRead indicates the ca-file strategy could not read its CA bundle
	// file.
	ErrCAFileRead = xkmsresolver.ErrCAFileRead

	// ErrEndpointInvalid indicates the caller-supplied endpoint (or the scope's
	// server-url fallback) could not be parsed into host and port.
	ErrEndpointInvalid = xkmsresolver.ErrEndpointInvalid

	// ErrTOFUPinStoreRequired indicates the tofu strategy was configured but no
	// TOFUPinStore was injected at construction time.
	ErrTOFUPinStoreRequired = xkmsresolver.ErrTOFUPinStoreRequired

	// ErrNoStrategySucceeded indicates every strategy in a scope's chain failed.
	// Attempts records each failure in chain order.
	ErrNoStrategySucceeded = xkmsresolver.ErrNoStrategySucceeded
)

// =============================================================================
// Constructor, defaults, and option functions
// =============================================================================

var (
	// New validates the configuration and constructs a Resolver.
	//
	// When the tofu strategy is listed in any scope and no WithTOFUPinStore option
	// is provided, New returns ErrTOFUPinStoreRequired.
	New = xkmsresolver.New

	// DefaultConfig returns a Config pre-configured with the standard fetch chain
	// (dane, noise, spki) and a 5-second per-method timeout for the "default"
	// scope. TOFU is intentionally excluded from the Go-level default; ship TOFU
	// only in explicit YAML templates where the operator can read the comment
	// explaining trust-on-first-use semantics.
	DefaultConfig = xkmsresolver.DefaultConfig

	// InstallToSystemTrust writes the PEM-encoded CA bundle to the given
	// certificate path, creating parent directories as needed. The caller is
	// responsible for running OS-specific trust store update commands afterward.
	InstallToSystemTrust = xkmsresolver.InstallToSystemTrust

	// WithLogger sets the slog logger used for strategy WARN/DEBUG messages.
	// When nil, slog.Default() is used.
	WithLogger = xkmsresolver.WithLogger

	// WithPinStore injects a per-endpoint SPKI pin source. Endpoint pins take
	// precedence over the scope's static pin.
	WithPinStore = xkmsresolver.WithPinStore

	// WithCABundleProvider injects the CA bundle source required by the
	// provider-ca strategy.
	WithCABundleProvider = xkmsresolver.WithCABundleProvider

	// WithTOFUPinStore injects the TOFU pin persistence store required by the
	// tofu strategy.
	WithTOFUPinStore = xkmsresolver.WithTOFUPinStore

	// WithTOFUConfirmFunc injects an optional confirmation hook called before a
	// newly captured TOFU pin is persisted. Returning an error aborts the
	// bootstrap and prevents pin storage.
	WithTOFUConfirmFunc = xkmsresolver.WithTOFUConfirmFunc
)

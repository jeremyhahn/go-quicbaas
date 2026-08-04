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
	"errors"
	"os"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file is the single central crypto backend resolver. It is the ONLY place
// in quicbaas that maps a configured backend type + key reference to a concrete
// go-xkms constructor. No subsystem may call xkmssdk.NewSoftwareBackend,
// xkmssdk.NewTPM2Backend, or any cloud/HSM backend constructor directly.
//
// It mirrors the build-tag init()-registration pattern used by
// seal_factories.go: the software, tpm2, and quantum factories are tag-free and
// register themselves from their own files' init(); the pkcs11/awskms/gcpkms/
// azurekv/vault factories live in //go:build-gated files and register only when
// their tag is compiled. A configured-but-not-compiled backend yields a typed
// BackendNotCompiledError — never a panic, never a silent software fallback.

// BackendSpec aliases config.BackendSpec so server code can write BackendSpec{}
// ergonomically while there remains exactly one definition (in pkg/config,
// which cannot import pkg/server). See config.BackendSpec for field docs.
type BackendSpec = config.BackendSpec

// serviceBackendFactory constructs a fully-wrapped xkmssdk.ServiceBackend from
// the full server config, a resolved spec, and a key store. The keyStore is the
// persistence seam: callers that need keys to survive restarts (startKMS) pass a
// KVStore/file-backed store; callers that do not (in-memory probes) pass an
// in-memory store. Each backend type registers one factory; the registry is
// populated entirely from init() functions so reads after package init need no
// synchronization. Backends whose keys live in hardware (TPM2) or off-host
// (cloud) may ignore keyStore for the key material itself, but accept it
// uniformly so the dispatch stays a single signature.
type serviceBackendFactory func(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error)

// backendFactories maps a concrete BackendType to its factory. Tag-free
// backends (software/tpm2/quantum) register here unconditionally; build-tagged
// backends register only when their tag is compiled. A missing entry for a
// configured type is what produces BackendNotCompiledError.
var backendFactories = map[config.BackendType]serviceBackendFactory{}

// registerBackendFactory installs f under bt. Called from each backend file's
// init(). A nil factory or a duplicate registration is a programming error
// (two files claiming the same type) and panics at init time.
func registerBackendFactory(bt config.BackendType, f serviceBackendFactory) {
	if f == nil {
		panic("server: nil service backend factory for " + string(bt))
	}
	if _, exists := backendFactories[bt]; exists {
		panic("server: duplicate service backend factory for " + string(bt))
	}
	backendFactories[bt] = f
}

// storeTypeByBackend maps the backend types that have a corresponding go-xkms
// key-store constant. Only these are valid for CA signing-key storage. Cloud
// and quantum backends have no StoreType in this surface and yield a
// StoreTypeUnsupportedError.
var storeTypeByBackend = map[config.BackendType]xkmssdk.StoreType{
	config.BackendTypeSoftware: xkmssdk.StoreSoftware,
	config.BackendTypeTPM2:     xkmssdk.StoreTPM2,
	config.BackendTypePKCS11:   xkmssdk.StorePKCS11,
}

// tagsByBackend maps a build-tagged backend type to the -tags value required to
// compile it in. Used to build the BackendNotCompiledError diagnostic. Tag-free
// backends are absent (they are always registered, so never reach this map).
var tagsByBackend = map[config.BackendType]string{
	config.BackendTypePKCS11:  "pkcs11",
	config.BackendTypeAWSKMS:  "awskms",
	config.BackendTypeGCPKMS:  "gcpkms",
	config.BackendTypeAzureKV: "azurekv",
	config.BackendTypeVault:   "vault",
}

// tagForBackend returns the -tags value needed to compile bt in, or "" when bt
// is not a build-tagged backend (software/tpm2/quantum, or an unrecognised
// value).
func tagForBackend(bt config.BackendType) string {
	return tagsByBackend[bt]
}

// BackendNotCompiledError is returned when a BackendType is configured but its
// constructor was not compiled into this binary (the corresponding build tag is
// absent). It is a first-class typed error: the resolver never panics and never
// silently downgrades to software, which would defeat the security intent of
// the configuration.
type BackendNotCompiledError struct {
	Backend config.BackendType
	Tag     string
}

// Error implements the error interface.
func (e *BackendNotCompiledError) Error() string {
	return "crypto backend " + string(e.Backend) +
		" not compiled into this binary; rebuild with -tags " + e.Tag
}

// UnknownBackendError is returned when a configured backend value is neither
// auto/empty nor a recognised go-xkms backend type (typically a typo).
type UnknownBackendError struct {
	Backend config.BackendType
}

// Error implements the error interface.
func (e *UnknownBackendError) Error() string {
	return "crypto backend: unknown backend type " + string(e.Backend)
}

// BackendConstructError reports a failure constructing a backend from operator
// config. Backend names the backend type; Cause carries the go-xkms failure.
type BackendConstructError struct {
	Backend config.BackendType
	Cause   error
}

// Error implements the error interface.
func (e *BackendConstructError) Error() string {
	if e.Cause == nil {
		return "crypto backend: constructing " + string(e.Backend) + " backend failed"
	}
	return "crypto backend: constructing " + string(e.Backend) + " backend failed: " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *BackendConstructError) Unwrap() error { return e.Cause }

// StoreTypeUnsupportedError is returned by StoreType for a backend that has no
// go-xkms key-store constant (cloud, quantum). Only software/tpm2/pkcs11 are
// valid CA signing-key stores.
type StoreTypeUnsupportedError struct {
	Backend config.BackendType
}

// Error implements the error interface.
func (e *StoreTypeUnsupportedError) Error() string {
	return "crypto backend: " + string(e.Backend) + " has no key-store type (valid: software, tpm2, pkcs11)"
}

// AEADUnsupportedError is returned by AEADProvider for a backend that cannot
// back an AEAD provider in this phase (anything other than software/tpm2).
type AEADUnsupportedError struct {
	Backend config.BackendType
}

// Error implements the error interface.
func (e *AEADUnsupportedError) Error() string {
	return "crypto backend: " + string(e.Backend) + " AEAD provider not supported (valid: software, tpm2)"
}

// HardwareProbe is the cached result of probing the local platform for crypto
// hardware. It is computed once by DetectHardware at resolver construction.
type HardwareProbe struct {
	// TPM2Present reports that a TPM 2.0 device responded to the probe. A TPM
	// that is present but not yet provisioned still counts as present.
	TPM2Present bool
	// PKCS11Present reports that a configured PKCS#11 library path exists on
	// disk. PKCS#11 is never auto-selected; this is diagnostic only.
	PKCS11Present bool
	// TPM2Device is the device path that responded (diagnostic logging).
	TPM2Device string
	// PKCS11Library is the library path that exists on disk (diagnostic).
	PKCS11Library string
}

// BackendResolver constructs go-xkms backends from BackendSpec values. It is the
// single location where BackendType + KeyRef maps to a go-xkms constructor.
// Construct once at daemon startup via NewBackendResolver and pass it to each
// subsystem wiring function.
type BackendResolver struct {
	cfg   *config.Config
	probe HardwareProbe
}

// NewBackendResolver creates a resolver that has already probed the local
// platform. Hardware detection runs exactly once; the result is cached.
func NewBackendResolver(cfg *config.Config) *BackendResolver {
	return &BackendResolver{cfg: cfg, probe: DetectHardware(cfg)}
}

// Probe returns the cached hardware detection result (diagnostic use).
func (r *BackendResolver) Probe() HardwareProbe {
	return r.probe
}

// Resolve maps a BackendSpec to a concrete BackendType, applying the
// hardware-prefer policy when spec.Backend is "" or "auto": TPM2 if a device is
// present, else software. Cloud and HSM backends are NEVER auto-selected — they
// must be named explicitly. Returns UnknownBackendError for an unrecognised
// type and BackendNotCompiledError when the resolved type's factory is not
// compiled in.
func (r *BackendResolver) Resolve(spec BackendSpec) (config.BackendType, error) {
	bt := spec.Backend
	if config.IsAutoBackend(bt) {
		if r.probe.TPM2Present {
			bt = config.BackendTypeTPM2
		} else {
			bt = config.BackendTypeSoftware
		}
	}
	if !config.IsValidBackendType(bt) {
		return "", &UnknownBackendError{Backend: bt}
	}
	if _, ok := backendFactories[bt]; !ok {
		return "", &BackendNotCompiledError{Backend: bt, Tag: tagForBackend(bt)}
	}
	return bt, nil
}

// ServiceBackend constructs a fully-wrapped xkmssdk.ServiceBackend ready to be
// registered with the global xkms service. Key material is held in a fresh
// go-xkms in-memory store — use this variant only for callers that do NOT need
// persistence across restarts. Persistent callers (startKMS) must use
// ServiceBackendWithStore so keys survive a daemon restart.
func (r *BackendResolver) ServiceBackend(spec BackendSpec) (xkmssdk.ServiceBackend, error) {
	return r.ServiceBackendWithStore(spec, xkmssdk.NewStorageMemory())
}

// ServiceBackendWithStore constructs the resolved backend with caller-supplied
// key storage. This is the persistence seam: startKMS builds a KVStore/file
// store from the data dir and injects it here so KMS keys survive restarts,
// while all build-tag, hardware-prefer, and typed-error logic stays centralised
// in the resolver. A nil keyStore defaults to an in-memory store so callers
// never have to guard against it. The resolved spec (post hardware-prefer) is
// handed to the registered factory.
func (r *BackendResolver) ServiceBackendWithStore(spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	bt, err := r.Resolve(spec)
	if err != nil {
		return nil, err
	}
	if keyStore == nil {
		keyStore = xkmssdk.NewStorageMemory()
	}
	// Present by construction: Resolve already verified the factory exists.
	factory := backendFactories[bt]
	return factory(r.cfg, BackendSpec{Backend: bt, KeyRef: spec.KeyRef}, keyStore)
}

// StoreType maps a BackendSpec to an xkmssdk.StoreType constant for use in
// xkmssdk.KeyConfig / TrustedCAIdentity (CA signing-key storage). It resolves
// the spec first, so a not-compiled hardware backend yields
// BackendNotCompiledError rather than a usable-looking store type. Backends
// without a store constant yield StoreTypeUnsupportedError.
func (r *BackendResolver) StoreType(spec BackendSpec) (xkmssdk.StoreType, error) {
	bt, err := r.Resolve(spec)
	if err != nil {
		return "", err
	}
	st, ok := storeTypeByBackend[bt]
	if !ok {
		return "", &StoreTypeUnsupportedError{Backend: bt}
	}
	return st, nil
}

// AEADProvider returns a qrdb AEAD provider for the data-plane barrier's
// symmetric key wrapping, or nil when the spec resolves to software (nil = qrdb
// uses its software default, which is already AES-256-GCM).
//
// Phase 1 scope: tpm2 also returns nil — the barrier root key is protected by
// the TPM seal strategy, while the per-key-epoch AEAD remains software until the
// TPM backend instance is shared out of buildTPM2Strategy (see design 5.3). Any
// other backend yields AEADUnsupportedError.
func (r *BackendResolver) AEADProvider(spec BackendSpec) (qrdbsdk.AEADProvider, error) {
	bt, err := r.Resolve(spec)
	if err != nil {
		return nil, err
	}
	switch bt {
	case config.BackendTypeSoftware, config.BackendTypeTPM2:
		return nil, nil
	default:
		return nil, &AEADUnsupportedError{Backend: bt}
	}
}

// wrapServiceBackend wraps a constructed go-xkms KeyProvider in a service
// backend with in-memory certificate storage. Certificates are always stored
// externally even for hardware key providers, per the go-xkms contract.
func wrapServiceBackend(bt config.BackendType, kp xkmssdk.KeyProvider) (xkmssdk.ServiceBackend, error) {
	sb, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     kp,
		CertStorage: xkmssdk.NewStorageMemory(),
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: bt, Cause: err}
	}
	return sb, nil
}

// DetectHardware probes the local platform for crypto hardware exactly once. It
// never panics and never fails boot: an absent or inaccessible device simply
// reports absent. Cloud backends are never probed (never auto-selected).
func DetectHardware(cfg *config.Config) HardwareProbe {
	tpmPresent, tpmDevice := probeTPM2(cfg)
	p11Present, p11Library := probePKCS11(cfg)
	return HardwareProbe{
		TPM2Present:   tpmPresent,
		TPM2Device:    tpmDevice,
		PKCS11Present: p11Present,
		PKCS11Library: p11Library,
	}
}

// probeTPM2 reports whether a TPM 2.0 device is accessible. It opens the device
// via go-xkms; success — or an "accessible but not yet provisioned" result —
// counts as present. Any other error (no device, permission denied) counts as
// absent. The probe backend is discarded (and closed when closeable); presence
// is the only signal.
func probeTPM2(cfg *config.Config) (present bool, device string) {
	dev := "/dev/tpmrm0"
	if cfg != nil && cfg.Seal.TPM2.Device != "" {
		dev = cfg.Seal.TPM2.Device
	}
	backend, err := xkmssdk.NewTPM2Backend(&xkmssdk.TPM2BackendConfig{Device: dev})
	if err == nil || errors.Is(err, xkmssdk.ErrTPM2BackendNotInitialized) {
		if closer, ok := any(backend).(interface{ Close() error }); ok && backend != nil {
			_ = closer.Close()
		}
		return true, dev
	}
	return false, ""
}

// probePKCS11 reports whether a configured PKCS#11 library exists on disk.
// Presence is inferred from config + os.Stat, never from dlopen, to avoid the
// cost and risk of loading a shared object at probe time. PKCS#11 is never
// auto-selected; this is diagnostic only.
func probePKCS11(cfg *config.Config) (present bool, library string) {
	if cfg == nil {
		return false, ""
	}
	lib := cfg.Seal.PKCS11.Library
	if lib == "" {
		lib = cfg.KMS.PKCS11.Library
	}
	if lib == "" {
		return false, ""
	}
	if _, err := os.Stat(lib); err != nil {
		return false, ""
	}
	return true, lib
}

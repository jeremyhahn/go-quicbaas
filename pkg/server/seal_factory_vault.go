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

//go:build vault

package server

import (
	"context"
	"crypto/x509"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the HashiCorp Vault Transit sealing strategy with the
// barrier composer. It is compiled only under the `vault` build tag, which
// pulls in go-xkms/pkg/backend/vault (hashicorp/vault/api). When the tag is
// absent the factory is not registered and buildHardwareSealStrategies returns
// a StrategyNotCompiledError instructing the operator to rebuild with -tags
// vault.
//
// QuicBaaS composes go-xkms: a vaultbackend.Backend (which implements
// xkmstypes.Sealer) is constructed from the operator config and wrapped by
// xkmssdk.NewCloudKMSStrategy. No Transit crypto is reimplemented here — the
// plaintext root key is sealed by Vault's Transit Encrypt API under a Transit
// key and never leaves Vault in plaintext form.

// VaultConfigError reports that the operator's Vault seal configuration is
// missing a required field. It is returned before any backend construction is
// attempted so the operator gets a precise, library-independent diagnostic.
// Field names the missing configuration key.
type VaultConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *VaultConfigError) Error() string {
	return "seal strategy: vault config missing required field: " + e.Field
}

// VaultKeyRegistrationError reports that the factory could not ensure the
// operator's Vault Transit sealing key is usable by the backend. KeyName names
// the configured Transit key; Cause carries the underlying go-xkms failure.
type VaultKeyRegistrationError struct {
	KeyName string
	Cause   error
}

// Error implements the error interface.
func (e *VaultKeyRegistrationError) Error() string {
	return "seal strategy: vault sealing key registration failed for key " + e.KeyName + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *VaultKeyRegistrationError) Unwrap() error { return e.Cause }

// buildVaultStrategy constructs the Vault Transit sealing strategy from
// cfg.Seal.Vault.
//
// It validates the operator config up front and surfaces any missing required
// field as a *StrategyBuildError wrapping a *VaultConfigError, so the failure
// is distinguishable from a backend-construction failure. The required fields
// are:
//
//   - Address: the Vault server address.
//   - KeyName: the Transit key name used to wrap the root key.
//
// The Vault token is validated by the go-xkms backend's own config check; an
// empty token surfaces as a *StrategyBuildError wrapping that diagnostic.
//
// On success it builds a vaultbackend.Backend from the config (with in-memory
// metadata stores — the Transit key material stays in Vault), ensures the
// operator's Transit sealing key is registered with the backend (see
// ensureVaultSealKey), and wraps the backend plus the xkmstypes.KeyAttributes
// that identify the Transit-managed AES-256-GCM key by its configured CN (the
// operator's KeyName) in xkmssdk.NewCloudKMSStrategy.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then derives a software DEK from the Vault-protected root key,
// keeping the root key Transit-bound while avoiding a Vault round-trip per data
// operation.
func buildVaultStrategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	v := cfg.Seal.Vault

	if v.Address == "" {
		return nil, &StrategyBuildError{Backend: "vault", Cause: &VaultConfigError{Field: "address"}}
	}
	if v.KeyName == "" {
		return nil, &StrategyBuildError{Backend: "vault", Cause: &VaultConfigError{Field: "key_name"}}
	}

	backendCfg := &xkmssdk.VaultBackendConfig{
		Address:       v.Address,
		Token:         v.Token,
		TransitPath:   v.TransitPath,
		Namespace:     v.Namespace,
		TLSSkipVerify: v.TLSSkipVerify,
		KeyStorage:    xkmssdk.NewStorageMemory(),
		CertStorage:   xkmssdk.NewStorageMemory(),
	}

	backend, err := xkmssdk.NewVaultBackend(backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "vault", Cause: err}
	}

	keyAttrs := vaultSealKeyAttributes(v.KeyName)

	if err := ensureVaultSealKey(backend, keyAttrs); err != nil {
		return nil, &StrategyBuildError{Backend: "vault", Cause: err}
	}

	return xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyVault, backend, keyAttrs, nil), nil
}

// ensureVaultSealKey registers the operator's Transit sealing key with the
// backend so the sealer's SymmetricEncrypter can resolve it.
//
// The Vault backend keeps its key metadata in an in-process store that starts
// empty on every daemon boot; without this step Backend.SymmetricEncrypter
// returns ErrKeyNotFound and the very first barrier init fails. The backend's
// GenerateSymmetricKey both (a) ensures the named Transit key exists in Vault —
// Transit key creation is a server-side idempotent upsert, so re-running it on
// restart neither rotates nor destroys the existing key — and (b) seeds the
// in-process metadata the encrypter needs.
//
// Idempotency: the local metadata store is fresh on each boot, so the backend's
// own ErrKeyAlreadyExists guard (which checks that local store) normally never
// trips here; GenerateSymmetricKey re-seeds the local metadata and re-asserts
// the Transit key every boot without disturbing the operator's key material.
// As a belt-and-braces guard, an ErrKeyAlreadyExists result is treated as
// success — the key is already registered and resolvable — so the routine never
// rotates or destroys an existing key. Any other failure is wrapped in
// *VaultKeyRegistrationError.
func ensureVaultSealKey(b *xkmssdk.VaultBackend, keyAttrs *xkmssdk.KeyAttributes) error {
	if _, err := b.GenerateSymmetricKey(context.Background(), keyAttrs); err != nil && !errors.Is(err, xkmssdk.ErrBackendKeyAlreadyExists) {
		return &VaultKeyRegistrationError{KeyName: keyAttrs.CN, Cause: err}
	}
	return nil
}

// vaultSealKeyAttributes builds the xkmstypes.KeyAttributes that identify the
// Vault Transit-managed symmetric key used to seal and unseal the barrier root
// key. The Vault backend resolves the Transit key from CN, so CN carries the
// operator-configured KeyName.
//
// The attributes describe a 256-bit AES-GCM symmetric key resident in the
// Vault store. SymmetricAlgorithm being non-empty is what makes the backend
// treat the request as a symmetric seal (Backend.Seal rejects asymmetric keys);
// KeyType KeyTypeEncryption marks the key's purpose.
func vaultSealKeyAttributes(keyName string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:                 keyName,
		StoreType:          xkmssdk.StoreVault,
		KeyType:            xkmssdk.KeyTypeEncryption,
		KeyAlgorithm:       x509.UnknownPublicKeyAlgorithm,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
}

// init registers the Vault Transit strategy factory. Reached only when the
// binary is built with -tags vault.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyVault, buildVaultStrategy)
}

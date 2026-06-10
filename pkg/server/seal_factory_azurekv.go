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

//go:build azurekv

package server

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the Azure Key Vault sealing strategy with the barrier
// composer. It is compiled only under the `azurekv` build tag, which pulls in
// go-xkms/pkg/backend/azurekv (azure-sdk-for-go). When the tag is absent the
// factory is not registered and buildHardwareSealStrategies returns a
// StrategyNotCompiledError instructing the operator to rebuild with -tags
// azurekv.
//
// QuicBaaS composes go-xkms: an azurekvbackend.Backend (which implements
// xkmstypes.Sealer) is constructed from the operator config and wrapped by
// xkmssdk.NewCloudKMSStrategy. No vault crypto is reimplemented here — the
// symmetric wrapping key is managed by Azure Key Vault and the root key is
// sealed under it.

// AzureKVConfigError reports that the operator's Azure Key Vault seal
// configuration is missing a required field. It is returned before any backend
// construction is attempted so the operator gets a precise, library-independent
// diagnostic. Field names the missing configuration key.
type AzureKVConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *AzureKVConfigError) Error() string {
	return "seal strategy: azurekv config missing required field: " + e.Field
}

// AzureKVKeyRegistrationError reports that the factory could not ensure the
// operator's Azure Key Vault sealing key is usable by the backend. KeyName
// names the configured key; Cause carries the underlying go-xkms failure.
type AzureKVKeyRegistrationError struct {
	KeyName string
	Cause   error
}

// Error implements the error interface.
func (e *AzureKVKeyRegistrationError) Error() string {
	return "seal strategy: azurekv sealing key registration failed for key " + e.KeyName + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AzureKVKeyRegistrationError) Unwrap() error { return e.Cause }

// buildAzureKVStrategy constructs the Azure Key Vault sealing strategy from
// cfg.Seal.AzureKV.
//
// It validates the operator config up front and surfaces any missing required
// field as a *StrategyBuildError wrapping an *AzureKVConfigError, so the failure
// is distinguishable from a backend-construction failure. The required fields
// are:
//
//   - VaultURL: the Key Vault base URL.
//   - KeyName: the name of the wrapping key within the vault.
//
// On success it builds an azurekvbackend.Backend from the config (with
// in-memory metadata stores — the wrapping key material is managed by Azure
// Key Vault), ensures the operator's wrapping key is usable by the backend
// (see ensureAzureKVSealKey), and wraps the backend plus the
// xkmstypes.KeyAttributes that identify the vault-managed AES-256-GCM key by its
// configured CN (the operator's KeyName) in xkmssdk.NewCloudKMSStrategy.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then derives a software DEK from the vault-protected root key,
// keeping the root key cloud-bound while avoiding a vault round-trip per data
// operation.
func buildAzureKVStrategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	az := cfg.Seal.AzureKV

	if az.VaultURL == "" {
		return nil, &StrategyBuildError{Backend: "azurekv", Cause: &AzureKVConfigError{Field: "vault_url"}}
	}
	if az.KeyName == "" {
		return nil, &StrategyBuildError{Backend: "azurekv", Cause: &AzureKVConfigError{Field: "key_name"}}
	}

	backendCfg := &xkmssdk.AzureKVBackendConfig{
		VaultURL:     az.VaultURL,
		TenantID:     az.TenantID,
		ClientID:     az.ClientID,
		ClientSecret: az.ClientSecret,
		KeyStorage:   xkmssdk.NewStorageMemory(),
		CertStorage:  xkmssdk.NewStorageMemory(),
	}

	backend, err := xkmssdk.NewAzureKVBackend(backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "azurekv", Cause: err}
	}

	keyAttrs := azureKVSealKeyAttributes(az.KeyName)

	if err := ensureAzureKVSealKey(backend, keyAttrs); err != nil {
		return nil, &StrategyBuildError{Backend: "azurekv", Cause: err}
	}

	return xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyAzureKV, backend, keyAttrs, nil), nil
}

// ensureAzureKVSealKey ensures the operator's Azure Key Vault wrapping key is
// usable by the backend so the sealer's SymmetricEncrypter can resolve it.
//
// The Azure KV backend stores symmetric key material as an Azure secret and
// resolves it through GetSymmetricKey, which requires both (a) an in-process
// metadata entry keyed on CN and (b) the secret present in the vault. The
// metadata store starts empty on every daemon boot, so this routine first seeds
// it with Save (overwrite=true) so GetSymmetricKey will reach out to the vault.
//
// It then performs a check-and-skip: GetSymmetricKey succeeds when the secret
// already exists in the vault (a restart after the first init), in which case
// the persisted key is reused unchanged. Only when the secret is genuinely
// absent (backend.ErrKeyNotFound) does it call GenerateSymmetricKey, which
// creates a fresh random AES key, persists it as an Azure secret, and re-seeds
// the metadata — the first-init path.
//
// Idempotency: an existing wrapping key is detected and reused; the factory
// never rotates or deletes it, so previously-sealed data unseals after a
// restart. Failures are wrapped in *AzureKVKeyRegistrationError.
//
// This path can only be validated end to end against a real Azure Key Vault:
// the symmetric-key secret store has no local emulator.
func ensureAzureKVSealKey(b *xkmssdk.AzureKVBackend, keyAttrs *xkmssdk.KeyAttributes) error {
	metadata, err := json.Marshal(map[string]interface{}{
		"cn":        keyAttrs.CN,
		"algorithm": string(keyAttrs.SymmetricAlgorithm),
		"key_size":  keyAttrs.SymmetricAlgorithm.KeySize(),
		"symmetric": true,
	})
	if err != nil {
		return &AzureKVKeyRegistrationError{KeyName: keyAttrs.CN, Cause: err}
	}
	if err := b.Save(keyAttrs, metadata, xkmssdk.FSExtPrivateBlob, true); err != nil {
		return &AzureKVKeyRegistrationError{KeyName: keyAttrs.CN, Cause: err}
	}

	if _, err := b.GetSymmetricKey(context.Background(), keyAttrs); err == nil {
		return nil
	} else if !errors.Is(err, xkmssdk.ErrBackendKeyNotFound) {
		return &AzureKVKeyRegistrationError{KeyName: keyAttrs.CN, Cause: err}
	}

	if _, err := b.GenerateSymmetricKey(context.Background(), keyAttrs); err != nil {
		return &AzureKVKeyRegistrationError{KeyName: keyAttrs.CN, Cause: err}
	}
	return nil
}

// azureKVSealKeyAttributes builds the xkmstypes.KeyAttributes that identify the
// Azure Key Vault-managed symmetric key used to seal and unseal the barrier
// root key. The Azure KV backend resolves the key from CN, so CN carries the
// operator-configured KeyName.
//
// The attributes describe a 256-bit AES-GCM symmetric key resident in the
// Azure Key Vault store. SymmetricAlgorithm being non-empty is what makes the
// backend treat the request as a symmetric seal (Backend.Seal rejects
// asymmetric keys); KeyType KeyTypeEncryption marks the key's purpose.
func azureKVSealKeyAttributes(keyName string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:                 keyName,
		StoreType:          xkmssdk.StoreAzureKV,
		KeyType:            xkmssdk.KeyTypeEncryption,
		KeyAlgorithm:       x509.UnknownPublicKeyAlgorithm,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
}

// init registers the Azure Key Vault strategy factory. Reached only when the
// binary is built with -tags azurekv.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyAzureKV, buildAzureKVStrategy)
}

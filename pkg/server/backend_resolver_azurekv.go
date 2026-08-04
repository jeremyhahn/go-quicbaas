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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the Azure Key Vault backend factory. It is compiled only
// under the `azurekv` build tag. When the tag is absent the factory is
// unregistered and the resolver returns BackendNotCompiledError. The plaintext
// key never leaves Azure Key Vault; nothing is reimplemented here.

// buildAzureKVServiceBackend constructs the Azure Key Vault ServiceBackend from
// cfg.Seal.AzureKV. keyStore is the caller's persistence seam: although the
// plaintext key never leaves Azure Key Vault, the backend's key-metadata store
// is backed by keyStore so per-key references survive restarts.
func buildAzureKVServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	az := cfg.Seal.AzureKV
	backend, err := xkmssdk.NewAzureKVBackend(&xkmssdk.AzureKVBackendConfig{
		VaultURL:     az.VaultURL,
		TenantID:     az.TenantID,
		ClientID:     az.ClientID,
		ClientSecret: az.ClientSecret,
		KeyStorage:   keyStore,
		CertStorage:  xkmssdk.NewStorageMemory(),
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeAzureKV, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeAzureKV, backend)
}

// init registers the Azure Key Vault factory. Reached only under -tags azurekv.
func init() {
	registerBackendFactory(config.BackendTypeAzureKV, buildAzureKVServiceBackend)
}

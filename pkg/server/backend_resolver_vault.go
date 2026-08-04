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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the HashiCorp Vault Transit backend factory. It is
// compiled only under the `vault` build tag. When the tag is absent the factory
// is unregistered and the resolver returns BackendNotCompiledError. The
// plaintext key never leaves Vault Transit; nothing is reimplemented here.

// buildVaultServiceBackend constructs the Vault Transit ServiceBackend from
// cfg.Seal.Vault. keyStore is the caller's persistence seam: although the
// plaintext key never leaves Vault Transit, the backend's key-metadata store is
// backed by keyStore so per-key references survive restarts.
func buildVaultServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	v := cfg.Seal.Vault
	backend, err := xkmssdk.NewVaultBackend(&xkmssdk.VaultBackendConfig{
		Address:       v.Address,
		Token:         v.Token,
		TransitPath:   v.TransitPath,
		Namespace:     v.Namespace,
		TLSSkipVerify: v.TLSSkipVerify,
		KeyStorage:    keyStore,
		CertStorage:   xkmssdk.NewStorageMemory(),
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeVault, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeVault, backend)
}

// init registers the Vault factory. Reached only under -tags vault.
func init() {
	registerBackendFactory(config.BackendTypeVault, buildVaultServiceBackend)
}

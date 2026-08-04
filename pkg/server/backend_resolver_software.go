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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the build-tag-free software backend factory. The software
// backend is always compiled in; it is the default and the auto-detect fallback
// when no hardware is present.

// buildSoftwareServiceBackend constructs the go-xkms software ServiceBackend.
// Key material is held in keyStore — the caller's persistence seam: startKMS
// passes a KVStore/file-backed store so software keys survive restarts; the
// in-memory ServiceBackend variant passes an in-memory store. No crypto is
// reimplemented here — go-xkms owns AES-GCM / ChaCha20-Poly1305 and PKCS#8
// asymmetric operations.
func buildSoftwareServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	backend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStore,
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeSoftware, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeSoftware, backend)
}

// init registers the software factory unconditionally.
func init() {
	registerBackendFactory(config.BackendTypeSoftware, buildSoftwareServiceBackend)
}

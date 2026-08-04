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

// This file registers the build-tag-free post-quantum backend factory. The
// quantum backend (ML-DSA / ML-KEM) is pure Go (cloudflare/circl + crypto/mlkem)
// and requires no build tag. It is never auto-selected; it must be named
// explicitly via backend: quantum.

// buildQuantumServiceBackend constructs the post-quantum ServiceBackend. Key
// material is held in keyStore — the caller's persistence seam: startKMS passes
// a KVStore/file-backed store so quantum keys survive restarts. No PQC is
// reimplemented here — go-xkms owns ML-DSA (FIPS 204) and ML-KEM (FIPS 203).
func buildQuantumServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	backend, err := xkmssdk.NewQuantumBackend(keyStore)
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeQuantum, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeQuantum, backend)
}

// init registers the quantum factory unconditionally.
func init() {
	registerBackendFactory(config.BackendTypeQuantum, buildQuantumServiceBackend)
}

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

//go:build pkcs11

package server

import (
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the PKCS#11 HSM backend factory. It is compiled only
// under the `pkcs11` build tag (cgo + miekg/pkcs11). When the tag is absent the
// factory is unregistered and the resolver returns BackendNotCompiledError. The
// go-xkms pkcs11 backend keeps all key material on the token; nothing is
// reimplemented here.

// buildPKCS11ServiceBackend constructs the PKCS#11 ServiceBackend from
// cfg.Seal.PKCS11. keyStore is the caller's persistence seam: although private
// key material never leaves the token, the backend's key-metadata store is
// backed by keyStore so per-key references survive restarts.
func buildPKCS11ServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	backendCfg, err := pkcs11BackendConfig(cfg.Seal.PKCS11, keyStore)
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypePKCS11, Cause: err}
	}
	backend, err := xkmssdk.NewPKCS11Backend(backendCfg)
	if err != nil {
		backendCfg.PIN.Clear()
		return nil, &BackendConstructError{Backend: config.BackendTypePKCS11, Cause: err}
	}
	sb, err := wrapServiceBackend(config.BackendTypePKCS11, backend)
	if err != nil {
		return nil, errors.Join(err, releasePKCS11Backend(backend))
	}
	return sb, nil
}

// init registers the PKCS#11 factory. Reached only under -tags pkcs11.
func init() {
	registerBackendFactory(config.BackendTypePKCS11, buildPKCS11ServiceBackend)
}

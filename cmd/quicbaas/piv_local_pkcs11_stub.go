//go:build !pkcs11

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

// piv_local_pkcs11_stub.go is the non-pkcs11 build half of the local PIV
// session: selecting the pkcs11 backend in a binary built without the
// pkcs11 tag reports a typed capability error, matching the repo's
// established //go:build pkcs11 gating (pkg/server/seal_factory_pkcs11.go)
// and xkey's identical PIV pattern.
package main

// newPKCS11PIVSession reports that PKCS#11 support is not compiled into
// this binary.
func newPKCS11PIVSession(_ *localPIVConfig) (localPIVSession, error) {
	return nil, &PIVLocalBackendUnsupportedError{
		Backend: localPIVBackendPKCS11,
		Reason:  "this binary was built without PKCS#11 support; rebuild with -tags pkcs11",
	}
}

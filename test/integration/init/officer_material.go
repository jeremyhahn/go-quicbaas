// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

// officer_material.go writes the officer CSR a shamir ceremony row submits to
// `quicbaasd init officer register --csr <path>`. The file is hermetic —
// generated per subtest, stored under the per-subtest tmpdir — so parallel
// rows cannot collide on shared host state.
//
// Nothing here creates a key or builds a CSR itself. The key comes from
// pkg/xkms.GenerateRequesterKey, the one audited generator in this repository
// (a ceremony officer's key is the officer's to hold, which is precisely the
// case that generator exists for), and the CSR is built by go-xkms
// CreateCSRWithKey. Before go-quicbaas#943 this file called
// ecdsa.GenerateKey and x509.CreateCertificateRequest directly, so the daemon's
// CSR loader was exercised against a request no part of the product builds.

package init

import (
	"os"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// OfficerMaterialError reports a failure producing an officer's enrollment
// material.
type OfficerMaterialError struct {
	// Op names the step that failed ("generate key", "create csr", "write").
	Op string

	// Username is the officer the material was for.
	Username string

	// Path is the file involved, when the failure was a write.
	Path string

	// Cause is the underlying failure.
	Cause error
}

func (e *OfficerMaterialError) Error() string {
	msg := "officer material: " + e.Op + " for " + e.Username
	if e.Path != "" {
		msg += " (" + e.Path + ")"
	}
	return msg + ": " + e.Cause.Error()
}

func (e *OfficerMaterialError) Unwrap() error { return e.Cause }

// writeOfficerCSR builds a PKCS#10 certificate signing request for
// CN=<username> over a freshly generated requester key and writes the PEM to
// path.
//
// The daemon's loader verifies the CSR's self-signature, so the request has to
// be genuinely signed by the key it names — which it is: CreateCSRWithKey signs
// with the crypto.Signer it is handed.
func writeOfficerCSR(path, username string) error {
	signer, _, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
	if err != nil {
		return &OfficerMaterialError{Op: "generate key", Username: username, Cause: err}
	}

	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: username},
	}, signer)
	if err != nil {
		return &OfficerMaterialError{Op: "create csr", Username: username, Cause: err}
	}

	if err := os.WriteFile(path, csrPEM, 0o600); err != nil {
		return &OfficerMaterialError{Op: "write", Username: username, Path: path, Cause: err}
	}
	return nil
}

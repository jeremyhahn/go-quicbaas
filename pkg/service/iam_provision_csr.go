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

package service

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strconv"
)

// minAdminRSABits is the smallest RSA modulus the admin certificate accepts.
const minAdminRSABits = 2048

// adminECDSACurves are the curves the admin certificate accepts.
var adminECDSACurves = map[elliptic.Curve]struct{}{
	elliptic.P256(): {},
	elliptic.P384(): {},
	elliptic.P521(): {},
}

// validateAdminCSR checks the admin CSR before provisioning writes anything:
// qrdb signs it only after the admin user exists, so a CSR refused there
// would leave an admin behind with no certificate.
func validateAdminCSR(csrPEM string) error {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return &ProvisionAdminCSRInvalidError{Reason: "not a PEM CERTIFICATE REQUEST block"}
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return &ProvisionAdminCSRInvalidError{Reason: "unparseable request", Cause: err}
	}
	if err := csr.CheckSignature(); err != nil {
		return &ProvisionAdminCSRInvalidError{Reason: "signature does not verify", Cause: err}
	}
	return checkAdminKey(csr.PublicKey)
}

// checkAdminKey enforces the admin certificate key policy.
func checkAdminKey(pub any) error {
	switch key := pub.(type) {
	case *rsa.PublicKey:
		if bits := key.N.BitLen(); bits < minAdminRSABits {
			return &ProvisionAdminCSRKeyError{Key: "RSA-" + strconv.Itoa(bits)}
		}
	case *ecdsa.PublicKey:
		if _, ok := adminECDSACurves[key.Curve]; !ok {
			return &ProvisionAdminCSRKeyError{Key: "ECDSA " + key.Curve.Params().Name}
		}
	case ed25519.PublicKey:
	default:
		return &ProvisionAdminCSRKeyError{Key: fmt.Sprintf("%T", pub)}
	}
	return nil
}

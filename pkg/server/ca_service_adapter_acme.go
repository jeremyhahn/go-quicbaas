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
	"encoding/pem"
	"math/big"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ca_service_adapter_acme.go wires the broker-as-CA capabilities the ACME
// service's Renew verb needs onto the wrapped xkms TrustedCA. The adapter
// already satisfies service.ACMECA via SignCSRPEM (used by Issue and
// FinalizeOrder); these two methods complete service.ACMECertResolver so
// Renew can resolve and revoke an existing certificate by serial number
// without any additional dependency.

// CertificateBySerial returns the PEM-encoded leaf certificate whose hex
// serial number matches serialHex. It enumerates the wrapped CA's certificate
// store (the same store ListStoredCertificates uses) and returns
// service.ErrACMECertNotFound when no issued certificate matches. Satisfies
// service.ACMECertResolver.
func (a *caServiceAdapter) CertificateBySerial(serialHex string) ([]byte, error) {
	want, ok := normalizeSerial(serialHex)
	if !ok {
		return nil, ErrCAServiceAdapterInvalidSerial
	}

	ca := a.getCA()
	if ca == nil {
		return nil, ErrCAServiceAdapterNilCA
	}
	store := ca.CertStore()
	if store == nil {
		return nil, ErrCAServiceAdapterCertStoreUnavailable
	}

	certs, err := store.ListCertificates()
	if err != nil {
		return nil, err
	}
	for _, cert := range certs {
		if cert == nil || cert.SerialNumber == nil {
			continue
		}
		if cert.SerialNumber.Cmp(want) == 0 {
			return pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}), nil
		}
	}
	return nil, service.ErrACMECertNotFound
}

// RevokeBySerial revokes the certificate identified by serialHex on the
// wrapped CA. Satisfies service.ACMECertResolver.
func (a *caServiceAdapter) RevokeBySerial(serialHex string, reason int) error {
	serial, ok := normalizeSerial(serialHex)
	if !ok {
		return ErrCAServiceAdapterInvalidSerial
	}
	ca := a.getCA()
	if ca == nil {
		return ErrCAServiceAdapterNilCA
	}
	return ca.Revoke(serial, reason)
}

// normalizeSerial parses a hex serial number, tolerating an optional "0x"
// prefix and surrounding whitespace. It returns ok=false when the value is not
// valid hexadecimal.
func normalizeSerial(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" {
		return nil, false
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, false
	}
	return n, true
}

// Compile-time checks: the adapter satisfies both ACME service capabilities.
var (
	_ service.ACMECA           = (*caServiceAdapter)(nil)
	_ service.ACMECertResolver = (*caServiceAdapter)(nil)
)

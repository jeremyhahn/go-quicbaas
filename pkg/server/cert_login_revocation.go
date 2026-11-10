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
	"context"
	"crypto/x509"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// caRevocationChecker answers qrdb's per-request revocation contract
// (qrdbsdk.RevocationChecker) on the certificate-login route from the platform
// xkms CA: the authority that issues the tenant admin and agent client
// certificates this node trusts, and the one that records their revocation.
// It decides nothing itself; the CA's revocation store is the answer.
//
// The CA is read from the CA adapter on every call rather than captured, so a
// CA adopted after the node is composed (Server.UpdateCA) is the one consulted.
type caRevocationChecker struct {
	server *Server
}

// IsCertRevoked reports whether the platform CA has revoked peer. With no CA
// to ask, or no serial to ask about, it returns an error so the login fails
// closed.
func (c *caRevocationChecker) IsCertRevoked(_ context.Context, peer *x509.Certificate) (bool, error) {
	ca := c.server.revocationCA()
	if ca == nil {
		return false, &CertRevocationAuthorityUnavailableError{}
	}
	if peer == nil || peer.SerialNumber == nil {
		return false, &CertRevocationSerialMissingError{}
	}
	revoked, err := ca.IsRevoked(peer.SerialNumber)
	if err != nil {
		return false, &CertRevocationLookupError{Cause: err}
	}
	return revoked, nil
}

// revocationCA returns the CA the certificate-login revocation check asks: the
// adapter's, read under its lock, so a concurrent UpdateCA is never a data race.
// It is nil when no CA adapter is wired (TLS disabled).
func (s *Server) revocationCA() xkmssdk.TrustedCA {
	if s.caAdapter == nil {
		return nil
	}
	return s.caAdapter.getCA()
}

// CertRevocationAuthorityUnavailableError reports a certificate login on a
// node that holds no certificate authority to ask about revocation.
type CertRevocationAuthorityUnavailableError struct{}

// Error describes the missing authority.
func (e *CertRevocationAuthorityUnavailableError) Error() string {
	return "server: no certificate authority to check client certificate revocation"
}

// CertRevocationSerialMissingError reports a client certificate with no serial
// number, whose revocation status therefore cannot be looked up.
type CertRevocationSerialMissingError struct{}

// Error describes the missing serial.
func (e *CertRevocationSerialMissingError) Error() string {
	return "server: client certificate carries no serial number to check for revocation"
}

// CertRevocationLookupError reports that the certificate authority could not
// answer whether a client certificate is revoked.
type CertRevocationLookupError struct {
	Cause error
}

// Error describes the failed lookup.
func (e *CertRevocationLookupError) Error() string {
	return "server: client certificate revocation lookup failed: " + e.Cause.Error()
}

// Unwrap returns the CA's error.
func (e *CertRevocationLookupError) Unwrap() error {
	return e.Cause
}

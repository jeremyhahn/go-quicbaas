//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
)

// Token endpoint auth method constants (RFC 8705).
const (
	// AuthMethodClientSecretBasic is the default HTTP Basic client auth method.
	AuthMethodClientSecretBasic = "client_secret_basic"

	// AuthMethodClientSecretPost sends the secret as a form parameter.
	AuthMethodClientSecretPost = "client_secret_post"

	// AuthMethodTLSClientAuth validates the client cert against a PKI trust chain
	// and matches by Subject DN or SAN dNSName (RFC 8705 §2.1).
	AuthMethodTLSClientAuth = "tls_client_auth"

	// AuthMethodSelfSignedTLSClientAuth validates by pinned SHA-256 cert thumbprint
	// without requiring a PKI chain (RFC 8705 §2.2).
	AuthMethodSelfSignedTLSClientAuth = "self_signed_tls_client_auth"
)

// MTLSClientAuthenticator authenticates a confidential OAuth2 client that
// presents an X.509 client certificate at the token endpoint (RFC 8705).
type MTLSClientAuthenticator interface {
	// AuthenticateClient verifies the presented certificate and returns the
	// matching OAuth2 client, or an error if authentication fails.
	AuthenticateClient(ctx context.Context, cert *x509.Certificate, clientID string) (*Application, error)
}

// CertThumbprintS256 returns the base64url-encoded SHA-256 thumbprint of the
// given certificate's raw DER bytes as defined in RFC 8705 §3.1 and RFC 7517.
func CertThumbprintS256(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkiClientAuth authenticates clients whose certificate chains to an operator-
// trusted root pool and whose Subject DN or SAN dNSName matches the registered
// client metadata (RFC 8705 §2.1).
type pkiClientAuth struct {
	roots   *x509.CertPool
	storage AppStorage
}

// NewPKIClientAuth creates a PKI-based client authenticator.
// roots is the operator-managed pool of trusted CA certificates.
func NewPKIClientAuth(roots *x509.CertPool, storage AppStorage) MTLSClientAuthenticator {
	return &pkiClientAuth{roots: roots, storage: storage}
}

// AuthenticateClient verifies the cert chain and resolves the OAuth2 client by
// Subject DN or SAN dNSName match per RFC 8705 §2.1.
func (a *pkiClientAuth) AuthenticateClient(ctx context.Context, cert *x509.Certificate, clientID string) (*Application, error) {
	if cert == nil {
		return nil, ErrMTLSNoCertificate
	}

	// Verify the certificate chain against the trusted root pool.
	opts := x509.VerifyOptions{
		Roots:     a.roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := cert.Verify(opts); err != nil {
		return nil, &MTLSCertVerificationError{Cause: err}
	}

	app, err := a.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, ErrMTLSClientNotFound
	}

	if app.TokenEndpointAuthMethod != AuthMethodTLSClientAuth {
		return nil, ErrMTLSAuthMethodMismatch
	}

	// Match by Subject DN or SAN dNSName per RFC 8705 §2.1.
	if !a.matchesClient(cert, app) {
		return nil, ErrMTLSCertMismatch
	}

	return app, nil
}

// matchesClient returns true when the certificate's Subject DN or any SAN
// dNSName matches the registered client metadata.
func (a *pkiClientAuth) matchesClient(cert *x509.Certificate, app *Application) bool {
	if app.MTLSSubjectDN != "" && cert.Subject.String() == app.MTLSSubjectDN {
		return true
	}
	if app.MTLSSANDNS != "" {
		for _, san := range cert.DNSNames {
			if san == app.MTLSSANDNS {
				return true
			}
		}
	}
	return false
}

// selfSignedClientAuth authenticates clients by comparing the SHA-256 cert
// thumbprint to the value pinned at registration (RFC 8705 §2.2).
// No PKI chain validation is performed; the thumbprint alone is the binding.
type selfSignedClientAuth struct {
	storage AppStorage
}

// NewSelfSignedClientAuth creates a thumbprint-based client authenticator.
func NewSelfSignedClientAuth(storage AppStorage) MTLSClientAuthenticator {
	return &selfSignedClientAuth{storage: storage}
}

// AuthenticateClient verifies the cert thumbprint against the pinned value.
func (a *selfSignedClientAuth) AuthenticateClient(ctx context.Context, cert *x509.Certificate, clientID string) (*Application, error) {
	if cert == nil {
		return nil, ErrMTLSNoCertificate
	}

	app, err := a.storage.GetApp(ctx, clientID)
	if err != nil {
		return nil, ErrMTLSClientNotFound
	}

	if app.TokenEndpointAuthMethod != AuthMethodSelfSignedTLSClientAuth {
		return nil, ErrMTLSAuthMethodMismatch
	}

	if app.MTLSCertThumbprint == "" {
		return nil, ErrMTLSThumbprintNotPinned
	}

	thumbprint := CertThumbprintS256(cert)
	if thumbprint != app.MTLSCertThumbprint {
		return nil, ErrMTLSThumbprintMismatch
	}

	return app, nil
}

// PinMTLSCert parses a PEM-encoded certificate and stores its SHA-256
// thumbprint on the application for self_signed_tls_client_auth registration.
func (app *Application) PinMTLSCert(certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return ErrMTLSInvalidCertPEM
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &MTLSCertParseError{Cause: err}
	}
	app.MTLSCertThumbprint = CertThumbprintS256(cert)
	app.TokenEndpointAuthMethod = AuthMethodSelfSignedTLSClientAuth
	return nil
}

// VerifyMTLSBoundToken checks that the token's cnf.x5t#S256 claim matches
// the SHA-256 thumbprint of the peer certificate presented by the caller.
// Call this from resource-server handlers to enforce RFC 8705 §3.1 binding.
func VerifyMTLSBoundToken(claims map[string]interface{}, peerCert *x509.Certificate) error {
	if peerCert == nil {
		return ErrMTLSNoCertificate
	}

	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok {
		return ErrMTLSMissingCNFClaim
	}

	pinned, ok := cnf["x5t#S256"].(string)
	if !ok || pinned == "" {
		return ErrMTLSMissingCNFClaim
	}

	presented := CertThumbprintS256(peerCert)
	if presented != pinned {
		return ErrMTLSThumbprintMismatch
	}

	return nil
}

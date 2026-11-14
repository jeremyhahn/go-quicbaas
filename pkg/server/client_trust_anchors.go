package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// clientTrustAnchors is the one client trust-anchor set: the CA certificates
// an operator lists in security.tls_ca_file. The TLS handshake and every
// application-layer client-certificate check (the RFC 8705 tls_client_auth
// authenticator, the PIV login verifier and PIV invite activation) trust this
// same set, so a certificate the handshake accepts is one the authenticators
// accept too.
type clientTrustAnchors struct {
	certs []*x509.Certificate
	pool  *x509.CertPool
}

// ClientTrustAnchorsError is returned when an operator-configured
// security.tls_ca_file cannot be read or holds no usable certificate.
type ClientTrustAnchorsError struct {
	Path  string
	Cause error
}

// Error implements error.
func (e *ClientTrustAnchorsError) Error() string {
	return "security.tls_ca_file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying read or parse error.
func (e *ClientTrustAnchorsError) Unwrap() error { return e.Cause }

// loadOperatorClientTrustAnchors loads security.tls_ca_file into
// s.clientTrustAnchors once, before any listener or authenticator is built.
// It must run before initializeCA defaults the path, so only a file the
// operator configured is loaded. An empty path leaves the set nil. A file that
// cannot be read or parses to no certificate is a boot error.
func (s *Server) loadOperatorClientTrustAnchors() error {
	path := s.config.Security.TLSCAFile
	// The defaulted path holds the chain this server's own CA wrote on an
	// earlier boot, not an operator anchor set.
	if path == "" || path == s.defaultTLSCAFile() || s.clientTrustAnchors != nil {
		return nil
	}
	certs, err := loadCertificates(path)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.loadOperatorClientTrustAnchors",
			Err:  &ClientTrustAnchorsError{Path: path, Cause: err},
		}
	}
	anchors := &clientTrustAnchors{certs: certs, pool: x509.NewCertPool()}
	anchors.appendTo(anchors.pool)
	s.clientTrustAnchors = anchors
	s.logger.Infof("client trust anchors loaded from security.tls_ca_file %s", path)
	return nil
}

// defaultTLSCAFile is where initializeCA writes the self-issued CA chain when
// the operator configured no security.tls_ca_file.
func (s *Server) defaultTLSCAFile() string {
	return filepath.Join(s.config.Storage.DataDir, "certs", "ca-chain.crt")
}

// appendTo adds every trust anchor to pool. A nil set adds nothing.
func (a *clientTrustAnchors) appendTo(pool *x509.CertPool) {
	if a == nil {
		return
	}
	for _, cert := range a.certs {
		pool.AddCert(cert)
	}
}

// certPool returns the shared pool, or nil when no tls_ca_file is configured.
// Callers must not add to it; appendTo builds an extended pool instead.
func (a *clientTrustAnchors) certPool() *x509.CertPool {
	if a == nil {
		return nil
	}
	return a.pool
}

// appendClientTrustAnchors adds the operator's trust anchors to the
// handshake's ClientCAs, creating the pool when the config has none.
func (s *Server) appendClientTrustAnchors(tlsConfig *tls.Config) {
	if s.clientTrustAnchors == nil {
		return
	}
	if tlsConfig.ClientCAs == nil {
		tlsConfig.ClientCAs = x509.NewCertPool()
	}
	s.clientTrustAnchors.appendTo(tlsConfig.ClientCAs)
}

// loadCertificates reads every CERTIFICATE block of the PEM bundle at path.
// A bundle with no certificate is a CertPoolEmptyError.
func loadCertificates(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &CertPoolReadError{Path: path, Cause: err}
	}

	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, &CertPoolParseError{Path: path, Cause: err}
		}
		certs = append(certs, cert)
	}

	if len(certs) == 0 {
		return nil, &CertPoolEmptyError{Path: path}
	}

	return certs, nil
}

// CertPoolReadError is returned when a CA pool file cannot be read.
type CertPoolReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *CertPoolReadError) Error() string {
	return "failed to read CA pool file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *CertPoolReadError) Unwrap() error {
	return e.Cause
}

// CertPoolParseError is returned when a certificate in a CA pool file cannot be parsed.
type CertPoolParseError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *CertPoolParseError) Error() string {
	return "failed to parse certificate in " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *CertPoolParseError) Unwrap() error {
	return e.Cause
}

// CertPoolEmptyError is returned when a CA pool file contains no certificates.
type CertPoolEmptyError struct {
	Path string
}

// Error implements the error interface.
func (e *CertPoolEmptyError) Error() string {
	return "no certificates found in " + e.Path
}

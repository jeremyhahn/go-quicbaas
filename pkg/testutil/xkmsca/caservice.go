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

package xkmsca

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// CAService is the transport-shaped view of a real CA, for code that consumes
// xkmstransport.CAService rather than a direct TrustedCA handle.
//
// It is NOT a mock. Every method runs the same go-xkms CA underneath, so
// SignCSR really signs the CSR it is given and IssueCertificate really returns
// what a go-xkms backend returns — including the part a mock always gets wrong:
// an EMPTY PrivateKeyPEM, because the keystore holds the key as a
// non-exportable opaque handle. A stub that invents a private key there is what
// let go-quicbaas#910 ship a client of this interface that could never work.
//
// It stands in for the wire, not for the CA: only the request and response
// structs are transport shapes, and nothing is serialized. A test that needs
// real encoding needs the integration suite.
type CAService struct {
	ca xkmssdk.TrustedCA
	t  *testing.T

	// mu guards every fault and record field below. A served CAService is
	// called on transport handler goroutines while the test goroutine sets
	// faults and reads records, so each field is reached only through the
	// setter and getter methods.
	mu sync.Mutex

	// signCSRErr, when set, is returned by SignCSR instead of signing. It is
	// the honest "the CA refused" condition; the rest of the type stays real.
	signCSRErr error

	// bundleErr, when set, is returned by GetCABundle. Used to exercise the
	// caller's bundle fallback.
	bundleErr error

	// nilBundleResponse makes GetCABundle return (nil, nil) — a served
	// implementation that answers without answering. Real transports can do
	// this, and a caller that dereferences the response crashes on it.
	nilBundleResponse bool

	// suppressChain makes SignCSR omit ChainPEM, standing for a CA that returns
	// the leaf but cannot produce its own chain. It is the second half of the
	// caller's bundle fallback: with bundleErr set too, there is no trust anchor
	// at all and the caller must refuse rather than deliver an unusable bundle.
	suppressChain bool

	// corruptCertificatePEM makes SignCSR return a well-formed PEM envelope
	// around bytes that are not a certificate. Fault injection on the wire, not
	// a fake CA: it is how a caller's parse-failure path gets exercised without
	// pretending the CA signed something it did not.
	corruptCertificatePEM bool

	// signCSRCalls counts the CSRs this service was asked to sign, so a test
	// can assert the caller took the CSR path rather than the keypair path.
	signCSRCalls int

	// lastSignCSR records the most recent request, so a test can inspect the
	// CSR the caller actually built.
	lastSignCSR *xkmstransport.SignCSRRequest
}

// SetSignCSRErr makes SignCSR return err instead of signing; nil restores
// signing.
func (s *CAService) SetSignCSRErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signCSRErr = err
}

// SetBundleErr makes GetCABundle return err; nil restores the bundle.
func (s *CAService) SetBundleErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundleErr = err
}

// SetNilBundleResponse makes GetCABundle answer (nil, nil) while on is true.
func (s *CAService) SetNilBundleResponse(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nilBundleResponse = on
}

// SetSuppressChain makes SignCSR omit ChainPEM while on is true.
func (s *CAService) SetSuppressChain(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suppressChain = on
}

// SetCorruptCertificatePEM makes SignCSR return a PEM envelope around bytes
// that are not a certificate while on is true.
func (s *CAService) SetCorruptCertificatePEM(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.corruptCertificatePEM = on
}

// SignCSRCalls reports how many CSRs this service was asked to sign.
func (s *CAService) SignCSRCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signCSRCalls
}

// LastSignCSR returns the most recent SignCSR request, or nil before the
// first call.
func (s *CAService) LastSignCSR() *xkmstransport.SignCSRRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSignCSR
}

// Compile-time proof the real CA satisfies the interface its consumers declare.
// The assertion is the point: go-xkms's own enroll-01 compliance note records a
// shipped bug where a locally-declared CA interface stopped being satisfiable
// and only a runtime type assertion noticed.
var _ xkmstransport.CAService = (*CAService)(nil)

// NewCAService wraps a real in-memory go-xkms CA in the transport CAService
// interface. commonName names the root; give each test its own.
func NewCAService(t *testing.T, commonName string) *CAService {
	t.Helper()
	return NewCAServiceOver(t, New(t, commonName))
}

// NewCAServiceOver wraps the supplied CA. Pass NewUninitialized's result to get
// a service whose every certificate operation fails the way a CA that is not up
// fails — the honest form of "the CA is unavailable", as opposed to a configured
// error on an otherwise healthy CA.
func NewCAServiceOver(t *testing.T, ca xkmssdk.TrustedCA) *CAService {
	t.Helper()
	return &CAService{ca: ca, t: t}
}

// TrustedCA exposes the underlying CA so a test can verify an issued
// certificate against the authority that signed it.
func (s *CAService) TrustedCA() xkmssdk.TrustedCA { return s.ca }

// SignCSR signs the supplied PKCS#10 CSR with the real CA, under the client
// profile when one is named.
func (s *CAService) SignCSR(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	s.mu.Lock()
	s.signCSRCalls++
	s.lastSignCSR = req
	signErr, corrupt, suppressChain := s.signCSRErr, s.corruptCertificatePEM, s.suppressChain
	s.mu.Unlock()
	if signErr != nil {
		return nil, signErr
	}

	cert, err := s.ca.SignCSR(ctx, req.CSRPEM, &xkmssdk.TrustedCASignOptions{
		Profile:      req.Profile,
		ValidityDays: req.ValidityDays,
		TenantID:     req.TenantID,
	})
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if corrupt {
		certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not-a-certificate")})
	}

	var chain []byte
	if !suppressChain {
		chain = s.bundle()
	}

	return &xkmstransport.SignCSRResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chain,
		SerialNumber:   cert.SerialNumber.Text(16),
	}, nil
}

// IssueCertificate generates inside the CA's keystore, exactly as a served
// go-xkms CA does — which means the response carries NO private key. Kept real
// so a caller that depends on one fails here rather than in production.
func (s *CAService) IssueCertificate(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	issued, err := s.ca.IssueCertificate(ctx, &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:   req.CommonName,
			Organization: req.Organization,
		},
		Valid:    req.ValidityDays,
		TenantID: req.TenantID,
	})
	if err != nil {
		return nil, err
	}
	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: issued.CertificatePEM,
		ChainPEM:       s.bundle(),
		PrivateKeyPEM:  issued.PrivateKeyPEM,
		SerialNumber:   issued.SerialNumber.Text(16),
	}, nil
}

// GetCABundle returns the CA's own certificate as the trust bundle.
func (s *CAService) GetCABundle(_ context.Context, _ *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	s.mu.Lock()
	bundleErr, nilResponse := s.bundleErr, s.nilBundleResponse
	s.mu.Unlock()
	if bundleErr != nil {
		return nil, bundleErr
	}
	if nilResponse {
		return nil, nil
	}
	return &xkmstransport.GetCABundleResponse{BundlePEM: s.bundle()}, nil
}

// GetCACertificate returns the CA's own certificate.
func (s *CAService) GetCACertificate(_ context.Context, _ *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	return &xkmstransport.GetCACertificateResponse{CertificatePEM: s.bundle()}, nil
}

// RevokeCertificate revokes a serial through the real CA.
func (s *CAService) RevokeCertificate(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	serial, ok := parseHexSerial(req.SerialNumber)
	if !ok {
		return &xkmstransport.RevokeCertificateResponse{Success: false}, nil
	}
	if err := s.ca.Revoke(ctx, serial, req.Reason); err != nil {
		return nil, err
	}
	return &xkmstransport.RevokeCertificateResponse{Success: true}, nil
}

// GenerateCRL returns the real CA's CRL.
func (s *CAService) GenerateCRL(ctx context.Context, _ *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	crl, err := s.ca.GenerateCRL(ctx)
	if err != nil {
		return nil, err
	}
	return &xkmstransport.GenerateCRLResponse{CRLPEM: crl}, nil
}

// IsRevoked asks the real CA whether a serial is revoked.
func (s *CAService) IsRevoked(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	serial, ok := parseHexSerial(req.SerialNumber)
	if !ok {
		return &xkmstransport.IsRevokedResponse{Revoked: false}, nil
	}
	revoked, err := s.ca.IsRevoked(serial)
	if err != nil {
		return nil, err
	}
	return &xkmstransport.IsRevokedResponse{Revoked: revoked}, nil
}

// bundle returns the CA certificate PEM, or nil when the CA cannot produce one.
func (s *CAService) bundle() []byte {
	s.t.Helper()
	caCert, err := s.ca.CACertificate()
	if err != nil || caCert == nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})
}

// VerifyAsClientCert reports whether leaf chains to this CA for client
// authentication — the check a peer's mTLS authenticator performs.
func (s *CAService) VerifyAsClientCert(leaf *x509.Certificate) error {
	s.t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(s.bundle()) {
		s.t.Fatal("xkmsca: CA bundle is not usable as a trust anchor")
	}
	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return err
}

// parseHexSerial parses the hex serial spelling the transport uses. It reports
// failure rather than erroring so a caller passing a malformed serial gets the
// same "not revoked" answer a served CA gives for an unknown one.
func parseHexSerial(serial string) (*big.Int, bool) {
	return new(big.Int).SetString(serial, 16)
}

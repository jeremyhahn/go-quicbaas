//go:build rest

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

package rest

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// The adapter also satisfies go-xkms's TCGCAService, so go-xkms's CAHandlers
// serve their TCG routes (EK, AK, TCG-CSR, enroll) over the same CA and
// the other transports reach them through the same adapter (#1330). Like
// the CA verbs, each method only translates types and delegates to the
// wired CA's TCGCA methods.
var _ xkmstransport.TCGCAService = (*xkmsCAServicer)(nil)

// ErrXKMSCATCGNotSupported is returned when the wired CA is not a TCGCA.
var ErrXKMSCATCGNotSupported = errors.New("xkms ca: CA does not support TCG operations")

// ErrXKMSCAMissingPublicKey is returned when an EK or AK request carries no
// public key.
var ErrXKMSCAMissingPublicKey = errors.New("xkms ca: public key is required")

// ErrXKMSCAMissingTCGCSR is returned when a TCG-CSR request carries no CSR.
var ErrXKMSCAMissingTCGCSR = errors.New("xkms ca: tcg csr is required")

// NewXKMSTCGCAService returns the go-xkms TCGCAService adapter the REST
// /api/v1/xkms/ca/tcg/* routes serve, for the server's other transports.
// It returns nil when ca is nil.
func NewXKMSTCGCAService(ca xkmssdk.TrustedCA, spiffeTrustDomain string) xkmstransport.TCGCAService {
	s := newXKMSCAServicer(ca, spiffeTrustDomain)
	if s == nil {
		return nil
	}
	return s
}

// tcgRequest checks the TCG preconditions shared by every TCG verb and
// builds the CertificateRequest the TCGCA methods take.
func (a *xkmsCAServicer) tcgRequest(ctx context.Context, op, commonName, organization, tenant string) (xkmssdk.TCGCA, *xkmssdk.TrustedCACertificateRequest, error) {
	tcg, ok := a.ca.(xkmssdk.TCGCA)
	if !ok {
		return nil, nil, ErrXKMSCATCGNotSupported
	}
	if commonName == "" {
		return nil, nil, ErrXKMSCAMissingCommonName
	}
	tenantID, err := a.resolveTenant(ctx, op, tenant)
	if err != nil {
		return nil, nil, err
	}
	return tcg, &xkmssdk.TrustedCACertificateRequest{
		Subject:  xkmssdk.TrustedCASubject{CommonName: commonName, Organization: organization},
		TenantID: tenantID,
	}, nil
}

// issuedKeyCertificate parses a DER public key, has issue sign a certificate
// for it, and returns the certificate as DER, PEM and hex serial.
func issuedKeyCertificate(op string, pubDER []byte, issue func(pub any) (*x509.Certificate, error)) (der, pemBytes []byte, serial string, err error) {
	if len(pubDER) == 0 {
		return nil, nil, "", ErrXKMSCAMissingPublicKey
	}
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, nil, "", &XKMSCAOperationError{Op: op, Err: err}
	}
	cert, err := issue(pub)
	if err != nil {
		return nil, nil, "", &XKMSCAOperationError{Op: op, Err: err}
	}
	if cert == nil {
		return nil, nil, "", &XKMSCAOperationError{Op: op, Err: errors.New("ca returned nil certificate")}
	}
	return cert.Raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), cert.SerialNumber.Text(16), nil
}

// IssueEKCertificate issues an Endorsement Key certificate for a DER EK
// public key.
func (a *xkmsCAServicer) IssueEKCertificate(ctx context.Context, req *xkmstransport.IssueEKCertificateRequest) (*xkmstransport.IssueEKCertificateResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	tcg, certReq, err := a.tcgRequest(ctx, "issue ek certificate", req.CommonName, req.Organization, req.TenantID)
	if err != nil {
		return nil, err
	}
	der, p, serial, err := issuedKeyCertificate("issue ek certificate", req.EKPublicKey, func(pub any) (*x509.Certificate, error) {
		return tcg.IssueEKCertificate(ctx, certReq, pub)
	})
	if err != nil {
		return nil, err
	}
	return &xkmstransport.IssueEKCertificateResponse{CertificateDER: der, CertificatePEM: p, SerialNumber: serial}, nil
}

// IssueAKCertificate issues an Attestation Key certificate for a DER AK
// public key.
func (a *xkmsCAServicer) IssueAKCertificate(ctx context.Context, req *xkmstransport.IssueAKCertificateRequest) (*xkmstransport.IssueAKCertificateResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	tcg, certReq, err := a.tcgRequest(ctx, "issue ak certificate", req.CommonName, req.Organization, req.TenantID)
	if err != nil {
		return nil, err
	}
	der, p, serial, err := issuedKeyCertificate("issue ak certificate", req.PublicKey, func(pub any) (*x509.Certificate, error) {
		return tcg.IssueAKCertificate(ctx, certReq, pub)
	})
	if err != nil {
		return nil, err
	}
	return &xkmstransport.IssueAKCertificateResponse{CertificateDER: der, CertificatePEM: p, SerialNumber: serial}, nil
}

// SignTCGCSR signs a packed TCG-CSR-IDEVID and returns the IAK and IDevID
// certificates.
func (a *xkmsCAServicer) SignTCGCSR(ctx context.Context, req *xkmstransport.SignTCGCSRRequest) (*xkmstransport.SignTCGCSRResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	tcg, certReq, err := a.tcgRequest(ctx, "sign tcg csr", req.CommonName, req.Organization, req.TenantID)
	if err != nil {
		return nil, err
	}
	if len(req.TCGCSR) == 0 {
		return nil, ErrXKMSCAMissingTCGCSR
	}
	csr, err := xkmssdk.UnmarshalIDevIDCSR(req.TCGCSR)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "sign tcg csr", Err: err}
	}
	iak, idevid, err := tcg.SignTCGCSRIDevID(ctx, csr, certReq)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "sign tcg csr", Err: err}
	}
	return &xkmstransport.SignTCGCSRResponse{IAKCertDER: iak, IDevIDCertDER: idevid}, nil
}

// EnrollDevice runs TCG device enrollment for a packed TCG-CSR-IDEVID and
// returns the certificates with the credential-activation challenge.
func (a *xkmsCAServicer) EnrollDevice(ctx context.Context, req *xkmstransport.EnrollDeviceRequest) (*xkmstransport.EnrollDeviceResponse, error) {
	if req == nil {
		return nil, ErrXKMSCANilRequest
	}
	tcg, certReq, err := a.tcgRequest(ctx, "enroll device", req.CommonName, req.Organization, req.TenantID)
	if err != nil {
		return nil, err
	}
	if len(req.PackedCSR) == 0 {
		return nil, ErrXKMSCAMissingTCGCSR
	}
	res, err := tcg.EnrollDevice(ctx, req.PackedCSR, certReq)
	if err != nil {
		return nil, &XKMSCAOperationError{Op: "enroll device", Err: err}
	}
	if res == nil {
		return nil, &XKMSCAOperationError{Op: "enroll device", Err: errors.New("ca returned nil enrollment")}
	}
	return &xkmstransport.EnrollDeviceResponse{
		IAKCertDER: res.IAKCertDER, IDevIDCertDER: res.IDevIDCertDER,
		CredentialBlob: res.CredentialBlob, EncryptedSecret: res.EncryptedSecret, PlainSecret: res.PlainSecret,
	}, nil
}

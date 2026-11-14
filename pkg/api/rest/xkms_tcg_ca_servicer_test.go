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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTCGCA answers the TCG verbs; every other TCGCA method is the nil
// embedded interface and is never called by these tests.
type fakeTCGCA struct {
	xkmssdk.TCGCA
	cert       *x509.Certificate
	gotReq     *xkmssdk.TrustedCACertificateRequest
	gotPub     crypto.PublicKey
	gotPacked  []byte
	enrollment *xkmssdk.TCGEnrollmentResult
}

func (f *fakeTCGCA) IssueEKCertificate(_ context.Context, req *xkmssdk.TrustedCACertificateRequest, pub crypto.PublicKey) (*x509.Certificate, error) {
	f.gotReq, f.gotPub = req, pub
	return f.cert, nil
}

func (f *fakeTCGCA) IssueAKCertificate(_ context.Context, req *xkmssdk.TrustedCACertificateRequest, pub crypto.PublicKey) (*x509.Certificate, error) {
	f.gotReq, f.gotPub = req, pub
	return f.cert, nil
}

func (f *fakeTCGCA) EnrollDevice(_ context.Context, packed []byte, req *xkmssdk.TrustedCACertificateRequest) (*xkmssdk.TCGEnrollmentResult, error) {
	f.gotReq, f.gotPacked = req, packed
	return f.enrollment, nil
}

// plainCA is a TrustedCA that is not a TCGCA.
type plainCA struct{ xkmssdk.TrustedCA }

func tcgTestKeyAndCert(t *testing.T) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(0x2a), Subject: pkix.Name{CommonName: "ek"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return pubDER, cert
}

// TestXKMSTCGCAServicer_DelegatesToTheTCGCA pins #1330's TCGCAService: each
// verb parses its wire input and delegates to the wired TCGCA.
func TestXKMSTCGCAServicer_DelegatesToTheTCGCA(t *testing.T) {
	pubDER, cert := tcgTestKeyAndCert(t)
	ca := &fakeTCGCA{cert: cert, enrollment: &xkmssdk.TCGEnrollmentResult{IAKCertDER: []byte("IAK"), CredentialBlob: []byte("BLOB")}}
	svc := NewXKMSTCGCAService(ca, "")
	require.NotNil(t, svc)
	ctx := context.Background()

	ek, err := svc.IssueEKCertificate(ctx, &xkmstransport.IssueEKCertificateRequest{CommonName: "dev-1", Organization: "acme", EKPublicKey: pubDER})
	require.NoError(t, err)
	assert.Equal(t, cert.Raw, ek.CertificateDER)
	assert.Contains(t, string(ek.CertificatePEM), "BEGIN CERTIFICATE")
	assert.Equal(t, "2a", ek.SerialNumber)
	assert.Equal(t, "dev-1", ca.gotReq.Subject.CommonName)
	assert.Equal(t, "acme", ca.gotReq.Subject.Organization)
	assert.IsType(t, &ecdsa.PublicKey{}, ca.gotPub)

	ak, err := svc.IssueAKCertificate(ctx, &xkmstransport.IssueAKCertificateRequest{CommonName: "ak-1", PublicKey: pubDER})
	require.NoError(t, err)
	assert.Equal(t, "2a", ak.SerialNumber)
	assert.Equal(t, "ak-1", ca.gotReq.Subject.CommonName)

	enrolled, err := svc.EnrollDevice(ctx, &xkmstransport.EnrollDeviceRequest{CommonName: "dev-1", PackedCSR: []byte("PACKED")})
	require.NoError(t, err)
	assert.Equal(t, []byte("PACKED"), ca.gotPacked)
	assert.Equal(t, []byte("IAK"), enrolled.IAKCertDER)
	assert.Equal(t, []byte("BLOB"), enrolled.CredentialBlob)
}

// TestXKMSTCGCAServicer_Refusals covers the input checks and a CA that is
// not a TCGCA.
func TestXKMSTCGCAServicer_Refusals(t *testing.T) {
	ctx := context.Background()
	svc := NewXKMSTCGCAService(&fakeTCGCA{}, "")

	_, err := svc.IssueEKCertificate(ctx, nil)
	assert.ErrorIs(t, err, ErrXKMSCANilRequest)
	_, err = svc.IssueEKCertificate(ctx, &xkmstransport.IssueEKCertificateRequest{EKPublicKey: []byte{1}})
	assert.ErrorIs(t, err, ErrXKMSCAMissingCommonName)
	_, err = svc.IssueAKCertificate(ctx, &xkmstransport.IssueAKCertificateRequest{CommonName: "ak"})
	assert.ErrorIs(t, err, ErrXKMSCAMissingPublicKey)
	_, err = svc.IssueAKCertificate(ctx, &xkmstransport.IssueAKCertificateRequest{CommonName: "ak", PublicKey: []byte("not der")})
	var opErr *XKMSCAOperationError
	assert.ErrorAs(t, err, &opErr)
	_, err = svc.SignTCGCSR(ctx, &xkmstransport.SignTCGCSRRequest{CommonName: "d"})
	assert.ErrorIs(t, err, ErrXKMSCAMissingTCGCSR)
	_, err = svc.SignTCGCSR(ctx, &xkmstransport.SignTCGCSRRequest{CommonName: "d", TCGCSR: []byte{0}})
	assert.ErrorAs(t, err, &opErr)
	_, err = svc.EnrollDevice(ctx, &xkmstransport.EnrollDeviceRequest{CommonName: "d"})
	assert.ErrorIs(t, err, ErrXKMSCAMissingTCGCSR)

	_, err = NewXKMSTCGCAService(&plainCA{}, "").EnrollDevice(ctx, &xkmstransport.EnrollDeviceRequest{CommonName: "d", PackedCSR: []byte{1}})
	assert.ErrorIs(t, err, ErrXKMSCATCGNotSupported)
	assert.Nil(t, NewXKMSTCGCAService(nil, ""))
}

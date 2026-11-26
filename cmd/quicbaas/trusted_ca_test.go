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

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	xkmstransport "github.com/jeremyhahn/go-xkms/pkg/api/transport"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Mocks
// =============================================================================

// mockTrustedCAService records every request and returns canned responses.
type mockTrustedCAService struct {
	err error

	bundleReq   *xkmstransport.GetCABundleRequest
	certReq     *xkmstransport.GetCACertificateRequest
	signReq     *xkmstransport.SignCSRRequest
	issueReq    *xkmstransport.IssueCertificateRequest
	revokeReq   *xkmstransport.RevokeCertificateRequest
	crlReq      *xkmstransport.GenerateCRLRequest
	isRevokeReq *xkmstransport.IsRevokedRequest

	bundle *xkmstransport.GetCABundleResponse
	cert   *xkmstransport.GetCACertificateResponse
	sign   *xkmstransport.SignCSRResponse
	issue  *xkmstransport.IssueCertificateResponse
	revoke *xkmstransport.RevokeCertificateResponse
	crl    *xkmstransport.GenerateCRLResponse
	isRev  *xkmstransport.IsRevokedResponse
}

func (m *mockTrustedCAService) GetCABundle(_ context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	m.bundleReq = req
	return m.bundle, m.err
}

func (m *mockTrustedCAService) GetCACertificate(_ context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	m.certReq = req
	return m.cert, m.err
}

func (m *mockTrustedCAService) SignCSR(_ context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	m.signReq = req
	return m.sign, m.err
}

func (m *mockTrustedCAService) IssueCertificate(_ context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	m.issueReq = req
	return m.issue, m.err
}

func (m *mockTrustedCAService) RevokeCertificate(_ context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	m.revokeReq = req
	return m.revoke, m.err
}

func (m *mockTrustedCAService) GenerateCRL(_ context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	m.crlReq = req
	return m.crl, m.err
}

func (m *mockTrustedCAService) IsRevoked(_ context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	m.isRevokeReq = req
	return m.isRev, m.err
}

// mockTCGCAService records every request and returns canned responses.
type mockTCGCAService struct {
	err error

	ekReq     *xkmstransport.IssueEKCertificateRequest
	akReq     *xkmstransport.IssueAKCertificateRequest
	signReq   *xkmstransport.SignTCGCSRRequest
	enrollReq *xkmstransport.EnrollDeviceRequest

	ek     *xkmstransport.IssueEKCertificateResponse
	ak     *xkmstransport.IssueAKCertificateResponse
	sign   *xkmstransport.SignTCGCSRResponse
	enroll *xkmstransport.EnrollDeviceResponse
}

func (m *mockTCGCAService) IssueEKCertificate(_ context.Context, req *xkmstransport.IssueEKCertificateRequest) (*xkmstransport.IssueEKCertificateResponse, error) {
	m.ekReq = req
	return m.ek, m.err
}

func (m *mockTCGCAService) IssueAKCertificate(_ context.Context, req *xkmstransport.IssueAKCertificateRequest) (*xkmstransport.IssueAKCertificateResponse, error) {
	m.akReq = req
	return m.ak, m.err
}

func (m *mockTCGCAService) SignTCGCSR(_ context.Context, req *xkmstransport.SignTCGCSRRequest) (*xkmstransport.SignTCGCSRResponse, error) {
	m.signReq = req
	return m.sign, m.err
}

func (m *mockTCGCAService) EnrollDevice(_ context.Context, req *xkmstransport.EnrollDeviceRequest) (*xkmstransport.EnrollDeviceResponse, error) {
	m.enrollReq = req
	return m.enroll, m.err
}

// =============================================================================
// Helpers
// =============================================================================

var errTrustedCAFactoryUnreachable = errors.New("factory must not be reached")

// withTrustedCAMocks swaps both factories for the given mocks (nil means the
// factory fails if reached) and restores them, and the timeout, on cleanup.
func withTrustedCAMocks(t *testing.T, ca *mockTrustedCAService, tcg *mockTCGCAService) {
	t.Helper()
	origCA := trustedCAClientFactory
	origTCG := tcgCAClientFactory
	origTimeout := globalTimeout

	trustedCAClientFactory = func() (quicbaas.TrustedCAService, error) {
		if ca == nil {
			return nil, errTrustedCAFactoryUnreachable
		}
		return ca, nil
	}
	tcgCAClientFactory = func() (quicbaas.TCGCAService, error) {
		if tcg == nil {
			return nil, errTrustedCAFactoryUnreachable
		}
		return tcg, nil
	}
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}

	t.Cleanup(func() {
		trustedCAClientFactory = origCA
		tcgCAClientFactory = origTCG
		globalTimeout = origTimeout
	})
}

// runTrustedCACLI executes `trusted-ca <args...>` through a fresh command tree
// (which resets every flag to its default) and returns stdout.
func runTrustedCACLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newTrustedCACommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// trustedCATestCA returns a CA certificate and key able to sign CRLs.
func trustedCATestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: "trusted-ca.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		SubjectKeyId:          []byte{1, 2, 3, 4},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

func pemCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func writeTrustedCAFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func publicKeyPEMFile(t *testing.T) (string, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return writeTrustedCAFile(t, "pub.pem", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), der
}

// =============================================================================
// Tree
// =============================================================================

func TestNewTrustedCACommand_VerbTree(t *testing.T) {
	cmd := newTrustedCACommand()
	require.Equal(t, "trusted-ca", cmd.Use)
	require.NotNil(t, cmd.PersistentFlags().Lookup("output"))
	assert.Contains(t, cmd.Long, "Security().TrustedCA()")
	assert.Contains(t, cmd.Long, "Security().TCGCA()")

	got := make(map[string]bool)
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, verb := range []string{"bundle", "certificate", "sign-csr", "issue", "revoke", "is-revoked", "crl", "tcg"} {
		assert.True(t, got[verb], "trusted-ca is missing the %q verb", verb)
	}
	assert.Len(t, cmd.Commands(), 8)

	tcg, _, err := cmd.Find([]string{"tcg"})
	require.NoError(t, err)
	tcgVerbs := make(map[string]bool)
	for _, c := range tcg.Commands() {
		tcgVerbs[c.Name()] = true
	}
	for _, verb := range []string{"ek", "ak", "sign-csr", "enroll"} {
		assert.True(t, tcgVerbs[verb], "trusted-ca tcg is missing the %q verb", verb)
	}
	assert.Len(t, tcg.Commands(), 4)
}

// =============================================================================
// CAService verbs
// =============================================================================

func TestTrustedCABundle_Success(t *testing.T) {
	ca, _ := trustedCATestCA(t)
	m := &mockTrustedCAService{bundle: &xkmstransport.GetCABundleResponse{
		BundlePEM:    pemCert(ca),
		Certificates: [][]byte{ca.Raw, []byte("not der")},
		ContentType:  "application/x-pem-file",
	}}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "bundle", "--store-type", "software", "--algorithm", "ECDSA")
	require.NoError(t, err)
	require.NotNil(t, m.bundleReq)
	assert.Equal(t, "software", m.bundleReq.StoreType)
	assert.Equal(t, "ECDSA", m.bundleReq.Algorithm)
	assert.Contains(t, out, "Certificates:")
	assert.Contains(t, out, "CN=trusted-ca.test")
	assert.Contains(t, out, "application/x-pem-file")
	assert.NotContains(t, out, "BEGIN CERTIFICATE", "table output omits PEM")
}

func TestTrustedCACertificate_SuccessAllFormats(t *testing.T) {
	ca, _ := trustedCATestCA(t)
	resp := &xkmstransport.GetCACertificateResponse{
		CertificatePEM: pemCert(ca),
		Subject:        "CN=trusted-ca.test",
		Issuer:         "CN=trusted-ca.test",
		SerialNumber:   "07",
		NotBefore:      "2026-01-01T00:00:00Z",
		NotAfter:       "2027-01-01T00:00:00Z",
		IsCA:           true,
	}
	m := &mockTrustedCAService{cert: resp}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "certificate", "--identity", "root")
	require.NoError(t, err)
	assert.Equal(t, "root", m.certReq.Identity)
	for _, want := range []string{"CN=trusted-ca.test", "07", "2027-01-01T00:00:00Z", "Is-CA:", "true"} {
		assert.Contains(t, out, want)
	}

	out, err = runTrustedCACLI(t, "certificate", "--output", "json")
	require.NoError(t, err)
	var gotJSON xkmstransport.GetCACertificateResponse
	require.NoError(t, json.Unmarshal([]byte(out), &gotJSON))
	assert.Equal(t, *resp, gotJSON, "json carries the go-xkms response as is, PEM included")

	out, err = runTrustedCACLI(t, "certificate", "--output", "yaml")
	require.NoError(t, err)
	var gotYAML map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(out), &gotYAML))
	assert.Equal(t, "07", gotYAML["serialnumber"])

	_, err = runTrustedCACLI(t, "certificate", "--output", "xml")
	require.Error(t, err)
}

func TestTrustedCASignCSR_Success(t *testing.T) {
	ca, _ := trustedCATestCA(t)
	csrPath := writeTrustedCAFile(t, "req.csr", []byte("-----BEGIN CERTIFICATE REQUEST-----\n"))
	m := &mockTrustedCAService{sign: &xkmstransport.SignCSRResponse{CertificatePEM: pemCert(ca), SerialNumber: "0a"}}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "sign-csr", "--csr", csrPath, "--profile", "server", "--validity-days", "30")
	require.NoError(t, err)
	require.NotNil(t, m.signReq)
	assert.Equal(t, []byte("-----BEGIN CERTIFICATE REQUEST-----\n"), m.signReq.CSRPEM)
	assert.Equal(t, "server", m.signReq.Profile)
	assert.Equal(t, 30, m.signReq.ValidityDays)
	assert.Contains(t, out, "0a")
	assert.Contains(t, out, "CN=trusted-ca.test")
	assert.Contains(t, out, "Not-After:")
}

func TestTrustedCAIssue_Success(t *testing.T) {
	m := &mockTrustedCAService{issue: &xkmstransport.IssueCertificateResponse{
		CertificatePEM: []byte("garbage"),
		PrivateKeyPEM:  []byte("key"),
		SerialNumber:   "0b",
	}}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "issue", "--common-name", "leaf.example.com",
		"--organization", "Acme", "--dns", "a.example.com", "--dns", "b.example.com",
		"--profile", "client", "--validity-days", "7", "--algorithm", "ecdsa-p256")
	require.NoError(t, err)
	require.NotNil(t, m.issueReq)
	assert.Equal(t, xkmstransport.IssueCertificateRequest{
		Profile:      "client",
		CommonName:   "leaf.example.com",
		Organization: "Acme",
		SANs:         []string{"DNS:a.example.com", "DNS:b.example.com"},
		ValidityDays: 7,
		Algorithm:    "ecdsa-p256",
	}, *m.issueReq)
	assert.Contains(t, out, "0b")
	assert.Contains(t, out, "Private-Key:")
	assert.NotContains(t, out, "key\n")
}

func TestTrustedCARevokeAndIsRevoked_Success(t *testing.T) {
	m := &mockTrustedCAService{
		revoke: &xkmstransport.RevokeCertificateResponse{Success: true, Message: "revoked"},
		isRev:  &xkmstransport.IsRevokedResponse{Revoked: true, Reason: 1, Message: "keyCompromise"},
	}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "revoke", "0c", "--reason", "1")
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.RevokeCertificateRequest{SerialNumber: "0c", Reason: 1}, *m.revokeReq)
	assert.Contains(t, out, "Success:")
	assert.Contains(t, out, "revoked")

	out, err = runTrustedCACLI(t, "is-revoked", "0c", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.IsRevokedRequest{SerialNumber: "0c"}, *m.isRevokeReq)
	var got xkmstransport.IsRevokedResponse
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.True(t, got.Revoked)
	assert.Equal(t, 1, got.Reason)

	out, err = runTrustedCACLI(t, "is-revoked", "0c")
	require.NoError(t, err)
	assert.Contains(t, out, "Revoked:")
	assert.Contains(t, out, "keyCompromise")
}

func TestTrustedCACRL_Success(t *testing.T) {
	ca, key := trustedCATestCA(t)
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:     big.NewInt(1),
		ThisUpdate: time.Now().Add(-time.Minute),
		NextUpdate: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		RevokedCertificateEntries: []x509.RevocationListEntry{
			{SerialNumber: big.NewInt(9), RevocationTime: time.Now().Add(-time.Minute)},
		},
	}, ca, key)
	require.NoError(t, err)
	m := &mockTrustedCAService{crl: &xkmstransport.GenerateCRLResponse{
		CRLPEM: pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}),
	}}
	withTrustedCAMocks(t, m, nil)

	out, err := runTrustedCACLI(t, "crl")
	require.NoError(t, err)
	require.NotNil(t, m.crlReq)
	assert.Contains(t, out, "CRL-Bytes:")
	assert.Contains(t, out, "2030-01-02T03:04:05Z")
	assert.NotContains(t, out, "BEGIN X509 CRL")

	m.crl = &xkmstransport.GenerateCRLResponse{CRLPEM: []byte("not pem")}
	out, err = runTrustedCACLI(t, "crl")
	require.NoError(t, err)
	assert.NotContains(t, out, "Next-Update")
}

// =============================================================================
// TCGCAService verbs
// =============================================================================

func TestTrustedCATCGKeyVerbs_Success(t *testing.T) {
	ca, _ := trustedCATestCA(t)
	pubPath, pubDER := publicKeyPEMFile(t)
	m := &mockTCGCAService{
		ek: &xkmstransport.IssueEKCertificateResponse{CertificateDER: ca.Raw, SerialNumber: "e1"},
		ak: &xkmstransport.IssueAKCertificateResponse{CertificateDER: ca.Raw, SerialNumber: "a1"},
	}
	withTrustedCAMocks(t, nil, m)

	out, err := runTrustedCACLI(t, "tcg", "ek", "--common-name", "ek.device", "--organization", "Acme", "--public-key", pubPath)
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.IssueEKCertificateRequest{CommonName: "ek.device", Organization: "Acme", EKPublicKey: pubDER}, *m.ekReq)
	assert.Contains(t, out, "e1")
	assert.Contains(t, out, "CN=trusted-ca.test")

	out, err = runTrustedCACLI(t, "tcg", "ak", "--common-name", "ak.device", "--public-key", pubPath)
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.IssueAKCertificateRequest{CommonName: "ak.device", PublicKey: pubDER}, *m.akReq)
	assert.Contains(t, out, "a1")
}

func TestTrustedCATCGCSRVerbs_Success(t *testing.T) {
	ca, _ := trustedCATestCA(t)
	csrPath := writeTrustedCAFile(t, "tcg.csr", []byte{0x01, 0x02, 0x03})
	m := &mockTCGCAService{
		sign:   &xkmstransport.SignTCGCSRResponse{IAKCertDER: ca.Raw, IDevIDCertDER: ca.Raw},
		enroll: &xkmstransport.EnrollDeviceResponse{IAKCertDER: ca.Raw, CredentialBlob: []byte{1, 2}},
	}
	withTrustedCAMocks(t, nil, m)

	out, err := runTrustedCACLI(t, "tcg", "sign-csr", "--common-name", "dev", "--organization", "Acme", "--tcg-csr", csrPath)
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.SignTCGCSRRequest{CommonName: "dev", Organization: "Acme", TCGCSR: []byte{0x01, 0x02, 0x03}}, *m.signReq)
	assert.Contains(t, out, "IAK-Subject:")
	assert.Contains(t, out, "IDevID-Subject:")

	out, err = runTrustedCACLI(t, "tcg", "enroll", "--common-name", "dev", "--tcg-csr", csrPath)
	require.NoError(t, err)
	assert.Equal(t, xkmstransport.EnrollDeviceRequest{CommonName: "dev", PackedCSR: []byte{0x01, 0x02, 0x03}}, *m.enrollReq)
	assert.Contains(t, out, "Credential-Blob-Bytes:")
}

// =============================================================================
// Errors
// =============================================================================

func TestTrustedCA_MissingFlagsAndArgs(t *testing.T) {
	pubPath, _ := publicKeyPEMFile(t)
	csrPath := writeTrustedCAFile(t, "tcg.csr", []byte{0x01})
	withTrustedCAMocks(t, nil, nil)

	flagCases := []struct {
		args []string
		verb string
		flag string
	}{
		{[]string{"sign-csr"}, "sign-csr", "csr"},
		{[]string{"issue"}, "issue", "common-name"},
		{[]string{"tcg", "ek", "--public-key", pubPath}, "tcg ek", "common-name"},
		{[]string{"tcg", "ek", "--common-name", "x"}, "tcg ek", "public-key"},
		{[]string{"tcg", "ak", "--common-name", "x"}, "tcg ak", "public-key"},
		{[]string{"tcg", "sign-csr", "--tcg-csr", csrPath}, "tcg sign-csr", "common-name"},
		{[]string{"tcg", "sign-csr", "--common-name", "x"}, "tcg sign-csr", "tcg-csr"},
		{[]string{"tcg", "enroll", "--common-name", "x"}, "tcg enroll", "tcg-csr"},
	}
	for _, tc := range flagCases {
		_, err := runTrustedCACLI(t, tc.args...)
		var flagErr *TrustedCAFlagRequiredError
		require.ErrorAs(t, err, &flagErr, "%v", tc.args)
		assert.Equal(t, tc.verb, flagErr.Verb)
		assert.Equal(t, tc.flag, flagErr.Flag)
		assert.Contains(t, err.Error(), "--"+tc.flag+" is required")
	}

	for _, verb := range []string{"revoke", "is-revoked"} {
		_, err := runTrustedCACLI(t, verb)
		var argErr *TrustedCAArgRequiredError
		require.ErrorAs(t, err, &argErr, verb)
		assert.Equal(t, verb, argErr.Verb)
		assert.Contains(t, err.Error(), "<serial>")
	}
}

func TestTrustedCA_UnreadableAndInvalidFiles(t *testing.T) {
	withTrustedCAMocks(t, nil, nil)
	missing := filepath.Join(t.TempDir(), "absent")

	for _, args := range [][]string{
		{"sign-csr", "--csr", missing},
		{"tcg", "ek", "--common-name", "x", "--public-key", missing},
		{"tcg", "enroll", "--common-name", "x", "--tcg-csr", missing},
	} {
		_, err := runTrustedCACLI(t, args...)
		var readErr *TrustedCAReadError
		require.ErrorAs(t, err, &readErr, "%v", args)
		assert.Equal(t, missing, readErr.Path)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "read "+missing)
	}

	notPEM := writeTrustedCAFile(t, "pub.txt", []byte("not pem"))
	_, err := runTrustedCACLI(t, "tcg", "ak", "--common-name", "x", "--public-key", notPEM)
	var pemErr *TrustedCAPEMError
	require.ErrorAs(t, err, &pemErr)
	assert.Contains(t, err.Error(), "holds no PEM block")
}

func TestTrustedCA_FactoryErrorPropagates(t *testing.T) {
	withTrustedCAMocks(t, nil, nil)
	pubPath, _ := publicKeyPEMFile(t)
	csrPath := writeTrustedCAFile(t, "x", []byte{1})

	for _, args := range [][]string{
		{"bundle"},
		{"certificate"},
		{"sign-csr", "--csr", csrPath},
		{"issue", "--common-name", "x"},
		{"revoke", "01"},
		{"is-revoked", "01"},
		{"crl"},
		{"tcg", "ek", "--common-name", "x", "--public-key", pubPath},
		{"tcg", "ak", "--common-name", "x", "--public-key", pubPath},
		{"tcg", "sign-csr", "--common-name", "x", "--tcg-csr", csrPath},
		{"tcg", "enroll", "--common-name", "x", "--tcg-csr", csrPath},
	} {
		_, err := runTrustedCACLI(t, args...)
		require.ErrorIs(t, err, errTrustedCAFactoryUnreachable, "%v", args)
	}
}

func TestTrustedCA_ServiceErrorPropagates(t *testing.T) {
	svcErr := errors.New("xkms ca: TCG certificates are issued in the system tenant only")
	withTrustedCAMocks(t, &mockTrustedCAService{err: svcErr}, &mockTCGCAService{err: svcErr})
	pubPath, _ := publicKeyPEMFile(t)
	csrPath := writeTrustedCAFile(t, "x", []byte{1})

	for _, args := range [][]string{
		{"bundle"},
		{"certificate"},
		{"sign-csr", "--csr", csrPath},
		{"issue", "--common-name", "x"},
		{"revoke", "01"},
		{"is-revoked", "01"},
		{"crl"},
		{"tcg", "ek", "--common-name", "x", "--public-key", pubPath},
		{"tcg", "ak", "--common-name", "x", "--public-key", pubPath},
		{"tcg", "sign-csr", "--common-name", "x", "--tcg-csr", csrPath},
		{"tcg", "enroll", "--common-name", "x", "--tcg-csr", csrPath},
	} {
		_, err := runTrustedCACLI(t, args...)
		require.ErrorIs(t, err, svcErr, "%v", args)
	}
}

func TestTrustedCA_SDKFactories(t *testing.T) {
	sdkErr := errors.New("no transport")
	withFakeSDKFactory(t, nil, sdkErr)

	_, err := createSDKTrustedCAClient()
	require.ErrorIs(t, err, sdkErr)
	_, err = createSDKTCGCAClient()
	require.ErrorIs(t, err, sdkErr)
}

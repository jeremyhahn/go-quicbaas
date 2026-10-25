package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	errSANTestCA             = errors.New("san test: ca failure")
	errSANTestAlreadyRevoked = errors.New("san test: certificate already revoked")
	errSANTestNotFound       = errors.New("san test: storage: not found")
)

// sanCAMock is a TrustedCA whose issuance and revocation mirror go-xkms: a
// certificate is stored by Common Name, issuing over an existing one fails
// with ErrCertificateAlreadyExists, and revoking a certificate twice fails.
type sanCAMock struct {
	xkmssdk.TrustedCA
	certs        *sanCertStoreMock
	keys         *sanKeyStoreMock
	caCert       *x509.Certificate
	caCertErr    error
	issueErr     error
	revokeErr    error
	isRevokedErr error
	issued       int
	revoked      []*x509.Certificate
	checked      []*big.Int
}

func (m *sanCAMock) IssueCertificate(_ context.Context, req *xkmssdk.TrustedCACertificateRequest) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	if m.issueErr != nil {
		return nil, m.issueErr
	}
	if _, exists := m.certs.certs[req.Subject.CommonName]; exists {
		return nil, xkmssdk.ErrCertificateAlreadyExists
	}
	cert := newSANTestLeaf(m.certs.t, req.Subject.CommonName, req.SANS)
	m.certs.certs[req.Subject.CommonName] = cert
	m.issued++
	return &xkmssdk.TrustedCAIssuedCertificate{Certificate: cert, SerialNumber: cert.SerialNumber}, nil
}

func (m *sanCAMock) RevokeCert(_ context.Context, cert *x509.Certificate, _ xkmssdk.RevokeOptions) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	if m.isRevoked(cert.SerialNumber) {
		return errSANTestAlreadyRevoked
	}
	m.revoked = append(m.revoked, cert)
	return nil
}

func (m *sanCAMock) IsRevoked(serial *big.Int) (bool, error) {
	m.checked = append(m.checked, serial)
	if m.isRevokedErr != nil {
		return false, m.isRevokedErr
	}
	return m.isRevoked(serial), nil
}

func (m *sanCAMock) isRevoked(serial *big.Int) bool {
	return slices.ContainsFunc(m.revoked, func(c *x509.Certificate) bool {
		return c.SerialNumber.Cmp(serial) == 0
	})
}

func (m *sanCAMock) CertStore() xkmssdk.CertStore { return m.certs }

func (m *sanCAMock) KeyStore() xkmssdk.ServiceBackend { return m.keys }

func (m *sanCAMock) CACertificate() (*x509.Certificate, error) {
	return m.caCert, m.caCertErr
}

// ServerTLSConfig mirrors go-xkms: the served certificate comes from the
// keystore by Common Name, never from the CertStore.
func (m *sanCAMock) ServerTLSConfig(_ context.Context, attrs *xkmssdk.KeyAttributes, _ bool) (*tls.Config, error) {
	chain, ok := m.keys.chains[attrs.CN]
	if !ok {
		return nil, errSANTestNotFound
	}
	served := tls.Certificate{Leaf: chain[0]}
	for _, cert := range chain {
		served.Certificate = append(served.Certificate, cert.Raw)
	}
	return &tls.Config{Certificates: []tls.Certificate{served}}, nil
}

// sanKeyStoreMock is a keystore whose certificate storage is keyed by
// Common Name, recording the order of writes.
type sanKeyStoreMock struct {
	xkmssdk.ServiceBackend
	certs    map[string]*x509.Certificate
	chains   map[string][]*x509.Certificate
	writes   []string
	chainErr error
	certErr  error
}

func newSANKeyStoreMock() *sanKeyStoreMock {
	return &sanKeyStoreMock{certs: map[string]*x509.Certificate{}, chains: map[string][]*x509.Certificate{}}
}

func (k *sanKeyStoreMock) SaveCertChain(_ context.Context, keyID string, chain []*x509.Certificate) error {
	if k.chainErr != nil {
		return k.chainErr
	}
	k.chains[keyID] = chain
	k.writes = append(k.writes, "chain")
	return nil
}

func (k *sanKeyStoreMock) SaveCert(_ context.Context, keyID string, cert *x509.Certificate) error {
	if k.certErr != nil {
		return k.certErr
	}
	k.certs[keyID] = cert
	k.writes = append(k.writes, "cert")
	return nil
}

// newSANCAMock returns a sanCAMock with empty stores and a CA certificate.
func newSANCAMock(t *testing.T) *sanCAMock {
	return &sanCAMock{
		certs:  newSANCertStoreMock(t),
		keys:   newSANKeyStoreMock(),
		caCert: newSANTestLeaf(t, "Test Root CA", &xkmssdk.TrustedCASubjectAlternativeNames{}),
	}
}

// sanCertStoreMock is a CertStore keyed by Common Name.
type sanCertStoreMock struct {
	xkmssdk.CertStore
	t         *testing.T
	certs     map[string]*x509.Certificate
	getErr    error
	deleteErr error
}

func newSANCertStoreMock(t *testing.T) *sanCertStoreMock {
	return &sanCertStoreMock{t: t, certs: map[string]*x509.Certificate{}}
}

func (s *sanCertStoreMock) GetCertificate(cn string) (*x509.Certificate, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	cert, ok := s.certs[cn]
	if !ok {
		return nil, errSANTestCA
	}
	return cert, nil
}

func (s *sanCertStoreMock) DeleteCertificate(cn string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.certs, cn)
	return nil
}

// newSANTestLeaf returns a self-signed certificate carrying sans.
func newSANTestLeaf(t *testing.T, cn string, sans *xkmssdk.TrustedCASubjectAlternativeNames) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     sans.DNS,
	}
	for _, entry := range sans.IPs {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(entry))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func newSANTestServer(t *testing.T) *Server {
	return &Server{config: &config.Config{}, logger: buildTestLogger(t)}
}

func sanRequest(cn string, sans *xkmssdk.TrustedCASubjectAlternativeNames) *xkmssdk.TrustedCACertificateRequest {
	return &xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: cn},
		SANS:    sans,
	}
}

func countFold(list []string, want string) int {
	n := 0
	for _, entry := range list {
		if strings.EqualFold(entry, want) {
			n++
		}
	}
	return n
}

func TestServerCertSANs_HostnameNamedThreeWaysIsOneEntry(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)
	if hostname == "" || hostname == "localhost" || net.ParseIP(hostname) != nil {
		hostname = "node-1.example.test"
	}
	s := newSANTestServer(t)
	s.config.Server.Host = hostname
	s.config.Server.AdditionalSANDNS = []string{hostname, strings.ToUpper(hostname), "localhost"}

	sans := s.serverCertSANs()

	assert.Equal(t, 1, countFold(sans.DNS, hostname))
	assert.Equal(t, 1, countFold(sans.DNS, "localhost"))
	assert.Equal(t, "localhost", sans.DNS[0], "order is first-seen")
}

func TestServerCertSANs_LoopbackLongFormsAreNotDuplicated(t *testing.T) {
	s := newSANTestServer(t)
	s.config.Server.Host = "127.0.0.1"
	s.config.Server.AdditionalSANIPs = []string{"127.0.0.1", "0:0:0:0:0:0:0:1", "::ffff:127.0.0.1"}

	sans := s.serverCertSANs()

	assert.Equal(t, []string{"127.0.0.1", "::1"}, sans.IPs)
}

func TestServerCertSANs_KeepsOperatorEntriesAndRoutesIPs(t *testing.T) {
	s := newSANTestServer(t)
	s.config.Server.Host = "0.0.0.0"
	s.config.Server.AdditionalSANDNS = []string{"api.example.test", "192.0.2.7", ""}
	s.config.Server.AdditionalSANIPs = []string{"2001:DB8::1", "not-an-ip", "not-an-ip"}

	sans := s.serverCertSANs()

	assert.Contains(t, sans.DNS, "api.example.test")
	assert.NotContains(t, sans.DNS, "192.0.2.7", "an IP-looking DNS entry is an IP SAN")
	assert.NotContains(t, sans.DNS, "", "empty entries are not SANs")
	assert.NotContains(t, sans.DNS, "0.0.0.0", "the wildcard bind address is not a SAN")
	assert.Equal(t, []string{"127.0.0.1", "::1", "192.0.2.7", "2001:db8::1", "not-an-ip"}, sans.IPs,
		"an unparseable IP is kept for the validator, never silently dropped")
}

func TestMissingSANs_CoveredIgnoresCaseAndIPSpelling(t *testing.T) {
	cert := newSANTestLeaf(t, "localhost", &xkmssdk.TrustedCASubjectAlternativeNames{
		DNS: []string{"Node-1.Example.Test"},
		IPs: []string{"::1"},
	})
	want := &xkmssdk.TrustedCASubjectAlternativeNames{
		DNS: []string{"node-1.example.test"},
		IPs: []string{"0:0:0:0:0:0:0:1"},
	}

	assert.Empty(t, missingSANs(cert, want))
}

func TestMissingSANs_ReportsAbsentAndUnparseableEntries(t *testing.T) {
	cert := newSANTestLeaf(t, "localhost", &xkmssdk.TrustedCASubjectAlternativeNames{
		DNS: []string{"localhost"},
		IPs: []string{"127.0.0.1"},
	})
	want := &xkmssdk.TrustedCASubjectAlternativeNames{
		DNS: []string{"localhost", "api.example.test"},
		IPs: []string{"127.0.0.1", "192.0.2.10", "not-an-ip"},
	}

	assert.Equal(t, []string{"api.example.test", "192.0.2.10", "not-an-ip"}, missingSANs(cert, want))
}

func TestIssueServerCert_FirstIssue(t *testing.T) {
	ca := newSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.NoError(t, err)
	require.NotNil(t, issued)
	assert.Equal(t, 1, ca.issued)
}

func TestIssueServerCert_CoveringCertIsKept(t *testing.T) {
	ca := newSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}, IPs: []string{"127.0.0.1"}}
	existing := newSANTestLeaf(t, "localhost", sans)
	ca.certs.certs["localhost"] = existing

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.NoError(t, err)
	assert.Nil(t, issued)
	assert.Zero(t, ca.issued)
	assert.Empty(t, ca.revoked)
	assert.Same(t, existing, ca.certs.certs["localhost"])
}

// A previous boot may have issued the covering certificate and then failed
// to store it; the next boot writes the existing leaf into the keystore.
func TestIssueServerCert_CoveringCertIsSyncedToEmptyKeyStore(t *testing.T) {
	ca := newSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	existing := newSANTestLeaf(t, "localhost", sans)
	ca.certs.certs["localhost"] = existing

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.NoError(t, err)
	assert.Nil(t, issued)
	assert.Zero(t, ca.issued)
	assert.Same(t, existing, ca.keys.certs["localhost"])
	require.Len(t, ca.keys.chains["localhost"], 2)
	assert.Same(t, existing, ca.keys.chains["localhost"][0])
	assert.Same(t, ca.caCert, ca.keys.chains["localhost"][1])
}

func TestIssueServerCert_CoveringCertKeyStoreWriteFails(t *testing.T) {
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	tests := []struct {
		name  string
		setup func(ca *sanCAMock)
	}{
		{"CA certificate unreadable", func(ca *sanCAMock) { ca.caCertErr = errSANTestCA }},
		{"keystore chain write fails", func(ca *sanCAMock) { ca.keys.chainErr = errSANTestCA }},
		{"keystore leaf write fails", func(ca *sanCAMock) { ca.keys.certErr = errSANTestCA }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca := newSANCAMock(t)
			ca.certs.certs["localhost"] = newSANTestLeaf(t, "localhost", sans)
			tt.setup(ca)

			issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

			require.ErrorIs(t, err, errSANTestCA)
			var typed *dberrors.QBError
			require.ErrorAs(t, err, &typed)
			assert.Nil(t, issued)
			assert.Zero(t, ca.issued)
		})
	}
}

func TestIssueServerCert_CoveringCertAlreadyInKeyStore(t *testing.T) {
	ca := newSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	existing := newSANTestLeaf(t, "localhost", sans)
	ca.certs.certs["localhost"] = existing
	ca.keys.certs["localhost"] = existing
	ca.keys.chains["localhost"] = []*x509.Certificate{existing, ca.caCert}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.NoError(t, err)
	assert.Nil(t, issued)
	assert.Zero(t, ca.issued)
	assert.Same(t, existing, ca.keys.certs["localhost"])
	assert.Same(t, existing, ca.keys.chains["localhost"][0])
}

func TestIssueServerCert_MissingSANReissues(t *testing.T) {
	ca := newSANCAMock(t)
	old := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}, IPs: []string{"127.0.0.1"}}
	existing := newSANTestLeaf(t, "localhost", old)
	ca.certs.certs["localhost"] = existing
	wanted := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}, IPs: []string{"127.0.0.1", "192.0.2.10"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", wanted))

	require.NoError(t, err)
	require.NotNil(t, issued)
	assert.Equal(t, []*x509.Certificate{existing}, ca.revoked, "the superseded cert is revoked")
	assert.Empty(t, missingSANs(issued.Certificate, wanted))
	assert.Same(t, issued.Certificate, ca.certs.certs["localhost"])
}

func TestIssueServerCert_Errors(t *testing.T) {
	old := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	wanted := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost", "api.example.test"}}
	tests := []struct {
		name  string
		setup func(ca *sanCAMock)
	}{
		{"issue fails", func(ca *sanCAMock) { ca.issueErr = errSANTestCA }},
		{"existing cert unreadable", func(ca *sanCAMock) { ca.certs.getErr = errSANTestCA }},
		{"revocation check fails", func(ca *sanCAMock) { ca.isRevokedErr = errSANTestCA }},
		{"revoke fails", func(ca *sanCAMock) { ca.revokeErr = errSANTestCA }},
		{"delete fails", func(ca *sanCAMock) { ca.certs.deleteErr = errSANTestCA }},
		{"CA certificate unreadable", func(ca *sanCAMock) { ca.caCertErr = errSANTestCA }},
		{"keystore chain write fails", func(ca *sanCAMock) { ca.keys.chainErr = errSANTestCA }},
		{"keystore leaf write fails", func(ca *sanCAMock) { ca.keys.certErr = errSANTestCA }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca := newSANCAMock(t)
			ca.certs.certs["localhost"] = newSANTestLeaf(t, "localhost", old)
			tt.setup(ca)

			issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", wanted))

			require.ErrorIs(t, err, errSANTestCA)
			var typed *dberrors.QBError
			require.ErrorAs(t, err, &typed)
			assert.Nil(t, issued)
		})
	}
}

// A boot that revoked the stored certificate but did not delete it leaves a
// revoked certificate behind. The next boot must replace it rather than ask
// the CA to revoke it a second time.
func TestIssueServerCert_AlreadyRevokedCertIsReplaced(t *testing.T) {
	ca := newSANCAMock(t)
	old := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	existing := newSANTestLeaf(t, "localhost", old)
	ca.certs.certs["localhost"] = existing
	ca.revoked = []*x509.Certificate{existing}
	wanted := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost", "api.example.test"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", wanted))

	require.NoError(t, err)
	require.NotNil(t, issued)
	assert.Equal(t, []*x509.Certificate{existing}, ca.revoked, "the cert is not revoked twice")
	assert.Contains(t, issued.Certificate.DNSNames, "api.example.test")
	assert.Same(t, issued.Certificate, ca.certs.certs["localhost"])
}

// A live stored certificate whose revocation fails stays stored, and boot
// fails, after the CA has been asked whether that serial is already revoked.
func TestIssueServerCert_RevokeFailureKeepsLiveCert(t *testing.T) {
	ca := newSANCAMock(t)
	ca.revokeErr = errSANTestCA
	old := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	existing := newSANTestLeaf(t, "localhost", old)
	ca.certs.certs["localhost"] = existing
	wanted := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost", "api.example.test"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", wanted))

	require.ErrorIs(t, err, errSANTestCA)
	var typed *dberrors.QBError
	require.ErrorAs(t, err, &typed)
	assert.Nil(t, issued)
	assert.Equal(t, []*big.Int{existing.SerialNumber}, ca.checked)
	assert.Same(t, existing, ca.certs.certs["localhost"])
}

// go-xkms IssueCertificate stores the leaf only in the CertStore, while
// ServerTLSConfig reads the keystore. A first issue must leave the leaf and
// its chain where ServerTLSConfig looks, chain first.
func TestIssueServerCert_FirstIssueIsServedByServerTLSConfig(t *testing.T) {
	ca := newSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)

	tlsConfig, err := ca.ServerTLSConfig(context.Background(), &xkmssdk.KeyAttributes{CN: "localhost"}, false)
	require.NoError(t, err)
	require.Len(t, tlsConfig.Certificates, 1)
	assert.Same(t, issued.Certificate, tlsConfig.Certificates[0].Leaf)
	assert.Equal(t, [][]byte{issued.Certificate.Raw, ca.caCert.Raw}, tlsConfig.Certificates[0].Certificate)
	assert.Equal(t, []string{"chain", "cert"}, ca.keys.writes)
}

// A re-issue replaces the keystore copy too, so the served certificate
// carries the newly configured SANs.
func TestIssueServerCert_ReissueIsServedByServerTLSConfig(t *testing.T) {
	ca := newSANCAMock(t)
	old := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	stale := newSANTestLeaf(t, "localhost", old)
	ca.certs.certs["localhost"] = stale
	ca.keys.chains["localhost"] = []*x509.Certificate{stale, ca.caCert}
	wanted := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost", "api.example.test"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", wanted))
	require.NoError(t, err)

	tlsConfig, err := ca.ServerTLSConfig(context.Background(), &xkmssdk.KeyAttributes{CN: "localhost"}, false)
	require.NoError(t, err)
	assert.Same(t, issued.Certificate, tlsConfig.Certificates[0].Leaf)
	assert.Same(t, issued.Certificate, ca.keys.certs["localhost"])
	assert.Contains(t, tlsConfig.Certificates[0].Leaf.DNSNames, "api.example.test")
}

// A CA that reports no certificate and no error cannot chain the leaf, and
// nothing reaches the keystore.
func TestIssueServerCert_NilCACertificateFails(t *testing.T) {
	ca := newSANCAMock(t)
	ca.caCert = nil
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}

	issued, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.ErrorIs(t, err, ErrServerCertChainCAMissing)
	assert.Nil(t, issued)
	assert.Empty(t, ca.keys.writes)
}

func TestIssueTLSCertFromExternalCA_CarriesAdditionalSANs(t *testing.T) {
	s := newSANTestServer(t)
	s.config.Server.AdditionalSANIPs = []string{"192.0.2.10"}
	ca := newSANCAMock(t)
	ca.certs.certs["localhost"] = newSANTestLeaf(t, "localhost",
		&xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}})
	s.xkmsCA = ca

	require.NoError(t, s.issueTLSCertFromExternalCA())

	assert.Len(t, ca.revoked, 1)
	assert.Empty(t, missingSANs(ca.certs.certs["localhost"], s.serverCertSANs()))
}

// servedLeaf returns the certificate the listener's tls.Config serves.
func servedLeaf(t *testing.T, cfg *tls.Config) *x509.Certificate {
	t.Helper()
	require.NotNil(t, cfg)
	var cert *tls.Certificate
	if len(cfg.Certificates) > 0 {
		cert = &cfg.Certificates[0]
	} else {
		require.NotNil(t, cfg.GetCertificate, "tls.Config serves no certificate")
		var err error
		cert, err = cfg.GetCertificate(&tls.ClientHelloInfo{})
		require.NoError(t, err)
	}
	require.NotEmpty(t, cert.Certificate)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	return leaf
}

// diskLeaf returns the first certificate in the server.crt written to disk.
func diskLeaf(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	block, _ := pem.Decode(data)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf
}

// restartCA runs initializeCA again as a freshly loaded configuration would:
// the cert paths are unset, so they default to the CA-issued files.
func restartCA(t *testing.T, srv *Server) {
	t.Helper()
	srv.config.Security.TLSCertFile = ""
	srv.config.Security.TLSKeyFile = ""
	srv.config.Security.TLSCAFile = ""
	require.NoError(t, srv.initializeCA())
}

func TestInitializeCA_RestartWithCoveredSANs_KeepsCert(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.Server.AdditionalSANIPs = []string{"192.0.2.10"}
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initializeCA())
	first := servedLeaf(t, srv.tlsConfig)

	restartCA(t, srv)

	assert.Equal(t, first.Raw, servedLeaf(t, srv.tlsConfig).Raw, "a covering cert is not re-issued")
	assert.Equal(t, first.Raw, diskLeaf(t, cfg.Security.TLSCertFile).Raw)
}

func TestInitializeCA_RestartWithAddedIP_ServesReissuedCert(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initializeCA())
	first := servedLeaf(t, srv.tlsConfig)
	added := net.ParseIP("192.0.2.10")
	require.NotContains(t, first.IPAddresses, added)

	cfg.Server.AdditionalSANIPs = []string{"192.0.2.10"}
	restartCA(t, srv)

	served := servedLeaf(t, srv.tlsConfig)
	assert.True(t, slices.ContainsFunc(served.IPAddresses, added.Equal), "served cert carries the added IP")
	assert.True(t, slices.ContainsFunc(diskLeaf(t, cfg.Security.TLSCertFile).IPAddresses, added.Equal), "server.crt carries the added IP")
	assert.NotEqual(t, first.SerialNumber, served.SerialNumber)
	assert.Equal(t, first.RawSubjectPublicKeyInfo, served.RawSubjectPublicKeyInfo, "the existing key is kept")
}

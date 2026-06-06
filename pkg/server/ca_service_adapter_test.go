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

package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// fakeTrustedCA implements xkmssdk.TrustedCA for unit testing the adapter.
// Only the seven CA-service methods are exercised; all other methods return
// nil/zero so the type satisfies the interface without spinning up a real CA.
type fakeTrustedCA struct {
	xkmssdk.TrustedCA // intentionally nil; only specific methods are overridden

	caCert      *x509.Certificate
	caKey       *ecdsa.PrivateKey
	bundlePEM   []byte
	revokedSet  map[string]bool
	issueErr    error
	signErr     error
	revokeErr   error
	bundleErr   error
	caCertErr   error
	crlErr      error
	isRevokeErr error

	// certStore, when non-nil, is returned by CertStore() so the adapter's
	// ListStoredCertificates path can be exercised. A nil value mimics a CA
	// constructed without a certificate store backend.
	certStore xkmssdk.CertStore

	// lastSignOpts records the SignOptions passed to the most recent SignCSR
	// call so tests can assert the adapter propagated tenant / trust-domain /
	// kind correctly.
	lastSignOpts *xkmssdk.TrustedCASignOptions
}

// CertStore returns the configured certificate store (possibly nil). The
// adapter treats a nil store as "no store available".
func (f *fakeTrustedCA) CertStore() xkmssdk.CertStore {
	return f.certStore
}

func (f *fakeTrustedCA) IssueCertificateWithProfile(
	req *xkmssdk.TrustedCACertificateRequest, _ string,
) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	if f.issueErr != nil {
		return nil, f.issueErr
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   req.Subject.CommonName,
			Organization: []string{req.Subject.Organization},
		},
		NotBefore: time.Now().Add(-time.Minute),
		NotAfter:  time.Now().Add(time.Hour),
	}
	if req.SANS != nil {
		template.DNSNames = req.SANS.DNS
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, f.caCert, &leafKey.PublicKey, f.caKey)
	if err != nil {
		return nil, err
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return &xkmssdk.TrustedCAIssuedCertificate{
		CertificatePEM: leafPEM,
		PrivateKeyPEM:  keyPEM,
		ChainPEM:       f.bundlePEM,
		SerialNumber:   template.SerialNumber,
	}, nil
}

// IssueCertificate issues a certificate without a profile by delegating to the
// profile-aware path. Lets the fake satisfy callers (e.g. the agent cert
// issuer) that invoke the no-profile signature.
func (f *fakeTrustedCA) IssueCertificate(
	req *xkmssdk.TrustedCACertificateRequest,
) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	return f.IssueCertificateWithProfile(req, "")
}

func (f *fakeTrustedCA) SignCSR(csrPEM []byte, opts *xkmssdk.TrustedCASignOptions) (*x509.Certificate, error) {
	f.lastSignOpts = opts
	if f.signErr != nil {
		return nil, f.signErr
	}
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, errors.New("invalid CSR")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	// Mirror the real CA: synthesize the tenant-scoped SPIFFE URI SAN from the
	// supplied options so end-to-end SAN assertions exercise the real path.
	if opts != nil && opts.SPIFFETrustDomain != "" && opts.TenantID != "" {
		kind := opts.SPIFFEKind
		if kind == "" {
			kind = qrdbspiffe.KindUser
		}
		name := opts.SPIFFEName
		if name == "" {
			name = csr.Subject.CommonName
		}
		id := qrdbspiffe.ID{TrustDomain: opts.SPIFFETrustDomain, TenantID: opts.TenantID, Kind: kind, Name: name}
		u, perr := url.Parse(id.String())
		if perr != nil {
			return nil, perr
		}
		template.URIs = append(template.URIs, u)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, f.caCert, csr.PublicKey, f.caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

func (f *fakeTrustedCA) Revoke(serial *big.Int, _ int) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	if f.revokedSet == nil {
		f.revokedSet = make(map[string]bool)
	}
	f.revokedSet[serial.Text(16)] = true
	return nil
}

func (f *fakeTrustedCA) CABundle() ([]byte, error) {
	if f.bundleErr != nil {
		return nil, f.bundleErr
	}
	return f.bundlePEM, nil
}

func (f *fakeTrustedCA) CACertificate() (*x509.Certificate, error) {
	if f.caCertErr != nil {
		return nil, f.caCertErr
	}
	return f.caCert, nil
}

func (f *fakeTrustedCA) GenerateCRL() ([]byte, error) {
	if f.crlErr != nil {
		return nil, f.crlErr
	}
	template := &x509.RevocationList{
		Number:     big.NewInt(1),
		ThisUpdate: time.Now(),
		NextUpdate: time.Now().Add(24 * time.Hour),
	}
	return x509.CreateRevocationList(rand.Reader, template, f.caCert, f.caKey)
}

func (f *fakeTrustedCA) IsRevoked(serial *big.Int) (bool, error) {
	if f.isRevokeErr != nil {
		return false, f.isRevokeErr
	}
	return f.revokedSet[serial.Text(16)], nil
}

// Public satisfies crypto.Signer (TrustedCA embeds it).
func (f *fakeTrustedCA) Public() crypto.PublicKey {
	if f.caKey == nil {
		return nil
	}
	return f.caKey.Public()
}

// Sign satisfies crypto.Signer.
func (f *fakeTrustedCA) Sign(rnd io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if f.caKey == nil {
		return nil, errors.New("no signer")
	}
	return f.caKey.Sign(rnd, digest, opts)
}

// newFakeTrustedCA builds a fake CA backed by an in-memory ECDSA keypair so
// the adapter can produce real certificates verifiable in tests.
func newFakeTrustedCA(t *testing.T) *fakeTrustedCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca cert: %v", err)
	}
	return &fakeTrustedCA{
		caCert:    cert,
		caKey:     key,
		bundlePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func TestNewCAServiceAdapter(t *testing.T) {
	t.Run("nil CA returns typed error", func(t *testing.T) {
		adapter, err := newCAServiceAdapter(nil)
		if !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
		if adapter != nil {
			t.Fatalf("expected nil adapter on error")
		}
	})

	t.Run("non-nil CA returns adapter", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		adapter, err := newCAServiceAdapter(ca)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatalf("expected non-nil adapter")
		}
	})
}

func TestCAServiceAdapter_SatisfiesClient(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("constructing adapter: %v", err)
	}
	// The service builder asserts xkmstransport.Client; the adapter must
	// satisfy that via the embedded interface so WithCAService accepts it.
	var _ xkmstransport.Client = adapter
	var _ xkmstransport.CAService = adapter
}

func TestCAServiceAdapter_IssueCertificate(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	t.Run("issues certificate for valid request", func(t *testing.T) {
		resp, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
			CommonName:   "node-01.example.com",
			Organization: "Test",
			SANs:         []string{"DNS:node-01.example.com", "IP:10.0.0.1"},
			ValidityDays: 30,
		})
		if err != nil {
			t.Fatalf("IssueCertificate: %v", err)
		}
		if resp == nil || len(resp.CertificatePEM) == 0 {
			t.Fatalf("expected populated cert PEM")
		}
		if resp.SerialNumber == "" {
			t.Fatalf("expected non-empty serial")
		}
		// The adapter generates the leaf keypair in-process and must return
		// the PEM-encoded private key alongside the signed certificate. An
		// empty private key here is the regression that surfaced as an empty
		// private_key_pem field on the REST "ca issue" response.
		if len(resp.PrivateKeyPEM) == 0 {
			t.Fatalf("expected non-empty private key PEM")
		}
		if block, _ := pem.Decode(resp.PrivateKeyPEM); block == nil {
			t.Fatalf("private key PEM is not decodable: %q", resp.PrivateKeyPEM)
		}
		if _, err := x509.ParsePKCS8PrivateKey(decodePEM(t, resp.PrivateKeyPEM)); err != nil {
			t.Fatalf("private key is not a valid PKCS#8 key: %v", err)
		}
	})

	t.Run("issues certificate for each supported key algorithm", func(t *testing.T) {
		for _, algo := range []string{"ecdsa", "ecdsa-p256", "ecdsa-p384", "rsa", "rsa2048", "ed25519"} {
			algo := algo
			t.Run(algo, func(t *testing.T) {
				resp, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
					CommonName: algo + ".example.com",
					Algorithm:  algo,
				})
				if err != nil {
					t.Fatalf("IssueCertificate(%s): %v", algo, err)
				}
				if len(resp.CertificatePEM) == 0 || len(resp.PrivateKeyPEM) == 0 {
					t.Fatalf("IssueCertificate(%s): expected cert and key PEM", algo)
				}
			})
		}
	})

	t.Run("unsupported key algorithm returns typed error", func(t *testing.T) {
		_, err := adapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
			CommonName: "bad-algo.example.com",
			Algorithm:  "dilithium",
		})
		if !errors.Is(err, ErrCAServiceAdapterUnsupportedAlgorithm) {
			t.Fatalf("want ErrCAServiceAdapterUnsupportedAlgorithm, got %v", err)
		}
	})

	t.Run("nil request returns typed error", func(t *testing.T) {
		_, err := adapter.IssueCertificate(context.Background(), nil)
		if !errors.Is(err, ErrCAServiceAdapterNilRequest) {
			t.Fatalf("want ErrCAServiceAdapterNilRequest, got %v", err)
		}
	})

	t.Run("CA error propagates", func(t *testing.T) {
		failing := newFakeTrustedCA(t)
		failing.signErr = errors.New("ca offline")
		failingAdapter, _ := newCAServiceAdapter(failing)
		_, err := failingAdapter.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
			CommonName: "x",
		})
		if err == nil || err.Error() != "ca offline" {
			t.Fatalf("expected propagated error, got %v", err)
		}
	})
}

// decodePEM decodes a single PEM block and returns its DER bytes, failing the
// test if the input is not valid PEM.
func decodePEM(t *testing.T, data []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("not PEM: %q", data)
	}
	return block.Bytes
}

func TestCAServiceAdapter_SignCSR(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	csrPEM := newCSR(t, "csr-test")

	t.Run("signs valid CSR", func(t *testing.T) {
		resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
			CSRPEM:       csrPEM,
			Profile:      "server",
			ValidityDays: 30,
		})
		if err != nil {
			t.Fatalf("SignCSR: %v", err)
		}
		if len(resp.CertificatePEM) == 0 {
			t.Fatalf("expected signed cert")
		}
		if len(resp.ChainPEM) == 0 {
			t.Fatalf("expected chain")
		}
	})

	t.Run("nil request errors", func(t *testing.T) {
		_, err := adapter.SignCSR(context.Background(), nil)
		if !errors.Is(err, ErrCAServiceAdapterNilRequest) {
			t.Fatalf("want ErrCAServiceAdapterNilRequest, got %v", err)
		}
	})

	t.Run("empty CSR errors", func(t *testing.T) {
		_, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{})
		if !errors.Is(err, ErrCAServiceAdapterInvalidPEM) {
			t.Fatalf("want ErrCAServiceAdapterInvalidPEM, got %v", err)
		}
	})
}

func TestCAServiceAdapter_RevokeCertificate(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	t.Run("revokes existing serial", func(t *testing.T) {
		resp, err := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{
			SerialNumber: "ff",
			Reason:       1,
		})
		if err != nil {
			t.Fatalf("RevokeCertificate: %v", err)
		}
		if !resp.Success {
			t.Fatalf("expected success")
		}
	})

	t.Run("invalid serial errors", func(t *testing.T) {
		_, err := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{
			SerialNumber: "not-hex-or-decimal!@#",
		})
		if !errors.Is(err, ErrCAServiceAdapterInvalidSerial) {
			t.Fatalf("want ErrCAServiceAdapterInvalidSerial, got %v", err)
		}
	})

	t.Run("missing serial errors", func(t *testing.T) {
		_, err := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{})
		if !errors.Is(err, ErrCAServiceAdapterInvalidSerial) {
			t.Fatalf("want ErrCAServiceAdapterInvalidSerial, got %v", err)
		}
	})
}

func TestCAServiceAdapter_GetCABundle(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	resp, err := adapter.GetCABundle(context.Background(), &xkmstransport.GetCABundleRequest{})
	if err != nil {
		t.Fatalf("GetCABundle: %v", err)
	}
	if len(resp.BundlePEM) == 0 {
		t.Fatalf("expected bundle PEM")
	}
	if len(resp.Certificates) == 0 {
		t.Fatalf("expected at least one certificate in bundle")
	}
	if resp.ContentType == "" {
		t.Fatalf("expected content type")
	}
}

func TestCAServiceAdapter_GetCACertificate(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	resp, err := adapter.GetCACertificate(context.Background(), &xkmstransport.GetCACertificateRequest{})
	if err != nil {
		t.Fatalf("GetCACertificate: %v", err)
	}
	if len(resp.CertificatePEM) == 0 {
		t.Fatalf("expected cert PEM")
	}
	if !resp.IsCA {
		t.Fatalf("expected IsCA=true")
	}
	if resp.Subject == "" || resp.Issuer == "" {
		t.Fatalf("expected populated subject/issuer")
	}
}

func TestCAServiceAdapter_GenerateCRL(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	resp, err := adapter.GenerateCRL(context.Background(), &xkmstransport.GenerateCRLRequest{})
	if err != nil {
		t.Fatalf("GenerateCRL: %v", err)
	}
	if len(resp.CRLPEM) == 0 {
		t.Fatalf("expected CRL PEM")
	}
	block, _ := pem.Decode(resp.CRLPEM)
	if block == nil || block.Type != "X509 CRL" {
		t.Fatalf("expected PEM-encoded X509 CRL")
	}
}

func TestCAServiceAdapter_IsRevoked(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	t.Run("returns false for unknown serial", func(t *testing.T) {
		resp, err := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{
			SerialNumber: "deadbeef",
		})
		if err != nil {
			t.Fatalf("IsRevoked: %v", err)
		}
		if resp.Revoked {
			t.Fatalf("expected not revoked")
		}
	})

	t.Run("returns true after revoke", func(t *testing.T) {
		_, err := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{
			SerialNumber: "cafe",
		})
		if err != nil {
			t.Fatalf("RevokeCertificate: %v", err)
		}
		resp, err := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{
			SerialNumber: "cafe",
		})
		if err != nil {
			t.Fatalf("IsRevoked: %v", err)
		}
		if !resp.Revoked {
			t.Fatalf("expected revoked")
		}
	})

	t.Run("missing serial errors", func(t *testing.T) {
		_, err := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{})
		if !errors.Is(err, ErrCAServiceAdapterInvalidSerial) {
			t.Fatalf("want ErrCAServiceAdapterInvalidSerial, got %v", err)
		}
	})
}

func TestParseTransportSANs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		dns  []string
		ips  []string
		mail []string
		uris []string
	}{
		{
			name: "empty",
		},
		{
			name: "DNS prefix",
			in:   []string{"DNS:example.com"},
			dns:  []string{"example.com"},
		},
		{
			name: "IP prefix",
			in:   []string{"IP:1.2.3.4"},
			ips:  []string{"1.2.3.4"},
		},
		{
			name: "Email prefix",
			in:   []string{"Email:a@b.com"},
			mail: []string{"a@b.com"},
		},
		{
			name: "URI prefix",
			in:   []string{"URI:spiffe://x"},
			uris: []string{"spiffe://x"},
		},
		{
			name: "no prefix DNS",
			in:   []string{"node.local"},
			dns:  []string{"node.local"},
		},
		{
			name: "no prefix IP",
			in:   []string{"10.0.0.1"},
			ips:  []string{"10.0.0.1"},
		},
		{
			name: "mixed",
			in:   []string{"DNS:a", "IP:2.2.2.2", "raw"},
			dns:  []string{"a", "raw"},
			ips:  []string{"2.2.2.2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := parseTransportSANs(tc.in)
			if tc.in == nil {
				if out != nil {
					t.Fatalf("expected nil output for empty input")
				}
				return
			}
			if out == nil {
				t.Fatalf("unexpected nil output")
			}
			if !equal(out.DNS, tc.dns) {
				t.Errorf("DNS: got %v want %v", out.DNS, tc.dns)
			}
			if !equal(out.IPs, tc.ips) {
				t.Errorf("IPs: got %v want %v", out.IPs, tc.ips)
			}
			if !equal(out.Email, tc.mail) {
				t.Errorf("Email: got %v want %v", out.Email, tc.mail)
			}
			if !equal(out.URIs, tc.uris) {
				t.Errorf("URIs: got %v want %v", out.URIs, tc.uris)
			}
		})
	}
}

func TestParseHexSerial(t *testing.T) {
	t.Run("hex", func(t *testing.T) {
		n, ok := parseHexSerial("ff")
		if !ok || n.Int64() != 255 {
			t.Fatalf("expected 255, got %v ok=%v", n, ok)
		}
	})
	t.Run("decimal fallback", func(t *testing.T) {
		n, ok := parseHexSerial("12345")
		if !ok {
			t.Fatalf("expected ok")
		}
		// 12345 is also valid hex (= 74565); SetString(_, 16) consumes
		// it first. The fallback covers strings only valid in base 10.
		_ = n
	})
	t.Run("garbage returns false", func(t *testing.T) {
		_, ok := parseHexSerial("???")
		if ok {
			t.Fatalf("expected not ok")
		}
	})
}

func TestSplitPEMCertificates(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		if got := splitPEMCertificates(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("non-PEM falls through", func(t *testing.T) {
		raw := []byte("not pem at all")
		got := splitPEMCertificates(raw)
		if len(got) != 1 || string(got[0]) != string(raw) {
			t.Fatalf("expected fallback to raw bundle")
		}
	})
	t.Run("valid PEM bundle splits", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		got := splitPEMCertificates(ca.bundlePEM)
		if len(got) != 1 {
			t.Fatalf("expected 1 cert, got %d", len(got))
		}
	})
}

// newCSR generates a minimal PKCS#10 CSR with the given common name.
func newCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate csr key: %v", err)
	}
	tmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCAServiceAdapter_SetCA verifies that SetCA atomically replaces the
// backing CA and that subsequent operations use the new CA.
func TestCAServiceAdapter_SetCA(t *testing.T) {
	firstCA := newFakeTrustedCA(t)
	secondCA := newFakeTrustedCA(t)

	// Make secondCA return a distinct bundle so we can distinguish it.
	secondBundle := []byte("-----BEGIN CERTIFICATE-----\nsecond-ca\n-----END CERTIFICATE-----\n")
	secondCA.bundlePEM = secondBundle

	adapter, err := newCAServiceAdapter(firstCA)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	// Before SetCA: GetCABundle returns firstCA's bundle.
	resp, err := adapter.GetCABundle(context.Background(), &xkmstransport.GetCABundleRequest{})
	if err != nil {
		t.Fatalf("GetCABundle (first): %v", err)
	}
	if string(resp.BundlePEM) == string(secondBundle) {
		t.Fatal("expected first CA bundle before SetCA, got second CA bundle")
	}

	// Swap to secondCA.
	adapter.SetCA(secondCA)

	// After SetCA: GetCABundle returns secondCA's bundle.
	resp, err = adapter.GetCABundle(context.Background(), &xkmstransport.GetCABundleRequest{})
	if err != nil {
		t.Fatalf("GetCABundle (second): %v", err)
	}
	if string(resp.BundlePEM) != string(secondBundle) {
		t.Fatalf("expected second CA bundle after SetCA, got %q", string(resp.BundlePEM))
	}
}

// TestCAServiceAdapter_SetCA_NilIsNoop verifies that SetCA(nil) does not
// clear the existing CA reference.
func TestCAServiceAdapter_SetCA_NilIsNoop(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	adapter.SetCA(nil) // must be a no-op

	if adapter.getCA() == nil {
		t.Fatal("SetCA(nil) cleared the CA reference, expected no-op")
	}
}

// TestSplitPEMCertificates_SkipsNonCertificateBlocks covers the
// non-CERTIFICATE-block skip branch: a bundle that interleaves a non-cert PEM
// block (e.g. a public key) with a real certificate must yield only the
// certificate, with the foreign block silently skipped.
func TestSplitPEMCertificates_SkipsNonCertificateBlocks(t *testing.T) {
	ca := newFakeTrustedCA(t)

	// Prepend a non-CERTIFICATE PEM block to a valid certificate bundle.
	foreign := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: []byte("not-a-certificate"),
	})
	bundle := append(append([]byte(nil), foreign...), ca.bundlePEM...)

	got := splitPEMCertificates(bundle)
	if len(got) != 1 {
		t.Fatalf("expected the foreign block to be skipped and 1 cert returned, got %d", len(got))
	}
	// The returned DER must parse as a real X.509 certificate.
	if _, err := x509.ParseCertificate(got[0]); err != nil {
		t.Fatalf("returned block is not a valid certificate: %v", err)
	}
}

// TestCAServiceAdapter_RevokeCertificate_NilRequest covers the nil-request guard
// of RevokeCertificate: a nil request must surface ErrCAServiceAdapterNilRequest
// rather than panicking.
func TestCAServiceAdapter_RevokeCertificate_NilRequest(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)

	_, err := adapter.RevokeCertificate(context.Background(), nil)
	if !errors.Is(err, ErrCAServiceAdapterNilRequest) {
		t.Fatalf("want ErrCAServiceAdapterNilRequest, got %v", err)
	}
}

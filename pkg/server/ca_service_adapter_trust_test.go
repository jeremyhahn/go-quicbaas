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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// errCrossSignNotFound mimics the not-found error CrossSignedPEM returns when a
// CA has never been cross-signed. CrossSignedRootPEMs must fold this into a
// native-root-only result rather than treating it as a failure.
var errCrossSignNotFound = errors.New("ca: no cross-signed certificates")

// fakeTrustedCATrust extends the base fake with the three TrustCA-surface
// methods (TrustedRoot / CrossSignedPEM / ImportIssuingCAs) that the trust and
// ACME adapter paths exercise. Embedding the base fake keeps the seven core
// CA-service methods intact while adding the trust hooks here so the base fake
// in ca_service_adapter_test.go stays focused.
type fakeTrustedCATrust struct {
	*fakeTrustedCA

	trustedRoot    *x509.Certificate
	trustedRootErr error

	crossSignedPEM []byte
	crossSignErr   error

	importErr  error
	importCert *x509.Certificate
}

func (f *fakeTrustedCATrust) TrustedRoot() (*x509.Certificate, error) {
	return f.trustedRoot, f.trustedRootErr
}

func (f *fakeTrustedCATrust) CrossSignedPEM(_ *xkmssdk.KeyAttributes) ([]byte, error) {
	if f.crossSignErr != nil {
		return nil, f.crossSignErr
	}
	return f.crossSignedPEM, nil
}

func (f *fakeTrustedCATrust) ImportIssuingCAs(_ context.Context, cert *x509.Certificate) error {
	f.importCert = cert
	return f.importErr
}

// newFakeTrustedCATrust builds a trust-capable fake whose TrustedRoot defaults
// to the base CA certificate (single-tier hierarchy: the CA is its own root).
func newFakeTrustedCATrust(t *testing.T) *fakeTrustedCATrust {
	t.Helper()
	base := newFakeTrustedCA(t)
	return &fakeTrustedCATrust{
		fakeTrustedCA: base,
		trustedRoot:   base.caCert,
	}
}

// makeCSRPEM builds a valid PEM-encoded CSR with the given common name so the
// SignCSRPEM path can be driven end to end.
func makeCSRPEM(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// SignCSRPEM signs a valid CSR and returns the leaf + chain PEMs.
func TestCAServiceAdapter_SignCSRPEM(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	t.Run("signs valid CSR", func(t *testing.T) {
		csrPEM := makeCSRPEM(t, "leaf.example.com")
		certPEM, chainPEM, err := adapter.SignCSRPEM(context.Background(), csrPEM)
		if err != nil {
			t.Fatalf("SignCSRPEM: %v", err)
		}
		block, _ := pem.Decode(certPEM)
		if block == nil || block.Type != "CERTIFICATE" {
			t.Fatalf("expected CERTIFICATE PEM, got %q", string(certPEM))
		}
		if _, perr := x509.ParseCertificate(block.Bytes); perr != nil {
			t.Fatalf("issued cert does not parse: %v", perr)
		}
		if len(chainPEM) == 0 {
			t.Fatal("expected non-empty chain PEM")
		}
	})

	t.Run("empty CSR returns invalid-PEM error", func(t *testing.T) {
		_, _, err := adapter.SignCSRPEM(context.Background(), nil)
		if !errors.Is(err, ErrCAServiceAdapterInvalidPEM) {
			t.Fatalf("want ErrCAServiceAdapterInvalidPEM, got %v", err)
		}
	})

	t.Run("nil CA returns nil-CA error", func(t *testing.T) {
		empty, _ := newCAServiceAdapter(newFakeTrustedCA(t))
		empty.ca = nil
		_, _, err := empty.SignCSRPEM(context.Background(), makeCSRPEM(t, "x"))
		if !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
	})

	t.Run("CA sign error propagates", func(t *testing.T) {
		failing := newFakeTrustedCA(t)
		failing.signErr = errors.New("boom")
		fa, _ := newCAServiceAdapter(failing)
		_, _, err := fa.SignCSRPEM(context.Background(), makeCSRPEM(t, "x"))
		if err == nil {
			t.Fatal("expected sign error to propagate")
		}
	})
}

// CrossSignedRootPEMs returns just the native root when the CA has never been
// cross-signed, and appends recorded cross-signed roots when present.
func TestCAServiceAdapter_CrossSignedRootPEMs(t *testing.T) {
	t.Run("native root only when no cross-sign", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		ca.crossSignErr = errCrossSignNotFound
		adapter, _ := newCAServiceAdapter(ca)

		roots, err := adapter.CrossSignedRootPEMs()
		if err != nil {
			t.Fatalf("CrossSignedRootPEMs: %v", err)
		}
		if len(roots) != 1 {
			t.Fatalf("want 1 native root, got %d", len(roots))
		}
		block, _ := pem.Decode(roots[0])
		if block == nil || block.Type != "CERTIFICATE" {
			t.Fatalf("root is not a CERTIFICATE PEM")
		}
	})

	t.Run("appends cross-signed roots when present", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		// A second self-signed cert stands in for a cross-signed root.
		xRoot := buildSelfSignedRoot(t, "Cross Signer")
		ca.crossSignedPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: xRoot.Raw})
		adapter, _ := newCAServiceAdapter(ca)

		roots, err := adapter.CrossSignedRootPEMs()
		if err != nil {
			t.Fatalf("CrossSignedRootPEMs: %v", err)
		}
		if len(roots) != 2 {
			t.Fatalf("want native + cross-signed = 2 roots, got %d", len(roots))
		}
	})

	t.Run("falls back to CA cert when TrustedRoot unavailable", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		ca.trustedRoot = nil
		ca.trustedRootErr = errors.New("no root configured")
		ca.crossSignErr = errCrossSignNotFound
		adapter, _ := newCAServiceAdapter(ca)

		roots, err := adapter.CrossSignedRootPEMs()
		if err != nil {
			t.Fatalf("CrossSignedRootPEMs fallback: %v", err)
		}
		if len(roots) != 1 {
			t.Fatalf("want 1 root from CA-cert fallback, got %d", len(roots))
		}
	})

	t.Run("nil CA returns nil-CA error", func(t *testing.T) {
		adapter, _ := newCAServiceAdapter(newFakeTrustedCATrust(t))
		adapter.ca = nil
		if _, err := adapter.CrossSignedRootPEMs(); !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
	})

	t.Run("no resolvable root returns no-root error", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		ca.trustedRoot = nil
		ca.trustedRootErr = errors.New("no root")
		ca.caCert = nil
		ca.caCertErr = errors.New("no ca cert")
		adapter, _ := newCAServiceAdapter(ca)
		if _, err := adapter.CrossSignedRootPEMs(); !errors.Is(err, ErrCAServiceAdapterNoRoot) {
			t.Fatalf("want ErrCAServiceAdapterNoRoot, got %v", err)
		}
	})
}

// ImportIssuingCAs delegates to the wrapped CA and surfaces nil-CA / delegate
// errors.
func TestCAServiceAdapter_ImportIssuingCAs(t *testing.T) {
	t.Run("delegates to wrapped CA", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		adapter, _ := newCAServiceAdapter(ca)
		cert := buildSelfSignedRoot(t, "Leaf")
		if err := adapter.ImportIssuingCAs(context.Background(), cert); err != nil {
			t.Fatalf("ImportIssuingCAs: %v", err)
		}
		if ca.importCert != cert {
			t.Fatal("expected the cert to be forwarded to the wrapped CA")
		}
	})

	t.Run("delegate error propagates", func(t *testing.T) {
		ca := newFakeTrustedCATrust(t)
		ca.importErr = errors.New("aia walk failed")
		adapter, _ := newCAServiceAdapter(ca)
		if err := adapter.ImportIssuingCAs(context.Background(), buildSelfSignedRoot(t, "Leaf")); err == nil {
			t.Fatal("expected delegate error to propagate")
		}
	})

	t.Run("nil CA returns nil-CA error", func(t *testing.T) {
		adapter, _ := newCAServiceAdapter(newFakeTrustedCATrust(t))
		adapter.ca = nil
		if err := adapter.ImportIssuingCAs(context.Background(), buildSelfSignedRoot(t, "Leaf")); !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
	})
}

// buildSelfSignedRoot creates a throwaway self-signed CA certificate for tests
// that just need a parseable *x509.Certificate.
func buildSelfSignedRoot(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

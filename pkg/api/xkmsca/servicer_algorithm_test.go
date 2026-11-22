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

package xkmsca

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestXKMSCAServicer_IssueCertificateHonoursAlgorithm issues one certificate
// per requested algorithm and checks the issued certificate's public key is
// of that algorithm, not the CA's ECDSA P-256 default.
func TestXKMSCAServicer_IssueCertificateHonoursAlgorithm(t *testing.T) {
	cases := []struct {
		name      string
		algorithm string
		check     func(t *testing.T, pub any)
	}{
		{name: "default", algorithm: "", check: wantECDSA(elliptic.P256())},
		{name: "p384", algorithm: "ecdsa-p384", check: wantECDSA(elliptic.P384())},
		{name: "rsa2048", algorithm: "rsa2048", check: wantRSA(2048)},
		{name: "ed25519", algorithm: "ed25519", check: func(t *testing.T, pub any) {
			t.Helper()
			if _, ok := pub.(ed25519.PublicKey); !ok {
				t.Fatalf("public key is %T, want ed25519.PublicKey", pub)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newXKMSCAServicer(testca.New(t, "xkmsca-alg-"+tc.name+"-ca"), "")
			resp, err := a.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
				CommonName:   "alg-" + tc.name + ".example.test",
				ValidityDays: 1,
				Algorithm:    tc.algorithm,
			})
			if err != nil {
				t.Fatalf("issue certificate with algorithm %q: %v", tc.algorithm, err)
			}
			block, _ := pem.Decode(resp.CertificatePEM)
			if block == nil {
				t.Fatal("certificate PEM did not decode")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatalf("parse certificate: %v", err)
			}
			tc.check(t, cert.PublicKey)
		})
	}
}

// TestXKMSCAServicer_IssueCertificateRejectsUnknownAlgorithm refuses an
// unsupported algorithm with the typed error, before the CA is called.
func TestXKMSCAServicer_IssueCertificateRejectsUnknownAlgorithm(t *testing.T) {
	ca := &countingCA{TrustedCA: testca.New(t, "xkmsca-alg-reject-ca")}
	a := newXKMSCAServicer(ca, "")

	_, err := a.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		CommonName: "alg-reject.example.test",
		Algorithm:  "dsa",
	})
	var unsupported *api.UnsupportedKeyAlgorithmError
	if !stderrors.As(err, &unsupported) {
		t.Fatalf("expected *api.UnsupportedKeyAlgorithmError, got %T: %v", err, err)
	}
	if n := ca.calls.Load(); n != 0 {
		t.Fatalf("CA called %d times for an unsupported algorithm, want 0", n)
	}
}

func wantECDSA(curve elliptic.Curve) func(t *testing.T, pub any) {
	return func(t *testing.T, pub any) {
		t.Helper()
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			t.Fatalf("public key is %T, want *ecdsa.PublicKey", pub)
		}
		if key.Curve != curve {
			t.Fatalf("curve is %s, want %s", key.Curve.Params().Name, curve.Params().Name)
		}
	}
}

func wantRSA(bits int) func(t *testing.T, pub any) {
	return func(t *testing.T, pub any) {
		t.Helper()
		key, ok := pub.(*rsa.PublicKey)
		if !ok {
			t.Fatalf("public key is %T, want *rsa.PublicKey", pub)
		}
		if got := key.N.BitLen(); got != bits {
			t.Fatalf("RSA modulus is %d bits, want %d", got, bits)
		}
	}
}

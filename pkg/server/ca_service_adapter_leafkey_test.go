// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"testing"
)

// generateLeafKey produces the correct signer + public-key algorithm for every
// documented algorithm spelling and rejects unknown algorithms.
func TestGenerateLeafKey(t *testing.T) {
	tests := []struct {
		algorithm string
		wantAlg   x509.PublicKeyAlgorithm
		assert    func(t *testing.T, signer any)
	}{
		{"", x509.ECDSA, func(t *testing.T, s any) {
			if _, ok := s.(*ecdsa.PrivateKey); !ok {
				t.Fatalf("default should yield ECDSA, got %T", s)
			}
		}},
		{"ECDSA", x509.ECDSA, nil},
		{"p256", x509.ECDSA, nil},
		{"  ecdsa-p384  ", x509.ECDSA, func(t *testing.T, s any) {
			k := s.(*ecdsa.PrivateKey)
			if k.Curve.Params().BitSize != 384 {
				t.Fatalf("expected P-384, got bit size %d", k.Curve.Params().BitSize)
			}
		}},
		{"rsa", x509.RSA, func(t *testing.T, s any) {
			if _, ok := s.(*rsa.PrivateKey); !ok {
				t.Fatalf("expected RSA, got %T", s)
			}
		}},
		{"rsa-3072", x509.RSA, nil},
		{"rsa4096", x509.RSA, nil},
		{"ed25519", x509.Ed25519, func(t *testing.T, s any) {
			if _, ok := s.(ed25519.PrivateKey); !ok {
				t.Fatalf("expected ed25519, got %T", s)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			signer, alg, err := generateLeafKey(tt.algorithm)
			if err != nil {
				t.Fatalf("generateLeafKey(%q): %v", tt.algorithm, err)
			}
			if alg != tt.wantAlg {
				t.Fatalf("alg = %v, want %v", alg, tt.wantAlg)
			}
			if signer == nil {
				t.Fatal("signer is nil")
			}
			if tt.assert != nil {
				tt.assert(t, signer)
			}
		})
	}
}

func TestGenerateLeafKey_Unsupported(t *testing.T) {
	_, _, err := generateLeafKey("dilithium")
	if !errors.Is(err, ErrCAServiceAdapterUnsupportedAlgorithm) {
		t.Fatalf("expected unsupported-algorithm error, got %v", err)
	}
}

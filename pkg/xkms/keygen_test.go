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

package xkms

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"reflect"
	"testing"

	quicbaasapi "github.com/jeremyhahn/go-quicbaas/pkg/api"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// CanonicalKeyAlgorithm
// =============================================================================

// Every spelling the pre-#910 call sites accepted still resolves, and to the
// same algorithm, so no caller's input changed meaning.
func TestCanonicalKeyAlgorithm_AcceptedSpellings(t *testing.T) {
	tests := []struct {
		algorithm string
		want      string
	}{
		{"", KeyAlgorithmECDSAP256},
		{"   ", KeyAlgorithmECDSAP256},
		{"ecdsa", KeyAlgorithmECDSAP256},
		{"ECDSA", KeyAlgorithmECDSAP256},
		{"ecdsa-p256", KeyAlgorithmECDSAP256},
		{"ecdsap256", KeyAlgorithmECDSAP256},
		{"p256", KeyAlgorithmECDSAP256},
		{"  ecdsa-p384  ", KeyAlgorithmECDSAP384},
		{"ECDSAP384", KeyAlgorithmECDSAP384},
		{"p384", KeyAlgorithmECDSAP384},
		{"rsa", KeyAlgorithmRSA2048},
		{"rsa2048", KeyAlgorithmRSA2048},
		{"rsa-2048", KeyAlgorithmRSA2048},
		{"rsa3072", KeyAlgorithmRSA3072},
		{"RSA-3072", KeyAlgorithmRSA3072},
		{"rsa4096", KeyAlgorithmRSA4096},
		{"rsa-4096", KeyAlgorithmRSA4096},
		{"ed25519", KeyAlgorithmEd25519},
		{"Ed25519", KeyAlgorithmEd25519},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			got, err := CanonicalKeyAlgorithm(tt.algorithm)
			if err != nil {
				t.Fatalf("CanonicalKeyAlgorithm(%q): %v", tt.algorithm, err)
			}
			if got != tt.want {
				t.Fatalf("CanonicalKeyAlgorithm(%q) = %q, want %q", tt.algorithm, got, tt.want)
			}
		})
	}
}

// An algorithm this package does not generate is refused with a typed error
// that carries the caller's spelling verbatim, and it matches the sentinel so a
// caller can map it without depending on the concrete type.
func TestCanonicalKeyAlgorithm_Rejected(t *testing.T) {
	tests := []string{"dilithium", "rsa1024", "ecdsa-p521", "ed448", "p-256", "ecdsa p256"}

	for _, algorithm := range tests {
		t.Run(algorithm, func(t *testing.T) {
			got, err := CanonicalKeyAlgorithm(algorithm)
			if err == nil {
				t.Fatalf("CanonicalKeyAlgorithm(%q) = %q, want an error", algorithm, got)
			}
			if got != "" {
				t.Fatalf("canonical name should be empty on failure, got %q", got)
			}
			if !errors.Is(err, ErrUnsupportedKeyAlgorithm) {
				t.Fatalf("error should match ErrUnsupportedKeyAlgorithm, got %v", err)
			}
			var typed *quicbaasapi.UnsupportedKeyAlgorithmError
			if !errors.As(err, &typed) {
				t.Fatalf("error should be *api.UnsupportedKeyAlgorithmError, got %T", err)
			}
			if typed.Algorithm != algorithm {
				t.Fatalf("Algorithm = %q, want the caller's spelling %q", typed.Algorithm, algorithm)
			}
		})
	}
}

// =============================================================================
// SupportedKeyAlgorithms
// =============================================================================

// The advertised set is exactly what the dispatch table can generate, sorted so
// an operator-facing message is stable.
func TestSupportedKeyAlgorithms_MatchesDispatchTable(t *testing.T) {
	got := SupportedKeyAlgorithms()
	want := []string{
		KeyAlgorithmECDSAP256,
		KeyAlgorithmECDSAP384,
		KeyAlgorithmEd25519,
		KeyAlgorithmRSA2048,
		KeyAlgorithmRSA3072,
		KeyAlgorithmRSA4096,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedKeyAlgorithms() = %v, want %v", got, want)
	}
}

// Every advertised algorithm actually resolves and generates: an entry that
// could not would be a promise the package cannot keep.
func TestSupportedKeyAlgorithms_EveryEntryIsGenerable(t *testing.T) {
	for _, algorithm := range SupportedKeyAlgorithms() {
		t.Run(algorithm, func(t *testing.T) {
			canonical, err := CanonicalKeyAlgorithm(algorithm)
			if err != nil {
				t.Fatalf("advertised algorithm %q does not resolve: %v", algorithm, err)
			}
			if canonical != algorithm {
				t.Fatalf("advertised algorithm %q is not canonical (resolves to %q)", algorithm, canonical)
			}
			if _, ok := requesterKeyGenerators[canonical]; !ok {
				t.Fatalf("advertised algorithm %q has no generator", canonical)
			}
		})
	}
}

// =============================================================================
// GenerateRequesterKey
// =============================================================================

// Each algorithm yields a usable signer of the right concrete type, reporting
// the x509.PublicKeyAlgorithm an encoder needs, and the signer really signs:
// a key that cannot sign cannot produce the CSR the CA is meant to sign.
//
// RSA-3072 and RSA-4096 are covered by
// TestGenerateRequesterKey_LargeRSA so the common path stays fast.
func TestGenerateRequesterKey(t *testing.T) {
	tests := []struct {
		algorithm string
		wantAlg   x509.PublicKeyAlgorithm
		assert    func(t *testing.T, signer crypto.Signer)
	}{
		{"", x509.ECDSA, func(t *testing.T, s crypto.Signer) {
			key, ok := s.(*ecdsa.PrivateKey)
			if !ok {
				t.Fatalf("default should yield ECDSA, got %T", s)
			}
			if key.Curve.Params().BitSize != 256 {
				t.Fatalf("default should be P-256, got bit size %d", key.Curve.Params().BitSize)
			}
		}},
		{"ecdsa", x509.ECDSA, nil},
		{"p256", x509.ECDSA, nil},
		{"  ecdsa-p384  ", x509.ECDSA, func(t *testing.T, s crypto.Signer) {
			key := s.(*ecdsa.PrivateKey)
			if key.Curve.Params().BitSize != 384 {
				t.Fatalf("expected P-384, got bit size %d", key.Curve.Params().BitSize)
			}
		}},
		{"rsa", x509.RSA, func(t *testing.T, s crypto.Signer) {
			key, ok := s.(*rsa.PrivateKey)
			if !ok {
				t.Fatalf("expected RSA, got %T", s)
			}
			if key.N.BitLen() != 2048 {
				t.Fatalf("expected a 2048-bit modulus, got %d", key.N.BitLen())
			}
		}},
		{"ed25519", x509.Ed25519, func(t *testing.T, s crypto.Signer) {
			if _, ok := s.(ed25519.PrivateKey); !ok {
				t.Fatalf("expected ed25519, got %T", s)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			signer, alg, err := GenerateRequesterKey(tt.algorithm)
			if err != nil {
				t.Fatalf("GenerateRequesterKey(%q): %v", tt.algorithm, err)
			}
			if alg != tt.wantAlg {
				t.Fatalf("public key algorithm = %v, want %v", alg, tt.wantAlg)
			}
			if signer == nil {
				t.Fatal("signer is nil")
			}
			if signer.Public() == nil {
				t.Fatal("signer has no public key")
			}
			assertSignerWorks(t, signer)
			if tt.assert != nil {
				tt.assert(t, signer)
			}
		})
	}
}

// The larger RSA moduli are generated too, kept in their own test so the
// frequently-run table above does not pay for them.
func TestGenerateRequesterKey_LargeRSA(t *testing.T) {
	tests := []struct {
		algorithm string
		wantBits  int
	}{
		{KeyAlgorithmRSA3072, 3072},
		{KeyAlgorithmRSA4096, 4096},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			signer, alg, err := GenerateRequesterKey(tt.algorithm)
			if err != nil {
				t.Fatalf("GenerateRequesterKey(%q): %v", tt.algorithm, err)
			}
			if alg != x509.RSA {
				t.Fatalf("public key algorithm = %v, want RSA", alg)
			}
			key, ok := signer.(*rsa.PrivateKey)
			if !ok {
				t.Fatalf("expected RSA, got %T", signer)
			}
			if key.N.BitLen() != tt.wantBits {
				t.Fatalf("modulus = %d bits, want %d", key.N.BitLen(), tt.wantBits)
			}
		})
	}
}

// An unsupported algorithm produces no key, the unknown public-key algorithm,
// and the sentinel the adapters map onto their own API errors.
func TestGenerateRequesterKey_UnsupportedAlgorithm(t *testing.T) {
	for _, algorithm := range []string{"dilithium", "rsa1024", "ecdsa-p521"} {
		t.Run(algorithm, func(t *testing.T) {
			signer, alg, err := GenerateRequesterKey(algorithm)
			if err == nil {
				t.Fatal("expected an error for an unsupported algorithm")
			}
			if signer != nil {
				t.Fatalf("signer should be nil on failure, got %T", signer)
			}
			if alg != x509.UnknownPublicKeyAlgorithm {
				t.Fatalf("algorithm = %v, want UnknownPublicKeyAlgorithm", alg)
			}
			if !errors.Is(err, ErrUnsupportedKeyAlgorithm) {
				t.Fatalf("error should match ErrUnsupportedKeyAlgorithm, got %v", err)
			}
		})
	}
}

// Two calls never return the same key. A generator wired to a broken or
// constant entropy source would pass every other test in this file.
func TestGenerateRequesterKey_KeysAreDistinct(t *testing.T) {
	for _, algorithm := range []string{KeyAlgorithmECDSAP256, KeyAlgorithmEd25519} {
		t.Run(algorithm, func(t *testing.T) {
			first, _, err := GenerateRequesterKey(algorithm)
			if err != nil {
				t.Fatalf("first GenerateRequesterKey: %v", err)
			}
			second, _, err := GenerateRequesterKey(algorithm)
			if err != nil {
				t.Fatalf("second GenerateRequesterKey: %v", err)
			}
			firstDER, err := x509.MarshalPKIXPublicKey(first.Public())
			if err != nil {
				t.Fatalf("marshal first public key: %v", err)
			}
			secondDER, err := x509.MarshalPKIXPublicKey(second.Public())
			if err != nil {
				t.Fatalf("marshal second public key: %v", err)
			}
			if string(firstDER) == string(secondDER) {
				t.Fatal("two generated keys are identical; the entropy source is not random")
			}
		})
	}
}

// The go-xkms RNG resolver really is on the Ed25519 generation path: breaking
// the resolver breaks generation, wrapped in the typed error.
//
// Ed25519 alone, and the asymmetry is the toolchain's, not this package's. Since
// Go 1.26 ecdsa.GenerateKey and rsa.GenerateKey discard the io.Reader they are
// handed and use Go's FIPS 140-3 DRBG instead, so no test can observe the
// resolver on those paths — see TestGenerateRequesterKey_StdlibIgnoresTheReader,
// which pins that fact so the two tests cannot both quietly become wrong.
//
// If Go later stops honouring the reader here too, this test fails. That is the
// intent: the claim in pkg/xkms/keygen.go about where entropy comes from should
// not be able to go stale in silence.
func TestGenerateRequesterKey_XKMSResolverIsOnTheEd25519Path(t *testing.T) {
	restore := swapRNGResolver(t, failingRandResolver{})
	defer restore()

	signer, alg, err := GenerateRequesterKey(KeyAlgorithmEd25519)
	if err == nil {
		t.Fatal("expected Ed25519 key generation to fail when the xkms RNG fails")
	}
	if signer != nil {
		t.Fatalf("signer should be nil on failure, got %T", signer)
	}
	if alg != x509.UnknownPublicKeyAlgorithm {
		t.Fatalf("algorithm = %v, want UnknownPublicKeyAlgorithm", alg)
	}
	var typed *quicbaasapi.KeyGenerationError
	if !errors.As(err, &typed) {
		t.Fatalf("error should be *api.KeyGenerationError, got %T (%v)", err, err)
	}
	if typed.Algorithm != KeyAlgorithmEd25519 {
		t.Fatalf("Algorithm = %q, want %q", typed.Algorithm, KeyAlgorithmEd25519)
	}
	if !errors.Is(err, errRandUnavailable) {
		t.Fatalf("error should wrap the resolver failure, got %v", err)
	}
}

// ECDSA and RSA key generation succeeds even against a resolver that can produce
// no bytes at all, because the standard library ignores the reader it is given:
//
//	"Since Go 1.26, a secure source of random bytes is always used, and the
//	Reader is ignored unless GODEBUG=cryptocustomrand=1 is set."
//
// This is asserted, not assumed, because pkg/xkms/keygen.go's documentation
// depends on it and because the pre-#910 code and go-xkms's own pkg/xkms/rng.go
// comment both claimed the opposite. If a future toolchain restores the reader,
// this test fails and the documentation gets corrected with it.
func TestGenerateRequesterKey_StdlibIgnoresTheReader(t *testing.T) {
	restore := swapRNGResolver(t, failingRandResolver{})
	defer restore()

	for _, algorithm := range []string{KeyAlgorithmECDSAP256, KeyAlgorithmRSA2048} {
		t.Run(algorithm, func(t *testing.T) {
			signer, _, err := GenerateRequesterKey(algorithm)
			if err != nil {
				t.Fatalf("%s generation failed against a dead resolver, so the reader is "+
					"honoured after all; correct the entropy documentation in keygen.go: %v", algorithm, err)
			}
			if signer == nil {
				t.Fatal("signer is nil with no error")
			}
			assertSignerWorks(t, signer)
		})
	}
}

// assertSignerWorks proves the returned signer can sign, which is the whole
// reason it exists: it signs the CSR the go-xkms CA then signs.
func assertSignerWorks(t *testing.T, signer crypto.Signer) {
	t.Helper()

	message := []byte("go-quicbaas#910 requester key")
	switch key := signer.(type) {
	case ed25519.PrivateKey:
		sig := ed25519.Sign(key, message)
		if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, sig) {
			t.Fatal("ed25519 signature does not verify")
		}
	case *ecdsa.PrivateKey:
		digest := sha256.Sum256(message)
		sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
		if err != nil {
			t.Fatalf("ecdsa sign: %v", err)
		}
		if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], sig) {
			t.Fatal("ecdsa signature does not verify")
		}
	case *rsa.PrivateKey:
		digest := sha256.Sum256(message)
		sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
		if err != nil {
			t.Fatalf("rsa sign: %v", err)
		}
		if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
			t.Fatalf("rsa signature does not verify: %v", err)
		}
	default:
		t.Fatalf("unhandled signer type %T", signer)
	}
}

// =============================================================================
// RNG test seam
// =============================================================================

// swapRNGResolver installs resolver as the package RNG and returns a function
// that restores the previous one. It exists so a test can prove that
// GenerateRequesterKey really draws from the go-xkms resolver: with crypto/rand
// there would be nothing to break.
func swapRNGResolver(t *testing.T, resolver xkmssdk.RandResolver) func() {
	t.Helper()

	rngMu.Lock()
	previous := rngResolver
	rngResolver = resolver
	rngMu.Unlock()

	return func() {
		rngMu.Lock()
		rngResolver = previous
		rngMu.Unlock()
	}
}

// errRandUnavailable is what failingRandResolver reports.
var errRandUnavailable = errors.New("xkms rng: no source available")

// failingRandResolver is a RandResolver whose every read fails, standing in for
// a TPM or HSM that has gone away.
type failingRandResolver struct{}

func (failingRandResolver) Rand(int) ([]byte, error)   { return nil, errRandUnavailable }
func (failingRandResolver) Read([]byte) (int, error)   { return 0, errRandUnavailable }
func (failingRandResolver) Source() xkmssdk.RandSource { return nil }
func (failingRandResolver) Available() bool            { return false }
func (failingRandResolver) Close() error               { return nil }

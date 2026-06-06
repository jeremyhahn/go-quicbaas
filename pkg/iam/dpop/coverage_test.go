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

package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// privateKeyJWKMap returns the JWK header map for a *private* EC key so the
// verifier's IsPublic() guard (RFC 9449 §4.2 requires a public key in the jwk
// header) can be exercised.
func privateKeyJWKMap(t *testing.T, key *ecdsa.PrivateKey) map[string]interface{} {
	t.Helper()
	jwk := jose.JSONWebKey{Key: key}
	b, err := jwk.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal private jwk: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal private jwk: %v", err)
	}
	return m
}

// TestVerifyProof_JWKHeaderCarriesPrivateKey verifies that a proof whose jwk
// header embeds a private key (not the required public key) is rejected as
// malformed. Covers the !jwk.IsPublic() branch in VerifyProof.
func TestVerifyProof_JWKHeaderCarriesPrivateKey(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	privMap := privateKeyJWKMap(t, key)

	proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": privMap},
		jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "priv-1", "iat": clock.t.Unix()},
		jwt.SigningMethodES256, key)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
	var mal *MalformedProofError
	if !errors.As(err, &mal) || !strings.Contains(mal.Error(), "public key") {
		t.Fatalf("expected MalformedProofError about public key, got %v", err)
	}
}

// TestVerifyProof_AlgHeaderMismatch crafts a proof whose alg header claims one
// asymmetric algorithm while the token is actually signed with another. The
// keyfunc rejects the alg/method mismatch. Covers the keyfunc alg-mismatch
// branch and the resulting SignatureError wrap.
func TestVerifyProof_AlgHeaderMismatch(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)

	// Header advertises ES384 (an allowed alg) but the proof is signed ES256.
	proof := signRawProof(t,
		map[string]interface{}{"typ": ProofType, "jwk": jwkMap, "alg": "ES384"},
		jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "mismatch-1", "iat": clock.t.Unix()},
		jwt.SigningMethodES256, key)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
	if err == nil {
		t.Fatal("expected alg-mismatch proof to be rejected")
	}
}

// TestVerifyProof_BadExpectedHTU verifies that a non-absolute expected request
// URI (opts.HTU) yields an htu HTTPBindingError. Covers the wantHTU
// canonicalization error branch.
func TestVerifyProof_BadExpectedHTU(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, "POST", "https://x/y", clock)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "/relative-not-absolute", Clock: clock})
	var bindErr *HTTPBindingError
	if !errors.As(err, &bindErr) || bindErr.Field != "htu" {
		t.Fatalf("expected htu HTTPBindingError for bad expected HTU, got %v", err)
	}
}

// TestVerifyProof_BadProofHTU verifies that a proof carrying a non-absolute htu
// claim yields an htu HTTPBindingError. Covers the gotHTU canonicalization
// error branch (proof-side).
func TestVerifyProof_BadProofHTU(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)

	// Proof htu is a bare path; the canonicalizer rejects it as non-absolute.
	proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": jwkMap},
		jwt.MapClaims{"htm": "POST", "htu": "/bare/path", "jti": "badhtu-1", "iat": clock.t.Unix()},
		jwt.SigningMethodES256, key)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/bare/path", Clock: clock})
	var bindErr *HTTPBindingError
	if !errors.As(err, &bindErr) || bindErr.Field != "htu" {
		t.Fatalf("expected htu HTTPBindingError for bad proof htu, got %v", err)
	}
}

// TestParseEmbeddedJWK_Errors exercises the three failure paths of
// parseEmbeddedJWK: an unmarshalable input (json.Marshal fails), a structurally
// invalid JWK (UnmarshalJSON fails), and a JWK that decodes but is not Valid().
func TestParseEmbeddedJWK_Errors(t *testing.T) {
	t.Run("marshal failure", func(t *testing.T) {
		// A channel cannot be JSON-marshaled, so the re-marshal step fails.
		_, err := parseEmbeddedJWK(make(chan int))
		var mal *MalformedProofError
		if !errors.As(err, &mal) || !strings.Contains(mal.Error(), "re-marshal") {
			t.Fatalf("expected re-marshal MalformedProofError, got %v", err)
		}
	})

	t.Run("unmarshal failure", func(t *testing.T) {
		// kty is the wrong JSON type (number, not string): UnmarshalJSON rejects it.
		_, err := parseEmbeddedJWK(map[string]interface{}{"kty": 123})
		var mal *MalformedProofError
		if !errors.As(err, &mal) {
			t.Fatalf("expected MalformedProofError for invalid jwk, got %v", err)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		// An unknown key type unmarshals into a zero JSONWebKey that is not Valid().
		_, err := parseEmbeddedJWK(map[string]interface{}{"kty": "UNKNOWN"})
		var mal *MalformedProofError
		if !errors.As(err, &mal) {
			t.Fatalf("expected MalformedProofError for not-valid jwk, got %v", err)
		}
	})
}

// TestParseEmbeddedJWK_ValidPublic confirms the success path returns a usable
// public JWK for a real EC public key.
func TestParseEmbeddedJWK_ValidPublic(t *testing.T) {
	key := newECKey(t)
	jwkMap := ecJWKMap(t, key)
	jwk, err := parseEmbeddedJWK(jwkMap)
	if err != nil {
		t.Fatalf("parseEmbeddedJWK: %v", err)
	}
	if !jwk.IsPublic() {
		t.Fatal("expected public jwk")
	}
}

// TestIsAsymmetricPublic covers every supported type plus the default reject
// branch (non-key value).
func TestIsAsymmetricPublic(t *testing.T) {
	ecKey := newECKey(t)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 key: %v", err)
	}

	cases := []struct {
		name string
		key  interface{}
		want bool
	}{
		{"ecdsa", &ecKey.PublicKey, true},
		{"rsa", &rsaKey.PublicKey, true},
		{"ed25519", edPub, true},
		{"string", "not-a-key", false},
		{"nil", nil, false},
		{"bytes", []byte("raw"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAsymmetricPublic(tc.key); got != tc.want {
				t.Fatalf("isAsymmetricPublic(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestThumbprint_Errors confirms an unsupported key type returns a SignerError
// from Thumbprint, and that a supported key yields a stable base64url value.
func TestThumbprint_Errors(t *testing.T) {
	t.Run("unsupported key", func(t *testing.T) {
		_, err := Thumbprint("not-a-key")
		var se *SignerError
		if !errors.As(err, &se) {
			t.Fatalf("expected SignerError, got %v", err)
		}
	})

	t.Run("supported key stable", func(t *testing.T) {
		key := newECKey(t)
		a, err := Thumbprint(key.Public())
		if err != nil {
			t.Fatalf("Thumbprint: %v", err)
		}
		b, err := Thumbprint(key.Public())
		if err != nil {
			t.Fatalf("Thumbprint: %v", err)
		}
		if a == "" || a != b {
			t.Fatalf("thumbprint not stable: %q vs %q", a, b)
		}
	})
}

// TestNumericClaim covers every branch: float64 (the JSON default), int64,
// json.Number (valid and invalid), and the unsupported default.
func TestNumericClaim(t *testing.T) {
	cases := []struct {
		name    string
		claims  jwt.MapClaims
		key     string
		wantVal int64
		wantOK  bool
	}{
		{"float64", jwt.MapClaims{"iat": float64(42)}, "iat", 42, true},
		{"int64", jwt.MapClaims{"iat": int64(99)}, "iat", 99, true},
		{"json.Number valid", jwt.MapClaims{"iat": json.Number("7")}, "iat", 7, true},
		{"json.Number invalid", jwt.MapClaims{"iat": json.Number("notnum")}, "iat", 0, false},
		{"string rejected", jwt.MapClaims{"iat": "10"}, "iat", 0, false},
		{"absent", jwt.MapClaims{}, "iat", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numericClaim(tc.claims, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Fatalf("numericClaim = (%d,%v), want (%d,%v)", got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

// TestVerifyProof_IATAsJSONNumber confirms that an iat encoded as a json.Number
// is accepted, exercising the json.Number arm of numericClaim through the full
// verifier.
func TestVerifyProof_IATAsJSONNumber(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)

	proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": jwkMap},
		jwt.MapClaims{
			"htm": "POST", "htu": "https://x/y", "jti": "num-1",
			"iat": json.Number("1700000000"),
		},
		jwt.SigningMethodES256, key)

	if _, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock, ReplayCache: NewMemoryReplayCache(clock)}); err != nil {
		t.Fatalf("verify json.Number iat: %v", err)
	}
}

// TestAlgForKey covers each supported curve/type and the two unsupported
// branches (unknown ECDSA curve and unknown key type).
func TestAlgForKey(t *testing.T) {
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("p256: %v", err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("p384: %v", err)
	}
	p521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatalf("p521: %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}

	t.Run("supported", func(t *testing.T) {
		cases := []struct {
			name string
			key  crypto.Signer
			want string
		}{
			{"ES256", p256, "ES256"},
			{"ES384", p384, "ES384"},
			{"ES512", p521, "ES512"},
			{"RS256", rsaKey, "RS256"},
			{"EdDSA", edPriv, "EdDSA"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := algForKey(tc.key)
				if err != nil {
					t.Fatalf("algForKey: %v", err)
				}
				if got != tc.want {
					t.Fatalf("algForKey = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("unsupported curve", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
		if err != nil {
			t.Fatalf("p224: %v", err)
		}
		_, err = algForKey(key)
		var se *SignerError
		if !errors.As(err, &se) || !strings.Contains(se.Error(), "curve") {
			t.Fatalf("expected unsupported-curve SignerError, got %v", err)
		}
	})

	t.Run("unsupported key type", func(t *testing.T) {
		_, err := algForKey(unsupportedSigner{})
		var se *SignerError
		if !errors.As(err, &se) {
			t.Fatalf("expected unsupported-key SignerError, got %v", err)
		}
	})
}

// TestNewProofWithClock_UnsupportedKey confirms NewProofWithClock surfaces the
// algForKey error for an unsupported key type rather than producing a proof.
func TestNewProofWithClock_UnsupportedKey(t *testing.T) {
	_, err := NewProofWithClock(unsupportedSigner{}, "POST", "https://x/y", &fixedClock{t: time.Unix(1, 0)})
	var se *SignerError
	if !errors.As(err, &se) {
		t.Fatalf("expected SignerError for unsupported key, got %v", err)
	}
}

// TestNewProofWithClock_NilClockDefaults confirms a nil clock falls back to
// SystemClock and still produces a verifiable proof.
func TestNewProofWithClock_NilClockDefaults(t *testing.T) {
	key := newECKey(t)
	proof, err := NewProofWithClock(key, "POST", "https://x/y", nil)
	if err != nil {
		t.Fatalf("NewProofWithClock nil clock: %v", err)
	}
	if _, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y"}); err != nil {
		t.Fatalf("verify proof from default-clock signer: %v", err)
	}
}

// unsupportedSigner is a crypto.Signer whose public key is not one of the
// supported asymmetric types, used to drive the algForKey default branch.
type unsupportedSigner struct{}

func (unsupportedSigner) Public() crypto.PublicKey { return "unsupported-public-key" }

func (unsupportedSigner) Sign(_ io.Reader, _ []byte, _ crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("unsupportedSigner does not sign")
}

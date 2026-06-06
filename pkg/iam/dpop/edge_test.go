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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signRawProof signs a proof with explicit header/claims control so tests can
// craft malformed proofs (missing typ, missing jwk, missing iat/jti, HS256).
func signRawProof(t *testing.T, header map[string]interface{}, claims jwt.MapClaims, method jwt.SigningMethod, key interface{}) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	for k, v := range header {
		tok.Header[k] = v
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign raw proof: %v", err)
	}
	return s
}

func ecJWKMap(t *testing.T, key *ecdsa.PrivateKey) map[string]interface{} {
	t.Helper()
	p := newProof(t, key, "POST", "https://x/y", &fixedClock{t: time.Unix(1_700_000_000, 0)})
	parser := jwt.NewParser()
	tok, _, err := parser.ParseUnverified(p, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse for jwk: %v", err)
	}
	return tok.Header["jwk"].(map[string]interface{})
}

func TestVerifyProof_WrongTyp(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)
	proof := signRawProof(t, map[string]interface{}{"typ": "jwt", "jwk": jwkMap},
		jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "1", "iat": clock.t.Unix()},
		jwt.SigningMethodES256, key)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
	var mal *MalformedProofError
	if !errors.As(err, &mal) || !strings.Contains(mal.Error(), "typ") {
		t.Fatalf("expected typ MalformedProofError, got %v", err)
	}
}

func TestVerifyProof_MissingJWKHeader(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := signRawProof(t, map[string]interface{}{"typ": ProofType},
		jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "1", "iat": clock.t.Unix()},
		jwt.SigningMethodES256, key)

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
	var mal *MalformedProofError
	if !errors.As(err, &mal) {
		t.Fatalf("expected MalformedProofError for missing jwk, got %v", err)
	}
}

func TestVerifyProof_HS256Rejected(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := signRawProof(t, map[string]interface{}{"typ": ProofType},
		jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "1", "iat": clock.t.Unix()},
		jwt.SigningMethodHS256, []byte("symmetric-secret"))

	_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
	if err == nil {
		t.Fatal("expected HS256 proof to be rejected")
	}
}

func TestVerifyProof_MissingIATAndJTI(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)

	t.Run("missing iat", func(t *testing.T) {
		proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": jwkMap},
			jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "jti": "1"},
			jwt.SigningMethodES256, key)
		_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
		var mal *MalformedProofError
		if !errors.As(err, &mal) || !strings.Contains(mal.Error(), "iat") {
			t.Fatalf("expected missing iat error, got %v", err)
		}
	})

	t.Run("missing jti", func(t *testing.T) {
		proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": jwkMap},
			jwt.MapClaims{"htm": "POST", "htu": "https://x/y", "iat": clock.t.Unix()},
			jwt.SigningMethodES256, key)
		_, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock})
		var mal *MalformedProofError
		if !errors.As(err, &mal) || !strings.Contains(mal.Error(), "jti") {
			t.Fatalf("expected missing jti error, got %v", err)
		}
	})
}

func TestNewProof_AllCurves(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384(), elliptic.P521()} {
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		proof, err := NewProofWithClock(key, "POST", "https://x/y", clock)
		if err != nil {
			t.Fatalf("NewProof curve %v: %v", curve.Params().Name, err)
		}
		if _, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock, ReplayCache: NewMemoryReplayCache(clock)}); err != nil {
			t.Fatalf("verify curve %v: %v", curve.Params().Name, err)
		}
	}
}

func TestSystemClock_Now(t *testing.T) {
	before := time.Now()
	got := SystemClock{}.Now()
	if got.Before(before.Add(-time.Second)) {
		t.Fatalf("SystemClock.Now too far in the past: %v", got)
	}
}

func TestErrorMessages(t *testing.T) {
	cases := []error{
		&MalformedProofError{Detail: "x", Cause: errors.New("c")},
		&MalformedProofError{Detail: "x"},
		&SignatureError{Cause: errors.New("c")},
		&SignatureError{},
		&HTTPBindingError{Field: "htm", Expected: "a", Actual: "b"},
		&StaleProofError{Detail: "old"},
		&ReplayError{JTI: "j"},
		&ThumbprintMismatchError{Expected: "a", Actual: "b"},
		&MissingProofError{},
		&SignerError{Detail: "x", Cause: errors.New("c")},
		&SignerError{Detail: "x"},
	}
	for _, err := range cases {
		if err.Error() == "" {
			t.Errorf("empty error message for %T", err)
		}
	}
	// Unwrap paths.
	if errors.Unwrap(&MalformedProofError{Cause: errors.New("c")}) == nil {
		t.Error("MalformedProofError.Unwrap nil")
	}
	if errors.Unwrap(&SignatureError{Cause: errors.New("c")}) == nil {
		t.Error("SignatureError.Unwrap nil")
	}
	if errors.Unwrap(&SignerError{Cause: errors.New("c")}) == nil {
		t.Error("SignerError.Unwrap nil")
	}
}

func TestVerifyProof_DefaultClockAndNoReplayCache(t *testing.T) {
	key := newECKey(t)
	// No clock and no replay cache => uses SystemClock and skips replay.
	proof, err := NewProof(key, "POST", "https://x/y")
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	if _, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y"}); err != nil {
		t.Fatalf("VerifyProof with defaults: %v", err)
	}
}

func TestNewProof_Ed25519(t *testing.T) {
	pub, priv, err := ed25519KeyPair()
	if err != nil {
		t.Fatalf("ed25519 key: %v", err)
	}
	_ = pub
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof, err := NewProofWithClock(priv, "POST", "https://x/y", clock)
	if err != nil {
		t.Fatalf("NewProof ed25519: %v", err)
	}
	if _, err := VerifyProof(proof, VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock, ReplayCache: NewMemoryReplayCache(clock)}); err != nil {
		t.Fatalf("verify ed25519: %v", err)
	}
}

func TestNewMemoryReplayCache_NilClock(t *testing.T) {
	cache := NewMemoryReplayCache(nil)
	exp := time.Now().Add(time.Minute)
	if cache.SeenBefore("a", exp) {
		t.Fatal("first SeenBefore should be false")
	}
	if !cache.SeenBefore("a", exp) {
		t.Fatal("second SeenBefore should be true")
	}
}

func TestVerifyProof_ExplicitExpReplayWindow(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	jwkMap := ecJWKMap(t, key)
	// Proof carries an explicit exp; the replay window keys off it.
	proof := signRawProof(t, map[string]interface{}{"typ": ProofType, "jwk": jwkMap},
		jwt.MapClaims{
			"htm": "POST", "htu": "https://x/y", "jti": "exp-1",
			"iat": clock.t.Unix(), "exp": clock.t.Add(2 * time.Minute).Unix(),
		},
		jwt.SigningMethodES256, key)

	cache := NewMemoryReplayCache(clock)
	opts := VerifyOptions{HTM: "POST", HTU: "https://x/y", Clock: clock, ReplayCache: cache}
	if _, err := VerifyProof(proof, opts); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := VerifyProof(proof, opts); err == nil {
		t.Fatal("expected replay rejection within explicit exp window")
	}
}

func ed25519KeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

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
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"
)

// fixedClock is a deterministic Clock for tests so iat/skew and replay TTL are
// exercised without sleeping.
type fixedClock struct{ t time.Time }

func (f *fixedClock) Now() time.Time { return f.t }

func newECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	return k
}

const (
	testHTM = "POST"
	testHTU = "https://api.example.com/oauth/token"
)

func newProof(t *testing.T, key crypto.Signer, htm, htu string, clock Clock) string {
	t.Helper()
	p, err := NewProofWithClock(key, htm, htu, clock)
	if err != nil {
		t.Fatalf("NewProofWithClock: %v", err)
	}
	return p
}

func TestVerifyProof_Valid(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, clock)

	cache := NewMemoryReplayCache(clock)
	res, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       clock,
		ReplayCache: cache,
	})
	if err != nil {
		t.Fatalf("VerifyProof valid: unexpected error: %v", err)
	}
	if res.Jkt == "" {
		t.Fatal("expected non-empty jkt")
	}

	// The thumbprint must equal a directly computed thumbprint of the key.
	want, err := Thumbprint(key.Public())
	if err != nil {
		t.Fatalf("Thumbprint: %v", err)
	}
	if res.Jkt != want {
		t.Fatalf("jkt mismatch: got %q want %q", res.Jkt, want)
	}
}

func TestVerifyProof_ExpectedJktMatch(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, clock)

	jkt, err := Thumbprint(key.Public())
	if err != nil {
		t.Fatalf("Thumbprint: %v", err)
	}

	if _, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		ExpectedJkt: jkt,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	}); err != nil {
		t.Fatalf("VerifyProof with matching jkt: %v", err)
	}
}

func TestVerifyProof_WrongHTM(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, "GET", testHTU, clock)

	_, err := VerifyProof(proof, VerifyOptions{
		HTM:         "POST",
		HTU:         testHTU,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	})
	var bindErr *HTTPBindingError
	if !errors.As(err, &bindErr) || bindErr.Field != "htm" {
		t.Fatalf("expected htm HTTPBindingError, got %v", err)
	}
}

func TestVerifyProof_WrongHTU(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, "https://api.example.com/other", clock)

	_, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	})
	var bindErr *HTTPBindingError
	if !errors.As(err, &bindErr) || bindErr.Field != "htu" {
		t.Fatalf("expected htu HTTPBindingError, got %v", err)
	}
}

func TestVerifyProof_HTUIgnoresQueryAndFragment(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	// Proof carries query + fragment; request URI is the bare path. Both must
	// canonicalize to the same scheme+host+path.
	proof := newProof(t, key, testHTM, testHTU+"?a=1#frag", clock)

	if _, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	}); err != nil {
		t.Fatalf("htu with query/fragment should canonicalize equal: %v", err)
	}
}

func TestVerifyProof_ExpiredIAT(t *testing.T) {
	key := newECKey(t)
	issueClock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, issueClock)

	// Verify 5 minutes later — well outside the default ±60s skew.
	verifyClock := &fixedClock{t: issueClock.t.Add(5 * time.Minute)}
	_, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       verifyClock,
		ReplayCache: NewMemoryReplayCache(verifyClock),
	})
	var stale *StaleProofError
	if !errors.As(err, &stale) {
		t.Fatalf("expected StaleProofError, got %v", err)
	}
}

func TestVerifyProof_FutureIAT(t *testing.T) {
	key := newECKey(t)
	issueClock := &fixedClock{t: time.Unix(1_700_000_000, 0).Add(5 * time.Minute)}
	proof := newProof(t, key, testHTM, testHTU, issueClock)

	verifyClock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	_, err := VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       verifyClock,
		ReplayCache: NewMemoryReplayCache(verifyClock),
	})
	var stale *StaleProofError
	if !errors.As(err, &stale) {
		t.Fatalf("expected StaleProofError for future iat, got %v", err)
	}
}

func TestVerifyProof_ReplayedJTI(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, clock)
	cache := NewMemoryReplayCache(clock)

	opts := VerifyOptions{HTM: testHTM, HTU: testHTU, Clock: clock, ReplayCache: cache}
	if _, err := VerifyProof(proof, opts); err != nil {
		t.Fatalf("first use should succeed: %v", err)
	}
	_, err := VerifyProof(proof, opts)
	var replay *ReplayError
	if !errors.As(err, &replay) {
		t.Fatalf("expected ReplayError on second use, got %v", err)
	}
}

func TestVerifyProof_JktMismatch(t *testing.T) {
	key := newECKey(t)
	other := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, clock)

	otherJkt, err := Thumbprint(other.Public())
	if err != nil {
		t.Fatalf("Thumbprint: %v", err)
	}

	_, err = VerifyProof(proof, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		ExpectedJkt: otherJkt,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	})
	var mismatch *ThumbprintMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected ThumbprintMismatchError, got %v", err)
	}
}

func TestVerifyProof_TamperedSignature(t *testing.T) {
	key := newECKey(t)
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof := newProof(t, key, testHTM, testHTU, clock)

	// Mutate a character inside the signature segment so the signature no
	// longer verifies against the embedded key.
	dot := strings.LastIndexByte(proof, '.')
	sig := []byte(proof)
	idx := dot + 3
	if sig[idx] == 'A' {
		sig[idx] = 'B'
	} else {
		sig[idx] = 'A'
	}
	tampered := string(sig)

	_, err := VerifyProof(tampered, VerifyOptions{
		HTM:         testHTM,
		HTU:         testHTU,
		Clock:       clock,
		ReplayCache: NewMemoryReplayCache(clock),
	})
	var sigErr *SignatureError
	if !errors.As(err, &sigErr) {
		t.Fatalf("expected SignatureError, got %v", err)
	}
}

func TestVerifyProof_EmptyAndMalformed(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	for _, tc := range []struct{ name, proof string }{
		{"empty", ""},
		{"whitespace", "   "},
		{"garbage", "not.a.jwt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := VerifyProof(tc.proof, VerifyOptions{HTM: testHTM, HTU: testHTU, Clock: clock})
			var mal *MalformedProofError
			if !errors.As(err, &mal) {
				t.Fatalf("expected MalformedProofError, got %v", err)
			}
		})
	}
}

func TestNewProof_RSAAndEd25519Build(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	proof, err := NewProofWithClock(rsaKey, testHTM, testHTU, clock)
	if err != nil {
		t.Fatalf("NewProof RSA: %v", err)
	}
	if _, err := VerifyProof(proof, VerifyOptions{HTM: testHTM, HTU: testHTU, Clock: clock, ReplayCache: NewMemoryReplayCache(clock)}); err != nil {
		t.Fatalf("verify RSA proof: %v", err)
	}
}

func TestNewProof_NilKey(t *testing.T) {
	_, err := NewProof(nil, testHTM, testHTU)
	var se *SignerError
	if !errors.As(err, &se) {
		t.Fatalf("expected SignerError for nil key, got %v", err)
	}
}

func TestCanonicalHTU(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"https://API.Example.com/path?x=1#f", "https://api.example.com/path", false},
		{"HTTPS://host/p", "https://host/p", false},
		{"/relative", "", true},
		{"://bad", "", true},
	}
	for _, tc := range tests {
		got, err := CanonicalHTU(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CanonicalHTU(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("CanonicalHTU(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CanonicalHTU(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMemoryReplayCache_TTLExpiry(t *testing.T) {
	clock := &fixedClock{t: time.Unix(1_700_000_000, 0)}
	cache := NewMemoryReplayCache(clock)
	exp := clock.t.Add(time.Minute)

	if cache.SeenBefore("jti-1", exp) {
		t.Fatal("first SeenBefore should report not-seen")
	}
	if !cache.SeenBefore("jti-1", exp) {
		t.Fatal("second SeenBefore should report replay")
	}

	// Advance past expiry; the entry should be pruned and reusable.
	clock.t = exp.Add(time.Second)
	if cache.SeenBefore("jti-1", clock.t.Add(time.Minute)) {
		t.Fatal("after TTL expiry the jti should be accepted again")
	}
	if cache.Len() != 1 {
		t.Fatalf("expected 1 live entry, got %d", cache.Len())
	}
}

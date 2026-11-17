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

package platformtoken

import (
	"context"
	"crypto"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewValidator_KeyFunc_AsksIssuerEveryVerification proves a KeyFunc
// source caches nothing: the issuer is asked at each verification, so a key it
// stops trusting is refused at the next one.
func TestNewValidator_KeyFunc_AsksIssuerEveryVerification(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	var trusted atomic.Bool
	trusted.Store(true)
	var calls atomic.Int32

	v, err := NewValidator(Options{
		Issuer:   testIssuer,
		Audience: testAudience,
		KeyFunc: func(_ context.Context, kid string) (crypto.PublicKey, error) {
			calls.Add(1)
			if kid != testKID || !trusted.Load() {
				return nil, &UnknownKeyIDError{KID: kid}
			}
			return &key.priv.PublicKey, nil
		},
		Clock: fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		tid: "tenant-1", scp: []string{"app.read"},
		exp: now.Add(10 * time.Minute), iat: now,
	})

	for i := 0; i < 2; i++ {
		if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
			t.Fatalf("ValidatePlatformToken %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("KeyFunc calls = %d, want 2", got)
	}

	trusted.Store(false)
	_, err = v.ValidatePlatformToken(context.Background(), tok)
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) || unknown.KID != testKID {
		t.Fatalf("after the issuer stops trusting the key: err = %v, want *UnknownKeyIDError for %q", err, testKID)
	}
}

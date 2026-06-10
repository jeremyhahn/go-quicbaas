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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// mergedJWKS combines the single-key sets of multiple issuer keys into one
// aggregated set, mirroring OIDCProvider.GetJWKS()'s multi-tenant aggregation.
func mergedJWKS(keys ...*issuerKey) *jose.JSONWebKeySet {
	set := &jose.JSONWebKeySet{}
	for _, k := range keys {
		set.Keys = append(set.Keys, k.jwks().Keys...)
	}
	return set
}

func TestInProcessKeySource_ResolvesPresentKID(t *testing.T) {
	key := newIssuerKey(t, testKID)
	src := newInProcessKeySource(func() *jose.JSONWebKeySet { return key.jwks() })

	got, err := src.publicKey(context.Background(), testKID)
	if err != nil {
		t.Fatalf("publicKey: %v", err)
	}
	if got != key.priv.Public() {
		t.Fatalf("resolved key does not match the issuer public key")
	}
}

func TestInProcessKeySource_RefreshOnMissPicksUpNewTenantKey(t *testing.T) {
	keyA := newIssuerKey(t, "kid-A")
	keyB := newIssuerKey(t, "kid-B")

	var live atomic.Pointer[jose.JSONWebKeySet]
	live.Store(mergedJWKS(keyA))
	src := newInProcessKeySource(func() *jose.JSONWebKeySet { return live.Load() })

	// A resolves from the initial set.
	if _, err := src.publicKey(context.Background(), "kid-A"); err != nil {
		t.Fatalf("kid-A initial resolve: %v", err)
	}

	// A new tenant key (B) is added to the live set; refresh-on-miss must find it.
	live.Store(mergedJWKS(keyA, keyB))
	gotB, err := src.publicKey(context.Background(), "kid-B")
	if err != nil {
		t.Fatalf("kid-B resolve after rotation: %v", err)
	}
	if gotB != keyB.priv.Public() {
		t.Fatalf("kid-B resolved to the wrong key")
	}

	// A must still resolve after the refresh.
	if _, err := src.publicKey(context.Background(), "kid-A"); err != nil {
		t.Fatalf("kid-A resolve after rotation: %v", err)
	}
}

func TestInProcessKeySource_UnknownKID(t *testing.T) {
	key := newIssuerKey(t, testKID)
	src := newInProcessKeySource(func() *jose.JSONWebKeySet { return key.jwks() })

	_, err := src.publicKey(context.Background(), "kid-missing")
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected *UnknownKeyIDError, got %T: %v", err, err)
	}
	if unknown.KID != "kid-missing" {
		t.Fatalf("unexpected KID: %q", unknown.KID)
	}
}

func TestInProcessKeySource_NilKeySetNoPanic(t *testing.T) {
	src := newInProcessKeySource(func() *jose.JSONWebKeySet { return nil })

	_, err := src.publicKey(context.Background(), testKID)
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected *UnknownKeyIDError from nil set, got %T: %v", err, err)
	}
}

func TestNewValidator_KeySetFunc_ValidatesToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)

	v, err := NewValidator(Options{
		Issuer:     testIssuer,
		Audience:   testAudience,
		KeySetFunc: func() *jose.JSONWebKeySet { return key.jwks() },
		Clock:      fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		tid: "tenant-1", scp: []string{"app.read"},
		exp: now.Add(10 * time.Minute), iat: now,
	})

	claims, err := v.ValidatePlatformToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("ValidatePlatformToken: %v", err)
	}
	if claims.TenantID != "tenant-1" {
		t.Fatalf("unexpected tenant: %q", claims.TenantID)
	}
}

func TestNewValidator_KeySetFunc_NewTenantTokenValidates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	keyA := newIssuerKey(t, "kid-A")
	keyB := newIssuerKey(t, "kid-B")

	var live atomic.Pointer[jose.JSONWebKeySet]
	live.Store(mergedJWKS(keyA))

	v, err := NewValidator(Options{
		Issuer:     testIssuer,
		Audience:   testAudience,
		KeySetFunc: func() *jose.JSONWebKeySet { return live.Load() },
		Clock:      fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// New tenant key added after construction; a token it signs must validate.
	live.Store(mergedJWKS(keyA, keyB))
	tok := keyB.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-2",
		tid: "tenant-2", scp: []string{"app.read"},
		exp: now.Add(10 * time.Minute), iat: now,
	})

	if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
		t.Fatalf("ValidatePlatformToken (new tenant): %v", err)
	}
}

func TestNewValidator_NoKeySource(t *testing.T) {
	_, err := NewValidator(Options{
		Issuer:   testIssuer,
		Audience: testAudience,
	})
	var cfg *ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("expected *ConfigError, got %T: %v", err, err)
	}
}

func TestInProcessKeySource_ConcurrentResolveDuringRotation(t *testing.T) {
	keyA := newIssuerKey(t, "kid-A")
	keyB := newIssuerKey(t, "kid-B")

	var live atomic.Pointer[jose.JSONWebKeySet]
	live.Store(mergedJWKS(keyA))
	src := newInProcessKeySource(func() *jose.JSONWebKeySet { return live.Load() })

	const workers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			// Resolving A is always valid; resolving B may miss until the
			// live set is swapped, but must never panic or race.
			_, _ = src.publicKey(context.Background(), "kid-A")
			_, _ = src.publicKey(context.Background(), "kid-B")
		}()
	}

	close(start)
	live.Store(mergedJWKS(keyA, keyB))
	wg.Wait()

	if _, err := src.publicKey(context.Background(), "kid-B"); err != nil {
		t.Fatalf("kid-B resolve after concurrent rotation: %v", err)
	}
}

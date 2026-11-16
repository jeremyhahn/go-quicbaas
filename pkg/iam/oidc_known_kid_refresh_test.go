package iam

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// fakeRefreshClock replaces the routine-refresh clock for one test and
// returns a function that advances it.
func fakeRefreshClock(t *testing.T) func(time.Duration) {
	t.Helper()
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	prev := refreshNow
	refreshNow = func() time.Time { return time.Unix(0, now.Load()) }
	t.Cleanup(func() { refreshNow = prev })
	return func(d time.Duration) { now.Add(int64(d)) }
}

// TestOIDCProvider_KnownKIDLookupTakesRoutineRefresh proves a node that
// only verifies stops trusting a key a peer rotated out of the overlap once
// its routine refresh interval has passed, although every token it sees
// names a kid it already holds and it is never refreshed explicitly.
func TestOIDCProvider_KnownKIDLookupTakesRoutineRefresh(t *testing.T) {
	// Each lookup a verifier makes of a held kid: the bearer KeyFunc's
	// provider lookup and the signing key manager's own.
	lookups := map[string]func(n *sharedSigningNode, m *SigningKeyManager, kid string) error{
		"bearer key func": func(n *sharedSigningNode, _ *SigningKeyManager, kid string) error {
			_, err := n.provider.PublicKeyForKID(context.Background(), kid)
			return err
		},
		"signing key manager": func(_ *sharedSigningNode, m *SigningKeyManager, kid string) error {
			_, err := m.PublicForKID(kid)
			return err
		},
	}
	for lookupName, lookup := range lookups {
		for name, tenantID := range map[string]string{"global key": "", "dedicated tenant key": "dedicated-tenant"} {
			t.Run(lookupName+"/"+name, func(t *testing.T) {
				setupSharedSigningXKMS(t)
				advance := fakeRefreshClock(t)
				a := newSharedSigningNode(t, tenantID)
				b := newSharedSigningNode(t, tenantID)
				mintTenant := tenantID
				if mintTenant == "" {
					mintTenant = "ordinary-tenant"
				}
				mA, mB := a.manager(t, mintTenant), b.manager(t, mintTenant)
				if err := mB.Refresh(); err != nil {
					t.Fatalf("B refresh: %v", err)
				}
				b.resolver.JWKS(context.Background())

				r1, _ := xkms.JWTExtractKID(a.mint(t, mintTenant))
				if err := lookup(b, mB, r1); err != nil {
					t.Fatalf("B looking up R1: %v", err)
				}

				for i := range 2 {
					if err := mA.Rotate(); err != nil {
						t.Fatalf("A rotate %d: %v", i+2, err)
					}
				}
				if r3, _, _ := mA.Current(); r3 == r1 {
					t.Fatal("A did not rotate")
				}

				advance(2 * signingKeyRefreshInterval)
				if err := lookup(b, mB, r1); !errors.As(err, new(*UnknownKeyIDError)) {
					t.Fatalf("B looking up R1 two rotations later, past its refresh interval: %v, want UnknownKeyIDError", err)
				}
			})
		}
	}
}

// TestSigningKeyManager_KnownKIDFloodIsRateLimited proves a flood of lookups
// of a held kid re-reads the version record only at the routine pace.
func TestSigningKeyManager_KnownKIDFloodIsRateLimited(t *testing.T) {
	records := setupSharedSigningXKMS(t)
	advance := fakeRefreshClock(t)
	node := newSharedSigningNode(t, "")
	m := node.global
	if err := m.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	kid, _, _ := m.Current()

	records.gets.Store(0)
	if err := m.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	perRefresh := records.gets.Load()
	if perRefresh == 0 {
		t.Fatal("a refresh reads no version record")
	}

	const windows, flood = 3, 200
	records.gets.Store(0)
	for w := range windows {
		advance(signingKeyRefreshInterval)
		for range flood {
			if _, err := m.PublicForKID(kid); err != nil {
				t.Fatalf("window %d: PublicForKID: %v", w, err)
			}
			if _, err := node.provider.PublicKeyForKID(context.Background(), kid); err != nil {
				t.Fatalf("window %d: PublicKeyForKID: %v", w, err)
			}
		}
	}
	if got, limit := records.gets.Load(), perRefresh*windows; got == 0 || got > limit {
		t.Fatalf("%d known-kid lookups over %d refresh windows read the version record %d times, want 1..%d",
			2*flood*windows, windows, got, limit)
	}
}

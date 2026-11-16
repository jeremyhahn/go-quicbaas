package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// adoptionClock is an injected clock for the adoption cutoffs, safe for
// concurrent reads.
type adoptionClock struct{ ns atomic.Int64 }

// Now returns the clock's time.
func (c *adoptionClock) Now() time.Time { return time.Unix(0, c.ns.Load()) }

// set moves the clock to at.
func (c *adoptionClock) set(at time.Time) { c.ns.Store(at.UnixNano()) }

// useAdoptionClock makes the adoption cutoffs registered from here on read
// an injected clock starting at the real time.
func useAdoptionClock(t *testing.T) *adoptionClock {
	t.Helper()
	clock := &adoptionClock{}
	clock.set(time.Now())
	prev := oidcAdoptionNow
	oidcAdoptionNow = clock.Now
	t.Cleanup(func() { oidcAdoptionNow = prev })
	return clock
}

// TestOIDCSigningStore_AdoptedKeyStopsAtReplicatedCutoff proves the adopted
// pre-upgrade key L is trusted only until the cluster's cutoff for it, the
// adoption time plus the longest lifetime of a token the key signs. A token
// L signed just before the rotation verifies on both nodes, and on a node
// restarted before the cutoff, at the cutoff minus epsilon; at the cutoff
// plus epsilon a refresh drops L from verification and the JWKS on all of
// them, and a node started after the cutoff never adopts it again. A later
// adoption does not move the recorded cutoff.
func TestOIDCSigningStore_AdoptedKeyStopsAtReplicatedCutoff(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	const epsilon = time.Millisecond
	for name, tenantID := range map[string]string{"global key": "", "dedicated tenant key": "tenant-dedicated"} {
		t.Run(name, func(t *testing.T) {
			clock := useAdoptionClock(t)
			cutoff := clock.Now().Add(oidcSignedTokenLifetime(&config.AuthConfig{}))
			initOIDCSigningDefault(t)
			legacy, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, tenantID)
			require.NoError(t, err)
			lKid, lSigner, _ := legacy.Current()

			registerFreshOIDCSigningBackend(t)
			node := func(t *testing.T) *iam.SigningKeyManager {
				t.Helper()
				global, err := newOIDCSigningKeyManager(oidcSigningBackendName)
				require.NoError(t, err)
				if tenantID == "" {
					return global
				}
				resolver, err := iam.NewTenantSigningKeyResolver(global, dedicatedOIDCSettings{tenantID: true})
				require.NoError(t, err)
				m, err := resolver.ManagerFor(context.Background(), tenantID)
				require.NoError(t, err)
				return m
			}
			a, peer := node(t), node(t)

			token, err := dbxkms.JWTSignWithSignerKID(lSigner, dbxkms.JWTClaims{
				"sub": "user-signed-under-L",
				"exp": time.Now().Add(2 * time.Hour).Unix(),
			}, iam.SigningAlgorithmES256, lKid)
			require.NoError(t, err)
			require.NoError(t, a.Rotate())
			require.NoError(t, peer.Refresh())
			verify := func(m *iam.SigningKeyManager) error {
				pub, err := m.PublicForKID(lKid)
				if err != nil {
					return err
				}
				_, err = dbxkms.JWTVerifyWithKey(token, pub, iam.SigningAlgorithmES256)
				return err
			}

			clock.set(cutoff.Add(-epsilon))
			restartedBefore := node(t)
			nodes := map[string]*iam.SigningKeyManager{"a": a, "peer": peer, "restarted before the cutoff": restartedBefore}
			for side, m := range nodes {
				require.NoError(t, m.Refresh(), side)
				require.NoError(t, verify(m), "%s at the cutoff minus epsilon", side)
				require.Len(t, m.JWKS().Key(lKid), 1, side)
			}

			clock.set(cutoff.Add(epsilon))
			nodes["restarted after the cutoff"] = node(t)
			for side, m := range nodes {
				require.NoError(t, m.Refresh(), side)
				var unknown *iam.UnknownKeyIDError
				require.ErrorAs(t, verify(m), &unknown, "%s at the cutoff plus epsilon", side)
				require.Empty(t, m.JWKS().Key(lKid), side)
				kid, _, _ := m.Current()
				require.NotEqual(t, lKid, kid, side)
			}
		})
	}
}

// TestOIDCSigningStore_AdoptionWithoutCutoffIsRefused proves a manager on a
// backend with no cutoff record refuses to adopt the pre-upgrade key, since
// nothing would end its trust.
func TestOIDCSigningStore_AdoptionWithoutCutoffIsRefused(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	initOIDCSigningDefault(t)
	_, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, "")
	require.NoError(t, err)
	registerFreshOIDCSigningBackend(t)
	iam.SetAdoptionCutoffs(oidcSigningBackendName, nil)

	_, err = newOIDCSigningKeyManager(oidcSigningBackendName)
	var refused *iam.AdoptionCutoffUnavailableError
	require.ErrorAs(t, err, &refused)
}

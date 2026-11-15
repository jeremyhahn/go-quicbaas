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

package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// bootOIDCSigningNode simulates one node's boot: a fresh process-global xkms
// with its own node-local (memory) default backend, the OIDC signing backend
// registered over kv, and the signing key manager built on it.
func bootOIDCSigningNode(t *testing.T, kv xkmssdk.StorageBackend) *iam.SigningKeyManager {
	t.Helper()
	initOIDCSigningDefault(t)
	registerOIDCSigningBackend(t, kv)

	m, err := iam.NewSigningKeyManagerFromXKMSBackend(iam.OIDCSigningKeyCN, "", oidcSigningBackendName)
	require.NoError(t, err)
	return m
}

// initOIDCSigningDefault initializes xkms with an in-memory software default
// backend and a key-version store, as the server wires them.
func initOIDCSigningDefault(t *testing.T) {
	t.Helper()
	keyVersions, err := xkmssdk.NewKeyVersionStore(xkmssdk.NewStorageMemory())
	require.NoError(t, err)
	initOIDCSigningDefaultWith(t, keyVersions)
}

// initOIDCSigningDefaultWith initializes the process-global xkms service with
// a node-local software default backend and the key-version store keyVersions.
func initOIDCSigningDefaultWith(t *testing.T, keyVersions *xkmssdk.KeyVersionStore) {
	t.Helper()
	dbxkms.Reset()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	local, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	def, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     local,
		CertStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: string(xkmssdk.StoreSoftware),
		Backends:       map[string]xkmssdk.ServiceBackend{string(xkmssdk.StoreSoftware): def},
		KeyVersions:    keyVersions,
		Logger:         logger.NewAdapter(log),
	}))
}

// registerOIDCSigningBackend registers the replicated OIDC signing backend
// over kv.
func registerOIDCSigningBackend(t *testing.T, kv xkmssdk.StorageBackend) {
	t.Helper()
	backend, err := newOIDCSigningServiceBackend(kv)
	require.NoError(t, err)
	require.NoError(t, dbxkms.RegisterBackend(oidcSigningBackendName, backend))
	// The backend's key material changes only under the rotation fence, so
	// a backend registered alone rotates under a lease on its own kv, as
	// useReplicatedOIDCSigningStore wires it; a lease an earlier test's
	// server left may sit on a KV that is closed.
	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	require.NoError(t, err)
	guardedVersions, err := guardedOIDCStore(versions)
	require.NoError(t, err)
	iam.SetRotationLock(oidcSigningBackendName, newOIDCRotationLock(guarded, guardedVersions))
}

// TestOIDCSigningStore_PeerNodesShareOneKey proves a platform token signed on
// one node verifies on a peer: both nodes load the one key from the shared
// (replicated) store, so they agree on the kid. A peer on a different store
// gets a different kid, which shows the shared store is what makes it pass.
func TestOIDCSigningStore_PeerNodesShareOneKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)

	cases := []struct {
		name     string
		shared   bool
		sameKeys bool
	}{
		{name: "peer on the shared store", shared: true, sameKeys: true},
		{name: "peer on its own store", shared: false, sameKeys: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := xkmssdk.NewStorageMemory()
			a := bootOIDCSigningNode(t, kv)
			kidA, signerA, _ := a.Current()

			kvB := kv
			if !tc.shared {
				kvB = xkmssdk.NewStorageMemory()
			}
			b := bootOIDCSigningNode(t, kvB)
			kidB, _, _ := b.Current()

			if !tc.sameKeys {
				require.NotEqual(t, kidA, kidB)
				require.Empty(t, b.JWKS().Key(kidA))
				return
			}
			require.Equal(t, kidA, kidB)
			require.Len(t, b.JWKS().Key(kidA), 1)

			digest := sha256.Sum256([]byte("platform-token-payload"))
			sig, err := signerA.Sign(rand.Reader, digest[:], crypto.SHA256)
			require.NoError(t, err)
			pub, err := b.PublicForKID(kidA)
			require.NoError(t, err)
			ecPub, ok := pub.(*ecdsa.PublicKey)
			require.True(t, ok)
			require.True(t, ecdsa.VerifyASN1(ecPub, digest[:], sig))
		})
	}
}

// TestOIDCSigningStore_RotateOnReplicatedBackend rotates the signing key on
// the registered replicated backend: the current kid changes and the old kid
// stays in JWKS through the overlap window.
func TestOIDCSigningStore_RotateOnReplicatedBackend(t *testing.T) {
	t.Cleanup(dbxkms.Reset)

	m := bootOIDCSigningNode(t, xkmssdk.NewStorageMemory())
	oldKid, _, _ := m.Current()

	require.NoError(t, m.Rotate())

	newKid, _, _ := m.Current()
	require.NotEqual(t, oldKid, newKid)
	require.Len(t, m.JWKS().Key(oldKid), 1)
	require.Len(t, m.JWKS().Key(newKid), 1)
}

// bootOIDCSigningServer boots one node's OIDC signing key the way the server
// wires it: the xkms service gets the node's own key-version store
// (Server.keyVersionStore, node-local without a data dir) and the
// "oidc-replicated" backend goes on the shared replicated KV kv.
func bootOIDCSigningServer(t *testing.T, s *Server, kv xkmssdk.StorageBackend) *iam.SigningKeyManager {
	t.Helper()
	keyVersions, err := s.keyVersionStore()
	require.NoError(t, err)
	initOIDCSigningDefaultWith(t, keyVersions)
	require.NoError(t, s.useReplicatedOIDCSigningStore(kv))

	m, err := iam.NewSigningKeyManagerFromXKMSBackend(iam.OIDCSigningKeyCN, "", oidcSigningBackendName)
	require.NoError(t, err)
	return m
}

// A rotation on one node reaches its peer: the peer's fresh manager signs
// with the rotated key, because the key-version record of the replicated
// OIDC key is replicated with the key.
func TestOIDCSigningStore_RotationReachesPeerNode(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv := xkmssdk.NewStorageMemory()

	nodeA := &Server{config: &config.Config{}}
	a := bootOIDCSigningServer(t, nodeA, kv)
	oldKid, _, _ := a.Current()
	require.NoError(t, a.Rotate())
	rotatedKid, _, _ := a.Current()
	require.NotEqual(t, oldKid, rotatedKid)

	nodeB := &Server{config: &config.Config{}}
	b := bootOIDCSigningServer(t, nodeB, kv)
	kidB, _, _ := b.Current()
	require.Equal(t, rotatedKid, kidB)
	require.Len(t, b.JWKS().Key(rotatedKid), 1)
}

// TestOIDCSigningStore_TwoNodeRotationRestartAndPeerRotation rotates the
// shared OIDC key on node A, checks node B's running manager verifies A's new
// tokens, restarts A, then rotates from B: the restarted A still verifies the
// version before its rotation, B's rotation makes a new version without
// replacing A's, and A picks B's rotation up without a restart.
func TestOIDCSigningStore_TwoNodeRotationRestartAndPeerRotation(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv := xkmssdk.NewStorageMemory()
	verifies := func(m *iam.SigningKeyManager, kid string) func() bool {
		return func() bool {
			_, err := m.PublicForKID(kid)
			return err == nil
		}
	}

	a := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	v1, _, _ := a.Current()
	kidB, _, _ := b.Current()
	require.Equal(t, v1, kidB)

	// A rotates; B's running manager verifies A's new tokens.
	require.NoError(t, a.Rotate())
	v2, _, _ := a.Current()
	require.NotEqual(t, v1, v2)
	require.Eventually(t, verifies(b, v2), 5*time.Second, 20*time.Millisecond)
	v2Pub, err := a.PublicForKID(v2)
	require.NoError(t, err)

	// A restarts: it signs with v2 and still verifies its v1 tokens.
	a = bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	kidA, _, _ := a.Current()
	require.Equal(t, v2, kidA)
	_, err = a.PublicForKID(v1)
	require.NoError(t, err)
	require.Len(t, a.JWKS().Key(v1), 1)

	// B rotates from the record A left: a third version, and A's v2 kept.
	require.NoError(t, b.Rotate())
	v3, _, _ := b.Current()
	require.NotEqual(t, v1, v3)
	require.NotEqual(t, v2, v3)
	require.Eventually(t, verifies(a, v3), 5*time.Second, 20*time.Millisecond)
	pub, err := a.PublicForKID(v2)
	require.NoError(t, err)
	require.True(t, v2Pub.(*ecdsa.PublicKey).Equal(pub))

	// A node booted now loads v3 as current and A's original v2 material.
	c := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv)
	kidC, _, _ := c.Current()
	require.Equal(t, v3, kidC)
	pub, err = c.PublicForKID(v2)
	require.NoError(t, err)
	require.True(t, v2Pub.(*ecdsa.PublicKey).Equal(pub))
}

// TestOIDCRotationLock serializes two rotations of one key, takes over an
// expired lease and refuses a rotation whose lease stays held.
func TestOIDCRotationLock(t *testing.T) {
	ctx := context.Background()
	kv := guardedMemoryKV(t)
	lock := newOIDCRotationLock(kv, kv)
	cn := iam.OIDCSigningKeyCN

	_, unlock, err := lock(ctx, "", cn)
	require.NoError(t, err)
	acquired := make(chan func(), 1)
	failed := make(chan error, 1)
	go func() {
		_, u, err := lock(ctx, "", cn)
		if err != nil {
			failed <- err
			return
		}
		acquired <- u
	}()
	select {
	case <-acquired:
		t.Fatal("second rotation acquired the lease while the first held it")
	case err := <-failed:
		t.Fatalf("second rotation: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case u := <-acquired:
		u()
	case err := <-failed:
		t.Fatalf("second rotation: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second rotation did not acquire the released lease")
	}

	stale := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano) + " stale"
	require.NoError(t, kv.Put(ctx, oidcRotationLeasePrefix+"system/"+cn, []byte(stale)))
	_, u, err := lock(ctx, "", cn)
	require.NoError(t, err)
	u()

	wait := oidcRotationLeaseWait
	oidcRotationLeaseWait = 100 * time.Millisecond
	t.Cleanup(func() { oidcRotationLeaseWait = wait })
	_, held, err := lock(ctx, "tenant-a", cn)
	require.NoError(t, err)
	_, _, err = lock(ctx, "tenant-a", cn)
	require.ErrorAs(t, err, new(*OIDCRotationBusyError))
	held()
	_, u, err = lock(ctx, "tenant-a", cn)
	require.NoError(t, err)
	u()
}

// TestOIDCSigningStore_KeepsNonSoftwareDefault checks the backend selection:
// only a software default backend moves the key to the replicated store; a
// TPM, PKCS#11 or cloud-KMS default stays in use.
func TestOIDCSigningStore_KeepsNonSoftwareDefault(t *testing.T) {
	const defaultName = "operator-default"
	cases := []struct {
		name        string
		defaultType xkmssdk.BackendType
		want        string
	}{
		{name: "software default", defaultType: xkmssdk.BackendTypeSoftware, want: oidcSigningBackendName},
		{name: "pkcs11 default", defaultType: xkmssdk.BackendTypePKCS11, want: defaultName},
		{name: "tpm2 default", defaultType: xkmssdk.BackendTypeTPM2, want: defaultName},
		{name: "aws kms default", defaultType: xkmssdk.BackendTypeAWSKMS, want: defaultName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, selectOIDCSigningBackend(tc.defaultType, defaultName))
		})
	}

	// The process default in the test setup is software.
	t.Cleanup(dbxkms.Reset)
	initOIDCSigningDefault(t)
	defType, err := defaultBackendType()
	require.NoError(t, err)
	require.Equal(t, xkmssdk.BackendTypeSoftware, defType)
}

// TestOIDCSigningStore_AdoptsExistingDefaultKey upgrades a node whose OIDC
// signing key lives in the default backend: the replicated manager keeps the
// pre-upgrade key verify-only, so outstanding tokens still verify.
func TestOIDCSigningStore_AdoptsExistingDefaultKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)

	t.Run("existing default key", func(t *testing.T) {
		initOIDCSigningDefault(t)
		old, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, "")
		require.NoError(t, err)
		oldKid, oldSigner, _ := old.Current()

		registerOIDCSigningBackend(t, xkmssdk.NewStorageMemory())
		m, err := newOIDCSigningKeyManager(oidcSigningBackendName)
		require.NoError(t, err)

		newKid, _, _ := m.Current()
		require.NotEqual(t, oldKid, newKid)
		require.Len(t, m.JWKS().Key(oldKid), 1)
		require.Len(t, m.JWKS().Key(newKid), 1)

		digest := sha256.Sum256([]byte("pre-upgrade-token"))
		sig, err := oldSigner.Sign(rand.Reader, digest[:], crypto.SHA256)
		require.NoError(t, err)
		pub, err := m.PublicForKID(oldKid)
		require.NoError(t, err)
		ecPub, ok := pub.(*ecdsa.PublicKey)
		require.True(t, ok)
		require.True(t, ecdsa.VerifyASN1(ecPub, digest[:], sig))
	})

	t.Run("no default key", func(t *testing.T) {
		initOIDCSigningDefault(t)
		registerOIDCSigningBackend(t, xkmssdk.NewStorageMemory())
		m, err := newOIDCSigningKeyManager(oidcSigningBackendName)
		require.NoError(t, err)
		require.Len(t, m.JWKS().Keys, 1)
	})
}

// dedicatedOIDCSettings marks every tenant in its set as using a dedicated
// signing key.
type dedicatedOIDCSettings map[string]bool

func (d dedicatedOIDCSettings) OIDCSettingsFor(_ context.Context, tenantID string) (*iam.TenantOIDCConfig, error) {
	if !d[tenantID] {
		return nil, nil
	}
	return &iam.TenantOIDCConfig{DedicatedSigningKey: true}, nil
}

func (d dedicatedOIDCSettings) DedicatedSigningTenants(context.Context) ([]string, error) {
	ids := make([]string, 0, len(d))
	for id := range d {
		ids = append(ids, id)
	}
	return ids, nil
}

// TestOIDCSigningStore_TenantAdoptsExistingDefaultKey upgrades a node whose
// dedicated tenant key lives in the default backend: the tenant's manager on
// the replicated store keeps the pre-upgrade key, so a tenant token issued
// before the upgrade still verifies.
func TestOIDCSigningStore_TenantAdoptsExistingDefaultKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	const tenantID = "tenant-dedicated"

	initOIDCSigningDefault(t)
	old, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, tenantID)
	require.NoError(t, err)
	oldKid, oldSigner, _ := old.Current()
	digest := sha256.Sum256([]byte("pre-upgrade-tenant-token"))
	sig, err := oldSigner.Sign(rand.Reader, digest[:], crypto.SHA256)
	require.NoError(t, err)

	registerOIDCSigningBackend(t, xkmssdk.NewStorageMemory())
	global, err := newOIDCSigningKeyManager(oidcSigningBackendName)
	require.NoError(t, err)
	resolver, err := iam.NewTenantSigningKeyResolver(global, dedicatedOIDCSettings{tenantID: true})
	require.NoError(t, err)

	m, err := resolver.ManagerFor(context.Background(), tenantID)
	require.NoError(t, err)
	newKid, _, _ := m.Current()
	require.NotEqual(t, oldKid, newKid)

	pub, err := m.PublicForKID(oldKid)
	require.NoError(t, err)
	ecPub, ok := pub.(*ecdsa.PublicKey)
	require.True(t, ok)
	require.True(t, ecdsa.VerifyASN1(ecPub, digest[:], sig))
}

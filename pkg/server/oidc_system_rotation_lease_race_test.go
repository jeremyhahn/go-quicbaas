package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// oidcSystemKey is the blob path of a system OIDC signing key as go-xkms
// names it: version ("" for the first, "v2" onwards for a rotated one) and
// name in the algorithm segment, so a test has distinct names inside the
// system rotation fence's paths.
func oidcSystemKey(version, name string) string {
	cn := iam.OIDCSigningKeyCN
	if version != "" {
		cn += "--" + version
	}
	return "keys/software:signing:" + cn + ":" + name + ".key"
}

// tenantEntries is the part of a snapshotKV snapshot under tenant's paths:
// its version record, key material and key metadata.
func tenantEntries(snapshot map[string]string, tenant string) map[string]string {
	out := make(map[string]string)
	for k, v := range snapshot {
		if strings.Contains(k, tenant) {
			out[k] = v
		}
	}
	return out
}

// A system rotation passes the ambiguity check while no tenant holds a
// dedicated key, then pauses while it holds the system lease. Another node
// creates tenant acme's dedicated key and rotates it. When the system
// rotation resumes it is refused with SystemRotationAmbiguousError, the
// platform key does not move, and acme's record, material and metadata are
// as acme's own rotation left them: the system holder wrote and deleted
// nothing under acme's paths.
func TestOIDCSystemRotation_DedicatedKeyCreatedWhileLeaseHeldRefused(t *testing.T) {
	a, b, kv, _, _ := hostedRotationNodes(t)

	var once sync.Once
	paused, resume := make(chan struct{}), make(chan struct{})
	setLeaseHooks(t, func() {
		first := false
		once.Do(func() { first = true })
		if first {
			close(paused)
			<-resume
		}
	}, nil)
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(release)

	g1, _, _ := a.Current()
	rotated := make(chan error, 1)
	go func() { rotated <- a.Rotate() }()
	select {
	case <-paused:
	case err := <-rotated:
		t.Fatalf("system rotation finished before it held its lease: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("system rotation never took its lease")
	}

	tenants, err := iam.NewTenantSigningKeyResolver(b, dedicatedOIDCSettings{dedicatedRotationTenant: true})
	require.NoError(t, err)
	dbxkms.RegisterRotationDelegate(oidcSigningBackendName, oidcRotationDelegate(b, tenants))
	acme, err := tenants.ManagerFor(tenantctx.WithTenant(context.Background(), dedicatedRotationTenant), dedicatedRotationTenant)
	require.NoError(t, err)
	require.NoError(t, acme.Rotate())
	acmeKid, _, _ := acme.Current()
	acmeBefore := tenantEntries(snapshotKV(t, kv), dedicatedRotationTenant)
	require.NotEmpty(t, acmeBefore)

	release()
	select {
	case err = <-rotated:
	case <-time.After(10 * time.Second):
		t.Fatal("the resumed system rotation did not return")
	}
	var ambiguous *iam.SystemRotationAmbiguousError
	require.ErrorAs(t, err, &ambiguous)
	require.Equal(t, []string{dedicatedRotationTenant}, ambiguous.Tenants)

	require.Equal(t, acmeBefore, tenantEntries(snapshotKV(t, kv), dedicatedRotationTenant),
		"the system rotation changed acme's record, material or metadata")
	g, _, _ := a.Current()
	require.Equal(t, g1, g, "the platform key rotated")
	require.NoError(t, acme.Refresh())
	current, _, _ := acme.Current()
	require.Equal(t, acmeKid, current, "acme's key moved")
}

// The system rotation's fence owns the system key's paths only: a fenced
// put, create or delete of a tenant's key material or metadata is refused
// with *OIDCRotationFencedError and changes nothing, while the system key's
// own paths stay writable under the same fence. A tenant's fence likewise
// owns that tenant's paths only.
func TestOIDCKeyMaterialStore_FenceRefusesOtherTenantsPaths(t *testing.T) {
	kv, tx := oidcReplicatedKV(t)
	keys, err := newOIDCKeyMaterialStore(kv, tx)
	require.NoError(t, err)
	cn := iam.OIDCSigningKeyCN
	acmeID := dedicatedRotationTenant + ":software:signing:" + cn + ":ecdsa"
	acmeNext := dedicatedRotationTenant + ":software:signing:" + cn + "--v2:ecdsa"
	systemNext := "software:signing:" + cn + "--v2:ecdsa"
	acmeBlob := "keys/" + acmeID + ".key"
	_, err = keys.PutIfAbsent(context.Background(), acmeBlob, []byte("acme"))
	require.NoError(t, err)
	before := snapshotKV(t, kv)

	guarded, err := guardedOIDCStore(kv)
	require.NoError(t, err)
	lock := newOIDCRotationLock(guarded, guarded)

	refused := func(t *testing.T, err error) {
		t.Helper()
		var fenced *OIDCRotationFencedError
		require.ErrorAs(t, err, &fenced)
	}

	t.Run("system fence", func(t *testing.T) {
		ctx, unlock, err := lock(context.Background(), "", cn)
		require.NoError(t, err)
		defer unlock()
		got, err := keys.Get(ctx, acmeBlob)
		require.NoError(t, err)
		require.Equal(t, "acme", string(got))

		refused(t, keys.Put(ctx, acmeBlob, []byte("system")))
		refused(t, keys.Delete(ctx, acmeBlob))
		_, err = keys.PutIfAbsent(ctx, "keys/"+acmeNext+".key", []byte("system"))
		refused(t, err)
		refused(t, keys.Put(ctx, oidcKeyMetadataPrefix+acmeNext, []byte("system")))
		refused(t, keys.Put(ctx, "unrelated", []byte("system")))
		require.Equal(t, before, snapshotKVWithoutLeases(t, kv), "the system holder changed a tenant path")

		created, err := keys.PutIfAbsent(ctx, "keys/"+systemNext+".key", []byte("system"))
		require.NoError(t, err)
		require.True(t, created, "the system fence refused the system key's own path")
	})

	t.Run("tenant fence", func(t *testing.T) {
		ctx, unlock, err := lock(context.Background(), dedicatedRotationTenant, cn)
		require.NoError(t, err)
		defer unlock()
		_, err = keys.PutIfAbsent(ctx, "keys/other:software:signing:"+cn+"--v2:ecdsa.key", []byte("acme"))
		refused(t, err)
		_, err = keys.PutIfAbsent(ctx, "keys/software:signing:"+cn+"--v3:ecdsa.key", []byte("acme"))
		refused(t, err)
		created, err := keys.PutIfAbsent(ctx, "keys/"+acmeNext+".key", []byte("acme"))
		require.NoError(t, err)
		require.True(t, created, "the tenant fence refused its own key's path")
	})
}

// snapshotKVWithoutLeases is snapshotKV without the rotation leases, which
// a holder writes and renews as its own.
func snapshotKVWithoutLeases(t *testing.T, kv interface {
	List(context.Context, string) ([]string, error)
	Get(context.Context, string) ([]byte, error)
}) map[string]string {
	t.Helper()
	out := make(map[string]string)
	keys, err := kv.List(context.Background(), "")
	require.NoError(t, err)
	for _, k := range keys {
		if strings.HasPrefix(k, oidcRotationLeasePrefix) {
			continue
		}
		v, err := kv.Get(context.Background(), k)
		require.NoError(t, err)
		out[k] = string(v)
	}
	return out
}

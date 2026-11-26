//go:build !integration

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestOIDCSigningStore_ModuleRotateOfReplicatedKeyChangesNothing proves the
// storage seam gates every entry point: on two nodes sharing one replicated
// store, a rotate of oidc-replicated through the mounted go-xkms module's
// REST route, its KeystoreService/RotateKey and its rotate-all
// (RotateScopeKeys) does not take the rotation lease, so it changes neither
// the shared version record nor any key under oidc-signing-keys/, while the
// signing key manager's own rotation still works and reaches the peer.
func TestOIDCSigningStore_ModuleRotateOfReplicatedKeyChangesNothing(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	kv, tx := oidcReplicatedKV(t)
	srvA := &Server{config: &config.Config{}}
	a := bootOIDCSigningServer(t, srvA, kv, tx)
	b := bootOIDCSigningServer(t, &Server{config: &config.Config{}}, kv, tx)
	v1, _, _ := a.Current()
	before := snapshotKV(t, kv)

	hosted, err := srvA.hostXKMSModule(newXKMSPIVLoginModule(t, srvA))
	require.NoError(t, err)
	router := moduleRouter(hosted)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/keys/"+iam.OIDCSigningKeyCN+"/rotate?backend="+oidcSigningBackendName, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, "REST rotate: %s", rec.Body.String())
	require.Equal(t, before, snapshotKV(t, kv), "REST rotate changed the replicated store")

	services := capturedGRPCServices{}
	hosted.RegisterGRPC(services)
	_, err = services.call(t, "xkms.v1.KeystoreService", "RotateKey",
		&xkmsv1.RotateKeyRequest{KeyId: iam.OIDCSigningKeyCN, Backend: oidcSigningBackendName})
	require.ErrorContains(t, err, "not made under the rotation lease", "KeystoreService/RotateKey")
	require.Equal(t, before, snapshotKV(t, kv), "KeystoreService/RotateKey changed the replicated store")

	_, _ = services.call(t, "xkms.v1.KeystoreService", "RotateScopeKeys", &xkmsv1.RotateScopeKeysRequest{})
	require.Equal(t, before, snapshotKV(t, kv), "RotateScopeKeys changed the replicated store")

	require.NoError(t, b.Refresh())
	vB, _, _ := b.Current()
	require.Equal(t, v1, vB)

	require.NoError(t, a.Rotate())
	v2, _, _ := a.Current()
	require.NotEqual(t, v1, v2)
	require.NoError(t, b.Refresh())
	vB, _, _ = b.Current()
	require.Equal(t, v2, vB)
}

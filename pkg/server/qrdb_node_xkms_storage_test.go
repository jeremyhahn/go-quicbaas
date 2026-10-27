//go:build mcp && !integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/stretchr/testify/require"
)

// TestXKMSModule_ComposesStorageUnderRequireAuthz boots the production
// posture (security.require_authz=true) and asserts the composed go-xkms
// module opened its feature databases on the embedded node. Without the
// node's service context the module's storage calls carry no identity, the
// node refuses them as unauthenticated, and composition leaves every
// DAO-backed feature (passwords, SSH, key versions, trust roots) disabled
// (go-quicbaas#1727, Codex review 6886 on PR #1814).
func TestXKMSModule_ComposesStorageUnderRequireAuthz(t *testing.T) {
	cfg := productionPostureConfig(t, true)
	require.True(t, cfg.Security.RequireAuthz, "the production posture must enforce authorization")
	srv := bootProductionPostureServer(t, cfg)

	require.NotNil(t, srv.xkmsModule, "the boot must compose go-xkms's module")

	// The module composes its stores on first use; an anonymous request
	// consulting its authentication surface is the first use, and the
	// module refuses to authenticate it.
	identity, authErr := srv.xkmsModule.AuthenticateHTTP(httptest.NewRequest(http.MethodGet, "/", nil))
	require.Error(t, authErr, "an anonymous request must not authenticate")
	require.Nil(t, identity)

	stores := srv.xkmsModule.HostStores()
	require.NotNil(t, stores, "the module must compose its host stores during boot")
	// Features off for a reason other than storage (no PIN backend, CA
	// configuration or TPM2 backend in this posture) stay off; the four that
	// persist DAO entities must be on.
	require.NotNil(t, stores.PasswordStore, "the password store needs the node's DAO factory")
	require.NotNil(t, stores.SSH, "the SSH client needs the node's DAO factory")
	require.NotNil(t, stores.KeyVersions, "the key-version store needs the node's DAO factory")
	require.NotNil(t, stores.TrustRoots, "the trust-root store needs the node's DAO factory")
}

// TestXKMSModule_TenantRequestReachesHostedStorage drives a tenant caller's
// request into the composed module's DAOs on the production posture. The
// module opened its feature databases in the system tenant, and every DAO
// call runs through the host's service-context binder, so a request whose
// context names its own tenant must still reach storage in the system tenant:
// binding the system-tenant service principal while keeping the request's
// tenant is refused by the node's self-scope check, which fails tenant
// signing at KeyVersions.Load and every SSH record call (Codex review 7679
// on PR #1841).
func TestXKMSModule_TenantRequestReachesHostedStorage(t *testing.T) {
	const tenantID = "acme"
	cfg := productionPostureConfig(t, true)
	srv := bootProductionPostureServer(t, cfg)
	require.NotNil(t, srv.xkmsModule, "the boot must compose go-xkms's module")
	_, authErr := srv.xkmsModule.AuthenticateHTTP(httptest.NewRequest(http.MethodGet, "/", nil))
	require.Error(t, authErr, "an anonymous request must not authenticate")
	stores := srv.xkmsModule.HostStores()
	require.NotNil(t, stores)
	require.NotNil(t, stores.KeyVersions)
	require.NotNil(t, stores.SSH)

	// The request context a tenant caller's authenticated request carries:
	// its identity, and its own tenant as the request's target.
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:  tenantID + "-signer",
		TenantID: tenantID,
	})
	ctx = qrdbsdk.ContextWithTenant(ctx, tenantID)

	_, found, err := stores.KeyVersions.Load(ctx, tenantID, "pkcs8", "tenant-signing-key")
	require.NoError(t, err, "a tenant's key-version read must reach the module's storage")
	require.False(t, found, "no version was ever recorded for the key")

	pins, err := stores.SSH.KnownHosts().List(ctx, xkmsssh.ListKnownHostsRequest{})
	require.NoError(t, err, "a tenant's SSH record read must reach the module's storage")
	require.Empty(t, pins)
}

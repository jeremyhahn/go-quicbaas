//go:build mcp && !integration

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

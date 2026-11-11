//go:build !integration

package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// integrationNodeConfig is the configuration every node of the shared
// integration cluster boots from (test/integration/Dockerfile.node copies it
// to /etc/quicbaas/quicbaas.yaml).
var integrationNodeConfig = filepath.Join("..", "..", "test", "integration", "quicbaas.yaml")

// TestBuildQRDBConfig_IntegrationDaemonBindsTenants pins the posture the
// integration daemon gives its embedded qrdb node (#1727). The tenant is
// bound to a request only by the node's auth middleware: with the node's auth
// disabled, a tenant-admin bearer reaches /api/v1/tenants/{id}/roles with no
// tenant on its context and urlTenant refuses it with 403 "no tenant is bound
// to the request".
func TestBuildQRDBConfig_IntegrationDaemonBindsTenants(t *testing.T) {
	cfg, err := config.LoadFromFile(integrationNodeConfig)
	require.NoError(t, err)
	// The validator creates the storage directories; keep them off the host.
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.WALDir = t.TempDir()
	require.NoError(t, config.NewValidator().Validate(cfg),
		"the integration node must still pass the daemon's boot-time validation")

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.True(t, qcfg.Auth.IsEnabled(),
		"the integration node must run the qrdb auth middleware, the only layer that binds the caller's tenant")
}

// TestBuildQRDBConfig_AuthFollowsRequireAuthz covers both halves of the
// mapping: security.require_authz is the one switch for the node's
// authentication, so turning it off leaves no layer to bind a tenant.
func TestBuildQRDBConfig_AuthFollowsRequireAuthz(t *testing.T) {
	for _, requireAuthz := range []bool{true, false} {
		cfg := clusterTestConfig()
		cfg.Security.RequireAuthz = requireAuthz

		qcfg := mustBuildQRDBConfig(t, cfg, true)

		assert.Equal(t, requireAuthz, qcfg.Auth.IsEnabled(),
			"require_authz=%v must set the node's auth to the same value", requireAuthz)
	}
}

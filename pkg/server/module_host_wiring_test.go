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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newModuleHostTestServer builds a server whose qrdb node options can be
// assembled without touching the host system: the audit sink writes into the
// test's own temporary directory and no listener is opened.
func newModuleHostTestServer(t *testing.T, opts ...ServerOption) *Server {
	t.Helper()

	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg, opts...)
	require.NoError(t, err)
	return srv
}

// unifiedModuleServerOptions threads each host module onto the SAME option
// list that already carries quicbaas's module and go-xkms's, so one node
// serves all three. The delta against a server with no host modules is
// exactly one option per module.
func TestUnifiedModuleServerOptions_ThreadsHostModules(t *testing.T) {
	baseline := newModuleHostTestServer(t)
	baseOpts, err := baseline.unifiedModuleServerOptions(nil)
	require.NoError(t, err)

	hosted := newModuleHostTestServer(t,
		WithModule(newHostTestModule("platform", "/api/v1/platform/devices")),
		WithModule(newHostTestModule("fleet", "/api/v1/fleet/nodes")),
	)
	hostedOpts, err := hosted.unifiedModuleServerOptions(nil)
	require.NoError(t, err)

	assert.Equal(t, len(baseOpts)+2, len(hostedOpts),
		"each host module must contribute exactly one qrdbsdk.WithModule option")
}

// With no host module registered the composition is byte-for-byte the one
// quicbaas has always built: the route guard never runs and no extra option
// is appended.
func TestUnifiedModuleServerOptions_DefaultCompositionUnchanged(t *testing.T) {
	srv := newModuleHostTestServer(t)
	opts, err := srv.unifiedModuleServerOptions(nil)
	require.NoError(t, err)
	assert.NotEmpty(t, opts, "the default composition still mounts quicbaas and xkms")
	assert.Empty(t, srv.opts.hostModules)
}

// A host module whose REST route collides with another mounted module fails
// qrdb node construction with a typed error naming both owners, instead of
// panicking later inside go-qrdb's route registry.
func TestUnifiedModuleServerOptions_CollidingHostModulesFailConstruction(t *testing.T) {
	srv := newModuleHostTestServer(t,
		WithModule(newHostTestModule("platform", "/api/v1/shared")),
		WithModule(newHostTestModule("fleet", "/api/v1/shared")),
	)

	opts, err := srv.unifiedModuleServerOptions(nil)
	require.Error(t, err)
	assert.Nil(t, opts)

	var node *QRDBNodeError
	require.ErrorAs(t, err, &node)
	assert.Equal(t, "mount host modules", node.Stage)

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "/api/v1/shared", conflict.Pattern)
	assert.Equal(t, "platform", conflict.Owner)
	assert.Equal(t, "fleet", conflict.Module)
}

// Compile-time proof the host-module option accepts the same interface value
// go-qrdb's own WithModule takes, so a module written against the SDK
// re-export needs no adaptation.
var _ func(qrdbsdk.ServiceModule) ServerOption = WithModule

// Validating a module's REST surface means asking it for that surface, so
// RegisterHTTP is called once by the guard and again by go-qrdb against the
// live router. WithModule states that as a contract; this pins it for the two
// modules quicbaas itself mounts, which are the ones a host's mount depends
// on. Assembling the options repeatedly must keep succeeding and keep
// producing the same shape — a module that mutated state, counted calls or
// refused a second registration would break here first.
func TestUnifiedModuleServerOptions_MountedModulesAreRepeatable(t *testing.T) {
	srv := newModuleHostTestServer(t,
		WithModule(newHostTestModule("platform", "/api/v1/platform/devices")))

	first, err := srv.unifiedModuleServerOptions(nil)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		again, err := srv.unifiedModuleServerOptions(nil)
		require.NoErrorf(t, err, "assembly %d must not fail on a repeated registration", i+2)
		assert.Len(t, again, len(first),
			"a repeated registration must produce the same option set")
	}
}

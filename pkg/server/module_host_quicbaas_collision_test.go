//go:build rest

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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
)

// A host module that claims a route go-quicbaas's OWN module already serves
// fails qrdb node construction, naming quicbaas as the incumbent owner. This
// is the acceptance test for the host seam's headline safety property: a
// downstream host cannot silently shadow the broker's REST surface, and it
// learns which route to move rather than watching a panic surface from deep
// inside go-qrdb's route registry during REST server construction.
//
// The colliding pattern is taken from the live route table (ModuleRoutes),
// not hardcoded, so the test keeps testing a REAL collision as the table
// evolves.
func TestUnifiedModuleServerOptions_HostModuleCollidingWithQuicBaaSFails(t *testing.T) {
	routes := restapi.ModuleRoutes()
	require.NotEmpty(t, routes, "the quicbaas module route table must not be empty on rest builds")
	victim := routes[0]

	srv := newModuleHostTestServer(t, WithModule(&hostTestModule{
		name: "trusted-platform",
		routes: []hostTestRoute{
			{method: victim.Method, pattern: victim.Pattern},
		},
	}))

	opts, err := srv.unifiedModuleServerOptions(nil)
	require.Error(t, err, "a host module may not claim a route quicbaas already serves")
	assert.Nil(t, opts)

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, apimodule.ModuleName, conflict.Owner,
		"quicbaas's own module must be reported as the incumbent owner")
	assert.Equal(t, "trusted-platform", conflict.Module)
	assert.Equal(t, victim.Method, conflict.Method)
	assert.Equal(t, victim.Pattern, conflict.Pattern)
}

// A host module whose routes live under its own prefix composes cleanly with
// both quicbaas's and go-xkms's full REST surfaces — the guard must not
// produce false positives against the real, large route tables.
func TestUnifiedModuleServerOptions_HostModuleWithOwnPrefixComposes(t *testing.T) {
	srv := newModuleHostTestServer(t,
		WithModule(newHostTestModule("trusted-platform", "/api/v1/trusted-platform/devices")))

	opts, err := srv.unifiedModuleServerOptions(nil)
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

// The reserved namespace must leave the prefix a host module is TOLD to use
// wide open. quicbaas's own module is the largest real route table available,
// and the /api/v1 part of it is exactly the shape the docs hand a host, so
// none of it may be refused. (quicbaas's own routes are never actually
// subjected to this check — it applies to host modules only — so this pins
// that the documented host shape stays mountable, not that quicbaas is
// exempt.)
func TestQRDBReservedRouteSegments_LeaveTheDocumentedHostPrefixOpen(t *testing.T) {
	routes := restapi.ModuleRoutes()
	require.NotEmpty(t, routes)

	var refused []string
	checked := 0
	for _, spec := range routes {
		if !strings.HasPrefix(spec.Pattern, "/api/") {
			continue
		}
		checked++
		if isReservedRoute(spec.Pattern) {
			refused = append(refused, spec.Method+" "+spec.Pattern)
		}
	}
	require.NotZero(t, checked, "sanity: the module table must carry /api routes")
	assert.Emptyf(t, refused,
		"the documented host prefix must never be reserved, or no host module "+
			"could be mounted in the shape the docs prescribe: %v", refused)
}

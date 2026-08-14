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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// TestNewServer_RegistersQuicBaaSPermissions asserts the composition root
// contributes QuicBaaS's permission vocabulary to qrdb's global registry.
//
// The per-transport guard tests (pkg/api/{grpc,rest,quic,mcp}) call
// quicbaaspermission.Register themselves because importing pkg/server from
// those packages would be an import cycle. This test is the other end of that
// arrangement: it proves PRODUCTION performs the registration, so the guards
// are not asserting a property that only ever holds in tests.
//
// server.NewServer is the sole composition root — cmd/quicbaasd (serve and
// init), cmd/quicbaas embedded, and the Wails bindings all construct through
// it — so registering there covers every entry point.
func TestNewServer_RegistersQuicBaaSPermissions(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Level = "error"

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NotNil(t, srv)

	for _, perm := range quicbaaspermission.All() {
		assert.Truef(t, qrdbsdk.IsPermissionRegistered(perm),
			"NewServer did not register %q; every transport authz table that maps onto "+
				"it resolves a permission no role can hold", perm)
	}
}

// TestNewServer_RegistersOnlyTheDeclaredPermissions is the negative twin: the
// composition root must contribute EXACTLY the pkg/permission list and nothing
// else. A stray registration elsewhere in the startup path would put a
// permission in every role picker and in permissions:list that no transport
// enforces — the inverse of the dead-endpoint bug, and just as invisible.
//
// The check is by resource family: qrdb owns every family it registers itself,
// so any family present in the registry that is neither qrdb's nor declared in
// pkg/permission.All means something registered behind the single list.
func TestNewServer_RegistersOnlyTheDeclaredPermissions(t *testing.T) {
	cfg := config.Default()
	cfg.Logging.Level = "error"

	_, err := NewServer(cfg)
	require.NoError(t, err)

	declared := make(map[qrdbsdk.Permission]struct{}, len(quicbaaspermission.All()))
	for _, perm := range quicbaaspermission.All() {
		declared[perm] = struct{}{}
	}

	for _, perm := range qrdbsdk.AllPermissions() {
		if _, ours := declared[perm]; ours {
			continue
		}
		// Everything else must belong to a qrdb-owned family, i.e. it must not
		// share a resource family with anything we declared.
		for _, mine := range quicbaaspermission.All() {
			assert.NotEqualf(t, qrdbsdk.PermissionResource(mine), qrdbsdk.PermissionResource(perm),
				"permission %q is registered in the %q family but is not in "+
					"pkg/permission.All; the single list has been bypassed",
				perm, qrdbsdk.PermissionResource(mine))
		}
	}
}

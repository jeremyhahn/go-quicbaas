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

package quicbaas_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// This file is the downstream-host acceptance test for the module-host seam.
// It lives in the external test package quicbaas_test and touches NOTHING but
// the quicbaas SDK surface — no go-qrdb, no go-xkms, no go-quicbaas/pkg —
// which is precisely the import budget go-trusted-platform is held to. If a
// host cannot write and mount a module with only these symbols, the seam is
// incomplete, and this test stops compiling.

// deviceModule stands in for go-trusted-platform's own capability plane: a
// module written outside go-quicbaas entirely, mounted onto the same unified
// node that already carries the quicbaas and xkms modules.
type deviceModule struct {
	quicbaas.BaseHostModule
}

// devicePermission is the qrdb-canonical resource:action a host declares at
// the registration site; the node's single authorizer renders the decision.
const devicePermission quicbaas.Permission = "device:read"

func (m *deviceModule) Name() string { return "trusted-platform" }

// RegisterHTTP registers every route through the permission-recording
// registrar: Handle for authorized routes, HandlePublic for the genuine
// first-contact surface. Registering straight onto the embedded chi.Router
// would bypass the permission registry and the node's default-deny startup
// assertion would then refuse to serve.
func (m *deviceModule) RegisterHTTP(r *quicbaas.ModuleHTTPRegistrar) {
	r.Handle(http.MethodGet, "/api/v1/devices", devicePermission,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"devices":[]}`))
		})
	r.HandlePublic(http.MethodGet, "/api/v1/devices/health",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
}

// MCPToolPermissions declares the module's MCP tool authorization. Without it
// a module tool stays fail-closed on an authenticated node.
func (m *deviceModule) MCPToolPermissions() map[string]quicbaas.Permission {
	return map[string]quicbaas.Permission{"device_list": devicePermission}
}

// Compile-time proof the host's module satisfies the seam's contracts using
// only re-exported SDK types.
var (
	_ quicbaas.HostModule               = (*deviceModule)(nil)
	_ quicbaas.ModuleMCPToolPermissions = (*deviceModule)(nil)
)

// capturedRoute is one (method, pattern) registration observed on the shared
// registrar, with the permission the module declared for it.
type capturedRoute struct {
	permission quicbaas.Permission
	public     bool
	optional   bool
}

// mountModule replays a module's RegisterHTTP through a permission-recording
// registrar wired the same way the unified node wires its own: the callbacks
// record the declared permission AND mount the handler on the live router, so
// the returned router actually serves what the module registered.
func mountModule(m quicbaas.HostModule) (*chi.Mux, map[string]capturedRoute) {
	router := chi.NewRouter()
	seen := map[string]capturedRoute{}

	reg := quicbaas.NewModuleHTTPRegistrar(router,
		func(method, pattern string, perm quicbaas.Permission, h http.HandlerFunc) {
			seen[method+" "+pattern] = capturedRoute{permission: perm}
			router.Method(method, pattern, h)
		},
		func(method, pattern string, h http.HandlerFunc) {
			seen[method+" "+pattern] = capturedRoute{public: true}
			router.Method(method, pattern, h)
		},
		func(method, pattern string, h http.HandlerFunc) {
			seen[method+" "+pattern] = capturedRoute{optional: true}
			router.Method(method, pattern, h)
		},
	)
	m.RegisterHTTP(reg)
	return router, seen
}

// A module written entirely against the SDK re-exports mounts on the shared
// registrar and its routes actually serve, carrying the permissions it
// declared at each registration site.
func TestHostModule_MountsAndServes(t *testing.T) {
	mod := &deviceModule{}
	router, seen := mountModule(mod)

	require.Contains(t, seen, "GET /api/v1/devices")
	assert.Equal(t, devicePermission, seen["GET /api/v1/devices"].permission,
		"an authorized route must carry the permission the module declared")
	require.Contains(t, seen, "GET /api/v1/devices/health")
	assert.True(t, seen["GET /api/v1/devices/health"].public,
		"HandlePublic must record the route as public for the default-deny assertion")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"devices":[]}`, rec.Body.String())

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/devices/health", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	assert.Equal(t, map[string]quicbaas.Permission{"device_list": devicePermission},
		mod.MCPToolPermissions())
}

// The canonical mounting call: a host builds the node through
// quicbaas.NewServer and threads its module in with quicbaas.WithModule. The
// server accepts the registration, so the module rides the SAME node as the
// quicbaas and xkms modules.
func TestNewServer_MountsHostModule(t *testing.T) {
	srv, err := quicbaas.NewServer(quicbaas.DefaultServerConfig(),
		quicbaas.WithModule(&deviceModule{}))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// A host module may not take a name go-quicbaas or go-xkms already registers
// on every node: the conflict is a typed error at the host's own composition
// site, not a panic from inside node construction.
func TestNewServer_RejectsReservedHostModuleNames(t *testing.T) {
	for _, name := range []string{quicbaas.ServiceModuleName, quicbaas.XKMSServiceModuleName} {
		t.Run(name, func(t *testing.T) {
			srv, err := quicbaas.NewServer(quicbaas.DefaultServerConfig(),
				quicbaas.WithModule(&renamedModule{name: name}))
			require.Error(t, err)
			assert.Nil(t, srv)

			var conflict *quicbaas.HostModuleNameConflictError
			require.ErrorAs(t, err, &conflict)
			assert.Equal(t, name, conflict.Name)
			assert.NotEmpty(t, conflict.Owner, "the incumbent owner must be named")
		})
	}
}

// A nil registration and an unnamed module are rejected the same way, so a
// host never gets a silently unmounted capability plane.
func TestNewServer_RejectsUnusableHostModules(t *testing.T) {
	_, err := quicbaas.NewServer(quicbaas.DefaultServerConfig(), quicbaas.WithModule(nil))
	require.ErrorAs(t, err, new(*quicbaas.NilHostModuleError))

	_, err = quicbaas.NewServer(quicbaas.DefaultServerConfig(),
		quicbaas.WithModule(&renamedModule{name: ""}))
	require.ErrorAs(t, err, new(*quicbaas.UnnamedHostModuleError))
}

// Two host modules cannot share a name: the second registration is reported
// against the first.
func TestNewServer_RejectsDuplicateHostModuleNames(t *testing.T) {
	_, err := quicbaas.NewServer(quicbaas.DefaultServerConfig(),
		quicbaas.WithModule(&renamedModule{name: "trusted-platform"}),
		quicbaas.WithModule(&renamedModule{name: "trusted-platform"}),
	)
	var conflict *quicbaas.HostModuleNameConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "trusted-platform", conflict.Name)
}

// renamedModule is a name-only host module used to exercise the naming guard.
type renamedModule struct {
	quicbaas.BaseHostModule
	name string
}

func (m *renamedModule) Name() string { return m.name }

// The SDK re-exports the hook argument types for RegisterGRPC and RegisterMCP
// so a module's method set is spelled entirely in quicbaas terms. (A module
// still imports grpc and mcp-go for its generated code and tool builders —
// see the docs; these aliases only cover the signatures.)
func TestModuleHookTypes_AreReExported(t *testing.T) {
	var registerGRPC func(quicbaas.ModuleGRPCRegistrar)
	var registerMCP func(*quicbaas.ModuleMCPServer)
	assert.Nil(t, registerGRPC)
	assert.Nil(t, registerMCP)
}

// A module whose Name is stable is a HostModule regardless of which optional
// extensions it implements; BaseHostModule supplies the rest.
func TestBaseHostModule_SuppliesEveryOtherHook(t *testing.T) {
	var m quicbaas.HostModule = &renamedModule{name: "minimal"}
	assert.Equal(t, "minimal", m.Name())

	// The no-op hooks must be callable without a live node.
	m.RegisterHTTP(quicbaas.NewModuleHTTPRegistrar(chi.NewRouter(),
		func(string, string, quicbaas.Permission, http.HandlerFunc) {},
		func(string, string, http.HandlerFunc) {},
		func(string, string, http.HandlerFunc) {}))
	require.NoError(t, m.Start(context.Background()))
	require.NoError(t, m.Stop(context.Background()))
}

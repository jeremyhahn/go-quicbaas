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
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// hostTestModule is a minimal downstream capability module: it embeds
// go-qrdb's BaseModule (so it satisfies ServiceModule as the seam evolves)
// and registers a handful of REST routes through the permission-recording
// registrar, exactly as a real host module must.
type hostTestModule struct {
	qrdbsdk.BaseModule

	name   string
	routes []hostTestRoute
}

type hostTestRoute struct {
	method  string
	pattern string
	public  bool
}

func (m *hostTestModule) Name() string { return m.name }

func (m *hostTestModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	for _, rt := range m.routes {
		h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
		if rt.public {
			r.HandlePublic(rt.method, rt.pattern, h)
			continue
		}
		r.Handle(rt.method, rt.pattern, qrdbsdk.Permission("device:read"), h)
	}
}

// newHostTestModule builds a host module with one authorized route under the
// caller-supplied prefix.
func newHostTestModule(name, prefix string) *hostTestModule {
	return &hostTestModule{
		name: name,
		routes: []hostTestRoute{
			{method: http.MethodGet, pattern: prefix},
			{method: http.MethodGet, pattern: prefix + "/health", public: true},
		},
	}
}

// A host module registered via WithModule is recorded on the server options
// so unifiedModuleServerOptions can mount it on the unified qrdb node.
func TestWithModule_RecordsHostModule(t *testing.T) {
	mod := newHostTestModule("platform", "/api/v1/platform/devices")

	srv, err := NewServer(config.Default(), WithModule(mod))
	require.NoError(t, err)
	require.Len(t, srv.opts.hostModules, 1,
		"WithModule must record the host module on serverOptions")
	assert.Equal(t, "platform", srv.opts.hostModules[0].Name())
}

// WithModule is repeatable: each call appends, preserving registration order
// so the node mounts modules in the order the host declared them.
func TestWithModule_RepeatableAppendsInOrder(t *testing.T) {
	srv, err := NewServer(config.Default(),
		WithModule(newHostTestModule("platform", "/api/v1/platform")),
		WithModule(newHostTestModule("fleet", "/api/v1/fleet")),
	)
	require.NoError(t, err)
	require.Len(t, srv.opts.hostModules, 2)
	assert.Equal(t, "platform", srv.opts.hostModules[0].Name())
	assert.Equal(t, "fleet", srv.opts.hostModules[1].Name())
}

// A server built without WithModule mounts no host modules, leaving the
// default quicbaas + xkms composition untouched.
func TestWithModule_DefaultNoHostModules(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)
	assert.Empty(t, srv.opts.hostModules)
}

// Every invalid host-module registration fails NewServer with a typed error
// naming the offender, rather than panicking later inside go-qrdb's own
// WithModule when the node is built.
func TestNewServer_RejectsInvalidHostModules(t *testing.T) {
	tests := []struct {
		name   string
		opts   []ServerOption
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil module",
			opts: []ServerOption{WithModule(nil)},
			assert: func(t *testing.T, err error) {
				var target *NilHostModuleError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, 0, target.Index)
			},
		},
		{
			name: "empty name",
			opts: []ServerOption{WithModule(&hostTestModule{name: ""})},
			assert: func(t *testing.T, err error) {
				var target *UnnamedHostModuleError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, 0, target.Index)
			},
		},
		{
			name: "name reserved by quicbaas",
			opts: []ServerOption{WithModule(newHostTestModule(apimodule.ModuleName, "/x"))},
			assert: func(t *testing.T, err error) {
				var target *HostModuleNameConflictError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, apimodule.ModuleName, target.Name)
				assert.Equal(t, "go-quicbaas", target.Owner)
			},
		},
		{
			name: "name reserved by xkms",
			opts: []ServerOption{WithModule(newHostTestModule(xkmssdk.ServiceModuleName, "/x"))},
			assert: func(t *testing.T, err error) {
				var target *HostModuleNameConflictError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, xkmssdk.ServiceModuleName, target.Name)
				assert.Equal(t, "go-xkms", target.Owner)
			},
		},
		{
			name: "duplicate host module name",
			opts: []ServerOption{
				WithModule(newHostTestModule("platform", "/api/v1/a")),
				WithModule(newHostTestModule("platform", "/api/v1/b")),
			},
			assert: func(t *testing.T, err error) {
				var target *HostModuleNameConflictError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, "platform", target.Name)
				assert.Contains(t, target.Owner, "earlier")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := NewServer(config.Default(), tc.opts...)
			require.Error(t, err, "NewServer must reject the registration")
			assert.Nil(t, srv)
			tc.assert(t, err)
		})
	}
}

// Distinct modules whose route tables do not overlap compose cleanly: the
// guard reports no conflict.
func TestAssertNoHostModuleRouteConflicts_DisjointModulesCompose(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(
		[]qrdbsdk.ServiceModule{newHostTestModule("mounted", "/api/v1/mounted")},
		[]qrdbsdk.ServiceModule{
			newHostTestModule("platform", "/api/v1/platform/devices"),
			newHostTestModule("fleet", "/api/v1/fleet/nodes"),
		})
	assert.NoError(t, err)
}

// Overlap between the ALREADY-mounted modules (quicbaas's own and go-xkms's)
// is not the host's problem: it predates this seam, only go-qrdb can rule on
// it, and reporting it here would newly fail a composition that boots today.
// The guard stays silent about it and still catches the host's own collision.
func TestAssertNoHostModuleRouteConflicts_IgnoresPreexistingMountedOverlap(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{
		newHostTestModule("quicbaas-like", "/api/v1/tenants"),
		newHostTestModule("xkms-like", "/api/v1/tenants"),
	}

	err := assertNoHostModuleRouteConflicts(mounted,
		[]qrdbsdk.ServiceModule{newHostTestModule("platform", "/api/v1/platform")})
	assert.NoError(t, err, "overlap between mounted modules must not fail a host mount")

	err = assertNoHostModuleRouteConflicts(mounted,
		[]qrdbsdk.ServiceModule{newHostTestModule("platform", "/api/v1/tenants")})
	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict,
		"a host claiming a mounted module's route is still a conflict")
	assert.Equal(t, "quicbaas-like", conflict.Owner, "the first claimant owns the route")
}

// Two modules claiming the same (method, pattern) pair fail construction with
// a typed error that names BOTH owners — the loud construction-time collision
// contract go-qrdb's route registry enforces with a panic, surfaced earlier
// and actionably.
func TestAssertNoHostModuleRouteConflicts_CollidingRouteFailsConstruction(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{
		newHostTestModule("platform", "/api/v1/shared"),
		newHostTestModule("fleet", "/api/v1/shared"),
	})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, http.MethodGet, conflict.Method)
	assert.Equal(t, "/api/v1/shared", conflict.Pattern)
	assert.Equal(t, "platform", conflict.Owner, "the first claimant owns the route")
	assert.Equal(t, "fleet", conflict.Module, "the second claimant is reported as the offender")
	assert.Contains(t, err.Error(), "/api/v1/shared")
}

// A public route collides exactly like an authorized one: the registrar
// records both kinds in the same namespace, so HandlePublic cannot be used to
// slip past the guard.
func TestAssertNoHostModuleRouteConflicts_PublicRouteCollides(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{
		&hostTestModule{name: "platform", routes: []hostTestRoute{
			{method: http.MethodGet, pattern: "/api/v1/ping", public: true},
		}},
		&hostTestModule{name: "fleet", routes: []hostTestRoute{
			{method: http.MethodGet, pattern: "/api/v1/ping"},
		}},
	})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "/api/v1/ping", conflict.Pattern)
}

// A nil entry in the module set is skipped rather than dereferenced: the
// name-level guard already rejected it at NewServer, and the route guard must
// not panic if it is ever handed one.
func TestAssertNoHostModuleRouteConflicts_SkipsNilModules(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(
		[]qrdbsdk.ServiceModule{nil},
		[]qrdbsdk.ServiceModule{nil, newHostTestModule("platform", "/api/v1/platform")})
	assert.NoError(t, err)
}

// The typed errors read as complete sentences that name the offending module,
// route or index — the message is the host's only diagnostic when the failure
// surfaces from its own composition site.
func TestHostModuleErrors_Messages(t *testing.T) {
	assert.Contains(t, (&NilHostModuleError{Index: 2}).Error(), "index 2")
	assert.Contains(t, (&UnnamedHostModuleError{Index: 1}).Error(), "index 1")

	conflict := &HostModuleNameConflictError{Name: "xkms", Owner: "go-xkms"}
	assert.Contains(t, conflict.Error(), `"xkms"`)
	assert.Contains(t, conflict.Error(), "go-xkms")

	route := &HostModuleRouteConflictError{
		Method: http.MethodPost, Pattern: "/api/v1/x", Module: "fleet", Owner: "platform",
	}
	assert.Contains(t, route.Error(), "POST /api/v1/x")
	assert.Contains(t, route.Error(), `"fleet"`)
	assert.Contains(t, route.Error(), `"platform"`)

	// The NewServer wrapper preserves errors.As traversal to the typed cause.
	_, err := NewServer(config.Default(), WithModule(nil))
	require.Error(t, err)
	assert.True(t, errors.As(err, new(*NilHostModuleError)))
}

// bypassModule registers its routes straight onto the registrar's embedded
// chi.Router, skipping the permission-recording Handle/HandlePublic path.
type bypassModule struct {
	qrdbsdk.BaseModule

	name   string
	routes []hostTestRoute
}

func (m *bypassModule) Name() string { return m.name }

func (m *bypassModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	for _, rt := range m.routes {
		r.Method(rt.method, rt.pattern,
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
	}
}

// A host module that registers around the permission-recording registrar is
// rejected. Such a route declares no permission and, because chi silently
// replaces the handler for a pattern it already holds, it could take over a
// route another module serves while the registry still names that other
// module — invisible to both the collision guard and the node's default-deny
// startup assertion.
func TestAssertNoHostModuleRouteConflicts_RejectsRegistrarBypass(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{
		&bypassModule{name: "platform", routes: []hostTestRoute{
			{method: http.MethodGet, pattern: "/api/v1/platform/devices"},
		}},
	})

	var bypass *HostModuleUnregisteredRouteError
	require.ErrorAs(t, err, &bypass)
	assert.Equal(t, "platform", bypass.Module)
	assert.Equal(t, http.MethodGet, bypass.Method)
	assert.Equal(t, "/api/v1/platform/devices", bypass.Pattern)
	assert.Contains(t, err.Error(), "HandlePublic")
}

// A MOUNTED module's bypass route is not rejected — quicbaas's and go-xkms's
// registrations predate this seam — but it still occupies the router, so a
// host claiming the same route is reported against it.
func TestAssertNoHostModuleRouteConflicts_MountedBypassStillClaimsTheRoute(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{
		&bypassModule{name: "quicbaas-like", routes: []hostTestRoute{
			{method: http.MethodGet, pattern: "/api/v1/shared"},
		}},
	}

	err := assertNoHostModuleRouteConflicts(mounted,
		[]qrdbsdk.ServiceModule{newHostTestModule("platform", "/api/v1/elsewhere")})
	assert.NoError(t, err, "a mounted module's bypass route must not fail a host mount")

	err = assertNoHostModuleRouteConflicts(mounted,
		[]qrdbsdk.ServiceModule{newHostTestModule("platform", "/api/v1/shared")})
	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "quicbaas-like", conflict.Owner)
}

// Renaming a path parameter does not create a new route: chi matches a
// parameter by position, so "/devices/{id}" and "/devices/{deviceID}" are the
// same route and the later registration silently replaces the earlier one.
// The guard compares route SHAPES so this collision cannot slip through, and
// the error reports both spellings.
func TestAssertNoHostModuleRouteConflicts_RenamedPathParamCollides(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "quicbaas-like",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/tenants/{tenant_id}"}},
	}}

	err := assertNoHostModuleRouteConflicts(mounted, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/tenants/{tid}"}},
	}})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "/api/v1/tenants/{tid}", conflict.Pattern)
	assert.Equal(t, "/api/v1/tenants/{tenant_id}", conflict.OwnerPattern)
	assert.Contains(t, err.Error(), "a path parameter's name is not part of the match")
}

// A regex constraint IS part of chi's match, so "{id:[0-9]+}" and "{name}"
// route independently and must NOT be reported as a collision — the guard
// must not produce false positives that block a legitimate host module.
func TestAssertNoHostModuleRouteConflicts_RegexConstrainedParamIsDistinct(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "quicbaas-like",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/x/{id:[0-9]+}"}},
	}}

	err := assertNoHostModuleRouteConflicts(mounted, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/x/{name}"}},
	}})
	assert.NoError(t, err)
}

// A regex constraint containing a brace quantifier is a valid chi pattern and
// must not be mistaken for a malformed one: the guard hands patterns to chi
// itself rather than parsing them.
func TestAssertNoHostModuleRouteConflicts_RegexQuantifierIsValid(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/x/{id:[0-9]{3}}"}},
	}})
	assert.NoError(t, err)
}

// A pattern chi refuses (here: one that does not begin with "/") fails the
// mount with a named error instead of panicking the process from inside node
// construction, which is what chi does on its own.
func TestAssertNoHostModuleRouteConflicts_InvalidPatternIsTyped(t *testing.T) {
	err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "api/v1/no-leading-slash"}},
	}})

	var invalid *HostModuleInvalidRouteError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "platform", invalid.Module)
	assert.Equal(t, "api/v1/no-leading-slash", invalid.Pattern)
	assert.NotEmpty(t, invalid.Reason, "the error must carry chi's own explanation")
}

// A wildcard route and a parameter route at the same position are distinct to
// chi, so mounting both must not be reported as a collision.
func TestAssertNoHostModuleRouteConflicts_WildcardAndParamAreDistinct(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "quicbaas-like",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/kv/*"}},
	}}

	err := assertNoHostModuleRouteConflicts(mounted, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/kv/{key}"}},
	}})
	assert.NoError(t, err)
}

// The method is part of the route identity: the same path under a different
// method is a different route.
func TestAssertNoHostModuleRouteConflicts_DifferentMethodIsDistinct(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "quicbaas-like",
		routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/shared"}},
	}}

	err := assertNoHostModuleRouteConflicts(mounted, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: http.MethodPost, pattern: "/api/v1/shared"}},
	}})
	assert.NoError(t, err)
}

// rawRouterModule reaches past the registrar and drives the embedded
// chi.Router directly, the way a module can misuse the seam.
type rawRouterModule struct {
	qrdbsdk.BaseModule

	name string
	run  func(r *qrdbsdk.ModuleHTTPRegistrar)
}

func (m *rawRouterModule) Name() string { return m.name }

func (m *rawRouterModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) { m.run(r) }

// chi panics on an unsupported HTTP method, and a module reaching past the
// registrar hits that inside RegisterHTTP — outside the shadow-router insert.
// The dry run must still convert it to a named error rather than letting the
// panic escape node construction.
func TestAssertNoHostModuleRouteConflicts_PanicInsideRegisterHTTPIsTyped(t *testing.T) {
	mod := &rawRouterModule{name: "platform", run: func(r *qrdbsdk.ModuleHTTPRegistrar) {
		r.Method("BOGUS", "/api/v1/platform", http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {}))
	}}

	var err error
	require.NotPanics(t, func() {
		err = assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{mod})
	}, "a module's registration panic must not escape the guard")

	var invalid *HostModuleInvalidRouteError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "platform", invalid.Module)
	assert.Contains(t, invalid.Reason, "BOGUS")
}

// A host module registers AFTER quicbaas's and go-xkms's, so the shared router
// already holds routes and chi refuses further router-level middleware. The
// probe is seeded to match that state, so a host calling Use fails the mount
// here instead of panicking during real registration.
func TestAssertNoHostModuleRouteConflicts_HostMiddlewareIsRejected(t *testing.T) {
	mod := &rawRouterModule{name: "platform", run: func(r *qrdbsdk.ModuleHTTPRegistrar) {
		r.Use(func(next http.Handler) http.Handler { return next })
	}}

	var err error
	require.NotPanics(t, func() {
		err = assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{mod})
	})

	var invalid *HostModuleInvalidRouteError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "platform", invalid.Module)
	assert.Contains(t, invalid.Reason, "middlewares")
}

// The mounted modules register first and legitimately meet an EMPTY router, so
// they are probed against one: middleware from quicbaas or go-xkms must not be
// reported as a failure just because a host module is also being mounted.
func TestAssertNoHostModuleRouteConflicts_MountedMiddlewareIsAllowed(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&rawRouterModule{
		name: "quicbaas-like",
		run: func(r *qrdbsdk.ModuleHTTPRegistrar) {
			r.Use(func(next http.Handler) http.Handler { return next })
		},
	}}

	err := assertNoHostModuleRouteConflicts(mounted,
		[]qrdbsdk.ServiceModule{newHostTestModule("platform", "/api/v1/platform")})
	assert.NoError(t, err)
}

// The probe's own seed route is invisible: it must not be reported as a
// bypassing registration by the module that never made it.
func TestCaptureModuleRoutes_SeedRouteIsNotAttributedToTheModule(t *testing.T) {
	routes, err := captureModuleRoutes(newHostTestModule("platform", "/api/v1/platform"), true)
	require.NoError(t, err)
	assert.Empty(t, routes.bypassed, "the probe's own seed route must not surface as a module route")
	assert.Len(t, routes.declared, 2)
}

// chi stores and reports methods upper-cased. A module spelling one in lower
// case must still be attributed correctly, so a later collision names the
// incumbent instead of an empty owner.
func TestAssertNoHostModuleRouteConflicts_MethodCaseIsNormalized(t *testing.T) {
	mounted := []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "quicbaas-like",
		routes: []hostTestRoute{{method: "get", pattern: "/api/v1/shared"}},
	}}

	err := assertNoHostModuleRouteConflicts(mounted, []qrdbsdk.ServiceModule{&hostTestModule{
		name:   "platform",
		routes: []hostTestRoute{{method: "GET", pattern: "/api/v1/shared"}},
	}})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "quicbaas-like", conflict.Owner,
		"a lower-case method must not lose the incumbent's ownership")
	assert.Equal(t, http.MethodGet, conflict.Method)
}

// The invalid-registration error reads correctly whether or not the offending
// route could be attributed.
func TestHostModuleInvalidRouteError_Message(t *testing.T) {
	attributed := &HostModuleInvalidRouteError{
		Module: "platform", Method: "GET", Pattern: "bad", Reason: "chi: boom",
	}
	assert.Contains(t, attributed.Error(), "GET bad")
	assert.Contains(t, attributed.Error(), "chi: boom")

	unattributed := &HostModuleInvalidRouteError{Module: "platform", Reason: "chi: boom"}
	assert.Contains(t, unattributed.Error(), `"platform"`)
	assert.Contains(t, unattributed.Error(), "chi: boom")
	assert.NotContains(t, unattributed.Error(), "()")
}

// The probe's seed route is identified by handler identity, not by path. A
// module that registers over that path — replacing the seeded GET handler, or
// adding another method on it — must be reported as the bypassing
// registration it is, not hidden behind the sentinel's name.
func TestCaptureModuleRoutes_SentinelPathCannotHideABypass(t *testing.T) {
	const sentinelPath = "/__quicbaas_module_host_probe__"

	tests := []struct {
		name   string
		method string
	}{
		{"replacing the seeded handler", http.MethodGet},
		{"another method on the seeded path", http.MethodPost},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mod := &rawRouterModule{name: "platform", run: func(r *qrdbsdk.ModuleHTTPRegistrar) {
				r.Method(tc.method, sentinelPath, http.HandlerFunc(
					func(w http.ResponseWriter, _ *http.Request) {}))
			}}

			routes, err := captureModuleRoutes(mod, true)
			require.NoError(t, err)
			require.Len(t, routes.bypassed, 1,
				"a registration on the sentinel path must still be reported")
			assert.Equal(t, sentinelPath, routes.bypassed[0].pattern)

			err = assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{mod})
			var bypass *HostModuleUnregisteredRouteError
			require.ErrorAs(t, err, &bypass)
			assert.Equal(t, sentinelPath, bypass.Pattern)
		})
	}
}

// A typed nil — WithModule((*deviceModule)(nil)) — puts a non-nil interface
// header around a nil pointer, so a plain `m == nil` misses it and Name()
// dereferences a nil receiver. WithModule promises a typed error for a missing
// module, so this must not panic.
func TestNewServer_RejectsTypedNilHostModule(t *testing.T) {
	var typedNil *hostTestModule

	var (
		srv *Server
		err error
	)
	require.NotPanics(t, func() {
		srv, err = NewServer(config.Default(), WithModule(typedNil))
	}, "a typed-nil module must be rejected, not dereferenced")

	assert.Nil(t, srv)
	var target *NilHostModuleError
	require.ErrorAs(t, err, &target)
	assert.Equal(t, 0, target.Index)
}

// isNilModule sees past the interface header: a plain nil and a typed nil are
// both "no module", while a real value is not.
func TestIsNilModule(t *testing.T) {
	var typedNil *hostTestModule
	var iface qrdbsdk.ServiceModule = typedNil

	assert.True(t, isNilModule(nil), "a plain nil is no module")
	assert.True(t, isNilModule(iface), "a typed nil is no module")
	assert.False(t, isNilModule(newHostTestModule("platform", "/api/v1/platform")),
		"a real module is not nil")
}

// The route guard tolerates a typed nil in either module set rather than
// dereferencing it, so a defensive path can never panic where NewServer's
// validation would already have refused.
func TestAssertNoHostModuleRouteConflicts_SkipsTypedNilModules(t *testing.T) {
	var typedNil *hostTestModule

	require.NotPanics(t, func() {
		err := assertNoHostModuleRouteConflicts(
			[]qrdbsdk.ServiceModule{typedNil},
			[]qrdbsdk.ServiceModule{typedNil, newHostTestModule("platform", "/api/v1/platform")})
		assert.NoError(t, err)
	})
}

// A host module claiming a path inside go-qrdb's built-in namespace is
// refused. On a unified node modules register BEFORE built-ins and a built-in
// silently yields to a module that already claimed its path, so such a route
// would take over a core endpoint under the module's authorization contract
// with nothing downstream reporting it — fail-open at a reserved path.
func TestAssertNoHostModuleRouteConflicts_ReservedNamespaceIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		segment string
		public  bool
	}{
		{"qrdb kv plane", "/kv/range", "/kv", false},
		{"qrdb admin plane", "/admin/shards/{id}", "/admin", false},
		{"qrdb time-series plane", "/ts/query", "/ts", false},
		{"qrdb vector plane", "/vector/get", "/vector", false},
		{"qrdb v1 databases", "/v1/databases", "/v1", false},
		{"qrdb v1 databases by name", "/v1/databases/{name}", "/v1", false},
		{"qrdb v1 collections", "/v1/collections/*", "/v1", false},
		{"qrdb v1 credentials", "/v1/credentials/{name}", "/v1", false},
		{"a host cannot camp anywhere under v1", "/v1/trusted-platform/devices", "/v1", false},
		{"public is not an escape hatch", "/kv", "/kv", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{&hostTestModule{
				name:   "platform",
				routes: []hostTestRoute{{method: http.MethodGet, pattern: tc.pattern, public: tc.public}},
			}})

			var reserved *HostModuleReservedRouteError
			require.ErrorAs(t, err, &reserved)
			assert.Equal(t, "platform", reserved.Module)
			assert.Equal(t, tc.pattern, reserved.Pattern)
			assert.Equal(t, tc.segment, reserved.Segment)
			assert.Contains(t, err.Error(), "namespace the module's routes")
		})
	}
}

// The reserved check is first-segment-exact, so it refuses only a module that
// reaches into go-qrdb's namespace — a host mounting under its own prefix, or
// under the /api/v1 prefix quicbaas and xkms use, is unaffected.
func TestAssertNoHostModuleRouteConflicts_OwnNamespaceIsUnaffected(t *testing.T) {
	for _, pattern := range []string{
		"/api/v1/devices",
		"/api/v1/kv/passthrough",
		"/trusted-platform/devices",
		"/api/v1/users/{id}",
	} {
		t.Run(pattern, func(t *testing.T) {
			err := assertNoHostModuleRouteConflicts(nil, []qrdbsdk.ServiceModule{&hostTestModule{
				name:   "platform",
				routes: []hostTestRoute{{method: http.MethodGet, pattern: pattern}},
			}})
			assert.NoError(t, err)
		})
	}
}

// routeFirstSegment carves a pattern at the granularity go-qrdb's built-in
// surface is namespaced at.
func TestRouteFirstSegment(t *testing.T) {
	tests := []struct{ pattern, want string }{
		{"/api/v1/devices", "/api"},
		{"/kv/range", "/kv"},
		{"/kv", "/kv"},
		{"/", "/"},
		{"", ""},
		{"no-leading-slash", ""},
	}
	for _, tc := range tests {
		assert.Equalf(t, tc.want, routeFirstSegment(tc.pattern), "pattern %q", tc.pattern)
	}
}

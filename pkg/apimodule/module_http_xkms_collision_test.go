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

package apimodule

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// declaredModuleRouteOverlaps are the (method, pattern) pairs BOTH the
// quicbaas and the go-xkms ServiceModule register on a composed unified
// server, in the order the recorder below observes them. quicbaas registers
// first (pkg/server/qrdb_node.go unifiedModuleServerOptions appends its
// module before xkms's), and go-qrdb's registrar gives the path to the first
// claimant, so quicbaas owns both — which is the intended outcome: the tenant
// control plane is quicbaas's (CLAUDE.md: "System admin provisions tenants
// minimally (control plane)").
//
// It is owned IMPLICITLY, by registration order, which is a latent hazard
// rather than a decision: reordering the two WithModule calls would hand the
// tenant create/list verbs to xkms's own tenant handlers with no error
// anywhere. Making it explicit — the way /api/v1/barrier/* now is, via
// NewRouteExcludingModule — is an open decision for the tenant plane's owner
// (go-quicbaas#405 audit), not something this lane settles.
var declaredModuleRouteOverlaps = []string{
	`POST /api/v1/tenants/ registered by BOTH "quicbaas" and "xkms"`,
	`GET /api/v1/tenants/ registered by BOTH "quicbaas" and "xkms"`,
}

// TestModule_RegisterHTTP_ComposesWithXKMSModuleWithoutRouteCollision is the
// P3.3/host-wiring acceptance test: it proves quicbaas's own ServiceModule
// and go-xkms's ServiceModule register their REST surfaces onto ONE shared
// qrdbsdk.ModuleHTTPRegistrar without either module claiming the same
// (method, pattern) pair the other already owns, AND that the xkms module
// actually registers its own routes now that go-xkms v0.7.1-alpha's Host
// self-build options are wired (xkmssdk.WithServiceModuleQRDBClient /
// WithServiceModuleWebAuthn / WithServiceModuleSession /
// WithServiceModulePIVLogin, the same four options
// pkg/server/qrdb_node.go's unifiedModuleServerOptions passes in production).
//
// Before v0.7.1-alpha (and before these options were wired), the module
// registered ZERO HTTP routes without a REST resolver — RegisterHTTP
// returned nothing to register, so the no-collision assertion below passed
// VACUOUSLY (an empty set never collides with anything). go-xkms's own
// module_http.go doc comment now states the route table "registers
// unconditionally" once a host supplies either a REST resolver or these
// self-build options, so the real acceptance is that the xkms module's own
// key public routes are actually PRESENT in the shared registrar.
//
// This reproduces pkg/server/qrdb_node.go's exact registration path
// (qrdbsdk.WithModule(quicbaasModule), qrdbsdk.WithModule(xkmsModule)) at
// construction speed, without booting gRPC/MCP/a live listener, so the guard
// runs fast and deterministically as a unit test rather than a live-server
// integration test.
//
// A real qrdb node's own registrar (go-qrdb pkg/apimodule.HTTPRegistrar,
// wired through the server's route/permission bookkeeping) already panics on
// a duplicate (method, pattern) registration; this test reimplements that
// exact detection rule locally so a regression here fails LOUDLY as a fast
// unit-test assertion instead of only surfacing as a live-node panic deep in
// an integration boot.
func TestModule_RegisterHTTP_ComposesWithXKMSModuleWithoutRouteCollision(t *testing.T) {
	// WithRESTServer is load-bearing for this guard, not decoration:
	// RegisterHTTP returns immediately when m.resolveREST is nil, so a module
	// built without it registers ZERO routes and the no-collision assertion
	// below passes VACUOUSLY on the quicbaas side — the exact vacuity this
	// test's doc comment calls out for xkms. A nil resolver return is fine
	// (routes late-bind and answer 503 while composing); what matters is that
	// the route table registers at all.
	quicbaasModule, err := NewModule(
		func() service.QuicBaaS { return nil },
		testLogger(t),
		WithRESTServer(func() *restapi.Server { return nil }),
	)
	require.NoError(t, err)

	xkmsModule, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService { return nil },
		// The exact four Host self-build options
		// pkg/server/qrdb_node.go's unifiedModuleServerOptions wires in
		// production. A nil qrdb client resolver return is fine while
		// composing (per WithServiceModuleQRDBClient's doc comment); the
		// module still registers its full route table regardless.
		xkmssdk.WithServiceModuleQRDBClient(func() *qrdbsdk.Client { return nil }),
		xkmssdk.WithServiceModuleWebAuthn(&xkmssdk.ServiceModuleWebAuthnConfig{
			Enabled:       true,
			RPID:          "test.example.com",
			RPOrigins:     []string{"https://test.example.com"},
			RPDisplayName: "quicbaas test",
		}),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
		xkmssdk.WithServiceModulePIVLogin(&xkmssdk.ServiceModulePIVLoginOptions{
			Enabled: true,
		}),
	)
	require.NoError(t, err)

	type owner struct {
		module, method, pattern string
	}
	registered := map[string]owner{}
	var collisions []string

	record := func(mod, method, pattern string) {
		key := method + " " + pattern
		if prior, seen := registered[key]; seen {
			collisions = append(collisions, fmt.Sprintf(
				"%s %s registered by BOTH %q and %q", method, pattern, prior.module, mod))
			return
		}
		registered[key] = owner{module: mod, method: method, pattern: pattern}
	}

	// registerFor builds a registrar whose route()/public() callbacks funnel
	// through record with the given module name attributed, mirroring
	// go-qrdb's own HTTPRegistrar wiring (pkg/apimodule/module.go
	// NewHTTPRegistrar) without needing a live server's permission registry.
	registerFor := func(mod string, register func(*qrdbsdk.ModuleHTTPRegistrar)) {
		router := chi.NewRouter()
		reg := qrdbsdk.NewModuleHTTPRegistrar(router,
			func(method, pattern string, _ qrdbsdk.Permission, _ http.HandlerFunc) {
				record(mod, method, pattern)
			},
			func(method, pattern string, _ http.HandlerFunc) {
				record(mod, method, pattern)
			},
		)
		register(reg)
	}

	// The xkms module registers through the composer's route-precedence
	// wrapper, exactly as pkg/server/qrdb_node.go's
	// unifiedModuleServerOptions mounts it. Without the wrapper the two
	// modules collide on /api/v1/barrier/* — see
	// TestRouteExcludingModule_XKMSBarrierCollisionIsReal.
	hostedXKMS, err := NewRouteExcludingModule(xkmsModule, XKMSBarrierRouteExclusions)
	require.NoError(t, err)

	registerFor("quicbaas", quicbaasModule.RegisterHTTP)
	registerFor("xkms", hostedXKMS.RegisterHTTP)

	// Overlaps are resolved, not merely absent. go-qrdb's unified server does
	// NOT panic on a module-vs-module duplicate the way this file's original
	// comment claimed: routeIf consults routePerms.Covered FIRST and returns
	// early, so the SECOND module to register a path silently yields to the
	// first at slog.Debug. Silent, order-dependent precedence is exactly how
	// go-quicbaas#405 stayed invisible, so every remaining overlap must be
	// named here with the module that owns it and why. A new, undeclared
	// overlap fails this test.
	require.Equal(t, declaredModuleRouteOverlaps, collisions,
		"quicbaas/xkms module route overlap changed; each overlapping path must be "+
			"declared with its owning module (registration order decides it silently otherwise)")
	require.NotEmpty(t, registered, "sanity: at least one route must have registered from each module")

	// The real acceptance (go-xkms v0.7.1-alpha): with the host options
	// wired, the xkms module's own key public routes are actually present
	// in the shared registrar, not silently absent as they were pre-0.7.1.
	for _, want := range []struct{ method, pattern string }{
		{"POST", "/api/v1/webauthn/registration/begin"},
		{"POST", "/api/v1/auth/login/piv"},
	} {
		key := want.method + " " + want.pattern
		got, ok := registered[key]
		require.True(t, ok, "xkms module must register %s (go-xkms v0.7.1-alpha self-build surface)", key)
		require.Equal(t, "xkms", got.module, "%s must be owned by the xkms module, not %q", key, got.module)
	}

	// The go-quicbaas#405 acceptance: the canonical barrier surface is owned
	// by the quicbaas module, whose handlers delegate to the go-qrdb SDK
	// BarrierClient. Before the fix these paths were absent from
	// ModuleRoutes() entirely and the xkms module owned them, answering
	// `503 rest: barrier not configured` for a configured, unsealed barrier.
	for _, want := range []struct{ method, pattern string }{
		{"GET", "/api/v1/barrier/status"},
		{"POST", "/api/v1/barrier/unseal"},
		{"POST", "/api/v1/barrier/seal"},
		{"POST", "/api/v1/barrier/rekey"},
		{"POST", "/api/v1/barrier/recovery/generate"},
		{"POST", "/api/v1/barrier/root-token"},
	} {
		key := want.method + " " + want.pattern
		got, ok := registered[key]
		require.True(t, ok, "the composed unified surface must serve %s", key)
		require.Equal(t, "quicbaas", got.module,
			"%s must be owned by the quicbaas module (go-qrdb barrier), not %q", key, got.module)
	}

	// Non-vacuity for the quicbaas side: a module built without a REST
	// resolver registers nothing, which would make every assertion above
	// pass for the wrong reason.
	var quicbaasOwned int
	for _, o := range registered {
		if o.module == "quicbaas" {
			quicbaasOwned++
		}
	}
	require.Greater(t, quicbaasOwned, 100,
		"sanity: the quicbaas module must register its full route table, not an empty set")
}

// TestRouteExcludingModule_XKMSBarrierCollisionIsReal is the non-vacuousness
// proof for the wrapper above: registered WITHOUT the composer's route
// precedence, the go-xkms module claims the canonical
// /api/v1/barrier/* surface, and co-hosting it with the quicbaas module —
// which now carries the same paths — is a duplicate registration that go-qrdb's
// registrar fails construction on.
//
// This is the server-side half of go-quicbaas#405 reproduced as a unit test:
// the path resolved to xkms's host-inert barrier handler, which answers
// `503 {"error":"rest: barrier not configured"}` however healthy the real
// (go-qrdb) barrier is.
func TestRouteExcludingModule_XKMSBarrierCollisionIsReal(t *testing.T) {
	xkmsModule, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService { return nil },
		xkmssdk.WithServiceModuleQRDBClient(func() *qrdbsdk.Client { return nil }),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)

	raw := collectModuleRoutes(t, xkmsModule.RegisterHTTP)
	require.Contains(t, raw, "GET /api/v1/barrier/status",
		"sanity: the unwrapped xkms module must still claim the canonical barrier surface; "+
			"if go-xkms has since gated these routes on its nil Barrier, the wrapper is redundant and should go")

	hosted, err := NewRouteExcludingModule(xkmsModule, XKMSBarrierRouteExclusions)
	require.NoError(t, err)
	filtered := collectModuleRoutes(t, hosted.RegisterHTTP)

	for key := range raw {
		if strings.HasPrefix(strings.SplitN(key, " ", 2)[1], "/api/v1/barrier/") {
			require.NotContains(t, filtered, key,
				"the route-precedence wrapper must drop the xkms module's barrier registrations")
			continue
		}
		require.Contains(t, filtered, key,
			"the route-precedence wrapper must leave every non-excluded xkms route intact")
	}
	require.Less(t, len(filtered), len(raw), "sanity: the wrapper must actually drop routes")
}

// TestNewRouteExcludingModule_RejectsBadInput proves the wrapper refuses the
// inputs it cannot honour instead of silently degrading: a nil module, an
// empty exclusion set (pure indirection), and a module missing an optional
// qrdb SDK extension the wrapper would otherwise answer for with an empty
// declaration set.
func TestNewRouteExcludingModule_RejectsBadInput(t *testing.T) {
	t.Run("nil module", func(t *testing.T) {
		got, err := NewRouteExcludingModule(nil, XKMSBarrierRouteExclusions)
		require.Nil(t, got)
		var want *NilModuleError
		require.ErrorAs(t, err, &want)
	})

	t.Run("no exclusions", func(t *testing.T) {
		xkmsModule, err := xkmssdk.NewServiceModule(func() *xkmslib.XKMSService { return nil })
		require.NoError(t, err)
		got, err := NewRouteExcludingModule(xkmsModule, nil)
		require.Nil(t, got)
		var want *EmptyRouteExclusionsError
		require.ErrorAs(t, err, &want)
	})

	t.Run("module missing an optional extension", func(t *testing.T) {
		got, err := NewRouteExcludingModule(&bareModule{}, XKMSBarrierRouteExclusions)
		require.Nil(t, got)
		var want *ModuleExtensionMissingError
		require.ErrorAs(t, err, &want)
		require.Equal(t, "bare", want.Module)
		require.Equal(t, "ModuleMCPToolPermissioner", want.Extension)
	})
}

// TestRouteExcludingModule_ForwardsEveryOtherHook proves the wrapper is
// transparent outside RegisterHTTP: name, gRPC/MCP registration, lifecycle,
// and all three optional qrdb SDK extensions reach the wrapped module.
func TestRouteExcludingModule_ForwardsEveryOtherHook(t *testing.T) {
	inner := &recordingModule{
		permissions: map[string]qrdbsdk.Permission{"tool": qrdbsdk.Permission("keys:read")},
		publicGRPC:  []string{"/pkg.Service/Method"},
	}
	hosted, err := NewRouteExcludingModule(inner, XKMSBarrierRouteExclusions)
	require.NoError(t, err)

	require.Equal(t, "recording", hosted.Name())

	hosted.RegisterGRPC(nil)
	require.True(t, inner.grpcRegistered)

	hosted.RegisterMCP(nil)
	require.True(t, inner.mcpRegistered)

	require.NoError(t, hosted.Start(context.Background()))
	require.True(t, inner.started)
	require.NoError(t, hosted.Stop(context.Background()))
	require.True(t, inner.stopped)

	permissioner, ok := hosted.(qrdbsdk.ModuleMCPToolPermissioner)
	require.True(t, ok)
	require.Equal(t, inner.permissions, permissioner.MCPToolPermissions())

	lister, ok := hosted.(qrdbsdk.ModuleGRPCPublicMethodLister)
	require.True(t, ok)
	require.Equal(t, inner.publicGRPC, lister.PublicGRPCMethods())

	fallback, ok := hosted.(qrdbsdk.ModuleAuthFallback)
	require.True(t, ok)
	identity, err := fallback.AuthenticateHTTP(nil)
	require.Nil(t, identity)
	require.ErrorIs(t, err, errRecordingAuth)

	// A nil registrar must be a no-op, not a panic: the wrapper builds a
	// substitute registrar from the host's, and there is nothing to build
	// from.
	require.NotPanics(t, func() { hosted.RegisterHTTP(nil) })
	require.False(t, inner.httpRegistered)
}

// collectModuleRoutes registers a module's HTTP surface into a recording
// registrar and returns the "METHOD /pattern" keys it claimed.
func collectModuleRoutes(t *testing.T, register func(*qrdbsdk.ModuleHTTPRegistrar)) map[string]struct{} {
	t.Helper()
	claimed := map[string]struct{}{}
	reg := qrdbsdk.NewModuleHTTPRegistrar(chi.NewRouter(),
		func(method, pattern string, _ qrdbsdk.Permission, _ http.HandlerFunc) {
			claimed[method+" "+pattern] = struct{}{}
		},
		func(method, pattern string, _ http.HandlerFunc) {
			claimed[method+" "+pattern] = struct{}{}
		},
	)
	register(reg)
	return claimed
}

// bareModule is a ServiceModule with none of the optional qrdb SDK
// extensions, used to prove the wrapper refuses to answer for declarations a
// module never made.
type bareModule struct{ qrdbsdk.BaseModule }

// Name identifies the bare module.
func (bareModule) Name() string { return "bare" }

// errRecordingAuth is the sentinel recordingModule's auth fallback returns so
// the forwarding assertion has something unambiguous to match.
var errRecordingAuth = errors.New("recording module: no identity")

// recordingModule implements the full co-hosted module contract and records
// which hooks the wrapper forwarded.
type recordingModule struct {
	permissions map[string]qrdbsdk.Permission
	publicGRPC  []string

	quicRegistered bool
	grpcRegistered bool
	mcpRegistered  bool
	httpRegistered bool
	started        bool
	stopped        bool
}

// Name identifies the recording module.
func (m *recordingModule) Name() string { return "recording" }

// RegisterGRPC records the forwarded gRPC hook.
func (m *recordingModule) RegisterGRPC(grpc.ServiceRegistrar) { m.grpcRegistered = true }

// RegisterQUIC records the forwarded QUIC hook.
func (m *recordingModule) RegisterQUIC(grpc.ServiceRegistrar) { m.quicRegistered = true }

// RegisterHTTP records the forwarded HTTP hook.
func (m *recordingModule) RegisterHTTP(*qrdbsdk.ModuleHTTPRegistrar) { m.httpRegistered = true }

// RegisterMCP records the forwarded MCP hook.
func (m *recordingModule) RegisterMCP(*mcpserver.MCPServer) { m.mcpRegistered = true }

// Start records the forwarded lifecycle start.
func (m *recordingModule) Start(context.Context) error { m.started = true; return nil }

// Stop records the forwarded lifecycle stop.
func (m *recordingModule) Stop(context.Context) error { m.stopped = true; return nil }

// MCPToolPermissions returns the recorded declarations.
func (m *recordingModule) MCPToolPermissions() map[string]qrdbsdk.Permission { return m.permissions }

// PublicGRPCMethods returns the recorded declarations.
func (m *recordingModule) PublicGRPCMethods() []string { return m.publicGRPC }

// AuthenticateHTTP always fails closed with the sentinel.
func (m *recordingModule) AuthenticateHTTP(*http.Request) (*qrdbsdk.Identity, error) {
	return nil, errRecordingAuth
}

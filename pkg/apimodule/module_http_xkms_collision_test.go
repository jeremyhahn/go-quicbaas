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
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

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
	quicbaasModule, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
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

	registerFor("quicbaas", quicbaasModule.RegisterHTTP)
	registerFor("xkms", xkmsModule.RegisterHTTP)

	require.Empty(t, collisions,
		"quicbaas and xkms REST modules must not register the same route; "+
			"P3.3 de-collision requires each colliding path to be owned by exactly one module:\n%v",
		collisions)
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
}

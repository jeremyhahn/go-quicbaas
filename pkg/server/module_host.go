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
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
)

// This file owns the module-host seam: the ONE way a downstream host
// (go-trusted-platform) mounts its own qrdb ServiceModule onto the same
// unified node that already carries quicbaas's module and go-xkms's module.
//
// WHY the option lives on quicbaas.NewServer rather than a re-exported
// qrdbsdk.NewServer: go-qrdb's SDK deliberately publishes only the server
// *options* (ServerOption, WithModule, WithServerTLS, ...) and no NewServer,
// because the node's construction is the composer's job — doc 00 L7,
// "composition happens in one place". quicbaas IS that composer: building the
// node means assembling the ONE audit sink, the module shard router, the
// late-binding REST/MCP resolvers, go-xkms's module with its four host
// options, and the unified TLS config — all of which read unexported Server
// state (see unifiedModuleServerOptions in qrdb_node.go). A host handed a raw
// NewServer would have to reimplement that composition root, which is exactly
// what the ownership boundary forbids. Threading the host's modules INTO
// quicbaas's composition keeps one node, one authorizer, one audit plane, and
// one place where modules are registered.

// reservedModuleNames are the ServiceModule names quicbaas's own composition
// root already registers on every unified node. A host module claiming one of
// them would make go-qrdb's WithModule panic deep inside node construction, so
// the conflict is reported as a typed error from NewServer instead.
var reservedModuleNames = map[string]string{
	apimodule.ModuleName:      "go-quicbaas",
	xkmssdk.ServiceModuleName: "go-xkms",
}

// NilHostModuleError reports a nil ServiceModule passed to WithModule. The
// index identifies which registration in the option list was nil, since a nil
// module carries no name to report.
type NilHostModuleError struct {
	Index int
}

// Error implements the error interface.
func (e *NilHostModuleError) Error() string {
	return "server: host ServiceModule at index " + strconv.Itoa(e.Index) + " is nil"
}

// UnnamedHostModuleError reports a host ServiceModule whose Name() is empty.
// The name is the module's identity for duplicate detection, logs and metrics
// across every transport, so an empty one is a composition error.
type UnnamedHostModuleError struct {
	Index int
}

// Error implements the error interface.
func (e *UnnamedHostModuleError) Error() string {
	return "server: host ServiceModule at index " + strconv.Itoa(e.Index) +
		" returned an empty Name(); every module needs a unique name"
}

// HostModuleNameConflictError reports a host ServiceModule whose Name()
// duplicates a module already mounted on the node — either one of quicbaas's
// own composed modules or an earlier host module. Owner names the incumbent.
type HostModuleNameConflictError struct {
	Name  string
	Owner string
}

// Error implements the error interface.
func (e *HostModuleNameConflictError) Error() string {
	return "server: host ServiceModule name " + strconv.Quote(e.Name) +
		" is already registered by " + e.Owner
}

// HostModuleRouteConflictError reports two modules claiming the same REST
// route SHAPE on the shared unified router. go-qrdb's route registry treats a
// duplicate as a construction-time programmer error and panics from inside
// REST server construction; quicbaas detects it earlier, while assembling the
// node's module set, and reports it as a typed error naming BOTH owners so the
// host can see which of its routes to move.
//
// Pattern and OwnerPattern are the two literal patterns as each module wrote
// them. They are frequently NOT identical: chi matches a path parameter by
// position, not by name, so "/devices/{id}" and "/devices/{deviceID}" are one
// and the same route to the router. Reporting both spellings is what makes
// such a collision legible.
type HostModuleRouteConflictError struct {
	Method       string
	Pattern      string
	Module       string
	Owner        string
	OwnerPattern string
}

// Error implements the error interface.
func (e *HostModuleRouteConflictError) Error() string {
	msg := "server: REST route " + e.Method + " " + e.Pattern +
		" is claimed by both module " + strconv.Quote(e.Owner) +
		" and module " + strconv.Quote(e.Module)
	if e.OwnerPattern != "" && e.OwnerPattern != e.Pattern {
		msg += " (" + strconv.Quote(e.Owner) + " registered it as " + e.OwnerPattern +
			"; a path parameter's name is not part of the match)"
	}
	return msg
}

// HostModuleUnregisteredRouteError reports a host module route registered
// straight onto the registrar's embedded chi.Router instead of through
// Handle or HandlePublic.
//
// Such a route declares no permission, so it never reaches the node's
// authorization registry — and because chi silently REPLACES the handler for a
// pattern it already holds, the route can quietly take over one another module
// already serves while the registry still records that other module's
// permission for it. Neither the collision guard nor the node's default-deny
// startup assertion can see it, so the mount fails closed here.
type HostModuleUnregisteredRouteError struct {
	Module  string
	Method  string
	Pattern string
}

// Error implements the error interface.
func (e *HostModuleUnregisteredRouteError) Error() string {
	return "server: host ServiceModule " + strconv.Quote(e.Module) +
		" registered REST route " + e.Method + " " + e.Pattern +
		" directly on the embedded chi.Router; every module route must go through " +
		"Handle (declaring its permission) or HandlePublic"
}

// HostModuleInvalidRouteError reports a REST registration chi itself refuses —
// a pattern that does not begin with "/", an unparseable regex constraint, an
// unsupported HTTP method, or router middleware added after routes already
// exist. chi signals every one of these by panicking at registration time; the
// guard catches that during the dry run so a host mistake fails the mount with
// a named error instead of taking the process down from inside node
// construction.
//
// Method and Pattern are empty when the panic came from inside the module's
// own RegisterHTTP — a registration made straight on the embedded chi.Router
// never passes through the permission-recording callbacks, so there is nothing
// to attribute it to beyond chi's own message.
type HostModuleInvalidRouteError struct {
	Module  string
	Method  string
	Pattern string
	Reason  string
}

// Error implements the error interface.
func (e *HostModuleInvalidRouteError) Error() string {
	msg := "server: module " + strconv.Quote(e.Module) + " made an invalid REST registration"
	if e.Method != "" || e.Pattern != "" {
		msg += " (" + strings.TrimSpace(e.Method+" "+e.Pattern) + ")"
	}
	return msg + ": " + e.Reason
}

// HostModuleReservedRouteError reports a host module route inside go-qrdb's
// own built-in REST namespace. Modules register before built-ins and a
// built-in silently yields to a module that already claimed its path, so such
// a route would take over a core endpoint — with the module's authorization
// contract rather than the node's — and nothing downstream would say so. The
// mount refuses it.
type HostModuleReservedRouteError struct {
	Module  string
	Method  string
	Pattern string
	Segment string
}

// Error implements the error interface.
func (e *HostModuleReservedRouteError) Error() string {
	return "server: host ServiceModule " + strconv.Quote(e.Module) +
		" claims REST route " + e.Method + " " + e.Pattern +
		", inside go-qrdb's reserved namespace " + strconv.Quote(e.Segment) +
		"; a module registers ahead of the node's built-in routes and would " +
		"shadow them silently — namespace the module's routes under a prefix of its own"
}

// HostModuleRouterInspectionError reports a failure to enumerate the routes a
// module registered on the registrar's embedded chi.Router. The guard cannot
// certify the module's REST surface without that enumeration, so the mount
// fails closed rather than proceeding on an incomplete picture.
type HostModuleRouterInspectionError struct {
	Module string
	Cause  error
}

// Error implements the error interface.
func (e *HostModuleRouterInspectionError) Error() string {
	return "server: cannot enumerate the routes module " + strconv.Quote(e.Module) +
		" registered on its router: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *HostModuleRouterInspectionError) Unwrap() error { return e.Cause }

// WithModule returns a ServerOption that mounts a host-supplied capability
// module onto the unified qrdb node this server builds, alongside quicbaas's
// own module and go-xkms's. The module's gRPC services, REST routes and MCP
// tools are attached to every transport the node serves, gated by the node's
// single authorizer and recorded in the same permission registry as the
// built-in routes; its Start/Stop lifecycle runs with the node's.
//
// The option is repeatable — call it once per module. Registrations are
// validated by NewServer, which rejects a nil module, an empty Name(), and a
// name that duplicates "quicbaas", "xkms" or an earlier host module. During
// node construction the module's REST surface is dry-run and rejected if it
// claims a route another mounted module already owns (comparing route SHAPES,
// so a renamed path parameter does not slip through) or registers any route
// straight onto the registrar's embedded chi.Router instead of through
// Handle/HandlePublic. Those are typed errors rather than the panic go-qrdb
// raises, so a host gets an actionable failure at its own composition site.
//
// The guard cannot see go-qrdb's own BUILT-IN REST routes — they are not
// exposed through the SDK, and on a unified node modules register first while
// built-ins yield to them. Namespace a module's routes under a prefix of its
// own so it cannot shadow the node's core surface.
//
// CONTRACT: RegisterHTTP must be a REPEATABLE, side-effect-free declaration of
// the module's route table. Validating a module's REST surface means asking it
// for that surface, so the seam calls RegisterHTTP once against a throwaway
// router and go-qrdb calls it again against the live one. A module that
// mutates state, counts calls or refuses a second registration will not
// survive that, and neither its validated surface nor its live one would then
// describe the other. go-qrdb's ServiceModule interface does not itself demand
// this; mounting THROUGH WithModule does, and quicbaas's own module and
// go-xkms's both satisfy it (pinned by
// TestUnifiedModuleServerOptions_MountedModulesAreRepeatable). Lifting the
// requirement needs a registration-time introspection hook from go-qrdb, filed
// as go-qrdb#194.
//
// Two transports are NOT covered by the module seam, and a host must plan for
// both (go-qrdb pkg/apimodule/module.go:73-79):
//
//   - QUIC: go-qrdb's QUIC transport is a bespoke frame protocol with
//     method-keyed dispatch, not HTTP-over-chi, so RegisterHTTP does not
//     reach it. A module needs its own QUIC surface until go-qrdb grows a
//     RegisterQUIC hook.
//   - embedded: the in-process transport is each capability owner's own SDK
//     surface, independent of this seam. A host calls its module's Go API
//     directly in-process.
func WithModule(m qrdbsdk.ServiceModule) ServerOption {
	return func(o *serverOptions) { o.hostModules = append(o.hostModules, m) }
}

// qrdbReservedRouteSegments are the first path segments go-qrdb's own built-in
// REST surface occupies on a unified node, derived from its route
// registrations (go-qrdb v0.9.0-alpha pkg/api/rest).
//
// WHY this list has to exist here: on a unified node MODULES register before
// built-ins, and a built-in whose (method, pattern) a module already claimed
// yields to the module without a word (go-qrdb pkg/api/rest routeIf/publicIf).
// A host module could therefore claim, say, GET /kv — or worse, HandlePublic
// it — and node construction would succeed while callers reached the host's
// handler under the host's authorization contract at a core endpoint. That is
// fail-open at a reserved path, so the mount refuses it.
//
// "/v1" is reserved WHOLESALE rather than by enumerating the sub-namespaces
// go-qrdb serves there (/v1/databases, /v1/collections, /v1/credentials,
// /v1/backups, /v1/authfactors, /v1/tenants, ...). Reserving the segment is
// both simpler and stronger: it needs no list to keep in sync, and it covers
// every /v1 route go-qrdb adds in future. It costs a host nothing, because
// this check applies ONLY to host modules — quicbaas's own module and
// go-xkms's are never checked against it, so their /v1/barrier/*,
// /v1/ceremony/* and /v1/init/* routes are unaffected. /v1 is the node's own
// API-version namespace; a host module belongs under a prefix of its own (the
// documented shape is /api/v1/<module>/...), which /api never collides with.
//
// The BARE segments remain a snapshot: go-qrdb does not export its built-in
// route table through the SDK, so a plane it adds under a NEW top-level
// segment would not be covered. go-qrdb#194 asks for the table (or for the
// check to move upstream, where it cannot drift at all).
var qrdbReservedRouteSegments = map[string]struct{}{
	"/admin":        {},
	"/batch":        {},
	"/capabilities": {},
	"/certs":        {},
	"/drain":        {},
	"/health":       {},
	"/index":        {},
	"/kv":           {},
	"/lease":        {},
	"/lock":         {},
	"/membership":   {},
	"/metrics":      {},
	"/peers":        {},
	"/permissions":  {},
	"/roles":        {},
	"/snapshot":     {},
	"/sql":          {},
	"/status":       {},
	"/ts":           {},
	"/tx":           {},
	"/users":        {},
	"/v1":           {},
	"/vector":       {},
	"/watch":        {},
}

// routeFirstSegment returns the leading path segment of a chi pattern
// ("/api/v1/devices" -> "/api"), which is the granularity go-qrdb's built-in
// surface is carved at.
func routeFirstSegment(pattern string) string {
	if !strings.HasPrefix(pattern, "/") {
		return ""
	}
	if i := strings.IndexByte(pattern[1:], '/'); i >= 0 {
		return pattern[:i+1]
	}
	return pattern
}

// isReservedRoute reports whether a pattern reaches into go-qrdb's own
// built-in REST namespace.
func isReservedRoute(pattern string) bool {
	_, reserved := qrdbReservedRouteSegments[routeFirstSegment(pattern)]
	return reserved
}

// isNilModule reports whether a registration carries no usable module.
//
// A plain nil is the easy case. The one that bites is a TYPED nil —
// WithModule((*deviceModule)(nil)) — where the interface header holds a type
// and so is not == nil, while the value inside is: calling Name() on it
// dereferences a nil receiver and panics. WithModule promises a typed error
// for a missing module, so the guard has to look past the interface header
// before touching the value.
func isNilModule(m qrdbsdk.ServiceModule) bool {
	if m == nil {
		return true
	}
	v := reflect.ValueOf(m)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
		return v.IsNil()
	default:
		return false
	}
}

// validateHostModules checks every host module registration at construction
// time, before any component starts, so a naming mistake fails at
// NewServer with a typed error naming the offender rather than panicking
// inside go-qrdb's WithModule when the node is finally built.
func validateHostModules(modules []qrdbsdk.ServiceModule) error {
	seen := make(map[string]string, len(modules))
	for i, m := range modules {
		if isNilModule(m) {
			return &NilHostModuleError{Index: i}
		}
		name := m.Name()
		if name == "" {
			return &UnnamedHostModuleError{Index: i}
		}
		if owner, reserved := reservedModuleNames[name]; reserved {
			return &HostModuleNameConflictError{Name: name, Owner: owner}
		}
		if owner, dup := seen[name]; dup {
			return &HostModuleNameConflictError{Name: name, Owner: owner}
		}
		seen[name] = "an earlier WithModule registration"
	}
	return nil
}

// moduleRoute is one REST registration observed while dry-running a module.
type moduleRoute struct {
	method  string
	pattern string
}

// moduleRoutes is what a dry run of one module's RegisterHTTP produced.
// declared routes went through the permission-recording Handle/HandlePublic;
// bypassed routes were put straight onto the embedded chi.Router and carry no
// permission declaration at all.
type moduleRoutes struct {
	declared []moduleRoute
	bypassed []moduleRoute
}

// captureModuleRoutes dry-runs one module's RegisterHTTP and reports what it
// registered, split by whether the registration declared a permission.
//
// The capture registrar mirrors the wiring go-qrdb's REST server uses
// (NewModuleHTTPRegistrar with its route/public helpers) over a throwaway chi
// router. Handle and HandlePublic land in declared; anything the module puts
// straight onto the embedded router lands on the router itself and is
// recovered by walking it afterwards, which is the only way to see a
// registration that deliberately skipped the permission-recording path.
//
// The dry run is side-effect free: both quicbaas's and go-xkms's RegisterHTTP
// only build lazily-bound handler closures and mutate no module state, so
// running them once here and once for real costs nothing but the closures.
func captureModuleRoutes(m qrdbsdk.ServiceModule, routesAlreadyMounted bool) (out moduleRoutes, err error) {
	name := m.Name()
	probe := chi.NewRouter()

	// go-qrdb's registrar refuses Use from every module (v0.25). A module can
	// still reach past it onto the embedded chi.Router, and a host module
	// registers AFTER quicbaas's and go-xkms's, when the shared router already
	// holds routes and chi refuses router-level middleware ("all middlewares
	// must be defined before routes on a mux"). The probe is seeded to match
	// the state that module will actually meet, so such a call fails here, by
	// name, rather than as a panic during real registration. The mounted
	// modules register first and meet an empty router, so they are probed
	// against one.
	const middlewareSentinelPath = "/__quicbaas_module_host_probe__"
	var sentinel http.Handler
	if routesAlreadyMounted {
		sentinel = &probeSentinel{}
		probe.Method(http.MethodGet, middlewareSentinelPath, sentinel)
	}

	record := func(method, pattern string) {
		// chi upper-cases the method it stores and chi.Walk reports it that
		// way, so declared routes are normalized here to keep route identity
		// and ownership keyed consistently with the walked ones.
		out.declared = append(out.declared,
			moduleRoute{method: strings.ToUpper(method), pattern: pattern})
	}
	reg := qrdbsdk.NewModuleHTTPRegistrar(probe,
		func(method, pattern string, _ qrdbsdk.Permission, _ http.HandlerFunc) {
			record(method, pattern)
		},
		func(method, pattern string, _ http.HandlerFunc) {
			record(method, pattern)
		},
	)

	// RegisterHTTP itself can panic: chi rejects an unsupported method, a
	// pattern without a leading slash and late middleware by panicking, and a
	// module reaching past the registrar onto the embedded chi.Router hits
	// those directly. Recover here so the mount fails with a named error
	// rather than killing the process from inside node construction.
	registerErr := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = &HostModuleInvalidRouteError{Module: name, Reason: fmt.Sprint(rec)}
			}
		}()
		m.RegisterHTTP(reg)
		return nil
	}()
	if registerErr != nil {
		return moduleRoutes{}, registerErr
	}

	walkErr := chi.Walk(probe, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Skip the seed only when it is still exactly the handler that was
		// seeded. A module that registered over that path — replacing the
		// GET handler, or adding another method on it — yields a different
		// handler and is reported like any other bypassing registration.
		if sentinel != nil && route == middlewareSentinelPath && h == sentinel {
			return nil
		}
		out.bypassed = append(out.bypassed, moduleRoute{method: method, pattern: route})
		return nil
	})
	if walkErr != nil {
		return moduleRoutes{}, &HostModuleRouterInspectionError{Module: name, Cause: walkErr}
	}
	return out, nil
}

// routeNoop is the placeholder handler the shadow router holds. The shadow is
// only ever used to decide whether two patterns are the same route; it never
// serves a request.
func routeNoop(http.ResponseWriter, *http.Request) {}

// probeSentinel is the handler seeded onto a host module's probe router so chi
// sees a non-empty mux (see captureModuleRoutes). It is a pointer type rather
// than a func so the seeded value can be compared by identity: a module that
// registers over the sentinel's path — same method or another — installs a
// DIFFERENT handler, and the walk then reports it as the bypassing
// registration it is. Skipping by path alone would let a module hide a direct
// registration behind the sentinel's name.
type probeSentinel struct{}

// ServeHTTP implements http.Handler. The sentinel never serves: the probe
// router exists only to be inspected.
func (probeSentinel) ServeHTTP(http.ResponseWriter, *http.Request) {}

// routeClaims records which module owns each REST route, deciding "same route"
// with a shadow chi.Mux rather than by comparing pattern text.
//
// WHY a real router and not string keys: chi matches a path parameter by
// POSITION, not by name, so "/devices/{id}" and "/devices/{deviceID}" are the
// same route — registering the second silently REPLACES the first, with no
// panic and no error (verified against chi v5: a request then reaches the
// second handler, and chi.Walk lists only the second pattern). A regex
// CONSTRAINT is part of the match, so "{id:[0-9]+}" and "{name}" genuinely
// coexist. Any hand-rolled normalization is a model of those rules that can
// drift from them; routing the patterns through the same matcher the live
// router uses cannot. go-qrdb's own registry keys on raw pattern text and so
// misses the renamed-parameter case entirely.
type routeClaims struct {
	shadow *chi.Mux
	owners map[string]moduleClaim
}

// moduleClaim is the module that first claimed a route, and the spelling it
// used — reported alongside the offender's spelling when the two differ.
type moduleClaim struct {
	module  string
	pattern string
}

func newRouteClaims() *routeClaims {
	return &routeClaims{shadow: chi.NewRouter(), owners: map[string]moduleClaim{}}
}

// routeSet enumerates the shadow router's current routes as "METHOD pattern".
func (c *routeClaims) routeSet() (map[string]struct{}, error) {
	set := map[string]struct{}{}
	err := chi.Walk(c.shadow, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		set[method+" "+route] = struct{}{}
		return nil
	})
	return set, err
}

// insert adds a route to the shadow router, recovering the panic chi raises on
// a malformed pattern so a host typo fails the mount with a typed error
// instead of taking the process down from inside node construction.
func (c *routeClaims) insert(module string, r moduleRoute) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = &HostModuleInvalidRouteError{
				Module: module, Method: r.method, Pattern: r.pattern,
				Reason: fmt.Sprint(rec),
			}
		}
	}()
	c.shadow.Method(r.method, r.pattern, http.HandlerFunc(routeNoop))
	return nil
}

// seed records a route already mounted on the node. Overlap between the
// modules quicbaas itself composes predates the host seam and is go-qrdb's to
// police, so a seeded route never fails: the FIRST claimant keeps ownership
// and later ones are silently absorbed.
func (c *routeClaims) seed(module string, r moduleRoute) error {
	if err := c.insert(module, r); err != nil {
		return err
	}
	key := r.method + " " + r.pattern
	if _, taken := c.owners[key]; !taken {
		c.owners[key] = moduleClaim{module: module, pattern: r.pattern}
	}
	return nil
}

// claim records a HOST module's route, reporting a conflict when the shadow
// router shows the route was not new — i.e. chi folded it into a route another
// module already owns, whatever the two patterns look like.
func (c *routeClaims) claim(module string, r moduleRoute) error {
	before, err := c.routeSet()
	if err != nil {
		return &HostModuleRouterInspectionError{Module: module, Cause: err}
	}
	if err := c.insert(module, r); err != nil {
		return err
	}
	after, err := c.routeSet()
	if err != nil {
		return &HostModuleRouterInspectionError{Module: module, Cause: err}
	}

	key := r.method + " " + r.pattern
	if len(after) > len(before) {
		c.owners[key] = moduleClaim{module: module, pattern: r.pattern}
		return nil
	}

	// The route count did not grow, so this registration replaced one the
	// shadow already held. The victim is the entry that vanished; when the
	// two patterns were spelled identically nothing vanishes and the victim
	// is the key itself.
	victim := key
	for k := range before {
		if _, survived := after[k]; !survived {
			victim = k
			break
		}
	}
	prior := c.owners[victim]
	return &HostModuleRouteConflictError{
		Method:       r.method,
		Pattern:      r.pattern,
		Module:       module,
		Owner:        prior.module,
		OwnerPattern: prior.pattern,
	}
}

// assertNoHostModuleRouteConflicts dry-runs every module's RegisterHTTP and
// reports the first REST route a HOST module claims that another mounted
// module already owns, or that the host registered without declaring a
// permission.
//
// WHY a dry run: go-qrdb's own guard (pkg/auth/middleware RESTRouteRegistry)
// is authoritative but fires as a panic from deep inside REST server
// construction, long after the host's NewServer call returned. Replaying the
// registrations first is cheap and side-effect free, and it converts that
// panic into an error naming both owners. It also catches two collisions
// go-qrdb's registry cannot see, because that registry keys on raw pattern
// text: a renamed path parameter, and a route registered around the
// permission-recording registrar entirely.
//
// WHY only conflicts involving a host module: overlap BETWEEN the mounted
// modules (quicbaas's own and go-xkms's) predates this seam and is go-qrdb's
// to enforce, exactly as it is today. Reporting it here would newly fail a
// composition that boots today, and it is never something a downstream host
// can act on. The mounted modules therefore seed the claim set first-wins, and
// only host registrations are checked — against them and against each other.
// The whole check runs ONLY when a host module is present, so the default
// composition keeps precisely the behavior it has today.
//
// WHAT THE GUARD DOES NOT COVER, and why those residuals are upstream:
//
//   - go-qrdb's own BUILT-IN REST routes are not exposed through the SDK, so
//     the guard compares against a SNAPSHOT of go-qrdb's bare top-level planes
//     (qrdbReservedRouteSegments) rather than the live table. A host route
//     inside one is refused — including anything under /v1, reserved wholesale
//     so no future go-qrdb route there can be shadowed. What remains uncovered
//     is a plane go-qrdb adds under a NEW top-level segment after this
//     snapshot. go-qrdb#194 asks for the table itself, which removes that.
//   - A module whose RegisterHTTP is not deterministic. This is a dry run, so
//     it certifies the surface the module declares WHEN ASKED; go-qrdb calls
//     RegisterHTTP again against the live registrar, and a module that
//     registered a different surface the second time, or that stashed the live
//     chi.Router and mutated it later from Start, would not be described by
//     what was checked here. Only the owner of the live registrar can close
//     that — it is the same property go-qrdb's own registry has for quicbaas's
//     and go-xkms's modules, and it is filed with the rest as go-qrdb#194.
//     RegisterHTTP is expected to be a pure declaration of the module's route
//     table; quicbaas's and go-xkms's both are.
func assertNoHostModuleRouteConflicts(mounted, hosts []qrdbsdk.ServiceModule) error {
	claims := newRouteClaims()

	for _, m := range mounted {
		if isNilModule(m) {
			continue
		}
		name := m.Name()
		routes, err := captureModuleRoutes(m, false)
		if err != nil {
			return err
		}
		// A mounted module's own bypass routes still occupy the router, so
		// they count as claims a host must not collide with — but they are not
		// rejected here, for the same reason mounted-vs-mounted overlap is not.
		for _, r := range append(routes.declared, routes.bypassed...) {
			if err := claims.seed(name, r); err != nil {
				return err
			}
		}
	}

	for _, m := range hosts {
		if isNilModule(m) {
			continue
		}
		name := m.Name()
		routes, err := captureModuleRoutes(m, true)
		if err != nil {
			return err
		}
		if len(routes.bypassed) > 0 {
			r := routes.bypassed[0]
			return &HostModuleUnregisteredRouteError{
				Module: name, Method: r.method, Pattern: r.pattern,
			}
		}
		for _, r := range routes.declared {
			if isReservedRoute(r.pattern) {
				return &HostModuleReservedRouteError{
					Module: name, Method: r.method, Pattern: r.pattern,
					Segment: routeFirstSegment(r.pattern),
				}
			}
			if err := claims.claim(name, r); err != nil {
				return err
			}
		}
	}
	return nil
}

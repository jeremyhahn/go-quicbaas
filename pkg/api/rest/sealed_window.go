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

package rest

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// SetSealedWindowGate installs the sealed-window middleware at the top of the
// REST middleware chain and rebuilds the router. While probe reports sealed,
// every request whose path is not on the allowlist is rejected with 503 and
// qrdb's documented sealed envelope; allowlisted paths (barrier / ceremony /
// init / bootstrap / health) pass through so operators can drive the unseal
// flow. When the probe reports unsealed the middleware is a pass-through, so
// the gate stays installed for the daemon's whole lifetime — a post-boot
// barrier seal immediately re-narrows the surface.
//
// The middleware itself is go-qrdb's canonical REST sealed-window middleware
// (consumed via the SDK re-export qrdbsdk.SealedWindowMiddleware); quicbaas
// supplies only the allowlist, derived from the single canonical table in
// pkg/api/sealedwindow (sealedwindow.RESTAllowlist) so REST cannot drift from
// the gRPC / QUIC / MCP gates.
//
// The gate also derives the shared sealed-window authentication-bypass
// predicate (sealedwindow.AuthBypass) from the same probe. The authz
// middleware below the sealed gate consults it so that WHILE SEALED the
// allowlisted paths that pass the sealed gate are admitted WITHOUT
// authentication (audited, reason "sealed-window allowlist") — a sealed IAM
// plane cannot mint or validate tokens, so the allowlist is the entire
// control surface. Unsealed, the predicate is inert and enforcement is
// unchanged.
//
// operational is the ceremony-state probe that decides whether the
// pre-operational half of that exemption still applies (the self-authorizing
// ceremony claim handshakes and the ceremony status probe). It FAILS CLOSED:
// nil, or a probe reporting operational, withholds those exemptions so the
// claim routes require authentication on an already-initialized sealed daemon.
// The barrier / unseal surface is state-independent and is unaffected.
//
// Must be called before Start (it rebuilds the router, mirroring
// SetAuthorizationService). A nil probe leaves the gate uninstalled.
func (s *Server) SetSealedWindowGate(
	probe qrdbsdk.SealedWindowProbe,
	operational sealedwindow.OperationalProbe,
	allowlist []string,
) {
	s.sealedProbe = probe
	s.sealedAllowlist = allowlist

	if probe != nil {
		s.sealedBypass = sealedwindow.NewAuthBypass(
			probe, operational, sealedwindow.NewLoggerAuditSink(s.logger))
	} else {
		s.sealedBypass = nil
	}

	// Rebuild the router so the middleware is installed ahead of the authz
	// gates for every route group (setupRouter also propagates the bypass
	// predicate into the installed authz middleware).
	s.setupRouter()
}

// SetSealedWindowHandler installs qrdb's sealed REST surface for the
// allowlisted routes this server has no native handler for (officer
// registration, share claims, recovery officers, ...). Natively served
// routes keep their handlers. Passing nil removes the mounts.
func (s *Server) SetSealedWindowHandler(h http.Handler) {
	s.sealedWindowHandler = h
	s.setupRouter()
}

// sealedWindowNativeAliases are the sealed allowlist paths go-qrdb's sealed
// REST handler cannot answer: it is built without an admin service, so it
// registers neither /health nor /status, and it has no /v1/health. This
// server answers them itself, each with a response an allowlisted route
// already serves while sealed: the health aliases are /api/v1/health, and
// /status is the barrier status of /v1/barrier/status.
func (s *Server) sealedWindowNativeAliases() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/health":    s.HandleHealth,
		"/v1/health": s.HandleHealth,
		"/status":    s.HandleBarrierStatus,
	}
}

// mountSealedWindowRoutes mounts s.sealedWindowHandler on every sealed
// allowlist entry r does not already route. An entry is "METHOD /path"
// (that method only), "/path" (every method) or "/prefix/" (the subtree).
// Called by setupRouter after the native routes are registered, so a native
// handler is never shadowed. The health and status aliases go-qrdb's handler
// cannot answer are served natively instead (sealedWindowNativeAliases).
func (s *Server) mountSealedWindowRoutes(r *chi.Mux) {
	s.sealedMounted = nil
	if s.sealedWindowHandler == nil {
		return
	}
	aliases := s.sealedWindowNativeAliases()
	mounted := chi.NewRouter()
	s.sealedMounted = mounted
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, entry := range s.sealedAllowlist {
		methods := sealedWindowAnyMethod
		path := entry
		if method, rest, ok := strings.Cut(entry, " "); ok {
			methods = []string{method}
			path = rest
		}
		pattern, matchPath := path, path
		if strings.HasSuffix(path, "/") {
			pattern = path + "*"
			matchPath = path + "sealed-window"
		}
		if sealedWindowRouted(r, methods, matchPath) {
			continue
		}
		if h, ok := aliases[path]; ok {
			r.Get(path, h)
			continue
		}
		if len(methods) == 1 {
			r.Method(methods[0], pattern, s.sealedWindowHandler)
			mounted.Method(methods[0], pattern, noop)
			continue
		}
		r.Handle(pattern, s.sealedWindowHandler)
		mounted.Handle(pattern, noop)
	}
}

// sealedListenerProbe is the sealed window's listener probe: the live barrier
// state plus the window's one Admission, shared by every sealed transport.
type sealedListenerProbe = qrdbsealedwindow.ListenerProbe

// SetSealedWindowAdmission makes every natively served request on this
// listener hold probe's Admission for its whole duration: a Transition request
// (barrier initialize / unseal, see sealedwindow.IsRESTTransition)
// exclusively, every other request shared. Once probe reports the barrier
// unsealed, a native request is refused with 503 and
// qrdbsealedwindow.ClosedMessage. Routes served by the sealed-window handler
// are left to it, since it admits on the same probe. Pass the probe given to
// that handler and to the other sealed transports. Must be called before
// Start; nil removes the admission.
func (s *Server) SetSealedWindowAdmission(probe sealedListenerProbe) {
	s.sealedAdmission = probe
	s.setupRouter()
}

// sealedWindowAdmissionMiddleware implements SetSealedWindowAdmission.
func (s *Server) sealedWindowAdmissionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sealedMounted != nil && s.sealedMounted.Match(chi.NewRouteContext(), r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		release := s.sealedAdmission.Admit(sealedwindow.IsRESTTransition(r.Method, r.URL.Path))
		defer release()
		if !s.sealedAdmission.IsSealed() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": qrdbsealedwindow.ClosedMessage})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sealedWindowAnyMethod are the methods an allowlist entry without a method
// is route-matched with.
var sealedWindowAnyMethod = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// sealedWindowRouted reports whether r routes path for any of methods.
func sealedWindowRouted(r *chi.Mux, methods []string, path string) bool {
	for _, method := range methods {
		if r.Match(chi.NewRouteContext(), method, path) {
			return true
		}
	}
	return false
}

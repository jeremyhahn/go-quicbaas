//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"net/http"
)

// =============================================================================
// Bootstrap REST handlers — /api/v1/bootstrap surface.
//
// The bootstrap surface drives first-touch CA discovery for clients that have
// never spoken to the cluster before. It supports three trust-anchor methods
// (DANE/TLSA, Noise PSK, SPKI pin) plus an "auto" entry point that lets the
// daemon pick the strongest method that satisfies the request.
//
// The handlers in this file follow a strict three-stage lifecycle:
//
//  1. The route is always mounted (so callers receive a 5xx, never a 404).
//  2. If the bootstrap servicer is nil, the handler returns 503
//     ServiceUnavailable with ErrBootstrapServiceUnavailable.
//  3. Once the servicer is wired, individual methods that have not yet been
//     implemented return 501 NotImplemented with a per-method message
//     describing the bootstrap mode the caller asked for. As concrete
//     servicer methods land, this file will swap each 501 for a real
//     delegation call.
//
// This staged approach matches the pattern already used by the barrier and
// ceremony handlers: route presence is independent of service readiness, and
// service readiness is independent of method completeness.
// =============================================================================

// requireBootstrapService writes a 503 Service Unavailable response when the
// bootstrap facade is unwired. Returns true when the service is present so
// that callers can continue handling the request.
func (s *Server) requireBootstrapService(w http.ResponseWriter, r *http.Request) bool {
	if s.bootstrapSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBootstrapServiceUnavailable)
		return false
	}
	return true
}

// respondBootstrapNotImplemented emits a 501 NotImplemented response with a
// JSON body that explains which bootstrap mode is unimplemented. We deliver
// this as JSON (rather than plain text) so callers can decode the response
// with the same machinery they use for every other endpoint on the surface.
func respondBootstrapNotImplemented(w http.ResponseWriter, r *http.Request, mode string) {
	RespondJSON(w, r, http.StatusNotImplemented, map[string]interface{}{
		"error": "not implemented",
		"mode":  mode,
		"message": "bootstrap method " + mode +
			" is not yet wired to a concrete servicer; the route is registered" +
			" so callers receive a typed 501 instead of a 404",
	})
}

// HandleBootstrapAuto handles POST /api/v1/bootstrap/auto.
//
// The "auto" mode delegates trust-anchor selection to the daemon: it picks
// the strongest method available (DANE > Noise > SPKI) given the caller's
// reachable network surface and the configured trust policy.
func (s *Server) HandleBootstrapAuto(w http.ResponseWriter, r *http.Request) {
	if !s.requireBootstrapService(w, r) {
		return
	}
	respondBootstrapNotImplemented(w, r, "auto")
}

// HandleBootstrapDane handles POST /api/v1/bootstrap/dane.
//
// DANE/TLSA bootstrap pins the daemon's TLS chain to a TLSA record in DNSSEC.
// Callers submit the resolved TLSA record set; the daemon verifies its own
// certificate chain matches before signing a trust-anchor advertisement.
func (s *Server) HandleBootstrapDane(w http.ResponseWriter, r *http.Request) {
	if !s.requireBootstrapService(w, r) {
		return
	}
	respondBootstrapNotImplemented(w, r, "dane")
}

// HandleBootstrapNoise handles POST /api/v1/bootstrap/noise.
//
// Noise bootstrap establishes trust via a pre-shared key (PSK) Noise XK
// handshake. The PSK is delivered out-of-band (e.g. printed during initial
// `quicbaasd init`) and lets the caller verify cluster identity without any
// existing TLS trust.
func (s *Server) HandleBootstrapNoise(w http.ResponseWriter, r *http.Request) {
	if !s.requireBootstrapService(w, r) {
		return
	}
	respondBootstrapNotImplemented(w, r, "noise")
}

// HandleBootstrapSpki handles POST /api/v1/bootstrap/spki.
//
// SPKI-pin bootstrap matches the daemon's SubjectPublicKeyInfo SHA-256 hash
// against a value supplied by the caller. The pin is the same fingerprint
// that go-truststrap and `xkey` install, so this endpoint completes the loop
// for clients that already have the pin from a previous interaction.
func (s *Server) HandleBootstrapSpki(w http.ResponseWriter, r *http.Request) {
	if !s.requireBootstrapService(w, r) {
		return
	}
	respondBootstrapNotImplemented(w, r, "spki")
}

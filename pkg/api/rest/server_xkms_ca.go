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

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// caHandlerSet is the minimum interface the xkms CA handler wiring must
// expose: the 7 CA operations defined by go-xkms's REST CA contract.
// Declared locally so server.go does not need a direct import on the
// go-xkms REST package. *xkmsrest.CAHandlers satisfies this interface
// structurally.
type caHandlerSet interface {
	HandleGetCABundle(w http.ResponseWriter, r *http.Request)
	HandleGetCACertificate(w http.ResponseWriter, r *http.Request)
	HandleSignCSR(w http.ResponseWriter, r *http.Request)
	HandleIssueCertificate(w http.ResponseWriter, r *http.Request)
	HandleRevokeCertificate(w http.ResponseWriter, r *http.Request)
	HandleGenerateCRL(w http.ResponseWriter, r *http.Request)
	HandleIsRevoked(w http.ResponseWriter, r *http.Request)
}

// Compile-time assertion that go-xkms's *CAHandlers satisfies the
// caHandlerSet interface shape. Guards against interface drift across
// go-xkms releases.
var _ caHandlerSet = (*xkmssdk.CAHandlers)(nil)

// SetCertificateAuthority installs a TrustedCA-backed CA handler set on
// the REST server. After this call, the /api/v1/xkms/ca/* routes delegate
// to the handlers exported by github.com/jeremyhahn/go-xkms/pkg/api/rest
// instead of returning 503. Passing nil tears down the wiring.
//
// The CA must already be initialized (Load or Init called). Safe to call
// at any point before Start; after Start it is racy only with in-flight
// CA requests, which resolve against whichever handlers were installed
// at the time of dispatch.
//
// Wired from pkg/server.lifecycle_rest.go once the embedded TrustedCA
// has completed bootstrap.
func (s *Server) SetCertificateAuthority(ca xkmssdk.TrustedCA) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fleetCA = ca
	if ca == nil {
		s.xkmsCAHandlers = nil
		return
	}

	// newXKMSCAServicer wraps the TrustedCA so it satisfies go-xkms's
	// transport CAService interface. go-xkms's CAHandlers then treats
	// the adapter as a regular servicer.
	servicer := newXKMSCAServicer(ca, s.config.SPIFFETrustDomain)

	// nil logger → CAHandlers falls back to slog.Default(), which is
	// already configured by the server process.
	s.xkmsCAHandlers = xkmssdk.NewCAHandlers(servicer, nil)
}

// fleetCertificateAuthority returns the installed TrustedCA, or nil before
// SetCertificateAuthority.
func (s *Server) fleetCertificateAuthority() xkmssdk.TrustedCA {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fleetCA
}

// caHandlers returns the currently installed CA handler set, or nil if
// the CA has not been wired yet. Read under the server mutex so
// SetCertificateAuthority races write cleanly.
func (s *Server) caHandlers() caHandlerSet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.xkmsCAHandlers
}

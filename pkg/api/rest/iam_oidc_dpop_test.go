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

//go:build rest

package rest

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRequestAbsoluteURI verifies the RFC 9449 DPoP htu reconstruction the token
// endpoint feeds to the issuer. The htu must reflect the externally-visible URL
// the client dialed, so TLS presence and the reverse-proxy forwarding headers
// must be honored; query and fragment are dropped (the verifier canonicalizes to
// scheme+host+path).
func TestRequestAbsoluteURI(t *testing.T) {
	t.Run("plain HTTP uses http scheme and Host", func(t *testing.T) {
		r := httptest.NewRequest("POST", "http://broker.example/oauth2/token", nil)
		r.Host = "broker.example"
		assert.Equal(t, "http://broker.example/oauth2/token", requestAbsoluteURI(r))
	})

	t.Run("TLS connection uses https scheme", func(t *testing.T) {
		r := httptest.NewRequest("POST", "https://broker.example/oauth2/token", nil)
		r.Host = "broker.example"
		r.TLS = &tls.ConnectionState{}
		assert.Equal(t, "https://broker.example/oauth2/token", requestAbsoluteURI(r))
	})

	t.Run("X-Forwarded-Proto and -Host win for proxied deployments", func(t *testing.T) {
		// A TLS-terminating proxy fronts the service: the inbound connection is
		// plain HTTP to an internal host, but the client dialed the external
		// https URL, which is what it signed into the proof htu.
		r := httptest.NewRequest("POST", "http://10.0.0.5:8080/oauth2/token", nil)
		r.Host = "10.0.0.5:8080"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "broker.example")
		assert.Equal(t, "https://broker.example/oauth2/token", requestAbsoluteURI(r))
	})

	t.Run("query and fragment are not part of the reconstructed URI", func(t *testing.T) {
		r := httptest.NewRequest("POST", "http://broker.example/oauth2/token?x=1", nil)
		r.Host = "broker.example"
		assert.Equal(t, "http://broker.example/oauth2/token", requestAbsoluteURI(r))
	})
}

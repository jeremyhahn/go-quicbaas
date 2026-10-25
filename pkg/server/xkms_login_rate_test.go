//go:build !integration

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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xkmsDefaultLoginRatePerMin is go-xkms's per-client login limit when the
// session options leave LoginRatePerMin at zero.
const xkmsDefaultLoginRatePerMin = 10

// TestXKMSLogin_ConfiguredRateReachesModule is go-quicbaas#1771: the hosted
// go-xkms login plane takes its per-client login limit from
// iam.login_rate_per_min. The module offers no clock seam, so the logins are
// sent back to back, well inside one rate window.
func TestXKMSLogin_ConfiguredRateReachesModule(t *testing.T) {
	const limit = 20
	srv, _, cert := newIntegrationLikeNode(t, func(s *Server) {
		s.config.IAM.LoginRatePerMin = limit
	})
	router := xkmsPIVLoginRouter(t, srv)

	for i := 1; i <= limit; i++ {
		rec := postXKMSPIVLogin(router, cert)
		require.Equal(t, http.StatusOK, rec.Code, "login %d of %d: %s", i, limit, rec.Body.String())
	}

	rec := postXKMSPIVLogin(router, cert)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "login %d must be refused for rate", limit+1)
}

// TestXKMSLogin_UnsetRateUsesXKMSDefault: with iam.login_rate_per_min unset
// the module keeps go-xkms's own default limit.
func TestXKMSLogin_UnsetRateUsesXKMSDefault(t *testing.T) {
	srv, _, cert := newIntegrationLikeNode(t)
	require.Zero(t, srv.loginSessionModuleOptions().LoginRatePerMin)
	router := xkmsPIVLoginRouter(t, srv)

	for i := 1; i <= xkmsDefaultLoginRatePerMin; i++ {
		rec := postXKMSPIVLogin(router, cert)
		require.Equal(t, http.StatusOK, rec.Code, "login %d: %s", i, rec.Body.String())
	}

	rec := postXKMSPIVLogin(router, cert)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code,
		"login %d must be refused by go-xkms's default limit", xkmsDefaultLoginRatePerMin+1)
}

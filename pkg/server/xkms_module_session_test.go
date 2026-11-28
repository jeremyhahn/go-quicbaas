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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestXKMSModuleSessionOptions_FromConfig pins that the hosted xkms login
// module takes its issuer, token lifetimes and login rate from quicbaas
// config rather than the module's own defaults.
func TestXKMSModuleSessionOptions_FromConfig(t *testing.T) {
	t.Run("configured values", func(t *testing.T) {
		s := newTestServer(t)
		s.config = &config.Config{}
		s.config.Server.Host = "127.0.0.1"
		s.config.Server.REST.Port = 8443
		s.config.Auth.AccessTokenTTL = 7 * time.Minute
		s.config.Auth.RefreshTokenTTL = 3 * time.Hour
		s.config.IAM.LoginRatePerMin = 25

		opts := s.xkmsModuleSessionOptions()
		require.NotNil(t, opts)
		assert.Equal(t, issuerURLFromConfig(s.config), opts.Issuer)
		assert.NotEmpty(t, opts.Issuer)
		assert.Equal(t, 7*time.Minute, opts.AccessTTL)
		assert.Equal(t, 3*time.Hour, opts.RefreshTTL)
		assert.Equal(t, 25, opts.LoginRatePerMin)
	})

	t.Run("zero config takes the OIDC defaults", func(t *testing.T) {
		s := newTestServer(t)
		s.config = &config.Config{}

		opts := s.xkmsModuleSessionOptions()
		require.NotNil(t, opts)
		assert.Equal(t, issuerURLFromConfig(s.config), opts.Issuer)
		assert.Equal(t, config.DefaultOIDCAccessTokenTTL, opts.AccessTTL)
		assert.Equal(t, config.DefaultOIDCRefreshTokenTTL, opts.RefreshTTL)
		assert.Equal(t, 0, opts.LoginRatePerMin)
	})
}

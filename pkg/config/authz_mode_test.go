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

package config

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func TestParseAuthzMode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want platformtoken.AuthzMode
	}{
		{"empty defaults to hybrid", "", platformtoken.AuthzModeHybrid},
		{"offline", "offline", platformtoken.AuthzModeOffline},
		{"online", "online", platformtoken.AuthzModeOnline},
		{"hybrid", "hybrid", platformtoken.AuthzModeHybrid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAuthzMode(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseAuthzModeInvalid(t *testing.T) {
	got, err := ParseAuthzMode("bogus")
	require.Error(t, err)
	assert.Empty(t, string(got))

	var invalid *InvalidAuthzModeError
	require.True(t, errors.As(err, &invalid), "expected *InvalidAuthzModeError, got %T", err)
	assert.Equal(t, "bogus", invalid.Value)
	assert.Contains(t, invalid.Error(), "offline, online, hybrid")
	assert.Contains(t, invalid.Error(), "bogus")
}

func TestSecurityConfigEffectiveAuthzMode(t *testing.T) {
	t.Run("unset resolves to hybrid", func(t *testing.T) {
		sec := SecurityConfig{}
		mode, err := sec.EffectiveAuthzMode()
		require.NoError(t, err)
		assert.Equal(t, platformtoken.AuthzModeHybrid, mode)
	})

	t.Run("explicit offline", func(t *testing.T) {
		sec := SecurityConfig{AuthzMode: "offline"}
		mode, err := sec.EffectiveAuthzMode()
		require.NoError(t, err)
		assert.Equal(t, platformtoken.AuthzModeOffline, mode)
	})

	t.Run("invalid surfaces error", func(t *testing.T) {
		sec := SecurityConfig{AuthzMode: "nope"}
		_, err := sec.EffectiveAuthzMode()
		require.Error(t, err)
		var invalid *InvalidAuthzModeError
		assert.True(t, errors.As(err, &invalid))
	})
}

func TestSecurityConfigValidateAuthzMode(t *testing.T) {
	t.Run("empty is valid", func(t *testing.T) {
		sec := SecurityConfig{}
		assert.NoError(t, sec.ValidateAuthzMode())
	})

	t.Run("online is valid", func(t *testing.T) {
		sec := SecurityConfig{AuthzMode: "online"}
		assert.NoError(t, sec.ValidateAuthzMode())
	})

	t.Run("garbage is invalid", func(t *testing.T) {
		sec := SecurityConfig{AuthzMode: "garbage"}
		err := sec.ValidateAuthzMode()
		require.Error(t, err)
		var invalid *InvalidAuthzModeError
		assert.True(t, errors.As(err, &invalid))
	})
}

func TestSecurityConfigEffectiveLivenessCacheTTL(t *testing.T) {
	t.Run("zero falls back to default", func(t *testing.T) {
		sec := SecurityConfig{}
		assert.Equal(t, DefaultLivenessCacheTTL, sec.EffectiveLivenessCacheTTL())
	})

	t.Run("negative falls back to default", func(t *testing.T) {
		sec := SecurityConfig{LivenessCacheTTL: -5 * time.Second}
		assert.Equal(t, DefaultLivenessCacheTTL, sec.EffectiveLivenessCacheTTL())
	})

	t.Run("positive is honored", func(t *testing.T) {
		sec := SecurityConfig{LivenessCacheTTL: 42 * time.Second}
		assert.Equal(t, 42*time.Second, sec.EffectiveLivenessCacheTTL())
	})
}

func TestAuthzModeDefaultsWired(t *testing.T) {
	t.Run("DefaultSecurityConfig", func(t *testing.T) {
		sec := DefaultSecurityConfig()
		assert.Equal(t, DefaultAuthzMode, sec.AuthzMode)
		assert.Equal(t, "hybrid", sec.AuthzMode)
		assert.Equal(t, DefaultLivenessCacheTTL, sec.LivenessCacheTTL)
		assert.Equal(t, 15*time.Second, sec.LivenessCacheTTL)

		mode, err := sec.EffectiveAuthzMode()
		require.NoError(t, err)
		assert.Equal(t, platformtoken.AuthzModeHybrid, mode)
	})

	t.Run("DefaultConfig top-level", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, "hybrid", cfg.Security.AuthzMode)
		assert.Equal(t, 15*time.Second, cfg.Security.LivenessCacheTTL)
	})
}

func TestAuthzModeEnvOverrides(t *testing.T) {
	t.Run("QUICBAAS_SECURITY_AUTHZ_MODE overrides", func(t *testing.T) {
		os.Setenv("QUICBAAS_SECURITY_AUTHZ_MODE", "online")
		defer os.Unsetenv("QUICBAAS_SECURITY_AUTHZ_MODE")

		loader := NewLoader()
		cfg, err := loader.Load("")
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "online", cfg.Security.AuthzMode)
	})

	t.Run("QUICBAAS_SECURITY_LIVENESS_CACHE_TTL overrides", func(t *testing.T) {
		os.Setenv("QUICBAAS_SECURITY_LIVENESS_CACHE_TTL", "30s")
		defer os.Unsetenv("QUICBAAS_SECURITY_LIVENESS_CACHE_TTL")

		loader := NewLoader()
		cfg, err := loader.Load("")
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 30*time.Second, cfg.Security.LivenessCacheTTL)
	})

	t.Run("invalid TTL string fails the load", func(t *testing.T) {
		t.Setenv("QUICBAAS_SECURITY_LIVENESS_CACHE_TTL", "not-a-duration")

		loader := NewLoader()
		cfg, err := loader.Load("")
		require.ErrorIs(t, err, ErrInvalidEnvValue)
		assert.Nil(t, cfg)
	})
}

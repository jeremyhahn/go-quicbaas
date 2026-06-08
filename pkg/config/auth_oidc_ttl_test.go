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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestOIDCTokenTTLDefaults verifies that unset (zero) TTLs resolve to the
// historical hardcoded broker lifetimes, preserving existing behavior.
func TestOIDCTokenTTLDefaults(t *testing.T) {
	a := &AuthConfig{}

	assert.Equal(t, DefaultOIDCAccessTokenTTL, a.OIDCAccessTokenTTL())
	assert.Equal(t, DefaultOIDCRefreshTokenTTL, a.OIDCRefreshTokenTTL())
	assert.Equal(t, DefaultOIDCIDTokenTTL, a.OIDCIDTokenTTL())
	assert.Equal(t, DefaultOIDCAuthCodeTTL, a.OIDCAuthCodeTTL())

	// Confirm the defaults match the historical literals.
	assert.Equal(t, 15*time.Minute, DefaultOIDCAccessTokenTTL)
	assert.Equal(t, 7*24*time.Hour, DefaultOIDCRefreshTokenTTL)
	assert.Equal(t, 1*time.Hour, DefaultOIDCIDTokenTTL)
	assert.Equal(t, 10*time.Minute, DefaultOIDCAuthCodeTTL)
}

// TestOIDCTokenTTLDefaultsOnNegative verifies negative values also fall back to
// the defaults (defensive: a misconfigured negative duration is treated as
// unset rather than producing an invalid lifetime).
func TestOIDCTokenTTLDefaultsOnNegative(t *testing.T) {
	a := &AuthConfig{
		AccessTokenTTL:  -1,
		RefreshTokenTTL: -1,
		IDTokenTTL:      -1,
		AuthCodeTTL:     -1,
	}

	assert.Equal(t, DefaultOIDCAccessTokenTTL, a.OIDCAccessTokenTTL())
	assert.Equal(t, DefaultOIDCRefreshTokenTTL, a.OIDCRefreshTokenTTL())
	assert.Equal(t, DefaultOIDCIDTokenTTL, a.OIDCIDTokenTTL())
	assert.Equal(t, DefaultOIDCAuthCodeTTL, a.OIDCAuthCodeTTL())
}

// TestOIDCTokenTTLConfigured verifies explicit, in-range TTLs are honored.
func TestOIDCTokenTTLConfigured(t *testing.T) {
	a := &AuthConfig{
		AccessTokenTTL:  5 * time.Minute,
		RefreshTokenTTL: 48 * time.Hour,
		IDTokenTTL:      30 * time.Minute,
		AuthCodeTTL:     2 * time.Minute,
	}

	assert.Equal(t, 5*time.Minute, a.OIDCAccessTokenTTL())
	assert.Equal(t, 48*time.Hour, a.OIDCRefreshTokenTTL())
	assert.Equal(t, 30*time.Minute, a.OIDCIDTokenTTL())
	assert.Equal(t, 2*time.Minute, a.OIDCAuthCodeTTL())
}

// TestValidateAuthAccessTokenTTLBounds verifies the access-token TTL bound
// check: zero is accepted (defaulted), in-range values pass, and out-of-range
// values are rejected by the validator.
func TestValidateAuthAccessTokenTTLBounds(t *testing.T) {
	tests := []struct {
		name      string
		ttl       time.Duration
		wantError bool
	}{
		{"unset is accepted", 0, false},
		{"lower bound accepted", DefaultOIDCAccessTokenTTLMin, false},
		{"upper bound accepted", DefaultOIDCAccessTokenTTLMax, false},
		{"mid-range accepted", 10 * time.Minute, false},
		{"below lower bound rejected", 30 * time.Second, true},
		{"above upper bound rejected", 2 * time.Hour, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator()
			v.validateAuth(&AuthConfig{AccessTokenTTL: tt.ttl})
			err := v.Errors()
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "auth.access_token_ttl")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateAuthOtherTTLsNonNegative verifies that negative refresh/id/auth-
// code TTLs are rejected while zero (default) and positive values pass.
func TestValidateAuthOtherTTLsNonNegative(t *testing.T) {
	tests := []struct {
		name      string
		cfg       AuthConfig
		field     string
		wantError bool
	}{
		{"zero refresh ok", AuthConfig{}, "", false},
		{"negative refresh rejected", AuthConfig{RefreshTokenTTL: -1}, "auth.refresh_token_ttl", true},
		{"negative id rejected", AuthConfig{IDTokenTTL: -1}, "auth.id_token_ttl", true},
		{"negative auth code rejected", AuthConfig{AuthCodeTTL: -1}, "auth.auth_code_ttl", true},
		{"positive values ok", AuthConfig{RefreshTokenTTL: time.Hour, IDTokenTTL: time.Hour, AuthCodeTTL: time.Minute}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator()
			v.validateAuth(&tt.cfg)
			err := v.Errors()
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.field)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

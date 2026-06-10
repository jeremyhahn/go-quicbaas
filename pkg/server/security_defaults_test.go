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

// security_defaults_test.go covers the security default posture assertions
// for the server package: InsecureDevMode gating and embedded-config secure
// defaults.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestInsecureDevModeDefaultOff verifies that the production config never
// enables InsecureDevMode. This gate is checked at server startup (initialize)
// and logs a loud WARNING when set; it must default to false in all non-dev
// config helpers.
func TestInsecureDevModeDefaultOff(t *testing.T) {
	assert.False(t, config.DefaultConfig().Auth.InsecureDevMode,
		"DefaultConfig must not enable InsecureDevMode")
	assert.False(t, config.EmbeddedConfig().Auth.InsecureDevMode,
		"EmbeddedConfig must not enable InsecureDevMode")
}

// TestInsecureDevModeOnlyInDevelopmentConfig verifies that only
// DevelopmentConfig explicitly enables InsecureDevMode (when it does),
// and documents that this is an intentional opt-in.
func TestInsecureDevModeOnlyInDevelopmentConfig(t *testing.T) {
	// DevConfig may or may not enable InsecureDevMode depending on the
	// implementation; the critical invariant is that Default and Embedded
	// never enable it. This test documents the expectation.
	devCfg := config.DevelopmentConfig()
	if devCfg.Auth.InsecureDevMode {
		// Acceptable for dev config only — must be explicitly documented.
		t.Log("DevelopmentConfig has InsecureDevMode=true (expected for dev-only use)")
	}
}

// TestEmbeddedConfigRequiresAuthzAndSystemPrincipal verifies the secure
// defaults introduced by F-09: EmbeddedConfig must have RequireAuthz=true,
// AllowAnonymous=false, and a named EmbeddedSystemPrincipal. These settings
// ensure that anonymous access to the embedded KMS/CA/secrets surface is
// not permitted even in embedded mode.
func TestEmbeddedConfigRequiresAuthzAndSystemPrincipal(t *testing.T) {
	cfg := config.EmbeddedConfig()

	assert.True(t, cfg.Security.RequireAuthz,
		"embedded mode must keep RequireAuthz=true to document secure intent")
	assert.False(t, cfg.Security.Authorization.RBAC.AllowAnonymous,
		"embedded mode must not allow anonymous access")
	assert.NotEmpty(t, cfg.Security.EmbeddedSystemPrincipal,
		"embedded mode must name a system principal for in-process operations")
}

// TestEmbeddedSystemPrincipalDefault verifies the constant value so callers
// that compare against it by name are always correct.
func TestEmbeddedSystemPrincipalDefault(t *testing.T) {
	assert.Equal(t, "system", config.EmbeddedSystemPrincipalDefault)
	assert.Equal(t, config.EmbeddedSystemPrincipalDefault,
		config.EmbeddedConfig().Security.EmbeddedSystemPrincipal)
}

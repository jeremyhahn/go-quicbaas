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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The boot gate must run IAMDynamicClientRegistrationConfig.Validate, so a
// configuration that would let unauthenticated callers mint clients in the
// system tenant is refused at startup.

func TestValidator_DynamicClientRegistration_DisabledWithSystemTenantPasses(t *testing.T) {
	v := NewValidator()
	v.validateIAM(&IAMConfig{DynamicClientRegistration: IAMDynamicClientRegistrationConfig{
		Enabled:           false,
		AnonymousTenantID: SystemTenantID,
	}})
	assert.NoError(t, v.Errors())
}

func TestValidator_DynamicClientRegistration_EnabledWithSystemTenantRefused(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  dynamic_client_registration:
    enabled: true
    anonymous_tenant_id: system
`)
	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	err := v.Errors()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "iam.dynamic_client_registration.anonymous_tenant_id"),
		"expected the error to name the anonymous_tenant_id field, got: %v", err)
}

func TestValidator_DynamicClientRegistration_EnabledWithTenantPasses(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  dynamic_client_registration:
    enabled: true
    anonymous_tenant_id: public-apps
`)
	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	assert.NoError(t, v.Errors())
}

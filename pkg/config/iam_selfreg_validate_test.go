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

// The boot gate must run IAMSelfServiceRegistrationConfig.Validate, so an
// invalid enabled section is refused at startup and not at the first
// registration.

func TestValidator_SelfServiceRegistration_DisabledWithGarbagePasses(t *testing.T) {
	v := NewValidator()
	v.validateIAM(&IAMConfig{SelfServiceRegistration: IAMSelfServiceRegistrationConfig{
		Enabled:               false,
		DefaultRole:           "NoSuchRole",
		AllowedIsolationModes: []string{"no-such-mode"},
		DefaultIsolationMode:  "no-such-mode",
	}})
	assert.NoError(t, v.Errors())
}

func TestValidator_SelfServiceRegistration_EnabledWithDefaultsPasses(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	assert.NoError(t, v.Errors())
}

func TestValidator_SelfServiceRegistration_UnknownRoleRefused(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    default_role: NoSuchRole
`)
	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	err := v.Errors()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "iam.self_service_registration.default_role"),
		"expected the error to name the default_role field, got: %v", err)
}

func TestValidator_SelfServiceRegistration_BarrierWithEmptyPoolRefused(t *testing.T) {
	cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    allowed_isolation_modes: [keyspace, barrier]
    tenant_barrier_volume_pool: ""
`)
	v := NewValidator()
	v.validateIAM(&cfg.IAM)
	err := v.Errors()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "iam.self_service_registration.tenant_barrier_volume_pool"),
		"expected the error to name the tenant_barrier_volume_pool field, got: %v", err)
}

func TestValidator_AddSectionError_FilesForeignErrorsUnderTheSection(t *testing.T) {
	v := NewValidator()
	v.addSectionError(nil, "iam.self_service_registration")
	require.NoError(t, v.Errors())

	v.addSectionError(errTestForeignSectionError, "iam.self_service_registration")
	err := v.Errors()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iam.self_service_registration")
	assert.Contains(t, err.Error(), errTestForeignSectionError.Error())
}

// testForeignSectionError is an error that is not a ConfigValidationError.
type testForeignSectionError struct{}

func (testForeignSectionError) Error() string { return "foreign section failure" }

var errTestForeignSectionError = testForeignSectionError{}

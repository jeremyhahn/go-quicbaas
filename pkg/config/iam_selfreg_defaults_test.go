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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadSelfRegYAML writes content to a temporary config file and loads it
// through the production loader.
func loadSelfRegYAML(t *testing.T, content string) *Config {
	t.Helper()
	configFile := filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o600))
	cfg, err := NewLoader().Load(configFile)
	require.NoError(t, err)
	return cfg
}

// TestSelfServiceRegistration_RequireAdminApprovalDefaults pins the
// documented safe default: a self-registered account is held pending admin
// approval unless the operator explicitly opts out. Before the default was
// applied, the zero value false let every registrant through unreviewed.
func TestSelfServiceRegistration_RequireAdminApprovalDefaults(t *testing.T) {
	t.Run("Default() approval on", func(t *testing.T) {
		assert.True(t, Default().IAM.SelfServiceRegistration.RequireAdminApproval)
	})

	t.Run("no key gives true", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
		assert.True(t, cfg.IAM.SelfServiceRegistration.Enabled)
		assert.True(t, cfg.IAM.SelfServiceRegistration.RequireAdminApproval)
	})

	t.Run("explicit false gives false", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    require_admin_approval: false
`)
		assert.False(t, cfg.IAM.SelfServiceRegistration.RequireAdminApproval)
	})

	t.Run("explicit true gives true", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    require_admin_approval: true
`)
		assert.True(t, cfg.IAM.SelfServiceRegistration.RequireAdminApproval)
	})

	t.Run("env var overrides YAML", func(t *testing.T) {
		t.Setenv("QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_REQUIRE_ADMIN_APPROVAL", "false")
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    require_admin_approval: true
`)
		assert.False(t, cfg.IAM.SelfServiceRegistration.RequireAdminApproval)
	})
}

// TestSelfServiceRegistration_DefaultRoleDefaults pins the documented
// default role. Before the default was applied, an operator who enabled
// registration and left default_role unset failed validation at startup
// instead of getting "User".
func TestSelfServiceRegistration_DefaultRoleDefaults(t *testing.T) {
	t.Run("no key gives User", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
		assert.Equal(t, "User", cfg.IAM.SelfServiceRegistration.DefaultRole)
	})

	t.Run("enabled with no key passes validation", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
		assert.NoError(t, cfg.IAM.SelfServiceRegistration.Validate())
	})

	t.Run("explicit valid role wins", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    default_role: Auditor
`)
		assert.Equal(t, "Auditor", cfg.IAM.SelfServiceRegistration.DefaultRole)
		assert.NoError(t, cfg.IAM.SelfServiceRegistration.Validate())
	})

	t.Run("explicit invalid role is rejected", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    default_role: Superuser
`)
		err := cfg.IAM.SelfServiceRegistration.Validate()
		var validationErr *ConfigValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "iam.self_service_registration.default_role", validationErr.Field)
	})
}

// TestSelfServiceRegistration_DefaultIsolationModeDefaults pins the documented
// default_isolation_mode of "keyspace" in the config itself, so the validator
// no longer has to substitute it.
func TestSelfServiceRegistration_DefaultIsolationModeDefaults(t *testing.T) {
	t.Run("no key gives keyspace", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
		assert.Equal(t, TenantIsolationKeyspace, cfg.IAM.SelfServiceRegistration.DefaultIsolationMode)
	})

	t.Run("explicit allowed mode wins", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    allowed_isolation_modes: [keyspace, barrier]
    default_isolation_mode: barrier
`)
		assert.Equal(t, TenantIsolationBarrier, cfg.IAM.SelfServiceRegistration.DefaultIsolationMode)
		assert.NoError(t, cfg.IAM.SelfServiceRegistration.Validate())
	})

	t.Run("explicit empty mode is rejected", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    default_isolation_mode: ""
`)
		err := cfg.IAM.SelfServiceRegistration.Validate()
		var validationErr *ConfigValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "iam.self_service_registration.default_isolation_mode", validationErr.Field)
	})
}

// TestSelfServiceRegistration_TenantBarrierVolumePoolDefaults pins the
// documented tenant_barrier_volume_pool of "default": allowing barrier or
// volume isolation without naming a pool must not fail validation.
func TestSelfServiceRegistration_TenantBarrierVolumePoolDefaults(t *testing.T) {
	t.Run("no key gives default", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
`)
		assert.Equal(t, "default", cfg.IAM.SelfServiceRegistration.TenantBarrierVolumePool)
	})

	t.Run("explicit pool wins", func(t *testing.T) {
		cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    tenant_barrier_volume_pool: fast-nvme
`)
		assert.Equal(t, "fast-nvme", cfg.IAM.SelfServiceRegistration.TenantBarrierVolumePool)
	})

	for _, mode := range []string{TenantIsolationBarrier, TenantIsolationVolume} {
		t.Run(mode+" allowed with no pool key passes validation", func(t *testing.T) {
			cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    allowed_isolation_modes: [keyspace, `+mode+`]
`)
			assert.NoError(t, cfg.IAM.SelfServiceRegistration.Validate())
		})

		t.Run(mode+" allowed with explicit empty pool is rejected", func(t *testing.T) {
			cfg := loadSelfRegYAML(t, `
iam:
  self_service_registration:
    enabled: true
    allowed_isolation_modes: [keyspace, `+mode+`]
    tenant_barrier_volume_pool: ""
`)
			err := cfg.IAM.SelfServiceRegistration.Validate()
			var validationErr *ConfigValidationError
			require.ErrorAs(t, err, &validationErr)
			assert.Equal(t, "iam.self_service_registration.tenant_barrier_volume_pool", validationErr.Field)
		})
	}
}

// TestSelfServiceRegistration_EnvironmentReachesAnAbsentKey pins that the
// environment alone, with no file entry, sets the registration knobs. Before
// the keys were bound, AutomaticEnv reached only keys the file also named.
func TestSelfServiceRegistration_EnvironmentReachesAnAbsentKey(t *testing.T) {
	t.Setenv("QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_ENABLED", "true")
	t.Setenv("QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_REQUIRE_ADMIN_APPROVAL", "false")
	t.Setenv("QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_DEFAULT_ROLE", "Auditor")

	cfg := loadSelfRegYAML(t, `
server:
  mode: standalone
`)
	reg := cfg.IAM.SelfServiceRegistration
	assert.True(t, reg.Enabled)
	assert.False(t, reg.RequireAdminApproval)
	assert.Equal(t, "Auditor", reg.DefaultRole)
}

// TestSelfServiceRegistration_UnparseableEnvironmentRefusesTheLoad pins that
// a malformed boolean in the environment fails the load rather than being
// silently read as false.
func TestSelfServiceRegistration_UnparseableEnvironmentRefusesTheLoad(t *testing.T) {
	t.Setenv("QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_ENABLED", "maybe")

	configFile := filepath.Join(t.TempDir(), "quicbaas.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("server:\n  mode: standalone\n"), 0o600))
	_, err := NewLoader().Load(configFile)
	assert.Error(t, err)
}

// TestSelfServiceRegistration_EveryScalarFieldHasABoundKey is the drift gate:
// a scalar field added to IAMSelfServiceRegistrationConfig without a bound key
// is a knob the environment cannot reach.
func TestSelfServiceRegistration_EveryScalarFieldHasABoundKey(t *testing.T) {
	const prefix = "iam.self_service_registration."

	keys := map[string]bool{}
	for _, key := range IAMSelfServiceRegistrationKeys {
		require.True(t, strings.HasPrefix(key, prefix), "key %q is not under %q", key, prefix)
		keys[key] = true
	}

	named := map[string]bool{}
	structType := reflect.TypeOf(IAMSelfServiceRegistrationConfig{})
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		key := prefix + field.Tag.Get("mapstructure")
		if field.Type.Kind() == reflect.Slice {
			assert.False(t, keys[key], "list field %s is file-only and must not be bound", field.Name)
			continue
		}
		named[key] = true
		assert.True(t, keys[key], "field %s maps to %q, which IAMSelfServiceRegistrationKeys does not name", field.Name, key)
	}
	for key := range keys {
		assert.True(t, named[key], "IAMSelfServiceRegistrationKeys names %q, which no field maps to", key)
	}
}

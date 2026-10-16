//go:build mcp

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

package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// convertStringNumerics Tests
// =============================================================================

func TestConvertStringNumerics_IntegerConversion(t *testing.T) {
	input := map[string]interface{}{
		"port":    "8080",
		"timeout": "30",
	}
	result := convertStringNumerics(input)
	assert.Equal(t, 8080, result["port"])
	assert.Equal(t, 30, result["timeout"])
}

func TestConvertStringNumerics_BooleanConversion(t *testing.T) {
	input := map[string]interface{}{
		"enabled":  "true",
		"disabled": "false",
	}
	result := convertStringNumerics(input)
	assert.Equal(t, true, result["enabled"])
	assert.Equal(t, false, result["disabled"])
}

func TestConvertStringNumerics_NonStringValues(t *testing.T) {
	input := map[string]interface{}{
		"count":  42,
		"flag":   true,
		"nested": map[string]interface{}{"key": "value"},
	}
	result := convertStringNumerics(input)
	assert.Equal(t, 42, result["count"])
	assert.Equal(t, true, result["flag"])
	assert.NotNil(t, result["nested"])
}

func TestConvertStringNumerics_MixedValues(t *testing.T) {
	input := map[string]interface{}{
		"name":    "test-service",
		"port":    "443",
		"enabled": "true",
		"count":   5,
	}
	result := convertStringNumerics(input)
	assert.Equal(t, "test-service", result["name"])
	assert.Equal(t, 443, result["port"])
	assert.Equal(t, true, result["enabled"])
	assert.Equal(t, 5, result["count"])
}

func TestConvertStringNumerics_EmptyMap(t *testing.T) {
	result := convertStringNumerics(map[string]interface{}{})
	assert.Empty(t, result)
}

// =============================================================================
// buildPolicyFromSDKFormat Tests
// =============================================================================

func TestBuildPolicyFromSDKFormat_NoSpecYAML(t *testing.T) {
	// Without spec_yaml the policy has no rules; it is refused, not built.
	policy, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"name":        "test-policy",
		"description": "A test policy",
	}, "tenant-1")
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err), "error = %v, want INVALID_INPUT", err)
	assert.Nil(t, policy)
}

func TestBuildPolicyFromSDKFormat_TranslatesMetadata(t *testing.T) {
	policy, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"name":      "read-docs",
		"spec_yaml": "rules: []",
		"labels":    map[string]interface{}{"team": "a"},
		"priority":  float64(5),
		"enabled":   true,
		"effect":    float64(0),
	}, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "a"}, policy.Metadata.Labels)
	assert.Equal(t, 5, policy.Metadata.Priority)
}

func TestBuildPolicyFromSDKFormat_RefusesUntranslatedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		val  interface{}
	}{
		{"rules", "rules", []interface{}{map[string]interface{}{"effect": "allow"}}},
		{"targets", "targets", []interface{}{}},
		{"conditions", "conditions", []interface{}{}},
		{"enabled_false", "enabled", false},
		{"effect_allow_number", "effect", float64(1)},
		{"effect_allow_string", "effect", "allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildPolicyFromSDKFormat(map[string]interface{}{
				"name":      "p",
				"spec_yaml": "rules: []",
				tc.key:      tc.val,
			}, "tenant-1")
			assert.True(t, errors.IsInvalidInput(err), "error = %v, want INVALID_INPUT", err)
		})
	}
}

func TestBuildPolicyFromSDKFormat_SpecOnlyYAML(t *testing.T) {
	specYAML := `rules:
  - actions: ["read"]
    resources: ["documents"]
    effect: "allow"`

	policy, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"name":        "read-docs",
		"spec_yaml":   specYAML,
		"description": "Read documents policy",
	}, "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "read-docs", policy.Metadata.Name)
	assert.Equal(t, "iam.quicbaas.io/v1", policy.APIVersion)
}

func TestBuildPolicyFromSDKFormat_FullPolicyYAML(t *testing.T) {
	fullYAML := `apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: original-name
spec:
  rules:
    - actions: ["write"]
      resources: ["files"]
      effect: "allow"`

	policy, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"name":        "override-name",
		"spec_yaml":   fullYAML,
		"description": "Override desc",
	}, "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "override-name", policy.Metadata.Name)
	assert.Equal(t, "tenant-1", policy.Metadata.Tenant)
	assert.Equal(t, "Override desc", policy.Metadata.Description)
}

func TestBuildPolicyFromSDKFormat_InvalidSpecYAML(t *testing.T) {
	_, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"spec_yaml": "{{invalid yaml",
	}, "tenant-1")
	require.Error(t, err)
}

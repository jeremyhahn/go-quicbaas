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
	policy, err := buildPolicyFromSDKFormat(map[string]interface{}{
		"name":        "test-policy",
		"description": "A test policy",
	}, "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "test-policy", policy.Metadata.Name)
	assert.Equal(t, "tenant-1", policy.Metadata.Tenant)
	assert.Equal(t, "A test policy", policy.Metadata.Description)
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

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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsIAMService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "user service",
			service:  "quicbaas.IAM.User",
			expected: true,
		},
		{
			name:     "tenant service",
			service:  "quicbaas.IAM.Tenant",
			expected: true,
		},
		{
			name:     "group service",
			service:  "quicbaas.IAM.Group",
			expected: true,
		},
		{
			name:     "role service",
			service:  "quicbaas.IAM.Role",
			expected: true,
		},
		{
			name:     "policy service",
			service:  "quicbaas.IAM.Policy",
			expected: true,
		},
		{
			name:     "app service",
			service:  "quicbaas.IAM.App",
			expected: true,
		},
		{
			name:     "session service",
			service:  "quicbaas.IAM.Session",
			expected: true,
		},
		{
			name:     "audit service",
			service:  "quicbaas.IAM.Audit",
			expected: true,
		},
		{
			name:     "admin service",
			service:  "quicbaas.IAM.Admin",
			expected: true,
		},
		{
			name:     "gdpr service",
			service:  "quicbaas.IAM.GDPR",
			expected: true,
		},
		{
			name:     "pep service",
			service:  "quicbaas.IAM.PEP",
			expected: true,
		},
		{
			name:     "mfa service",
			service:  "quicbaas.IAM.MFA",
			expected: true,
		},
		{
			name:     "kv service",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "kms service",
			service:  "quicbaas.KMS",
			expected: false,
		},
		{
			name:     "empty service",
			service:  "",
			expected: false,
		},
		{
			name:     "lowercase iam",
			service:  "quicbaas.iam.User",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIAMService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToMap(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: false,
		},
		{
			name:     "already a map",
			input:    map[string]interface{}{"key": "value"},
			expected: true,
		},
		{
			name: "struct input",
			input: struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
			}{
				Name:  "test",
				Value: 42,
			},
			expected: true,
		},
		{
			name: "pointer to struct",
			input: &struct {
				Name string `json:"name"`
			}{
				Name: "test",
			},
			expected: true,
		},
		{
			name:     "nil pointer",
			input:    (*struct{})(nil),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toMap(tt.input)
			assert.Equal(t, tt.expected, ok)
			if tt.expected {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestToMapStructFields(t *testing.T) {
	type TestStruct struct {
		Name        string            `json:"name"`
		DisplayName string            `json:"display_name"`
		Count       int               `json:"count"`
		Active      bool              `json:"active"`
		Labels      map[string]string `json:"labels,omitempty"`
	}

	input := TestStruct{
		Name:        "test",
		DisplayName: "Test Display",
		Count:       42,
		Active:      true,
		Labels:      map[string]string{"env": "prod"},
	}

	result, ok := toMap(input)
	require.True(t, ok)
	require.NotNil(t, result)

	name, ok := getString(result, "name")
	assert.True(t, ok)
	assert.Equal(t, "test", name)

	displayName, ok := getString(result, "display_name")
	assert.True(t, ok)
	assert.Equal(t, "Test Display", displayName)

	count, ok := getInt(result, "count")
	assert.True(t, ok)
	assert.Equal(t, 42, count)

	active, ok := getBool(result, "active")
	assert.True(t, ok)
	assert.True(t, active)
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"string_val": "hello",
		"int_val":    42,
		"nil_val":    nil,
	}

	tests := []struct {
		name        string
		key         string
		expectedVal string
		expectedOK  bool
	}{
		{
			name:        "existing string",
			key:         "string_val",
			expectedVal: "hello",
			expectedOK:  true,
		},
		{
			name:        "non-string value",
			key:         "int_val",
			expectedVal: "",
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedVal: "",
			expectedOK:  false,
		},
		{
			name:        "nil value",
			key:         "nil_val",
			expectedVal: "",
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getString(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			assert.Equal(t, tt.expectedVal, val)
		})
	}

	// Test nil map
	val, ok := getString(nil, "key")
	assert.False(t, ok)
	assert.Equal(t, "", val)
}

func TestGetInt(t *testing.T) {
	m := map[string]interface{}{
		"int_val":     42,
		"int32_val":   int32(42),
		"int64_val":   int64(42),
		"float64_val": float64(42.0),
		"float32_val": float32(42.0),
		"string_val":  "42",
	}

	tests := []struct {
		name        string
		key         string
		expectedVal int
		expectedOK  bool
	}{
		{
			name:        "int value",
			key:         "int_val",
			expectedVal: 42,
			expectedOK:  true,
		},
		{
			name:        "int32 value",
			key:         "int32_val",
			expectedVal: 42,
			expectedOK:  true,
		},
		{
			name:        "int64 value",
			key:         "int64_val",
			expectedVal: 42,
			expectedOK:  true,
		},
		{
			name:        "float64 value",
			key:         "float64_val",
			expectedVal: 42,
			expectedOK:  true,
		},
		{
			name:        "float32 value",
			key:         "float32_val",
			expectedVal: 42,
			expectedOK:  true,
		},
		{
			name:        "string value",
			key:         "string_val",
			expectedVal: 0,
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedVal: 0,
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getInt(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			assert.Equal(t, tt.expectedVal, val)
		})
	}

	// Test nil map
	val, ok := getInt(nil, "key")
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestGetBool(t *testing.T) {
	m := map[string]interface{}{
		"true_val":   true,
		"false_val":  false,
		"string_val": "true",
	}

	tests := []struct {
		name        string
		key         string
		expectedVal bool
		expectedOK  bool
	}{
		{
			name:        "true value",
			key:         "true_val",
			expectedVal: true,
			expectedOK:  true,
		},
		{
			name:        "false value",
			key:         "false_val",
			expectedVal: false,
			expectedOK:  true,
		},
		{
			name:        "string value",
			key:         "string_val",
			expectedVal: false,
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedVal: false,
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getBool(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			assert.Equal(t, tt.expectedVal, val)
		})
	}

	// Test nil map
	val, ok := getBool(nil, "key")
	assert.False(t, ok)
	assert.False(t, val)
}

func TestGetMap(t *testing.T) {
	m := map[string]interface{}{
		"nested": map[string]interface{}{
			"key": "value",
		},
		"string_val": "test",
	}

	tests := []struct {
		name       string
		key        string
		expectedOK bool
	}{
		{
			name:       "nested map",
			key:        "nested",
			expectedOK: true,
		},
		{
			name:       "non-map value",
			key:        "string_val",
			expectedOK: false,
		},
		{
			name:       "missing key",
			key:        "missing",
			expectedOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getMap(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.NotNil(t, val)
			}
		})
	}

	// Test nil map
	val, ok := getMap(nil, "key")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestGetStringSlice(t *testing.T) {
	m := map[string]interface{}{
		"string_slice": []string{"a", "b", "c"},
		"iface_slice":  []interface{}{"x", "y", "z"},
		"mixed_slice":  []interface{}{"a", 1, "c"},
		"string_val":   "test",
	}

	tests := []struct {
		name        string
		key         string
		expectedLen int
		expectedOK  bool
	}{
		{
			name:        "string slice",
			key:         "string_slice",
			expectedLen: 3,
			expectedOK:  true,
		},
		{
			name:        "interface slice",
			key:         "iface_slice",
			expectedLen: 3,
			expectedOK:  true,
		},
		{
			name:        "mixed slice",
			key:         "mixed_slice",
			expectedLen: 2, // Only string elements
			expectedOK:  true,
		},
		{
			name:        "non-slice value",
			key:         "string_val",
			expectedLen: 0,
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedLen: 0,
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getStringSlice(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Len(t, val, tt.expectedLen)
			}
		})
	}

	// Test nil map
	val, ok := getStringSlice(nil, "key")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestGetStringMap(t *testing.T) {
	m := map[string]interface{}{
		"string_map": map[string]string{
			"a": "1",
			"b": "2",
		},
		"iface_map": map[string]interface{}{
			"x": "1",
			"y": "2",
		},
		"mixed_map": map[string]interface{}{
			"a": "1",
			"b": 2,
		},
		"string_val": "test",
	}

	tests := []struct {
		name        string
		key         string
		expectedLen int
		expectedOK  bool
	}{
		{
			name:        "string map",
			key:         "string_map",
			expectedLen: 2,
			expectedOK:  true,
		},
		{
			name:        "interface map",
			key:         "iface_map",
			expectedLen: 2,
			expectedOK:  true,
		},
		{
			name:        "mixed map",
			key:         "mixed_map",
			expectedLen: 1, // Only string values
			expectedOK:  true,
		},
		{
			name:        "non-map value",
			key:         "string_val",
			expectedLen: 0,
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedLen: 0,
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getStringMap(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Len(t, val, tt.expectedLen)
			}
		})
	}

	// Test nil map
	val, ok := getStringMap(nil, "key")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestGetBytes(t *testing.T) {
	m := map[string]interface{}{
		"bytes_val":  []byte("hello"),
		"string_val": "world",
		"int_val":    42,
	}

	tests := []struct {
		name        string
		key         string
		expectedLen int
		expectedOK  bool
	}{
		{
			name:        "bytes value",
			key:         "bytes_val",
			expectedLen: 5,
			expectedOK:  true,
		},
		{
			name:        "string value converts to bytes",
			key:         "string_val",
			expectedLen: 5,
			expectedOK:  true,
		},
		{
			name:        "int value",
			key:         "int_val",
			expectedLen: 0,
			expectedOK:  false,
		},
		{
			name:        "missing key",
			key:         "missing",
			expectedLen: 0,
			expectedOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := getBytes(m, tt.key)
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Len(t, val, tt.expectedLen)
			}
		})
	}

	// Test nil map
	val, ok := getBytes(nil, "key")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestNewIAMHandler(t *testing.T) {
	// Test that handler can be created without a connection (it will fail on actual calls)
	handler := newIAMHandler(nil)
	assert.NotNil(t, handler)
}

func TestConvertProtoUserToSDK_Nil(t *testing.T) {
	result := convertProtoUserToSDK(nil)
	assert.Nil(t, result)
}

func TestConvertProtoTenantToSDK_Nil(t *testing.T) {
	result := convertProtoTenantToSDK(nil)
	assert.Nil(t, result)
}

func TestConvertProtoRoleToSDK_Nil(t *testing.T) {
	result := convertProtoRoleToSDK(nil)
	assert.Nil(t, result)
}

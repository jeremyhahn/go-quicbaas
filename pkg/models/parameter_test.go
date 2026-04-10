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

package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParameter_GetID(t *testing.T) {
	tests := []struct {
		name     string
		id       uint64
		expected uint64
	}{
		{
			name:     "zero ID",
			id:       0,
			expected: 0,
		},
		{
			name:     "positive ID",
			id:       12345,
			expected: 12345,
		},
		{
			name:     "max uint64",
			id:       ^uint64(0),
			expected: ^uint64(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{ID: tt.id}
			if got := p.GetID(); got != tt.expected {
				t.Errorf("GetID() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestParameter_SetID(t *testing.T) {
	tests := []struct {
		name     string
		id       uint64
		expected uint64
	}{
		{
			name:     "set zero ID",
			id:       0,
			expected: 0,
		},
		{
			name:     "set positive ID",
			id:       67890,
			expected: 67890,
		},
		{
			name:     "set max uint64",
			id:       ^uint64(0),
			expected: ^uint64(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{}
			p.SetID(tt.id)
			if p.ID != tt.expected {
				t.Errorf("SetID() set ID to %d, want %d", p.ID, tt.expected)
			}
		})
	}
}

func TestParameter_GetData(t *testing.T) {
	tests := []struct {
		name     string
		value    []byte
		expected []byte
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: nil,
		},
		{
			name:     "empty value",
			value:    []byte{},
			expected: []byte{},
		},
		{
			name:     "string value",
			value:    []byte("test value"),
			expected: []byte("test value"),
		},
		{
			name:     "binary value",
			value:    []byte{0x00, 0x01, 0x02, 0xFF},
			expected: []byte{0x00, 0x01, 0x02, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{Value: tt.value}
			got := p.GetData()
			if string(got) != string(tt.expected) {
				t.Errorf("GetData() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParameter_SetData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []byte
	}{
		{
			name:     "set nil data",
			data:     nil,
			expected: nil,
		},
		{
			name:     "set empty data",
			data:     []byte{},
			expected: []byte{},
		},
		{
			name:     "set string data",
			data:     []byte("new test value"),
			expected: []byte("new test value"),
		},
		{
			name:     "set binary data",
			data:     []byte{0xDE, 0xAD, 0xBE, 0xEF},
			expected: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{}
			p.SetData(tt.data)
			if string(p.Value) != string(tt.expected) {
				t.Errorf("SetData() set Value to %v, want %v", p.Value, tt.expected)
			}
		})
	}
}

func TestParameter_IsExpired(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name      string
		expiresAt *time.Time
		expected  bool
	}{
		{
			name:      "nil expiration - never expires",
			expiresAt: nil,
			expected:  false,
		},
		{
			name:      "past expiration - expired",
			expiresAt: &past,
			expected:  true,
		},
		{
			name:      "future expiration - not expired",
			expiresAt: &future,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{ExpiresAt: tt.expiresAt}
			if got := p.IsExpired(); got != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParameter_IsExpired_EdgeCases(t *testing.T) {
	t.Run("just expired", func(t *testing.T) {
		expired := time.Now().Add(-1 * time.Millisecond)
		p := &Parameter{ExpiresAt: &expired}
		if !p.IsExpired() {
			t.Error("IsExpired() should return true for just-expired parameter")
		}
	})

	t.Run("about to expire", func(t *testing.T) {
		notExpired := time.Now().Add(1 * time.Second)
		p := &Parameter{ExpiresAt: &notExpired}
		if p.IsExpired() {
			t.Error("IsExpired() should return false for not-yet-expired parameter")
		}
	})
}

func TestParameter_GetTTLDuration(t *testing.T) {
	tests := []struct {
		name     string
		ttl      int64
		expected time.Duration
	}{
		{
			name:     "zero TTL",
			ttl:      0,
			expected: 0,
		},
		{
			name:     "one second TTL",
			ttl:      1,
			expected: 1 * time.Second,
		},
		{
			name:     "one minute TTL",
			ttl:      60,
			expected: 60 * time.Second,
		},
		{
			name:     "one hour TTL",
			ttl:      3600,
			expected: 3600 * time.Second,
		},
		{
			name:     "one day TTL",
			ttl:      86400,
			expected: 86400 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{TTL: tt.ttl}
			if got := p.GetTTLDuration(); got != tt.expected {
				t.Errorf("GetTTLDuration() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParameter_SetTTLDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected int64
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: 0,
		},
		{
			name:     "one second",
			duration: 1 * time.Second,
			expected: 1,
		},
		{
			name:     "one minute",
			duration: 1 * time.Minute,
			expected: 60,
		},
		{
			name:     "one hour",
			duration: 1 * time.Hour,
			expected: 3600,
		},
		{
			name:     "one day",
			duration: 24 * time.Hour,
			expected: 86400,
		},
		{
			name:     "subsecond duration rounds down",
			duration: 500 * time.Millisecond,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{}
			p.SetTTLDuration(tt.duration)
			if p.TTL != tt.expected {
				t.Errorf("SetTTLDuration() set TTL to %d, want %d", p.TTL, tt.expected)
			}
		})
	}
}

func TestParameter_MarshalBinary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(1 * time.Hour)

	tests := []struct {
		name  string
		param *Parameter
	}{
		{
			name: "full parameter",
			param: &Parameter{
				ID:           123,
				Path:         "/app/database/host",
				Value:        []byte("localhost:5432"),
				Type:         ParameterTypeString,
				KMSKeyID:     "kms-key-123",
				IsEncrypted:  true,
				Tags:         map[string]string{"env": "prod", "team": "backend"},
				TTL:          3600,
				ExpiresAt:    &expires,
				TenantID:     "tenant-abc",
				AuditEnabled: true,
				Description:  "Database host configuration",
				CreatedAt:    now,
				UpdatedAt:    now,
				CreatedBy:    "user1",
				UpdatedBy:    "user2",
			},
		},
		{
			name: "minimal parameter",
			param: &Parameter{
				ID:   1,
				Path: "/simple",
			},
		},
		{
			name:  "empty parameter",
			param: &Parameter{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.param.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary() error = %v", err)
			}

			// Verify it's valid JSON
			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("MarshalBinary() produced invalid JSON: %v", err)
			}
		})
	}
}

func TestParameter_UnmarshalBinary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(1 * time.Hour)

	tests := []struct {
		name     string
		original *Parameter
	}{
		{
			name: "full parameter",
			original: &Parameter{
				ID:           456,
				Path:         "/app/config/setting",
				Value:        []byte("value123"),
				Type:         ParameterTypeNumber,
				KMSKeyID:     "kms-456",
				IsEncrypted:  false,
				Tags:         map[string]string{"key": "value"},
				TTL:          7200,
				ExpiresAt:    &expires,
				TenantID:     "tenant-xyz",
				AuditEnabled: false,
				Description:  "Some config",
				CreatedAt:    now,
				UpdatedAt:    now,
				CreatedBy:    "admin",
				UpdatedBy:    "admin",
			},
		},
		{
			name: "minimal parameter",
			original: &Parameter{
				ID:   2,
				Path: "/test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.original)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			p := &Parameter{}
			if err := p.UnmarshalBinary(data); err != nil {
				t.Fatalf("UnmarshalBinary() error = %v", err)
			}

			if p.ID != tt.original.ID {
				t.Errorf("ID = %d, want %d", p.ID, tt.original.ID)
			}
			if p.Path != tt.original.Path {
				t.Errorf("Path = %s, want %s", p.Path, tt.original.Path)
			}
		})
	}
}

func TestParameter_UnmarshalBinary_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "invalid JSON",
			data: []byte("invalid json"),
		},
		{
			name: "truncated JSON",
			data: []byte(`{"id": 1, "path": "/test`),
		},
		{
			name: "wrong type",
			data: []byte(`"just a string"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{}
			err := p.UnmarshalBinary(tt.data)
			if err == nil {
				t.Error("UnmarshalBinary() with invalid data should return error")
			}
		})
	}
}

func TestParameter_MarshalUnmarshal_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(2 * time.Hour)

	original := &Parameter{
		ID:           789,
		Path:         "/app/secrets/api-key",
		Value:        []byte("secret-value-123"),
		Type:         ParameterTypeBinary,
		KMSKeyID:     "kms-key-789",
		IsEncrypted:  true,
		Tags:         map[string]string{"env": "staging", "app": "myapp"},
		TTL:          3600,
		ExpiresAt:    &expires,
		TenantID:     "tenant-123",
		AuditEnabled: true,
		Description:  "API key for external service",
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    "deployer",
		UpdatedBy:    "deployer",
	}

	// Marshal
	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	// Unmarshal
	decoded := &Parameter{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}

	// Verify all fields match
	if decoded.ID != original.ID {
		t.Errorf("ID = %d, want %d", decoded.ID, original.ID)
	}
	if decoded.Path != original.Path {
		t.Errorf("Path = %s, want %s", decoded.Path, original.Path)
	}
	if string(decoded.Value) != string(original.Value) {
		t.Errorf("Value = %s, want %s", string(decoded.Value), string(original.Value))
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %s, want %s", decoded.Type, original.Type)
	}
	if decoded.KMSKeyID != original.KMSKeyID {
		t.Errorf("KMSKeyID = %s, want %s", decoded.KMSKeyID, original.KMSKeyID)
	}
	if decoded.IsEncrypted != original.IsEncrypted {
		t.Errorf("IsEncrypted = %v, want %v", decoded.IsEncrypted, original.IsEncrypted)
	}
	if len(decoded.Tags) != len(original.Tags) {
		t.Errorf("Tags length = %d, want %d", len(decoded.Tags), len(original.Tags))
	}
	for k, v := range original.Tags {
		if decoded.Tags[k] != v {
			t.Errorf("Tags[%s] = %s, want %s", k, decoded.Tags[k], v)
		}
	}
	if decoded.TTL != original.TTL {
		t.Errorf("TTL = %d, want %d", decoded.TTL, original.TTL)
	}
	if decoded.ExpiresAt == nil || original.ExpiresAt == nil {
		if decoded.ExpiresAt != original.ExpiresAt {
			t.Errorf("ExpiresAt nil mismatch")
		}
	} else if !decoded.ExpiresAt.Equal(*original.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", decoded.ExpiresAt, original.ExpiresAt)
	}
	if decoded.TenantID != original.TenantID {
		t.Errorf("TenantID = %s, want %s", decoded.TenantID, original.TenantID)
	}
	if decoded.AuditEnabled != original.AuditEnabled {
		t.Errorf("AuditEnabled = %v, want %v", decoded.AuditEnabled, original.AuditEnabled)
	}
	if decoded.Description != original.Description {
		t.Errorf("Description = %s, want %s", decoded.Description, original.Description)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
	if decoded.CreatedBy != original.CreatedBy {
		t.Errorf("CreatedBy = %s, want %s", decoded.CreatedBy, original.CreatedBy)
	}
	if decoded.UpdatedBy != original.UpdatedBy {
		t.Errorf("UpdatedBy = %s, want %s", decoded.UpdatedBy, original.UpdatedBy)
	}
}

func TestParameter_Clone(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(1 * time.Hour)

	original := &Parameter{
		ID:           100,
		Path:         "/app/clone/test",
		Value:        []byte("original value"),
		Type:         ParameterTypeJSON,
		KMSKeyID:     "kms-clone",
		IsEncrypted:  true,
		Tags:         map[string]string{"tag1": "value1", "tag2": "value2"},
		TTL:          1800,
		ExpiresAt:    &expires,
		TenantID:     "tenant-clone",
		AuditEnabled: true,
		Description:  "Clone test parameter",
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    "cloner",
		UpdatedBy:    "cloner",
	}

	clone := original.Clone()

	// Verify clone has same values
	if clone.ID != original.ID {
		t.Errorf("Clone ID = %d, want %d", clone.ID, original.ID)
	}
	if clone.Path != original.Path {
		t.Errorf("Clone Path = %s, want %s", clone.Path, original.Path)
	}
	if string(clone.Value) != string(original.Value) {
		t.Errorf("Clone Value = %s, want %s", string(clone.Value), string(original.Value))
	}
	if clone.Type != original.Type {
		t.Errorf("Clone Type = %s, want %s", clone.Type, original.Type)
	}

	// Verify deep copy of Value - modifying clone should not affect original
	clone.Value[0] = 'X'
	if original.Value[0] == 'X' {
		t.Error("Clone Value is not a deep copy - modifying clone affected original")
	}

	// Verify deep copy of Tags - modifying clone should not affect original
	clone.Tags["tag1"] = "modified"
	if original.Tags["tag1"] == "modified" {
		t.Error("Clone Tags is not a deep copy - modifying clone affected original")
	}

	// Verify deep copy of ExpiresAt - modifying clone should not affect original
	newExpires := expires.Add(1 * time.Hour)
	clone.ExpiresAt = &newExpires
	if !original.ExpiresAt.Equal(expires) {
		t.Error("Clone ExpiresAt is not a deep copy - modifying clone affected original")
	}
}

func TestParameter_Clone_NilFields(t *testing.T) {
	t.Run("nil Value", func(t *testing.T) {
		original := &Parameter{
			ID:    1,
			Path:  "/nil/value",
			Value: nil,
		}
		clone := original.Clone()
		if clone.Value != nil {
			t.Errorf("Clone Value = %v, want nil", clone.Value)
		}
	})

	t.Run("nil Tags", func(t *testing.T) {
		original := &Parameter{
			ID:   2,
			Path: "/nil/tags",
			Tags: nil,
		}
		clone := original.Clone()
		if clone.Tags != nil {
			t.Errorf("Clone Tags = %v, want nil", clone.Tags)
		}
	})

	t.Run("nil ExpiresAt", func(t *testing.T) {
		original := &Parameter{
			ID:        3,
			Path:      "/nil/expires",
			ExpiresAt: nil,
		}
		clone := original.Clone()
		if clone.ExpiresAt != nil {
			t.Errorf("Clone ExpiresAt = %v, want nil", clone.ExpiresAt)
		}
	})

	t.Run("all nil fields", func(t *testing.T) {
		original := &Parameter{
			ID:        4,
			Path:      "/all/nil",
			Value:     nil,
			Tags:      nil,
			ExpiresAt: nil,
		}
		clone := original.Clone()
		if clone.Value != nil || clone.Tags != nil || clone.ExpiresAt != nil {
			t.Error("Clone should have nil fields when original has nil fields")
		}
	})
}

func TestParameter_Clone_EmptyFields(t *testing.T) {
	t.Run("empty Value", func(t *testing.T) {
		original := &Parameter{
			ID:    1,
			Path:  "/empty/value",
			Value: []byte{},
		}
		clone := original.Clone()
		if clone.Value == nil || len(clone.Value) != 0 {
			t.Errorf("Clone Value = %v, want empty slice", clone.Value)
		}
	})

	t.Run("empty Tags", func(t *testing.T) {
		original := &Parameter{
			ID:   2,
			Path: "/empty/tags",
			Tags: map[string]string{},
		}
		clone := original.Clone()
		if clone.Tags == nil || len(clone.Tags) != 0 {
			t.Errorf("Clone Tags = %v, want empty map", clone.Tags)
		}
	})
}

func TestParameter_Validate(t *testing.T) {
	tests := []struct {
		name      string
		param     *Parameter
		wantErr   bool
		errTarget error
	}{
		{
			name: "valid parameter",
			param: &Parameter{
				Path:  "/valid/path",
				Value: []byte("value"),
			},
			wantErr: false,
		},
		{
			name: "empty path",
			param: &Parameter{
				Path:  "",
				Value: []byte("value"),
			},
			wantErr:   true,
			errTarget: ErrParameterPathEmpty,
		},
		{
			name: "path too long",
			param: &Parameter{
				Path:  "/" + strings.Repeat("a", MaxParameterPathLength),
				Value: []byte("value"),
			},
			wantErr:   true,
			errTarget: ErrParameterPathTooLong,
		},
		{
			name: "value too large",
			param: &Parameter{
				Path:  "/valid/path",
				Value: make([]byte, MaxParameterValueSize+1),
			},
			wantErr:   true,
			errTarget: ErrParameterValueTooLarge,
		},
		{
			name: "path at max length - valid",
			param: &Parameter{
				Path:  "/" + strings.Repeat("a", MaxParameterPathLength-1),
				Value: []byte("value"),
			},
			wantErr: false,
		},
		{
			name: "value at max size - valid",
			param: &Parameter{
				Path:  "/valid/path",
				Value: make([]byte, MaxParameterValueSize),
			},
			wantErr: false,
		},
		{
			name: "nil value - valid",
			param: &Parameter{
				Path:  "/valid/path",
				Value: nil,
			},
			wantErr: false,
		},
		{
			name: "empty value - valid",
			param: &Parameter{
				Path:  "/valid/path",
				Value: []byte{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.param.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() error = nil, want error")
					return
				}
				if tt.errTarget != nil && err != tt.errTarget {
					t.Errorf("Validate() error = %v, want %v", err, tt.errTarget)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			}
		})
	}
}

func TestParameter_Validate_PathEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "root path",
			path:    "/",
			wantErr: false,
		},
		{
			name:    "deep nested path",
			path:    "/a/b/c/d/e/f/g/h/i/j",
			wantErr: false,
		},
		{
			name:    "path with special characters",
			path:    "/app/config-v2/my_setting.json",
			wantErr: false,
		},
		{
			name:    "path with spaces",
			path:    "/app/my setting",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parameter{Path: tt.path}
			err := p.Validate()
			if tt.wantErr && err == nil {
				t.Error("Validate() should return error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestParameterValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "path empty error",
			err:      ErrParameterPathEmpty,
			expected: "parameter path cannot be empty",
		},
		{
			name:     "path too long error",
			err:      ErrParameterPathTooLong,
			expected: "parameter path exceeds maximum length",
		},
		{
			name:     "value too large error",
			err:      ErrParameterValueTooLarge,
			expected: "parameter value exceeds maximum size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestParameterType_Values(t *testing.T) {
	tests := []struct {
		name     string
		pType    ParameterType
		expected string
	}{
		{
			name:     "string type",
			pType:    ParameterTypeString,
			expected: "string",
		},
		{
			name:     "number type",
			pType:    ParameterTypeNumber,
			expected: "number",
		},
		{
			name:     "json type",
			pType:    ParameterTypeJSON,
			expected: "json",
		},
		{
			name:     "binary type",
			pType:    ParameterTypeBinary,
			expected: "binary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.pType) != tt.expected {
				t.Errorf("ParameterType = %s, want %s", tt.pType, tt.expected)
			}
		})
	}
}

func TestParameter_EmptyParameter(t *testing.T) {
	p := &Parameter{}

	// Test GetID on empty parameter
	if id := p.GetID(); id != 0 {
		t.Errorf("Empty parameter GetID() = %d, want 0", id)
	}

	// Test GetData on empty parameter
	if data := p.GetData(); data != nil {
		t.Errorf("Empty parameter GetData() = %v, want nil", data)
	}

	// Test IsExpired on empty parameter (nil ExpiresAt)
	if p.IsExpired() {
		t.Error("Empty parameter IsExpired() should return false")
	}

	// Test GetTTLDuration on empty parameter
	if ttl := p.GetTTLDuration(); ttl != 0 {
		t.Errorf("Empty parameter GetTTLDuration() = %v, want 0", ttl)
	}

	// Test Validate on empty parameter (empty path should fail)
	if err := p.Validate(); err == nil {
		t.Error("Empty parameter Validate() should return error for empty path")
	}

	// Test Clone on empty parameter
	clone := p.Clone()
	if clone == nil {
		t.Error("Empty parameter Clone() returned nil")
	}

	// Test MarshalBinary on empty parameter
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatalf("Empty parameter MarshalBinary() error = %v", err)
	}

	// Verify it unmarshals correctly
	decoded := &Parameter{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("Empty parameter UnmarshalBinary() error = %v", err)
	}
}

func TestParameter_TTLDuration_RoundTrip(t *testing.T) {
	durations := []time.Duration{
		0,
		1 * time.Second,
		30 * time.Second,
		5 * time.Minute,
		1 * time.Hour,
		24 * time.Hour,
		7 * 24 * time.Hour,
	}

	for _, d := range durations {
		t.Run(d.String(), func(t *testing.T) {
			p := &Parameter{}
			p.SetTTLDuration(d)
			got := p.GetTTLDuration()

			// Note: subsecond precision is lost
			expected := time.Duration(int64(d.Seconds())) * time.Second
			if got != expected {
				t.Errorf("TTL round trip = %v, want %v", got, expected)
			}
		})
	}
}

func TestParameter_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	p := &Parameter{
		ID:        999,
		Path:      "/json/test",
		Value:     []byte(`{"nested": "json"}`),
		Type:      ParameterTypeJSON,
		Tags:      map[string]string{"format": "json"},
		TenantID:  "tenant-json",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Marshal to JSON
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify JSON structure
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Check required fields are present
	requiredFields := []string{"id", "path", "value", "type", "tenant_id", "created_at", "updated_at"}
	for _, field := range requiredFields {
		if _, ok := jsonMap[field]; !ok {
			t.Errorf("JSON missing required field: %s", field)
		}
	}

	// Unmarshal back
	decoded := &Parameter{}
	if err := json.Unmarshal(data, decoded); err != nil {
		t.Fatalf("json.Unmarshal() to Parameter error = %v", err)
	}

	if decoded.ID != p.ID {
		t.Errorf("Decoded ID = %d, want %d", decoded.ID, p.ID)
	}
	if decoded.Path != p.Path {
		t.Errorf("Decoded Path = %s, want %s", decoded.Path, p.Path)
	}
}

func TestParameter_Clone_Independence(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(1 * time.Hour)

	original := &Parameter{
		ID:           1,
		Path:         "/original",
		Value:        []byte("original"),
		Tags:         map[string]string{"key": "original"},
		ExpiresAt:    &expires,
		TenantID:     "original-tenant",
		AuditEnabled: false,
	}

	clone := original.Clone()

	// Modify all mutable fields in clone
	clone.ID = 999
	clone.Path = "/modified"
	clone.Value[0] = 'M'
	clone.Tags["key"] = "modified"
	clone.Tags["new"] = "tag"
	newExpires := expires.Add(24 * time.Hour)
	clone.ExpiresAt = &newExpires
	clone.TenantID = "modified-tenant"
	clone.AuditEnabled = true

	// Verify original is unchanged
	if original.ID != 1 {
		t.Errorf("Original ID was modified: %d", original.ID)
	}
	if original.Path != "/original" {
		t.Errorf("Original Path was modified: %s", original.Path)
	}
	if original.Value[0] != 'o' {
		t.Errorf("Original Value was modified: %s", string(original.Value))
	}
	if original.Tags["key"] != "original" {
		t.Errorf("Original Tags were modified: %v", original.Tags)
	}
	if _, ok := original.Tags["new"]; ok {
		t.Error("Original Tags has unexpected key 'new'")
	}
	if !original.ExpiresAt.Equal(expires) {
		t.Errorf("Original ExpiresAt was modified: %v", original.ExpiresAt)
	}
	if original.TenantID != "original-tenant" {
		t.Errorf("Original TenantID was modified: %s", original.TenantID)
	}
	if original.AuditEnabled != false {
		t.Errorf("Original AuditEnabled was modified: %v", original.AuditEnabled)
	}
}

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

// Package models provides data models for QuicBaaS entities.
package models

import (
	"encoding/json"
	"time"
)

// ParameterType defines the value type of a parameter.
type ParameterType string

const (
	// ParameterTypeString represents a string parameter value.
	ParameterTypeString ParameterType = "string"

	// ParameterTypeNumber represents a numeric parameter value.
	ParameterTypeNumber ParameterType = "number"

	// ParameterTypeJSON represents a JSON parameter value.
	ParameterTypeJSON ParameterType = "json"

	// ParameterTypeBinary represents a binary parameter value.
	ParameterTypeBinary ParameterType = "binary"
)

// Parameter represents a hierarchical configuration parameter.
// Parameters are tenant-scoped and support encryption, TTL, and tagging.
type Parameter struct {
	// ID is the unique internal identifier (auto-generated).
	ID uint64 `json:"id"`

	// Path is the hierarchical path (e.g., "/app/database/host").
	// Paths are slash-separated and must start with '/'.
	Path string `json:"path"`

	// Value is the parameter value (plaintext or encrypted).
	// For encrypted parameters, this contains the ciphertext.
	Value []byte `json:"value"`

	// Type specifies the parameter value type.
	Type ParameterType `json:"type"`

	// KMSKeyID is the optional KMS key for envelope encryption.
	// When set, the Value field contains encrypted data.
	KMSKeyID string `json:"kms_key_id,omitempty"`

	// IsEncrypted indicates if the value is encrypted.
	IsEncrypted bool `json:"is_encrypted"`

	// Tags are key-value labels for organizing and filtering.
	Tags map[string]string `json:"tags,omitempty"`

	// TTL is the time-to-live duration in seconds. Zero means no expiration.
	TTL int64 `json:"ttl,omitempty"`

	// ExpiresAt is the calculated expiration timestamp.
	// Computed as CreatedAt + TTL when TTL > 0.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// TenantID is the owning tenant (extracted from context).
	TenantID string `json:"tenant_id"`

	// AuditEnabled enables audit logging for this parameter.
	// Default is false; can be enabled at tenant or parameter level.
	AuditEnabled bool `json:"audit_enabled"`

	// Description is an optional description of the parameter.
	Description string `json:"description,omitempty"`

	// CreatedAt is when the parameter was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the parameter was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// CreatedBy is the user ID who created the parameter.
	CreatedBy string `json:"created_by,omitempty"`

	// UpdatedBy is the user ID who last modified the parameter.
	UpdatedBy string `json:"updated_by,omitempty"`
}

// GetID implements the Entity interface for DAO support.
func (p *Parameter) GetID() uint64 {
	return p.ID
}

// SetID implements the Entity interface for DAO support.
func (p *Parameter) SetID(id uint64) {
	p.ID = id
}

// GetData returns the parameter's value as the data payload.
func (p *Parameter) GetData() []byte {
	return p.Value
}

// SetData sets the parameter's value.
func (p *Parameter) SetData(data []byte) {
	p.Value = data
}

// IsExpired returns true if the parameter has expired.
func (p *Parameter) IsExpired() bool {
	if p.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*p.ExpiresAt)
}

// GetTTLDuration returns the TTL as a time.Duration.
func (p *Parameter) GetTTLDuration() time.Duration {
	return time.Duration(p.TTL) * time.Second
}

// SetTTLDuration sets the TTL from a time.Duration.
func (p *Parameter) SetTTLDuration(ttl time.Duration) {
	p.TTL = int64(ttl.Seconds())
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (p *Parameter) MarshalBinary() ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (p *Parameter) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, p)
}

// Clone creates a deep copy of the parameter.
func (p *Parameter) Clone() *Parameter {
	clone := *p

	// Deep copy Value
	if p.Value != nil {
		clone.Value = make([]byte, len(p.Value))
		copy(clone.Value, p.Value)
	}

	// Deep copy Tags
	if p.Tags != nil {
		clone.Tags = make(map[string]string, len(p.Tags))
		for k, v := range p.Tags {
			clone.Tags[k] = v
		}
	}

	// Deep copy ExpiresAt
	if p.ExpiresAt != nil {
		expiresCopy := *p.ExpiresAt
		clone.ExpiresAt = &expiresCopy
	}

	return &clone
}

// Validate performs basic validation on the parameter.
func (p *Parameter) Validate() error {
	if p.Path == "" {
		return ErrParameterPathEmpty
	}
	if len(p.Path) > MaxParameterPathLength {
		return ErrParameterPathTooLong
	}
	if len(p.Value) > MaxParameterValueSize {
		return ErrParameterValueTooLarge
	}
	return nil
}

// Parameter validation errors.
var (
	ErrParameterPathEmpty     = &parameterValidationError{message: "parameter path cannot be empty"}
	ErrParameterPathTooLong   = &parameterValidationError{message: "parameter path exceeds maximum length"}
	ErrParameterValueTooLarge = &parameterValidationError{message: "parameter value exceeds maximum size"}
)

// Parameter limits.
const (
	// MaxParameterPathLength is the maximum allowed path length.
	MaxParameterPathLength = 2048

	// MaxParameterValueSize is the maximum allowed value size (64KB).
	MaxParameterValueSize = 65536

	// MaxParameterTagKeyLength is the maximum tag key length.
	MaxParameterTagKeyLength = 128

	// MaxParameterTagValueLength is the maximum tag value length.
	MaxParameterTagValueLength = 256

	// MaxParameterTags is the maximum number of tags per parameter.
	MaxParameterTags = 50
)

// parameterValidationError is a simple validation error type.
type parameterValidationError struct {
	message string
}

func (e *parameterValidationError) Error() string {
	return e.message
}

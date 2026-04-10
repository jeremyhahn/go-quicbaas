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

// Entity represents a generic key-value entity stored in QuicBaaS.
// This is the core data structure used for all database operations.
type Entity struct {
	// ID is the unique identifier for this entity
	ID uint64 `json:"id"`

	// Data is the JSON-encoded entity data
	Data []byte `json:"data"`

	// Type is an optional entity type identifier
	Type string `json:"type,omitempty"`

	// CreatedAt is the timestamp when this entity was created
	CreatedAt time.Time `json:"created_at,omitempty"`

	// UpdatedAt is the timestamp when this entity was last updated
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// GetID implements statemachine.KeyValueEntity.
func (e *Entity) GetID() uint64 {
	return e.ID
}

// SetID implements statemachine.KeyValueEntity.
func (e *Entity) SetID(id uint64) {
	e.ID = id
}

// GetData returns the entity's data payload.
func (e *Entity) GetData() []byte {
	return e.Data
}

// SetData sets the entity's data payload.
func (e *Entity) SetData(data []byte) {
	e.Data = data
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (e *Entity) MarshalBinary() ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (e *Entity) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, e)
}

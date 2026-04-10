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
	"testing"
	"time"
)

func TestEntity_GetID(t *testing.T) {
	e := &Entity{ID: 12345}
	if got := e.GetID(); got != 12345 {
		t.Errorf("GetID() = %d, want %d", got, 12345)
	}
}

func TestEntity_SetID(t *testing.T) {
	e := &Entity{}
	e.SetID(67890)
	if e.ID != 67890 {
		t.Errorf("SetID() set ID to %d, want %d", e.ID, 67890)
	}
}

func TestEntity_GetData(t *testing.T) {
	data := []byte("test data")
	e := &Entity{Data: data}
	got := e.GetData()
	if string(got) != string(data) {
		t.Errorf("GetData() = %s, want %s", string(got), string(data))
	}
}

func TestEntity_SetData(t *testing.T) {
	e := &Entity{}
	data := []byte("new test data")
	e.SetData(data)
	if string(e.Data) != string(data) {
		t.Errorf("SetData() set Data to %s, want %s", string(e.Data), string(data))
	}
}

func TestEntity_MarshalBinary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	e := &Entity{
		ID:        123,
		Data:      []byte("test data"),
		Type:      "test-type",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := e.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	// Verify it's valid JSON
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("MarshalBinary() produced invalid JSON: %v", err)
	}

	// Verify fields
	if int(decoded["id"].(float64)) != 123 {
		t.Errorf("MarshalBinary() id = %v, want 123", decoded["id"])
	}
	if decoded["type"] != "test-type" {
		t.Errorf("MarshalBinary() type = %v, want test-type", decoded["type"])
	}
}

func TestEntity_UnmarshalBinary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := &Entity{
		ID:        456,
		Data:      []byte("unmarshal test"),
		Type:      "unmarshal-type",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	e := &Entity{}
	if err := e.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}

	if e.ID != original.ID {
		t.Errorf("UnmarshalBinary() ID = %d, want %d", e.ID, original.ID)
	}
	if string(e.Data) != string(original.Data) {
		t.Errorf("UnmarshalBinary() Data = %s, want %s", string(e.Data), string(original.Data))
	}
	if e.Type != original.Type {
		t.Errorf("UnmarshalBinary() Type = %s, want %s", e.Type, original.Type)
	}
}

func TestEntity_UnmarshalBinary_InvalidJSON(t *testing.T) {
	e := &Entity{}
	err := e.UnmarshalBinary([]byte("invalid json"))
	if err == nil {
		t.Error("UnmarshalBinary() with invalid JSON should return error")
	}
}

func TestEntity_MarshalUnmarshal_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := &Entity{
		ID:        789,
		Data:      []byte("round trip test"),
		Type:      "round-trip-type",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Marshal
	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	// Unmarshal
	decoded := &Entity{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}

	// Verify all fields match
	if decoded.ID != original.ID {
		t.Errorf("Round trip ID = %d, want %d", decoded.ID, original.ID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Round trip Data = %s, want %s", string(decoded.Data), string(original.Data))
	}
	if decoded.Type != original.Type {
		t.Errorf("Round trip Type = %s, want %s", decoded.Type, original.Type)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("Round trip CreatedAt = %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("Round trip UpdatedAt = %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
}

func TestEntity_EmptyEntity(t *testing.T) {
	e := &Entity{}

	// Test GetID on empty entity
	if id := e.GetID(); id != 0 {
		t.Errorf("Empty entity GetID() = %d, want 0", id)
	}

	// Test GetData on empty entity
	if data := e.GetData(); data != nil {
		t.Errorf("Empty entity GetData() = %v, want nil", data)
	}

	// Test Marshal on empty entity
	data, err := e.MarshalBinary()
	if err != nil {
		t.Fatalf("Empty entity MarshalBinary() error = %v", err)
	}

	// Verify it unmarshals correctly
	decoded := &Entity{}
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("Empty entity UnmarshalBinary() error = %v", err)
	}
}

func TestEntity_TypeField(t *testing.T) {
	tests := []struct {
		name     string
		typeVal  string
		wantType string
	}{
		{"WithType", "document", "document"},
		{"EmptyType", "", ""},
		{"LongType", "very-long-type-name-with-dashes", "very-long-type-name-with-dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Entity{
				ID:   1,
				Type: tt.typeVal,
			}

			data, err := e.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary() error = %v", err)
			}

			decoded := &Entity{}
			if err := decoded.UnmarshalBinary(data); err != nil {
				t.Fatalf("UnmarshalBinary() error = %v", err)
			}

			if decoded.Type != tt.wantType {
				t.Errorf("Type = %s, want %s", decoded.Type, tt.wantType)
			}
		})
	}
}

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

//go:build vector

package service

import (
	"testing"
)

// TestWithVectorShardID tests the WithVectorShardID option.
func TestWithVectorShardID(t *testing.T) {
	tests := []struct {
		name    string
		shardID uint64
	}{
		{
			name:    "default shard ID",
			shardID: 2,
		},
		{
			name:    "custom shard ID",
			shardID: 5,
		},
		{
			name:    "zero shard ID",
			shardID: 0,
		},
		{
			name:    "large shard ID",
			shardID: 999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := &vectorService{}
			opt := WithVectorShardID(tt.shardID)
			opt(vs)

			if vs.shardID != tt.shardID {
				t.Errorf("expected shardID %d, got %d", tt.shardID, vs.shardID)
			}
		})
	}
}

// TestVectorServiceOptionChaining tests applying multiple options.
func TestVectorServiceOptionChaining(t *testing.T) {
	vs := &vectorService{}

	opts := []VectorServiceOption{
		WithVectorShardID(10),
	}

	for _, opt := range opts {
		opt(vs)
	}

	if vs.shardID != 10 {
		t.Errorf("expected shardID 10, got %d", vs.shardID)
	}
}

// TestVectorServiceOptionOverwriting tests that later options overwrite earlier ones.
func TestVectorServiceOptionOverwriting(t *testing.T) {
	vs := &vectorService{}

	// Apply first shardID
	WithVectorShardID(5)(vs)
	if vs.shardID != 5 {
		t.Errorf("expected shardID 5, got %d", vs.shardID)
	}

	// Apply second shardID (should overwrite)
	WithVectorShardID(10)(vs)
	if vs.shardID != 10 {
		t.Errorf("expected shardID 10 after overwrite, got %d", vs.shardID)
	}
}

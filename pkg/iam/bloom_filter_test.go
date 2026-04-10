//go:build iam

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

package iam

import (
	"testing"
)

func TestBloomFilter_Clear(t *testing.T) {
	// Create a bloom filter and add items
	bf := NewBloomFilter(1024, 3)

	// Add some items
	bf.Add([]byte("item1"))
	bf.Add([]byte("item2"))
	bf.Add([]byte("item3"))

	// Verify items are present
	if !bf.Contains([]byte("item1")) {
		t.Error("expected item1 to be present before clear")
	}
	if !bf.Contains([]byte("item2")) {
		t.Error("expected item2 to be present before clear")
	}

	// Clear the filter
	bf.Clear()

	// Verify items are no longer present
	if bf.Contains([]byte("item1")) {
		t.Error("expected item1 to be absent after clear")
	}
	if bf.Contains([]byte("item2")) {
		t.Error("expected item2 to be absent after clear")
	}
	if bf.Contains([]byte("item3")) {
		t.Error("expected item3 to be absent after clear")
	}
}

func TestBloomFilter_ClearEmpty(t *testing.T) {
	// Clear an empty filter should not panic
	bf := NewBloomFilter(1024, 3)
	bf.Clear()

	// Should still work after clearing empty filter
	bf.Add([]byte("test"))
	if !bf.Contains([]byte("test")) {
		t.Error("expected test to be present after adding to cleared filter")
	}
}

func TestBloomFilter_ClearAndReuse(t *testing.T) {
	bf := NewBloomFilter(1024, 3)

	// First use
	bf.Add([]byte("first"))
	if !bf.Contains([]byte("first")) {
		t.Error("expected first to be present")
	}

	// Clear
	bf.Clear()

	// Second use - should work as fresh filter
	bf.Add([]byte("second"))
	if !bf.Contains([]byte("second")) {
		t.Error("expected second to be present")
	}
	if bf.Contains([]byte("first")) {
		t.Error("expected first to be absent after clear")
	}
}

func TestBloomFilter_Contains_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		size     uint64
		k        uint32
		items    [][]byte
		check    []byte
		expected bool
	}{
		{
			name:     "small filter",
			size:     64,
			k:        2,
			items:    [][]byte{[]byte("a")},
			check:    []byte("a"),
			expected: true,
		},
		{
			name:     "empty data",
			size:     1024,
			k:        3,
			items:    [][]byte{[]byte("")},
			check:    []byte(""),
			expected: true,
		},
		{
			name:     "item not added",
			size:     1024,
			k:        3,
			items:    [][]byte{[]byte("added")},
			check:    []byte("not-added"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bf := NewBloomFilter(tt.size, tt.k)
			for _, item := range tt.items {
				bf.Add(item)
			}
			got := bf.Contains(tt.check)
			if got != tt.expected {
				t.Errorf("Contains(%q) = %v, want %v", tt.check, got, tt.expected)
			}
		})
	}
}

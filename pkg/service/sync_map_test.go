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

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncMap_LoadStoreDelete(t *testing.T) {
	var m syncMap[string, int]

	_, ok := m.Load("missing")
	assert.False(t, ok, "a missing key must not be reported as present")

	m.Store("a", 1)
	got, ok := m.Load("a")
	require.True(t, ok)
	assert.Equal(t, 1, got)

	m.Delete("a")
	_, ok = m.Load("a")
	assert.False(t, ok, "a deleted key must not be reported as present")
}

func TestSyncMap_LoadOrStore(t *testing.T) {
	var m syncMap[string, *AEADUsage]
	first := &AEADUsage{}
	second := &AEADUsage{}

	assert.Same(t, first, m.LoadOrStore("k", first), "an absent key stores and returns the new value")
	assert.Same(t, first, m.LoadOrStore("k", second), "a present key returns the existing value")
}

func TestSyncMap_Range(t *testing.T) {
	var m syncMap[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := map[string]int{}
	m.Range(func(key string, value int) bool {
		seen[key] = value
		return true
	})
	assert.Equal(t, map[string]int{"a": 1, "b": 2, "c": 3}, seen)

	visits := 0
	m.Range(func(string, int) bool {
		visits++
		return false
	})
	assert.Equal(t, 1, visits, "Range must stop when the callback returns false")
}

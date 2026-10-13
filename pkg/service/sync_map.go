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

import "sync"

// syncMap is a type-safe wrapper over sync.Map. It keeps sync.Map's
// lock-free reads while confining the interface{} type assertions to one
// place, where each one is checked.
type syncMap[K comparable, V any] struct {
	m sync.Map
}

// Load returns the value stored for key and whether it was present.
func (s *syncMap[K, V]) Load(key K) (V, bool) {
	stored, ok := s.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	value, ok := stored.(V)
	return value, ok
}

// LoadOrStore returns the existing value for key if present; otherwise it
// stores and returns value.
func (s *syncMap[K, V]) LoadOrStore(key K, value V) V {
	actual, _ := s.m.LoadOrStore(key, value)
	if existing, ok := actual.(V); ok {
		return existing
	}
	return value
}

// Store sets the value for key.
func (s *syncMap[K, V]) Store(key K, value V) {
	s.m.Store(key, value)
}

// Delete removes the value for key.
func (s *syncMap[K, V]) Delete(key K) {
	s.m.Delete(key)
}

// Range calls f for each key and value in the map until f returns false.
func (s *syncMap[K, V]) Range(f func(key K, value V) bool) {
	s.m.Range(func(k, v any) bool {
		key, keyOK := k.(K)
		value, valueOK := v.(V)
		if !keyOK || !valueOK {
			return true
		}
		return f(key, value)
	})
}

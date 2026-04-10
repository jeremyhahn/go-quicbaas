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
	"encoding/base64"
	"encoding/json"
	"reflect"
)

// toMap converts an interface to a map[string]interface{} using JSON marshaling
// to properly handle struct fields with json tags.
func toMap(v interface{}) (map[string]interface{}, bool) {
	if v == nil {
		return nil, false
	}

	// If already a map, return it.
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}

	// Use reflection to check if it's a pointer.
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, false
		}
		val = val.Elem()
	}

	// For structs, marshal to JSON and unmarshal to map.
	if val.Kind() == reflect.Struct {
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, false
		}
		return result, true
	}

	return nil, false
}

// getString extracts a string value from a map.
func getString(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// getInt extracts an int value from a map.
func getInt(m map[string]interface{}, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}

	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// getBool extracts a bool value from a map.
func getBool(m map[string]interface{}, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// getMap extracts a nested map from a map.
func getMap(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}

	// If it's already a map[string]interface{}, return it.
	if nested, ok := v.(map[string]interface{}); ok {
		return nested, true
	}

	// Try to convert from a struct using JSON.
	return toMap(v)
}

// getStringSlice extracts a string slice from a map.
func getStringSlice(m map[string]interface{}, key string) ([]string, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}

	// If already a string slice, return it.
	if ss, ok := v.([]string); ok {
		return ss, true
	}

	// If it's an interface slice, convert each element.
	if is, ok := v.([]interface{}); ok {
		result := make([]string, 0, len(is))
		for _, item := range is {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result, true
	}

	return nil, false
}

// getStringMap extracts a map[string]string from a map.
func getStringMap(m map[string]interface{}, key string) (map[string]string, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}

	// If already a map[string]string, return it.
	if sm, ok := v.(map[string]string); ok {
		return sm, true
	}

	// If it's a map[string]interface{}, convert each value.
	if im, ok := v.(map[string]interface{}); ok {
		result := make(map[string]string, len(im))
		for k, val := range im {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		return result, true
	}

	return nil, false
}

// getBytes extracts a byte slice from a map.
// Handles both raw byte slices and base64-encoded strings (from JSON marshaling).
func getBytes(m map[string]interface{}, key string) ([]byte, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}

	// If already a byte slice, return it.
	if b, ok := v.([]byte); ok {
		return b, true
	}

	// If it's a string, try to decode as base64 first (JSON marshals []byte as base64).
	if s, ok := v.(string); ok {
		// Try base64 decoding first
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err == nil {
			return decoded, true
		}
		// If not valid base64, return as raw bytes
		return []byte(s), true
	}

	return nil, false
}

// getInterfaceSlice extracts an interface slice from a map.
func getInterfaceSlice(m map[string]interface{}, key string) ([]interface{}, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	is, ok := v.([]interface{})
	return is, ok
}

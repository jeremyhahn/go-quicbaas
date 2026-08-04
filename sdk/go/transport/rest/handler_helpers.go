// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// handler_helpers.go holds small shared helpers used by the per-service typed
// REST handlers. The SDK client packages build unexported request structs for
// path-parameter-only operations (e.g. kms.getKeyRequest{KeyID}). Because those
// types are unexported in their own package, the rest transport cannot
// type-assert them; instead it reads the exported path-parameter field by name
// via reflection.

package rest

import (
	"reflect"
	"strings"
)

// extractStringField returns the value of the named exported string field on
// the supplied request struct (or pointer to struct). It reports false when the
// request is nil, is not a struct, or lacks a string field with that name.
func extractStringField(req interface{}, field string) (string, bool) {
	if req == nil {
		return "", false
	}
	v := reflect.ValueOf(req)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	f := v.FieldByName(field)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// extractKeyIDField returns the value of the exported KeyID field on a request
// struct, used by the KMS handler for path-parameter-only operations.
func extractKeyIDField(req interface{}) (string, bool) {
	return extractStringField(req, "KeyID")
}

// extractIntField returns the value of the named exported integer field on the
// supplied request struct (or pointer to struct) as an int64. It reports false
// when the field is absent or not an integer kind.
func extractIntField(req interface{}, field string) (int64, bool) {
	if req == nil {
		return 0, false
	}
	v := reflect.ValueOf(req)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	f := v.FieldByName(field)
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return f.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(f.Uint()), true
	default:
		return 0, false
	}
}

// extractBoolField returns the value of the named exported bool field on the
// supplied request struct (or pointer to struct). It reports false when the
// field is absent or not a bool.
func extractBoolField(req interface{}, field string) (bool, bool) {
	if req == nil {
		return false, false
	}
	v := reflect.ValueOf(req)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false, false
	}
	f := v.FieldByName(field)
	if !f.IsValid() || f.Kind() != reflect.Bool {
		return false, false
	}
	return f.Bool(), true
}

// splitPathSegments splits a hierarchical resource path on "/" into its
// individual segments, dropping a leading slash if present.
func splitPathSegments(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// joinPathSegments rejoins escaped path segments with "/" separators.
func joinPathSegments(segments []string) string {
	return strings.Join(segments, "/")
}

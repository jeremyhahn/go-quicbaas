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

// result_helpers.go contains shared helpers used by the typed MCP service
// handlers (certificate, messaging, objectstorage) to build tool arguments
// and decode the JSON-decoded tool result map into concrete SDK types.
//
// The MCP transport's extractContent step parses the tool's text content as
// JSON, yielding a map[string]interface{} (or a scalar). These helpers turn
// that loosely-typed map into the strongly-typed fields the SDK clients
// expect, applying the wire conventions used by quicbaasd (RFC 3339
// timestamps, hex-encoded checksums, JSON-stringified arrays, etc.).

package mcp

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// asResultMap coerces the tool result into a map[string]interface{}. A nil
// result yields an empty map so callers can decode zero-valued responses
// without a nil check at every field access.
func asResultMap(resp interface{}, op string) (map[string]interface{}, error) {
	if resp == nil {
		return map[string]interface{}{}, nil
	}
	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.CodeInternal, op,
			"unexpected MCP tool result shape, expected JSON object")
	}
	return m, nil
}

// getResultString returns the string value for key, or "" when absent or of
// the wrong type.
func getResultString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getResultBool returns the bool value for key, or false when absent.
func getResultBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// getResultInt64 returns the integer value for key. JSON numbers decode as
// float64, so both float64 and int64 are accepted.
func getResultInt64(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	}
	return 0
}

// getResultUint64 returns the unsigned integer value for key.
func getResultUint64(m map[string]interface{}, key string) uint64 {
	switch v := m[key].(type) {
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case uint64:
		return v
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil && n >= 0 {
			return uint64(n)
		}
	}
	return 0
}

// getResultFloat64 returns the float value for key, or 0 when absent.
func getResultFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

// resultFloat32Slice extracts a []float32 vector from a result field.
func resultFloat32Slice(m map[string]interface{}, key string) []float32 {
	return toFloat32Slice(m[key])
}

// resultFloat32Matrix extracts a list of []float32 vectors from a result
// field; an entry that is not a vector decodes as nil, keeping the order.
func resultFloat32Matrix(m map[string]interface{}, key string) [][]float32 {
	switch v := m[key].(type) {
	case []interface{}:
		rows := make([][]float32, len(v))
		for i, row := range v {
			rows[i] = toFloat32Slice(row)
		}
		return rows
	case [][]float32:
		return v
	default:
		return nil
	}
}

func toFloat32Slice(raw interface{}) []float32 {
	switch v := raw.(type) {
	case []interface{}:
		result := make([]float32, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				result = append(result, float32(f))
			}
		}
		return result
	case []float64:
		result := make([]float32, len(v))
		for i, f := range v {
			result[i] = float32(f)
		}
		return result
	case []float32:
		return v
	default:
		return nil
	}
}

// getResultTime parses an RFC 3339 timestamp string for key, falling back to
// a numeric Unix-seconds value when the field is a number. Returns the zero
// time when the field is missing or unparseable.
func getResultTime(m map[string]interface{}, key string) time.Time {
	switch v := m[key].(type) {
	case string:
		return transformTimeFromString(v)
	case float64:
		return time.Unix(int64(v), 0).UTC()
	}
	return time.Time{}
}

// getResultStringMap returns a map[string]string for key, decoding from a
// map[string]interface{} whose values are strings.
func getResultStringMap(m map[string]interface{}, key string) map[string]string {
	raw, ok := m[key].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// putStringArg sets args[key] = value only when value is non-empty, keeping
// optional fields out of the tool argument map.
func putStringArg(args map[string]interface{}, key, value string) {
	if value != "" {
		args[key] = value
	}
}

// putJSONArrayArg JSON-encodes a non-empty string slice and stores it as a
// string argument. quicbaasd tools accept JSON-array-encoded string params
// (e.g. dns_names, ip_addresses).
func putJSONArrayArg(args map[string]interface{}, key string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return errors.NewInternal("mcp.putJSONArrayArg",
			"failed to encode "+key+": "+err.Error())
	}
	args[key] = string(encoded)
	return nil
}

// trimServiceName returns the trailing component of a dot-separated service
// name. "quicbaas.Messaging" -> "Messaging".
func trimServiceName(service string) string {
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		return service[idx+1:]
	}
	return service
}

// parseDurationLoose parses a Go duration string, returning the parsed
// duration and any parse error.
func parseDurationLoose(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

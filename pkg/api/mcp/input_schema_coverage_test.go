//go:build mcp

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

// input_schema_coverage_test.go is the registry-exhaustive guard for
// go-quicbaas#1024: three SSH tools (quicbaas_ssh_authorized_list,
// quicbaas_ssh_key_list, quicbaas_ssh_known_host_list) shipped with
// `"required": null` in their inputSchema instead of `"required": []` (or the
// key omitted) — a nil []string built from a variadic parameter called with
// zero arguments, which encoding/json marshals as JSON "null". Claude Code's
// MCP client validates every tool's inputSchema against the JSON-Schema
// shape the specification requires and rejected the WHOLE tools/list
// response over one malformed entry.
//
// This test marshals every registered tool's inputSchema exactly the way the
// server does over the wire, decodes it back into the generic shape a client
// sees, and asserts the properties a real MCP client enforces: an object
// schema, "properties" is an object when present, and "required" — when the
// key is present at all — is an array of strings that are all declared
// properties. It walks both registries the server merges into one tools/list
// (see registeredToolNames in authz_qrdb_coverage_test.go), so a bad schema
// anywhere in the MCP surface fails this test the same way it would fail an
// MCP client's own validation.
package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// wireSchema is one tool's inputSchema, marshaled and re-decoded exactly as
// it crosses the wire to an MCP client.
type wireSchema struct {
	toolName string
	raw      map[string]interface{}
}

// registeredToolSchemas marshals and re-decodes every registered tool's
// inputSchema from both registries the server merges into tools/list, so
// each entry here is the exact JSON shape a client receives.
func registeredToolSchemas(t *testing.T) []wireSchema {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	var schemas []wireSchema

	for _, tool := range NewToolRegistry(&agentMockQuicBaaS{}, log).ListTools() {
		schemas = append(schemas, wireSchema{toolName: tool.Name, raw: roundTripSchema(t, tool.Name, tool.InputSchema)})
	}
	for _, tool := range NewMCPServer(nil, "", log).GetTools() {
		schemas = append(schemas, wireSchema{toolName: tool.Name, raw: roundTripSchema(t, tool.Name, tool.InputSchema)})
	}

	require.NotEmpty(t, schemas, "the MCP surface must register at least one tool")
	return schemas
}

// roundTripSchema marshals schema (whatever concrete type the registry
// stores it as) and decodes it back into a generic map, matching what an MCP
// client actually parses — a struct field with a Go zero value that
// encoding/json happens to render acceptably would hide the defect this test
// exists to catch.
func roundTripSchema(t *testing.T, toolName string, schema interface{}) map[string]interface{} {
	t.Helper()

	data, err := json.Marshal(schema)
	require.NoErrorf(t, err, "tool %q: inputSchema must be marshalable", toolName)

	var decoded map[string]interface{}
	require.NoErrorf(t, json.Unmarshal(data, &decoded), "tool %q: inputSchema must decode as a JSON object", toolName)
	return decoded
}

// TestToolInputSchemas_AreValidObjectSchemas is the regression test for
// go-quicbaas#1024's schema defect: every registered tool's inputSchema must
// be type "object", "properties" (when present) must be a JSON object, and
// "required" (when the key is present at all) must be a JSON array — never
// JSON null, which is exactly what a nil Go slice marshals to.
func TestToolInputSchemas_AreValidObjectSchemas(t *testing.T) {
	t.Parallel()

	for _, schema := range registeredToolSchemas(t) {
		t.Run(schema.toolName, func(t *testing.T) {
			t.Parallel()

			assert.Equalf(t, "object", schema.raw["type"],
				"tool %q: inputSchema.type must be \"object\"", schema.toolName)

			properties, hasProperties := schema.raw["properties"]
			if hasProperties {
				assert.IsTypef(t, map[string]interface{}{}, properties,
					"tool %q: inputSchema.properties must be a JSON object", schema.toolName)
			}

			requiredRaw, hasRequired := schema.raw["required"]
			if !hasRequired {
				// Omitting "required" entirely is valid MCP JSON-Schema for a
				// tool with no mandatory arguments; nothing further to check.
				return
			}

			required, ok := requiredRaw.([]interface{})
			require.Truef(t, ok,
				"tool %q: inputSchema.required must be a JSON array when present, got %T (%v) — "+
					"a nil Go []string marshals to JSON null, which is exactly this defect "+
					"(go-quicbaas#1024): build it with make([]string, 0) or append, never leave "+
					"a zero-argument variadic unset", schema.toolName, requiredRaw, requiredRaw)

			props, _ := properties.(map[string]interface{})
			for _, name := range required {
				nameStr, ok := name.(string)
				require.Truef(t, ok, "tool %q: inputSchema.required entries must be strings, got %T (%v)",
					schema.toolName, name, name)
				_, declared := props[nameStr]
				assert.Truef(t, declared,
					"tool %q: inputSchema.required names %q, which is not a declared property",
					schema.toolName, nameStr)
			}
		})
	}
}

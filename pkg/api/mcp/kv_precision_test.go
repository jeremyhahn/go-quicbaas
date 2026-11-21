//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEntityKVTools_IDAbove2To53RoundTrips proves the entity put and get
// tools keep an ID above 2^53 exact both ways: the decimal-string id the SDK
// sends (the MCP layer decodes a JSON number argument through float64) is
// stored under that exact ID, and the tool text the module adapter
// json.Marshals answers it with the exact integer token.
func TestEntityKVTools_IDAbove2To53RoundTrips(t *testing.T) {
	const idToken = "9007199254740993" // 2^53+1
	registry, _ := newKVStoreRegistry(t)
	ctx := context.Background()
	for _, call := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"quicbaas_kv_put", map[string]interface{}{
			"entityType": "users", "id": idToken, "data": base64.StdEncoding.EncodeToString([]byte("v")),
		}},
		{"quicbaas_kv_get", map[string]interface{}{"entityType": "users", "id": idToken}},
	} {
		result, err := registry.ExecuteTool(ctx, call.tool, call.args)
		require.NoError(t, err, call.tool)
		text, err := json.Marshal(result)
		require.NoError(t, err, call.tool)
		if !strings.Contains(string(text), `"id":`+idToken) {
			t.Errorf("%s answered %s, want id %s", call.tool, text, idToken)
		}
	}
}

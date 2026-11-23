//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive the quicbaas_kv_* tools with the exact arguments the Go
// SDK's MCP transport sends (sdk/go/transport/mcp kv_handler.go) and decode
// the answer text the way the SDK does, so a drift between the two sides of
// the wire fails here. The root module cannot import sdk/go, so the SDK's
// shapes are restated below.

// sdkKVArgs is the argument map kv_handler.go builds: the id as a decimal
// string, and shardID only when it is not zero. Over JSON, the shard
// arrives as a float64.
func sdkKVArgs(entityType string, id, shardID uint64) map[string]interface{} {
	args := map[string]interface{}{
		"entity_type": entityType,
		"id":          strconv.FormatUint(id, 10),
	}
	if shardID != 0 {
		args["shardID"] = float64(shardID)
	}
	return args
}

// sdkPutAnswer, sdkGetAnswer and sdkDeleteAnswer are the shapes the SDK
// decodes the tools' answers into: ids and versions as JSON numbers.
type sdkPutAnswer struct {
	ID        uint64 `json:"id"`
	Version   uint64 `json:"version"`
	Timestamp int64  `json:"timestamp"`
	LeaderID  uint64 `json:"leader_id"`
}

type sdkGetAnswer struct {
	ID        uint64 `json:"id"`
	Found     bool   `json:"found"`
	Data      []byte `json:"data"`
	Version   uint64 `json:"version"`
	Timestamp int64  `json:"timestamp"`
}

type sdkDeleteAnswer struct {
	Deleted   bool  `json:"deleted"`
	Timestamp int64 `json:"timestamp"`
}

// sdkKVCall runs a tool with SDK arguments, renders the result as the server's
// text content (pkg/apimodule module_mcp.go json.Marshal) and decodes it into
// the SDK's shape.
func sdkKVCall(t *testing.T, registry *ToolRegistry, tool string, args map[string]interface{}, out interface{}) {
	t.Helper()
	result, err := registry.ExecuteTool(context.Background(), tool, args)
	require.NoError(t, err)
	text, err := json.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(text, out), "answer %s is not the SDK's shape", text)
}

func TestKVSDKRoundTrip_PutGetDeleteGet(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	const id = uint64(18446744073709551557)
	data := []byte{0x00, 0xff, 'v'}

	putArgs := sdkKVArgs("users", id, 3)
	putArgs["data"] = base64.StdEncoding.EncodeToString(data)
	var put sdkPutAnswer
	sdkKVCall(t, registry, "quicbaas_kv_put", putArgs, &put)
	assert.Equal(t, id, put.ID)
	assert.Equal(t, uint64(1<<60+1), put.Version)
	assert.Equal(t, uint64(1<<63+5), put.LeaderID)

	var got sdkGetAnswer
	sdkKVCall(t, registry, "quicbaas_kv_get", sdkKVArgs("users", id, 3), &got)
	assert.True(t, got.Found)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, data, got.Data)

	var del sdkDeleteAnswer
	sdkKVCall(t, registry, "quicbaas_kv_delete", sdkKVArgs("users", id, 3), &del)
	assert.True(t, del.Deleted)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kv_get", sdkKVArgs("users", id, 3))
	requireKVNotFound(t, result, err)

	require.Len(t, svc.puts, 1)
	assert.Equal(t, id, svc.puts[0].ID)
	assert.Equal(t, "users", svc.puts[0].EntityType)
	assert.Equal(t, uint64(3), svc.puts[0].ShardID)
	for _, req := range svc.gets {
		assert.Equal(t, uint64(3), req.ShardID)
	}
	require.Len(t, svc.deletes, 1)
	assert.Equal(t, uint64(3), svc.deletes[0].ShardID)
}

func TestKVSDKRoundTrip_ServiceErrorIsNotAMiss(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	svc.getErr = kvServiceTestError{}

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kv_get", sdkKVArgs("users", 7, 0))
	require.ErrorIs(t, err, kvServiceTestError{}, "the SDK must see a tool error, never found:false")
}

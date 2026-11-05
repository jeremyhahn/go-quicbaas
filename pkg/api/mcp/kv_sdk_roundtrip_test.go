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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// These tests drive the quicbaas_kv_* tools with the exact arguments the Go
// SDK's MCP transport sends (sdk/go/transport/mcp kv_handler.go kvArgs) and
// decode the answer text the way the SDK does, so a drift between the two
// sides of the wire fails here. The root module cannot import sdk/go, so the
// SDK's shapes are restated below.

// sdkKVArgs is the argument map kv_handler.go's kvArgs builds.
func sdkKVArgs(entityType string, id, shardID uint64, consistency service.ConsistencyLevel) map[string]interface{} {
	return map[string]interface{}{
		"entity_type": entityType,
		"id":          strconv.FormatUint(id, 10),
		"shard_id":    strconv.FormatUint(shardID, 10),
		"consistency": strconv.Itoa(int(consistency)),
	}
}

// sdkPutAnswer, sdkGetAnswer and sdkDeleteAnswer are the shapes the SDK
// decodes the tools' answers into.
type sdkPutAnswer struct {
	ID        uint64 `json:"id,string"`
	Version   uint64 `json:"version,string"`
	Timestamp int64  `json:"timestamp"`
	LeaderID  uint64 `json:"leader_id,string"`
}

type sdkGetAnswer struct {
	ID        uint64 `json:"id,string"`
	Found     bool   `json:"found"`
	Data      []byte `json:"data"`
	Version   uint64 `json:"version,string"`
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

	putArgs := sdkKVArgs("users", id, 3, service.ConsistencyLevelStrong)
	putArgs["data"] = base64.StdEncoding.EncodeToString(data)
	var put sdkPutAnswer
	sdkKVCall(t, registry, "quicbaas_kv_put", putArgs, &put)
	assert.Equal(t, id, put.ID)
	assert.Equal(t, uint64(1<<60+1), put.Version)
	assert.Equal(t, uint64(1<<63+5), put.LeaderID)

	var got sdkGetAnswer
	sdkKVCall(t, registry, "quicbaas_kv_get", sdkKVArgs("users", id, 3, service.ConsistencyLevelStrong), &got)
	assert.True(t, got.Found)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, data, got.Data)

	var del sdkDeleteAnswer
	sdkKVCall(t, registry, "quicbaas_kv_delete", sdkKVArgs("users", id, 3, service.ConsistencyLevelStrong), &del)
	assert.True(t, del.Deleted)

	var miss sdkGetAnswer
	sdkKVCall(t, registry, "quicbaas_kv_get", sdkKVArgs("users", id, 3, service.ConsistencyLevelStrong), &miss)
	assert.False(t, miss.Found, "a deleted entity reads as a miss")
	assert.Nil(t, miss.Data)

	require.Len(t, svc.puts, 1)
	assert.Equal(t, id, svc.puts[0].ID)
	assert.Equal(t, uint64(3), svc.puts[0].ShardID)
	assert.Equal(t, service.ConsistencyLevelStrong, svc.puts[0].Consistency)
	for _, req := range svc.gets {
		assert.Equal(t, uint64(3), req.ShardID)
	}
	require.Len(t, svc.deletes, 1)
	assert.Equal(t, uint64(3), svc.deletes[0].ShardID)
}

func TestKVSDKRoundTrip_ServiceErrorIsNotAMiss(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	svc.getErr = kvServiceTestError{}

	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kv_get",
		sdkKVArgs("users", 7, 0, service.ConsistencyLevelUnspecified))
	require.ErrorIs(t, err, kvServiceTestError{}, "the SDK must see a tool error, never found:false")
}

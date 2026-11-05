//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Delete removes an entity from the shared in-memory map, so a record the
// qrdb_delete tool removes is gone for the service's own Get.
func (s *kvStoreService) Delete(_ context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	key := kvStoreKey(req.EntityType, req.ID)
	_, found := s.entries[key]
	delete(s.entries, key)
	return &service.DeleteResponse{Deleted: found}, nil
}

// sdkWireArgs builds the arguments the Go SDK's MCP transport sends for a KV
// call (sdk/go/transport/mcp kv_handler.go kvPutArgs, kvGetArgs and
// kvDeleteArgs, which the root module cannot import) and passes them through
// JSON, as they cross the wire.
func sdkWireArgs(t *testing.T, entityType string, id uint64, data []byte) map[string]interface{} {
	t.Helper()
	args := map[string]interface{}{"entity_type": entityType, "id": id}
	if data != nil {
		args["data"] = base64.StdEncoding.EncodeToString(data)
	}
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &wire))
	return wire
}

// decodeToolAnswer decodes a tool result the way the SDK does: from the JSON
// the server sends into the service response type.
func decodeToolAnswer(t *testing.T, result interface{}, resp interface{}) {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, resp))
}

func TestKVSDKRoundTrip_PutAndDeleteReachServiceGet(t *testing.T) {
	registry, store := newKVStoreRegistry(t)
	ctx := context.Background()
	data := []byte{0x00, 0x01, 0xfe}

	result, err := registry.ExecuteTool(ctx, "qrdb_put", sdkWireArgs(t, "users", 7, data))
	require.NoError(t, err)
	var put service.PutResponse
	decodeToolAnswer(t, result, &put)
	assert.Equal(t, uint64(7), put.ID)

	got, err := store.Get(ctx, &service.GetRequest{EntityType: "users", ID: 7})
	require.NoError(t, err)
	assert.True(t, got.Found, "service Get must find the record the SDK put")
	assert.Equal(t, data, got.Data)

	result, err = registry.ExecuteTool(ctx, "qrdb_get", sdkWireArgs(t, "users", 7, nil))
	require.NoError(t, err)
	var read service.GetResponse
	decodeToolAnswer(t, result, &read)
	assert.True(t, read.Found)
	assert.Equal(t, data, read.Data)

	result, err = registry.ExecuteTool(ctx, "qrdb_delete", sdkWireArgs(t, "users", 7, nil))
	require.NoError(t, err)
	var del service.DeleteResponse
	decodeToolAnswer(t, result, &del)
	assert.True(t, del.Deleted)

	got, err = store.Get(ctx, &service.GetRequest{EntityType: "users", ID: 7})
	require.NoError(t, err)
	assert.False(t, got.Found, "service Get must not find the record the SDK deleted")
}

func TestKVSDKRoundTrip_StringKeyMissesTheEntity(t *testing.T) {
	registry, store := newKVStoreRegistry(t)
	ctx := context.Background()

	// The old SDK key form hashes to another ID, so the service never sees
	// users/7: the reason the SDK sends entity_type and id.
	_, err := registry.ExecuteTool(ctx, "qrdb_put", map[string]interface{}{
		"entity_type": "users",
		"key":         "users/7",
		"data":        base64.StdEncoding.EncodeToString([]byte("v")),
	})
	require.NoError(t, err)

	got, err := store.Get(ctx, &service.GetRequest{EntityType: "users", ID: 7})
	require.NoError(t, err)
	assert.False(t, got.Found)
}

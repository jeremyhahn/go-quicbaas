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
// quicbaas_batch_delete tool removes is gone for the service's own Get.
func (s *kvStoreService) Delete(_ context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	key := kvStoreKey(req.EntityType, req.ID)
	_, found := s.entries[key]
	delete(s.entries, key)
	return &service.DeleteResponse{Deleted: found}, nil
}

// BatchGet answers each get from the shared map, as the service's batch
// layer does: one GetResponse per request, Found=false when it is missing.
func (s *kvStoreService) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	resp := &service.BatchGetResponse{}
	for _, get := range req.Requests {
		getResp, err := s.Get(ctx, get)
		if err != nil {
			return nil, err
		}
		resp.Responses = append(resp.Responses, getResp)
	}
	return resp, nil
}

// BatchDelete removes each entity from the shared map.
func (s *kvStoreService) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	resp := &service.BatchDeleteResponse{}
	for _, del := range req.Requests {
		delResp, err := s.Delete(ctx, del)
		if err != nil {
			return nil, err
		}
		resp.Responses = append(resp.Responses, delResp)
	}
	return resp, nil
}

// sdkBatchArgs builds the arguments the Go SDK's MCP transport sends for a
// typed KV call (sdk/go/transport/mcp kv_handler.go kvPutArgs, kvGetArgs and
// kvDeleteArgs, which the root module cannot import) and passes them through
// JSON, as they cross the wire.
func sdkBatchArgs(t *testing.T, entityType string, id uint64, data []byte) map[string]interface{} {
	t.Helper()
	op := map[string]interface{}{"entityType": entityType, "id": id}
	if data != nil {
		op["data"] = base64.StdEncoding.EncodeToString(data)
	}
	raw, err := json.Marshal(map[string]interface{}{"operations": []interface{}{op}})
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &wire))
	return wire
}

// decodeToolAnswer decodes a tool result the way the SDK does: from the JSON
// the server sends into the service batch response type.
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

	result, err := registry.ExecuteTool(ctx, "quicbaas_batch_put", sdkBatchArgs(t, "users", 7, data))
	require.NoError(t, err)
	var put service.BatchPutResponse
	decodeToolAnswer(t, result, &put)
	require.Len(t, put.Responses, 1)
	assert.Equal(t, uint64(7), put.Responses[0].ID)

	got, err := store.Get(ctx, &service.GetRequest{EntityType: "users", ID: 7})
	require.NoError(t, err)
	assert.True(t, got.Found, "service Get must find the record the SDK put")
	assert.Equal(t, data, got.Data)

	result, err = registry.ExecuteTool(ctx, "quicbaas_batch_get", sdkBatchArgs(t, "users", 7, nil))
	require.NoError(t, err)
	var read service.BatchGetResponse
	decodeToolAnswer(t, result, &read)
	require.Len(t, read.Responses, 1)
	assert.True(t, read.Responses[0].Found)
	assert.Equal(t, data, read.Responses[0].Data)

	result, err = registry.ExecuteTool(ctx, "quicbaas_batch_delete", sdkBatchArgs(t, "users", 7, nil))
	require.NoError(t, err)
	var del service.BatchDeleteResponse
	decodeToolAnswer(t, result, &del)
	require.Len(t, del.Responses, 1)
	assert.True(t, del.Responses[0].Deleted)

	got, err = store.Get(ctx, &service.GetRequest{EntityType: "users", ID: 7})
	require.NoError(t, err)
	assert.False(t, got.Found, "service Get must not find the record the SDK deleted")

	result, err = registry.ExecuteTool(ctx, "quicbaas_batch_get", sdkBatchArgs(t, "users", 7, nil))
	require.NoError(t, err)
	read = service.BatchGetResponse{}
	decodeToolAnswer(t, result, &read)
	require.Len(t, read.Responses, 1)
	assert.False(t, read.Responses[0].Found, "a deleted entity reads back as Found=false")
}

func TestKVSDKRoundTrip_MissingIDIsRejected(t *testing.T) {
	registry, _ := newKVStoreRegistry(t)

	args := sdkBatchArgs(t, "users", 7, []byte("v"))
	delete(args["operations"].([]interface{})[0].(map[string]interface{}), "id")
	_, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_put", args)
	var parseErr *BatchOperationParseError
	require.ErrorAs(t, err, &parseErr)
	assert.Equal(t, "id", parseErr.Field)
}

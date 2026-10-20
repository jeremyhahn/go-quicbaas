//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kvStoreService is a mockService whose Put, BatchPut and Get share one
// in-memory map, so a value written by a put tool can be read back by
// qrdb_get byte for byte.
type kvStoreService struct {
	*mockService
	entries map[string][]byte
}

func newKVStoreService() *kvStoreService {
	return &kvStoreService{mockService: &mockService{}, entries: map[string][]byte{}}
}

func kvStoreKey(entityType string, id uint64) string {
	return entityType + "/" + strconv.FormatUint(id, 10)
}

func (s *kvStoreService) Put(_ context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	s.entries[kvStoreKey(req.EntityType, req.ID)] = req.Data
	return &service.PutResponse{ID: req.ID, Version: 1}, nil
}

func (s *kvStoreService) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	resp := &service.BatchPutResponse{}
	for _, put := range req.Requests {
		putResp, err := s.Put(ctx, put)
		if err != nil {
			return nil, err
		}
		resp.Responses = append(resp.Responses, putResp)
	}
	return resp, nil
}

func (s *kvStoreService) Get(_ context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	data, found := s.entries[kvStoreKey(req.EntityType, req.ID)]
	return &service.GetResponse{ID: req.ID, Data: data, Found: found}, nil
}

func newKVStoreRegistry(t *testing.T) (*ToolRegistry, *kvStoreService) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	store := newKVStoreService()
	return NewToolRegistry(store, log), store
}

// getBytes reads an entity through qrdb_get and decodes the base64
// data field the JSON result carries.
func getBytes(t *testing.T, registry *ToolRegistry, entityType string, id float64) []byte {
	t.Helper()
	result, err := registry.ExecuteTool(context.Background(), "qrdb_get", map[string]interface{}{
		"entityType": entityType,
		"id":         id,
	})
	require.NoError(t, err)
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "qrdb_get result is %T", result)
	encoded, ok := resultMap["data"].(string)
	require.True(t, ok, "qrdb_get data is %T", resultMap["data"])
	data, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	return data
}

var binaryValue = []byte{0x00, 0xff, 0x80}

func TestPutTool_BinaryValueRoundTrips(t *testing.T) {
	for _, key := range []string{"data", "value"} {
		t.Run(key, func(t *testing.T) {
			registry, _ := newKVStoreRegistry(t)
			_, err := registry.ExecuteTool(context.Background(), "qrdb_put", map[string]interface{}{
				"entityType": "blobs",
				"id":         float64(7),
				key:          base64.StdEncoding.EncodeToString(binaryValue),
			})
			require.NoError(t, err)
			assert.Equal(t, binaryValue, getBytes(t, registry, "blobs", 7))
		})
	}
}

func TestPutTool_BadBase64Refused(t *testing.T) {
	registry, store := newKVStoreRegistry(t)
	_, err := registry.ExecuteTool(context.Background(), "qrdb_put", map[string]interface{}{
		"entityType": "blobs",
		"id":         float64(7),
		"data":       "not base64!",
	})
	require.Error(t, err)
	var argErr *UserToolInvalidArgumentError
	require.ErrorAs(t, err, &argErr)
	assert.Equal(t, "data", argErr.Arg)
	assert.Empty(t, store.entries, "a refused put must store nothing")
}

func TestBatchPutTool_BinaryValueRoundTrips(t *testing.T) {
	registry, _ := newKVStoreRegistry(t)
	_, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_put", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"entityType": "blobs",
				"id":         float64(1),
				"data":       base64.StdEncoding.EncodeToString(binaryValue),
			},
			map[string]interface{}{
				"entityType": "blobs",
				"id":         float64(2),
				"value":      base64.StdEncoding.EncodeToString([]byte{0x80}),
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, binaryValue, getBytes(t, registry, "blobs", 1))
	assert.Equal(t, []byte{0x80}, getBytes(t, registry, "blobs", 2))
}

func TestBatchPutTool_BadBase64Refused(t *testing.T) {
	registry, store := newKVStoreRegistry(t)
	_, err := registry.ExecuteTool(context.Background(), "quicbaas_batch_put", map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"entityType": "blobs",
				"id":         float64(1),
				"data":       base64.StdEncoding.EncodeToString(binaryValue),
			},
			map[string]interface{}{
				"entityType": "blobs",
				"id":         float64(2),
				"data":       "not base64!",
			},
		},
	})
	require.Error(t, err)
	var parseErr *BatchOperationParseError
	require.ErrorAs(t, err, &parseErr)
	assert.Equal(t, 1, parseErr.Index)
	assert.Equal(t, "data", parseErr.Field)
	assert.Empty(t, store.entries, "a refused batch must store nothing, not even its valid operations")
}

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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kvServiceTestError is a service failure that is not a not-found.
type kvServiceTestError struct{}

func (kvServiceTestError) Error() string { return "kv service unavailable" }

// kvRecordingService is a KV service double that keeps entries in a map,
// fronts them with a read cache the way the KV service does, records every
// request it receives, and answers a miss with the service's not-found error.
type kvRecordingService struct {
	*mockService
	entries map[string][]byte
	cache   map[string][]byte
	puts    []*service.PutRequest
	gets    []*service.GetRequest
	deletes []*service.DeleteRequest
	getErr  error
}

func newKVRecordingRegistry(t *testing.T) (*ToolRegistry, *kvRecordingService) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	svc := &kvRecordingService{
		mockService: &mockService{},
		entries:     map[string][]byte{},
		cache:       map[string][]byte{},
	}
	return NewToolRegistry(svc, log), svc
}

func kvRecordingKey(entityType string, id uint64) string {
	return entityType + ":" + strconv.FormatUint(id, 10)
}

func (s *kvRecordingService) Put(_ context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	s.puts = append(s.puts, req)
	key := kvRecordingKey(req.EntityType, req.ID)
	s.entries[key] = req.Data
	delete(s.cache, key)
	return &service.PutResponse{ID: req.ID, Version: 1<<60 + 1, LeaderID: 1<<63 + 5}, nil
}

func (s *kvRecordingService) Get(_ context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	s.gets = append(s.gets, req)
	if s.getErr != nil {
		return nil, s.getErr
	}
	key := kvRecordingKey(req.EntityType, req.ID)
	if data, ok := s.cache[key]; ok {
		return &service.GetResponse{ID: req.ID, Data: data, Found: true}, nil
	}
	data, ok := s.entries[key]
	if !ok {
		return nil, &qberrors.QBError{
			Code: qberrors.ErrNotFound,
			Op:   "service.Get",
			Err:  &api.NotFoundError{Resource: req.EntityType, ID: strconv.FormatUint(req.ID, 10)},
		}
	}
	s.cache[key] = data
	return &service.GetResponse{ID: req.ID, Data: data, Found: true}, nil
}

func (s *kvRecordingService) Delete(_ context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	s.deletes = append(s.deletes, req)
	key := kvRecordingKey(req.EntityType, req.ID)
	_, found := s.entries[key]
	delete(s.entries, key)
	delete(s.cache, key)
	return &service.DeleteResponse{Deleted: found}, nil
}

// kvToolCall runs a KV tool and decodes its answer from the JSON the server
// sends, as a client reads it.
func kvToolCall(t *testing.T, registry *ToolRegistry, tool string, args map[string]interface{}, out interface{}) {
	t.Helper()
	result, err := registry.ExecuteTool(context.Background(), tool, args)
	require.NoError(t, err)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, out))
}

func kvArgs(id string, extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{"entity_type": "users", "id": id}
	for key, value := range extra {
		args[key] = value
	}
	return args
}

func TestKVTools_PutThenGetReturnsSameBytes(t *testing.T) {
	registry, _ := newKVRecordingRegistry(t)
	data := []byte{0x00, 0x01, 0xfe}

	var put map[string]interface{}
	kvToolCall(t, registry, "quicbaas_kv_put", kvArgs("7", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString(data),
	}), &put)
	assert.Equal(t, "7", put["id"])
	assert.Equal(t, "1152921504606846977", put["version"], "version is an exact decimal string")
	assert.Equal(t, "9223372036854775813", put["leader_id"], "leader_id is an exact decimal string")

	var got kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", nil), &got)
	assert.True(t, got.Found)
	assert.Equal(t, data, got.Data)
}

func TestKVTools_IDAbove2To53RoundTripsExactly(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	const id = "18446744073709551557"

	var put map[string]interface{}
	kvToolCall(t, registry, "quicbaas_kv_put", kvArgs(id, map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString([]byte("v")),
	}), &put)
	assert.Equal(t, id, put["id"])
	require.Len(t, svc.puts, 1)
	assert.Equal(t, uint64(18446744073709551557), svc.puts[0].ID)

	var got map[string]interface{}
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs(id, nil), &got)
	assert.Equal(t, id, got["id"])
	assert.Equal(t, true, got["found"])
}

func TestKVTools_ShardAndConsistencyReachTheService(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	extra := map[string]interface{}{"shard_id": "3", "consistency": "2"}

	putArgs := kvArgs("7", map[string]interface{}{"data": base64.StdEncoding.EncodeToString([]byte("v"))})
	putArgs["shard_id"] = "3"
	putArgs["consistency"] = "2"
	var put kvPutResult
	kvToolCall(t, registry, "quicbaas_kv_put", putArgs, &put)
	var got kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", extra), &got)
	var del kvDeleteResult
	kvToolCall(t, registry, "quicbaas_kv_delete", kvArgs("7", extra), &del)

	require.Len(t, svc.puts, 1)
	require.Len(t, svc.gets, 1)
	require.Len(t, svc.deletes, 1)
	assert.Equal(t, uint64(3), svc.puts[0].ShardID)
	assert.Equal(t, uint64(3), svc.gets[0].ShardID)
	assert.Equal(t, uint64(3), svc.deletes[0].ShardID)
	assert.Equal(t, service.ConsistencyLevelLinearizable, svc.puts[0].Consistency)
	assert.Equal(t, service.ConsistencyLevelLinearizable, svc.gets[0].Consistency)
	assert.Equal(t, service.ConsistencyLevelLinearizable, svc.deletes[0].Consistency)
	assert.Equal(t, "users", svc.puts[0].EntityType)
}

func TestKVTools_DefaultShardIsZero(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	var got kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", nil), &got)
	require.Len(t, svc.gets, 1)
	assert.Equal(t, uint64(0), svc.gets[0].ShardID)
}

func TestKVTools_GetNotFoundIsAMiss(t *testing.T) {
	registry, _ := newKVRecordingRegistry(t)
	var got kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", nil), &got)
	assert.False(t, got.Found)
	assert.Nil(t, got.Data)
}

func TestKVTools_GetServiceErrorIsAToolError(t *testing.T) {
	registry, svc := newKVRecordingRegistry(t)
	svc.getErr = kvServiceTestError{}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kv_get", kvArgs("7", nil))
	require.ErrorIs(t, err, kvServiceTestError{})
	assert.Nil(t, result, "a service failure must not read as found:false")
}

func TestKVTools_DeleteThenGetMissesThroughWarmCache(t *testing.T) {
	registry, _ := newKVRecordingRegistry(t)
	kvToolCall(t, registry, "quicbaas_kv_put", kvArgs("7", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString([]byte("v")),
	}), &kvPutResult{})

	var warm kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", nil), &warm)
	require.True(t, warm.Found)

	var del kvDeleteResult
	kvToolCall(t, registry, "quicbaas_kv_delete", kvArgs("7", nil), &del)
	assert.True(t, del.Deleted)

	var got kvGetResult
	kvToolCall(t, registry, "quicbaas_kv_get", kvArgs("7", nil), &got)
	assert.False(t, got.Found, "a deleted entity must not be served from the cache")
}

func TestKVTools_BadArgumentsRefusedBeforeTheService(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]interface{}
		arg  string
	}{
		{"bad id", "quicbaas_kv_get", kvArgs("abc", nil), "id"},
		{"negative shard", "quicbaas_kv_get", kvArgs("7", map[string]interface{}{"shard_id": "-1"}), "shard_id"},
		{"missing id", "quicbaas_kv_delete", map[string]interface{}{"entity_type": "users"}, "id"},
		{"numeric id", "quicbaas_kv_put", map[string]interface{}{"entity_type": "users", "id": float64(7), "data": "dg=="}, "id"},
		{"missing entity type", "quicbaas_kv_get", map[string]interface{}{"id": "7"}, "entity_type"},
		{"consistency out of range", "quicbaas_kv_get", kvArgs("7", map[string]interface{}{"consistency": "9"}), "consistency"},
		{"bad base64", "quicbaas_kv_put", kvArgs("7", map[string]interface{}{"data": "not base64!"}), "data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry, svc := newKVRecordingRegistry(t)
			_, err := registry.ExecuteTool(context.Background(), tc.tool, tc.args)
			var argErr *UserToolInvalidArgumentError
			require.ErrorAs(t, err, &argErr)
			assert.Equal(t, tc.arg, argErr.Arg)
			assert.Empty(t, svc.puts)
			assert.Empty(t, svc.gets)
			assert.Empty(t, svc.deletes)
		})
	}
}

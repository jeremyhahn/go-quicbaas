//go:build mcp

package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// b64 is the standard base64 form of s.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func (s *kvStoreService) CompareAndSwap(_ context.Context, req *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error) {
	key := kvStoreKey(req.EntityType, req.ID)
	held, ok := s.entries[key]
	if !ok || !bytes.Equal(held, req.Expect) {
		return &service.ConditionalPutResponse{Applied: false}, nil
	}
	s.entries[key] = req.Set
	return &service.ConditionalPutResponse{Applied: true}, nil
}

func (s *kvStoreService) PutIfAbsent(_ context.Context, req *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error) {
	key := kvStoreKey(req.EntityType, req.ID)
	if _, ok := s.entries[key]; ok {
		return &service.ConditionalPutResponse{Applied: false}, nil
	}
	s.entries[key] = req.Data
	return &service.ConditionalPutResponse{Applied: true}, nil
}

// TestKVConditionalTools_ShareQRDBPutsKeyspace pins the keyspace: a value
// written by qrdb_put is the one the conditional tools compare and see.
func TestKVConditionalTools_ShareQRDBPutsKeyspace(t *testing.T) {
	registry, store := newKVStoreRegistry(t)
	ctx := context.Background()

	_, err := registry.ExecuteTool(ctx, "qrdb_put", map[string]interface{}{
		"entityType": "locks", "id": float64(7), "data": b64("v1"),
	})
	require.NoError(t, err)

	out, err := registry.ExecuteTool(ctx, "quicbaas_kv_put_if_absent", map[string]interface{}{
		"entityType": "locks", "id": float64(7), "value": b64("other"),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"stored": false}, out)

	out, err = registry.ExecuteTool(ctx, "quicbaas_kv_compare_and_swap", map[string]interface{}{
		"entityType": "locks", "id": float64(7), "expected": b64("stale"), "next": b64("v2"),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"swapped": false}, out)

	out, err = registry.ExecuteTool(ctx, "quicbaas_kv_compare_and_swap", map[string]interface{}{
		"entityType": "locks", "id": float64(7), "expected": b64("v1"), "next": b64("v2"),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"swapped": true}, out)
	assert.Equal(t, []byte("v2"), store.entries[kvStoreKey("locks", 7)])
}

func TestKVConditionalTools_RefuseNonBase64(t *testing.T) {
	registry, _ := newKVStoreRegistry(t)
	_, err := registry.ExecuteTool(context.Background(), "quicbaas_kv_compare_and_swap", map[string]interface{}{
		"entityType": "locks", "id": float64(7), "expected": "%%%", "next": b64("v2"),
	})
	var argErr *UserToolInvalidArgumentError
	require.ErrorAs(t, err, &argErr)
}

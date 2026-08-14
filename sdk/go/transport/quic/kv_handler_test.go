// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsKVService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.KV", true},
		{"quicbaas.Batch", true},
		{"quicbaas.Transaction", true},
		{"quicbaas.IAM.User", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isKVService(tt.svc), tt.svc)
	}
}

// TestConvertKVResponse_AllMethods verifies the map-to-typed conversion the
// QUIC transport applies after the generic JSON decode for KV services.
func TestConvertKVResponse_AllMethods(t *testing.T) {
	t.Run("nil data", func(t *testing.T) {
		got, err := convertKVResponse("Get", nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("Put", func(t *testing.T) {
		got, err := convertKVResponse("Put", map[string]interface{}{
			"id": float64(7), "version": float64(2), "timestamp": float64(100), "leader_id": float64(1),
		})
		require.NoError(t, err)
		resp := got.(*service.PutResponse)
		assert.Equal(t, uint64(7), resp.ID)
		assert.Equal(t, uint64(2), resp.Version)
		assert.Equal(t, int64(100), resp.Timestamp)
		assert.Equal(t, uint64(1), resp.LeaderID)
	})

	t.Run("Get found", func(t *testing.T) {
		got, err := convertKVResponse("Get", map[string]interface{}{
			"id": float64(3), "data": "hello", "version": float64(1), "timestamp": float64(50), "found": true,
		})
		require.NoError(t, err)
		resp := got.(*service.GetResponse)
		assert.Equal(t, uint64(3), resp.ID)
		assert.Equal(t, []byte("hello"), resp.Data)
		assert.True(t, resp.Found)
	})

	t.Run("Delete", func(t *testing.T) {
		got, err := convertKVResponse("Delete", map[string]interface{}{
			"deleted": true, "timestamp": float64(9),
		})
		require.NoError(t, err)
		resp := got.(*service.DeleteResponse)
		assert.True(t, resp.Deleted)
		assert.Equal(t, int64(9), resp.Timestamp)
	})

	t.Run("List", func(t *testing.T) {
		got, err := convertKVResponse("List", map[string]interface{}{
			"entities": []interface{}{
				map[string]interface{}{
					"entity_type": "user", "id": float64(1), "data": "x",
					"version": float64(1), "timestamp": float64(2),
				},
				"not-a-map", // exercises the type-assertion skip branch
			},
			"next_page_token": "tok",
			"total_count":     float64(1),
		})
		require.NoError(t, err)
		resp := got.(*service.ListResponse)
		require.Len(t, resp.Entities, 1)
		assert.Equal(t, "user", resp.Entities[0].EntityType)
		assert.Equal(t, []byte("x"), resp.Entities[0].Data)
		assert.Equal(t, "tok", resp.NextPageToken)
		assert.Equal(t, uint64(1), resp.TotalCount)
	})

	t.Run("ListEntityTypes", func(t *testing.T) {
		got, err := convertKVResponse("ListEntityTypes", map[string]interface{}{
			"entity_types":    []interface{}{"user", "doc", 42}, // 42 exercises non-string skip
			"next_page_token": "n",
			"total_count":     float64(2),
		})
		require.NoError(t, err)
		resp := got.(*service.ListEntityTypesResponse)
		assert.Equal(t, []string{"user", "doc"}, resp.EntityTypes)
		assert.Equal(t, "n", resp.NextPageToken)
		assert.Equal(t, 2, resp.TotalCount)
	})

	t.Run("unsupported method", func(t *testing.T) {
		_, err := convertKVResponse("Bogus", map[string]interface{}{"a": 1})
		require.Error(t, err)
		assert.Equal(t, errors.CodeUnimplemented, errors.Code(err))
	})
}

// TestConvertEntity_EmptyDefaults verifies the empty-map path of convertEntity
// returns zero-value fields with a non-nil empty Data slice.
func TestConvertEntity_EmptyDefaults(t *testing.T) {
	e := convertEntity(map[string]interface{}{})
	assert.Equal(t, []byte{}, e.Data)
	assert.Equal(t, uint64(0), e.ID)
	assert.Empty(t, e.EntityType)
}

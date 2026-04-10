//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// handleUpdateMetadata Tests
// =============================================================================

func TestHandleUpdateMetadata_Success(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace": "test-ns",
		"id":        "vec-1",
		"metadata": map[string]interface{}{
			"category": "science",
			"author":   "test-user",
		},
	}

	resp, err := server.handleUpdateMetadata(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)

	var result map[string]interface{}
	unmarshalErr := json.Unmarshal([]byte(resp.Content[0].Text), &result)
	require.NoError(t, unmarshalErr)
	assert.Equal(t, "vec-1", result["id"])
	assert.Equal(t, "test-ns", result["namespace"])
}

func TestHandleUpdateMetadata_MissingNamespace(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	_, err := server.handleUpdateMetadata(ctx, map[string]interface{}{
		"id":       "vec-1",
		"metadata": map[string]interface{}{"key": "val"},
	})
	require.Error(t, err)
}

func TestHandleUpdateMetadata_MissingID(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	_, err := server.handleUpdateMetadata(ctx, map[string]interface{}{
		"namespace": "test-ns",
		"metadata":  map[string]interface{}{"key": "val"},
	})
	require.Error(t, err)
}

func TestHandleUpdateMetadata_MissingMetadata(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	_, err := server.handleUpdateMetadata(ctx, map[string]interface{}{
		"namespace": "test-ns",
		"id":        "vec-1",
	})
	require.Error(t, err)
}

func TestHandleUpdateMetadata_MetadataAsJSONString(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	params := map[string]interface{}{
		"namespace": "test-ns",
		"id":        "vec-1",
		"metadata":  `{"category": "science"}`,
	}

	resp, err := server.handleUpdateMetadata(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.IsError)
}

func TestHandleUpdateMetadata_InvalidJSONStringMetadata(t *testing.T) {
	server := newTestVectorServer(t)
	ctx := context.Background()

	_, err := server.handleUpdateMetadata(ctx, map[string]interface{}{
		"namespace": "test-ns",
		"id":        "vec-1",
		"metadata":  "not-valid-json",
	})
	require.Error(t, err)
}

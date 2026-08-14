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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestDecodeOrchestratorResponse covers the raw-bytes decode path used by
// doRequest (distinct from convertOrchestratorResponse which takes a map).
func TestDecodeOrchestratorResponse(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		got, err := decodeOrchestratorResponse("Health", nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("typed converter", func(t *testing.T) {
		payload, err := EncodePayload(&orchestrator.SpawnSessionResponse{SessionID: "s1"})
		require.NoError(t, err)
		got, err := decodeOrchestratorResponse("SpawnSession", payload)
		require.NoError(t, err)
		assert.Equal(t, "s1", got.(*orchestrator.SpawnSessionResponse).SessionID)
	})

	t.Run("unknown method falls back to map", func(t *testing.T) {
		got, err := decodeOrchestratorResponse("Bogus", []byte(`{"x":1}`))
		require.NoError(t, err)
		m := got.(map[string]interface{})
		assert.Equal(t, float64(1), m["x"])
	})

	t.Run("unknown method invalid json", func(t *testing.T) {
		_, err := decodeOrchestratorResponse("Bogus", []byte(`{bad`))
		require.Error(t, err)
	})
}

// TestConvertListBackendsResponse_WrappedObject covers the wrapped-object shape
// branch of the backend list converter ({"backends":[...]} rather than a raw
// JSON array).
func TestConvertListBackendsResponse_WrappedObject(t *testing.T) {
	payload, err := EncodePayload(&orchestrator.ListBackendsResponse{
		Backends: []*orchestrator.BackendInfo{{Name: "b1", Type: "ollama", Status: "available", Available: true}},
		Count:    1,
	})
	require.NoError(t, err)

	got, err := convertListBackendsResponse(payload)
	require.NoError(t, err)
	resp := got.(*orchestrator.ListBackendsResponse)
	require.Len(t, resp.Backends, 1)
	assert.Equal(t, "b1", resp.Backends[0].Name)
}

// TestConvertListBackendsResponse_RawArrayInferredStatus covers the raw-array
// branch where status is inferred from the available flag.
func TestConvertListBackendsResponse_RawArrayInferredStatus(t *testing.T) {
	got, err := convertListBackendsResponse([]byte(`[{"name":"b1","type":"t"},{"name":"b2","type":"t","available":false}]`))
	require.NoError(t, err)
	resp := got.(*orchestrator.ListBackendsResponse)
	require.Len(t, resp.Backends, 2)
	assert.Equal(t, "available", resp.Backends[0].Status)
	assert.Equal(t, "unavailable", resp.Backends[1].Status)
}

func TestConvertListBackendsResponse_InvalidJSON(t *testing.T) {
	_, err := convertListBackendsResponse([]byte(`{bad`))
	require.Error(t, err)
}

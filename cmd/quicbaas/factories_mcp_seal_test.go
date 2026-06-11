//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSealInitResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"root_token": "root-token-abc",
			"version":    float64(1),
		}
		resp, err := parseSealInitResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "root-token-abc", resp.RootToken)
		assert.Equal(t, uint64(1), resp.Version)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("StaleShareKeysIgnored", func(t *testing.T) {
		// Legacy servers may still emit shares/recovery_keys; the
		// share-free contract means the parser must ignore them and the
		// response type carries no fields to receive them.
		share1 := base64.StdEncoding.EncodeToString([]byte("share-1"))
		rk := base64.StdEncoding.EncodeToString([]byte("recovery-key"))
		data := map[string]interface{}{
			"shares":        []interface{}{share1},
			"recovery_keys": []interface{}{rk},
			"root_token":    "root-tok",
			"version":       float64(2),
		}
		resp, err := parseSealInitResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "root-tok", resp.RootToken)
		assert.Equal(t, uint64(2), resp.Version)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseSealInitResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.RootToken)
	})

	t.Run("EmptyMap", func(t *testing.T) {
		resp, err := parseSealInitResponse(map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", resp.RootToken)
	})
}

func TestParseUnsealResponse(t *testing.T) {
	t.Run("StillSealed", func(t *testing.T) {
		data := map[string]interface{}{
			"sealed":          true,
			"shares_provided": float64(1),
			"threshold":       float64(3),
		}
		resp, err := parseUnsealResponse(data)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.Equal(t, 1, resp.SharesProvided)
		assert.Equal(t, 3, resp.Threshold)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("Unsealed", func(t *testing.T) {
		data := map[string]interface{}{
			"sealed":          false,
			"shares_provided": float64(3),
			"threshold":       float64(3),
		}
		resp, err := parseUnsealResponse(data)
		require.NoError(t, err)
		assert.False(t, resp.Sealed)
	})

	t.Run("WithError", func(t *testing.T) {
		data := map[string]interface{}{
			"sealed": true,
			"error":  "invalid share",
		}
		resp, err := parseUnsealResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "invalid share", resp.Error)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseUnsealResponse(nil)
		require.NoError(t, err)
		assert.False(t, resp.Sealed)
	})
}

func TestParseSealStatusResponse(t *testing.T) {
	t.Run("FullStatus", func(t *testing.T) {
		data := map[string]interface{}{
			"sealed":          true,
			"initialized":     true,
			"mode":            "shamir",
			"threshold":       float64(3),
			"total_shares":    float64(5),
			"shares_provided": float64(1),
			"tpm_available":   false,
			"cluster_nodes":   float64(3),
			"shares_received": float64(1),
			"version":         float64(2),
		}
		resp, err := parseSealStatusResponse(data)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.True(t, resp.Initialized)
		assert.Equal(t, "shamir", resp.Mode)
		assert.Equal(t, 3, resp.Threshold)
		assert.Equal(t, 5, resp.TotalShares)
		assert.Equal(t, 1, resp.SharesProvided)
		assert.False(t, resp.TPMAvailable)
		assert.Equal(t, 3, resp.ClusterNodes)
		assert.Equal(t, 1, resp.SharesReceived)
		assert.Equal(t, uint64(2), resp.Version)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseSealStatusResponse(nil)
		require.NoError(t, err)
		assert.False(t, resp.Sealed)
		assert.False(t, resp.Initialized)
	})
}

func TestParseRekeyResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		// Stale "shares" keys from legacy servers are ignored under the
		// share-free contract.
		share := base64.StdEncoding.EncodeToString([]byte("new-share"))
		data := map[string]interface{}{
			"shares":  []interface{}{share},
			"version": float64(3),
		}
		resp, err := parseRekeyResponse(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseRekeyResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), resp.Version)
	})
}

// Interface compliance
func TestMCPSealClientInterfaceCompliance(t *testing.T) {
	var _ SealClient = (*mcpSealClient)(nil)
}

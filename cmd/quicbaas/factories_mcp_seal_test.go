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
		share1 := base64.StdEncoding.EncodeToString([]byte("share-1"))
		share2 := base64.StdEncoding.EncodeToString([]byte("share-2"))
		data := map[string]interface{}{
			"shares":     []interface{}{share1, share2},
			"root_token": "root-token-abc",
			"version":    float64(1),
		}
		resp, err := parseSealInitResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "root-token-abc", resp.RootToken)
		assert.Equal(t, uint64(1), resp.Version)
		assert.Len(t, resp.Shares, 2)
		assert.Equal(t, []byte("share-1"), resp.Shares[0])
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("WithRecoveryKeys", func(t *testing.T) {
		rk := base64.StdEncoding.EncodeToString([]byte("recovery-key"))
		data := map[string]interface{}{
			"shares":        []interface{}{},
			"root_token":    "root-tok",
			"version":       float64(2),
			"recovery_keys": []interface{}{rk},
		}
		resp, err := parseSealInitResponse(data)
		require.NoError(t, err)
		assert.Len(t, resp.RecoveryKeys, 1)
		assert.Equal(t, []byte("recovery-key"), resp.RecoveryKeys[0])
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseSealInitResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.RootToken)
		assert.Nil(t, resp.Shares)
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
		share := base64.StdEncoding.EncodeToString([]byte("new-share"))
		data := map[string]interface{}{
			"shares":  []interface{}{share},
			"version": float64(3),
		}
		resp, err := parseRekeyResponse(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version)
		assert.Len(t, resp.Shares, 1)
		assert.Equal(t, []byte("new-share"), resp.Shares[0])
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseRekeyResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), resp.Version)
	})
}

func TestParseBytesSlice(t *testing.T) {
	t.Run("NilMap", func(t *testing.T) {
		result := parseBytesSlice(nil, "shares")
		assert.Nil(t, result)
	})

	t.Run("MissingKey", func(t *testing.T) {
		result := parseBytesSlice(map[string]interface{}{}, "shares")
		assert.Nil(t, result)
	})

	t.Run("NotASlice", func(t *testing.T) {
		m := map[string]interface{}{"shares": "not-a-slice"}
		result := parseBytesSlice(m, "shares")
		assert.Nil(t, result)
	})

	t.Run("Base64Strings", func(t *testing.T) {
		s1 := base64.StdEncoding.EncodeToString([]byte("data-1"))
		s2 := base64.StdEncoding.EncodeToString([]byte("data-2"))
		m := map[string]interface{}{
			"shares": []interface{}{s1, s2},
		}
		result := parseBytesSlice(m, "shares")
		assert.Len(t, result, 2)
		assert.Equal(t, []byte("data-1"), result[0])
		assert.Equal(t, []byte("data-2"), result[1])
	})

	t.Run("InvalidBase64FallsBackToRawString", func(t *testing.T) {
		m := map[string]interface{}{
			"shares": []interface{}{"not-valid-base64!!!"},
		}
		result := parseBytesSlice(m, "shares")
		assert.Len(t, result, 1)
		assert.Equal(t, []byte("not-valid-base64!!!"), result[0])
	})

	t.Run("ByteSliceItems", func(t *testing.T) {
		m := map[string]interface{}{
			"shares": []interface{}{[]byte("raw-bytes")},
		}
		result := parseBytesSlice(m, "shares")
		assert.Len(t, result, 1)
		assert.Equal(t, []byte("raw-bytes"), result[0])
	})

	t.Run("MixedTypes", func(t *testing.T) {
		b64 := base64.StdEncoding.EncodeToString([]byte("encoded"))
		m := map[string]interface{}{
			"shares": []interface{}{b64, []byte("raw"), 42}, // 42 is skipped
		}
		result := parseBytesSlice(m, "shares")
		assert.Len(t, result, 2) // only string and []byte
	})

	t.Run("EmptySlice", func(t *testing.T) {
		m := map[string]interface{}{
			"shares": []interface{}{},
		}
		result := parseBytesSlice(m, "shares")
		assert.Empty(t, result)
	})
}

// Interface compliance
func TestMCPSealClientInterfaceCompliance(t *testing.T) {
	var _ SealClient = (*mcpSealClient)(nil)
}

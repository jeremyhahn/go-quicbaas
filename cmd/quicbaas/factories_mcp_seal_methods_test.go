//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestMCPSealClient_SealInit tests the MCP seal init method.
func TestMCPSealClient_SealInit(t *testing.T) {
	ctx := context.Background()
	share1 := base64.StdEncoding.EncodeToString([]byte("share-1"))
	share2 := base64.StdEncoding.EncodeToString([]byte("share-2"))

	t.Run("Success", func(t *testing.T) {
		// Stale "shares" keys from legacy servers are ignored under the
		// share-free contract.
		respMap := map[string]interface{}{
			"shares":     []interface{}{share1, share2},
			"root_token": "root-tok-123",
			"version":    float64(1),
		}
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{
			Threshold:   3,
			TotalShares: 5,
		})
		require.NoError(t, err)
		assert.Equal(t, "root-tok-123", resp.RootToken)
		assert.Equal(t, uint64(1), resp.Version)
	})

	t.Run("WithRecoveryKeys", func(t *testing.T) {
		// Recovery key material is never returned (share-free contract);
		// stale "recovery_keys" payload keys are ignored.
		rk := base64.StdEncoding.EncodeToString([]byte("recovery-key"))
		respMap := map[string]interface{}{
			"shares":        []interface{}{share1},
			"root_token":    "root-tok",
			"version":       float64(1),
			"recovery_keys": []interface{}{rk},
		}
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{
			Threshold:           3,
			TotalShares:         5,
			RecoveryKeysEnabled: true,
			RecoveryThreshold:   2,
			RecoveryTotalKeys:   3,
		})
		require.NoError(t, err)
		assert.Equal(t, "root-tok", resp.RootToken)
		assert.Equal(t, uint64(1), resp.Version)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{Threshold: 3, TotalShares: 5})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSealClient_Unseal tests the MCP unseal method.
func TestMCPSealClient_Unseal(t *testing.T) {
	ctx := context.Background()

	t.Run("StillSealed", func(t *testing.T) {
		respMap := map[string]interface{}{
			"sealed":          true,
			"shares_provided": float64(1),
			"threshold":       float64(3),
		}
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{
			Share: []byte("my-share"),
		})
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.Equal(t, 1, resp.SharesProvided)
		assert.Equal(t, 3, resp.Threshold)
	})

	t.Run("Unsealed", func(t *testing.T) {
		respMap := map[string]interface{}{
			"sealed":          false,
			"shares_provided": float64(3),
			"threshold":       float64(3),
		}
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{
			Share: []byte("final-share"),
		})
		require.NoError(t, err)
		assert.False(t, resp.Sealed)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{Share: []byte("s")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSealClient_Seal tests the MCP seal method.
func TestMCPSealClient_Seal(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(map[string]interface{}{}, nil)}
		err := c.Seal(ctx)
		assert.NoError(t, err)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		err := c.Seal(ctx)
		assert.Error(t, err)
	})
}

// TestMCPSealClient_SealStatus tests the MCP seal status method.
func TestMCPSealClient_SealStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
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
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.SealStatus(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.True(t, resp.Initialized)
		assert.Equal(t, "shamir", resp.Mode)
		assert.Equal(t, 3, resp.Threshold)
		assert.Equal(t, 5, resp.TotalShares)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.SealStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSealClient_Rekey tests the MCP rekey method.
func TestMCPSealClient_Rekey(t *testing.T) {
	ctx := context.Background()
	share := base64.StdEncoding.EncodeToString([]byte("new-share"))

	t.Run("Success", func(t *testing.T) {
		// Stale "shares" keys from legacy servers are ignored under the
		// share-free contract.
		respMap := map[string]interface{}{
			"shares":  []interface{}{share},
			"version": float64(3),
		}
		c := &mcpSealClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.Rekey(ctx, &service.RekeyRequest{
			NewThreshold:   2,
			NewTotalShares: 3,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.Rekey(ctx, &service.RekeyRequest{NewThreshold: 2, NewTotalShares: 3})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSealClient_Close tests the MCP seal client close method.
func TestMCPSealClient_Close(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, nil)}
		err := c.Close()
		assert.NoError(t, err)
	})
}

// TestMCPSealClient_CallTool tests the internal callTool method.
func TestMCPSealClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"ok": true}
		c := &mcpSealClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestCreateMCPSealClient tests the factory function.
func TestCreateMCPSealClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createMCPSealClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createMCPSealClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

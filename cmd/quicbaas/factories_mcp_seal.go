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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"encoding/base64"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// mcpSealClient implements the SealClient interface using MCP transport.
type mcpSealClient struct {
	sdk quicbaas.Client
}

// createMCPSealClient creates an MCP client for seal operations.
func createMCPSealClient() (SealClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpSealClient{sdk: sdk}, nil
}

// SealInit initializes the seal and generates key shares.
func (c *mcpSealClient) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	args := map[string]interface{}{
		"threshold":    req.Threshold,
		"total_shares": req.TotalShares,
	}
	if req.RecoveryKeysEnabled {
		args["recovery_keys_enabled"] = req.RecoveryKeysEnabled
	}
	if req.RecoveryThreshold > 0 {
		args["recovery_threshold"] = req.RecoveryThreshold
	}
	if req.RecoveryTotalKeys > 0 {
		args["recovery_total_keys"] = req.RecoveryTotalKeys
	}

	result, err := c.callTool(ctx, "quicbaas_seal_init", args)
	if err != nil {
		return nil, err
	}

	return parseSealInitResponse(result)
}

// Unseal provides a key share to unseal the database.
func (c *mcpSealClient) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	args := map[string]interface{}{
		"share": base64.StdEncoding.EncodeToString(req.Share),
	}

	result, err := c.callTool(ctx, "quicbaas_unseal", args)
	if err != nil {
		return nil, err
	}

	return parseUnsealResponse(result)
}

// Seal re-seals the database.
func (c *mcpSealClient) Seal(ctx context.Context) error {
	_, err := c.callTool(ctx, "quicbaas_seal", nil)
	return err
}

// SealStatus returns the current seal status.
func (c *mcpSealClient) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	result, err := c.callTool(ctx, "quicbaas_seal_status", nil)
	if err != nil {
		return nil, err
	}

	return parseSealStatusResponse(result)
}

// Rekey generates new key shares with a new threshold configuration.
func (c *mcpSealClient) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	args := map[string]interface{}{
		"new_threshold":    req.NewThreshold,
		"new_total_shares": req.NewTotalShares,
	}

	result, err := c.callTool(ctx, "quicbaas_seal_rekey", args)
	if err != nil {
		return nil, err
	}

	return parseRekeyResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpSealClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpSealClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// Response Parsing Functions
// =============================================================================

// parseSealInitResponse parses the MCP response into a SealInitResponse.
func parseSealInitResponse(result interface{}) (*service.SealInitResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SealInitResponse{
		Shares:    parseBytesSlice(data, "shares"),
		RootToken: getString(data, "root_token"),
		Version:   uint64(getInt64(data, "version")),
		Timestamp: time.Now(),
	}

	if recoveryKeys := parseBytesSlice(data, "recovery_keys"); len(recoveryKeys) > 0 {
		resp.RecoveryKeys = recoveryKeys
	}

	return resp, nil
}

// parseUnsealResponse parses the MCP response into an UnsealResponse.
func parseUnsealResponse(result interface{}) (*service.UnsealResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.UnsealResponse{
		Sealed:         getBool(data, "sealed"),
		SharesProvided: int(getInt64(data, "shares_provided")),
		Threshold:      int(getInt64(data, "threshold")),
		Error:          getString(data, "error"),
		Timestamp:      time.Now(),
	}
	return resp, nil
}

// parseSealStatusResponse parses the MCP response into a SealStatusResponse.
func parseSealStatusResponse(result interface{}) (*service.SealStatusResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SealStatusResponse{
		Sealed:         getBool(data, "sealed"),
		Initialized:    getBool(data, "initialized"),
		Mode:           getString(data, "mode"),
		Threshold:      int(getInt64(data, "threshold")),
		TotalShares:    int(getInt64(data, "total_shares")),
		SharesProvided: int(getInt64(data, "shares_provided")),
		TPMAvailable:   getBool(data, "tpm_available"),
		ClusterNodes:   int(getInt64(data, "cluster_nodes")),
		SharesReceived: int(getInt64(data, "shares_received")),
		Version:        uint64(getInt64(data, "version")),
		Timestamp:      time.Now(),
	}
	return resp, nil
}

// parseRekeyResponse parses the MCP response into a RekeyResponse.
func parseRekeyResponse(result interface{}) (*service.RekeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.RekeyResponse{
		Shares:    parseBytesSlice(data, "shares"),
		Version:   uint64(getInt64(data, "version")),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseBytesSlice extracts a slice of byte slices from a map.
// Handles both base64-encoded strings and raw byte arrays.
func parseBytesSlice(m map[string]interface{}, key string) [][]byte {
	if m == nil {
		return nil
	}

	raw, ok := m[key]
	if !ok {
		return nil
	}

	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	result := make([][]byte, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			decoded, decodeErr := base64.StdEncoding.DecodeString(v)
			if decodeErr != nil {
				result = append(result, []byte(v))
			} else {
				result = append(result, decoded)
			}
		case []byte:
			result = append(result, v)
		}
	}
	return result
}

// Compile-time interface check.
var _ SealClient = (*mcpSealClient)(nil)

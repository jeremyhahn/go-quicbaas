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
	"encoding/base64"
	"encoding/hex"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerObjectTransferTools registers all object transfer MCP tools.
func (r *ToolRegistry) registerObjectTransferTools() {
	// Single Object Fetch
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_transfer_fetch",
		Description: "Fetch a single object from a peer node's local object store. Used for lazy snapshot recovery where recovering nodes fetch objects marked as pending after metadata-only snapshot restoration.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shard_id": map[string]interface{}{
					"type":        "integer",
					"description": "Shard ID that owns the object",
				},
				"object_ref": map[string]interface{}{
					"type":        "string",
					"description": "Storage path/key in the peer's object store",
				},
				"checksum": map[string]interface{}{
					"type":        "string",
					"description": "Expected SHA-256 checksum for verification (base64 encoded, optional)",
				},
				"request_id": map[string]interface{}{
					"type":        "string",
					"description": "Request ID for tracing and deduplication (optional)",
				},
			},
			"required": []string{"shard_id", "object_ref"},
		},
	})

	// Stream Fetch Multiple Objects
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_transfer_stream_fetch",
		Description: "Fetch multiple objects from a peer node in a single operation. More efficient for bulk recovery after snapshot restoration. Returns an array of fetched objects.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shard_id": map[string]interface{}{
					"type":        "integer",
					"description": "Shard ID that owns the objects",
				},
				"items": map[string]interface{}{
					"type":        "array",
					"description": "Array of objects to fetch",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"object_ref": map[string]interface{}{
								"type":        "string",
								"description": "Storage path/key for the object",
							},
							"checksum": map[string]interface{}{
								"type":        "string",
								"description": "Expected SHA-256 checksum for verification (base64 encoded, optional)",
							},
						},
						"required": []string{"object_ref"},
					},
				},
			},
			"required": []string{"shard_id", "items"},
		},
	})
}

// =============================================================================
// Object Transfer Service Accessor
// =============================================================================

// getObjectTransferService retrieves the ObjectTransferService from the service.
func (r *ToolRegistry) getObjectTransferService() (service.ObjectTransferService, error) {
	svc := r.service.ObjectTransferService()
	if svc == nil {
		return nil, api.NewServiceUnavailableError("object transfer")
	}
	return svc, nil
}

// =============================================================================
// Object Transfer Operations
// =============================================================================

// executeObjectTransferFetch handles the quicbaas_object_transfer_fetch tool.
func (r *ToolRegistry) executeObjectTransferFetch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectTransferService()
	if err != nil {
		return nil, err
	}

	// Validate required fields
	shardID := getUint64(args, "shard_id")
	if shardID == 0 {
		return nil, api.NewRequiredFieldError("shard_id")
	}

	objectRef := getString(args, "object_ref")
	if objectRef == "" {
		return nil, api.NewRequiredFieldError("object_ref")
	}

	// Build request
	req := &service.FetchObjectRequest{
		ShardID:   shardID,
		ObjectRef: objectRef,
		RequestID: getString(args, "request_id"),
	}

	// Decode checksum if provided (base64)
	if checksumStr := getString(args, "checksum"); checksumStr != "" {
		checksum, decErr := base64.StdEncoding.DecodeString(checksumStr)
		if decErr != nil {
			return nil, api.NewInvalidBase64Error("checksum", decErr)
		}
		req.Checksum = checksum
	}

	// Execute fetch
	resp, err := svc.FetchObject(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build response
	result := map[string]interface{}{
		"object_ref": resp.ObjectRef,
		"found":      resp.Found,
		"timestamp":  time.Now().Unix(),
	}

	if resp.Found {
		result["data"] = base64.StdEncoding.EncodeToString(resp.Data)
		result["checksum"] = hex.EncodeToString(resp.Checksum)
		result["size"] = resp.Size
		result["compressed"] = resp.Compressed
		if resp.Compressed {
			result["compression_algo"] = resp.CompressionAlgo
		}
	}

	if resp.ErrorMessage != "" {
		result["error_message"] = resp.ErrorMessage
	}

	return marshalResult(result)
}

// executeObjectTransferStreamFetch handles the quicbaas_object_transfer_stream_fetch tool.
func (r *ToolRegistry) executeObjectTransferStreamFetch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectTransferService()
	if err != nil {
		return nil, err
	}

	// Validate required fields
	shardID := getUint64(args, "shard_id")
	if shardID == 0 {
		return nil, api.NewRequiredFieldError("shard_id")
	}

	itemsRaw, ok := args["items"].([]interface{})
	if !ok || len(itemsRaw) == 0 {
		return nil, api.NewEmptyCollectionError("items")
	}

	// Parse items
	items := make([]service.ObjectFetchItem, 0, len(itemsRaw))
	for i, itemRaw := range itemsRaw {
		itemMap, ok := itemRaw.(map[string]interface{})
		if !ok {
			return nil, api.NewBatchItemValidationError(i, "is not a valid object")
		}

		objectRef := getStringFromMap(itemMap, "object_ref")
		if objectRef == "" {
			return nil, api.NewBatchItemValidationError(i, "object_ref is required")
		}

		item := service.ObjectFetchItem{
			ObjectRef: objectRef,
		}

		// Decode checksum if provided (base64)
		if checksumStr := getStringFromMap(itemMap, "checksum"); checksumStr != "" {
			checksum, decErr := base64.StdEncoding.DecodeString(checksumStr)
			if decErr != nil {
				return nil, api.NewBatchItemError(i, "checksum encoding", decErr)
			}
			item.Checksum = checksum
		}

		items = append(items, item)
	}

	// Build request
	req := &service.StreamFetchObjectsRequest{
		ShardID: shardID,
		Items:   items,
	}

	// Execute stream fetch
	resp, err := svc.StreamFetchObjects(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build response objects array
	objects := make([]map[string]interface{}, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		objResult := map[string]interface{}{
			"object_ref": obj.ObjectRef,
			"found":      obj.Found,
		}

		if obj.Found {
			objResult["data"] = base64.StdEncoding.EncodeToString(obj.Data)
			objResult["checksum"] = hex.EncodeToString(obj.Checksum)
			objResult["size"] = obj.Size
			objResult["compressed"] = obj.Compressed
			if obj.Compressed {
				objResult["compression_algo"] = obj.CompressionAlgo
			}
		}

		if obj.ErrorMessage != "" {
			objResult["error_message"] = obj.ErrorMessage
		}

		objects = append(objects, objResult)
	}

	return marshalResult(map[string]interface{}{
		"objects":       objects,
		"success_count": resp.SuccessCount,
		"failure_count": resp.FailureCount,
		"total_count":   resp.TotalCount,
		"timestamp":     time.Now().Unix(),
	})
}

// isObjectTransferTool checks if the tool name is an object transfer tool.
func isObjectTransferTool(name string) bool {
	switch name {
	case "quicbaas_object_transfer_fetch",
		"quicbaas_object_transfer_stream_fetch":
		return true
	default:
		return false
	}
}

// executeObjectTransferTool routes object transfer tool calls to appropriate handlers.
func (r *ToolRegistry) executeObjectTransferTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_object_transfer_fetch":
		return r.executeObjectTransferFetch(ctx, args)
	case "quicbaas_object_transfer_stream_fetch":
		return r.executeObjectTransferStreamFetch(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

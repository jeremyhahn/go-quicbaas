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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// KV service prefixes for routing.
const (
	kvServicePrefix          = "quicbaas.KV"
	batchServicePrefix       = "quicbaas.Batch"
	transactionServicePrefix = "quicbaas.Transaction"
)

// isKVService checks if the service name is a KV/Batch/Transaction service.
func isKVService(svc string) bool {
	return strings.HasPrefix(svc, kvServicePrefix) ||
		strings.HasPrefix(svc, batchServicePrefix) ||
		strings.HasPrefix(svc, transactionServicePrefix)
}

// convertKVResponse converts a map response to the appropriate SDK type based on the method.
func convertKVResponse(method string, data map[string]interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}

	switch method {
	case "Put":
		return convertPutResponse(data), nil
	case "Get":
		return convertGetResponse(data), nil
	case "Delete":
		return convertDeleteResponse(data), nil
	case "List":
		return convertListResponse(data), nil
	case "ListEntityTypes":
		return convertListEntityTypesResponse(data), nil
	default:
		return nil, errors.NewUnimplemented("quic.convertKVResponse",
			"unsupported method: "+method)
	}
}

// convertPutResponse converts a map to PutResponse.
func convertPutResponse(data map[string]interface{}) *service.PutResponse {
	resp := &service.PutResponse{}

	if v, ok := data["id"].(float64); ok {
		resp.ID = uint64(v)
	}
	if v, ok := data["version"].(float64); ok {
		resp.Version = uint64(v)
	}
	if v, ok := data["timestamp"].(float64); ok {
		resp.Timestamp = int64(v)
	}
	if v, ok := data["leader_id"].(float64); ok {
		resp.LeaderID = uint64(v)
	}

	return resp
}

// convertGetResponse converts a map to GetResponse.
func convertGetResponse(data map[string]interface{}) *service.GetResponse {
	resp := &service.GetResponse{
		// Initialize Data with empty slice for consistent behavior.
		// This ensures that empty values are returned as []byte{}, not nil.
		Data: []byte{},
	}

	if v, ok := data["id"].(float64); ok {
		resp.ID = uint64(v)
	}
	if v, ok := data["data"].(string); ok {
		resp.Data = []byte(v)
	}
	if v, ok := data["version"].(float64); ok {
		resp.Version = uint64(v)
	}
	if v, ok := data["timestamp"].(float64); ok {
		resp.Timestamp = int64(v)
	}
	if v, ok := data["found"].(bool); ok {
		resp.Found = v
	}

	return resp
}

// convertDeleteResponse converts a map to DeleteResponse.
func convertDeleteResponse(data map[string]interface{}) *service.DeleteResponse {
	resp := &service.DeleteResponse{}

	if v, ok := data["deleted"].(bool); ok {
		resp.Deleted = v
	}
	if v, ok := data["timestamp"].(float64); ok {
		resp.Timestamp = int64(v)
	}

	return resp
}

// convertListResponse converts a map to ListResponse.
func convertListResponse(data map[string]interface{}) *service.ListResponse {
	resp := &service.ListResponse{
		Entities: []*service.Entity{},
	}

	if v, ok := data["entities"].([]interface{}); ok {
		for _, e := range v {
			if entityMap, ok := e.(map[string]interface{}); ok {
				resp.Entities = append(resp.Entities, convertEntity(entityMap))
			}
		}
	}
	if v, ok := data["next_page_token"].(string); ok {
		resp.NextPageToken = v
	}
	if v, ok := data["total_count"].(float64); ok {
		resp.TotalCount = uint64(v)
	}

	return resp
}

// convertEntity converts a map to Entity.
func convertEntity(data map[string]interface{}) *service.Entity {
	entity := &service.Entity{
		// Initialize Data with empty slice for consistent behavior.
		// This ensures that empty values are returned as []byte{}, not nil.
		Data: []byte{},
	}

	if v, ok := data["entity_type"].(string); ok {
		entity.EntityType = v
	}
	if v, ok := data["id"].(float64); ok {
		entity.ID = uint64(v)
	}
	if v, ok := data["data"].(string); ok {
		entity.Data = []byte(v)
	}
	if v, ok := data["version"].(float64); ok {
		entity.Version = uint64(v)
	}
	if v, ok := data["timestamp"].(float64); ok {
		entity.Timestamp = int64(v)
	}

	return entity
}

// convertListEntityTypesResponse converts a map to ListEntityTypesResponse.
func convertListEntityTypesResponse(data map[string]interface{}) *service.ListEntityTypesResponse {
	resp := &service.ListEntityTypesResponse{
		EntityTypes: []string{},
	}

	if v, ok := data["entity_types"].([]interface{}); ok {
		for _, t := range v {
			if str, ok := t.(string); ok {
				resp.EntityTypes = append(resp.EntityTypes, str)
			}
		}
	}
	if v, ok := data["next_page_token"].(string); ok {
		resp.NextPageToken = v
	}
	if v, ok := data["total_count"].(float64); ok {
		resp.TotalCount = int(v)
	}

	return resp
}

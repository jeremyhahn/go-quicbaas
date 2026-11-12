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
	stderrors "errors"
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The quicbaas_kv_* tools reach the same KV service REST and gRPC call, so
// the service builds the canonical key from the caller's tenant
// ("users:7", "__t:<tenant>:users:7"), routes to the requested shard and
// keeps its cache coherent. Every uint64 crosses the wire as a decimal
// string: a JSON number loses precision above 2^53.

// kvPutResult is the answer of quicbaas_kv_put.
type kvPutResult struct {
	ID        uint64 `json:"id,string"`
	Version   uint64 `json:"version,string"`
	Timestamp int64  `json:"timestamp"`
	LeaderID  uint64 `json:"leader_id,string"`
}

// kvGetResult is the answer of quicbaas_kv_get. Data is standard base64
// (encoding/json encodes a byte slice that way).
type kvGetResult struct {
	ID        uint64 `json:"id,string"`
	Found     bool   `json:"found"`
	Data      []byte `json:"data,omitempty"`
	Version   uint64 `json:"version,string"`
	Timestamp int64  `json:"timestamp"`
}

// kvDeleteResult is the answer of quicbaas_kv_delete.
type kvDeleteResult struct {
	Deleted   bool  `json:"deleted"`
	Timestamp int64 `json:"timestamp"`
}

// kvTarget is the entity a KV tool call addresses.
type kvTarget struct {
	entityType  string
	id          uint64
	shardID     uint64
	consistency service.ConsistencyLevel
}

func kvToolSchema(withData, withTimeout bool) map[string]interface{} {
	properties := map[string]interface{}{
		"entity_type": map[string]interface{}{"type": "string", "description": "Entity type"},
		"id":          map[string]interface{}{"type": "string", "description": "Entity ID as a decimal string"},
		"shard_id":    map[string]interface{}{"type": "string", "description": "Target shard ID as a decimal string (default 0: the service routes)"},
		"consistency": map[string]interface{}{"type": "string", "description": "Consistency level as a decimal string: 0 default, 1 eventual, 2 linearizable, 3 strong"},
	}
	required := []string{"entity_type", "id"}
	if withData {
		properties["data"] = map[string]interface{}{"type": "string", "description": "Base64-encoded (standard) data"}
		required = append(required, "data")
	}
	if withTimeout {
		properties["timeout_ms"] = map[string]interface{}{"type": "string", "description": "Read timeout in milliseconds; 0 or absent uses the default"}
	}
	return map[string]interface{}{"type": "object", "properties": properties, "required": required}
}

// registerKVTools adds quicbaas_kv_put, quicbaas_kv_get and quicbaas_kv_delete.
func (r *ToolRegistry) registerKVTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kv_put",
		Description: "Store an entity through the QuicBaaS KV service",
		InputSchema: kvToolSchema(true, false),
	})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kv_get",
		Description: "Read an entity through the QuicBaaS KV service; a missing entity answers found:false",
		InputSchema: kvToolSchema(false, true),
	})
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kv_delete",
		Description: "Delete an entity through the QuicBaaS KV service",
		InputSchema: kvToolSchema(false, false),
	})
}

// parseKVTarget reads the arguments every KV tool takes.
func parseKVTarget(args map[string]interface{}) (*kvTarget, error) {
	entityType := getString(args, "entity_type")
	if entityType == "" {
		return nil, &UserToolInvalidArgumentError{Arg: "entity_type"}
	}
	id, err := parseDecimalArg(args, "id", true)
	if err != nil {
		return nil, err
	}
	shardID, err := parseDecimalArg(args, "shard_id", false)
	if err != nil {
		return nil, err
	}
	consistency, err := parseDecimalArg(args, "consistency", false)
	if err != nil {
		return nil, err
	}
	if consistency > uint64(service.ConsistencyLevelStrong) {
		return nil, &UserToolInvalidArgumentError{Arg: "consistency"}
	}
	return &kvTarget{
		entityType:  entityType,
		id:          id,
		shardID:     shardID,
		consistency: service.ConsistencyLevel(consistency),
	}, nil
}

// parseDecimalArg parses a uint64 argument sent as a decimal string. An
// optional argument that is absent or empty is 0.
func parseDecimalArg(args map[string]interface{}, key string, required bool) (uint64, error) {
	raw, present := args[key]
	if !present || raw == "" {
		if required {
			return 0, &UserToolInvalidArgumentError{Arg: key}
		}
		return 0, nil
	}
	text, ok := raw.(string)
	if !ok {
		return 0, &UserToolInvalidArgumentError{Arg: key}
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, &UserToolInvalidArgumentError{Arg: key, Err: err}
	}
	return value, nil
}

func (r *ToolRegistry) executeKVPut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target, err := parseKVTarget(args)
	if err != nil {
		return nil, err
	}
	data, err := decodePutData(getString(args, "data"))
	if err != nil {
		return nil, err
	}
	resp, err := r.service.Put(ctx, &service.PutRequest{
		EntityType:  target.entityType,
		ID:          target.id,
		Data:        data,
		Consistency: target.consistency,
		ShardID:     target.shardID,
	})
	if err != nil {
		return nil, err
	}
	return &kvPutResult{ID: resp.ID, Version: resp.Version, Timestamp: resp.Timestamp, LeaderID: resp.LeaderID}, nil
}

func (r *ToolRegistry) executeKVGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target, err := parseKVTarget(args)
	if err != nil {
		return nil, err
	}
	timeoutMs, err := parseDecimalArg(args, "timeout_ms", false)
	if err != nil {
		return nil, err
	}
	resp, err := r.service.Get(ctx, &service.GetRequest{
		EntityType:  target.entityType,
		ID:          target.id,
		Consistency: target.consistency,
		ShardID:     target.shardID,
		TimeoutMs:   timeoutMs,
	})
	if isKVNotFound(err) {
		return &kvGetResult{ID: target.id, Found: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return &kvGetResult{
		ID:        target.id,
		Found:     resp.Found,
		Data:      resp.Data,
		Version:   resp.Version,
		Timestamp: resp.Timestamp,
	}, nil
}

func (r *ToolRegistry) executeKVDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target, err := parseKVTarget(args)
	if err != nil {
		return nil, err
	}
	resp, err := r.service.Delete(ctx, &service.DeleteRequest{
		EntityType:  target.entityType,
		ID:          target.id,
		Consistency: target.consistency,
		ShardID:     target.shardID,
	})
	if err != nil {
		return nil, err
	}
	return &kvDeleteResult{Deleted: resp.Deleted, Timestamp: resp.Timestamp}, nil
}

// isKVNotFound reports whether err is the service's entity-not-found error.
func isKVNotFound(err error) bool {
	var notFound *api.NotFoundError
	return qberrors.IsNotFound(err) || stderrors.As(err, &notFound)
}

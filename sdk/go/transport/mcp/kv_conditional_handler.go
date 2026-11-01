// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// kvConditionalVerb describes how one KV conditional write reaches the
// embedded qrdb node's guarded MCP tool: the tool's arguments and the name of
// the boolean field its answer carries.
type kvConditionalVerb struct {
	arguments func(req interface{}) (map[string]interface{}, error)
	field     string
}

// kvConditionalVerbs maps each SDK KV conditional write to qrdb's guarded
// tool. qrdb decides the precondition inside its state machine, so racing
// callers cannot both win. The tool names live in ToolName.
var kvConditionalVerbs = map[string]kvConditionalVerb{
	"CompareAndSwap": {arguments: compareAndSwapArguments, field: "swapped"},
	"PutIfAbsent":    {arguments: putIfAbsentArguments, field: "stored"},
}

// isMCPKVService reports whether service names the KV service, with or
// without the quicbaas. package prefix.
func isMCPKVService(service string) bool {
	return strings.TrimPrefix(service, "quicbaas.") == "KV"
}

// kvKey builds the qrdb key for an entity: {entityType}:{id}. qrdb scopes
// it to the caller's tenant, as the embedded transport's keyspace does.
func kvKey(entityType string, id uint64) string {
	return entityType + ":" + strconv.FormatUint(id, 10)
}

// compareAndSwapArguments builds qrdb_compare_and_swap's arguments. Values
// travel base64-encoded (RFC 4648), as qrdb's tool requires.
func compareAndSwapArguments(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*service.CompareAndSwapRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.kv.CompareAndSwap",
			"invalid request type, expected *service.CompareAndSwapRequest")
	}
	if r.EntityType == "" {
		return nil, errors.NewInvalidInput("mcp.kv.CompareAndSwap", "entity type is required")
	}
	return map[string]interface{}{
		"key":      kvKey(r.EntityType, r.ID),
		"expected": base64.StdEncoding.EncodeToString(r.Expect),
		"next":     base64.StdEncoding.EncodeToString(r.Set),
	}, nil
}

// putIfAbsentArguments builds qrdb_put_if_absent's arguments.
func putIfAbsentArguments(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*service.PutIfAbsentRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.kv.PutIfAbsent",
			"invalid request type, expected *service.PutIfAbsentRequest")
	}
	if r.EntityType == "" {
		return nil, errors.NewInvalidInput("mcp.kv.PutIfAbsent", "entity type is required")
	}
	return map[string]interface{}{
		"key":   kvKey(r.EntityType, r.ID),
		"value": base64.StdEncoding.EncodeToString(r.Data),
	}, nil
}

// requestKVConditional calls the guarded tool for a KV conditional write and
// returns its decision as a ConditionalPutResponse. A precondition that did
// not hold is a successful call carrying applied=false, not an error.
func (t *Transport) requestKVConditional(ctx context.Context, toolName string, verb kvConditionalVerb, req interface{}) (interface{}, error) {
	arguments, err := verb.arguments(req)
	if err != nil {
		return nil, err
	}
	result, err := t.callTool(ctx, toolName, arguments)
	if err != nil {
		return nil, err
	}
	var outcome map[string]bool
	if err := remarshal(result, &outcome); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.kv."+toolName,
			"failed to decode tool result")
	}
	applied, ok := outcome[verb.field]
	if !ok {
		return nil, errors.New(errors.CodeInternal, "mcp.kv."+toolName,
			"tool result has no "+verb.field+" field")
	}
	return &service.ConditionalPutResponse{Applied: applied}, nil
}

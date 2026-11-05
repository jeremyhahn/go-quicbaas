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
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// In a composed deployment the qrdb_put, qrdb_get and qrdb_delete names are
// served by go-qrdb's native tools, which take a raw string "key": quicbaas's
// keyspace never sees the entity type and ID, so no canonical key is built.
// The SDK's typed KV Put, Get and Delete therefore call quicbaas's
// service-backed batch tools with a single operation. The service builds the
// canonical key from entityType and id, the same path REST and gRPC take, and
// the tool answers with the service's JSON batch response.

// kvVerb maps one KV method onto its batch tool, arguments and answer. args
// reports false when req is not the method's typed SDK request.
type kvVerb struct {
	tool   string
	args   func(req interface{}) (map[string]interface{}, bool)
	result func(text string) (interface{}, error)
}

var kvVerbs = map[string]kvVerb{
	"Put":    {tool: "quicbaas_batch_put", args: kvPutArgs, result: kvPutResult},
	"Get":    {tool: "quicbaas_batch_get", args: kvGetArgs, result: kvGetResult},
	"Delete": {tool: "quicbaas_batch_delete", args: kvDeleteArgs, result: kvDeleteResult},
}

// isMCPKVService reports whether service names the KV service.
func isMCPKVService(service string) bool {
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		service = service[idx+1:]
	}
	return service == "KV"
}

// requestKV calls a KV batch tool and decodes its JSON answer.
func (t *Transport) requestKV(ctx context.Context, verb kvVerb, args map[string]interface{}) (interface{}, error) {
	result, err := t.callToolResult(ctx, verb.tool, args)
	if err != nil {
		return nil, err
	}
	return verb.result(firstText(result.Content))
}

// kvOperation is one batch operation addressing an entity by type and ID.
func kvOperation(entityType string, id uint64) map[string]interface{} {
	return map[string]interface{}{"entityType": entityType, "id": id}
}

// kvBatchArgs wraps a single operation in the batch tools' arguments.
func kvBatchArgs(op map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"operations": []interface{}{op}}
}

// decodeKVAnswer decodes a batch tool's JSON answer into resp.
func decodeKVAnswer(text, op string, resp interface{}) error {
	if err := json.Unmarshal([]byte(text), resp); err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "KV tool answered a result that is not the service batch response")
	}
	return nil
}

// singleKVResponse returns the one response a single-operation batch answers.
func singleKVResponse[T any](op string, responses []*T) (*T, error) {
	if len(responses) != 1 || responses[0] == nil {
		return nil, errors.New(errors.CodeInternal, op, "KV tool answered a batch that does not hold exactly one response")
	}
	return responses[0], nil
}

func kvPutArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.PutRequest)
	if !ok || r == nil {
		return nil, false
	}
	op := kvOperation(r.EntityType, r.ID)
	op["data"] = base64.StdEncoding.EncodeToString(r.Data)
	return kvBatchArgs(op), true
}

func kvPutResult(text string) (interface{}, error) {
	var batch service.BatchPutResponse
	if err := decodeKVAnswer(text, "mcp.kv.Put", &batch); err != nil {
		return nil, err
	}
	return singleKVResponse("mcp.kv.Put", batch.Responses)
}

func kvGetArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.GetRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvBatchArgs(kvOperation(r.EntityType, r.ID)), true
}

func kvGetResult(text string) (interface{}, error) {
	var batch service.BatchGetResponse
	if err := decodeKVAnswer(text, "mcp.kv.Get", &batch); err != nil {
		return nil, err
	}
	return singleKVResponse("mcp.kv.Get", batch.Responses)
}

func kvDeleteArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.DeleteRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvBatchArgs(kvOperation(r.EntityType, r.ID)), true
}

func kvDeleteResult(text string) (interface{}, error) {
	var batch service.BatchDeleteResponse
	if err := decodeKVAnswer(text, "mcp.kv.Delete", &batch); err != nil {
		return nil, err
	}
	return singleKVResponse("mcp.kv.Delete", batch.Responses)
}

// firstText returns the first non-empty text content of a tool result.
func firstText(content []mcp.Content) string {
	for _, item := range content {
		if text, ok := mcp.AsTextContent(item); ok && text.Text != "" {
			return text.Text
		}
	}
	return ""
}

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
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// The SDK's typed KV requests go to quicbaas's quicbaas_kv_put, quicbaas_kv_get
// and quicbaas_kv_delete tools, which call the same KV service REST and gRPC
// call: the service builds the canonical key from the caller's tenant and
// routes to the requested shard. Every uint64 crosses the wire as a decimal
// string so IDs above 2^53 stay exact.

// kvVerb maps one KV method onto its tool, arguments and answer. args reports
// false when req is not the method's typed SDK request.
type kvVerb struct {
	tool   string
	args   func(req interface{}) (map[string]interface{}, bool)
	result func(text string) (interface{}, error)
}

var kvVerbs = map[string]kvVerb{
	"Put":    {tool: "quicbaas_kv_put", args: kvPutArgs, result: kvPutResult},
	"Get":    {tool: "quicbaas_kv_get", args: kvGetArgs, result: kvGetResult},
	"Delete": {tool: "quicbaas_kv_delete", args: kvDeleteArgs, result: kvDeleteResult},
}

// isMCPKVService reports whether service names the KV service.
func isMCPKVService(service string) bool {
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		service = service[idx+1:]
	}
	return service == "KV"
}

// requestKV calls a KV tool and decodes its JSON answer. A tool error comes
// back as an error, never as a miss.
func (t *Transport) requestKV(ctx context.Context, verb kvVerb, args map[string]interface{}) (interface{}, error) {
	result, err := t.callToolResult(ctx, verb.tool, args)
	if err != nil {
		return nil, err
	}
	return verb.result(firstText(result.Content))
}

// kvArgs is the arguments every KV tool takes.
func kvArgs(entityType string, id, shardID uint64, consistency service.ConsistencyLevel) map[string]interface{} {
	return map[string]interface{}{
		"entity_type": entityType,
		"id":          strconv.FormatUint(id, 10),
		"shard_id":    strconv.FormatUint(shardID, 10),
		"consistency": strconv.Itoa(int(consistency)),
	}
}

// decodeKVAnswer decodes a KV tool's JSON answer into out.
func decodeKVAnswer(tool, text string, out interface{}) error {
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "mcp.kv", tool+" answered a result that is not its JSON shape")
	}
	return nil
}

func kvPutArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.PutRequest)
	if !ok || r == nil {
		return nil, false
	}
	args := kvArgs(r.EntityType, r.ID, r.ShardID, r.Consistency)
	args["data"] = base64.StdEncoding.EncodeToString(r.Data)
	return args, true
}

func kvPutResult(text string) (interface{}, error) {
	var answer struct {
		ID        uint64 `json:"id,string"`
		Version   uint64 `json:"version,string"`
		Timestamp int64  `json:"timestamp"`
		LeaderID  uint64 `json:"leader_id,string"`
	}
	if err := decodeKVAnswer("quicbaas_kv_put", text, &answer); err != nil {
		return nil, err
	}
	return &service.PutResponse{
		ID:        answer.ID,
		Version:   answer.Version,
		Timestamp: answer.Timestamp,
		LeaderID:  answer.LeaderID,
	}, nil
}

func kvGetArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.GetRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvArgs(r.EntityType, r.ID, r.ShardID, r.Consistency), true
}

func kvGetResult(text string) (interface{}, error) {
	var answer struct {
		ID        uint64 `json:"id,string"`
		Found     bool   `json:"found"`
		Data      []byte `json:"data"`
		Version   uint64 `json:"version,string"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := decodeKVAnswer("quicbaas_kv_get", text, &answer); err != nil {
		return nil, err
	}
	return &service.GetResponse{
		ID:        answer.ID,
		Data:      answer.Data,
		Version:   answer.Version,
		Timestamp: answer.Timestamp,
		Found:     answer.Found,
	}, nil
}

func kvDeleteArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.DeleteRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvArgs(r.EntityType, r.ID, r.ShardID, r.Consistency), true
}

func kvDeleteResult(text string) (interface{}, error) {
	var answer service.DeleteResponse
	if err := decodeKVAnswer("quicbaas_kv_delete", text, &answer); err != nil {
		return nil, err
	}
	return &answer, nil
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

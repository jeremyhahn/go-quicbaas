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

// The server's qrdb_put, qrdb_get and qrdb_delete tools take the SDK's
// "entity_type" and numeric "id" (plus standard base64 "data" on put) and let
// the service layer build the canonical key, the same key REST and gRPC use.
// No "key" is sent: the server hashes a string key into a different ID. The
// tools answer with the service response as JSON text.

// kvVerb maps one KV method onto its qrdb tool's arguments and answer. args
// reports false when req is not the method's typed SDK request.
type kvVerb struct {
	args   func(req interface{}) (map[string]interface{}, bool)
	result func(req interface{}, text string) (interface{}, error)
}

var kvVerbs = map[string]kvVerb{
	"Put":    {args: kvPutArgs, result: kvPutResult},
	"Get":    {args: kvGetArgs, result: kvGetResult},
	"Delete": {args: kvDeleteArgs, result: kvDeleteResult},
}

// isMCPKVService reports whether service names the KV service.
func isMCPKVService(service string) bool {
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		service = service[idx+1:]
	}
	return service == "KV"
}

// requestKV calls a qrdb KV tool and decodes its JSON answer.
func (t *Transport) requestKV(ctx context.Context, tool string, verb kvVerb, args map[string]interface{}, req interface{}) (interface{}, error) {
	result, err := t.callToolResult(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	return verb.result(req, firstText(result.Content))
}

// kvEntityArgs are the arguments that address one entity.
func kvEntityArgs(entityType string, id uint64) map[string]interface{} {
	return map[string]interface{}{"entity_type": entityType, "id": id}
}

// decodeKVAnswer decodes a KV tool's JSON answer into resp.
func decodeKVAnswer(text, op string, resp interface{}) (interface{}, error) {
	if err := json.Unmarshal([]byte(text), resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "KV tool answered a result that is not the service response")
	}
	return resp, nil
}

func kvPutArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.PutRequest)
	if !ok || r == nil {
		return nil, false
	}
	args := kvEntityArgs(r.EntityType, r.ID)
	args["data"] = base64.StdEncoding.EncodeToString(r.Data)
	return args, true
}

func kvPutResult(_ interface{}, text string) (interface{}, error) {
	return decodeKVAnswer(text, "mcp.kv.Put", &service.PutResponse{})
}

func kvGetArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.GetRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvEntityArgs(r.EntityType, r.ID), true
}

func kvGetResult(_ interface{}, text string) (interface{}, error) {
	return decodeKVAnswer(text, "mcp.kv.Get", &service.GetResponse{})
}

func kvDeleteArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.DeleteRequest)
	if !ok || r == nil {
		return nil, false
	}
	return kvEntityArgs(r.EntityType, r.ID), true
}

func kvDeleteResult(_ interface{}, text string) (interface{}, error) {
	return decodeKVAnswer(text, "mcp.kv.Delete", &service.DeleteResponse{})
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

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
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// The unified server serves KV over go-qrdb's native qrdb_put, qrdb_get and
// qrdb_delete tools. They take a string "key", carry the value as standard
// base64 in "value", and answer with plain text: "ok" for a write and the
// base64 value for a read. The SDK's entity-typed requests are mapped onto
// that schema here.

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

// requestKV calls a qrdb KV tool and decodes its text answer.
func (t *Transport) requestKV(ctx context.Context, tool string, verb kvVerb, args map[string]interface{}, req interface{}) (interface{}, error) {
	result, err := t.callToolResult(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	return verb.result(req, firstText(result.Content))
}

// kvKey is the qrdb key an entity type and ID are stored under.
func kvKey(entityType string, id uint64) string {
	return entityType + "/" + strconv.FormatUint(id, 10)
}

func kvPutArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.PutRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{
		"key":   kvKey(r.EntityType, r.ID),
		"value": base64.StdEncoding.EncodeToString(r.Data),
	}, true
}

func kvPutResult(req interface{}, _ string) (interface{}, error) {
	return &service.PutResponse{ID: req.(*service.PutRequest).ID}, nil
}

func kvGetArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.GetRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"key": kvKey(r.EntityType, r.ID)}, true
}

func kvGetResult(req interface{}, text string) (interface{}, error) {
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.kv.Get", "qrdb_get answered a value that is not base64")
	}
	return &service.GetResponse{ID: req.(*service.GetRequest).ID, Data: data, Found: true}, nil
}

func kvDeleteArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*service.DeleteRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"key": kvKey(r.EntityType, r.ID)}, true
}

func kvDeleteResult(interface{}, string) (interface{}, error) {
	return &service.DeleteResponse{Deleted: true}, nil
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

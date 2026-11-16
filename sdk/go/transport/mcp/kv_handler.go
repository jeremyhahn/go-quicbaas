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
	"encoding/json"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPKVService reports whether service is the entity KV service whose
// Put/Get/Delete/List reach the quicbaas_kv_* tools.
func isMCPKVService(service string) bool {
	return service == "quicbaas.KV" || service == "KV"
}

// kvListToolResult is the quicbaas_kv_list tool's answer: one page of
// entities and the camelCase cursor fields the tool emits.
type kvListToolResult struct {
	Entities      []*service.Entity `json:"entities"`
	NextPageToken string            `json:"nextPageToken"`
	TotalCount    uint64            `json:"totalCount"`
}

// kvArguments converts a KV request to the entity tool's arguments. The
// generic conversion already yields entity_type, id and base64 data, which
// the tools read; List's paging fields are renamed to the pageSize and
// pageToken names the list tool reads.
func (t *Transport) kvArguments(method string, req interface{}) map[string]interface{} {
	args := toolArguments(req, true)
	if args == nil {
		return nil
	}
	// The server's MCP layer decodes tool arguments with plain
	// encoding/json, so a numeric id above 2^53 would round there; the
	// entity tools parse a decimal-string id exactly.
	// A map-based request carries its id as a Go integer, not json.Number,
	// so every integral form is sent as its decimal string.
	if id, ok := kvIDDecimal(args["id"]); ok {
		args["id"] = id
	}
	if method != "List" {
		return args
	}
	if v, ok := args["page_size"]; ok {
		args["pageSize"] = v
		delete(args, "page_size")
	}
	if v, ok := args["page_token"]; ok {
		args["pageToken"] = v
		delete(args, "page_token")
	}
	return args
}

// kvIDDecimal renders an integral KV id as its decimal string: json.Number
// (a struct request decoded exactly) and the Go integer types a map-based
// request carries. Anything else (a string, a fractional or negative value,
// a non-numeric type) is left for the entity tool to accept or refuse.
func kvIDDecimal(v interface{}) (string, bool) {
	switch id := v.(type) {
	case json.Number:
		return id.String(), true
	case uint64:
		return strconv.FormatUint(id, 10), true
	case uint:
		return strconv.FormatUint(uint64(id), 10), true
	case uint32:
		return strconv.FormatUint(uint64(id), 10), true
	case int64:
		if id >= 0 {
			return strconv.FormatInt(id, 10), true
		}
	case int:
		if id >= 0 {
			return strconv.Itoa(id), true
		}
	case int32:
		if id >= 0 {
			return strconv.FormatInt(int64(id), 10), true
		}
	}
	return "", false
}

// transformKVResponse decodes an entity KV tool result into the
// pkg/service response the KV client accepts from every transport.
func transformKVResponse(method string, result interface{}) (interface{}, error) {
	op := "mcp.kv." + method
	switch method {
	case "Put":
		var resp service.PutResponse
		if err := decodeToolResult(op, result, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	case "Get":
		var resp service.GetResponse
		if err := decodeToolResult(op, result, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	case "Delete":
		var resp service.DeleteResponse
		if err := decodeToolResult(op, result, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	case "List":
		var page kvListToolResult
		if err := decodeToolResult(op, result, &page); err != nil {
			return nil, err
		}
		return &service.ListResponse{
			Entities:      page.Entities,
			NextPageToken: page.NextPageToken,
			TotalCount:    page.TotalCount,
		}, nil
	default:
		return result, nil
	}
}

// decodeToolResult re-encodes a generic tool result and decodes it into out.
func decodeToolResult(op string, result, out interface{}) error {
	// An empty or JSON-null tool reply carries no response: decoding it would
	// leave out zero-valued and report a write that nothing confirmed as a
	// success, so it is a protocol error.
	if result == nil {
		return errors.New(errors.CodeInternal, op, "tool returned no result")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode tool result")
	}
	if string(data) == "null" {
		return errors.New(errors.CodeInternal, op, "tool returned no result")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "failed to decode tool result")
	}
	return nil
}

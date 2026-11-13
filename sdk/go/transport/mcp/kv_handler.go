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
	args := t.convertToArguments(req)
	if method != "List" || args == nil {
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
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode tool result")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "failed to decode tool result")
	}
	return nil
}

//go:build mcp

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

import "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"

// pageSchemaProperties are the paging, sorting and search arguments every MCP
// list tool accepts. They are spliced into each tool's input schema from here
// so the model sees one contract — the same one REST documents — instead of a
// per-tool paraphrase.
func pageSchemaProperties(sortFields string) map[string]interface{} {
	return map[string]interface{}{
		"limit": map[string]interface{}{
			"type":        "integer",
			"description": "Page size. Optional; defaults to 25 and is capped at 100.",
		},
		"offset": map[string]interface{}{
			"type":        "integer",
			"description": "Records to skip. Optional; defaults to 0.",
		},
		"sort": map[string]interface{}{
			"type":        "string",
			"description": "Sort as \"<field>\" or \"<field>:desc\". Allowed fields: " + sortFields + ". Optional.",
		},
		"q": map[string]interface{}{
			"type":        "string",
			"description": "Case-insensitive substring search over the listing's text fields. Optional.",
		},
	}
}

// withPageProperties returns the tool's own schema properties plus the shared
// paging ones.
func withPageProperties(properties map[string]interface{}, sortFields string) map[string]interface{} {
	for name, schema := range pageSchemaProperties(sortFields) {
		properties[name] = schema
	}
	return properties
}

// pageFromArgs reads the shared paging arguments off an MCP tool call.
// Non-numeric limits and offsets arrive as zero from getInt, which is the
// same thing as "unset" — the tool contract types them as integers, so a
// model that sends a word gets the default page rather than a refusal.
func pageFromArgs(args map[string]interface{}) *orchestration.Page {
	return &orchestration.Page{
		Limit:  getInt(args, "limit"),
		Offset: getInt(args, "offset"),
		Sort:   getString(args, "sort"),
		Query:  getString(args, "q"),
	}
}

// pageResultFields renders the paging half of a list tool's JSON result.
// next_offset is omitted on the last page, exactly as REST omits it.
func pageResultFields[T any](result *orchestration.PageResult[T], into map[string]interface{}) map[string]interface{} {
	into["total"] = result.Total
	into["limit"] = result.Limit
	into["offset"] = result.Offset
	if result.HasNext {
		into["next_offset"] = result.NextOffset
	}
	return into
}

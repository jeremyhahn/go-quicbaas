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

// batch_handler.go adapts the Batch client's Execute verb to the
// quicbaas_batch_execute MCP tool: the operation type travels as the PUT or
// DELETE string the tool accepts, the value as standard base64, and the
// result decodes into the client/batch type the SDK client type-asserts on.
// Put, Get and Delete keep the generic path.

package mcp

import (
	"encoding/base64"

	batchclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPBatchService reports whether the service name targets Batch.
func isMCPBatchService(service string) bool {
	return trimServiceName(service) == "Batch"
}

// batchVerbs are the Batch methods whose tool result must decode into a
// client type. Keep in sync with pkg/api/mcp/tools.go.
var batchVerbs = map[string]toolVerb{
	"Execute": {tool: "quicbaas_batch_execute", args: batchExecuteArgs, decode: decodeBatchExecute},
}

func batchExecuteArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*batchclient.BatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.batch.Execute",
			"invalid request type for Execute")
	}
	operations := make([]interface{}, len(r.Operations))
	for i, op := range r.Operations {
		operation := map[string]interface{}{
			"type":        op.Type.String(),
			"entity_type": op.EntityType,
			"key":         op.Key,
		}
		if len(op.Value) > 0 {
			operation["value"] = base64.StdEncoding.EncodeToString(op.Value)
		}
		operations[i] = operation
	}
	return map[string]interface{}{"operations": operations}, nil
}

func decodeBatchExecute(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "results")
	results := make([]batchclient.BatchResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, batchclient.BatchResult{
			Success: getResultBool(entry, "success"),
			Error:   getResultString(entry, "error"),
		})
	}
	return &batchclient.BatchResponse{
		Revision: getResultUint64(m, "revision"),
		Results:  results,
	}
}

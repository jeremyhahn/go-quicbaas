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

package quic

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

// RAG Opcodes - matching server protocol (0x59-0x5A).
const (
	OpRAGQuery        byte = 0x59
	OpRAGBuildContext byte = 0x5A
)

// ragMethodToOpcode maps RAG service/method combinations to opcodes.
var ragMethodToOpcode = map[string]byte{
	// Legacy format
	"RAG/Query":        OpRAGQuery,
	"RAG/BuildContext": OpRAGBuildContext,

	// New format
	"quicbaas.RAG/Query":        OpRAGQuery,
	"quicbaas.RAG/BuildContext": OpRAGBuildContext,
}

// ragOpcodeToMethod maps RAG opcodes to service/method combinations.
var ragOpcodeToMethod = map[byte]string{
	OpRAGQuery:        "quicbaas.RAG/Query",
	OpRAGBuildContext: "quicbaas.RAG/BuildContext",
}

// isRAGService returns true if the service is a RAG service.
func isRAGService(service string) bool {
	return service == "quicbaas.RAG" || service == "RAG"
}

// RAGMethodToOpcode converts a RAG service/method combination to an opcode.
func RAGMethodToOpcode(service, method string) (byte, bool) {
	key := service + "/" + method
	opcode, ok := ragMethodToOpcode[key]
	return opcode, ok
}

// RAGOpcodeToMethod converts a RAG opcode to a service/method combination.
func RAGOpcodeToMethod(opcode byte) (string, bool) {
	method, ok := ragOpcodeToMethod[opcode]
	return method, ok
}

// convertRAGResponse converts a raw map response to the appropriate RAG SDK type.
func convertRAGResponse(method string, decoded map[string]interface{}) (interface{}, error) {
	switch method {
	case "Query":
		return convertRAGQueryResponse(decoded)
	case "BuildContext":
		return convertRAGBuildContextResponse(decoded)
	default:
		return decoded, nil
	}
}

// convertRAGQueryResponse converts a raw map to a QueryResponse.
func convertRAGQueryResponse(m map[string]interface{}) (*rag.QueryResponse, error) {
	resp := &rag.QueryResponse{}

	if v, ok := m["context"].(string); ok {
		resp.Context = v
	}
	if v, ok := m["tokens"].(float64); ok {
		resp.Tokens = int(v)
	}
	if v, ok := m["truncated"].(bool); ok {
		resp.Truncated = v
	}
	if v, ok := m["result_count"].(float64); ok {
		resp.ResultCount = int(v)
	}

	if citations, ok := m["citations"].([]interface{}); ok {
		resp.Citations = make([]rag.Citation, len(citations))
		for i, c := range citations {
			if cm, ok := c.(map[string]interface{}); ok {
				resp.Citations[i] = convertCitation(cm)
			}
		}
	}

	return resp, nil
}

// convertRAGBuildContextResponse converts a raw map to a BuildContextResponse.
func convertRAGBuildContextResponse(m map[string]interface{}) (*rag.BuildContextResponse, error) {
	resp := &rag.BuildContextResponse{}

	if v, ok := m["context"].(string); ok {
		resp.Context = v
	}
	if v, ok := m["tokens"].(float64); ok {
		resp.Tokens = int(v)
	}
	if v, ok := m["truncated"].(bool); ok {
		resp.Truncated = v
	}
	if v, ok := m["result_count"].(float64); ok {
		resp.ResultCount = int(v)
	}

	if citations, ok := m["citations"].([]interface{}); ok {
		resp.Citations = make([]rag.Citation, len(citations))
		for i, c := range citations {
			if cm, ok := c.(map[string]interface{}); ok {
				resp.Citations[i] = convertCitation(cm)
			}
		}
	}

	return resp, nil
}

// convertCitation converts a map to a Citation.
func convertCitation(m map[string]interface{}) rag.Citation {
	c := rag.Citation{}

	if v, ok := m["index"].(float64); ok {
		c.Index = int(v)
	}
	if v, ok := m["file_path"].(string); ok {
		c.FilePath = v
	}
	if v, ok := m["start_line"].(float64); ok {
		c.StartLine = int(v)
	}
	if v, ok := m["end_line"].(float64); ok {
		c.EndLine = int(v)
	}
	if v, ok := m["score"].(float64); ok {
		c.Score = float32(v)
	}

	return c
}

// transformRAGRequest transforms SDK RAG request types to server-side format.
func transformRAGRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Query":
		return transformQueryRequest(req)
	case "BuildContext":
		return transformBuildContextRequest(req)
	default:
		return req, nil
	}
}

// transformQueryRequest transforms a QueryRequest to server format.
func transformQueryRequest(req interface{}) (interface{}, error) {
	if r, ok := req.(*rag.QueryRequest); ok {
		return map[string]interface{}{
			"query":             r.Query,
			"namespace":         r.Namespace,
			"top_k":             r.TopK,
			"max_tokens":        r.MaxTokens,
			"min_score":         r.MinScore,
			"template":          r.Template,
			"include_citations": r.IncludeCitations,
			"model":             r.Model,
		}, nil
	}
	return req, nil
}

// transformBuildContextRequest transforms a BuildContextRequest to server format.
func transformBuildContextRequest(req interface{}) (interface{}, error) {
	if r, ok := req.(*rag.BuildContextRequest); ok {
		results := make([]map[string]interface{}, len(r.Results))
		for i, res := range r.Results {
			results[i] = map[string]interface{}{
				"content":  res.Content,
				"score":    res.Score,
				"metadata": res.Metadata,
			}
		}
		return map[string]interface{}{
			"results":             results,
			"max_tokens":          r.MaxTokens,
			"template":            r.Template,
			"include_citations":   r.IncludeCitations,
			"truncation_strategy": r.TruncationStrategy,
		}, nil
	}
	return req, nil
}

// extractMethodName extracts the method name from a full method path.
func extractMethodName(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullMethod
}

func init() {
	// Register RAG opcodes in the main maps
	for k, v := range ragMethodToOpcode {
		methodToOpcode[k] = v
	}
	for k, v := range ragOpcodeToMethod {
		opcodeToMethod[k] = v
	}
}

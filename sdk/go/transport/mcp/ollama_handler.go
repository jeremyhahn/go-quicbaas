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

// ollama_handler.go adapts the Ollama client's verbs to the quicbaas_ollama_*
// MCP tools that serve them, some under resource-first names
// (quicbaas_ollama_model_list, not _list_models), and decodes their results
// into the client/ollama types the SDK client type-asserts on. Health and
// IsAvailable are both served by the health tool. EmbedWithModel keeps the
// generic path.

package mcp

import (
	ollamaclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// ollamaServiceName is the service name the Ollama client sends.
const ollamaServiceName = "quicbaas.Ollama"

// isMCPOllamaService reports whether the service name targets Ollama.
func isMCPOllamaService(service string) bool {
	return service == ollamaServiceName || service == "Ollama"
}

// ollamaVerbs are the Ollama methods whose tool name the generic derivation
// does not reach. Keep in sync with pkg/api/mcp/ollama_tools.go.
var ollamaVerbs = map[string]toolVerb{
	"ListModels":   {tool: "quicbaas_ollama_model_list", args: ollamaNoArgs, decode: decodeOllamaListModels},
	"GetModelInfo": {tool: "quicbaas_ollama_model_info", args: ollamaModelInfoArgs, decode: decodeOllamaModelInfo},
	"IsAvailable":  {tool: "quicbaas_ollama_health", args: ollamaNoArgs, decode: decodeOllamaHealth},
	"Health":       {tool: "quicbaas_ollama_health", args: ollamaNoArgs, decode: decodeOllamaHealth},
	"Embed":        {tool: "quicbaas_ollama_embed", args: ollamaEmbedArgs, decode: decodeOllamaEmbed},
	"EmbedBatch":   {tool: "quicbaas_ollama_embed_batch", args: ollamaEmbedBatchArgs, decode: decodeOllamaEmbedBatch},
}

// ollamaNoArgs serves the verbs whose tool takes no arguments.
func ollamaNoArgs(interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func ollamaModelInfoArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*ollamaclient.GetModelInfoRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.ollama.GetModelInfo",
			"invalid request type for GetModelInfo")
	}
	return map[string]interface{}{"model": r.Model}, nil
}

func ollamaEmbedArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*ollamaclient.EmbedRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.ollama.Embed",
			"invalid request type for Embed")
	}
	return map[string]interface{}{"text": r.Text}, nil
}

// ollamaEmbedBatchArgs encodes the texts as the JSON array string the
// quicbaas_ollama_embed_batch schema declares.
func ollamaEmbedBatchArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*ollamaclient.EmbedBatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.ollama.EmbedBatch",
			"invalid request type for EmbedBatch")
	}
	args := map[string]interface{}{}
	if err := putJSONStringArg(args, "mcp.ollama.EmbedBatch", "texts", r.Texts); err != nil {
		return nil, err
	}
	if r.Model != "" {
		args["model"] = r.Model
	}
	return args, nil
}

func decodeOllamaListModels(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "models")
	models := make([]ollamaclient.ModelInfo, 0, len(entries))
	for _, entry := range entries {
		models = append(models, ollamaclient.ModelInfo{
			Name:       getResultString(entry, "name"),
			Size:       getResultInt64(entry, "size"),
			Digest:     getResultString(entry, "digest"),
			ModifiedAt: getResultTime(entry, "modified_at"),
		})
	}
	return &ollamaclient.ListModelsResponse{
		Models: models,
		Count:  int(getResultInt64(m, "count")),
	}
}

func decodeOllamaModelInfo(m map[string]interface{}) interface{} {
	details := nestedResultMap(m, "details")
	return &ollamaclient.GetModelInfoResponse{
		Model: getResultString(m, "model"),
		Details: ollamaclient.ModelDetails{
			License:           getResultString(details, "license"),
			Format:            getResultString(details, "format"),
			Family:            getResultString(details, "family"),
			ParameterSize:     getResultString(details, "parameter_size"),
			QuantizationLevel: getResultString(details, "quantization_level"),
		},
	}
}

func decodeOllamaHealth(m map[string]interface{}) interface{} {
	return &ollamaclient.HealthResponse{
		Available:   getResultBool(m, "available"),
		Version:     getResultString(m, "version"),
		BaseURL:     getResultString(m, "base_url"),
		Error:       getResultString(m, "error"),
		LastChecked: getResultTime(m, "last_checked"),
	}
}

func decodeOllamaEmbed(m map[string]interface{}) interface{} {
	return &ollamaclient.EmbedResponse{
		Embedding:      resultFloat32Slice(m, "embedding"),
		Model:          getResultString(m, "model"),
		Dimensions:     int(getResultInt64(m, "dimensions")),
		ProcessingTime: resultDuration(m, "processing_time"),
	}
}

func decodeOllamaEmbedBatch(m map[string]interface{}) interface{} {
	return &ollamaclient.EmbedBatchResponse{
		Embeddings:     resultFloat32Matrix(m, "embeddings"),
		Model:          getResultString(m, "model"),
		Dimensions:     int(getResultInt64(m, "dimensions")),
		Count:          int(getResultInt64(m, "count")),
		ProcessingTime: resultDuration(m, "processing_time"),
	}
}

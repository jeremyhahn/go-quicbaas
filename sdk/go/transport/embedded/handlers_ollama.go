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

package embedded

import (
	"context"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
)

// ollamaServiceName is the service name the Ollama client sends.
const ollamaServiceName = "quicbaas.Ollama"

func init() {
	unaryHandlers[ollamaServiceName+"/Embed"] = handleOllamaEmbed
	unaryHandlers[ollamaServiceName+"/EmbedWithModel"] = handleOllamaEmbedWithModel
	unaryHandlers[ollamaServiceName+"/EmbedBatch"] = handleOllamaEmbedBatch
	unaryHandlers[ollamaServiceName+"/ListModels"] = handleOllamaListModels
	unaryHandlers[ollamaServiceName+"/GetModelInfo"] = handleOllamaGetModelInfo
	unaryHandlers[ollamaServiceName+"/Health"] = handleOllamaHealth
	unaryHandlers[ollamaServiceName+"/IsAvailable"] = handleOllamaIsAvailable
}

// ollamaRequest resolves the Ollama service and type-asserts the request the
// SDK client sent.
func ollamaRequest[T any](svc service.QuicBaaS, method string, req interface{}) (service.OllamaService, *T, error) {
	ollamaSvc := svc.Ollama()
	if ollamaSvc == nil {
		return nil, nil, NewServiceNotConfiguredError("Ollama")
	}
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, nil, NewInvalidRequestTypeError(ollamaServiceName, method,
			reflect.TypeOf((*T)(nil)).String(), typeNameOf(req))
	}
	return ollamaSvc, r, nil
}

func ollamaEmbedResponseToSDK(resp *service.OllamaEmbedResponse) *ollama.EmbedResponse {
	return &ollama.EmbedResponse{
		Embedding:      resp.Embedding,
		Model:          resp.Model,
		Dimensions:     resp.Dimensions,
		ProcessingTime: resp.ProcessingTime,
		Timestamp:      resp.Timestamp,
	}
}

func ollamaHealthResponseToSDK(resp *service.OllamaHealthResponse) *ollama.HealthResponse {
	return &ollama.HealthResponse{
		Available:   resp.Available,
		Version:     resp.Version,
		BaseURL:     resp.BaseURL,
		Error:       resp.Error,
		LastChecked: resp.LastChecked,
		Timestamp:   resp.Timestamp,
	}
}

func handleOllamaEmbed(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, r, err := ollamaRequest[ollama.EmbedRequest](svc, "Embed", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.Embed(ctx, &service.OllamaEmbedRequest{Text: r.Text})
	if err != nil {
		return nil, err
	}
	return ollamaEmbedResponseToSDK(resp), nil
}

func handleOllamaEmbedWithModel(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, r, err := ollamaRequest[ollama.EmbedWithModelRequest](svc, "EmbedWithModel", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{Text: r.Text, Model: r.Model})
	if err != nil {
		return nil, err
	}
	return ollamaEmbedResponseToSDK(resp), nil
}

func handleOllamaEmbedBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, r, err := ollamaRequest[ollama.EmbedBatchRequest](svc, "EmbedBatch", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.EmbedBatch(ctx, &service.OllamaEmbedBatchRequest{Texts: r.Texts, Model: r.Model})
	if err != nil {
		return nil, err
	}
	return &ollama.EmbedBatchResponse{
		Embeddings:     resp.Embeddings,
		Model:          resp.Model,
		Dimensions:     resp.Dimensions,
		Count:          resp.Count,
		ProcessingTime: resp.ProcessingTime,
		Timestamp:      resp.Timestamp,
	}, nil
}

func handleOllamaListModels(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, _, err := ollamaRequest[ollama.ListModelsRequest](svc, "ListModels", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.ListModels(ctx, &service.OllamaListModelsRequest{})
	if err != nil {
		return nil, err
	}
	models := make([]ollama.ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		models = append(models, ollama.ModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
		})
	}
	return &ollama.ListModelsResponse{
		Models:    models,
		Count:     resp.Count,
		Timestamp: resp.Timestamp,
	}, nil
}

func handleOllamaGetModelInfo(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, r, err := ollamaRequest[ollama.GetModelInfoRequest](svc, "GetModelInfo", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.GetModelInfo(ctx, &service.OllamaGetModelInfoRequest{Model: r.Model})
	if err != nil {
		return nil, err
	}
	return &ollama.GetModelInfoResponse{
		Model: resp.Model,
		Details: ollama.ModelDetails{
			License:           resp.Details.License,
			Modelfile:         resp.Details.Modelfile,
			Format:            resp.Details.Format,
			Family:            resp.Details.Family,
			ParameterSize:     resp.Details.ParameterSize,
			QuantizationLevel: resp.Details.QuantizationLevel,
		},
		Timestamp: resp.Timestamp,
	}, nil
}

func handleOllamaHealth(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, _, err := ollamaRequest[ollama.HealthRequest](svc, "Health", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.Health(ctx, &service.OllamaHealthRequest{})
	if err != nil {
		return nil, err
	}
	return ollamaHealthResponseToSDK(resp), nil
}

// handleOllamaIsAvailable serves IsAvailable from the health verb, as MCP
// does: the client sends an empty struct and reads Available off the
// health response.
func handleOllamaIsAvailable(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	ollamaSvc, _, err := ollamaRequest[struct{}](svc, "IsAvailable", req)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaSvc.Health(ctx, &service.OllamaHealthRequest{})
	if err != nil {
		return nil, err
	}
	return ollamaHealthResponseToSDK(resp), nil
}

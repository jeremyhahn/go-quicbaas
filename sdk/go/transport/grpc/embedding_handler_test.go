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

package grpc

import (
	"context"
	"strings"
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestIsEmbeddingService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "embedding service",
			service:  "quicbaas.Embedding",
			expected: true,
		},
		{
			name:     "embedding service with method suffix",
			service:  "quicbaas.Embedding.GenerateEmbedding",
			expected: true,
		},
		{
			name:     "non-embedding service",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "empty service",
			service:  "",
			expected: false,
		},
		{
			name:     "similar but different prefix",
			service:  "quicbaas.Embed",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmbeddingService(tt.service)
			if result != tt.expected {
				t.Errorf("isEmbeddingService(%q) = %v, want %v", tt.service, result, tt.expected)
			}
		})
	}
}

func TestNewEmbeddingHandler(t *testing.T) {
	handler := newEmbeddingHandler(nil)
	if handler == nil {
		t.Error("newEmbeddingHandler(nil) returned nil, want non-nil handler")
	}
	if handler.conn != nil {
		t.Error("handler.conn should be nil when created with nil")
	}
}

func TestEmbeddingHandler_HandleRequest_UnsupportedMethod(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "UnsupportedMethod", nil)
	if err == nil {
		t.Error("expected error for unsupported method")
	}
	if resp != nil {
		t.Error("expected nil response for unsupported method")
	}
}

func TestEmbeddingHandler_HandleGenerateEmbedding_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GenerateEmbedding", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleBatchGenerateEmbeddings_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "BatchGenerateEmbeddings", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleListProviders_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "ListProviders", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleGetProviderInfo_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleGetProviderInfo_UnknownProvider(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	t.Run("unrecognized provider returns not found", func(t *testing.T) {
		req := &embedding.GetProviderInfoRequest{
			Provider: embedding.Provider("nonexistent-provider-xyz"),
		}
		resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
		if resp != nil {
			t.Error("expected nil response for unknown provider")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "nonexistent-provider-xyz") {
			t.Errorf("error should contain the provider name, got: %v", err)
		}
	})

	t.Run("empty provider maps to UNSPECIFIED correctly", func(t *testing.T) {
		// When provider is empty, UNSPECIFIED is the correct mapping.
		// Verify the guard does NOT intercept empty provider strings.
		pbProvider := sdkProviderToProto("")
		if pbProvider != pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
			t.Errorf("empty provider should map to UNSPECIFIED, got %v", pbProvider)
		}
		// The empty string check (sdkReq.Provider != "") prevents treating
		// empty as unknown, so the request would pass through to the server.
	})

	t.Run("known providers do not trigger unknown detection", func(t *testing.T) {
		knownProviders := []embedding.Provider{
			embedding.ProviderVoyage,
			embedding.ProviderOpenAI,
			embedding.ProviderOllama,
			embedding.ProviderLMStudio,
			embedding.ProviderNPU,
		}
		for _, provider := range knownProviders {
			pbProvider := sdkProviderToProto(provider)
			if pbProvider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
				t.Errorf("known provider %q should not map to UNSPECIFIED", provider)
			}
		}
	})

	t.Run("multiple unknown providers all return not found", func(t *testing.T) {
		unknownProviders := []string{
			"fake-provider",
			"huggingface",
			"anthropic-embed",
			"random-123",
		}
		for _, name := range unknownProviders {
			req := &embedding.GetProviderInfoRequest{
				Provider: embedding.Provider(name),
			}
			resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetProviderInfo", req)
			if err == nil {
				t.Errorf("expected error for unknown provider %q", name)
			}
			if resp != nil {
				t.Errorf("expected nil response for unknown provider %q", name)
			}
			if !errors.IsNotFound(err) {
				t.Errorf("expected NotFound for provider %q, got: %v", name, err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error for provider %q should contain the name, got: %v", name, err)
			}
		}
	})
}

func TestEmbeddingHandler_HandleSetDefaultProvider_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "SetDefaultProvider", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleSetDefaultProvider_UnknownProvider(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	t.Run("unrecognized provider returns not found", func(t *testing.T) {
		req := &embedding.SetDefaultProviderRequest{
			Provider: embedding.Provider("totally-fake-provider"),
		}
		resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "SetDefaultProvider", req)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
		if resp != nil {
			t.Error("expected nil response for unknown provider")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected NotFound error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "totally-fake-provider") {
			t.Errorf("error should contain the provider name, got: %v", err)
		}
	})

	t.Run("empty provider maps to UNSPECIFIED correctly", func(t *testing.T) {
		// Same guard logic as GetProviderInfo: empty string should NOT
		// be treated as "unknown", it maps to UNSPECIFIED intentionally.
		pbProvider := sdkProviderToProto("")
		if pbProvider != pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
			t.Errorf("empty provider should map to UNSPECIFIED, got %v", pbProvider)
		}
	})

	t.Run("known providers do not trigger unknown detection", func(t *testing.T) {
		knownProviders := []embedding.Provider{
			embedding.ProviderVoyage,
			embedding.ProviderOpenAI,
			embedding.ProviderOllama,
			embedding.ProviderLMStudio,
			embedding.ProviderNPU,
		}
		for _, provider := range knownProviders {
			pbProvider := sdkProviderToProto(provider)
			if pbProvider == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
				t.Errorf("known provider %q should not map to UNSPECIFIED", provider)
			}
		}
	})

	t.Run("multiple unknown providers all return not found", func(t *testing.T) {
		unknownProviders := []string{
			"fake-provider",
			"cohere",
			"mistral-embed",
		}
		for _, name := range unknownProviders {
			req := &embedding.SetDefaultProviderRequest{
				Provider: embedding.Provider(name),
			}
			resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "SetDefaultProvider", req)
			if err == nil {
				t.Errorf("expected error for unknown provider %q", name)
			}
			if resp != nil {
				t.Errorf("expected nil response for unknown provider %q", name)
			}
			if !errors.IsNotFound(err) {
				t.Errorf("expected NotFound for provider %q, got: %v", name, err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error for provider %q should contain the name, got: %v", name, err)
			}
		}
	})
}

func TestEmbeddingHandler_HandleGetCacheStats_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "GetCacheStats", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestEmbeddingHandler_HandleClearCache_InvalidRequestType(t *testing.T) {
	handler := newEmbeddingHandler(nil)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.Embedding", "ClearCache", "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
	if resp != nil {
		t.Error("expected nil response for invalid request type")
	}
}

func TestSdkProviderToProto(t *testing.T) {
	tests := []struct {
		name     string
		provider embedding.Provider
	}{
		{
			name:     "voyage",
			provider: embedding.ProviderVoyage,
		},
		{
			name:     "openai",
			provider: embedding.ProviderOpenAI,
		},
		{
			name:     "ollama",
			provider: embedding.ProviderOllama,
		},
		{
			name:     "lmstudio",
			provider: embedding.ProviderLMStudio,
		},
		{
			name:     "empty",
			provider: "",
		},
		{
			name:     "unknown",
			provider: embedding.Provider("unknown"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pbProvider := sdkProviderToProto(tt.provider)
			sdkProvider := protoProviderToSDK(pbProvider)

			// Only providers with valid mappings should round-trip
			switch tt.provider {
			case embedding.ProviderVoyage, embedding.ProviderOpenAI, embedding.ProviderOllama, embedding.ProviderLMStudio:
				if sdkProvider != tt.provider {
					t.Errorf("round-trip failed: got %v, want %v", sdkProvider, tt.provider)
				}
			default:
				// Unknown providers should map to empty
				if sdkProvider != "" {
					t.Errorf("unknown provider should map to empty, got %v", sdkProvider)
				}
			}
		})
	}
}

func TestSdkInputTypeToProto(t *testing.T) {
	tests := []struct {
		name      string
		inputType embedding.InputType
	}{
		{
			name:      "document",
			inputType: embedding.InputTypeDocument,
		},
		{
			name:      "query",
			inputType: embedding.InputTypeQuery,
		},
		{
			name:      "code",
			inputType: embedding.InputTypeCode,
		},
		{
			name:      "empty",
			inputType: "",
		},
		{
			name:      "unknown",
			inputType: embedding.InputType("unknown"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pbInputType := sdkInputTypeToProto(tt.inputType)
			sdkInputType := protoInputTypeToSDK(pbInputType)

			// Only input types with valid mappings should round-trip
			switch tt.inputType {
			case embedding.InputTypeDocument, embedding.InputTypeQuery, embedding.InputTypeCode:
				if sdkInputType != tt.inputType {
					t.Errorf("round-trip failed: got %v, want %v", sdkInputType, tt.inputType)
				}
			default:
				// Unknown input types should map to empty
				if sdkInputType != "" {
					t.Errorf("unknown input type should map to empty, got %v", sdkInputType)
				}
			}
		})
	}
}

func TestProtoStatusToSDK(t *testing.T) {
	tests := []struct {
		name           string
		expectedStatus embedding.Status
	}{
		{
			name:           "available",
			expectedStatus: embedding.StatusAvailable,
		},
		{
			name:           "unavailable",
			expectedStatus: embedding.StatusUnavailable,
		},
		{
			name:           "rate_limited",
			expectedStatus: embedding.StatusRateLimited,
		},
		{
			name:           "error",
			expectedStatus: embedding.StatusError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the status constants are defined correctly
			if tt.expectedStatus == "" {
				t.Errorf("status should not be empty for %s", tt.name)
			}
		})
	}
}

func TestProtoMetadataToSDK_NilMetadata(t *testing.T) {
	result := protoMetadataToSDK(nil)
	if result != nil {
		t.Error("protoMetadataToSDK(nil) should return nil")
	}
}

func TestProtoProviderInfoToSDK_NilInfo(t *testing.T) {
	result := protoProviderInfoToSDK(nil)
	if result != nil {
		t.Error("protoProviderInfoToSDK(nil) should return nil")
	}
}

// TestSDKProviderRoundTripsEveryProvider guards both SDK conversion tables.
//
// handleGetProviderInfo and handleSetDefaultProvider reject any non-empty
// provider that maps to UNSPECIFIED, so a provider missing from
// sdkProviderToProto is not merely mistranslated -- it is unreachable through
// the SDK entirely, before the request ever leaves the client.
func TestSDKProviderRoundTripsEveryProvider(t *testing.T) {
	providers := []embedding.Provider{
		embedding.ProviderVoyage,
		embedding.ProviderOpenAI,
		embedding.ProviderOllama,
		embedding.ProviderLMStudio,
		embedding.ProviderNPU,
	}

	for _, provider := range providers {
		encoded := sdkProviderToProto(provider)
		if encoded == pb.EmbeddingProvider_EMBEDDING_PROVIDER_UNSPECIFIED {
			t.Errorf("provider %q has no proto enum value, so the SDK would reject it as unknown", provider)
			continue
		}
		if decoded := protoProviderToSDK(encoded); decoded != provider {
			t.Errorf("provider %q round-tripped to %q", provider, decoded)
		}
	}
}

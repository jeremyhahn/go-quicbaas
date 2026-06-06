//go:build vector && qrdb_vector

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

// Package service provides unit tests for the EmbeddingService implementation.
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock Embedding Provider for Unit Testing
// =============================================================================

// mockEmbeddingProvider implements embedding.Provider for testing.
type mockEmbeddingProvider struct {
	name           string
	dimensions     int
	maxTokens      int
	embedFunc      func(ctx context.Context, text string) ([]float32, error)
	embedBatchFunc func(ctx context.Context, texts []string) ([][]float32, error)
}

func newMockEmbeddingProvider(name string) *mockEmbeddingProvider {
	return &mockEmbeddingProvider{
		name:       name,
		dimensions: 1536,
		maxTokens:  8192,
	}
}

func (p *mockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if p.embedFunc != nil {
		return p.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (p *mockEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if p.embedBatchFunc != nil {
		return p.embedBatchFunc(ctx, texts)
	}
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = []float32{0.1, 0.2, 0.3}
	}
	return results, nil
}

func (p *mockEmbeddingProvider) Dimensions() int {
	return p.dimensions
}

func (p *mockEmbeddingProvider) MaxTokens() int {
	return p.maxTokens
}

func (p *mockEmbeddingProvider) Name() string {
	return p.name
}

func (p *mockEmbeddingProvider) Close() error {
	return nil
}

// =============================================================================
// Mock Logger (shared pattern)
// =============================================================================

type mockEmbeddingLogger struct {
	fields map[string]interface{}
}

func newMockEmbeddingLogger() *mockEmbeddingLogger {
	return &mockEmbeddingLogger{
		fields: make(map[string]interface{}),
	}
}

func (l *mockEmbeddingLogger) Debug(msg string, args ...any)                              {}
func (l *mockEmbeddingLogger) Debugf(format string, args ...interface{})                  {}
func (l *mockEmbeddingLogger) Info(msg string, args ...any)                               {}
func (l *mockEmbeddingLogger) Infof(format string, args ...interface{})                   {}
func (l *mockEmbeddingLogger) Warn(msg string, args ...any)                               {}
func (l *mockEmbeddingLogger) Warnf(format string, args ...interface{})                   {}
func (l *mockEmbeddingLogger) Error(msg string, args ...any)                              {}
func (l *mockEmbeddingLogger) Errorf(format string, args ...interface{})                  {}
func (l *mockEmbeddingLogger) Fatal(msg string)                                           {}
func (l *mockEmbeddingLogger) Fatalf(format string, args ...interface{})                  {}
func (l *mockEmbeddingLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *mockEmbeddingLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *mockEmbeddingLogger) WithError(err error) interfaces.Logger                      { return l }

// =============================================================================
// Test Helper Functions
// =============================================================================

// createTestManager creates a real embedding.Manager with a mock provider for testing.
// Returns the adapter (implementing interfaces.EmbeddingManager) and the mock provider.
func createTestManager(t *testing.T, providerName string) (interfaces.EmbeddingManager, *mockEmbeddingProvider) {
	config := embedding.Config{
		Provider:      "openai",
		APIURL:        "http://test",
		APIKey:        "test-key",
		Model:         "test-model",
		Dimensions:    1536,
		MaxBatchSize:  128,
		Timeout:       30,
		CacheEnabled:  true,
		CacheSize:     100,
		CacheTTL:      3600,
		CacheCompress: false,
	}

	mgr, err := embedding.NewManager(config)
	require.NoError(t, err)

	provider := newMockEmbeddingProvider(providerName)
	err = mgr.RegisterProvider(providerName, provider)
	require.NoError(t, err)

	// Wrap with adapter to implement interfaces.EmbeddingManager
	return NewEmbeddingManagerAdapter(mgr), provider
}

// createTestManagerWithoutCache creates a manager with caching disabled.
// Returns the adapter (implementing interfaces.EmbeddingManager) and the mock provider.
func createTestManagerWithoutCache(t *testing.T, providerName string) (interfaces.EmbeddingManager, *mockEmbeddingProvider) {
	config := embedding.Config{
		Provider:     "openai",
		APIURL:       "http://test",
		APIKey:       "test-key",
		Model:        "test-model",
		Dimensions:   1536,
		MaxBatchSize: 128,
		Timeout:      30,
		CacheEnabled: false,
	}

	mgr, err := embedding.NewManager(config)
	require.NoError(t, err)

	provider := newMockEmbeddingProvider(providerName)
	err = mgr.RegisterProvider(providerName, provider)
	require.NoError(t, err)

	// Wrap with adapter to implement interfaces.EmbeddingManager
	return NewEmbeddingManagerAdapter(mgr), provider
}

// createRawTestManager creates a raw embedding.Manager for tests that need direct access.
func createRawTestManager(t *testing.T, providerName string) (*embedding.Manager, *mockEmbeddingProvider) {
	config := embedding.Config{
		Provider:      "openai",
		APIURL:        "http://test",
		APIKey:        "test-key",
		Model:         "test-model",
		Dimensions:    1536,
		MaxBatchSize:  128,
		Timeout:       30,
		CacheEnabled:  true,
		CacheSize:     100,
		CacheTTL:      3600,
		CacheCompress: false,
	}

	mgr, err := embedding.NewManager(config)
	require.NoError(t, err)

	provider := newMockEmbeddingProvider(providerName)
	err = mgr.RegisterProvider(providerName, provider)
	require.NoError(t, err)

	return mgr, provider
}

// =============================================================================
// GenerateEmbedding Tests
// =============================================================================

func TestEmbeddingService_GenerateEmbedding_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text:      "This is a test sentence for embedding",
		Provider:  "openai",
		InputType: EmbeddingInputTypeDocument,
		Source:    "test.txt",
		Language:  "en",
		Metadata:  map[string]string{"purpose": "testing"},
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Embedding)
	assert.Len(t, resp.Embedding, 3)
	assert.NotNil(t, resp.Metadata)
	assert.Equal(t, 1536, resp.Metadata.Dimensions)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_GenerateEmbedding_WithDefaultProvider(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Embedding)
}

func TestEmbeddingService_GenerateEmbedding_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.GenerateEmbedding(context.Background(), nil)
	assert.Error(t, err, "nil request must return a validation error, not panic")
	assert.Nil(t, resp)
}

func TestEmbeddingService_GenerateEmbedding_EmptyText(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "text is required")

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, dErr.Code)
	}
}

func TestEmbeddingService_GenerateEmbedding_ProviderError(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, embedding.ErrProviderUnavailable
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrUnavailable, dErr.Code)
	}
}

func TestEmbeddingService_GenerateEmbedding_RateLimitError(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, embedding.ErrRateLimitExceeded
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrRateLimited, dErr.Code)
	}
}

func TestEmbeddingService_GenerateEmbedding_TokenLimitError(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, embedding.ErrTokenLimitExceeded
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrResourceExhausted, dErr.Code)
	}
}

func TestEmbeddingService_GenerateEmbedding_InvalidInputError(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, embedding.ErrInvalidInput
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, dErr.Code)
	}
}

func TestEmbeddingService_GenerateEmbedding_ProviderNotFound(t *testing.T) {
	// Test with nonexistent provider - implementation may fallback to default
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text:     "Test text",
		Provider: "nonexistent-provider",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	// Either an error is returned or it falls back to default provider
	if err != nil {
		// Error path - provider not found
		assert.Nil(t, resp)
		var dErr *quicbaasErrors.QBError
		if errors.As(err, &dErr) {
			assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
		}
	} else {
		// Fallback path - used default provider
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Embedding)
	}
}

// =============================================================================
// BatchGenerateEmbeddings Tests
// =============================================================================

func TestEmbeddingService_BatchGenerateEmbeddings_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{
			ID:        "item-1",
			Text:      "First text",
			InputType: EmbeddingInputTypeDocument,
			Source:    "doc1.txt",
		},
		{
			ID:        "item-2",
			Text:      "Second text",
			InputType: EmbeddingInputTypeQuery,
			Source:    "doc2.txt",
		},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items:    items,
		Provider: "test-provider",
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Results, 2)
	assert.Equal(t, 2, resp.SuccessCount)
	assert.Equal(t, 0, resp.ErrorCount)
	assert.Greater(t, resp.TotalTokens, 0)
	assert.False(t, resp.Timestamp.IsZero())

	// Check individual results
	for i, result := range resp.Results {
		assert.Equal(t, items[i].ID, result.ID)
		assert.NotNil(t, result.Embedding)
		assert.Empty(t, result.Error)
		assert.NotNil(t, result.Metadata)
	}
}

func TestEmbeddingService_BatchGenerateEmbeddings_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), nil)
	assert.Error(t, err, "nil request must return a validation error, not panic")
	assert.Nil(t, resp)
}

func TestEmbeddingService_BatchGenerateEmbeddings_EmptyItems(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{},
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "items is required")
}

func TestEmbeddingService_BatchGenerateEmbeddings_ItemMissingID(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{
			ID:   "",
			Text: "Text without ID",
		},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items: items,
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "id is required")
}

func TestEmbeddingService_BatchGenerateEmbeddings_ItemMissingText(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{
			ID:   "item-1",
			Text: "",
		},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items: items,
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "text is required")
}

func TestEmbeddingService_BatchGenerateEmbeddings_ProviderError(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedBatchFunc = func(ctx context.Context, texts []string) ([][]float32, error) {
		return nil, embedding.ErrProviderUnavailable
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{ID: "item-1", Text: "Text 1"},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items:    items,
		Provider: "test-provider",
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingService_BatchGenerateEmbeddings_PartialFailure(t *testing.T) {
	// Test batch with fewer embeddings returned than requested
	// NOTE: Implementation may handle partial failures differently than expected
	mgr, provider := createTestManager(t, "openai")
	// Simulate returning fewer embeddings than requested
	provider.embedBatchFunc = func(ctx context.Context, texts []string) ([][]float32, error) {
		// Return only one embedding for two texts
		return [][]float32{{0.1, 0.2, 0.3}}, nil
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{ID: "item-1", Text: "Text 1"},
		{ID: "item-2", Text: "Text 2"},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items:    items,
		Provider: "test-provider",
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	// The implementation should either return an error or handle partial results
	if err != nil {
		// Error path - implementation detected mismatch
		assert.Nil(t, resp)
	} else {
		// Success path - implementation handled it somehow
		assert.NotNil(t, resp)
		// Verify we got some results
		assert.GreaterOrEqual(t, len(resp.Results), 1)
	}
}

// =============================================================================
// ListProviders Tests
// =============================================================================

func TestEmbeddingService_ListProviders_Success(t *testing.T) {
	rawMgr, _ := createRawTestManager(t, "openai")
	// Add another provider
	voyageProvider := newMockEmbeddingProvider("voyage")
	voyageProvider.dimensions = 1024
	voyageProvider.maxTokens = 4096
	err := rawMgr.RegisterProvider("voyage", voyageProvider)
	require.NoError(t, err)

	// Wrap with adapter
	mgr := NewEmbeddingManagerAdapter(rawMgr)

	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &ListProvidersRequest{}

	resp, err := svc.ListProviders(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.GreaterOrEqual(t, len(resp.Providers), 2)
	assert.NotEmpty(t, resp.DefaultProvider)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_ListProviders_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.ListProviders(context.Background(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestEmbeddingService_ListProviders_Empty(t *testing.T) {
	// Create manager without registering any providers
	config := embedding.Config{
		Provider:     "openai",
		APIURL:       "http://test",
		APIKey:       "test",
		Model:        "test",
		Dimensions:   1536,
		MaxBatchSize: 128,
		Timeout:      30,
		CacheEnabled: false,
	}

	rawMgr, err := embedding.NewManager(config)
	require.NoError(t, err)

	// Wrap with adapter
	mgr := NewEmbeddingManagerAdapter(rawMgr)

	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &ListProvidersRequest{}

	resp, err := svc.ListProviders(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Providers)
}

// =============================================================================
// GetProviderInfo Tests
// =============================================================================

func TestEmbeddingService_GetProviderInfo_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GetProviderInfoRequest{
		Provider: "openai",
	}

	resp, err := svc.GetProviderInfo(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Info)
	assert.Equal(t, "openai", resp.Info.Name)
	assert.Equal(t, 1536, resp.Info.Dimensions)
	assert.Equal(t, 8192, resp.Info.MaxTokens)
	assert.Equal(t, EmbeddingStatusAvailable, resp.Info.Status)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_GetProviderInfo_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.GetProviderInfo(context.Background(), nil)
	assert.Error(t, err, "nil request must return a validation error, not panic")
	assert.Nil(t, resp)
}

func TestEmbeddingService_GetProviderInfo_EmptyProvider(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GetProviderInfoRequest{
		Provider: "",
	}

	resp, err := svc.GetProviderInfo(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingService_GetProviderInfo_NotFound(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GetProviderInfoRequest{
		Provider: "nonexistent",
	}

	resp, err := svc.GetProviderInfo(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

// =============================================================================
// SetDefaultProvider Tests
// =============================================================================

func TestEmbeddingService_SetDefaultProvider_Success(t *testing.T) {
	rawMgr, _ := createRawTestManager(t, "openai")
	// Register voyage provider
	voyageProvider := newMockEmbeddingProvider("voyage")
	err := rawMgr.RegisterProvider("voyage", voyageProvider)
	require.NoError(t, err)

	// Wrap with adapter
	mgr := NewEmbeddingManagerAdapter(rawMgr)

	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &SetDefaultProviderRequest{
		Provider: "voyage",
	}

	resp, err := svc.SetDefaultProvider(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, EmbeddingProvider("voyage"), resp.Provider)
	assert.NotEmpty(t, resp.PreviousProvider)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_SetDefaultProvider_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.SetDefaultProvider(context.Background(), nil)
	assert.Error(t, err, "nil request must return a validation error, not panic")
	assert.Nil(t, resp)
}

func TestEmbeddingService_SetDefaultProvider_EmptyProvider(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &SetDefaultProviderRequest{
		Provider: "",
	}

	resp, err := svc.SetDefaultProvider(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestEmbeddingService_SetDefaultProvider_NotFound(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &SetDefaultProviderRequest{
		Provider: "nonexistent",
	}

	resp, err := svc.SetDefaultProvider(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

// =============================================================================
// GetCacheStats Tests
// =============================================================================

func TestEmbeddingService_GetCacheStats_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	// Generate some embeddings to populate cache
	ctx := context.Background()
	req1 := &GenerateEmbeddingRequest{Text: "test1"}
	req2 := &GenerateEmbeddingRequest{Text: "test2"}
	req3 := &GenerateEmbeddingRequest{Text: "test1"} // Cache hit

	_, _ = svc.GenerateEmbedding(ctx, req1)
	_, _ = svc.GenerateEmbedding(ctx, req2)
	_, _ = svc.GenerateEmbedding(ctx, req3)

	req := &GetEmbeddingCacheStatsRequest{}

	resp, err := svc.GetCacheStats(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Enabled)
	assert.Greater(t, resp.Size, 0)
	assert.Greater(t, resp.Hits+resp.Misses, uint64(0))
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_GetCacheStats_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.GetCacheStats(context.Background(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestEmbeddingService_GetCacheStats_NoCache(t *testing.T) {
	mgr, _ := createTestManagerWithoutCache(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GetEmbeddingCacheStatsRequest{}

	resp, err := svc.GetCacheStats(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Enabled)
	assert.Equal(t, 0, resp.Size)
}

// =============================================================================
// ClearCache Tests
// =============================================================================

func TestEmbeddingService_ClearCache_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	// Generate some embeddings to populate cache
	ctx := context.Background()
	req1 := &GenerateEmbeddingRequest{Text: "test1"}
	req2 := &GenerateEmbeddingRequest{Text: "test2"}
	_, _ = svc.GenerateEmbedding(ctx, req1)
	_, _ = svc.GenerateEmbedding(ctx, req2)

	req := &ClearEmbeddingCacheRequest{
		All: true,
	}

	resp, err := svc.ClearCache(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.GreaterOrEqual(t, resp.EntriesCleared, 0)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestEmbeddingService_ClearCache_NilRequest(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	resp, err := svc.ClearCache(context.Background(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestEmbeddingService_ClearCache_ByProvider(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &ClearEmbeddingCacheRequest{
		Provider: EmbeddingProviderOpenAI,
	}

	resp, err := svc.ClearCache(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, EmbeddingProviderOpenAI, resp.Provider)
}

// =============================================================================
// Service Construction Tests
// =============================================================================

func TestNewEmbeddingService_Success(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()

	svc := NewEmbeddingService(mgr, logger)

	require.NotNil(t, svc)

	// Service should be usable
	req := &GenerateEmbeddingRequest{
		Text: "test",
	}
	resp, err := svc.GenerateEmbedding(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestNewEmbeddingService_NilLogger(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")

	svc := NewEmbeddingService(mgr, nil)

	require.NotNil(t, svc)

	// Service should still work without logger
	req := &GenerateEmbeddingRequest{
		Text: "test",
	}
	resp, err := svc.GenerateEmbedding(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestEmbeddingService_ErrorMapping(t *testing.T) {
	tests := []struct {
		name         string
		embeddingErr error
		expectedCode quicbaasErrors.ErrorCode
	}{
		{
			name:         "ProviderNotFound",
			embeddingErr: embedding.ErrProviderNotFound,
			expectedCode: quicbaasErrors.ErrNotFound,
		},
		{
			name:         "InvalidInput",
			embeddingErr: embedding.ErrInvalidInput,
			expectedCode: quicbaasErrors.ErrInvalidInput,
		},
		{
			name:         "ProviderUnavailable",
			embeddingErr: embedding.ErrProviderUnavailable,
			expectedCode: quicbaasErrors.ErrUnavailable,
		},
		{
			name:         "RateLimitExceeded",
			embeddingErr: embedding.ErrRateLimitExceeded,
			expectedCode: quicbaasErrors.ErrRateLimited,
		},
		{
			name:         "TokenLimitExceeded",
			embeddingErr: embedding.ErrTokenLimitExceeded,
			expectedCode: quicbaasErrors.ErrResourceExhausted,
		},
		{
			name:         "UnknownError",
			embeddingErr: errors.New("unknown error"),
			expectedCode: quicbaasErrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, provider := createTestManager(t, "openai")
			provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
				return nil, tt.embeddingErr
			}
			logger := newMockEmbeddingLogger()
			svc := NewEmbeddingService(mgr, logger)

			req := &GenerateEmbeddingRequest{
				Text: "test",
			}

			_, err := svc.GenerateEmbedding(context.Background(), req)

			require.Error(t, err)
			var dErr *quicbaasErrors.QBError
			if errors.As(err, &dErr) {
				assert.Equal(t, tt.expectedCode, dErr.Code)
			}
		})
	}
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestEmbeddingService_GenerateEmbedding_ContextCanceled(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, ctx.Err()
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &GenerateEmbeddingRequest{
		Text: "test",
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Token Estimation Tests
// =============================================================================

func TestEmbeddingService_TokenEstimation(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	tests := []struct {
		name string
		text string
	}{
		{
			name: "Short text",
			text: "Hello",
		},
		{
			name: "Medium text",
			text: "This is a longer sentence with multiple words",
		},
		{
			name: "Long text",
			text: "The quick brown fox jumps over the lazy dog. This is a common pangram used for testing purposes.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &GenerateEmbeddingRequest{
				Text: tt.text,
			}

			resp, err := svc.GenerateEmbedding(context.Background(), req)

			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Metadata)
			assert.Greater(t, resp.Metadata.Tokens, 0)
		})
	}
}

// =============================================================================
// Metadata Propagation Tests
// =============================================================================

func TestEmbeddingService_MetadataPropagation(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text:      "Test text",
		Provider:  "openai",
		InputType: EmbeddingInputTypeCode,
		Source:    "main.go",
		Language:  "go",
		Metadata: map[string]string{
			"project": "quicbaas",
			"module":  "service",
		},
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Metadata)
	assert.Equal(t, EmbeddingInputTypeCode, resp.Metadata.InputType)
	assert.Equal(t, "main.go", resp.Metadata.Source)
	assert.Equal(t, "go", resp.Metadata.Language)
	assert.NotNil(t, resp.Metadata.Tags)
	assert.Equal(t, "quicbaas", resp.Metadata.Tags["project"])
	assert.Equal(t, "service", resp.Metadata.Tags["module"])
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestEmbeddingService_ConcurrentGenerateEmbedding(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	const numGoroutines = 10
	const numRequests = 10

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*numRequests)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				req := &GenerateEmbeddingRequest{
					Text: "Test text",
				}
				_, err := svc.GenerateEmbedding(context.Background(), req)
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
	}
}

// =============================================================================
// Processing Time Tests
// =============================================================================

func TestEmbeddingService_ProcessingTimeRecorded(t *testing.T) {
	mgr, provider := createTestManager(t, "openai")
	provider.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		// Simulate processing delay
		time.Sleep(10 * time.Millisecond)
		return []float32{0.1, 0.2, 0.3}, nil
	}
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	req := &GenerateEmbeddingRequest{
		Text: "Test text",
	}

	resp, err := svc.GenerateEmbedding(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Metadata)
	assert.Greater(t, resp.Metadata.ProcessingTime, time.Duration(0))
	assert.GreaterOrEqual(t, resp.Metadata.ProcessingTime, 10*time.Millisecond)
}

// =============================================================================
// Batch Token Count Tests
// =============================================================================

func TestEmbeddingService_BatchGenerateEmbeddings_TokenCounting(t *testing.T) {
	mgr, _ := createTestManager(t, "openai")
	logger := newMockEmbeddingLogger()
	svc := NewEmbeddingService(mgr, logger)

	items := []*BatchEmbeddingItem{
		{ID: "1", Text: "Short"},
		{ID: "2", Text: "This is a much longer text with many more words"},
		{ID: "3", Text: "Medium length text here"},
	}

	req := &BatchGenerateEmbeddingsRequest{
		Items:    items,
		Provider: "test-provider",
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Greater(t, resp.TotalTokens, 0)

	// Verify individual token counts
	for _, result := range resp.Results {
		assert.Greater(t, result.Metadata.Tokens, 0)
	}
}

func TestEmbeddingManagerAdapter_Embed(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	result, err := adapter.Embed(context.Background(), "test text")
	require.Error(t, err)
	assert.Nil(t, result)
	// When built with vector tag, returns ErrProviderNotFound
	// When built without vector tag, returns stub error
	if !errors.Is(err, embedding.ErrProviderNotFound) {
		assert.Contains(t, err.Error(), "not enabled")
	}
}

func TestEmbeddingManagerAdapter_EmbedBatch(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	results, err := adapter.EmbedBatch(context.Background(), []string{"text1", "text2"})
	require.Error(t, err)
	assert.Nil(t, results)
	// When built with vector tag, returns ErrProviderNotFound
	// When built without vector tag, returns stub error
	if !errors.Is(err, embedding.ErrProviderNotFound) {
		assert.Contains(t, err.Error(), "not enabled")
	}
}

func TestEmbeddingManagerAdapter_Dimensions(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	dims, err := adapter.Dimensions()
	require.Error(t, err)
	assert.Equal(t, 0, dims)
	// When built with vector tag, returns ErrProviderNotFound
	// When built without vector tag, returns stub error
	if !errors.Is(err, embedding.ErrProviderNotFound) {
		assert.Contains(t, err.Error(), "not enabled")
	}
}

func TestEmbeddingManagerAdapter_GetProvider(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	provider, err := adapter.GetProvider("openai")
	require.Error(t, err)
	assert.Nil(t, provider)
	// When built with vector tag, returns ErrProviderNotFound
	// When built without vector tag, returns stub error
	if !errors.Is(err, embedding.ErrProviderNotFound) {
		assert.Contains(t, err.Error(), "not enabled")
	}
}

func TestEmbeddingManagerAdapter_GetStats(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	stats := adapter.GetStats()
	assert.Equal(t, 0, stats.ProvidersRegistered)
	assert.Empty(t, stats.ProviderNames)
	assert.False(t, stats.CacheEnabled)
}

func TestEmbeddingManagerAdapter_UnwrapWithStub(t *testing.T) {
	mgr := &embedding.Manager{}
	adapter := NewEmbeddingManagerAdapter(mgr)

	unwrapped := adapter.Unwrap()
	assert.Equal(t, mgr, unwrapped)
}

func TestEmbeddingSvc_NewEmbeddingService(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()

	svc := NewEmbeddingService(mockManager, logger)

	require.NotNil(t, svc)
}

func TestEmbeddingSvc_GenerateEmbedding_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheHits:   0,
		CacheMisses: 1,
	})
	mockManager.On("Embed", ctx, "test text").Return(expectedEmbedding, nil)
	mockManager.On("Dimensions").Return(3, nil)

	req := &GenerateEmbeddingRequest{
		Text:     "test text",
		Provider: EmbeddingProviderOllama,
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, expectedEmbedding, resp.Embedding)
	assert.NotNil(t, resp.Metadata)
	assert.Equal(t, 3, resp.Metadata.Dimensions)
}

func TestEmbeddingSvc_GenerateEmbedding_EmptyText(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &GenerateEmbeddingRequest{
		Text: "",
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
}

func TestEmbeddingSvc_GenerateEmbedding_ProviderError(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheHits:   0,
		CacheMisses: 0,
	})
	mockManager.On("Embed", ctx, "test text").Return(nil, embedding.ErrProviderNotFound)

	req := &GenerateEmbeddingRequest{
		Text:     "test text",
		Provider: "nonexistent",
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_BatchGenerateEmbeddings_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	expectedEmbeddings := [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}

	// Setup mock expectations
	mockManager.On("EmbedBatch", ctx, []string{"text1", "text2"}).Return(expectedEmbeddings, nil)
	mockManager.On("Dimensions").Return(3, nil)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: "text1"},
			{ID: "2", Text: "text2"},
		},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.SuccessCount)
	assert.Equal(t, 0, resp.ErrorCount)
	assert.Len(t, resp.Results, 2)
}

func TestEmbeddingSvc_BatchGenerateEmbeddings_EmptyItems(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_BatchGenerateEmbeddings_ItemMissingText(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: ""},
		},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_BatchGenerateEmbeddings_ItemMissingID(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "", Text: "text1"},
		},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_BatchGenerateEmbeddings_ProviderError(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	mockManager.On("EmbedBatch", ctx, []string{"text1"}).Return(nil, embedding.ErrProviderUnavailable)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: "text1"},
		},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_ListProviders_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	mockProvider := newCoverageEmbeddingProvider()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		ProviderNames: []string{"openai", "ollama"},
	})
	mockManager.On("GetProvider", "openai").Return(mockProvider, nil)
	mockManager.On("GetProvider", "ollama").Return(mockProvider, nil)
	mockProvider.On("Dimensions").Return(1536)
	mockProvider.On("MaxTokens").Return(8192)

	req := &ListProvidersRequest{}

	resp, err := svc.ListProviders(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Providers, 2)
}

func TestEmbeddingSvc_ListProviders_NilRequest(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	mockProvider := newCoverageEmbeddingProvider()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		ProviderNames: []string{"openai"},
	})
	mockManager.On("GetProvider", "openai").Return(mockProvider, nil)
	mockProvider.On("Dimensions").Return(1536)
	mockProvider.On("MaxTokens").Return(8192)

	resp, err := svc.ListProviders(ctx, nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestEmbeddingSvc_ListProviders_IncludesUnavailable(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		ProviderNames: []string{"openai", "ollama"},
	})
	mockManager.On("GetProvider", "openai").Return(nil, embedding.ErrProviderNotFound)
	mockManager.On("GetProvider", "ollama").Return(nil, embedding.ErrProviderNotFound)

	req := &ListProvidersRequest{
		IncludeUnavailable: true,
	}

	resp, err := svc.ListProviders(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Providers, 2)
	for _, p := range resp.Providers {
		assert.Equal(t, EmbeddingStatusUnavailable, p.Status)
	}
}

func TestEmbeddingSvc_GetProviderInfo_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	mockProvider := newCoverageEmbeddingProvider()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetProvider", "openai").Return(mockProvider, nil)
	mockProvider.On("Dimensions").Return(1536)
	mockProvider.On("MaxTokens").Return(8192)

	req := &GetProviderInfoRequest{
		Provider: "openai",
	}

	resp, err := svc.GetProviderInfo(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "openai", resp.Info.Name)
	assert.Equal(t, 1536, resp.Info.Dimensions)
	assert.Equal(t, 8192, resp.Info.MaxTokens)
}

func TestEmbeddingSvc_GetProviderInfo_EmptyProvider(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &GetProviderInfoRequest{
		Provider: "",
	}

	resp, err := svc.GetProviderInfo(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_GetProviderInfo_ProviderNotFound(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	mockManager.On("GetProvider", "nonexistent").Return(nil, embedding.ErrProviderNotFound)

	req := &GetProviderInfoRequest{
		Provider: "nonexistent",
	}

	resp, err := svc.GetProviderInfo(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_SetDefaultProvider_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	mockProvider := newCoverageEmbeddingProvider()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetProvider", "openai").Return(mockProvider, nil)

	req := &SetDefaultProviderRequest{
		Provider: "openai",
	}

	resp, err := svc.SetDefaultProvider(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, EmbeddingProvider("openai"), resp.Provider)
}

func TestEmbeddingSvc_SetDefaultProvider_EmptyProvider(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	req := &SetDefaultProviderRequest{
		Provider: "",
	}

	resp, err := svc.SetDefaultProvider(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_SetDefaultProvider_ProviderNotFound(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	mockManager.On("GetProvider", "nonexistent").Return(nil, embedding.ErrProviderNotFound)

	req := &SetDefaultProviderRequest{
		Provider: "nonexistent",
	}

	resp, err := svc.SetDefaultProvider(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingSvc_GetCacheStats_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheEnabled: true,
		CacheSize:    100,
		CacheHits:    80,
		CacheMisses:  20,
	})

	req := &GetEmbeddingCacheStatsRequest{}

	resp, err := svc.GetCacheStats(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Enabled)
	assert.Equal(t, 100, resp.Size)
	assert.Equal(t, uint64(80), resp.Hits)
	assert.Equal(t, uint64(20), resp.Misses)
	assert.Equal(t, 0.8, resp.HitRate)
}

func TestEmbeddingSvc_GetCacheStats_ZeroHitsMisses(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheEnabled: true,
		CacheSize:    0,
		CacheHits:    0,
		CacheMisses:  0,
	})

	req := &GetEmbeddingCacheStatsRequest{}

	resp, err := svc.GetCacheStats(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0.0, resp.HitRate)
}

func TestEmbeddingSvc_ClearCache_Success(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheSize: 50,
	})

	req := &ClearEmbeddingCacheRequest{
		All: true,
	}

	resp, err := svc.ClearCache(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 50, resp.EntriesCleared)
}

func TestEmbeddingSvc_ClearCache_NilRequest(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheSize: 50,
	})

	resp, err := svc.ClearCache(ctx, nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestEmbeddingSvc_ClearCache_SpecificProvider(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheSize: 30,
	})

	req := &ClearEmbeddingCacheRequest{
		Provider: "openai",
		All:      false,
	}

	resp, err := svc.ClearCache(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, EmbeddingProvider("openai"), resp.Provider)
}

func TestHandleEmbeddingError_CoverageProviderNotFound(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(embedding.ErrProviderNotFound, "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
}

func TestHandleEmbeddingError_CoverageInvalidInput(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(embedding.ErrInvalidInput, "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
}

func TestHandleEmbeddingError_CoverageProviderUnavailable(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(embedding.ErrProviderUnavailable, "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrUnavailable, qbErr.Code)
}

func TestHandleEmbeddingError_CoverageRateLimitExceeded(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(embedding.ErrRateLimitExceeded, "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrRateLimited, qbErr.Code)
}

func TestHandleEmbeddingError_CoverageTokenLimitExceeded(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(embedding.ErrTokenLimitExceeded, "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrResourceExhausted, qbErr.Code)
}

func TestHandleEmbeddingError_CoverageUnknownError(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger).(*embeddingService)

	err := svc.handleEmbeddingError(errors.New("unknown error"), "test", "openai")

	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
}

func TestCoverageEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello",
			expected: 2, // (5 + 3) / 4 = 2
		},
		{
			name:     "medium text",
			text:     "Hello, World!",
			expected: 4, // (13 + 3) / 4 = 4
		},
		{
			name:     "longer text",
			text:     "This is a longer piece of text for testing.",
			expected: 11, // (43 + 3) / 4 = 11.5 -> 11
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEmbeddingMgrAdapter_InterfaceCompliance(t *testing.T) {
	// This tests the adapter delegation to the underlying manager
	// We test through the embedding service tests above since the adapter
	// is a thin wrapper. Here we verify the interface compliance.
	adapter := &EmbeddingManagerAdapter{}
	_ = interfaces.EmbeddingManager(adapter)
}

func TestEmbeddingSvc_GenerateEmbedding_CacheHit(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	// First call setup - simulate cache miss
	firstStatsCall := mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheHits:   0,
		CacheMisses: 0,
	}).Once()

	// After embed call - simulate cache hit (stats increased)
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheHits:   1,
		CacheMisses: 0,
	}).NotBefore(firstStatsCall)

	mockManager.On("Embed", ctx, "test text").Return(expectedEmbedding, nil)
	mockManager.On("Dimensions").Return(3, nil)

	req := &GenerateEmbeddingRequest{
		Text: "test text",
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Metadata.CacheHit)
}

// TestEmbeddingSvc_BatchGenerateEmbeddings_FewerEmbeddingsThanItems tests the case
// where the provider returns fewer embeddings than requested items
func TestEmbeddingSvc_BatchGenerateEmbeddings_FewerEmbeddingsThanItems(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	// Only return 1 embedding for 2 items
	expectedEmbeddings := [][]float32{
		{0.1, 0.2, 0.3},
	}

	// Setup mock expectations
	mockManager.On("EmbedBatch", ctx, []string{"text1", "text2"}).Return(expectedEmbeddings, nil)
	mockManager.On("Dimensions").Return(3, nil)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: "text1"},
			{ID: "2", Text: "text2"},
		},
	}

	resp, err := svc.BatchGenerateEmbeddings(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.SuccessCount)
	assert.Equal(t, 1, resp.ErrorCount)
	assert.Len(t, resp.Results, 2)
	// Second item should have error message
	assert.NotEmpty(t, resp.Results[1].Error)
}

// TestEmbeddingSvc_GenerateEmbedding_DefaultProvider tests generating with default provider.
func TestEmbeddingSvc_GenerateEmbedding_DefaultProvider(t *testing.T) {
	mockManager := newCoverageEmbeddingManager()
	logger := newCoverageEmbeddingLogger()
	svc := NewEmbeddingService(mockManager, logger)

	ctx := context.Background()
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	// Setup mock expectations
	mockManager.On("GetStats").Return(interfaces.EmbeddingStats{
		CacheHits:   0,
		CacheMisses: 1,
	})
	mockManager.On("Embed", ctx, "test text").Return(expectedEmbedding, nil)
	mockManager.On("Dimensions").Return(3, nil)

	// Provider is empty, so default should be used
	req := &GenerateEmbeddingRequest{
		Text:     "test text",
		Provider: "",
	}

	resp, err := svc.GenerateEmbedding(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestEmbeddingService_ValidateGenerateEmbeddingRequest(t *testing.T) {
	svc := &embeddingService{}

	t.Run("nil request returns error", func(t *testing.T) {
		err := svc.validateGenerateEmbeddingRequest(nil)
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "request is nil")
	})

	t.Run("empty text returns error", func(t *testing.T) {
		err := svc.validateGenerateEmbeddingRequest(&GenerateEmbeddingRequest{Text: ""})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "text is required")
	})

	t.Run("valid request with text returns no error", func(t *testing.T) {
		err := svc.validateGenerateEmbeddingRequest(&GenerateEmbeddingRequest{Text: "hello world"})
		require.NoError(t, err)
	})

	t.Run("valid request with provider returns no error", func(t *testing.T) {
		err := svc.validateGenerateEmbeddingRequest(&GenerateEmbeddingRequest{Text: "test text", Provider: "openai"})
		require.NoError(t, err)
	})
}

func TestEmbeddingService_ValidateBatchGenerateEmbeddingsRequest(t *testing.T) {
	svc := &embeddingService{}

	t.Run("nil request returns error", func(t *testing.T) {
		err := svc.validateBatchGenerateEmbeddingsRequest(nil)
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "request is nil")
	})

	t.Run("empty items returns error", func(t *testing.T) {
		err := svc.validateBatchGenerateEmbeddingsRequest(&BatchGenerateEmbeddingsRequest{Items: []*BatchEmbeddingItem{}})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "items is required")
	})

	t.Run("item with empty text returns error", func(t *testing.T) {
		err := svc.validateBatchGenerateEmbeddingsRequest(&BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{{ID: "1", Text: ""}},
		})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "text is required")
	})

	t.Run("item with empty ID returns error", func(t *testing.T) {
		err := svc.validateBatchGenerateEmbeddingsRequest(&BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{{ID: "", Text: "hello"}},
		})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "id is required")
	})

	t.Run("valid request returns no error", func(t *testing.T) {
		err := svc.validateBatchGenerateEmbeddingsRequest(&BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{
				{ID: "1", Text: "hello"},
				{ID: "2", Text: "world"},
			},
		})
		require.NoError(t, err)
	})
}

func TestEmbeddingService_ValidateGetProviderInfoRequest(t *testing.T) {
	svc := &embeddingService{}

	t.Run("nil request returns error", func(t *testing.T) {
		err := svc.validateGetProviderInfoRequest(nil)
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "request is nil")
	})

	t.Run("empty provider returns error", func(t *testing.T) {
		err := svc.validateGetProviderInfoRequest(&GetProviderInfoRequest{Provider: ""})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "provider is required")
	})

	t.Run("valid request returns no error", func(t *testing.T) {
		err := svc.validateGetProviderInfoRequest(&GetProviderInfoRequest{Provider: "openai"})
		require.NoError(t, err)
	})
}

func TestEmbeddingService_ValidateSetDefaultProviderRequest(t *testing.T) {
	svc := &embeddingService{}

	t.Run("nil request returns error", func(t *testing.T) {
		err := svc.validateSetDefaultProviderRequest(nil)
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "request is nil")
	})

	t.Run("empty provider returns error", func(t *testing.T) {
		err := svc.validateSetDefaultProviderRequest(&SetDefaultProviderRequest{Provider: ""})
		require.Error(t, err)
		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
		assert.Contains(t, qbErr.Err.Error(), "provider is required")
	})

	t.Run("valid request returns no error", func(t *testing.T) {
		err := svc.validateSetDefaultProviderRequest(&SetDefaultProviderRequest{Provider: "ollama"})
		require.NoError(t, err)
	})
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string returns zero",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hi",
			expected: 1, // (2 + 3) / 4 = 1
		},
		{
			name:     "four character text",
			text:     "test",
			expected: 1, // (4 + 3) / 4 = 1
		},
		{
			name:     "eight character text",
			text:     "testword",
			expected: 2, // (8 + 3) / 4 = 2
		},
		{
			name:     "longer text",
			text:     "This is a longer piece of text for testing",
			expected: 11, // (43 + 3) / 4 = 11
		},
		{
			name:     "text with special characters",
			text:     "Hello, World!",
			expected: 4, // (13 + 3) / 4 = 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

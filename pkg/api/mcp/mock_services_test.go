//go:build mcp

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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Embedding Service
// =============================================================================

type mockEmbeddingService struct {
	generateFunc      func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error)
	batchFunc         func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error)
	listProvidersFunc func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error)
	getProviderFunc   func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error)
	setDefaultFunc    func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error)
	cacheStatsFunc    func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error)
	cacheClearFunc    func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error)
}

func (m *mockEmbeddingService) GenerateEmbedding(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, req)
	}
	return &service.GenerateEmbeddingResponse{
		Embedding: []float32{0.1, 0.2, 0.3},
		Metadata: &service.EmbeddingMetadata{
			Model:          "test-model",
			Provider:       "ollama",
			Dimensions:     3,
			Tokens:         10,
			CacheHit:       false,
			ProcessingTime: 50 * time.Millisecond,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) BatchGenerateEmbeddings(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	if m.batchFunc != nil {
		return m.batchFunc(ctx, req)
	}
	results := make([]*service.BatchEmbeddingResult, len(req.Items))
	for i, item := range req.Items {
		results[i] = &service.BatchEmbeddingResult{
			ID:        item.ID,
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: &service.EmbeddingMetadata{
				Model:      "test-model",
				Provider:   "ollama",
				Dimensions: 3,
			},
		}
	}
	return &service.BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   len(req.Items),
		ErrorCount:     0,
		TotalTokens:    len(req.Items) * 10,
		ProcessingTime: 100 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockEmbeddingService) ListProviders(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	if m.listProvidersFunc != nil {
		return m.listProvidersFunc(ctx, req)
	}
	return &service.ListProvidersResponse{
		Providers: []*service.ProviderInfo{
			{Name: "ollama", Type: "ollama", Model: "nomic-embed-text", Dimensions: 768, Status: "available", IsDefault: true},
		},
		DefaultProvider: "ollama",
		Timestamp:       time.Now(),
	}, nil
}

func (m *mockEmbeddingService) GetProviderInfo(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	if m.getProviderFunc != nil {
		return m.getProviderFunc(ctx, req)
	}
	return &service.GetProviderInfoResponse{
		Info: &service.ProviderInfo{
			Name: string(req.Provider), Type: req.Provider, Model: "test-model",
			Dimensions: 768, Status: "available", IsDefault: true,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) SetDefaultProvider(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	if m.setDefaultFunc != nil {
		return m.setDefaultFunc(ctx, req)
	}
	return &service.SetDefaultProviderResponse{
		Provider:         req.Provider,
		PreviousProvider: "ollama",
		Timestamp:        time.Now(),
	}, nil
}

func (m *mockEmbeddingService) GetCacheStats(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	if m.cacheStatsFunc != nil {
		return m.cacheStatsFunc(ctx, req)
	}
	return &service.GetEmbeddingCacheStatsResponse{
		Enabled: true, Size: 100, Capacity: 1000,
		Hits: 500, Misses: 100, HitRate: 0.83,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) ClearCache(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	if m.cacheClearFunc != nil {
		return m.cacheClearFunc(ctx, req)
	}
	return &service.ClearEmbeddingCacheResponse{
		EntriesCleared: 100, Provider: req.Provider, Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Mock Ollama Service
// =============================================================================

type mockOllamaService struct {
	embedFunc      func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error)
	embedModelFunc func(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error)
	embedBatchFunc func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error)
	listModelsFunc func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error)
	getModelFunc   func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error)
	healthFunc     func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error)
}

func (m *mockOllamaService) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding: []float32{0.1, 0.2, 0.3}, Model: "nomic-embed-text",
		Dimensions: 3, ProcessingTime: 50 * time.Millisecond, Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedModelFunc != nil {
		return m.embedModelFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding: []float32{0.4, 0.5, 0.6}, Model: req.Model,
		Dimensions: 3, ProcessingTime: 50 * time.Millisecond, Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	if m.embedBatchFunc != nil {
		return m.embedBatchFunc(ctx, req)
	}
	embeddings := make([][]float32, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = []float32{float32(i) * 0.1, float32(i) * 0.2}
	}
	return &service.OllamaEmbedBatchResponse{
		Embeddings: embeddings, Model: "nomic-embed-text", Dimensions: 2,
		Count: len(req.Texts), ProcessingTime: 100 * time.Millisecond, Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx, req)
	}
	return &service.OllamaListModelsResponse{
		Models: []service.OllamaModelInfo{
			{Name: "nomic-embed-text", Size: 274000000, Digest: "abc123"},
		},
		Count: 1, Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	if m.getModelFunc != nil {
		return m.getModelFunc(ctx, req)
	}
	return &service.OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: service.OllamaModelDetails{
			Family: "nomic", Format: "gguf", ParameterSize: "137M",
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	if m.healthFunc != nil {
		return m.healthFunc(ctx, req)
	}
	return &service.OllamaHealthResponse{
		Available: true, Version: "0.3.0", BaseURL: "http://localhost:11434",
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) IsAvailable() bool    { return true }
func (m *mockOllamaService) DefaultModel() string { return "nomic-embed-text" }
func (m *mockOllamaService) BaseURL() string      { return "http://localhost:11434" }

// =============================================================================
// Mock Scheduler Service
// =============================================================================

type mockSchedulerService struct {
	createJobFunc  func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	getJobFunc     func(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	updateJobFunc  func(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	deleteJobFunc  func(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	listJobsFunc   func(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	triggerJobFunc func(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	pauseJobFunc   func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	resumeJobFunc  func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	cancelJobFunc  func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	historyFunc    func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	statusFunc     func(ctx context.Context) (*service.GetSchedulerStatusResponse, error)
}

func (m *mockSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if m.createJobFunc != nil {
		return m.createJobFunc(ctx, req)
	}
	return &service.CreateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID: 1, Name: req.Name, Handler: req.Handler, Type: req.Type,
			Status: service.SchedulerJobStatusPending, CreatedAt: time.Now(),
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, req)
	}
	return &service.GetSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID: req.JobID, Name: "test-job", Handler: "test-handler",
			Status: service.SchedulerJobStatusPending,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if m.updateJobFunc != nil {
		return m.updateJobFunc(ctx, req)
	}
	return &service.UpdateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID: req.JobID, Name: req.Name, Handler: req.Handler,
			Status: service.SchedulerJobStatusPending, UpdatedAt: time.Now(),
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if m.deleteJobFunc != nil {
		return m.deleteJobFunc(ctx, req)
	}
	return &service.DeleteSchedulerJobResponse{
		JobID: req.JobID, Deleted: true, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, req)
	}
	return &service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{ID: 1, Name: "job-1", Handler: "handler-1", Status: service.SchedulerJobStatusPending},
		},
		HasMore: false, TotalCount: 1, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if m.triggerJobFunc != nil {
		return m.triggerJobFunc(ctx, req)
	}
	return &service.TriggerSchedulerJobResponse{
		JobID: req.JobID, Status: service.SchedulerJobStatusRunning, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if m.pauseJobFunc != nil {
		return m.pauseJobFunc(ctx, req)
	}
	return &service.PauseSchedulerJobResponse{
		JobID: req.JobID, OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusPaused, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if m.resumeJobFunc != nil {
		return m.resumeJobFunc(ctx, req)
	}
	return &service.ResumeSchedulerJobResponse{
		JobID: req.JobID, OldStatus: service.SchedulerJobStatusPaused,
		NewStatus: service.SchedulerJobStatusPending, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if m.cancelJobFunc != nil {
		return m.cancelJobFunc(ctx, req)
	}
	return &service.CancelSchedulerJobResponse{
		JobID: req.JobID, OldStatus: service.SchedulerJobStatusPending,
		NewStatus: service.SchedulerJobStatusCanceled, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if m.historyFunc != nil {
		return m.historyFunc(ctx, req)
	}
	return &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{ID: 1, JobID: req.JobID, Status: service.SchedulerJobStatusCompleted},
		},
		HasMore: false, Timestamp: time.Now(),
	}, nil
}

func (m *mockSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return &service.GetSchedulerStatusResponse{
		Running:   true,
		Stats:     &service.SchedulerStats{JobCount: 5, RunningJobs: 1, SuccessCount: 10},
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Mock Parameter Store Service
// =============================================================================

type mockParameterStoreService struct {
	getFunc      func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error)
	setFunc      func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error)
	deleteFunc   func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error)
	getMultiFunc func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error)
	setMultiFunc func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error)
	delMultiFunc func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error)
	listFunc     func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error)
	searchFunc   func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error)
}

func (m *mockParameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &interfaces.GetParameterResponse{
		Parameter: &interfaces.ParameterValue{
			Path: req.Path, Value: []byte("test-value"), Type: "string",
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	if m.setFunc != nil {
		return m.setFunc(ctx, req)
	}
	return &interfaces.SetParameterResponse{
		Path: req.Path, Created: true, Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &interfaces.DeleteParameterResponse{
		Path: req.Path, Deleted: true, Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	if m.getMultiFunc != nil {
		return m.getMultiFunc(ctx, req)
	}
	params := make([]*interfaces.ParameterValue, len(req.Paths))
	for i, path := range req.Paths {
		params[i] = &interfaces.ParameterValue{Path: path, Value: []byte("val"), Type: "string"}
	}
	return &interfaces.GetParametersResponse{
		Parameters: params, Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	if m.setMultiFunc != nil {
		return m.setMultiFunc(ctx, req)
	}
	results := make([]*interfaces.SetParameterResult, len(req.Parameters))
	for i, p := range req.Parameters {
		results[i] = &interfaces.SetParameterResult{Path: p.Path, Created: true}
	}
	return &interfaces.SetParametersResponse{
		Results: results, Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	if m.delMultiFunc != nil {
		return m.delMultiFunc(ctx, req)
	}
	results := make([]*interfaces.DeleteParameterResult, len(req.Paths))
	for i, path := range req.Paths {
		results[i] = &interfaces.DeleteParameterResult{Path: path, Deleted: true}
	}
	return &interfaces.DeleteParametersResponse{
		Results: results, Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return &interfaces.ListParametersResponse{
		Parameters: []*interfaces.ParameterValue{
			{Path: "/app/config", Value: []byte("value"), Type: "string"},
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, req)
	}
	return &interfaces.SearchByTagsResponse{
		Parameters: []*interfaces.ParameterValue{
			{Path: "/app/config", Value: []byte("value"), Type: "string", Tags: req.Tags},
		},
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Mock Messaging Service
// =============================================================================

type mockMessagingService struct {
	createFunc    func(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error)
	deleteFunc    func(ctx context.Context, name string) error
	getFunc       func(ctx context.Context, name string) (*messaging.Channel, error)
	listFunc      func(ctx context.Context) ([]*messaging.Channel, error)
	subscribeFunc func(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error)
	unsubFunc     func(ctx context.Context, subscriptionID string) error
	publishFunc   func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	pubDeltaFunc  func(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error)
	statsResult   *messaging.Stats
}

func (m *mockMessagingService) CreateChannel(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, name, opts...)
	}
	return &messaging.Channel{
		Name: name, Codec: "json", CreatedAt: time.Now(),
		BufferSize: 100, MaxHistory: 50,
	}, nil
}

func (m *mockMessagingService) DeleteChannel(ctx context.Context, name string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, name)
	}
	return nil
}

func (m *mockMessagingService) GetChannel(ctx context.Context, name string) (*messaging.Channel, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, name)
	}
	return &messaging.Channel{
		Name: name, Codec: "json", CreatedAt: time.Now(),
		BufferSize: 100, Subscribers: 3,
	}, nil
}

func (m *mockMessagingService) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []*messaging.Channel{
		{Name: "events", Codec: "json", CreatedAt: time.Now()},
		{Name: "logs", Codec: "json", CreatedAt: time.Now()},
	}, nil
}

func (m *mockMessagingService) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, channel, opts...)
	}
	return &messaging.Subscription{
		ID: "sub-1", Channel: channel, ClientID: "client-1", CreatedAt: time.Now(),
	}, nil
}

func (m *mockMessagingService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if m.unsubFunc != nil {
		return m.unsubFunc(ctx, subscriptionID)
	}
	return nil
}

func (m *mockMessagingService) Publish(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, channel, payload, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *mockMessagingService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.pubDeltaFunc != nil {
		return m.pubDeltaFunc(ctx, channel, old, new, opts...)
	}
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (m *mockMessagingService) WatchChannel(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return &messaging.Subscription{
		ID: "watch-1", Channel: keyPrefix, ClientID: "client-1", CreatedAt: time.Now(),
	}, nil
}

func (m *mockMessagingService) Start(ctx context.Context) error { return nil }
func (m *mockMessagingService) Stop(ctx context.Context) error  { return nil }
func (m *mockMessagingService) IsRunning() bool                 { return true }

func (m *mockMessagingService) GetStats() *messaging.Stats {
	if m.statsResult != nil {
		return m.statsResult
	}
	return &messaging.Stats{
		TotalChannels: 2, TotalSubscriptions: 5, MessagesPublished: 100,
		MessagesDelivered: 95, DedupHits: 3, DroppedMessages: 2,
		BufferUtilization: 0.45,
	}
}

// =============================================================================
// Extended Mock Service
// =============================================================================

// extendedMockService extends mockService with configurable sub-services.
type extendedMockService struct {
	mockService
	embeddingSvc    service.EmbeddingService
	ollamaSvc       service.OllamaService
	schedulerSvc    service.SchedulerService
	parameterSvc    service.ParameterStoreService
	messagingSvc    messaging.Service
	cacheConfigFunc func(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error)
	updateCacheFunc func(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error)
}

func (m *extendedMockService) Embedding() service.EmbeddingService {
	return m.embeddingSvc
}

func (m *extendedMockService) Ollama() service.OllamaService {
	return m.ollamaSvc
}

func (m *extendedMockService) Scheduler() service.SchedulerService {
	return m.schedulerSvc
}

func (m *extendedMockService) ParameterStoreService() service.ParameterStoreService {
	return m.parameterSvc
}

func (m *extendedMockService) Messaging() messaging.Service {
	return m.messagingSvc
}

func (m *extendedMockService) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	if m.cacheConfigFunc != nil {
		return m.cacheConfigFunc(ctx, req)
	}
	return &service.GetCacheConfigResponse{
		L1Size: 1000, L1TTL: 300 * time.Second,
		CleanupInterval: 60 * time.Second, Enabled: true,
	}, nil
}

func (m *extendedMockService) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	if m.updateCacheFunc != nil {
		return m.updateCacheFunc(ctx, req)
	}
	return &service.UpdateCacheConfigResponse{
		Updated: true,
		NewConfig: &service.GetCacheConfigResponse{
			L1Size: 2000, L1TTL: 600 * time.Second,
			CleanupInterval: 120 * time.Second, Enabled: true,
		},
	}, nil
}

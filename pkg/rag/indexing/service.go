//go:build rag

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

package indexing

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/watcher"
)

// IndexEventType classifies the kind of indexing event that occurred.
type IndexEventType int

const (
	// IndexEventFileAdded indicates a new file was indexed.
	IndexEventFileAdded IndexEventType = iota

	// IndexEventFileUpdated indicates an existing file was re-indexed.
	IndexEventFileUpdated

	// IndexEventFileRemoved indicates a file was removed from the index.
	IndexEventFileRemoved

	// IndexEventBatchComplete indicates a batch indexing operation completed.
	IndexEventBatchComplete

	// IndexEventError indicates an error occurred during indexing.
	IndexEventError
)

// IndexEvent describes an indexing event for observability.
type IndexEvent struct {
	Type       IndexEventType
	ProjectID  string
	FilePath   string
	ChunkCount int
	Duration   time.Duration
	Error      error
}

// IndexStats provides a snapshot of the indexing service state.
type IndexStats struct {
	FilesIndexed  int
	TotalChunks   int
	TotalVectors  int
	IndexBackend  string
	LastIndexedAt int64
	IsRunning     bool
}

// IndexingService coordinates file watching with the indexing pipeline.
type IndexingService struct {
	config   IndexingConfig
	pipeline *IndexingPipeline
	watcher  watcher.Watcher
	metadata MetadataStore
	logger   interfaces.Logger
	eventCh  chan IndexEvent
	running  atomic.Bool
	stopCh   chan struct{}
	doneCh   chan struct{}

	// Tracking stats using atomics for lock-free reads.
	filesIndexed  atomic.Int64
	totalChunks   atomic.Int64
	totalVectors  atomic.Int64
	lastIndexedAt atomic.Int64

	// watcherHandlers maps event types to handler methods for O(1) dispatch.
	watcherHandlers map[watcher.EventType]watcherEventHandler
}

// NewIndexingService creates a new IndexingService. It constructs the watcher and
// pipeline from the provided dependencies.
func NewIndexingService(
	cfg IndexingConfig,
	embedder embedding.Provider,
	index rag.VectorIndex,
	kvStore interfaces.KVStore,
	logger interfaces.Logger,
) (*IndexingService, error) {
	if !cfg.Enabled {
		return nil, &IndexingDisabledError{}
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metadataStore := NewMetadataStore(kvStore)
	pipeline := NewIndexingPipeline(cfg, embedder, index, metadataStore, logger)

	// Configure file watcher.
	watchCfg := watcher.Config{
		Paths:          []string{cfg.ProjectPath},
		Exclude:        cfg.WatchExclusions,
		DebounceDelay:  100 * time.Millisecond,
		BatchInterval:  1 * time.Second,
		IgnoreHidden:   true,
		FollowSymlinks: false,
		MaxDepth:       0,
		BufferSize:     1000,
		Recursive:      true,
	}

	w, err := watcher.New(watchCfg)
	if err != nil {
		return nil, err
	}

	svc := &IndexingService{
		config:   cfg,
		pipeline: pipeline,
		watcher:  w,
		metadata: metadataStore,
		logger:   logger,
		eventCh:  make(chan IndexEvent, 256),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	svc.initWatcherHandlers()
	return svc, nil
}

// newIndexingServiceWithWatcher creates an IndexingService with an externally
// provided watcher. This is used for testing to inject a controllable watcher.
func newIndexingServiceWithWatcher(
	cfg IndexingConfig,
	pipeline *IndexingPipeline,
	w watcher.Watcher,
	metadata MetadataStore,
	logger interfaces.Logger,
) *IndexingService {
	svc := &IndexingService{
		config:   cfg,
		pipeline: pipeline,
		watcher:  w,
		metadata: metadata,
		logger:   logger,
		eventCh:  make(chan IndexEvent, 256),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	svc.initWatcherHandlers()
	return svc
}

// Start begins the event loop that watches for file changes and indexes them.
func (s *IndexingService) Start(ctx context.Context) error {
	if s.running.Swap(true) {
		return &IndexingAlreadyRunningError{}
	}

	if err := s.watcher.Start(ctx); err != nil {
		s.running.Store(false)
		return err
	}

	go s.eventLoop(ctx)
	return nil
}

// Stop gracefully shuts down the indexing service.
func (s *IndexingService) Stop() error {
	if !s.running.Swap(false) {
		return &IndexingNotRunningError{}
	}

	close(s.stopCh)

	// Wait for the event loop to finish.
	<-s.doneCh

	return s.watcher.Stop()
}

// Events returns a read-only channel for observing indexing events.
func (s *IndexingService) Events() <-chan IndexEvent {
	return s.eventCh
}

// TriggerReindex performs a full reindex of all tracked files.
func (s *IndexingService) TriggerReindex(ctx context.Context) error {
	if !s.running.Load() {
		return &IndexingNotRunningError{}
	}

	records, err := s.metadata.ListFileRecords(ctx, s.config.Namespace)
	if err != nil {
		return err
	}

	paths := make([]string, len(records))
	for i, r := range records {
		paths[i] = r.FilePath
	}

	return s.IndexFiles(ctx, paths)
}

// IndexFiles indexes a specific list of file paths.
func (s *IndexingService) IndexFiles(ctx context.Context, paths []string) error {
	result, err := s.pipeline.IndexBatch(ctx, paths)
	if err != nil {
		return err
	}

	for _, res := range result.Results {
		if res.Skipped {
			continue
		}
		s.filesIndexed.Add(1)
		s.totalChunks.Add(int64(res.ChunkCount))
		s.totalVectors.Add(int64(len(res.VectorIDs)))
		s.lastIndexedAt.Store(time.Now().Unix())
	}

	s.emitEvent(IndexEvent{
		Type:       IndexEventBatchComplete,
		ProjectID:  s.config.ProjectID,
		ChunkCount: result.Indexed,
		Duration:   result.Duration,
	})

	return nil
}

// Stats returns a snapshot of the indexing service statistics.
func (s *IndexingService) Stats() *IndexStats {
	return &IndexStats{
		FilesIndexed:  int(s.filesIndexed.Load()),
		TotalChunks:   int(s.totalChunks.Load()),
		TotalVectors:  int(s.totalVectors.Load()),
		IndexBackend:  s.config.IndexType,
		LastIndexedAt: s.lastIndexedAt.Load(),
		IsRunning:     s.running.Load(),
	}
}

// eventLoop processes watcher events and drives the indexing pipeline.
func (s *IndexingService) eventLoop(ctx context.Context) {
	defer close(s.doneCh)

	watchEvents := s.watcher.Events()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case evt, ok := <-watchEvents:
			if !ok {
				return
			}
			s.handleWatcherEvent(ctx, evt)
		}
	}
}

// initWatcherHandlers populates the watcher event handler dispatch map.
// Called once during construction to avoid re-creating the map on every event.
func (s *IndexingService) initWatcherHandlers() {
	s.watcherHandlers = map[watcher.EventType]watcherEventHandler{
		watcher.EventCreate: s.handleFileCreateOrModify,
		watcher.EventModify: s.handleFileCreateOrModify,
		watcher.EventDelete: s.handleFileDelete,
		watcher.EventRename: s.handleFileRename,
	}
}

// watcherEventHandlers maps watcher event types to handler methods for O(1) dispatch.
type watcherEventHandler func(ctx context.Context, evt watcher.Event)

// handleWatcherEvent dispatches a watcher event to the appropriate handler.
func (s *IndexingService) handleWatcherEvent(ctx context.Context, evt watcher.Event) {
	handler, ok := s.watcherHandlers[evt.Type]
	if !ok {
		s.logger.Warnf("unhandled watcher event type: %s", evt.Type.String())
		return
	}
	handler(ctx, evt)
}

// handleFileCreateOrModify indexes a newly created or modified file.
func (s *IndexingService) handleFileCreateOrModify(ctx context.Context, evt watcher.Event) {
	start := time.Now()
	result, err := s.pipeline.IndexFile(ctx, evt.Path)
	if err != nil {
		s.emitEvent(IndexEvent{
			Type:      IndexEventError,
			ProjectID: s.config.ProjectID,
			FilePath:  evt.Path,
			Error:     err,
			Duration:  time.Since(start),
		})
		s.logger.Errorf("indexing failed for %s: %v", evt.Path, err)
		return
	}

	if result.Skipped {
		return
	}

	eventType := IndexEventFileAdded
	if evt.Type == watcher.EventModify {
		eventType = IndexEventFileUpdated
	}

	s.filesIndexed.Add(1)
	s.totalChunks.Add(int64(result.ChunkCount))
	s.totalVectors.Add(int64(len(result.VectorIDs)))
	s.lastIndexedAt.Store(time.Now().Unix())

	s.emitEvent(IndexEvent{
		Type:       eventType,
		ProjectID:  s.config.ProjectID,
		FilePath:   evt.Path,
		ChunkCount: result.ChunkCount,
		Duration:   result.Duration,
	})
}

// handleFileDelete removes a file from the index.
func (s *IndexingService) handleFileDelete(ctx context.Context, evt watcher.Event) {
	start := time.Now()
	if err := s.pipeline.RemoveFile(ctx, evt.Path); err != nil {
		s.emitEvent(IndexEvent{
			Type:      IndexEventError,
			ProjectID: s.config.ProjectID,
			FilePath:  evt.Path,
			Error:     err,
			Duration:  time.Since(start),
		})
		s.logger.Errorf("removal failed for %s: %v", evt.Path, err)
		return
	}

	s.emitEvent(IndexEvent{
		Type:      IndexEventFileRemoved,
		ProjectID: s.config.ProjectID,
		FilePath:  evt.Path,
		Duration:  time.Since(start),
	})
}

// handleFileRename removes the old path and indexes the new path.
func (s *IndexingService) handleFileRename(ctx context.Context, evt watcher.Event) {
	// Remove old path.
	if evt.OldPath != "" {
		if err := s.pipeline.RemoveFile(ctx, evt.OldPath); err != nil {
			s.logger.Warnf("failed to remove old path %s during rename: %v", evt.OldPath, err)
		}
	}

	// Index new path.
	s.handleFileCreateOrModify(ctx, evt)
}

// emitEvent sends an event to the event channel without blocking.
func (s *IndexingService) emitEvent(evt IndexEvent) {
	select {
	case s.eventCh <- evt:
	default:
		s.logger.Warn("indexing event channel full, dropping event")
	}
}

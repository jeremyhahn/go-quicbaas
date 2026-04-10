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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/watcher"
)

// newTestService creates a testable IndexingService with a controllable watcher.
func newTestService(t *testing.T) (*IndexingService, *testWatcher, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dimensions := 64

	cfg := IndexingConfig{
		Enabled:        true,
		IndexType:      "memory",
		BatchSize:      32,
		MaxConcurrency: 2,
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
		ProjectPath:    tmpDir,
		ProjectID:      "test-project",
		Namespace:      "test-ns",
	}

	embedder := newTestEmbeddingProvider(dimensions)
	index, err := rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     dimensions,
		DistanceMetric: rag.CosineDistance,
	})
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	kvStore := newMemoryKVStore()
	metadata := NewMetadataStore(kvStore)
	logger := newTestLogger()
	pipeline := NewIndexingPipeline(cfg, embedder, index, metadata, logger)
	tw := newTestWatcher()

	svc := newIndexingServiceWithWatcher(cfg, pipeline, tw, metadata, logger)
	return svc, tw, tmpDir
}

func TestIndexingService_StartStop_Success(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := svc.Stats()
	if !stats.IsRunning {
		t.Fatal("expected service to be running")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	stats = svc.Stats()
	if stats.IsRunning {
		t.Fatal("expected service to be stopped")
	}
}

func TestIndexingService_DoubleStart_Error(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		if stopErr := svc.Stop(); stopErr != nil {
			t.Logf("Stop error during cleanup: %v", stopErr)
		}
	}()

	err := svc.Start(ctx)
	if err == nil {
		t.Fatal("expected error on double start")
	}
	var alreadyRunning *IndexingAlreadyRunningError
	if !errors.As(err, &alreadyRunning) {
		t.Fatalf("expected *IndexingAlreadyRunningError, got %T", err)
	}
}

func TestIndexingService_StopWithoutStart_Error(t *testing.T) {
	svc, _, _ := newTestService(t)

	err := svc.Stop()
	if err == nil {
		t.Fatal("expected error on stop without start")
	}
	var notRunning *IndexingNotRunningError
	if !errors.As(err, &notRunning) {
		t.Fatalf("expected *IndexingNotRunningError, got %T", err)
	}
}

func TestIndexingService_FileCreateEvent(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "new.go")
	content := "package main\n\nfunc New() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventCreate,
		Path:      goFile,
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileAdded {
			t.Fatalf("expected IndexEventFileAdded, got %d", evt.Type)
		}
		if evt.FilePath != goFile {
			t.Fatalf("expected file path %s, got %s", goFile, evt.FilePath)
		}
		if evt.ChunkCount == 0 {
			t.Fatal("expected at least one chunk")
		}
	case <-timer.C:
		t.Fatal("timed out waiting for index event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_FileDeleteEvent(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "delete.go")
	content := "package main\n\nfunc Delete() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if _, err := svc.pipeline.IndexFile(ctx, goFile); err != nil {
		t.Fatalf("pre-index failed: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventDelete,
		Path:      goFile,
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileRemoved {
			t.Fatalf("expected IndexEventFileRemoved, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for delete event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_FileModifyEvent(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "modify.go")
	content := "package main\n\nfunc Modify() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventModify,
		Path:      goFile,
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileUpdated {
			t.Fatalf("expected IndexEventFileUpdated, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for modify event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_Stats_Initial(t *testing.T) {
	svc, _, _ := newTestService(t)

	stats := svc.Stats()
	if stats.FilesIndexed != 0 {
		t.Fatalf("expected 0 files indexed, got %d", stats.FilesIndexed)
	}
	if stats.TotalChunks != 0 {
		t.Fatalf("expected 0 total chunks, got %d", stats.TotalChunks)
	}
	if stats.IsRunning {
		t.Fatal("expected service not running initially")
	}
	if stats.IndexBackend != "memory" {
		t.Fatalf("expected index backend memory, got %s", stats.IndexBackend)
	}
}

func TestIndexingService_Stats_AfterIndexing(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "stats.go")
	content := "package main\n\nfunc Stats() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventCreate,
		Path:      goFile,
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-svc.Events():
	case <-timer.C:
		t.Fatal("timed out waiting for event")
	}

	stats := svc.Stats()
	if stats.FilesIndexed < 1 {
		t.Fatalf("expected at least 1 file indexed, got %d", stats.FilesIndexed)
	}
	if stats.LastIndexedAt == 0 {
		t.Fatal("expected LastIndexedAt to be set")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_IndexFiles_Manual(t *testing.T) {
	svc, _, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "manual.go")
	content := "package main\n\nfunc Manual() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := svc.IndexFiles(ctx, []string{goFile}); err != nil {
		t.Fatalf("IndexFiles failed: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventBatchComplete {
			t.Fatalf("expected IndexEventBatchComplete, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for batch complete event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_TriggerReindex_NotRunning(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	err := svc.TriggerReindex(ctx)
	if err == nil {
		t.Fatal("expected error when triggering reindex on stopped service")
	}
	var notRunning *IndexingNotRunningError
	if !errors.As(err, &notRunning) {
		t.Fatalf("expected *IndexingNotRunningError, got %T", err)
	}
}

func TestIndexingService_TriggerReindex_Success(t *testing.T) {
	svc, _, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "reindex.go")
	content := "package main\n\nfunc Reindex() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if _, err := svc.pipeline.IndexFile(ctx, goFile); err != nil {
		t.Fatalf("pre-index failed: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := svc.TriggerReindex(ctx); err != nil {
		t.Fatalf("TriggerReindex failed: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventBatchComplete {
			t.Fatalf("expected IndexEventBatchComplete, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for batch complete event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_ContextCancellation(t *testing.T) {
	svc, _, _ := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-svc.doneCh:
	case <-timer.C:
		t.Fatal("timed out waiting for event loop to exit after context cancellation")
	}
}

func TestIndexingService_RenameEvent(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	oldFile := filepath.Join(tmpDir, "old.go")
	newFile := filepath.Join(tmpDir, "new_renamed.go")
	content := "package main\n\nfunc Renamed() {}\n"
	if err := os.WriteFile(oldFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if _, err := svc.pipeline.IndexFile(ctx, oldFile); err != nil {
		t.Fatalf("pre-index failed: %v", err)
	}

	if err := os.WriteFile(newFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write renamed file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventRename,
		Path:      newFile,
		OldPath:   oldFile,
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileAdded {
			t.Fatalf("expected IndexEventFileAdded for rename, got %d", evt.Type)
		}
		if evt.FilePath != newFile {
			t.Fatalf("expected file path %s, got %s", newFile, evt.FilePath)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for rename event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_RenameEvent_NoOldPath(t *testing.T) {
	svc, tw, tmpDir := newTestService(t)
	ctx := context.Background()

	newFile := filepath.Join(tmpDir, "rename_no_old.go")
	content := "package main\n\nfunc RenameNoOld() {}\n"
	if err := os.WriteFile(newFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Rename event with empty OldPath.
	tw.SendEvent(watcher.Event{
		Type:      watcher.EventRename,
		Path:      newFile,
		OldPath:   "",
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileAdded {
			t.Fatalf("expected IndexEventFileAdded, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for rename event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestNewIndexingService_Disabled(t *testing.T) {
	cfg := IndexingConfig{Enabled: false}
	_, err := NewIndexingService(cfg, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for disabled config")
	}
	var disabled *IndexingDisabledError
	if !errors.As(err, &disabled) {
		t.Fatalf("expected *IndexingDisabledError, got %T", err)
	}
}

func TestNewIndexingService_InvalidConfig(t *testing.T) {
	cfg := IndexingConfig{
		Enabled:   true,
		IndexType: "bogus",
	}
	_, err := NewIndexingService(cfg, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNewIndexingService_Success(t *testing.T) {
	tmpDir := t.TempDir()
	dimensions := 64

	cfg := IndexingConfig{
		Enabled:         true,
		IndexType:       "memory",
		BatchSize:       32,
		MaxConcurrency:  2,
		ChunkMaxTokens:  2000,
		ChunkOverlap:    200,
		ProjectPath:     tmpDir,
		ProjectID:       "test-project",
		Namespace:       "test-ns",
		Triggers:        []string{string(TriggerFileChange)},
		WatchExclusions: []string{".git/"},
	}

	embedder := newTestEmbeddingProvider(dimensions)
	index, err := rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     dimensions,
		DistanceMetric: rag.CosineDistance,
	})
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	kvStore := newMemoryKVStore()
	logger := newTestLogger()

	svc, err := NewIndexingService(cfg, embedder, index, kvStore, logger)
	if err != nil {
		t.Fatalf("NewIndexingService failed: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}

	// Verify the service can start and stop.
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_FileCreateEvent_NonexistentFile(t *testing.T) {
	svc, tw, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	tw.SendEvent(watcher.Event{
		Type:      watcher.EventCreate,
		Path:      "/nonexistent/file.go",
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventError {
			t.Fatalf("expected IndexEventError for nonexistent file, got %d", evt.Type)
		}
		if evt.Error == nil {
			t.Fatal("expected non-nil error in error event")
		}
	case <-timer.C:
		t.Fatal("timed out waiting for error event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_IndexFiles_SkippedFiles(t *testing.T) {
	svc, _, tmpDir := newTestService(t)
	ctx := context.Background()

	goFile := filepath.Join(tmpDir, "skiptest.go")
	content := "package main\n\nfunc SkipTest() {}\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := svc.IndexFiles(ctx, []string{goFile}); err != nil {
		t.Fatalf("first IndexFiles failed: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-svc.Events():
	case <-timer.C:
		t.Fatal("timed out waiting for first batch event")
	}

	if err := svc.IndexFiles(ctx, []string{goFile}); err != nil {
		t.Fatalf("second IndexFiles failed: %v", err)
	}

	timer2 := time.NewTimer(5 * time.Second)
	defer timer2.Stop()

	select {
	case <-svc.Events():
	case <-timer2.C:
		t.Fatal("timed out waiting for second batch event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIndexingService_DeleteEvent_NonIndexedFile(t *testing.T) {
	svc, tw, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Delete event for a file that was never indexed should still emit removed event.
	tw.SendEvent(watcher.Event{
		Type:      watcher.EventDelete,
		Path:      "/some/non-indexed-file.go",
		Timestamp: time.Now(),
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case evt := <-svc.Events():
		if evt.Type != IndexEventFileRemoved {
			t.Fatalf("expected IndexEventFileRemoved, got %d", evt.Type)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for delete event")
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

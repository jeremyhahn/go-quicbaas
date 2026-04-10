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

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// mockAuditCleaner implements AuditCleaner for testing.
type mockAuditCleaner struct {
	cleanupFunc func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error)
	calls       []cleanupCall
}

type cleanupCall struct {
	Options *AuditCleanupOptions
}

func newMockAuditCleaner() *mockAuditCleaner {
	return &mockAuditCleaner{
		calls: make([]cleanupCall, 0),
	}
}

func (m *mockAuditCleaner) CleanupAuditLogs(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
	m.calls = append(m.calls, cleanupCall{Options: opts})

	if m.cleanupFunc != nil {
		return m.cleanupFunc(ctx, opts)
	}

	return &AuditCleanupResult{
		DeletedCount:  100,
		ScannedCount:  500,
		OldestDeleted: time.Now().Add(-90 * 24 * time.Hour),
		NewestDeleted: time.Now().Add(-91 * 24 * time.Hour),
		DryRun:        opts.DryRun,
	}, nil
}

func TestNewAuditCleanupHandler(t *testing.T) {
	t.Run("creates handler with valid cleaner", func(t *testing.T) {
		mock := newMockAuditCleaner()

		handler := NewAuditCleanupHandler(mock)

		require.NotNil(t, handler)
		assert.NotNil(t, handler.cleaner)
	})

	t.Run("creates handler with nil cleaner", func(t *testing.T) {
		handler := NewAuditCleanupHandler(nil)

		require.NotNil(t, handler)
		assert.Nil(t, handler.cleaner)
	})
}

func TestAuditCleanupHandler_Name(t *testing.T) {
	t.Run("returns correct handler name", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		name := handler.Name()

		assert.Equal(t, HandlerAuditCleanup, name)
		assert.Equal(t, "audit.cleanup", name)
	})
}

func TestAuditCleanupHandler_Handler(t *testing.T) {
	t.Run("returns non-nil JobHandler function", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		jobHandler := handler.Handler()

		require.NotNil(t, jobHandler)
	})

	t.Run("returned handler invokes Handle method", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    []string{"kms.key.rotate"},
			TenantID:      "tenant-123",
			DryRun:        false,
			BatchSize:     500,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      1,
			Name:    "test-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		jobHandler := handler.Handler()
		result, err := jobHandler(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, 30, mock.calls[0].Options.RetentionDays)
		assert.Equal(t, []string{"kms.key.rotate"}, mock.calls[0].Options.EventTypes)
		assert.Equal(t, "tenant-123", mock.calls[0].Options.TenantID)
		assert.False(t, mock.calls[0].Options.DryRun)
		assert.Equal(t, 500, mock.calls[0].Options.BatchSize)
	})
}

func TestAuditCleanupHandler_Handle(t *testing.T) {
	t.Run("successful cleanup with specific event types", func(t *testing.T) {
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return &AuditCleanupResult{
				DeletedCount:  50,
				ScannedCount:  200,
				OldestDeleted: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				NewestDeleted: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
				DryRun:        false,
			}, nil
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 60,
			EventTypes:    []string{"kms.key.create", "kms.key.delete"},
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      100,
			Name:    "cleanup-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "Deleted 50 audit log entries")
		assert.Contains(t, result.Message, "scanned 200")
		assert.Contains(t, result.Message, "older than 60 days")
		assert.Contains(t, result.Message, "event types:")
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, 60, mock.calls[0].Options.RetentionDays)
		assert.Equal(t, []string{"kms.key.create", "kms.key.delete"}, mock.calls[0].Options.EventTypes)
	})

	t.Run("successful cleanup with tenant filter", func(t *testing.T) {
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return &AuditCleanupResult{
				DeletedCount:  25,
				ScannedCount:  100,
				OldestDeleted: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
				NewestDeleted: time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC),
				DryRun:        false,
			}, nil
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 45,
			EventTypes:    nil,
			TenantID:      "tenant-xyz",
			DryRun:        false,
			BatchSize:     500,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      101,
			Name:    "tenant-cleanup-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "for tenant tenant-xyz")
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, "tenant-xyz", mock.calls[0].Options.TenantID)
	})

	t.Run("successful dry run cleanup", func(t *testing.T) {
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return &AuditCleanupResult{
				DeletedCount:  1000,
				ScannedCount:  5000,
				OldestDeleted: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
				NewestDeleted: time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC),
				DryRun:        true,
			}, nil
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 90,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        true,
			BatchSize:     2000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      102,
			Name:    "dry-run-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "Would delete 1000 audit log entries")
		assert.Len(t, mock.calls, 1)
		assert.True(t, mock.calls[0].Options.DryRun)
	})

	t.Run("cleanup with default retention days", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 0,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     0,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      103,
			Name:    "default-retention-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, DefaultRetentionDays, mock.calls[0].Options.RetentionDays)
		assert.Equal(t, DefaultBatchSize, mock.calls[0].Options.BatchSize)
	})

	t.Run("invalid JSON payload", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		job := &scheduler.Job{
			ID:      104,
			Name:    "invalid-json-job",
			Handler: HandlerAuditCleanup,
			Payload: []byte("not valid json {{{"),
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload", invalidJobErr.Field)
		assert.Contains(t, invalidJobErr.Message, "failed to parse audit cleanup payload")
		assert.Len(t, mock.calls, 0)
	})

	t.Run("empty payload", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		job := &scheduler.Job{
			ID:      105,
			Name:    "empty-payload-job",
			Handler: HandlerAuditCleanup,
			Payload: []byte(""),
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload", invalidJobErr.Field)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("nil payload field in job", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		job := &scheduler.Job{
			ID:      106,
			Name:    "nil-payload-job",
			Handler: HandlerAuditCleanup,
			Payload: nil,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload", invalidJobErr.Field)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("retention days below minimum", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: -1,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      107,
			Name:    "negative-retention-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.retention_days", invalidJobErr.Field)
		assert.Contains(t, invalidJobErr.Message, "retention_days must be at least")
		assert.Len(t, mock.calls, 0)
	})

	t.Run("batch size below valid range", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     -5,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      108,
			Name:    "negative-batch-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.batch_size", invalidJobErr.Field)
		assert.Contains(t, invalidJobErr.Message, "batch_size must be between")
		assert.Len(t, mock.calls, 0)
	})

	t.Run("batch size above maximum", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     MaxBatchSize + 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      109,
			Name:    "large-batch-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.batch_size", invalidJobErr.Field)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("retention days at minimum boundary is valid", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: MinRetentionDays,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     100,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      110,
			Name:    "min-retention-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, MinRetentionDays, mock.calls[0].Options.RetentionDays)
	})

	t.Run("batch size at maximum boundary is valid", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     MaxBatchSize,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      111,
			Name:    "max-batch-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, MaxBatchSize, mock.calls[0].Options.BatchSize)
	})

	t.Run("nil cleaner returns error", func(t *testing.T) {
		handler := NewAuditCleanupHandler(nil)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      112,
			Name:    "nil-cleaner-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.Equal(t, HandlerAuditCleanup, handlerFailedErr.Handler)
		assert.Equal(t, uint64(112), handlerFailedErr.JobID)

		var cleanerErr *CleanerNotConfiguredError
		require.True(t, errors.As(handlerFailedErr.Cause, &cleanerErr))
	})

	t.Run("cleaner returns error", func(t *testing.T) {
		cleanerErr := errors.New("database connection failed")
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return nil, cleanerErr
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      113,
			Name:    "cleaner-error-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.Equal(t, HandlerAuditCleanup, handlerFailedErr.Handler)
		assert.Equal(t, uint64(113), handlerFailedErr.JobID)
		assert.Equal(t, cleanerErr, handlerFailedErr.Cause)
		assert.Len(t, mock.calls, 1)
	})

	t.Run("context cancellation propagates to cleaner", func(t *testing.T) {
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return nil, ctx.Err()
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      114,
			Name:    "context-cancel-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := handler.Handle(ctx, job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.True(t, errors.Is(handlerFailedErr.Cause, context.Canceled))
	})

	t.Run("handles payload with extra fields gracefully", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := map[string]interface{}{
			"retention_days": 45,
			"event_types":    []string{"test.event"},
			"tenant_id":      "t1",
			"dry_run":        true,
			"batch_size":     500,
			"extra_field":    "should be ignored",
			"another":        12345,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      115,
			Name:    "extra-fields-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, 45, mock.calls[0].Options.RetentionDays)
		assert.Equal(t, []string{"test.event"}, mock.calls[0].Options.EventTypes)
		assert.Equal(t, "t1", mock.calls[0].Options.TenantID)
		assert.True(t, mock.calls[0].Options.DryRun)
		assert.Equal(t, 500, mock.calls[0].Options.BatchSize)
	})

	t.Run("handles empty event types array", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    []string{},
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      116,
			Name:    "empty-events-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.NotContains(t, result.Message, "event types:")
		assert.Len(t, mock.calls, 1)
	})

	t.Run("handles zero deleted count", func(t *testing.T) {
		mock := newMockAuditCleaner()
		mock.cleanupFunc = func(ctx context.Context, opts *AuditCleanupOptions) (*AuditCleanupResult, error) {
			return &AuditCleanupResult{
				DeletedCount: 0,
				ScannedCount: 100,
				DryRun:       false,
			}, nil
		}
		handler := NewAuditCleanupHandler(mock)

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      117,
			Name:    "zero-deleted-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "Deleted 0 audit log entries")
		assert.NotContains(t, result.Message, "[range:")
	})

	t.Run("handles large number of event types", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		eventTypes := make([]string, 20)
		for i := 0; i < 20; i++ {
			eventTypes[i] = "event.type." + string(rune('a'+i))
		}

		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    eventTypes,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     1000,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      118,
			Name:    "many-events-job",
			Handler: HandlerAuditCleanup,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Len(t, mock.calls[0].Options.EventTypes, 20)
	})
}

func TestAuditCleanupHandler_ImplementsHandlerInterface(t *testing.T) {
	t.Run("implements Handler interface", func(t *testing.T) {
		mock := newMockAuditCleaner()
		handler := NewAuditCleanupHandler(mock)

		var _ Handler = handler
	})
}

func TestAuditCleanupPayload_JSONMarshaling(t *testing.T) {
	t.Run("correctly marshals to JSON with all fields", func(t *testing.T) {
		payload := AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    []string{"kms.key.create", "kms.key.delete"},
			TenantID:      "tenant-123",
			DryRun:        true,
			BatchSize:     500,
		}

		data, err := json.Marshal(payload)

		require.NoError(t, err)
		assert.Contains(t, string(data), `"retention_days":30`)
		assert.Contains(t, string(data), `"event_types":["kms.key.create","kms.key.delete"]`)
		assert.Contains(t, string(data), `"tenant_id":"tenant-123"`)
		assert.Contains(t, string(data), `"dry_run":true`)
		assert.Contains(t, string(data), `"batch_size":500`)
	})

	t.Run("correctly marshals to JSON with minimal fields", func(t *testing.T) {
		payload := AuditCleanupPayload{
			RetentionDays: 90,
			EventTypes:    nil,
			TenantID:      "",
			DryRun:        false,
			BatchSize:     0,
		}

		data, err := json.Marshal(payload)

		require.NoError(t, err)
		assert.Contains(t, string(data), `"retention_days":90`)
		assert.NotContains(t, string(data), `"event_types"`)
		assert.NotContains(t, string(data), `"tenant_id"`)
		assert.Contains(t, string(data), `"dry_run":false`)
	})

	t.Run("correctly unmarshals from JSON", func(t *testing.T) {
		jsonData := `{"retention_days":45,"event_types":["test.event"],"tenant_id":"t1","dry_run":true,"batch_size":2000}`

		var payload AuditCleanupPayload
		err := json.Unmarshal([]byte(jsonData), &payload)

		require.NoError(t, err)
		assert.Equal(t, 45, payload.RetentionDays)
		assert.Equal(t, []string{"test.event"}, payload.EventTypes)
		assert.Equal(t, "t1", payload.TenantID)
		assert.True(t, payload.DryRun)
		assert.Equal(t, 2000, payload.BatchSize)
	})

	t.Run("correctly unmarshals minimal JSON", func(t *testing.T) {
		jsonData := `{}`

		var payload AuditCleanupPayload
		err := json.Unmarshal([]byte(jsonData), &payload)

		require.NoError(t, err)
		assert.Equal(t, 0, payload.RetentionDays)
		assert.Nil(t, payload.EventTypes)
		assert.Empty(t, payload.TenantID)
		assert.False(t, payload.DryRun)
		assert.Equal(t, 0, payload.BatchSize)
	})
}

func TestAuditCleanupOptions(t *testing.T) {
	t.Run("correctly holds all fields", func(t *testing.T) {
		opts := &AuditCleanupOptions{
			RetentionDays: 60,
			EventTypes:    []string{"event1", "event2"},
			TenantID:      "tenant-abc",
			DryRun:        true,
			BatchSize:     500,
		}

		assert.Equal(t, 60, opts.RetentionDays)
		assert.Equal(t, []string{"event1", "event2"}, opts.EventTypes)
		assert.Equal(t, "tenant-abc", opts.TenantID)
		assert.True(t, opts.DryRun)
		assert.Equal(t, 500, opts.BatchSize)
	})

	t.Run("JSON tags are correct", func(t *testing.T) {
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
			EventTypes:    []string{"e1"},
			TenantID:      "t1",
			DryRun:        true,
			BatchSize:     100,
		}

		data, err := json.Marshal(opts)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"retention_days":30`)
		assert.Contains(t, string(data), `"event_types":["e1"]`)
		assert.Contains(t, string(data), `"tenant_id":"t1"`)
		assert.Contains(t, string(data), `"dry_run":true`)
		assert.Contains(t, string(data), `"batch_size":100`)
	})
}

func TestAuditCleanupResult(t *testing.T) {
	t.Run("correctly holds all fields", func(t *testing.T) {
		now := time.Now()
		oldest := now.Add(-100 * 24 * time.Hour)
		newest := now.Add(-90 * 24 * time.Hour)

		result := &AuditCleanupResult{
			DeletedCount:  150,
			ScannedCount:  1000,
			OldestDeleted: oldest,
			NewestDeleted: newest,
			DryRun:        false,
		}

		assert.Equal(t, int64(150), result.DeletedCount)
		assert.Equal(t, int64(1000), result.ScannedCount)
		assert.Equal(t, oldest, result.OldestDeleted)
		assert.Equal(t, newest, result.NewestDeleted)
		assert.False(t, result.DryRun)
	})

	t.Run("JSON tags are correct", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount:  50,
			ScannedCount:  200,
			OldestDeleted: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
			NewestDeleted: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			DryRun:        true,
		}

		data, err := json.Marshal(result)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"deleted_count":50`)
		assert.Contains(t, string(data), `"scanned_count":200`)
		assert.Contains(t, string(data), `"oldest_deleted"`)
		assert.Contains(t, string(data), `"newest_deleted"`)
		assert.Contains(t, string(data), `"dry_run":true`)
	})
}

func TestHandlerAuditCleanupConstant(t *testing.T) {
	t.Run("constant has correct value", func(t *testing.T) {
		assert.Equal(t, "audit.cleanup", HandlerAuditCleanup)
	})
}

func TestDefaultConstants(t *testing.T) {
	t.Run("default retention days has correct value", func(t *testing.T) {
		assert.Equal(t, 90, DefaultRetentionDays)
	})

	t.Run("default batch size has correct value", func(t *testing.T) {
		assert.Equal(t, 1000, DefaultBatchSize)
	})

	t.Run("min retention days has correct value", func(t *testing.T) {
		assert.Equal(t, 1, MinRetentionDays)
	})

	t.Run("max batch size has correct value", func(t *testing.T) {
		assert.Equal(t, 10000, MaxBatchSize)
	})
}

func TestCleanerNotConfiguredError(t *testing.T) {
	t.Run("returns correct error message", func(t *testing.T) {
		err := &CleanerNotConfiguredError{}

		assert.Equal(t, "audit cleaner not configured", err.Error())
	})

	t.Run("can be used with errors.As", func(t *testing.T) {
		err := &CleanerNotConfiguredError{}
		var target *CleanerNotConfiguredError

		assert.True(t, errors.As(err, &target))
	})
}

func TestValidatePayload(t *testing.T) {
	t.Run("valid payload with all fields", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			EventTypes:    []string{"event1"},
			TenantID:      "tenant1",
			DryRun:        true,
			BatchSize:     500,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
	})

	t.Run("applies default retention days when zero", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 0,
			BatchSize:     500,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
		assert.Equal(t, DefaultRetentionDays, payload.RetentionDays)
	})

	t.Run("applies default batch size when zero", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			BatchSize:     0,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
		assert.Equal(t, DefaultBatchSize, payload.BatchSize)
	})

	t.Run("rejects retention days below minimum", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: -5,
			BatchSize:     500,
		}

		err := validatePayload(payload)

		require.Error(t, err)
		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.retention_days", invalidJobErr.Field)
	})

	t.Run("accepts retention days at minimum", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: MinRetentionDays,
			BatchSize:     500,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
	})

	t.Run("rejects batch size below minimum", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			BatchSize:     -1,
		}

		err := validatePayload(payload)

		require.Error(t, err)
		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.batch_size", invalidJobErr.Field)
	})

	t.Run("rejects batch size above maximum", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			BatchSize:     MaxBatchSize + 1,
		}

		err := validatePayload(payload)

		require.Error(t, err)
		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.batch_size", invalidJobErr.Field)
	})

	t.Run("accepts batch size at maximum", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			BatchSize:     MaxBatchSize,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
	})

	t.Run("accepts batch size at minimum of 1", func(t *testing.T) {
		payload := &AuditCleanupPayload{
			RetentionDays: 30,
			BatchSize:     1,
		}

		err := validatePayload(payload)

		assert.NoError(t, err)
	})
}

func TestBuildResultMessage(t *testing.T) {
	t.Run("formats deletion message", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount:  100,
			ScannedCount:  500,
			OldestDeleted: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			NewestDeleted: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			DryRun:        false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "Deleted 100 audit log entries")
		assert.Contains(t, msg, "scanned 500")
		assert.Contains(t, msg, "older than 30 days")
		assert.Contains(t, msg, "[range:")
	})

	t.Run("formats dry run message", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount:  200,
			ScannedCount:  1000,
			OldestDeleted: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			NewestDeleted: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC),
			DryRun:        true,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 60,
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "Would delete 200 audit log entries")
		assert.Contains(t, msg, "older than 60 days")
	})

	t.Run("includes tenant in message when specified", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount: 50,
			ScannedCount: 100,
			DryRun:       false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
			TenantID:      "tenant-xyz",
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "for tenant tenant-xyz")
	})

	t.Run("includes event types in message when specified", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount: 50,
			ScannedCount: 100,
			DryRun:       false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
			EventTypes:    []string{"kms.key.create", "kms.key.delete"},
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "(event types: [kms.key.create kms.key.delete])")
	})

	t.Run("excludes range when no entries deleted", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount: 0,
			ScannedCount: 100,
			DryRun:       false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "Deleted 0 audit log entries")
		assert.NotContains(t, msg, "[range:")
	})

	t.Run("excludes range when timestamps are zero", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount: 50,
			ScannedCount: 100,
			DryRun:       false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 30,
		}

		msg := buildResultMessage(result, opts)

		assert.NotContains(t, msg, "[range:")
	})

	t.Run("includes full message with all options", func(t *testing.T) {
		result := &AuditCleanupResult{
			DeletedCount:  75,
			ScannedCount:  300,
			OldestDeleted: time.Date(2025, 1, 1, 12, 30, 0, 0, time.UTC),
			NewestDeleted: time.Date(2025, 1, 10, 15, 45, 0, 0, time.UTC),
			DryRun:        false,
		}
		opts := &AuditCleanupOptions{
			RetentionDays: 45,
			TenantID:      "tenant-abc",
			EventTypes:    []string{"event.type.a"},
		}

		msg := buildResultMessage(result, opts)

		assert.Contains(t, msg, "Deleted 75 audit log entries")
		assert.Contains(t, msg, "scanned 300")
		assert.Contains(t, msg, "older than 45 days")
		assert.Contains(t, msg, "for tenant tenant-abc")
		assert.Contains(t, msg, "(event types: [event.type.a])")
		assert.Contains(t, msg, "[range:")
		assert.Contains(t, msg, "2025-01-01T12:30:00Z")
		assert.Contains(t, msg, "2025-01-10T15:45:00Z")
	})
}

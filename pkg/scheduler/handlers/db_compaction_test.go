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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// mockDBCompactor implements DBCompactor for testing.
type mockDBCompactor struct {
	compactFunc func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error)
	calls       []compactCall
}

type compactCall struct {
	Options *CompactionOptions
}

func newMockDBCompactor() *mockDBCompactor {
	return &mockDBCompactor{
		calls: make([]compactCall, 0),
	}
}

func (m *mockDBCompactor) Compact(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
	m.calls = append(m.calls, compactCall{Options: opts})

	if m.compactFunc != nil {
		return m.compactFunc(ctx, opts)
	}

	return &CompactionResult{
		TablesCompacted: 5,
		BytesReclaimed:  1024 * 1024,
		Duration:        100 * time.Millisecond,
	}, nil
}

func TestNewDBCompactionHandler(t *testing.T) {
	t.Run("creates handler with valid compactor", func(t *testing.T) {
		mock := newMockDBCompactor()

		handler := NewDBCompactionHandler(mock)

		require.NotNil(t, handler)
		assert.NotNil(t, handler.compactor)
	})

	t.Run("creates handler with nil compactor", func(t *testing.T) {
		handler := NewDBCompactionHandler(nil)

		require.NotNil(t, handler)
		assert.Nil(t, handler.compactor)
	})
}

func TestDBCompactionHandler_Name(t *testing.T) {
	t.Run("returns correct handler name", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		name := handler.Name()

		assert.Equal(t, HandlerDBCompaction, name)
		assert.Equal(t, "db.compaction", name)
	})
}

func TestDBCompactionHandler_Handler(t *testing.T) {
	t.Run("returns non-nil JobHandler function", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		jobHandler := handler.Handler()

		require.NotNil(t, jobHandler)
	})

	t.Run("returned handler invokes Handle method", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    []string{"keys"},
			Threshold: 0.3,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      1,
			Name:    "test-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		jobHandler := handler.Handler()
		result, err := jobHandler(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, []string{"keys"}, mock.calls[0].Options.Tables)
		assert.Equal(t, 0.3, mock.calls[0].Options.Threshold)
		assert.False(t, mock.calls[0].Options.Force)
	})
}

func TestDBCompactionHandler_Handle(t *testing.T) {
	t.Run("successful compaction with specific tables", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return &CompactionResult{
				TablesCompacted: 2,
				BytesReclaimed:  512 * 1024,
				Duration:        50 * time.Millisecond,
			}, nil
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    []string{"keys", "metadata"},
			Threshold: 0.25,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      100,
			Name:    "compaction-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "2 table(s) compacted")
		assert.Contains(t, result.Message, "2 specified table(s)")
		assert.Contains(t, result.Message, "524288 bytes reclaimed")
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, []string{"keys", "metadata"}, mock.calls[0].Options.Tables)
		assert.Equal(t, 0.25, mock.calls[0].Options.Threshold)
	})

	t.Run("successful compaction with all tables", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return &CompactionResult{
				TablesCompacted: 10,
				BytesReclaimed:  2 * 1024 * 1024,
				Duration:        200 * time.Millisecond,
			}, nil
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      101,
			Name:    "all-tables-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "10 table(s) compacted")
		assert.Contains(t, result.Message, "all tables")
		assert.Empty(t, mock.calls[0].Options.Tables)
	})

	t.Run("successful forced compaction", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    []string{"urgent_table"},
			Threshold: 0.0,
			Force:     true,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      102,
			Name:    "forced-compaction-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.True(t, mock.calls[0].Options.Force)
	})

	t.Run("invalid JSON payload", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		job := &scheduler.Job{
			ID:      103,
			Name:    "invalid-json-job",
			Handler: HandlerDBCompaction,
			Payload: []byte("not valid json {{{"),
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload", invalidJobErr.Field)
		assert.Contains(t, invalidJobErr.Message, "failed to parse compaction payload")
		assert.Len(t, mock.calls, 0)
	})

	t.Run("empty payload", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		job := &scheduler.Job{
			ID:      104,
			Name:    "empty-payload-job",
			Handler: HandlerDBCompaction,
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
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		job := &scheduler.Job{
			ID:      105,
			Name:    "nil-payload-job",
			Handler: HandlerDBCompaction,
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

	t.Run("threshold below valid range", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: -0.1,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      106,
			Name:    "negative-threshold-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.threshold", invalidJobErr.Field)
		assert.Equal(t, "threshold must be between 0 and 1", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("threshold above valid range", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 1.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      107,
			Name:    "high-threshold-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.threshold", invalidJobErr.Field)
		assert.Equal(t, "threshold must be between 0 and 1", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("threshold at boundary zero is valid", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.0,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      108,
			Name:    "zero-threshold-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, 0.0, mock.calls[0].Options.Threshold)
	})

	t.Run("threshold at boundary one is valid", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 1.0,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      109,
			Name:    "one-threshold-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, 1.0, mock.calls[0].Options.Threshold)
	})

	t.Run("nil compactor returns error", func(t *testing.T) {
		handler := NewDBCompactionHandler(nil)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      110,
			Name:    "nil-compactor-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.Equal(t, HandlerDBCompaction, handlerFailedErr.Handler)
		assert.Equal(t, uint64(110), handlerFailedErr.JobID)

		var compactorErr *CompactorNotConfiguredError
		require.True(t, errors.As(handlerFailedErr.Cause, &compactorErr))
	})

	t.Run("compactor returns error", func(t *testing.T) {
		compactorErr := errors.New("storage I/O error")
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return nil, compactorErr
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      111,
			Name:    "compactor-error-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.Equal(t, HandlerDBCompaction, handlerFailedErr.Handler)
		assert.Equal(t, uint64(111), handlerFailedErr.JobID)
		assert.Equal(t, compactorErr, handlerFailedErr.Cause)
		assert.Len(t, mock.calls, 1)
	})

	t.Run("context cancellation propagates to compactor", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return nil, ctx.Err()
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      112,
			Name:    "context-cancel-job",
			Handler: HandlerDBCompaction,
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
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := map[string]interface{}{
			"tables":      []string{"test"},
			"threshold":   0.5,
			"force":       true,
			"extra_field": "should be ignored",
			"another":     12345,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      113,
			Name:    "extra-fields-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, []string{"test"}, mock.calls[0].Options.Tables)
		assert.Equal(t, 0.5, mock.calls[0].Options.Threshold)
		assert.True(t, mock.calls[0].Options.Force)
	})

	t.Run("handles empty tables array", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    []string{},
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      114,
			Name:    "empty-tables-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "all tables")
		assert.Len(t, mock.calls, 1)
	})

	t.Run("handles special characters in table names", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    []string{"table-with-dashes", "table_with_underscores", "table.with.dots"},
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      115,
			Name:    "special-chars-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, []string{"table-with-dashes", "table_with_underscores", "table.with.dots"}, mock.calls[0].Options.Tables)
	})

	t.Run("handles large number of tables", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return &CompactionResult{
				TablesCompacted: len(opts.Tables),
				BytesReclaimed:  int64(len(opts.Tables)) * 1024,
				Duration:        time.Duration(len(opts.Tables)) * time.Millisecond,
			}, nil
		}
		handler := NewDBCompactionHandler(mock)

		tables := make([]string, 100)
		for i := 0; i < 100; i++ {
			tables[i] = fmt.Sprintf("table_%d", i)
		}

		payload := DBCompactionPayload{
			Tables:    tables,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      116,
			Name:    "many-tables-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "100 table(s) compacted")
		assert.Contains(t, result.Message, "100 specified table(s)")
	})

	t.Run("handles zero bytes reclaimed", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return &CompactionResult{
				TablesCompacted: 5,
				BytesReclaimed:  0,
				Duration:        10 * time.Millisecond,
			}, nil
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      117,
			Name:    "zero-reclaimed-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "0 bytes reclaimed")
	})

	t.Run("handles large bytes reclaimed", func(t *testing.T) {
		mock := newMockDBCompactor()
		mock.compactFunc = func(ctx context.Context, opts *CompactionOptions) (*CompactionResult, error) {
			return &CompactionResult{
				TablesCompacted: 1,
				BytesReclaimed:  10 * 1024 * 1024 * 1024, // 10 GB
				Duration:        5 * time.Minute,
			}, nil
		}
		handler := NewDBCompactionHandler(mock)

		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     true,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      118,
			Name:    "large-reclaimed-job",
			Handler: HandlerDBCompaction,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "10737418240 bytes reclaimed")
	})
}

func TestDBCompactionHandler_ImplementsHandlerInterface(t *testing.T) {
	t.Run("implements Handler interface", func(t *testing.T) {
		mock := newMockDBCompactor()
		handler := NewDBCompactionHandler(mock)

		var _ Handler = handler
	})
}

func TestDBCompactionPayload_JSONMarshaling(t *testing.T) {
	t.Run("correctly marshals to JSON with tables", func(t *testing.T) {
		payload := DBCompactionPayload{
			Tables:    []string{"keys", "metadata"},
			Threshold: 0.3,
			Force:     true,
		}

		data, err := json.Marshal(payload)

		require.NoError(t, err)
		assert.Contains(t, string(data), `"tables":["keys","metadata"]`)
		assert.Contains(t, string(data), `"threshold":0.3`)
		assert.Contains(t, string(data), `"force":true`)
	})

	t.Run("correctly marshals to JSON without tables", func(t *testing.T) {
		payload := DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.5,
			Force:     false,
		}

		data, err := json.Marshal(payload)

		require.NoError(t, err)
		assert.NotContains(t, string(data), `"tables"`)
		assert.Contains(t, string(data), `"threshold":0.5`)
		assert.Contains(t, string(data), `"force":false`)
	})

	t.Run("correctly unmarshals from JSON", func(t *testing.T) {
		jsonData := `{"tables":["test_table"],"threshold":0.25,"force":true}`

		var payload DBCompactionPayload
		err := json.Unmarshal([]byte(jsonData), &payload)

		require.NoError(t, err)
		assert.Equal(t, []string{"test_table"}, payload.Tables)
		assert.Equal(t, 0.25, payload.Threshold)
		assert.True(t, payload.Force)
	})

	t.Run("correctly unmarshals minimal JSON", func(t *testing.T) {
		jsonData := `{}`

		var payload DBCompactionPayload
		err := json.Unmarshal([]byte(jsonData), &payload)

		require.NoError(t, err)
		assert.Nil(t, payload.Tables)
		assert.Equal(t, 0.0, payload.Threshold)
		assert.False(t, payload.Force)
	})
}

func TestCompactionOptions(t *testing.T) {
	t.Run("correctly holds all fields", func(t *testing.T) {
		opts := &CompactionOptions{
			Tables:    []string{"table1", "table2"},
			Threshold: 0.4,
			Force:     true,
		}

		assert.Equal(t, []string{"table1", "table2"}, opts.Tables)
		assert.Equal(t, 0.4, opts.Threshold)
		assert.True(t, opts.Force)
	})

	t.Run("JSON tags are correct", func(t *testing.T) {
		opts := &CompactionOptions{
			Tables:    []string{"t1"},
			Threshold: 0.5,
			Force:     true,
		}

		data, err := json.Marshal(opts)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"tables":["t1"]`)
		assert.Contains(t, string(data), `"threshold":0.5`)
		assert.Contains(t, string(data), `"force":true`)
	})
}

func TestCompactionResult(t *testing.T) {
	t.Run("correctly holds all fields", func(t *testing.T) {
		result := &CompactionResult{
			TablesCompacted: 3,
			BytesReclaimed:  1024,
			Duration:        100 * time.Millisecond,
		}

		assert.Equal(t, 3, result.TablesCompacted)
		assert.Equal(t, int64(1024), result.BytesReclaimed)
		assert.Equal(t, 100*time.Millisecond, result.Duration)
	})

	t.Run("JSON tags are correct", func(t *testing.T) {
		result := &CompactionResult{
			TablesCompacted: 5,
			BytesReclaimed:  2048,
			Duration:        50 * time.Millisecond,
		}

		data, err := json.Marshal(result)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"tables_compacted":5`)
		assert.Contains(t, string(data), `"bytes_reclaimed":2048`)
		assert.Contains(t, string(data), `"duration":50000000`)
	})
}

func TestHandlerDBCompactionConstant(t *testing.T) {
	t.Run("constant has correct value", func(t *testing.T) {
		assert.Equal(t, "db.compaction", HandlerDBCompaction)
	})
}

func TestCompactorNotConfiguredError(t *testing.T) {
	t.Run("returns correct error message", func(t *testing.T) {
		err := &CompactorNotConfiguredError{}

		assert.Equal(t, "compactor not configured", err.Error())
	})

	t.Run("can be used with errors.As", func(t *testing.T) {
		err := &CompactorNotConfiguredError{}
		var target *CompactorNotConfiguredError

		assert.True(t, errors.As(err, &target))
	})
}

func TestValidateCompactionPayload(t *testing.T) {
	t.Run("valid payload with all fields", func(t *testing.T) {
		payload := &DBCompactionPayload{
			Tables:    []string{"table1"},
			Threshold: 0.5,
			Force:     true,
		}

		err := validateCompactionPayload(payload)

		assert.NoError(t, err)
	})

	t.Run("valid payload with zero threshold", func(t *testing.T) {
		payload := &DBCompactionPayload{
			Tables:    nil,
			Threshold: 0.0,
			Force:     false,
		}

		err := validateCompactionPayload(payload)

		assert.NoError(t, err)
	})

	t.Run("valid payload with threshold of one", func(t *testing.T) {
		payload := &DBCompactionPayload{
			Tables:    nil,
			Threshold: 1.0,
			Force:     false,
		}

		err := validateCompactionPayload(payload)

		assert.NoError(t, err)
	})

	t.Run("invalid negative threshold", func(t *testing.T) {
		payload := &DBCompactionPayload{
			Tables:    nil,
			Threshold: -0.1,
			Force:     false,
		}

		err := validateCompactionPayload(payload)

		require.Error(t, err)
		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.threshold", invalidJobErr.Field)
	})

	t.Run("invalid threshold above one", func(t *testing.T) {
		payload := &DBCompactionPayload{
			Tables:    nil,
			Threshold: 1.001,
			Force:     false,
		}

		err := validateCompactionPayload(payload)

		require.Error(t, err)
		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.threshold", invalidJobErr.Field)
	})
}

func TestFormatCompactionMessage(t *testing.T) {
	t.Run("formats message with specific tables", func(t *testing.T) {
		result := &CompactionResult{
			TablesCompacted: 2,
			BytesReclaimed:  1024,
			Duration:        100 * time.Millisecond,
		}
		payload := &DBCompactionPayload{
			Tables: []string{"t1", "t2"},
		}

		msg := formatCompactionMessage(result, payload)

		assert.Contains(t, msg, "2 table(s) compacted")
		assert.Contains(t, msg, "2 specified table(s)")
		assert.Contains(t, msg, "1024 bytes reclaimed")
		assert.Contains(t, msg, "100ms")
	})

	t.Run("formats message with all tables", func(t *testing.T) {
		result := &CompactionResult{
			TablesCompacted: 10,
			BytesReclaimed:  2048,
			Duration:        200 * time.Millisecond,
		}
		payload := &DBCompactionPayload{
			Tables: nil,
		}

		msg := formatCompactionMessage(result, payload)

		assert.Contains(t, msg, "10 table(s) compacted")
		assert.Contains(t, msg, "all tables")
		assert.Contains(t, msg, "2048 bytes reclaimed")
		assert.Contains(t, msg, "200ms")
	})

	t.Run("formats message with empty tables slice", func(t *testing.T) {
		result := &CompactionResult{
			TablesCompacted: 5,
			BytesReclaimed:  512,
			Duration:        50 * time.Millisecond,
		}
		payload := &DBCompactionPayload{
			Tables: []string{},
		}

		msg := formatCompactionMessage(result, payload)

		assert.Contains(t, msg, "5 table(s) compacted")
		assert.Contains(t, msg, "all tables")
	})
}

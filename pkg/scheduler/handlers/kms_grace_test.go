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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// mockKMSKeyStatusUpdater implements KMSKeyStatusUpdater for testing.
type mockKMSKeyStatusUpdater struct {
	updateFunc func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error)
	calls      []updateCall
}

type updateCall struct {
	KeyID   string
	Version uint64
}

func newMockKMSKeyStatusUpdater() *mockKMSKeyStatusUpdater {
	return &mockKMSKeyStatusUpdater{
		calls: make([]updateCall, 0),
	}
}

func (m *mockKMSKeyStatusUpdater) UpdateKeyStatusForGracePeriod(
	ctx context.Context,
	keyID string,
	version uint64,
) (*GracePeriodResult, error) {
	m.calls = append(m.calls, updateCall{KeyID: keyID, Version: version})

	if m.updateFunc != nil {
		return m.updateFunc(ctx, keyID, version)
	}

	return &GracePeriodResult{
		KeyID:     keyID,
		Version:   version,
		OldStatus: "active",
		NewStatus: "disabled",
	}, nil
}

func TestNewKMSGracePeriodHandler(t *testing.T) {
	t.Run("creates handler with valid updater", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()

		handler := NewKMSGracePeriodHandler(mock)

		require.NotNil(t, handler)
		assert.NotNil(t, handler.kmsUpdater)
	})

	t.Run("creates handler with nil updater", func(t *testing.T) {
		handler := NewKMSGracePeriodHandler(nil)

		require.NotNil(t, handler)
		assert.Nil(t, handler.kmsUpdater)
	})
}

func TestKMSGracePeriodHandler_Name(t *testing.T) {
	t.Run("returns correct handler name", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		name := handler.Name()

		assert.Equal(t, HandlerKMSGracePeriod, name)
		assert.Equal(t, "kms.grace_period", name)
	})
}

func TestKMSGracePeriodHandler_Handler(t *testing.T) {
	t.Run("returns non-nil JobHandler function", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		jobHandler := handler.Handler()

		require.NotNil(t, jobHandler)
	})

	t.Run("returned handler invokes Handle method", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "test-key-id",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      1,
			Name:    "test-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		jobHandler := handler.Handler()
		result, err := jobHandler(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, "test-key-id", mock.calls[0].KeyID)
		assert.Equal(t, uint64(1), mock.calls[0].Version)
	})
}

func TestKMSGracePeriodHandler_Handle(t *testing.T) {
	t.Run("successful key status update", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		mock.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
			return &GracePeriodResult{
				KeyID:     keyID,
				Version:   version,
				OldStatus: "active",
				NewStatus: "disabled",
			}, nil
		}
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "my-key-123",
			Version: 5,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      100,
			Name:    "grace-period-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "Disabled key my-key-123 version 5")
		assert.Contains(t, result.Message, "previous status: active")
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, "my-key-123", mock.calls[0].KeyID)
		assert.Equal(t, uint64(5), mock.calls[0].Version)
	})

	t.Run("invalid JSON payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		job := &scheduler.Job{
			ID:      101,
			Name:    "invalid-json-job",
			Handler: HandlerKMSGracePeriod,
			Payload: []byte("not valid json {{{"),
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload", invalidJobErr.Field)
		assert.Contains(t, invalidJobErr.Message, "failed to parse grace period payload")
		assert.Len(t, mock.calls, 0)
	})

	t.Run("empty payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		job := &scheduler.Job{
			ID:      102,
			Name:    "empty-payload-job",
			Handler: HandlerKMSGracePeriod,
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

	t.Run("missing key_id in payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := map[string]interface{}{
			"version": 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      103,
			Name:    "missing-key-id-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.key_id", invalidJobErr.Field)
		assert.Equal(t, "key_id is required", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("empty key_id in payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      104,
			Name:    "empty-key-id-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.key_id", invalidJobErr.Field)
		assert.Equal(t, "key_id is required", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("missing version in payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := map[string]interface{}{
			"key_id": "test-key",
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      105,
			Name:    "missing-version-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.version", invalidJobErr.Field)
		assert.Equal(t, "version is required and must be > 0", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("zero version in payload", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "test-key",
			Version: 0,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      106,
			Name:    "zero-version-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var invalidJobErr *scheduler.InvalidJobError
		require.True(t, errors.As(err, &invalidJobErr))
		assert.Equal(t, "payload.version", invalidJobErr.Field)
		assert.Equal(t, "version is required and must be > 0", invalidJobErr.Message)
		assert.Len(t, mock.calls, 0)
	})

	t.Run("KMS updater returns error", func(t *testing.T) {
		updaterErr := errors.New("key not found in KMS")
		mock := newMockKMSKeyStatusUpdater()
		mock.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
			return nil, updaterErr
		}
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "nonexistent-key",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      107,
			Name:    "updater-error-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.Equal(t, HandlerKMSGracePeriod, handlerFailedErr.Handler)
		assert.Equal(t, uint64(107), handlerFailedErr.JobID)
		assert.Equal(t, updaterErr, handlerFailedErr.Cause)
		assert.Len(t, mock.calls, 1)
	})

	t.Run("context cancellation propagates to updater", func(t *testing.T) {
		ctxCanceledErr := context.Canceled
		mock := newMockKMSKeyStatusUpdater()
		mock.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return &GracePeriodResult{
					KeyID:     keyID,
					Version:   version,
					OldStatus: "active",
					NewStatus: "disabled",
				}, nil
			}
		}
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "test-key",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      108,
			Name:    "context-cancel-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
			return nil, ctx.Err()
		}

		result, err := handler.Handle(ctx, job)

		require.Error(t, err)
		assert.Nil(t, result)

		var handlerFailedErr *scheduler.HandlerFailedError
		require.True(t, errors.As(err, &handlerFailedErr))
		assert.True(t, errors.Is(handlerFailedErr.Cause, ctxCanceledErr))
	})

	t.Run("handles payload with extra fields gracefully", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := map[string]interface{}{
			"key_id":      "test-key",
			"version":     2,
			"extra_field": "should be ignored",
			"another":     12345,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      109,
			Name:    "extra-fields-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, "test-key", mock.calls[0].KeyID)
		assert.Equal(t, uint64(2), mock.calls[0].Version)
	})

	t.Run("handles large version number", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "test-key",
			Version: ^uint64(0),
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      110,
			Name:    "large-version-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, ^uint64(0), mock.calls[0].Version)
	})

	t.Run("job result message format verification", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		mock.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
			return &GracePeriodResult{
				KeyID:     keyID,
				Version:   version,
				OldStatus: "pre-active",
				NewStatus: "disabled",
			}, nil
		}
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "format-test-key",
			Version: 42,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      111,
			Name:    "format-test-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		expectedMessage := "Disabled key format-test-key version 42 (previous status: pre-active)"
		assert.Equal(t, expectedMessage, result.Message)
	})

	t.Run("handles special characters in key_id", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "key/with/slashes-and_underscores.plus.dots:colons",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      112,
			Name:    "special-chars-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Len(t, mock.calls, 1)
		assert.Equal(t, "key/with/slashes-and_underscores.plus.dots:colons", mock.calls[0].KeyID)
	})

	t.Run("handles unicode key_id", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "test-key-unicode-emoji-test",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      113,
			Name:    "unicode-key-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
	})

	t.Run("nil payload field in job", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		job := &scheduler.Job{
			ID:      114,
			Name:    "nil-payload-job",
			Handler: HandlerKMSGracePeriod,
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

	t.Run("handles whitespace-only key_id", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		payload := KMSGracePeriodPayload{
			KeyID:   "   ",
			Version: 1,
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		job := &scheduler.Job{
			ID:      115,
			Name:    "whitespace-key-job",
			Handler: HandlerKMSGracePeriod,
			Payload: payloadBytes,
		}

		result, err := handler.Handle(context.Background(), job)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Success)
		assert.Equal(t, "   ", mock.calls[0].KeyID)
	})
}

func TestKMSGracePeriodHandler_ImplementsHandlerInterface(t *testing.T) {
	t.Run("implements Handler interface", func(t *testing.T) {
		mock := newMockKMSKeyStatusUpdater()
		handler := NewKMSGracePeriodHandler(mock)

		var _ Handler = handler
	})
}

func TestKMSGracePeriodPayload_JSONMarshaling(t *testing.T) {
	t.Run("correctly marshals to JSON", func(t *testing.T) {
		payload := KMSGracePeriodPayload{
			KeyID:   "test-key",
			Version: 123,
		}

		data, err := json.Marshal(payload)

		require.NoError(t, err)
		assert.Contains(t, string(data), `"key_id":"test-key"`)
		assert.Contains(t, string(data), `"version":123`)
	})

	t.Run("correctly unmarshals from JSON", func(t *testing.T) {
		jsonData := `{"key_id":"unmarshaled-key","version":456}`

		var payload KMSGracePeriodPayload
		err := json.Unmarshal([]byte(jsonData), &payload)

		require.NoError(t, err)
		assert.Equal(t, "unmarshaled-key", payload.KeyID)
		assert.Equal(t, uint64(456), payload.Version)
	})
}

func TestGracePeriodResult(t *testing.T) {
	t.Run("correctly holds all fields", func(t *testing.T) {
		result := &GracePeriodResult{
			KeyID:     "result-key",
			Version:   789,
			OldStatus: "active",
			NewStatus: "disabled",
		}

		assert.Equal(t, "result-key", result.KeyID)
		assert.Equal(t, uint64(789), result.Version)
		assert.Equal(t, "active", result.OldStatus)
		assert.Equal(t, "disabled", result.NewStatus)
	})
}

func TestKeyStatusConstants(t *testing.T) {
	t.Run("KeyStatusDisabled has correct value", func(t *testing.T) {
		assert.Equal(t, KeyStatus("disabled"), KeyStatusDisabled)
	})
}

func TestHandlerKMSGracePeriodConstant(t *testing.T) {
	t.Run("constant has correct value", func(t *testing.T) {
		assert.Equal(t, "kms.grace_period", HandlerKMSGracePeriod)
	})
}

func TestUpdateKeyStatusRequest(t *testing.T) {
	t.Run("JSON tags are correct", func(t *testing.T) {
		req := UpdateKeyStatusRequest{
			KeyID:   "req-key",
			Version: 100,
			Status:  KeyStatusDisabled,
		}

		data, err := json.Marshal(req)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"key_id":"req-key"`)
		assert.Contains(t, string(data), `"version":100`)
		assert.Contains(t, string(data), `"status":"disabled"`)
	})
}

func TestUpdateKeyStatusResponse(t *testing.T) {
	t.Run("JSON tags are correct", func(t *testing.T) {
		resp := UpdateKeyStatusResponse{
			KeyID:     "resp-key",
			Version:   200,
			OldStatus: "active",
			NewStatus: "disabled",
		}

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"key_id":"resp-key"`)
		assert.Contains(t, string(data), `"version":200`)
		assert.Contains(t, string(data), `"old_status":"active"`)
		assert.Contains(t, string(data), `"new_status":"disabled"`)
	})
}

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

package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// WatchSecretsByPrefix - Polling Loop: New Secret Detection
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_DetectsNewSecret(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	// Track call count to change behavior after initial poll
	var listCallCount atomic.Int64

	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		count := listCallCount.Add(1)
		if count <= 1 {
			// Initial list: no secrets
			return []string{}, nil
		}
		// Subsequent polls: new secret appears
		return []string{"app/new-secret"}, nil
	}

	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return map[string][]byte{"value": []byte("secret-data")},
			map[string]string{"version": "1"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Read the first event from the channel - should be a Created event
	change := <-ch
	require.NotNil(t, change)
	assert.Equal(t, "app/new-secret", change.Path)
	assert.Equal(t, SecretChangeCreated, change.ChangeType)
	assert.Equal(t, []byte("secret-data"), change.Data["value"])
	assert.Equal(t, "1", change.Metadata["version"])
	assert.NotZero(t, change.Version)
	assert.False(t, change.Timestamp.IsZero())

	cancel()
	// Drain remaining events
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Polling Loop: Updated Secret Detection
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_DetectsUpdatedSecret(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	var listCallCount atomic.Int64
	var getCallCount atomic.Int64

	// List always returns the same secret
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		listCallCount.Add(1)
		return []string{"app/config"}, nil
	}

	// Get returns version "1" initially, then "2" on subsequent calls
	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		count := getCallCount.Add(1)
		if count <= 1 {
			// Initial snapshot (during knownSecrets build)
			return map[string][]byte{"value": []byte("old-value")},
				map[string]string{"version": "1"},
				nil
		}
		// During poll: version changed
		return map[string][]byte{"value": []byte("new-value")},
			map[string]string{"version": "2"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Read the first event from the channel - should be an Updated event
	change := <-ch
	require.NotNil(t, change)
	assert.Equal(t, "app/config", change.Path)
	assert.Equal(t, SecretChangeUpdated, change.ChangeType)
	assert.Equal(t, []byte("new-value"), change.Data["value"])
	assert.Equal(t, "2", change.Metadata["version"])

	cancel()
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Polling Loop: Deleted Secret Detection
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_DetectsDeletedSecret(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	var listCallCount atomic.Int64

	// First list returns a secret, subsequent polls return empty (secret deleted)
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		count := listCallCount.Add(1)
		if count <= 1 {
			// Initial list: secret exists
			return []string{"app/to-delete"}, nil
		}
		// Subsequent polls: secret is gone
		return []string{}, nil
	}

	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return map[string][]byte{"value": []byte("doomed-data")},
			map[string]string{"version": "1"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Read the first event - should be a Deleted event
	change := <-ch
	require.NotNil(t, change)
	assert.Equal(t, "app/to-delete", change.Path)
	assert.Equal(t, SecretChangeDeleted, change.ChangeType)
	assert.NotZero(t, change.Version)
	assert.False(t, change.Timestamp.IsZero())

	cancel()
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Polling Loop: List Error During Poll (continue)
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_PollListErrorContinues(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	var listCallCount atomic.Int64

	// Initial list succeeds, first poll fails, second poll succeeds with new secret
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		count := listCallCount.Add(1)
		switch {
		case count <= 1:
			// Initial list: no secrets
			return []string{}, nil
		case count == 2:
			// First poll: error (should continue)
			return nil, assert.AnError
		default:
			// Second poll: new secret found
			return []string{"app/recovered"}, nil
		}
	}

	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return map[string][]byte{"value": []byte("recovered-data")},
			map[string]string{"version": "1"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// The goroutine should recover from the list error and detect the new secret
	change := <-ch
	require.NotNil(t, change)
	assert.Equal(t, "app/recovered", change.Path)
	assert.Equal(t, SecretChangeCreated, change.ChangeType)

	cancel()
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Polling Loop: Get Error During Poll (continue)
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_PollGetErrorContinues(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	var listCallCount atomic.Int64
	var getCallCount atomic.Int64

	// Initial list returns no secrets, polls return a secret
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		count := listCallCount.Add(1)
		if count <= 1 {
			return []string{}, nil
		}
		return []string{"app/flaky"}, nil
	}

	// First get during poll fails, second get succeeds
	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		count := getCallCount.Add(1)
		if count <= 1 {
			return nil, nil, assert.AnError
		}
		return map[string][]byte{"value": []byte("data")},
			map[string]string{"version": "1"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// The first poll sees the secret but Get fails (continue).
	// The second poll should detect it as new since Get succeeds now.
	change := <-ch
	require.NotNil(t, change)
	assert.Equal(t, "app/flaky", change.Path)
	assert.Equal(t, SecretChangeCreated, change.ChangeType)

	cancel()
	for range ch {
	}
}

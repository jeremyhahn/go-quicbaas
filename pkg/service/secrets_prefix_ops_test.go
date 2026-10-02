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

// Package service provides unit tests for secrets prefix operations.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// GetSecretsByPrefix Tests
// =============================================================================

func TestSecretsService_GetSecretsByPrefix_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx := tenantctx.WithSystem(context.Background())

	// Store some secrets with a prefix
	for i := 1; i <= 3; i++ {
		path := "/apps/myapp/secret" + string(rune('0'+i))
		data := map[string][]byte{"value": []byte("secret-data")}
		metadata := map[string]string{"status": string(SecretStatusEnabled), "version": "1"}
		err := mockManager.Store(ctx, path, data, metadata)
		require.NoError(t, err)
	}

	// Override list to return our paths
	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{
			"/apps/myapp/secret1",
			"/apps/myapp/secret2",
			"/apps/myapp/secret3",
		}, nil
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: true,
	}

	resp, err := svc.GetSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotZero(t, resp.TotalCount)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestSecretsService_GetSecretsByPrefix_NilRequest(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	resp, err := svc.GetSecretsByPrefix(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSecretsService_GetSecretsByPrefix_EmptyPrefix(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretsByPrefixRequest{
		Prefix: "",
	}

	resp, err := svc.GetSecretsByPrefix(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "prefix is required")
}

func TestSecretsService_GetSecretsByPrefix_ListError(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, errors.New("list operation failed")
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: true,
	}

	resp, err := svc.GetSecretsByPrefix(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "list secrets failed")
}

func TestSecretsService_GetSecretsByPrefix_WithLimit(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx := tenantctx.WithSystem(context.Background())

	// Store secrets
	paths := []string{
		"/apps/myapp/secret1",
		"/apps/myapp/secret2",
		"/apps/myapp/secret3",
		"/apps/myapp/secret4",
		"/apps/myapp/secret5",
	}

	for _, path := range paths {
		data := map[string][]byte{"value": []byte("secret-data")}
		metadata := map[string]string{"status": string(SecretStatusEnabled), "version": "1"}
		err := mockManager.Store(ctx, path, data, metadata)
		require.NoError(t, err)
	}

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return paths, nil
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: true,
		Limit:       2,
	}

	resp, err := svc.GetSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.HasMore)
	assert.Equal(t, 5, resp.TotalCount)
}

func TestSecretsService_GetSecretsByPrefix_DisabledSecretsFiltered(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx := tenantctx.WithSystem(context.Background())

	// Store an enabled secret
	err := mockManager.Store(ctx, "/apps/myapp/enabled",
		map[string][]byte{"value": []byte("data")},
		map[string]string{"status": string(SecretStatusEnabled), "version": "1"})
	require.NoError(t, err)

	// Store a disabled secret
	err = mockManager.Store(ctx, "/apps/myapp/disabled",
		map[string][]byte{"value": []byte("data")},
		map[string]string{"status": string(SecretStatusDisabled), "version": "1"})
	require.NoError(t, err)

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"/apps/myapp/enabled", "/apps/myapp/disabled"}, nil
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: true,
	}

	resp, err := svc.GetSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Only enabled secret should be returned
	assert.Len(t, resp.Secrets, 1)
	assert.Contains(t, resp.Secrets, "/apps/myapp/enabled")
}

func TestSecretsService_GetSecretsByPrefix_MetadataOnly(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx := tenantctx.WithSystem(context.Background())

	err := mockManager.Store(ctx, "/apps/myapp/secret",
		map[string][]byte{"value": []byte("secret-data")},
		map[string]string{"status": string(SecretStatusEnabled), "version": "1"})
	require.NoError(t, err)

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"/apps/myapp/secret"}, nil
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: false, // Don't include data
	}

	resp, err := svc.GetSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Secrets) // No data included
	assert.NotEmpty(t, resp.Metadata)
}

func TestSecretsService_GetSecretsByPrefix_GetError(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"/apps/myapp/secret"}, nil
	}

	// Make Get fail - secrets that can't be read are skipped
	mockManager.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return nil, nil, errors.New("get failed")
	}

	req := &GetSecretsByPrefixRequest{
		Prefix:      "/apps/myapp/",
		IncludeData: true,
	}

	resp, err := svc.GetSecretsByPrefix(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err) // Errors are logged but operation succeeds
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Secrets) // Secret was skipped due to error
}

// =============================================================================
// WatchSecretsByPrefix Tests
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_NilRequest(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ch, err := svc.WatchSecretsByPrefix(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSecretsService_WatchSecretsByPrefix_EmptyPrefix(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &WatchSecretsRequest{
		Prefix: "",
	}

	ch, err := svc.WatchSecretsByPrefix(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "prefix is required")
}

func TestSecretsService_WatchSecretsByPrefix_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))

	// Setup initial secrets
	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"/apps/myapp/secret1"}, nil
	}

	mockManager.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return map[string][]byte{"value": []byte("data")},
			map[string]string{"version": "1"},
			nil
	}

	req := &WatchSecretsRequest{
		Prefix: "/apps/myapp/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, ch)

	// Cancel the context to stop watching
	cancel()

	// Wait briefly for channel to close
	select {
	case _, ok := <-ch:
		if !ok {
			// Channel closed as expected
		}
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable since we may not receive any events
	}
}

func TestSecretsService_WatchSecretsByPrefix_InitialListError(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	defer cancel()

	// Make initial list fail
	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, errors.New("list failed")
	}

	req := &WatchSecretsRequest{
		Prefix: "/apps/myapp/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)

	require.NoError(t, err) // Watch starts, error happens in goroutine
	assert.NotNil(t, ch)

	// Channel should close quickly due to error
	select {
	case _, ok := <-ch:
		if !ok {
			// Channel closed as expected after error
		}
	case <-time.After(2 * time.Second):
		t.Error("Expected channel to close after initial list error")
	}
}

func TestSecretsService_WatchSecretsByPrefix_ContextCancellation(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))

	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{}, nil
	}

	req := &WatchSecretsRequest{
		Prefix: "/apps/myapp/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, ch)

	// Cancel immediately
	cancel()

	// Wait for channel to close
	select {
	case _, ok := <-ch:
		if !ok {
			// Channel closed as expected
		}
	case <-time.After(2 * time.Second):
		t.Error("Expected channel to close after context cancellation")
	}
}

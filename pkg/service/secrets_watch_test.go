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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// WatchSecretsByPrefix - Context Cancellation
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_ContextCanceled(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	// Pre-populate some secrets so the initial list succeeds
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"app/config/db-host"}, nil
	}
	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return map[string][]byte{"value": []byte("localhost")},
			map[string]string{"version": "1"},
			nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := &WatchSecretsRequest{
		Prefix:       "app/config/",
		StartVersion: 0,
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Cancel context - the goroutine should exit and close the channel
	cancel()

	// Drain channel until it's closed
	for range ch {
		// consume any events
	}
	// Channel is closed, test passes
}

// =============================================================================
// WatchSecretsByPrefix - Initial List Error
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_ListError(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	// Make List return an error so the goroutine exits early
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, stderrors.New("storage unavailable")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/secrets/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// The goroutine should exit after the list error and close the channel
	for range ch {
		// consume any events
	}
	// Channel is closed, test passes
}

// =============================================================================
// WatchSecretsByPrefix - Nil Logger Path
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_NilLoggerWithListError(t *testing.T) {
	mockMgr := newMockSecretsManager()
	svc := newSecretsService(mockMgr, nil)

	// Make List return error so goroutine exits quickly
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, stderrors.New("unavailable")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Wait for channel to close (goroutine exits due to list error)
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Initial Get Error (secrets exist but can't be read)
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_InitialGetError(t *testing.T) {
	mockMgr := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockMgr, logger)

	// List returns paths but Get fails for those paths
	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"app/secret1", "app/secret2"}, nil
	}
	mockMgr.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		return nil, nil, stderrors.New("get failed")
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Cancel to stop the watch loop
	cancel()

	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - Nil Logger with valid prefix (debug log path)
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_ValidPrefixNilLogger(t *testing.T) {
	mockMgr := newMockSecretsManager()
	svc := newSecretsService(mockMgr, nil)

	mockMgr.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := &WatchSecretsRequest{
		Prefix: "app/",
	}

	ch, err := svc.WatchSecretsByPrefix(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ch)

	cancel()
	for range ch {
	}
}

// =============================================================================
// WatchSecretsByPrefix - NilRequest with nil logger
// =============================================================================

func TestSecretsService_WatchSecretsByPrefix_NilRequest_NilLogger(t *testing.T) {
	mockMgr := newMockSecretsManager()
	svc := newSecretsService(mockMgr, nil)

	ch, err := svc.WatchSecretsByPrefix(context.Background(), nil)
	assert.Nil(t, ch)
	require.Error(t, err)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

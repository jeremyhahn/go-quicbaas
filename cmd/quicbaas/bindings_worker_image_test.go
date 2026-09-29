// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build desktop

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// workerImageBindingClient is an SDK client whose SDLC() is the CLI's own
// mockSDLCClient.
type workerImageBindingClient struct {
	mockClient
	sdlc *mockSDLCClient
}

func (c *workerImageBindingClient) SDLC() quicbaas.SDLCClient     { return c.sdlc }
func (c *workerImageBindingClient) Agents() quicbaas.AgentsClient { return nil }

func newWorkerImageBinding(t *testing.T, sdlc *mockSDLCClient) *OrchestratorBinding {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	conn := NewConnectionBinding(log)
	var client quicbaas.Client = &workerImageBindingClient{sdlc: sdlc}
	conn.client.Store(&client)
	binding := NewOrchestratorBinding(log, conn)
	binding.ctx = context.Background()
	return binding
}

func TestOrchestratorBinding_WorkerImageVerbsReachTheSDLCClient(t *testing.T) {
	mock := &mockSDLCClient{statuses: []*sdlcSDK.WorkerImageStatus{{Serving: true, Last: &sdlcSDK.WorkerImageReport{Outcome: "current"}}}}
	binding := newWorkerImageBinding(t, mock)

	status, err := binding.GetWorkerImageStatus()
	require.NoError(t, err)
	assert.True(t, status.Serving)
	assert.Equal(t, "current", status.Last.Outcome)

	queued, err := binding.RefreshWorkerImage()
	require.NoError(t, err)
	assert.True(t, queued.Queued)
	assert.Equal(t, 1, mock.refreshes)
}

func TestOrchestratorBinding_WorkerImageVerbsSurfaceErrors(t *testing.T) {
	binding := newWorkerImageBinding(t, &mockSDLCClient{err: errors.New("a refresh is already in progress")})
	status, err := binding.RefreshWorkerImage()
	require.Error(t, err)
	assert.Nil(t, status)
	status, err = binding.GetWorkerImageStatus()
	require.Error(t, err)
	assert.Nil(t, status)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	unconnected := NewOrchestratorBinding(log, NewConnectionBinding(log))
	_, err = unconnected.GetWorkerImageStatus()
	require.Error(t, err, "a binding with no connection must refuse")
	_, err = unconnected.RefreshWorkerImage()
	require.Error(t, err)
}

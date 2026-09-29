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

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestPromptAppendFlags_Value(t *testing.T) {
	file := filepath.Join(t.TempDir(), "context.md")
	require.NoError(t, os.WriteFile(file, []byte("from a file\n"), 0o600))

	cases := map[string]struct {
		flags promptAppendFlags
		stdin string
		want  string
	}{
		"nothing": {promptAppendFlags{}, "", ""},
		"inline":  {promptAppendFlags{text: "inline"}, "", "inline"},
		"file":    {promptAppendFlags{file: file}, "", "from a file\n"},
		"stdin":   {promptAppendFlags{file: "-"}, "piped", "piped"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.flags.value(strings.NewReader(tc.stdin))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPromptAppendFlags_UnreadableFileIsATypedError(t *testing.T) {
	flags := promptAppendFlags{file: filepath.Join(t.TempDir(), "missing.md")}
	_, err := flags.value(strings.NewReader(""))
	var readErr *PromptAppendReadError
	require.True(t, errors.As(err, &readErr), "err = %v", err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "missing.md")
}

func TestPromptAppendFlags_InlineAndFileAreExclusive(t *testing.T) {
	for _, cmd := range []interface {
		ParseFlags([]string) error
		ValidateFlagGroups() error
	}{newOrchSpawnCommand(), newOrchRunCommand(), newOrchTaskCreateCommand()} {
		require.NoError(t, cmd.ParseFlags([]string{"--prompt-append", "a", "--prompt-append-file", "b"}))
		assert.Error(t, cmd.ValidateFlagGroups())
	}
}

func TestOrchestratorSpawnCarriesPromptAppend(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	var got *orchestratorSDK.SpawnSessionRequest
	setMockOrchestratorFactory(&mockOrchestratorClient{
		spawnSessionFn: func(_ context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			got = req
			return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess_new"}, nil
		},
	})
	orchOutputFmt = "json"

	cmd := newOrchSpawnCommand()
	require.NoError(t, cmd.Flags().Set("agent", "claude-coder"))
	require.NoError(t, cmd.Flags().Set("prompt-append", "work on pkg/cache only"))
	require.NoError(t, cmd.RunE(cmd, []string{}))
	require.NotNil(t, got)
	assert.Equal(t, "work on pkg/cache only", got.PromptAppend)
}

func TestOrchestratorSpawnRefusesAnUnreadablePromptAppendFile(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	called := false
	setMockOrchestratorFactory(&mockOrchestratorClient{
		spawnSessionFn: func(_ context.Context, _ *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			called = true
			return &orchestratorSDK.SpawnSessionResponse{}, nil
		},
	})
	cmd := newOrchSpawnCommand()
	require.NoError(t, cmd.Flags().Set("agent", "claude-coder"))
	require.NoError(t, cmd.Flags().Set("prompt-append-file", filepath.Join(t.TempDir(), "missing.md")))
	assert.Error(t, cmd.RunE(cmd, []string{}))
	assert.False(t, called, "nothing is spawned when the material cannot be read")
}

func TestOrchestratorTaskCreateCarriesPromptAppend(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	var got *orchestratorSDK.CreateTaskRequest
	setMockOrchestratorFactory(&mockOrchestratorClient{
		createTaskFn: func(_ context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error) {
			got = req
			return &orchestratorSDK.Task{ID: "task_new", Subject: req.Subject}, nil
		},
	})
	orchOutputFmt = "json"

	cmd := newOrchTaskCreateCommand()
	orchTaskSubject = "Build the project"
	require.NoError(t, cmd.Flags().Set("prompt-append", "use the staging cluster"))
	require.NoError(t, cmd.RunE(cmd, []string{}))
	require.NotNil(t, got)
	assert.Equal(t, "use the staging cluster", got.PromptAppend)
}

func TestOrchestratorRunCarriesPromptAppend(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	var got *orchestratorSDK.StartJobRequest
	refused := errors.New("refused")
	setMockOrchestratorFactory(&mockOrchestratorClient{
		startJobFn: func(_ context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
			got = req
			return nil, refused
		},
	})

	cmd := newOrchRunCommand()
	require.NoError(t, cmd.Flags().Set("prompt-append", "the lead reads this"))
	assert.Error(t, cmd.RunE(cmd, []string{"Fix the flaky test"}))
	require.NotNil(t, got)
	assert.Equal(t, "the lead reads this", got.PromptAppend)
}

func TestPromptAppendLabel(t *testing.T) {
	assert.Equal(t, "-", promptAppendLabel(""))
	assert.Equal(t, "-", promptAppendLabel(" \n"))
	assert.Equal(t, "5 bytes", promptAppendLabel("hello"))
}

func TestReviewerSwitchNote_SaysNothingWasCancelled(t *testing.T) {
	started := reviewerSwitchNote(&sdlcSDK.ReviewerSelection{Profile: "qwen", Redispatched: true})
	assert.Equal(t, "A review by qwen started now.", started)

	waiting := reviewerSwitchNote(&sdlcSDK.ReviewerSelection{Profile: "qwen"})
	assert.Contains(t, waiting, "Nothing was cancelled")
	assert.Contains(t, waiting, "next review dispatch uses qwen")
}

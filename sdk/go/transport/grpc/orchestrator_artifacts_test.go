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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestOrchestratorHandler_SubmitTaskArtifacts_RoundTrips(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	raw, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "SubmitTaskArtifacts",
		&orchestrator.SubmitTaskArtifactsRequest{
			TenantID: "t1",
			TaskID:   "task-7",
			Artifacts: []*orchestrator.TaskArtifact{
				{Kind: "inline", Name: "notes.txt", MIMEType: "text/plain", Content: []byte("hi")},
				{Kind: "commit", Name: "fix", Ref: &orchestrator.TaskArtifactRef{SHA: "abc123", Branch: "main"}},
			},
		})
	require.NoError(t, err)

	resp, ok := raw.(*orchestrator.SubmitTaskArtifactsResponse)
	require.True(t, ok, "handler returned %T", raw)
	assert.Equal(t, "task-7", resp.TaskID)
	assert.Equal(t, 2, resp.Stored)
}

func TestOrchestratorHandler_GetTaskArtifact_RoundTrips(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	raw, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "GetTaskArtifact",
		&orchestrator.GetTaskArtifactRequest{TenantID: "t1", TaskID: "task-7", Name: "fix"})
	require.NoError(t, err)

	art, ok := raw.(*orchestrator.TaskArtifact)
	require.True(t, ok, "handler returned %T", raw)
	assert.Equal(t, "fix", art.Name)
	assert.Equal(t, "commit", art.Kind)
	assert.Equal(t, "text/plain", art.MIMEType)
	assert.Equal(t, []byte("hello"), art.Content)
	require.NotNil(t, art.Ref)
	assert.Equal(t, "a1", art.Ref.AgentID)
	assert.Equal(t, "out.txt", art.Ref.Path)
	assert.Equal(t, "abc123", art.Ref.SHA)
	assert.Equal(t, "main", art.Ref.Branch)
}

func TestOrchestratorHandler_TaskArtifacts_WrongRequestType(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	for _, method := range []string{"SubmitTaskArtifacts", "GetTaskArtifact"} {
		t.Run(method, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", method,
				&orchestrator.ListTaskArtifactsRequest{TaskID: "t1"})
			require.Error(t, err)
			assert.True(t, sdkerrors.IsInvalidInput(err), "want invalid input, got %v", err)
		})
	}
}

func TestSDKArtifactToProto_NilAndRef(t *testing.T) {
	assert.Nil(t, sdkArtifactToProto(nil))

	in := &orchestrator.TaskArtifact{
		Kind:     "file",
		Name:     "out.bin",
		MIMEType: "application/octet-stream",
		Size:     42,
		Metadata: map[string]string{"k": "v"},
		Ref:      &orchestrator.TaskArtifactRef{AgentID: "a1", Path: "/tmp/out.bin", SHA: "deadbeef", Branch: "dev"},
	}
	out := protoArtifactToSDK(sdkArtifactToProto(in))
	assert.Equal(t, in, out)

	noRef := sdkArtifactToProto(&orchestrator.TaskArtifact{Name: "x"})
	assert.Nil(t, noRef.GetRef())
}

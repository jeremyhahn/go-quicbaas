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

// Package main: error-branch coverage for runOrchAttach.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunOrchAttach_FactoryError surfaces the factory failure.
func TestRunOrchAttach_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors.New("factory boom"))
	err := runOrchAttach(newOrchAttachCommand(), []string{"sess-1"})
	require.Error(t, err)
}

// TestRunOrchAttach_SubscribeError exercises the subscribe-error branch.
func TestRunOrchAttach_SubscribeError(t *testing.T) {
	mc := &mockOrchestratorClient{}
	wantErr := stderrors.New("subscribe boom")
	setSubscribeSessionEventsFn(mc, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		return nil, wantErr
	})
	t.Cleanup(func() { clearSessionOverrides(mc) })
	withMockOrchestrator(t, mc)

	err := runOrchAttach(newOrchAttachCommand(), []string{"sess-1"})
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestRunOrchAttach_BadFormatRejected exercises the renderer-dispatch
// error branch by setting --format to an unknown value through the
// package-level attachFormat global.
func TestRunOrchAttach_BadFormatRejected(t *testing.T) {
	mc := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mc, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		close(ch)
		return ch, nil
	})
	t.Cleanup(func() { clearSessionOverrides(mc) })
	withMockOrchestrator(t, mc)

	cmd := newOrchAttachCommand()
	orig := attachFormat
	attachFormat = "yaml"
	t.Cleanup(func() { attachFormat = orig })

	err := runOrchAttach(cmd, []string{"sess-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --format")
}

// TestRunOrchAttach_HappyEmptyStream consumes a closed-empty event
// channel and returns nil.
func TestRunOrchAttach_HappyEmptyStream(t *testing.T) {
	mc := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mc, func(_ context.Context, _ *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		close(ch)
		return ch, nil
	})
	t.Cleanup(func() { clearSessionOverrides(mc) })
	withMockOrchestrator(t, mc)

	cmd := newOrchAttachCommand()
	orig := attachFormat
	attachFormat = "pretty"
	t.Cleanup(func() { attachFormat = orig })

	require.NoError(t, runOrchAttach(cmd, []string{"sess-1"}))
}

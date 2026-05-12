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

package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// This file extends mockOrchestratorClient (defined in orchestrator_test.go)
// with the session history / subscription methods added in Phase 4. Keeping
// the extension here avoids disturbing the primary mock definition while
// ensuring the test build compiles against the full OrchestratorClient
// interface.

// mockSessionOverrides holds the optional overrides for session subscription
// and history. Because a Go struct can't be re-opened across files, the
// overrides live in a package-level map keyed by the mock pointer. Tests
// attach a callback via setSessionHistoryFn / setSubscribeSessionEventsFn
// and it is invoked from the method implementations below.
type mockSessionOverrides struct {
	history   func(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error)
	subscribe func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error)
}

var mockSessionOverrideRegistry = map[*mockOrchestratorClient]*mockSessionOverrides{}

// setSessionHistoryFn installs a history override for the given mock.
func setSessionHistoryFn(m *mockOrchestratorClient, fn func(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error)) {
	o, ok := mockSessionOverrideRegistry[m]
	if !ok {
		o = &mockSessionOverrides{}
		mockSessionOverrideRegistry[m] = o
	}
	o.history = fn
}

// setSubscribeSessionEventsFn installs a subscribe override for the given mock.
func setSubscribeSessionEventsFn(m *mockOrchestratorClient, fn func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error)) {
	o, ok := mockSessionOverrideRegistry[m]
	if !ok {
		o = &mockSessionOverrides{}
		mockSessionOverrideRegistry[m] = o
	}
	o.subscribe = fn
}

// clearSessionOverrides removes any overrides attached to the mock. Tests
// should call this in t.Cleanup to avoid leaking entries across runs.
func clearSessionOverrides(m *mockOrchestratorClient) {
	delete(mockSessionOverrideRegistry, m)
}

// GetSessionHistory satisfies orchestratorSDK.Client. Returns an empty page
// by default; tests install overrides via setSessionHistoryFn.
func (m *mockOrchestratorClient) GetSessionHistory(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error) {
	if o, ok := mockSessionOverrideRegistry[m]; ok && o.history != nil {
		return o.history(ctx, req)
	}
	return &orchestratorSDK.GetSessionHistoryResponse{
		Events:  []*orchestratorSDK.SessionEventEnvelope{},
		NextSeq: 0,
	}, nil
}

// SubscribeSessionEvents satisfies orchestratorSDK.Client. Returns an error
// by default to simulate a transport that does not support streaming; tests
// install overrides via setSubscribeSessionEventsFn.
func (m *mockOrchestratorClient) SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
	if o, ok := mockSessionOverrideRegistry[m]; ok && o.subscribe != nil {
		return o.subscribe(ctx, req)
	}
	return nil, errors.New(errors.ErrUnavailable, "orchestrator.SubscribeSessionEvents", "streaming not supported")
}

// Inference satisfies OrchestratorClient. Delegates to inferenceFn when set.
func (m *mockOrchestratorClient) Inference(ctx context.Context, req *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
	if m.inferenceFn != nil {
		return m.inferenceFn(ctx, req)
	}
	return &orchestratorSDK.InferenceResponse{}, nil
}

// InferenceStream satisfies OrchestratorClient. Delegates to inferenceStreamFn when set.
func (m *mockOrchestratorClient) InferenceStream(ctx context.Context, req *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
	if m.inferenceStreamFn != nil {
		return m.inferenceStreamFn(ctx, req)
	}
	ch := make(chan *orchestratorSDK.InferenceChunk)
	close(ch)
	return ch, nil
}

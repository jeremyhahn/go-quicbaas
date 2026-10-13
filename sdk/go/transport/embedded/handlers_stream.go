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

package embedded

import (
	"context"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// Stream Handlers
// =============================================================================

// handleListStream handles the KV List streaming operation.
func handleListStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*service.ListRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.KV", "List", "*service.ListRequest", reflect.TypeOf(req).String())
	}

	// Create a channel for the service to send responses
	responseCh := make(chan *service.ListResponse, 100)

	// Start the service operation in a goroutine
	// Note: responseCh is closed by the service method, not here
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.List(ctx, r, responseCh)
	}()

	// Forward responses to the stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case resp, ok := <-responseCh:
			if !ok {
				// Channel closed, check for error
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			// Send response to stream
			if err := s.deliver(resp); err != nil {
				return err
			}
		}
	}
}

// handleWatchStream handles the KV Watch streaming operation.
func handleWatchStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*service.WatchRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.KV", "Watch", "*service.WatchRequest", reflect.TypeOf(req).String())
	}

	// Create a channel for the service to send responses
	responseCh := make(chan *service.WatchResponse, 100)

	// Start the watch operation
	watchID, err := svc.Watch(ctx, r, responseCh)
	if err != nil {
		return err
	}

	// Send the initial watch confirmation with watch ID
	if err := s.deliver(&service.WatchResponse{WatchID: watchID, Created: true}); err != nil {
		return err
	}

	// Forward responses to the stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case resp, ok := <-responseCh:
			if !ok {
				return nil
			}
			// Send response to stream
			if err := s.deliver(resp); err != nil {
				return err
			}
		}
	}
}

// handleQueryStream handles the Query streaming operation.
func handleQueryStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*service.QueryRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.Query", "Query", "*service.QueryRequest", reflect.TypeOf(req).String())
	}

	// Create a channel for the service to send responses
	responseCh := make(chan *service.QueryResponse, 100)

	// Start the service operation in a goroutine
	// Note: responseCh is closed by the service method, not here
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Query(ctx, r, responseCh)
	}()

	// Forward responses to the stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case resp, ok := <-responseCh:
			if !ok {
				// Channel closed, check for error
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			// Send response to stream
			if err := s.deliver(resp); err != nil {
				return err
			}
		}
	}
}

// handleScanIndexStream handles the ScanIndex streaming operation.
func handleScanIndexStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*service.ScanRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.Query", "ScanIndex", "*service.ScanRequest", reflect.TypeOf(req).String())
	}

	// Create a channel for the service to send responses
	responseCh := make(chan *service.ScanResponse, 100)

	// Start the service operation in a goroutine
	// Note: responseCh is closed by the service method, not here
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.ScanIndex(ctx, r, responseCh)
	}()

	// Forward responses to the stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case resp, ok := <-responseCh:
			if !ok {
				// Channel closed, check for error
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			// Send response to stream
			if err := s.deliver(resp); err != nil {
				return err
			}
		}
	}
}

// handleTimeSeriesQueryStream handles the TimeSeries Query streaming operation.
func handleTimeSeriesQueryStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*service.TimeSeriesQueryRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.TimeSeries", "Query", "*service.TimeSeriesQueryRequest", reflect.TypeOf(req).String())
	}

	// Create a channel for the service to send responses
	responseCh := make(chan *service.TimeSeriesQueryResponse, 100)

	// Start the service operation in a goroutine
	// Note: responseCh is closed by the service method, not here
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.TimeSeriesQuery(ctx, r, responseCh)
	}()

	// Forward responses to the stream
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case resp, ok := <-responseCh:
			if !ok {
				// Channel closed, check for error
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			// Send response to stream
			if err := s.deliver(resp); err != nil {
				return err
			}
		}
	}
}

// handleOrchestratorInference handles the Orchestrator Inference unary operation in-process.
// It calls AgentService.Infer, accumulates all chunks, and returns an *orchestratorSDK.InferenceResponse.
func handleOrchestratorInference(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*orchestratorSDK.InferenceRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Orchestrator", "Inference",
			"*orchestrator.InferenceRequest", reflect.TypeOf(req).String())
	}

	agentSvc := svc.Agent()
	if agentSvc == nil {
		return nil, NewUnknownMethodError("quicbaas.Orchestrator", "Inference")
	}

	inferReq := &orchestration.InferRequest{
		Backend:      r.Backend,
		Model:        r.Model,
		Prompt:       r.Prompt,
		SystemPrompt: r.SystemPrompt,
		HostAffinity: r.HostAffinity,
		Metadata:     r.Metadata,
	}

	chunks, err := agentSvc.Infer(ctx, inferReq)
	if err != nil {
		return nil, err
	}

	resp, err := orchestration.AccumulateChunks(chunks)
	if err != nil {
		return nil, err
	}

	// Convert orchestration.ContentBlock to SDK InferenceContentBlock.
	sdkContent := make([]orchestratorSDK.InferenceContentBlock, len(resp.Content))
	for i, block := range resp.Content {
		sdkContent[i] = orchestratorSDK.InferenceContentBlock{
			Type: block.Type,
			Text: block.Text,
		}
	}

	return &orchestratorSDK.InferenceResponse{
		Content:    sdkContent,
		Model:      resp.Model,
		StopReason: resp.StopReason,
	}, nil
}

// handleSubscribeJobEventsStream handles the Orchestrator SubscribeJobEvents streaming operation.
func handleSubscribeJobEventsStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	r, ok := req.(*orchestratorSDK.SubscribeJobEventsRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.Orchestrator", "SubscribeJobEvents",
			"*orchestrator.SubscribeJobEventsRequest", reflect.TypeOf(req).String())
	}

	agentSvc := svc.Agent()
	if agentSvc == nil {
		return NewUnknownMethodError("quicbaas.Orchestrator", "SubscribeJobEvents")
	}

	eventCh, err := agentSvc.SubscribeJobEvents(ctx, r.TenantID, r.JobID)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case evt, ok := <-eventCh:
			if !ok {
				return nil
			}
			// Convert orchestration.JobEvent to SDK orchestrator.JobEvent
			sdkEvt := &orchestratorSDK.JobEvent{
				JobID:     evt.JobID,
				Type:      evt.Type,
				State:     string(evt.State),
				Message:   evt.Message,
				Iteration: evt.Iteration,
				Metadata:  evt.Metadata,
				Timestamp: evt.Timestamp,
			}
			if err := s.deliver(sdkEvt); err != nil {
				return err
			}
		}
	}
}

// handleOrchestratorInferenceStream handles the Orchestrator InferenceStream streaming operation.
// It calls AgentService.Infer and forwards each InferChunk as an *orchestratorSDK.InferenceChunk.
func handleOrchestratorInferenceStream(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	// InferenceStream receives an *InferenceRequest from the SDK client.
	r, ok := req.(*orchestratorSDK.InferenceRequest)
	if !ok {
		return NewInvalidRequestTypeError("quicbaas.Orchestrator", "InferenceStream",
			"*orchestrator.InferenceRequest", reflect.TypeOf(req).String())
	}

	agentSvc := svc.Agent()
	if agentSvc == nil {
		return NewUnknownMethodError("quicbaas.Orchestrator", "InferenceStream")
	}

	inferReq := &orchestration.InferRequest{
		Backend:      r.Backend,
		Model:        r.Model,
		Prompt:       r.Prompt,
		SystemPrompt: r.SystemPrompt,
		HostAffinity: r.HostAffinity,
		Metadata:     r.Metadata,
	}

	chunkCh, err := agentSvc.Infer(ctx, inferReq)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.context().Done():
			return s.context().Err()
		case chunk, ok := <-chunkCh:
			if !ok {
				return nil
			}
			// Convert orchestration.ContentBlock slices to SDK InferenceContentBlock.
			sdkDelta := make([]orchestratorSDK.InferenceContentBlock, len(chunk.Delta))
			for i, block := range chunk.Delta {
				sdkDelta[i] = orchestratorSDK.InferenceContentBlock{
					Type: block.Type,
					Text: block.Text,
				}
			}
			sdkChunk := &orchestratorSDK.InferenceChunk{
				Delta:      sdkDelta,
				Model:      chunk.Model,
				StopReason: chunk.StopReason,
				Final:      chunk.Final,
			}
			if err := s.deliver(sdkChunk); err != nil {
				return err
			}
		}
	}
}

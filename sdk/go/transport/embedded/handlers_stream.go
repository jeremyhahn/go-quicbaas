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
			select {
			case s.recvChan() <- resp:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
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
	select {
	case s.recvChan() <- &service.WatchResponse{
		WatchID: watchID,
		Created: true,
	}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.context().Done():
		return s.context().Err()
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
			select {
			case s.recvChan() <- resp:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
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
			select {
			case s.recvChan() <- resp:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
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
			select {
			case s.recvChan() <- resp:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
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
			select {
			case s.recvChan() <- resp:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
			}
		}
	}
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
			select {
			case s.recvChan() <- sdkEvt:
			case <-ctx.Done():
				return ctx.Err()
			case <-s.context().Done():
				return s.context().Err()
			}
		}
	}
}

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

package embedded

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

var errServiceFailed = errors.New("service failed")

// streamHandler is the shape shared by the stream handlers under test.
type streamHandler func(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error

// assertServiceErrorSurvivesAClosedStream calls handler against a service
// that, like the production one, closes its response channel and only then
// returns its error. The service double closes its channel and blocks on
// release. Inside the synctest bubble, synctest.Wait returns once the handler
// has either returned or is blocked waiting for the verdict, so a handler that
// reads the closed channel as success is caught without depending on the
// scheduler. The service is let go before any failure is reported, so the
// bubble always unwinds.
func assertServiceErrorSurvivesAClosedStream(t *testing.T, handler streamHandler, newService func(release <-chan struct{}) service.QuicBaaS, req interface{}) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		svc := newService(release)
		ctx := context.Background()

		done := make(chan error, 1)
		go func() {
			done <- handler(ctx, svc, req, newStream(ctx, 1))
		}()

		synctest.Wait()
		var early error
		returnedEarly := false
		select {
		case early = <-done:
			returnedEarly = true
		default:
		}

		close(release)
		if returnedEarly {
			t.Fatalf("handler returned %v before the service returned its verdict", early)
		}
		if err := <-done; !errors.Is(err, errServiceFailed) {
			t.Fatalf("err = %v, want %v", err, errServiceFailed)
		}
	})
}

// TestServiceVerdict_StreamClosed checks the wait ends when the caller's stream
// does, even if the service never publishes a verdict.
func TestServiceVerdict_StreamClosed(t *testing.T) {
	ctx := context.Background()
	s := newStream(ctx, 1)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := serviceVerdict(ctx, s, make(chan error)); err == nil {
		t.Fatal("serviceVerdict returned nil for a stream that was closed")
	}
}

func TestHandleListStream_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	newService := func(release <-chan struct{}) service.QuicBaaS {
		return &mockQuicBaaS{
			listFunc: func(_ context.Context, _ *service.ListRequest, stream chan<- *service.ListResponse) error {
				close(stream)
				<-release
				return errServiceFailed
			},
		}
	}
	assertServiceErrorSurvivesAClosedStream(t, handleListStream, newService, &service.ListRequest{EntityType: "users"})
}

func TestHandleQueryStream_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	newService := func(release <-chan struct{}) service.QuicBaaS {
		return &mockQuicBaaS{
			queryFunc: func(_ context.Context, _ *service.QueryRequest, stream chan<- *service.QueryResponse) error {
				close(stream)
				<-release
				return errServiceFailed
			},
		}
	}
	assertServiceErrorSurvivesAClosedStream(t, handleQueryStream, newService, &service.QueryRequest{EntityType: "users"})
}

func TestHandleScanIndexStream_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	newService := func(release <-chan struct{}) service.QuicBaaS {
		return &mockQuicBaaS{
			scanIndexFunc: func(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
				close(stream)
				<-release
				return errServiceFailed
			},
		}
	}
	assertServiceErrorSurvivesAClosedStream(t, handleScanIndexStream, newService, &service.ScanRequest{EntityType: "users"})
}

func TestHandleTimeSeriesQueryStream_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	newService := func(release <-chan struct{}) service.QuicBaaS {
		return &mockQuicBaaS{
			timeSeriesQueryFunc: func(_ context.Context, _ *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
				close(stream)
				<-release
				return errServiceFailed
			},
		}
	}
	assertServiceErrorSurvivesAClosedStream(t, handleTimeSeriesQueryStream, newService, &service.TimeSeriesQueryRequest{})
}

// TestHandleQueryStream_SuccessReturnsNil checks the verdict wait does not
// turn a successful call into anything but nil once its response is forwarded.
func TestHandleQueryStream_SuccessReturnsNil(t *testing.T) {
	svc := &mockQuicBaaS{
		queryFunc: func(_ context.Context, _ *service.QueryRequest, stream chan<- *service.QueryResponse) error {
			stream <- &service.QueryResponse{Entities: []*service.Entity{{ID: 1}}}
			close(stream)
			return nil
		},
	}
	ctx := context.Background()
	s := newStream(ctx, 1)

	if err := handleQueryStream(ctx, svc, &service.QueryRequest{EntityType: "users"}, s); err != nil {
		t.Fatalf("err = %v on a successful call", err)
	}
	got, err := s.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if resp, ok := got.(*service.QueryResponse); !ok || len(resp.Entities) != 1 {
		t.Fatalf("forwarded %v, want the one response the service sent", got)
	}
}

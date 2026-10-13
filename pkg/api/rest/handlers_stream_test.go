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

package rest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The service closes stream on its way out and publishes its error afterwards
// -- `defer close(stream)` is the first statement in kvService.List -- so
// every test here reproduces that lifecycle. Leaving stream open, as the
// handler-level mocks do, is what let a fail-open live in this path unnoticed:
// a double that never closes the channel cannot exhibit the behaviour
// production has.
func completedList(page *service.ListResponse, listErr error) (<-chan *service.ListResponse, <-chan error) {
	stream := make(chan *service.ListResponse, 1)
	errChan := make(chan error, 1)
	if page != nil {
		stream <- page
	}
	close(stream)
	if listErr != nil {
		errChan <- listErr
	}
	close(errChan)
	return stream, errChan
}

// TestFirstListResponse_PageSurvivesACompletedCall is the regression test for
// the intermittent empty page. Both channels are ready before the select is
// evaluated, which is the state a finished call leaves behind. Go picks
// uniformly at random among ready cases, so on the old code each iteration was
// an independent coin flip between the page and an empty result.
func TestFirstListResponse_PageSurvivesACompletedCall(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		want := &service.ListResponse{
			Entities:   []*service.Entity{{ID: 1}},
			TotalCount: 1,
		}
		stream, errChan := completedList(want, nil)

		resp, timedOut, err := firstStreamResponse(context.Background(), stream, errChan)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
		if timedOut {
			t.Fatalf("iteration %d: reported a timeout on a completed call", i)
		}
		if resp != want {
			t.Fatalf("iteration %d: the page was dropped; a completed List that "+
				"returned %d rows was observed as %v", i, want.TotalCount, resp)
		}
	}
}

// TestFirstStreamResponse_QueryResultSurvivesACompletedCall is the #1284
// regression: Query buffered its one response and returned nil, so both the
// response and the closed errChan were ready when HandleQuery selected, and
// the handler answered with an empty result whenever errChan won.
func TestFirstStreamResponse_QueryResultSurvivesACompletedCall(t *testing.T) {
	const iterations = 64

	for i := range iterations {
		want := &service.QueryResponse{
			Entities:   []*service.Entity{{ID: 1}},
			TotalCount: 1,
		}
		stream := make(chan *service.QueryResponse, 1)
		errChan := make(chan error, 1)
		stream <- want
		close(stream)
		close(errChan)

		resp, timedOut, err := firstStreamResponse(context.Background(), stream, errChan)
		if err != nil || timedOut {
			t.Fatalf("iteration %d: err = %v, timedOut = %v on a completed call", i, err, timedOut)
		}
		if resp != want {
			t.Fatalf("iteration %d: the query result was dropped; got %v", i, resp)
		}
	}
}

// TestFirstListResponse_ServiceErrorSurvivesAClosedStream is the fail-open
// this path carried before the two-value receive.
//
// On failure the service closes stream BEFORE its error reaches errChan, so a
// one-value receive sees a nil response and no error -- and it wins the select
// essentially always, because closing stream wakes the blocked receiver before
// the goroutine reaches its send. Every List failure, authentication and
// authorization failures included, was rendered as an empty list with HTTP 200.
//
// Repeated so a fix that merely reorders the cases cannot pass by luck.
func TestFirstListResponse_ServiceErrorSurvivesAClosedStream(t *testing.T) {
	const iterations = 20

	wantErr := errors.New("list failed")

	for i := range iterations {
		stream, errChan := completedList(nil, wantErr)

		resp, timedOut, err := firstStreamResponse(context.Background(), stream, errChan)
		if !errors.Is(err, wantErr) {
			t.Fatalf("iteration %d: err = %v, want %v -- a service failure was "+
				"reported as an empty success", i, err, wantErr)
		}
		if timedOut {
			t.Errorf("iteration %d: reported a timeout on a service error", i)
		}
		if resp != nil {
			t.Errorf("iteration %d: resp = %v, want nil on a service error", i, resp)
		}
	}
}

// TestFirstListResponse_EmptyResultIsNotAnError covers a List that completes
// having sent nothing. That is a legal empty result, not a failure, and the
// fix for the fail-open above must not turn it into one.
func TestFirstListResponse_EmptyResultIsNotAnError(t *testing.T) {
	stream, errChan := completedList(nil, nil)

	resp, timedOut, err := firstStreamResponse(context.Background(), stream, errChan)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if timedOut {
		t.Error("reported a timeout on a completed call")
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
}

// TestFirstListResponse_ContextCancellation covers the third outcome, which
// the caller renders as 408 rather than as an empty page.
func TestFirstListResponse_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, timedOut, err := firstStreamResponse(ctx, make(chan *service.ListResponse, 1), make(chan error, 1))
	if !timedOut {
		t.Error("a cancelled context was not reported as a timeout")
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
}

// TestFirstListResponse_ClosedStreamWithNoCompletionYetStillWaits pins that
// the closed-stream branch waits for the verdict instead of racing it: the
// error arrives only after the call is already in flight.
func TestFirstListResponse_ClosedStreamWithNoCompletionYetStillWaits(t *testing.T) {
	wantErr := errors.New("late failure")

	stream := make(chan *service.ListResponse, 1)
	close(stream)
	errChan := make(chan error, 1)

	go func() {
		errChan <- wantErr
		close(errChan)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, timedOut, err := firstStreamResponse(ctx, stream, errChan)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if timedOut {
		t.Error("gave up on the verdict instead of waiting for it")
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
}

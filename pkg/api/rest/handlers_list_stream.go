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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// firstListResponse waits for the first page of a streaming List call.
//
// The service reports through two channels and closes both. It closes stream
// on its way out -- `defer close(stream)` is the first statement in
// kvService.List -- and the caller's goroutine publishes the returned error on
// errChan only afterwards. Two consequences follow, and this function exists
// for both.
//
// On success the page is buffered and the receive yields it. But once the call
// finishes, the completion signal is ready too, and a select with several
// ready cases picks uniformly at random -- so reading the completion first
// used to discard a page that was already in the buffer, and the caller
// rendered an empty page for a query that returned rows.
//
// On failure stream is closed BEFORE the error is published, so a one-value
// receive yields a nil response with no error at all -- and it wins the select
// essentially always, because closing stream wakes the blocked receiver before
// the goroutine reaches its send. A service error, including an authentication
// or authorization failure, was therefore rendered as an empty list with HTTP
// 200. The two-value receive is what separates "closed, so ask why" from "here
// is your page".
//
// Neither branch can hang: stream is closed only by a List that has returned,
// so the completion it is waiting for is a couple of instructions away, and
// ctx.Done backstops both.
//
// The three outcomes are distinct and the caller renders them differently:
// a page (which may be nil when List sent nothing, a legal empty result), a
// service error, and the request context expiring.
func firstListResponse(
	ctx context.Context,
	stream <-chan *service.ListResponse,
	errChan <-chan error,
) (resp *service.ListResponse, timedOut bool, err error) {
	select {
	case page, ok := <-stream:
		if ok {
			return page, false, nil
		}
		// Closed without a page. List has returned, so its verdict is on
		// errChan; an empty result and a failure are indistinguishable here
		// and must not both be reported as success.
		select {
		case listErr := <-errChan:
			return nil, false, listErr
		case <-ctx.Done():
			return nil, true, nil
		}

	case listErr := <-errChan:
		if listErr != nil {
			return nil, false, listErr
		}
		// List returned without error, so any page it sent is already in the
		// buffer. Take it if it is there.
		select {
		case page, ok := <-stream:
			if ok {
				resp = page
			}
		default:
		}
		return resp, false, nil

	case <-ctx.Done():
		return nil, true, nil
	}
}

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

import "context"

// firstStreamResponse waits for the first page of a streaming service call
// such as List or Query.
//
// The service reports through two channels and closes both. It closes stream
// on its way out -- `defer close(stream)` is the first statement in
// kvService.List and queryService.Query -- and the caller's goroutine
// publishes the returned error on errChan only afterwards. Two consequences follow, and this function exists
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
// Neither branch can hang: stream is closed only by a call that has returned,
// so the completion it is waiting for is a couple of instructions away, and
// ctx.Done backstops both.
//
// The three outcomes are distinct and the caller renders them differently:
// a page (which may be the zero value when the call sent nothing, a legal empty
// result), a service error, and the request context expiring. When more than
// one is available they take precedence in that order: a page beats a service
// error, and either beats a timeout. A call that produced neither a page nor
// an error while the context had expired is a timeout, never an empty success:
// its empty result may only be an artefact of the cancellation (go-quicbaas#1326).
func firstStreamResponse[T any](
	ctx context.Context,
	stream <-chan T,
	errChan <-chan error,
) (resp T, timedOut bool, err error) {
	select {
	case page, ok := <-stream:
		if ok {
			return page, false, nil
		}
		// Closed without a page. The call has returned, so its verdict is
		// on errChan; an empty result and a failure are indistinguishable
		// here and must not both be reported as success.
		select {
		case callErr := <-errChan:
			if callErr != nil {
				return resp, false, callErr
			}
			return resp, ctx.Err() != nil, nil
		case <-ctx.Done():
			return settleExpired(stream, errChan)
		}

	case callErr := <-errChan:
		if callErr != nil {
			return resp, false, callErr
		}
		// The call returned without error, so any page it sent is already
		// in the buffer. Take it if it is there.
		select {
		case page, ok := <-stream:
			if ok {
				return page, false, nil
			}
		default:
		}
		return resp, ctx.Err() != nil, nil

	case <-ctx.Done():
		return settleExpired(stream, errChan)
	}
}

// settleExpired decides the outcome once the request context has expired.
// A page or a service error that is already available still wins, because
// a select with several ready cases picks at random and must not turn a
// finished call into a timeout. Otherwise the outcome is a timeout.
func settleExpired[T any](stream <-chan T, errChan <-chan error) (resp T, timedOut bool, err error) {
	select {
	case page, ok := <-stream:
		if ok {
			return page, false, nil
		}
	default:
	}
	select {
	case callErr := <-errChan:
		if callErr != nil {
			return resp, false, callErr
		}
	default:
	}
	return resp, true, nil
}

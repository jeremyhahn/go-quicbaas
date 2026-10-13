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
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// errMsgServerFailure is the status message for any error that describes the
// server rather than the caller's request. It is the sentence REST answers
// with for the same classes (rest.ErrMsgServerFailure): the status code
// already carries everything a client can branch on, and the cause goes to
// the server log, where an operator can act on it.
const errMsgServerFailure = "the request could not be completed"

// requestErrorCodes are the error classes that describe the caller's request,
// so their text is safe and useful to echo. It is the set REST echoes; every
// other class, including any added later, gets errMsgServerFailure.
var requestErrorCodes = map[errors.ErrorCode]bool{
	errors.ErrNotFound:          true,
	errors.ErrInvalidInput:      true,
	errors.ErrValidation:        true,
	errors.ErrUnauthorized:      true,
	errors.ErrForbidden:         true,
	errors.ErrCrossTenantAccess: true,
	errors.ErrConflict:          true,
	errors.ErrAlreadyExists:     true,
}

// startServiceStream runs a streaming service call in its own goroutine and
// returns the channel it streams results on and a channel that yields the
// call's error exactly once.
//
// Every streaming service method closes its result channel before it returns,
// so a caller that drains the results and then reads the error sees one
// outcome per call: the error can neither be missed nor race the last result.
func startServiceStream[T any](call func(chan<- T) error) (<-chan T, <-chan error) {
	results := make(chan T, 1)
	errChan := make(chan error, 1)
	go func() {
		errChan <- call(results)
	}()
	return results, errChan
}

// streamServiceFailure records a failed streaming service call and returns the
// gRPC status the caller receives in its place.
func (s *Server) streamServiceFailure(method string, err error) error {
	s.metrics.requestsTotal.With(method, "error").Inc()
	s.logger.WithError(err).Error(method + " operation failed")
	return toSanitizedServiceError(err)
}

// toSanitizedServiceError maps a service error to a gRPC status under the
// REST disclosure rule: request-class errors keep their text, server-class
// errors keep their code but carry errMsgServerFailure.
func toSanitizedServiceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}

	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) {
		return status.Error(codes.Internal, errMsgServerFailure)
	}
	if requestErrorCodes[qbErr.Code] {
		return toServiceError(qbErr)
	}
	return status.Error(mapServiceErrorCode(qbErr.Code), errMsgServerFailure)
}

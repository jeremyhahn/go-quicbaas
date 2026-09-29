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

package grpc

import (
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// referenceStatus maps the service layer's reference errors: a reference
// naming an object that does not exist is InvalidArgument, and one whose
// store could not answer is Unavailable. The message is the reference
// error's own, naming the field, the kind and the value. ok is false for any
// other error.
func referenceStatus(err error) (mapped error, ok bool) {
	var notFound *quicbaaserrors.ReferenceNotFoundError
	if stderrors.As(err, &notFound) {
		return status.Error(codes.InvalidArgument, notFound.Error()), true
	}
	var unavailable *quicbaaserrors.ReferenceUnavailableError
	if stderrors.As(err, &unavailable) {
		return status.Error(codes.Unavailable, unavailable.Error()), true
	}
	return nil, false
}

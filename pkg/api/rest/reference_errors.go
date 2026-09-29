//go:build rest

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
	stderrors "errors"
	"net/http"
	"time"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Wire codes for the service layer's reference errors.
const (
	referenceNotFoundCode    = "reference_not_found"
	referenceUnavailableCode = "reference_unavailable"
)

// respondReferenceError answers a save the service layer refused because a
// reference field names an object that does not exist (400) or that could not
// be verified because its store could not answer (503). It reports whether
// err was a reference error and has been answered.
//
// The message is the reference error's own, naming the field, the kind and
// the value. The generic envelope would surface a wrapped store error's
// message instead and lose which field the operator has to fix.
func respondReferenceError(w http.ResponseWriter, r *http.Request, err error) bool {
	var notFound *quicbaaserrors.ReferenceNotFoundError
	if stderrors.As(err, &notFound) {
		respondReference(w, r, http.StatusBadRequest, referenceNotFoundCode, notFound.Error(), false)
		return true
	}
	var unavailable *quicbaaserrors.ReferenceUnavailableError
	if stderrors.As(err, &unavailable) {
		respondReference(w, r, http.StatusServiceUnavailable, referenceUnavailableCode, unavailable.Error(), true)
		return true
	}
	return false
}

// respondReference writes one reference-error envelope.
func respondReference(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	RespondJSON(w, r, status, ErrorResponse{
		Error:     message,
		Code:      code,
		Retryable: retryable,
		Timestamp: time.Now(),
		RequestID: GetRequestID(r.Context()),
	})
}

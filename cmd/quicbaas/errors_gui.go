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
	stderrors "errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// GUIUnavailableError indicates the GUI is unavailable because the binary
// was built without the desktop build tag or no display server was detected.
type GUIUnavailableError struct{}

// Error implements the error interface.
func (e *GUIUnavailableError) Error() string {
	return "GUI unavailable: binary built without desktop tag or no display detected"
}

// NewGUIUnavailableError creates a QBError for a GUI unavailable condition.
func NewGUIUnavailableError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   op,
		Err:  &GUIUnavailableError{},
	}
}

// GUINotConnectedError indicates no SDK client is connected to a QuicBaaS server.
type GUINotConnectedError struct{}

// Error implements the error interface.
func (e *GUINotConnectedError) Error() string {
	return "GUI not connected: no SDK client connected to server"
}

// NewGUINotConnectedError creates a QBError for a missing SDK client connection.
func NewGUINotConnectedError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   op,
		Err:  &GUINotConnectedError{},
	}
}

// GUITransportError indicates a transport or connection failure in the GUI layer.
type GUITransportError struct {
	Cause error
}

// Error implements the error interface.
func (e *GUITransportError) Error() string {
	if e.Cause != nil {
		return "GUI transport error: " + e.Cause.Error()
	}
	return "GUI transport error"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *GUITransportError) Unwrap() error {
	return e.Cause
}

// NewGUITransportError creates a QBError for a failed binding call. The code
// is classified from the cause: see classifyGUICause.
func NewGUITransportError(op string, cause error) *errors.QBError {
	return &errors.QBError{
		Code: classifyGUICause(cause),
		Op:   op,
		Err:  &GUITransportError{Cause: cause},
	}
}

// sdkAuthCodes maps the SDK transport codes that mean the server refused the
// caller to the QuicBaaS code the desktop frontend reports.
var sdkAuthCodes = map[sdkerrors.ErrorCode]errors.ErrorCode{
	sdkerrors.CodeUnauthorized: errors.ErrUnauthorized,
	sdkerrors.CodeForbidden:    errors.ErrForbidden,
}

// statusAuthCodes maps the HTTP status a typed QuicBaaS error carries to the
// code the desktop frontend reports when the server refused the caller.
var statusAuthCodes = map[int]errors.ErrorCode{
	http.StatusUnauthorized: errors.ErrUnauthorized,
	http.StatusForbidden:    errors.ErrForbidden,
}

// classifyGUICause decides the code of a failed binding call. A cause the
// server refused for authentication or authorization keeps that meaning, so
// the frontend can tell "not permitted" from "server unreachable"; every
// other cause is reported as unavailable.
func classifyGUICause(cause error) errors.ErrorCode {
	var sdkErr *sdkerrors.QBError
	if stderrors.As(cause, &sdkErr) {
		if code, ok := sdkAuthCodes[sdkErr.Code]; ok {
			return code
		}
	}
	var qbErr *errors.QBError
	if errors.As(cause, &qbErr) {
		if code, ok := statusAuthCodes[qbErr.Code.HTTPStatus()]; ok {
			return code
		}
	}
	return errors.ErrUnavailable
}

// GUIConnectError indicates a failure to connect to a QuicBaaS server from the GUI.
type GUIConnectError struct {
	Address string
	Cause   error
}

// Error implements the error interface.
func (e *GUIConnectError) Error() string {
	msg := "GUI connect failed: " + e.Address
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *GUIConnectError) Unwrap() error {
	return e.Cause
}

// NewGUIConnectError creates a QBError for a GUI connection failure.
func NewGUIConnectError(op, address string, cause error) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   op,
		Err:  &GUIConnectError{Address: address, Cause: cause},
	}
}

// GUIErrorPayload is the value a failed binding call rejects with in the
// desktop frontend. It carries the classified code and its HTTP status, so the
// frontend handles a Wails rejection the way it handles a REST error.
type GUIErrorPayload struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// formatGUIError is the Wails ErrorFormatter. Without it Wails sends only
// err.Error() to the frontend and the error's classification is lost.
func formatGUIError(err error) any {
	var qbErr *errors.QBError
	if errors.As(err, &qbErr) {
		return GUIErrorPayload{
			Code:    qbErr.Code.String(),
			Status:  qbErr.Code.HTTPStatus(),
			Message: err.Error(),
		}
	}
	return err.Error()
}

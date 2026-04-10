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
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// GUIUnavailableError indicates the GUI is unavailable because the binary
// was built without the desktop build tag or no display server was detected.
type GUIUnavailableError struct{}

// Error implements the error interface.
func (e *GUIUnavailableError) Error() string {
	return "GUI unavailable: binary built without desktop tag or no display detected"
}

// NewGUIUnavailableError creates a DragonError for a GUI unavailable condition.
func NewGUIUnavailableError(op string) *errors.DragonError {
	return &errors.DragonError{
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

// NewGUINotConnectedError creates a DragonError for a missing SDK client connection.
func NewGUINotConnectedError(op string) *errors.DragonError {
	return &errors.DragonError{
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

// NewGUITransportError creates a DragonError for a GUI transport failure.
func NewGUITransportError(op string, cause error) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrUnavailable,
		Op:   op,
		Err:  &GUITransportError{Cause: cause},
	}
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

// NewGUIConnectError creates a DragonError for a GUI connection failure.
func NewGUIConnectError(op, address string, cause error) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrUnavailable,
		Op:   op,
		Err:  &GUIConnectError{Address: address, Cause: cause},
	}
}

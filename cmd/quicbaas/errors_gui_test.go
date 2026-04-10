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
	"errors"
	"testing"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestGUIUnavailableError_Error(t *testing.T) {
	err := &GUIUnavailableError{}
	expected := "GUI unavailable: binary built without desktop tag or no display detected"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUIUnavailableError_ErrorsAs(t *testing.T) {
	dragonErr := NewGUIUnavailableError("test.op")
	var guiErr *GUIUnavailableError
	if !errors.As(dragonErr, &guiErr) {
		t.Error("expected errors.As to match GUIUnavailableError")
	}
	if dragonErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", dragonErr.Code)
	}
	if dragonErr.Op != "test.op" {
		t.Errorf("expected op test.op, got %s", dragonErr.Op)
	}
}

func TestGUINotConnectedError_Error(t *testing.T) {
	err := &GUINotConnectedError{}
	expected := "GUI not connected: no SDK client connected to server"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUINotConnectedError_ErrorsAs(t *testing.T) {
	dragonErr := NewGUINotConnectedError("test.op")
	var guiErr *GUINotConnectedError
	if !errors.As(dragonErr, &guiErr) {
		t.Error("expected errors.As to match GUINotConnectedError")
	}
	if dragonErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", dragonErr.Code)
	}
}

func TestGUITransportError_Error(t *testing.T) {
	cause := errors.New("connection refused")
	err := &GUITransportError{Cause: cause}
	expected := "GUI transport error: connection refused"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUITransportError_ErrorNilCause(t *testing.T) {
	err := &GUITransportError{}
	expected := "GUI transport error"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUITransportError_Unwrap(t *testing.T) {
	cause := errors.New("timeout")
	err := &GUITransportError{Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected Unwrap to return the cause")
	}
}

func TestGUITransportError_UnwrapNil(t *testing.T) {
	err := &GUITransportError{}
	if err.Unwrap() != nil {
		t.Error("expected nil from Unwrap when cause is nil")
	}
}

func TestGUITransportError_DragonError(t *testing.T) {
	cause := errors.New("broken pipe")
	dragonErr := NewGUITransportError("test.op", cause)
	if dragonErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", dragonErr.Code)
	}
	var transportErr *GUITransportError
	if !errors.As(dragonErr, &transportErr) {
		t.Error("expected errors.As to match GUITransportError")
	}
	if transportErr.Cause != cause {
		t.Error("expected cause to match")
	}
}

func TestGUIConnectError_Error(t *testing.T) {
	cause := errors.New("dial failed")
	err := &GUIConnectError{Address: "localhost:9090", Cause: cause}
	expected := "GUI connect failed: localhost:9090: dial failed"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUIConnectError_ErrorNilCause(t *testing.T) {
	err := &GUIConnectError{Address: "localhost:9090"}
	expected := "GUI connect failed: localhost:9090"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUIConnectError_Unwrap(t *testing.T) {
	cause := errors.New("refused")
	err := &GUIConnectError{Address: "localhost:9090", Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected Unwrap to return the cause")
	}
}

func TestGUIConnectError_UnwrapNil(t *testing.T) {
	err := &GUIConnectError{Address: "localhost:9090"}
	if err.Unwrap() != nil {
		t.Error("expected nil from Unwrap when cause is nil")
	}
}

func TestGUIConnectError_DragonError(t *testing.T) {
	cause := errors.New("connection refused")
	dragonErr := NewGUIConnectError("test.op", "localhost:9090", cause)
	if dragonErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", dragonErr.Code)
	}
	if dragonErr.Op != "test.op" {
		t.Errorf("expected op test.op, got %s", dragonErr.Op)
	}
	var connectErr *GUIConnectError
	if !errors.As(dragonErr, &connectErr) {
		t.Error("expected errors.As to match GUIConnectError")
	}
	if connectErr.Address != "localhost:9090" {
		t.Errorf("expected address localhost:9090, got %s", connectErr.Address)
	}
}

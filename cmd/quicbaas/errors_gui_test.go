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
	"net"
	"net/http"
	"testing"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestGUIUnavailableError_Error(t *testing.T) {
	err := &GUIUnavailableError{}
	expected := "GUI unavailable: binary built without desktop tag or no display detected"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGUIUnavailableError_ErrorsAs(t *testing.T) {
	qbErr := NewGUIUnavailableError("test.op")
	var guiErr *GUIUnavailableError
	if !errors.As(qbErr, &guiErr) {
		t.Error("expected errors.As to match GUIUnavailableError")
	}
	if qbErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", qbErr.Code)
	}
	if qbErr.Op != "test.op" {
		t.Errorf("expected op test.op, got %s", qbErr.Op)
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
	qbErr := NewGUINotConnectedError("test.op")
	var guiErr *GUINotConnectedError
	if !errors.As(qbErr, &guiErr) {
		t.Error("expected errors.As to match GUINotConnectedError")
	}
	if qbErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", qbErr.Code)
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

func TestGUITransportError_QBError(t *testing.T) {
	cause := errors.New("broken pipe")
	qbErr := NewGUITransportError("test.op", cause)
	if qbErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", qbErr.Code)
	}
	var transportErr *GUITransportError
	if !errors.As(qbErr, &transportErr) {
		t.Error("expected errors.As to match GUITransportError")
	}
	if transportErr.Cause != cause {
		t.Error("expected cause to match")
	}
}

func TestNewGUITransportError_ClassifiesCause(t *testing.T) {
	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	tests := []struct {
		name  string
		cause error
		want  pkgerrors.ErrorCode
	}{
		{"sdk 403", sdkerrors.NewForbidden("rest.parseErrorResponse", "forbidden"), pkgerrors.ErrForbidden},
		{"sdk 401", sdkerrors.NewUnauthorized("rest.parseErrorResponse", "unauthorized"), pkgerrors.ErrUnauthorized},
		{"wrapped sdk 403", &GUITransportError{Cause: sdkerrors.NewForbidden("grpc", "denied")}, pkgerrors.ErrForbidden},
		{"typed forbidden", &pkgerrors.QBError{Code: pkgerrors.ErrForbidden, Op: "iam"}, pkgerrors.ErrForbidden},
		{"typed unauthorized", &pkgerrors.QBError{Code: pkgerrors.ErrUnauthorized, Op: "iam"}, pkgerrors.ErrUnauthorized},
		{"raw dial failure", dialErr, pkgerrors.ErrUnavailable},
		{"sdk dial failure", sdkerrors.Wrap(dialErr, sdkerrors.CodeUnavailable, "rest", "network error"), pkgerrors.ErrUnavailable},
		{"sdk not found", sdkerrors.NewNotFound("rest", "missing"), pkgerrors.ErrUnavailable},
		{"nil cause", nil, pkgerrors.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewGUITransportError("test.op", tt.cause)
			if got.Code != tt.want {
				t.Errorf("code = %v, want %v", got.Code, tt.want)
			}
		})
	}
}

func TestFormatGUIError_QBError(t *testing.T) {
	err := NewGUITransportError("OrchestratorBinding.ListJobs", sdkerrors.NewForbidden("rest", "forbidden"))
	payload, ok := formatGUIError(err).(GUIErrorPayload)
	if !ok {
		t.Fatalf("expected GUIErrorPayload, got %T", formatGUIError(err))
	}
	if payload.Status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", payload.Status, http.StatusForbidden)
	}
	if payload.Code != pkgerrors.ErrForbidden.String() {
		t.Errorf("code = %q, want %q", payload.Code, pkgerrors.ErrForbidden.String())
	}
	if payload.Message != err.Error() {
		t.Errorf("message = %q, want %q", payload.Message, err.Error())
	}
}

func TestFormatGUIError_UntypedError(t *testing.T) {
	got := formatGUIError(errors.New("plain failure"))
	if got != "plain failure" {
		t.Errorf("expected the plain message, got %#v", got)
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

func TestGUIConnectError_QBError(t *testing.T) {
	cause := errors.New("connection refused")
	qbErr := NewGUIConnectError("test.op", "localhost:9090", cause)
	if qbErr.Code != pkgerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable code, got %v", qbErr.Code)
	}
	if qbErr.Op != "test.op" {
		t.Errorf("expected op test.op, got %s", qbErr.Op)
	}
	var connectErr *GUIConnectError
	if !errors.As(qbErr, &connectErr) {
		t.Error("expected errors.As to match GUIConnectError")
	}
	if connectErr.Address != "localhost:9090" {
		t.Errorf("expected address localhost:9090, got %s", connectErr.Address)
	}
}

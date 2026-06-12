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

package agent

import (
	"errors"
	"io"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// mapRuntimeError — every known domain code + default fallthrough
// =============================================================================

func TestMapRuntimeError_NilReturnsNil(t *testing.T) {
	t.Parallel()
	if got := mapRuntimeError(nil); got != nil {
		t.Errorf("mapRuntimeError(nil): got %v, want nil", got)
	}
}

func TestMapRuntimeError_SandboxViolationMapsToFailedPrecondition(t *testing.T) {
	t.Parallel()
	err := &orchestration.SandboxModeViolationError{
		Requested: orchestration.SandboxModeRequired,
	}
	got := mapRuntimeError(err)
	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("expected gRPC status, got %T: %v", got, got)
	}
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", st.Code())
	}
}

func TestMapRuntimeError_InputValidationMapsToInvalidArgument(t *testing.T) {
	t.Parallel()
	err := &orchestration.InputValidationError{Field: "prompt", Message: "cannot be empty"}
	got := mapRuntimeError(err)
	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("expected gRPC status, got %T", got)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestMapRuntimeError_UnknownBackendMapsToNotFound(t *testing.T) {
	t.Parallel()
	err := errors.New(ErrRuntimeUnknownBackend.Error() + ": bogus")
	// Wrap it so errors.Is works.
	wrapped := errors.Join(ErrRuntimeUnknownBackend, err)
	got := mapRuntimeError(wrapped)
	st, _ := status.FromError(got)
	if st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", st.Code())
	}
}

func TestMapRuntimeError_SessionNotFoundMapsToNotFound(t *testing.T) {
	t.Parallel()
	// A prompt/status/close against a session the backend doesn't hold is a
	// not-found condition, not an internal error. This guards the regression
	// where it previously fell through to codes.Internal and surfaced as a
	// generic 500 after a just-successful spawn.
	err := &orchestration.SessionNotFoundError{SessionID: "ses-abc"}
	got := mapRuntimeError(err)
	st, ok := status.FromError(got)
	if !ok {
		t.Fatalf("expected gRPC status, got %T", got)
	}
	if st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", st.Code())
	}
}

func TestMapRuntimeError_NoImageMapsToFailedPrecondition(t *testing.T) {
	t.Parallel()
	wrapped := errors.Join(ErrRuntimeNoImage, errors.New("backend acp has no image mapping"))
	got := mapRuntimeError(wrapped)
	st, _ := status.FromError(got)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", st.Code())
	}
}

func TestMapRuntimeError_UnknownErrorMapsToInternal(t *testing.T) {
	t.Parallel()
	err := errors.New("some unexpected failure")
	got := mapRuntimeError(err)
	st, _ := status.FromError(got)
	if st.Code() != codes.Internal {
		t.Fatalf("expected Internal, got %v", st.Code())
	}
}

// =============================================================================
// protoMCPServersToRuntime — field translation
// =============================================================================

func TestProtoMCPServersToRuntime_NilInputReturnsNil(t *testing.T) {
	t.Parallel()
	if got := protoMCPServersToRuntime(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
}

func TestProtoMCPServersToRuntime_EmptySliceReturnsNil(t *testing.T) {
	t.Parallel()
	if got := protoMCPServersToRuntime([]*proto.MCPServerConfig{}); got != nil {
		t.Errorf("empty slice: got %v, want nil", got)
	}
}

func TestProtoMCPServersToRuntime_TranslatesAllFields(t *testing.T) {
	t.Parallel()
	in := []*proto.MCPServerConfig{
		{Name: "server-alpha", Url: "http://alpha:8080/mcp"},
		{Name: "server-beta", Url: "http://beta:9090/mcp"},
	}
	out := protoMCPServersToRuntime(in)
	if len(out) != 2 {
		t.Fatalf("len: got %d, want 2", len(out))
	}
	if out[0].Name != "server-alpha" {
		t.Errorf("out[0].Name: got %q, want %q", out[0].Name, "server-alpha")
	}
	if out[0].Command != "http://alpha:8080/mcp" {
		t.Errorf("out[0].Command: got %q, want %q", out[0].Command, "http://alpha:8080/mcp")
	}
	if out[1].Name != "server-beta" {
		t.Errorf("out[1].Name: got %q, want %q", out[1].Name, "server-beta")
	}
}

// =============================================================================
// isGracefulServerClosed — recognizes nil and ErrServerStopped
// =============================================================================

func TestIsGracefulServerClosed_NilIsGraceful(t *testing.T) {
	t.Parallel()
	if !isGracefulServerClosed(nil) {
		t.Error("nil error should be treated as graceful")
	}
}

func TestIsGracefulServerClosed_ErrServerStoppedIsGraceful(t *testing.T) {
	t.Parallel()
	if !isGracefulServerClosed(grpc.ErrServerStopped) {
		t.Error("grpc.ErrServerStopped should be treated as graceful")
	}
}

func TestIsGracefulServerClosed_UnrelatedErrorIsNotGraceful(t *testing.T) {
	t.Parallel()
	err := errors.New("use of closed network connection")
	if isGracefulServerClosed(err) {
		t.Error("unrelated error should NOT be treated as graceful")
	}
}

func TestIsGracefulServerClosed_ArbitraryErrorIsNotGraceful(t *testing.T) {
	t.Parallel()
	if isGracefulServerClosed(errors.New("something completely different")) {
		t.Error("random error should NOT be graceful")
	}
}

// =============================================================================
// runtimeSessionStateToProto — all states + unknown fallthrough
// =============================================================================

func TestRuntimeSessionStateToProto_AllKnownStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   orchestration.SessionState
		want proto.SessionState
	}{
		{orchestration.SessionStateActive, proto.SessionState_SESSION_STATE_ACTIVE},
		{orchestration.SessionStateIdle, proto.SessionState_SESSION_STATE_IDLE},
		{orchestration.SessionStateClosed, proto.SessionState_SESSION_STATE_CLOSED},
		{orchestration.SessionStateError, proto.SessionState_SESSION_STATE_ERROR},
	}
	for _, tc := range cases {
		got := runtimeSessionStateToProto(tc.in)
		if got != tc.want {
			t.Errorf("runtimeSessionStateToProto(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRuntimeSessionStateToProto_UnknownFallsToUnspecified(t *testing.T) {
	t.Parallel()
	got := runtimeSessionStateToProto(orchestration.SessionState("nonexistent"))
	if got != proto.SessionState_SESSION_STATE_UNSPECIFIED {
		t.Errorf("unknown state: got %v, want SESSION_STATE_UNSPECIFIED", got)
	}
}

// =============================================================================
// grpcCodeString — nil, EOF, status errors, and unknown
// =============================================================================

func TestGRPCCodeString_NilIsOK(t *testing.T) {
	t.Parallel()
	if got := grpcCodeString(nil); got != codes.OK.String() {
		t.Errorf("nil: got %q, want %q", got, codes.OK.String())
	}
}

func TestGRPCCodeString_EOFIsOK(t *testing.T) {
	t.Parallel()
	if got := grpcCodeString(io.EOF); got != codes.OK.String() {
		t.Errorf("io.EOF: got %q, want %q", got, codes.OK.String())
	}
}

func TestGRPCCodeString_GRPCStatusReturnsCodeString(t *testing.T) {
	t.Parallel()
	err := status.Error(codes.PermissionDenied, "denied")
	if got := grpcCodeString(err); got != codes.PermissionDenied.String() {
		t.Errorf("PermissionDenied: got %q, want %q", got, codes.PermissionDenied.String())
	}
}

func TestGRPCCodeString_NonGRPCErrorIsUnknown(t *testing.T) {
	t.Parallel()
	err := errors.New("some random error")
	if got := grpcCodeString(err); got != codes.Unknown.String() {
		t.Errorf("random error: got %q, want %q", got, codes.Unknown.String())
	}
}

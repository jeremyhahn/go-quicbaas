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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listModelsFailingWith runs ListModels against a daemon that answers err.
func listModelsFailingWith(t *testing.T, err error) (*RemoteACPBackend, error) {
	t.Helper()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{listModelsErr: err})
	reg := newDaemonRegistration()
	reg.Backend = "ollama"
	backend, newErr := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Backend:      "ollama",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if newErr != nil {
		t.Fatalf("NewRemoteACPBackend: %v", newErr)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, listErr := backend.ListModels(ctx)
	return backend, listErr
}

// TestRemoteACPBackend_ListModels_NamesTheDaemonItCouldNotList
// (go-quicbaas#795): a failure on the route to the daemon is classified and
// names the daemon's address, so a gateway answers 503 or 502 instead of 500.
func TestRemoteACPBackend_ListModels_NamesTheDaemonItCouldNotList(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code codes.Code
		want DaemonRoutingFault
	}{
		{codes.Unavailable, DaemonUnreachable},
		{codes.DeadlineExceeded, DaemonUnreachable},
		{codes.Internal, DaemonFailed},
		{codes.Unimplemented, DaemonFailed},
	} {
		backend, err := listModelsFailingWith(t, status.Error(tc.code, "no answer"))

		routing, fault, ok := ClassifyDaemonRoutingError(err)
		if !ok || fault != tc.want {
			t.Fatalf("%s: classified (%v, %v), want %v", tc.code, fault, ok, tc.want)
		}
		if routing.Address != backend.registration.Host || routing.Backend != "ollama" {
			t.Fatalf("%s: routing error names %q/%q", tc.code, routing.Address, routing.Backend)
		}
		if !strings.Contains(err.Error(), backend.registration.Host) {
			t.Fatalf("%s: message %q does not name the daemon address", tc.code, err.Error())
		}
		var rpcErr *RemoteBackendRPCError
		if !errors.As(err, &rpcErr) {
			t.Fatalf("%s: the RPC error is no longer reachable through %v", tc.code, err)
		}
	}
}

// TestRemoteACPBackend_ListModels_TheDaemonsAnswerIsNotARoutingFault: a
// refusal the daemon reasons about (PermissionDenied, NotFound) is its answer
// about the request, not a routing failure, and keeps its own mapping.
func TestRemoteACPBackend_ListModels_TheDaemonsAnswerIsNotARoutingFault(t *testing.T) {
	t.Parallel()
	for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.InvalidArgument} {
		_, err := listModelsFailingWith(t, status.Error(code, "refused"))

		if _, _, ok := ClassifyDaemonRoutingError(err); ok {
			t.Fatalf("%s classified as a routing fault", code)
		}
		if st, _ := status.FromError(err); st.Code() != code {
			t.Fatalf("%s: wrapped status code is %s", code, st.Code())
		}
	}
}

// TestClassifyDaemonRoutingError_DialFailureIsUnreachable: a daemon that
// cannot be dialed is unreachable, and an error that is not a routing error
// is not classified at all.
func TestClassifyDaemonRoutingError_DialFailureIsUnreachable(t *testing.T) {
	t.Parallel()
	dialErr := &DaemonRoutingError{
		Backend: "ollama",
		Address: "agent-09.example.test:9091",
		Op:      "ListModels",
		Err:     &RemoteBackendDialError{Host: "agent-09.example.test:9091", Err: errors.New("connection refused")},
	}

	_, fault, ok := ClassifyDaemonRoutingError(dialErr)

	if !ok || fault != DaemonUnreachable {
		t.Fatalf("dial failure classified (%v, %v), want unreachable", fault, ok)
	}
	if !strings.Contains(dialErr.Error(), "agent-09.example.test:9091 is unreachable") {
		t.Fatalf("message %q", dialErr.Error())
	}
	if _, _, ok := ClassifyDaemonRoutingError(errors.New("boom")); ok {
		t.Fatal("a plain error was classified as a routing fault")
	}
}

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// iamInitError is the failure the seam injects. A distinct type, so the
// assertion proves the ORIGINAL cause reached the caller rather than some other
// error raised along the way.
type iamInitError struct{}

func (iamInitError) Error() string { return "iam: simulated component failure" }

// TestStartService_IAMInitFailureAbortsStartup is the regression for #170.
//
// The failure used to be downgraded to a warning and startup continued. That is
// not a degraded mode: every IAM accessor on the service facade returns
// (nil, nil) when no provider was set, so a caller checking `err != nil` sees
// success and a nil store, and the server serves with authentication and
// tenancy absent while nothing on the request path can tell.
func TestStartService_IAMInitFailureAbortsStartup(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) {
		return nil, iamInitError{}
	}
	seamMu.Unlock()

	err := srv.attachIAMComponents(&iamAttachRecorder{})
	if err == nil {
		t.Fatal("an IAM initialization failure must abort startup, not warn and continue")
	}
	if !errors.Is(err, error(iamInitError{})) {
		// errors.As on a non-pointer struct type needs a target of that type;
		// fall back to a string check so the assertion cannot pass vacuously on
		// an unrelated error.
		if got := err.Error(); got == "" || !containsString(got, "iam: simulated component failure") {
			t.Fatalf("the original failure must reach the caller, got %v", err)
		}
	}

	// And the server must not be carrying a half-attached IAM.
	if srv.iamComponents != nil {
		t.Fatal("a failed IAM init left components attached to the server")
	}
}

// TestStartService_NoIAMComponentsIsNotAFailure pins the case the fail-closed
// change must NOT break: a deployment with no qrdb client has no IAM by design,
// and initializeIAMComponents signals that as (nil, nil) rather than an error.
//
// This is the fact the whole decision rests on. If IAM absence travelled
// through the error path, refusing to start would refuse a supported
// deployment.
func TestStartService_NoIAMComponentsIsNotAFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	stubIAMSeam() // returns (nil, nil)

	recorder := &iamAttachRecorder{}
	if err := srv.attachIAMComponents(recorder); err != nil {
		t.Fatalf("a deployment without IAM must still start: %v", err)
	}
	if srv.iamComponents != nil {
		t.Fatal("no IAM components were built, so none must be attached")
	}
	if recorder.provider != nil {
		t.Fatal("no provider was built, so none must have been handed to the service")
	}
}

// TestAttachIAMComponents_AttachesWhenTheyBuild pins the success path, so the
// fail-closed change cannot be satisfied by refusing everything.
func TestAttachIAMComponents_AttachesWhenTheyBuild(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)

	built := &stubIAMComponents{}
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return built, nil }
	seamMu.Unlock()

	recorder := &iamAttachRecorder{}
	if err := srv.attachIAMComponents(recorder); err != nil {
		t.Fatalf("attachIAMComponents: %v", err)
	}
	if srv.iamComponents != service.IAMComponents(built) {
		t.Fatal("the built components were not retained on the server")
	}
	if recorder.provider != service.IAMComponents(built) {
		t.Fatal("the built components were not handed to the service facade")
	}
}

// containsString avoids pulling in a dependency for one substring check.
func containsString(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// iamAttachRecorder is the narrowest stand-in for the service facade that
// attachIAMComponents touches: it records the provider it is handed, so a test
// can tell "attached" from "not attached" without building a real service.
type iamAttachRecorder struct {
	service.QuicBaaS
	provider service.IAMComponents
}

func (r *iamAttachRecorder) SetIAMProvider(provider service.IAMComponents) {
	r.provider = provider
}

// stubIAMComponents is a component set that builds successfully. Every accessor
// is the zero value; the tests here assert on IDENTITY and attachment, not on
// what the components do.
type stubIAMComponents struct {
	service.IAMComponents
}

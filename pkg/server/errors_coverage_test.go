// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"errors"
	"strings"
	"testing"
)

func TestNilLoginIdentityError(t *testing.T) {
	e := &NilLoginIdentityError{}
	if !strings.Contains(e.Error(), "nil login identity") {
		t.Fatalf("unexpected message: %q", e.Error())
	}
}

func TestLoginCeremonyUnavailableError(t *testing.T) {
	e := &LoginCeremonyUnavailableError{}
	if !strings.Contains(e.Error(), "login ceremony unavailable") {
		t.Fatalf("unexpected message: %q", e.Error())
	}
}

func TestRaftMTLSLoadError(t *testing.T) {
	withCause := &RaftMTLSLoadError{File: "/tmp/ca.pem", Cause: errors.New("open denied")}
	if !strings.Contains(withCause.Error(), "/tmp/ca.pem") || !strings.Contains(withCause.Error(), "open denied") {
		t.Fatalf("unexpected message: %q", withCause.Error())
	}
	if !errors.Is(withCause, withCause.Cause) {
		t.Fatalf("Unwrap should return cause")
	}

	noCause := &RaftMTLSLoadError{File: "/tmp/cert.pem"}
	if strings.Contains(noCause.Error(), ": ") && strings.HasSuffix(noCause.Error(), ": ") {
		t.Fatalf("nil-cause message malformed: %q", noCause.Error())
	}
	if !strings.Contains(noCause.Error(), "/tmp/cert.pem") {
		t.Fatalf("file missing from message: %q", noCause.Error())
	}
	if noCause.Unwrap() != nil {
		t.Fatalf("Unwrap should be nil")
	}
}

func TestClusterMembersError(t *testing.T) {
	cause := errors.New("bad peer")
	withCause := &ClusterMembersError{Cause: cause}
	if !strings.Contains(withCause.Error(), "bad peer") {
		t.Fatalf("unexpected message: %q", withCause.Error())
	}
	if !errors.Is(withCause, cause) {
		t.Fatalf("Unwrap should return cause")
	}

	noCause := &ClusterMembersError{}
	if !strings.Contains(noCause.Error(), "invalid peer configuration") {
		t.Fatalf("unexpected nil-cause message: %q", noCause.Error())
	}
	if noCause.Unwrap() != nil {
		t.Fatalf("Unwrap should be nil")
	}
}

func TestPINBackendError(t *testing.T) {
	cause := errors.New("disk full")
	withCause := &PINBackendError{Stage: "open store", Cause: cause}
	if !strings.Contains(withCause.Error(), "open store") || !strings.Contains(withCause.Error(), "disk full") {
		t.Fatalf("unexpected message: %q", withCause.Error())
	}
	if !errors.Is(withCause, cause) {
		t.Fatalf("Unwrap should return cause")
	}

	noCause := &PINBackendError{Stage: "init"}
	if !strings.Contains(noCause.Error(), "init") {
		t.Fatalf("unexpected nil-cause message: %q", noCause.Error())
	}
	if noCause.Unwrap() != nil {
		t.Fatalf("Unwrap should be nil")
	}
}

func TestDataPlaneBarrierError(t *testing.T) {
	cause := errors.New("mkdir failed")
	withCause := &DataPlaneBarrierError{Stage: "mkdir seal dir", Cause: cause}
	if !strings.Contains(withCause.Error(), "mkdir seal dir") || !strings.Contains(withCause.Error(), "mkdir failed") {
		t.Fatalf("unexpected message: %q", withCause.Error())
	}
	if !errors.Is(withCause, cause) {
		t.Fatalf("Unwrap should return cause")
	}

	noCause := &DataPlaneBarrierError{Stage: "open"}
	if !strings.Contains(noCause.Error(), "open") {
		t.Fatalf("unexpected nil-cause message: %q", noCause.Error())
	}
	if noCause.Unwrap() != nil {
		t.Fatalf("Unwrap should be nil")
	}
}

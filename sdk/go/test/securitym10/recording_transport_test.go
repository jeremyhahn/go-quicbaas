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

package securitym10

import (
	"context"
	"errors"
	"testing"
)

// The tests below validate the recordingTransport fixture itself. The fixture
// is the substrate for every M10 conformance test: if its dispatch, error
// injection, or lifecycle behavior were wrong, the conformance suite would
// silently pass against a broken harness. These tests pin that contract.

// TestRecordingTransport_ErrorInjection verifies that an injected error is
// surfaced by Request, that a canned response is returned when no error is
// registered, and that an unregistered (service, method) yields the zero
// response with no error. This exercises both branches of setResponse and the
// error/response dispatch in Request.
func TestRecordingTransport_ErrorInjection(t *testing.T) {
	rt := &recordingTransport{responses: map[string]map[string]any{}}

	wantErr := errors.New("server rejected request")
	rt.setResponse("svc", "Fail", nil, wantErr)
	rt.setResponse("svc", "Ok", "payload", nil)

	if _, err := rt.Request(context.Background(), "svc", "Fail", nil); !errors.Is(err, wantErr) {
		t.Fatalf("expected injected error, got %v", err)
	}

	resp, err := rt.Request(context.Background(), "svc", "Ok", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "payload" {
		t.Errorf("expected canned response %q, got %v", "payload", resp)
	}

	// Unregistered method returns (nil, nil).
	resp, err = rt.Request(context.Background(), "svc", "Unknown", nil)
	if err != nil || resp != nil {
		t.Errorf("expected (nil, nil) for unregistered method, got (%v, %v)", resp, err)
	}

	// Calls are recorded for each invocation.
	if got := rt.callsFor("svc", "Fail"); got != 1 {
		t.Errorf("expected 1 recorded call for Fail, got %d", got)
	}
}

// TestRecordingTransport_Lifecycle verifies the fixture's transport.Transport
// lifecycle methods: Healthy flips to false after Close, Conn reports no live
// connection, and RequestStream returns the documented (nil, nil) since the
// conformance suite does not exercise streaming.
func TestRecordingTransport_Lifecycle(t *testing.T) {
	rt := &recordingTransport{responses: map[string]map[string]any{}}

	if !rt.Healthy() {
		t.Error("a fresh transport should be healthy")
	}
	if rt.Conn() != nil {
		t.Error("recording transport has no live connection")
	}

	stream, err := rt.RequestStream(context.Background(), "svc", "Stream", nil)
	if err != nil {
		t.Errorf("unexpected error from RequestStream: %v", err)
	}
	if stream != nil {
		t.Error("expected nil stream from non-streaming fixture")
	}

	if err := rt.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if rt.Healthy() {
		t.Error("transport should be unhealthy after Close")
	}
}

// TestSDK_ServerError_Propagates is an end-to-end conformance check that an
// error returned by the server transport is surfaced to the SDK caller rather
// than swallowed. It complements the happy-path Issue test by injecting a
// transport-level failure for the same wire method.
func TestSDK_ServerError_Propagates(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	wantErr := errors.New("upstream CA unavailable")
	rt.setResponse("quicbaas.ACME", "Issue", nil, wantErr)

	_, err := c.Security().ACME().Server().Issue(
		context.Background(), []byte("CSR"), "enroll-01", "device-1")
	if err == nil {
		t.Fatal("expected the injected server error to propagate to the caller")
	}

	if got := rt.callsFor("quicbaas.ACME", "Issue"); got != 1 {
		t.Errorf("expected exactly 1 wire call, got %d", got)
	}
}

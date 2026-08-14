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
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// The transport-facing half of the rotation verb: the capability narrowing
// every server transport goes through, and the JSON shape four of them put on
// the wire verbatim.
// =============================================================================

func TestRotationServiceFrom_ReturnsTheRotatingService(t *testing.T) {
	backend := &inPlacePoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h := newRotationHarness(t, backend)

	rotator, err := RotationServiceFrom(h.service, "rest")
	if err != nil {
		t.Fatalf("RotationServiceFrom: %v", err)
	}
	if rotator == nil {
		t.Fatal("RotationServiceFrom returned a nil rotator with no error")
	}
}

func TestRotationServiceFrom_RejectsAServiceWithoutTheVerb(t *testing.T) {
	tests := []struct {
		name string
		svc  AgentService
	}{
		// A real AgentService that predates the account pool: it satisfies
		// every orchestrator verb but not the rotation one.
		{name: "non-rotating service", svc: &healthMockAgentService{}},
		// A transport whose provider handed back nothing at all.
		{name: "nil service", svc: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rotator, err := RotationServiceFrom(tc.svc, "quic")
			if rotator != nil {
				t.Fatalf("RotationServiceFrom returned a rotator %T for a service that has no rotation verb", rotator)
			}
			if !errors.Is(err, ErrSessionRotationUnsupported) {
				t.Fatalf("err = %v, want ErrSessionRotationUnsupported", err)
			}
			var unsupported *SessionRotationUnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want a *SessionRotationUnsupportedError", err)
			}
			if unsupported.Transport != "quic" {
				t.Fatalf("Transport = %q, want the caller's surface so an operator knows where the gap is", unsupported.Transport)
			}
		})
	}
}

func TestSessionRotationUnsupportedError_MessageNamesTheTransport(t *testing.T) {
	withTransport := (&SessionRotationUnsupportedError{Transport: "mcp"}).Error()
	if withTransport == ErrSessionRotationUnsupported.Error() {
		t.Fatalf("Error() = %q, want the transport appended", withTransport)
	}
	if bare := (&SessionRotationUnsupportedError{}).Error(); bare != ErrSessionRotationUnsupported.Error() {
		t.Fatalf("Error() = %q, want the bare sentinel when no transport is set", bare)
	}
}

// TestRotateSessionAccountWire_RoundTripsTheOperatorRequest pins the JSON shape
// REST, QUIC and MCP put on the wire. The SDK request type mirrors these tags
// field for field, so a rename here silently breaks three transports at once.
func TestRotateSessionAccountWire_RoundTripsTheOperatorRequest(t *testing.T) {
	availableAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(&RotateSessionAccountRequest{
		SessionID:    "sess-1",
		Reason:       "seat retired",
		HoldOutgoing: true,
		AvailableAt:  availableAt,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"session_id", "reason", "hold_outgoing", "available_at"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("wire request %s is missing %q", encoded, key)
		}
	}

	var decoded RotateSessionAccountRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal into request: %v", err)
	}
	if decoded.SessionID != "sess-1" || decoded.Reason != "seat retired" || !decoded.HoldOutgoing {
		t.Fatalf("decoded = %+v, want the operator's request back", decoded)
	}
	if !decoded.AvailableAt.Equal(availableAt) {
		t.Fatalf("AvailableAt = %s, want %s", decoded.AvailableAt, availableAt)
	}
}

// TestRotateSessionAccountWire_OmitsUnsetOptionalFields keeps an empty operator
// POST from arriving as an explicit "hold this account forever".
func TestRotateSessionAccountWire_OmitsUnsetOptionalFields(t *testing.T) {
	encoded, err := json.Marshal(&RotateSessionAccountRequest{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"reason", "hold_outgoing"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("wire request %s carries %q even though it was never set", encoded, key)
		}
	}
}

func TestRotateSessionAccountWire_ResultCarriesBothAccounts(t *testing.T) {
	encoded, err := json.Marshal(&RotateSessionAccountResult{
		SessionID:         "sess-1",
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              AccountRotationModeResume,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var fields map[string]interface{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]string{
		"session_id":          "sess-1",
		"previous_account_id": "acct-a",
		"account_id":          "acct-b",
		"strategy":            "least_usage",
		"mode":                "resume",
	}
	for key, expected := range want {
		got, ok := fields[key].(string)
		if !ok || got != expected {
			t.Fatalf("wire result %s: %q = %v, want %q", encoded, key, fields[key], expected)
		}
	}
}

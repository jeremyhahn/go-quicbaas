//go:build rest

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

// handlers_orchestrator_heartbeat_observation_test.go covers the decode side
// of heartbeat reconciliation (go-quicbaas#699): the served list a daemon
// reports has to reach the registry, and "reported none" has to stay
// distinguishable from "reported nothing".

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestHeartbeatBody_ObservationDistinguishesAbsentFromEmpty is the whole
// reason the decoded field is a pointer. Both shapes decode to a nil slice and
// they mean opposite things: no backends key at all is a transport that
// carries no served list and reconciles nothing, while an empty list is a
// daemon that constructed no backend, which the registry refuses as a fault.
func TestHeartbeatBody_ObservationDistinguishesAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want *orchestration.FleetNodeObservation
	}{
		"no backends key": {
			body: `{"bind_addr":"10.0.0.5:9091"}`,
			want: nil,
		},
		"an empty served list": {
			body: `{"backends":[]}`,
			want: &orchestration.FleetNodeObservation{Backends: []string{}},
		},
		"a null served list": {
			body: `{"backends":null}`,
			want: nil,
		},
		"the served list": {
			body: `{"backends":["claude","codex","opencode"]}`,
			want: &orchestration.FleetNodeObservation{Backends: []string{"claude", "codex", "opencode"}},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var payload heartbeatBody
			require.NoError(t, json.Unmarshal([]byte(tt.body), &payload))
			assert.Equal(t, tt.want, payload.observation())
		})
	}
}

// TestHeartbeatBody_ObservationCarriesTheDeclaredCapacity: the slot count a
// daemon reports is admission's fleet axis, and it must survive the decode. It
// travels ON the served list rather than instead of it, because an observation
// reporting no backend is refused outright.
func TestHeartbeatBody_ObservationCarriesTheDeclaredCapacity(t *testing.T) {
	t.Parallel()

	var payload heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["claude"],"max_concurrent_jobs":6}`), &payload))

	observed := payload.observation()
	require.NotNil(t, observed)
	assert.Equal(t, 6, observed.MaxConcurrentJobs)
	assert.NoError(t, observed.Validate())
}

// TestHeartbeatBody_ObservationOfAnUnreportedCapacity: an absent key is zero,
// which the registry reads as "the daemon said nothing" and which leaves the
// record's existing count alone rather than erasing it.
func TestHeartbeatBody_ObservationOfAnUnreportedCapacity(t *testing.T) {
	t.Parallel()

	var payload heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["claude"]}`), &payload))

	observed := payload.observation()
	require.NotNil(t, observed)
	assert.Zero(t, observed.MaxConcurrentJobs)
}

// TestHeartbeatBody_ACapacityWithNoServedListReconcilesNothing pins the shape
// deliberately NOT supported: an observation reporting no backend is refused by
// the registry as a fault, so decoding one would turn a heartbeat into an error
// rather than into a capacity. pkg/fleet writes both fields in one payload.
func TestHeartbeatBody_ACapacityWithNoServedListReconcilesNothing(t *testing.T) {
	t.Parallel()

	var payload heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"max_concurrent_jobs":6}`), &payload))
	assert.Nil(t, payload.observation())
}

// TestHeartbeatBody_ObservationOfNilBody keeps the accessor safe for the
// handler's zero-value payload, which is what an unreadable or absent body
// leaves behind.
func TestHeartbeatBody_ObservationOfNilBody(t *testing.T) {
	t.Parallel()

	var nilBody *heartbeatBody
	assert.Nil(t, nilBody.observation())
	assert.Nil(t, (&heartbeatBody{}).observation())
}

// TestBackendsFromHeartbeat_PointerShapes pins the recovery path's reader
// against the same absent/empty distinction, since it shares the decoded body.
func TestBackendsFromHeartbeat_PointerShapes(t *testing.T) {
	t.Parallel()

	assert.Nil(t, backendsFromHeartbeat(nil))

	var absent heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{}`), &absent))
	assert.Nil(t, backendsFromHeartbeat(&absent))

	var empty heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":[]}`), &empty))
	assert.Nil(t, backendsFromHeartbeat(&empty))

	var served heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":[" claude ","","codex"]}`), &served))
	assert.Equal(t, []string{"claude", "codex"}, backendsFromHeartbeat(&served))
}

// TestHandleOrchestratorAgentHeartbeat_CarriesTheServedList drives the served
// list through the handler to the service. Without it the record's backend
// axis is written once at registration and never corrected, which is the whole
// of go-quicbaas#699.
func TestHandleOrchestratorAgentHeartbeat_CarriesTheServedList(t *testing.T) {
	srv, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	observations := make(chan *orchestration.FleetNodeObservation, 1)
	agentSvc.agentHeartbeatFunc = func(_ context.Context, _, _ string, observed *orchestration.FleetNodeObservation) error {
		observations <- observed
		return nil
	}

	w := doOrchReq(t, srv, http.MethodPost, "/api/v1/fleet/agent-1/heartbeat",
		`{"bind_addr":"10.0.0.5:9091","backends":["claude","codex","opencode"]}`)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	observed := <-observations
	require.NotNil(t, observed)
	assert.Equal(t, []string{"claude", "codex", "opencode"}, observed.Backends)
}

// TestHandleOrchestratorAgentHeartbeat_NoServedListObservesNothing keeps a
// body that reports nothing from being read as a daemon serving nothing.
func TestHandleOrchestratorAgentHeartbeat_NoServedListObservesNothing(t *testing.T) {
	srv, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	observations := make(chan *orchestration.FleetNodeObservation, 1)
	agentSvc.agentHeartbeatFunc = func(_ context.Context, _, _ string, observed *orchestration.FleetNodeObservation) error {
		observations <- observed
		return nil
	}

	w := doOrchReq(t, srv, http.MethodPost, "/api/v1/fleet/agent-1/heartbeat",
		`{"bind_addr":"10.0.0.5:9091"}`)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	assert.Nil(t, <-observations)
}

// TestHandleOrchestratorAgentHeartbeat_ServingNothingIsABadRequest surfaces
// the fault to the daemon: a served list that is present and empty is refused
// rather than applied, and the refusal is actionable rather than a 500.
func TestHandleOrchestratorAgentHeartbeat_ServingNothingIsABadRequest(t *testing.T) {
	srv, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.agentHeartbeatFunc = func(_ context.Context, _, _ string, observed *orchestration.FleetNodeObservation) error {
		return observed.Validate()
	}

	w := doOrchReq(t, srv, http.MethodPost, "/api/v1/fleet/agent-1/heartbeat",
		`{"bind_addr":"10.0.0.5:9091","backends":[]}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// TestHeartbeatBody_ObservationCarriesTheSigningAgents: the signing sockets a
// daemon can mount reach the registry, and an absent key (an older daemon)
// stays distinguishable from an empty list (a daemon that reaches none), for
// the reason the served list's pointer exists (go-quicbaas#981).
func TestHeartbeatBody_ObservationCarriesTheSigningAgents(t *testing.T) {
	t.Parallel()
	const id = "0123456789abcdef0123456789abcdef"
	tests := map[string]struct {
		body string
		want []string
	}{
		"absent":  {body: `{"backends":["claude"]}`, want: nil},
		"empty":   {body: `{"backends":["claude"],"signing_agents":[]}`, want: []string{}},
		"one":     {body: `{"backends":["claude"],"signing_agents":["` + id + `"]}`, want: []string{id}},
		"null is": {body: `{"backends":["claude"],"signing_agents":null}`, want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var payload heartbeatBody
			require.NoError(t, json.Unmarshal([]byte(tt.body), &payload))
			observed := payload.observation()
			require.NotNil(t, observed)
			assert.Equal(t, tt.want, observed.SigningAgents)
			assert.NoError(t, observed.Validate())
		})
	}
}

// TestHeartbeatBody_ObservationCarriesTheSigningRelay: a daemon that reports
// the signing relay has it reconciled; one whose body says nothing about it —
// an older build — reconciles nothing on that axis (go-quicbaas#1013).
func TestHeartbeatBody_ObservationCarriesTheSigningRelay(t *testing.T) {
	var reported heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["claude"],"signing_relay":true}`), &reported))
	observed := reported.observation()
	require.NotNil(t, observed)
	require.NotNil(t, observed.SigningRelay)
	assert.True(t, *observed.SigningRelay)

	var withdrawn heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["claude"],"signing_relay":false}`), &withdrawn))
	require.NotNil(t, withdrawn.observation().SigningRelay)
	assert.False(t, *withdrawn.observation().SigningRelay)

	var silent heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["claude"]}`), &silent))
	assert.Nil(t, silent.observation().SigningRelay)
}

// TestHeartbeatBody_ObservationCarriesDeviceTelemetry pins the device half of
// the body: a daemon's samples reach the observation with their provenance
// intact, and an unread signal stays unread rather than decoding as zero.
func TestHeartbeatBody_ObservationCarriesDeviceTelemetry(t *testing.T) {
	t.Parallel()
	var payload heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["ollama"],"devices":[`+
		`{"kind":"gpu","id":"GPU-1","power_watts":{"value":412.5,"provenance":"measured"},"utilization":{"provenance":"unknown"}}]}`), &payload))

	observed := payload.observation()
	require.NotNil(t, observed)
	require.Len(t, observed.Devices, 1)
	watts, ok := observed.Devices[0].PowerWatts.Value()
	require.True(t, ok)
	assert.Equal(t, 412.5, watts)
	assert.False(t, observed.Devices[0].Utilization.Known())

	var bare heartbeatBody
	require.NoError(t, json.Unmarshal([]byte(`{"backends":["ollama"]}`), &bare))
	assert.Nil(t, bare.observation().Devices, "a body with no devices reports nothing")
}

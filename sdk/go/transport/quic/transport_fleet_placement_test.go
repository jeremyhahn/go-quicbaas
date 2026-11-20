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

package quic

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_FleetPlacementOverHTTP3 proves the fleet placement verbs
// (go-quicbaas#1681) reach the REST routes over HTTP/3 and decode into the
// SDK types.
func TestRequest_FleetPlacementOverHTTP3(t *testing.T) {
	type hit struct{ method, path, body string }
	hits := make(chan hit, 8)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits <- hit{r.Method, r.URL.Path, string(body)}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/fleet/placements":
			_, _ = w.Write([]byte(`{"specs":[{"name":"agents","role":"agent"}],"count":1}`))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"name":"agents","withdrawn":true}`))
		case r.URL.Path == "/api/v1/fleet/placements/agents/drift":
			_, _ = w.Write([]byte(`{"spec":"agents","observed_work":1,"findings":[{"kind":"host_not_admitted","host":"h2","remedy":{"action":"move","destinations":["h1"]}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/fleet/placement-consents":
			_, _ = w.Write([]byte(`{"requests":[{"id":"c1","tenant_id":"t1","move":{"spec_name":"agents","work_id":"s1"},"state":"pending"}],"count":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/fleet/placement-consents/c1/answer":
			_, _ = w.Write([]byte(`{"id":"c1","tenant_id":"t1","state":"granted","by":"alice"}`))
		default:
			_, _ = w.Write([]byte(`{"tenant_id":"t1","name":"agents","role":"agent","hosts":["h1"],"generation":1}`))
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	ctx := context.Background()
	call := func(method string, req interface{}) interface{} {
		t.Helper()
		resp, err := tr.Request(ctx, "quicbaas.Orchestrator", method, req)
		require.NoError(t, err, method)
		return resp
	}

	spec := call("DeclareFleetPlacementSpec", &orchestratorclient.DeclareFleetPlacementSpecRequest{
		TenantID: "t1", Spec: &orchestratorclient.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	}).(*orchestratorclient.FleetPlacementSpec)
	assert.Equal(t, uint64(1), spec.Generation)
	got := <-hits
	assert.Equal(t, http.MethodPut, got.method)
	assert.Equal(t, "/api/v1/fleet/placements/agents", got.path)
	assert.Contains(t, got.body, `"hosts":["h1"]`)

	assert.Equal(t, "agent", call("GetFleetPlacementSpec", &orchestratorclient.FleetPlacementSpecRequest{TenantID: "t1", Name: "agents"}).(*orchestratorclient.FleetPlacementSpec).Role)
	assert.Equal(t, "/api/v1/fleet/placements/agents", (<-hits).path)

	assert.Equal(t, 1, call("ListFleetPlacementSpecs", &orchestratorclient.ListFleetPlacementSpecsRequest{TenantID: "t1"}).(*orchestratorclient.ListFleetPlacementSpecsResponse).Count)
	assert.Equal(t, "/api/v1/fleet/placements", (<-hits).path)

	report := call("GetFleetPlacementDrift", &orchestratorclient.FleetPlacementSpecRequest{TenantID: "t1", Name: "agents"}).(*orchestratorclient.FleetPlacementDrift)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, []string{"h1"}, report.Findings[0].Remedy.Destinations)
	assert.Equal(t, "/api/v1/fleet/placements/agents/drift", (<-hits).path)

	assert.True(t, call("WithdrawFleetPlacementSpec", &orchestratorclient.FleetPlacementSpecRequest{TenantID: "t1", Name: "agents"}).(*orchestratorclient.WithdrawFleetPlacementSpecResponse).Withdrawn)
	assert.Equal(t, http.MethodDelete, (<-hits).method)

	consents := call("ListFleetPlacementConsents", &orchestratorclient.ListFleetPlacementConsentsRequest{TenantID: "t1", State: "pending"}).(*orchestratorclient.ListFleetPlacementConsentsResponse)
	require.Len(t, consents.Requests, 1)
	assert.Equal(t, "s1", consents.Requests[0].Move.WorkID)
	got = <-hits
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/api/v1/fleet/placement-consents", got.path)

	answered := call("AnswerFleetPlacementConsent", &orchestratorclient.AnswerFleetPlacementConsentRequest{TenantID: "t1", ID: "c1", State: "granted"}).(*orchestratorclient.FleetPlacementConsentRequest)
	assert.Equal(t, "alice", answered.By)
	got = <-hits
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/api/v1/fleet/placement-consents/c1/answer", got.path)
	assert.Contains(t, got.body, `"state":"granted"`)
	assert.NotContains(t, got.body, `"by"`)
}

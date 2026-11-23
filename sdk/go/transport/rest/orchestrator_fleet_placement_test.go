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

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Each fleet placement verb reaches its route with its method, path, tenant
// and body, and decodes the server's answer.
func TestOrchestratorREST_FleetPlacementRoutes(t *testing.T) {
	type seen struct{ method, uri, body string }
	var got []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.RequestURI(), string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/fleet/placement-consents":
			_, _ = io.WriteString(w, `{"requests":[{"id":"c1","tenant_id":"acme","move":{"spec_name":"agents","spec_generation":2,"work_id":"s1"},"state":"pending","raised_at":"2026-11-14T00:00:00Z"}],"count":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/fleet/placement-consents/c 1/answer":
			_, _ = io.WriteString(w, `{"id":"c 1","tenant_id":"acme","state":"granted","by":"alice","reason":"fine"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/fleet/placements":
			_, _ = io.WriteString(w, `{"specs":[{"name":"agents","role":"agent"}],"count":1}`)
		case r.URL.Path == "/api/v1/fleet/placements/agents/drift":
			_, _ = io.WriteString(w, `{"spec":"agents","observed_work":2,"findings":[{"kind":"host_not_admitted","host":"h2","remedy":{"action":"move"}}]}`)
		case r.Method == http.MethodDelete:
			_, _ = io.WriteString(w, `{"name":"agents","withdrawn":true}`)
		case r.URL.Path == "/api/v1/fleet/placements/absent":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		default:
			_, _ = io.WriteString(w, `{"tenant_id":"acme","name":"agents","role":"agent","generation":1}`)
		}
	}))
	defer server.Close()
	client := orchestrator.NewClient(newTestTransport(t, server))
	ctx := context.Background()

	spec, err := client.DeclareFleetPlacementSpec(ctx, &orchestrator.DeclareFleetPlacementSpecRequest{
		TenantID: "acme", Spec: &orchestrator.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	})
	if err != nil || spec.Generation != 1 {
		t.Fatalf("Declare = %+v, %v", spec, err)
	}
	if _, err := client.GetFleetPlacementSpec(ctx, &orchestrator.FleetPlacementSpecRequest{Name: "agents"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	list, err := client.ListFleetPlacementSpecs(ctx, nil)
	if err != nil || list.Count != 1 || list.Specs[0].Name != "agents" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	report, err := client.GetFleetPlacementDrift(ctx, &orchestrator.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil || report.ObservedWork != 2 || len(report.Findings) != 1 || report.Findings[0].Remedy.Action != "move" {
		t.Fatalf("Drift = %+v, %v", report, err)
	}
	withdrawn, err := client.WithdrawFleetPlacementSpec(ctx, &orchestrator.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil || !withdrawn.Withdrawn {
		t.Fatalf("Withdraw = %+v, %v", withdrawn, err)
	}
	_, err = client.GetFleetPlacementSpec(ctx, &orchestrator.FleetPlacementSpecRequest{Name: "absent"})
	if !sdkerrors.IsNotFound(err) {
		t.Fatalf("Get absent = %v, want not found", err)
	}
	consents, err := client.ListFleetPlacementConsents(ctx, &orchestrator.ListFleetPlacementConsentsRequest{TenantID: "acme", State: "pending"})
	if err != nil || consents.Count != 1 || consents.Requests[0].ID != "c1" || consents.Requests[0].Move.SpecGeneration != 2 ||
		consents.Requests[0].RaisedAt.IsZero() {
		t.Fatalf("ListConsents = %+v, %v", consents, err)
	}
	if _, err := client.ListFleetPlacementConsents(ctx, nil); err != nil {
		t.Fatalf("ListConsents nil: %v", err)
	}
	answered, err := client.AnswerFleetPlacementConsent(ctx, &orchestrator.AnswerFleetPlacementConsentRequest{
		TenantID: "acme", ID: "c 1", State: "granted", Reason: "fine",
	})
	if err != nil || answered.By != "alice" || answered.State != "granted" {
		t.Fatalf("AnswerConsent = %+v, %v", answered, err)
	}

	want := []seen{
		{http.MethodPut, "/api/v1/fleet/placements/agents?tenant_id=acme", ""},
		{http.MethodGet, "/api/v1/fleet/placements/agents", ""},
		{http.MethodGet, "/api/v1/fleet/placements", ""},
		{http.MethodGet, "/api/v1/fleet/placements/agents/drift", ""},
		{http.MethodDelete, "/api/v1/fleet/placements/agents", ""},
		{http.MethodGet, "/api/v1/fleet/placements/absent", ""},
		{http.MethodGet, "/api/v1/fleet/placement-consents?state=pending&tenant_id=acme", ""},
		{http.MethodGet, "/api/v1/fleet/placement-consents", ""},
		{http.MethodPost, "/api/v1/fleet/placement-consents/c%201/answer?tenant_id=acme", ""},
	}
	if len(got) < len(want) {
		t.Fatalf("requests = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if got[i].method != w.method || got[i].uri != w.uri {
			t.Errorf("request %d = %s %s, want %s %s", i, got[i].method, got[i].uri, w.method, w.uri)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil || body["role"] != "agent" || body["name"] != "agents" {
		t.Fatalf("declare body = %s (%v), want the spec", got[0].body, err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(got[len(want)-1].body), &answer); err != nil || answer["state"] != "granted" ||
		answer["reason"] != "fine" || len(answer) != 2 {
		t.Fatalf("answer body = %s (%v), want only state and reason", got[len(want)-1].body, err)
	}
}

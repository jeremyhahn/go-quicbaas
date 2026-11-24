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
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// reachCase is one orchestrator client method: the request its client hands
// the transport, whether it is a server stream, and the route it must reach.
type reachCase struct {
	req    interface{}
	stream bool
	route  string // "VERB /path" the server must see; empty for notCarriedOverREST
}

// clientMethods is every method of orchestratorclient.Client, read from the
// interface itself so a method added there without a case fails this test.
func clientMethods() []string {
	ct := reflect.TypeOf((*orchestratorclient.Client)(nil)).Elem()
	names := make([]string, 0, ct.NumMethod())
	for i := 0; i < ct.NumMethod(); i++ {
		names = append(names, ct.Method(i).Name)
	}
	return names // reflect returns them sorted
}

// notCarriedOverREST are the methods the REST dialect has no route for: they
// fail Unimplemented without sending anything.
var notCarriedOverREST = map[string]bool{"InferenceStream": true, "SubscribeJobEvents": true}

func orchestratorReachCases() map[string]reachCase {
	def := &orchestratorclient.WorkflowDefinition{Name: "w1", Mode: orchestratorclient.WorkflowModeCanned}
	return map[string]reachCase{
		"AttentionList":          {req: &orchestratorclient.AttentionListRequest{}, route: "GET /api/v1/orchestrator/attention"},
		"AttentionResolve":       {req: &orchestratorclient.AttentionResolveRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/attention/t1/resolve"},
		"BlockTask":              {req: &orchestratorclient.BlockTaskRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/tasks/t1/block"},
		"CancelJob":              {req: &orchestratorclient.CancelJobRequest{JobID: "j1"}, route: "DELETE /api/v1/orchestrator/jobs/j1"},
		"ClaimTask":              {req: &orchestratorclient.ClaimTaskRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/tasks/t1/claim"},
		"CloseSession":           {req: &orchestratorclient.CloseSessionRequest{SessionID: "s1"}, route: "DELETE /api/v1/orchestrator/sessions/s1"},
		"CompleteTask":           {req: &orchestratorclient.CompleteTaskRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/tasks/t1/complete"},
		"CreateTask":             {req: &orchestratorclient.CreateTaskRequest{}, route: "POST /api/v1/orchestrator/tasks"},
		"CreateWorkflow":         {req: &orchestratorclient.CreateWorkflowRequest{Definition: def}, route: "POST /api/v1/orchestrator/workflows"},
		"DecomposeTask":          {req: &orchestratorclient.DecomposeTaskRequest{}, route: "POST /api/v1/orchestrator/tasks/decompose"},
		"DeleteTask":             {req: &orchestratorclient.DeleteTaskRequest{TaskID: "t1"}, route: "DELETE /api/v1/orchestrator/tasks/t1"},
		"DeleteWorkflow":         {req: &orchestratorclient.DeleteWorkflowRequest{Name: "w1"}, route: "DELETE /api/v1/orchestrator/workflows/w1"},
		"DisableFleetNode":       {req: &orchestratorclient.DisableFleetNodeRequest{NodeID: "n1"}, route: "POST /api/v1/fleet/n1/disable"},
		"EnableFleetNode":        {req: &orchestratorclient.EnableFleetNodeRequest{NodeID: "n1"}, route: "POST /api/v1/fleet/n1/enable"},
		"FleetNodeHeartbeat":     {req: &orchestratorclient.FleetNodeHeartbeatRequest{NodeID: "n1"}, route: "POST /api/v1/fleet/n1/heartbeat"},
		"GetFleetNode":           {req: &orchestratorclient.GetFleetNodeRequest{NodeID: "n1"}, route: "GET /api/v1/fleet/n1"},
		"GetJobStatus":           {req: &orchestratorclient.GetJobStatusRequest{JobID: "j1"}, route: "GET /api/v1/orchestrator/jobs/j1"},
		"GetSessionConversation": {req: &orchestratorclient.GetSessionConversationRequest{SessionID: "s1"}, route: "GET /api/v1/orchestrator/sessions/s1/history"},
		"GetSessionHistory":      {req: &orchestratorclient.GetSessionHistoryRequest{SessionID: "s1"}, route: "GET /api/v1/orchestrator/sessions/s1/events"},
		"GetSpendReport":         {req: &orchestratorclient.GetSpendReportRequest{}, route: "GET /api/v1/orchestrator/cost"},
		"GetTask":                {req: &orchestratorclient.GetTaskRequest{TaskID: "t1"}, route: "GET /api/v1/orchestrator/tasks/t1"},
		"GetTaskArtifact":        {req: &orchestratorclient.GetTaskArtifactRequest{TaskID: "t1", Name: "a1"}, route: "GET /api/v1/orchestrator/tasks/t1/artifacts/a1"},
		"GetWorkflow":            {req: &orchestratorclient.GetWorkflowRequest{Name: "w1"}, route: "GET /api/v1/orchestrator/workflows/w1"},
		// The client's Health request type is unexported; both dialects ignore it.
		"Health":                      {req: nil, route: "GET /api/v1/orchestrator/health"},
		"Inference":                   {req: &orchestratorclient.InferenceRequest{}, route: "POST /api/v1/orchestrator/inference"},
		"InferenceStream":             {req: &orchestratorclient.InferenceRequest{}, stream: true},
		"ListBackendModels":           {req: &orchestratorclient.ListBackendModelsRequest{NodeID: "n1", BackendName: "claude"}, route: "GET /api/v1/fleet/n1/backends/claude/models"},
		"ListBackends":                {req: &orchestratorclient.ListBackendsRequest{}, route: "GET /api/v1/orchestrator/backends"},
		"ListFleetNodes":              {req: &orchestratorclient.ListFleetNodesRequest{}, route: "GET /api/v1/fleet"},
		"ListJobs":                    {req: &orchestratorclient.ListJobsRequest{}, route: "GET /api/v1/orchestrator/jobs"},
		"ListRunners":                 {req: &orchestratorclient.ListRunnersRequest{}, route: "GET /api/v1/orchestrator/runners"},
		"ListSessions":                {req: &orchestratorclient.ListSessionsRequest{}, route: "GET /api/v1/orchestrator/sessions"},
		"ListTaskArtifacts":           {req: &orchestratorclient.ListTaskArtifactsRequest{TaskID: "t1"}, route: "GET /api/v1/orchestrator/tasks/t1/artifacts"},
		"ListTasks":                   {req: &orchestratorclient.ListTasksRequest{}, route: "GET /api/v1/orchestrator/tasks"},
		"ListWorkflows":               {req: &orchestratorclient.ListWorkflowsRequest{}, route: "GET /api/v1/orchestrator/workflows"},
		"PromptSession":               {req: &orchestratorclient.PromptSessionRequest{SessionID: "s1"}, route: "POST /api/v1/orchestrator/sessions/s1/prompt"},
		"ReadmitTask":                 {req: &orchestratorclient.ReadmitTaskRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/tasks/t1/readmit"},
		"RegisterFleetNode":           {req: &orchestratorclient.RegisterFleetNodeRequest{}, route: "POST /api/v1/fleet"},
		"RemoveFleetNode":             {req: &orchestratorclient.RemoveFleetNodeRequest{NodeID: "n1"}, route: "DELETE /api/v1/fleet/n1"},
		"RotateSessionAccount":        {req: &orchestratorclient.RotateSessionAccountRequest{SessionID: "s1"}, route: "POST /api/v1/orchestrator/sessions/s1/rotate-account"},
		"SessionStatus":               {req: &orchestratorclient.SessionStatusRequest{SessionID: "s1"}, route: "GET /api/v1/orchestrator/sessions/s1"},
		"SpawnChildJob":               {req: &orchestratorclient.SpawnChildJobRequest{ParentJobID: "j1"}, route: "POST /api/v1/orchestrator/jobs/j1/children"},
		"SpawnSession":                {req: &orchestratorclient.SpawnSessionRequest{Backend: "claude"}, route: "POST /api/v1/orchestrator/sessions"},
		"StartJob":                    {req: &orchestratorclient.StartJobRequest{}, route: "POST /api/v1/orchestrator/jobs"},
		"SubmitTaskArtifacts":         {req: &orchestratorclient.SubmitTaskArtifactsRequest{TaskID: "t1"}, route: "POST /api/v1/orchestrator/tasks/t1/artifacts"},
		"SubscribeJobEvents":          {req: &orchestratorclient.SubscribeJobEventsRequest{}, stream: true},
		"SubscribeSessionEvents":      {req: &orchestratorclient.SubscribeSessionEventsRequest{SessionID: "s1"}, stream: true, route: "GET /api/v1/orchestrator/sessions/s1/events"},
		"UpdateWorkflow":              {req: &orchestratorclient.UpdateWorkflowRequest{Name: "w1", Definition: def}, route: "PUT /api/v1/orchestrator/workflows/w1"},
		"GetTenantSpendPolicy":        {req: &orchestratorclient.GetTenantSpendPolicyRequest{}, route: "GET /api/v1/orchestrator/spend-policy"},
		"SetTenantSpendPolicy":        {req: &orchestratorclient.SetTenantSpendPolicyRequest{}, route: "PUT /api/v1/orchestrator/spend-policy"},
		"GetCostSettings":             {req: &orchestratorclient.GetCostSettingsRequest{}, route: "GET /api/v1/orchestrator/cost-settings"},
		"SetCostSettings":             {req: &orchestratorclient.SetCostSettingsRequest{}, route: "PUT /api/v1/orchestrator/cost-settings"},
		"DeclareFleetPlacementSpec":   {req: &orchestratorclient.DeclareFleetPlacementSpecRequest{Spec: &orchestratorclient.FleetPlacementSpec{Name: "p1"}}, route: "PUT /api/v1/fleet/placements/p1"},
		"GetFleetPlacementSpec":       {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, route: "GET /api/v1/fleet/placements/p1"},
		"ListFleetPlacementSpecs":     {req: &orchestratorclient.ListFleetPlacementSpecsRequest{}, route: "GET /api/v1/fleet/placements"},
		"WithdrawFleetPlacementSpec":  {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, route: "DELETE /api/v1/fleet/placements/p1"},
		"GetFleetPlacementDrift":      {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, route: "GET /api/v1/fleet/placements/p1/drift"},
		"ListFleetPlacementConsents":  {req: &orchestratorclient.ListFleetPlacementConsentsRequest{}, route: "GET /api/v1/fleet/placement-consents"},
		"AnswerFleetPlacementConsent": {req: &orchestratorclient.AnswerFleetPlacementConsentRequest{ID: "c1", State: "granted"}, route: "POST /api/v1/fleet/placement-consents/c1/answer"},
	}
}

// TestOrchestratorReach_QUIC calls every orchestrator client method over
// HTTP/3 and asserts that every request it sends equals its route, except
// the streams the REST dialect does not carry, which must fail Unimplemented
// without sending anything.
func TestOrchestratorReach_QUIC(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	cases := orchestratorReachCases()
	got := make([]string, 0, len(cases))
	for name := range cases {
		got = append(got, name)
	}
	sort.Strings(got)
	want := clientMethods()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("case table does not cover the client's methods\n got: %v\nwant: %v", got, want)
	}

	for name, c := range cases {
		if !notCarriedOverREST[name] && c.route == "" {
			t.Fatalf("%s: case has no route", name)
		}
	}

	for _, name := range clientMethods() {
		name, c := name, cases[name]
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mu.Lock()
			before := len(requests)
			mu.Unlock()

			var err error
			if c.stream {
				// The SSE request is sent when the stream opens, so Close
				// without Recv.
				s, serr := tr.RequestStream(ctx, "quicbaas.Orchestrator", name, c.req)
				if s != nil {
					_ = s.Close()
				}
				err = serr
			} else {
				// A response-decode error is fine: only the send matters.
				_, err = tr.Request(ctx, "quicbaas.Orchestrator", name, c.req)
			}

			mu.Lock()
			sent := append([]string(nil), requests[before:]...)
			mu.Unlock()

			if notCarriedOverREST[name] {
				if !errors.IsUnimplemented(err) {
					t.Errorf("%s: want Unimplemented, got %v", name, err)
				}
				if len(sent) != 0 {
					t.Errorf("%s: the dialect should send nothing, server saw %v", name, sent)
				}
				return
			}
			if len(sent) == 0 {
				t.Fatalf("%s: nothing reached the server (err %v)", name, err)
			}
			for _, r := range sent {
				if r != c.route {
					t.Errorf("%s: server saw %q, want %q (all: %v)", name, r, c.route, sent)
				}
			}
		})
	}
}

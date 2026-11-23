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
// the transport, and whether it is a server stream.
type reachCase struct {
	req    interface{}
	stream bool
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
var notCarriedOverREST = map[string]bool{"InferenceStream": true, "SubscribeJobEvents": true, "GetSessionHistory": true}

func orchestratorReachCases() map[string]reachCase {
	def := &orchestratorclient.WorkflowDefinition{Name: "w1", Mode: orchestratorclient.WorkflowModeCanned}
	return map[string]reachCase{
		"AttentionList":          {req: &orchestratorclient.AttentionListRequest{}},
		"AttentionResolve":       {req: &orchestratorclient.AttentionResolveRequest{}},
		"BlockTask":              {req: &orchestratorclient.BlockTaskRequest{}},
		"CancelJob":              {req: &orchestratorclient.CancelJobRequest{}},
		"ClaimTask":              {req: &orchestratorclient.ClaimTaskRequest{}},
		"CloseSession":           {req: &orchestratorclient.CloseSessionRequest{}},
		"CompleteTask":           {req: &orchestratorclient.CompleteTaskRequest{}},
		"CreateTask":             {req: &orchestratorclient.CreateTaskRequest{}},
		"CreateWorkflow":         {req: &orchestratorclient.CreateWorkflowRequest{Definition: def}},
		"DecomposeTask":          {req: &orchestratorclient.DecomposeTaskRequest{}},
		"DeleteTask":             {req: &orchestratorclient.DeleteTaskRequest{}},
		"DeleteWorkflow":         {req: &orchestratorclient.DeleteWorkflowRequest{}},
		"DisableFleetNode":       {req: &orchestratorclient.DisableFleetNodeRequest{}},
		"EnableFleetNode":        {req: &orchestratorclient.EnableFleetNodeRequest{}},
		"FleetNodeHeartbeat":     {req: &orchestratorclient.FleetNodeHeartbeatRequest{}},
		"GetFleetNode":           {req: &orchestratorclient.GetFleetNodeRequest{}},
		"GetJobStatus":           {req: &orchestratorclient.GetJobStatusRequest{}},
		"GetSessionConversation": {req: &orchestratorclient.GetSessionConversationRequest{SessionID: "s1"}},
		"GetSessionHistory":      {req: &orchestratorclient.GetSessionHistoryRequest{}},
		"GetSpendReport":         {req: &orchestratorclient.GetSpendReportRequest{}},
		"GetTask":                {req: &orchestratorclient.GetTaskRequest{}},
		"GetTaskArtifact":        {req: &orchestratorclient.GetTaskArtifactRequest{}},
		"GetWorkflow":            {req: &orchestratorclient.GetWorkflowRequest{}},
		// The client's Health request type is unexported; both dialects ignore it.
		"Health":                      {req: nil},
		"Inference":                   {req: &orchestratorclient.InferenceRequest{}},
		"InferenceStream":             {req: &orchestratorclient.InferenceRequest{}, stream: true},
		"ListBackendModels":           {req: &orchestratorclient.ListBackendModelsRequest{BackendName: "claude"}},
		"ListBackends":                {req: &orchestratorclient.ListBackendsRequest{}},
		"ListFleetNodes":              {req: &orchestratorclient.ListFleetNodesRequest{}},
		"ListJobs":                    {req: &orchestratorclient.ListJobsRequest{}},
		"ListRunners":                 {req: &orchestratorclient.ListRunnersRequest{}},
		"ListSessions":                {req: &orchestratorclient.ListSessionsRequest{}},
		"ListTaskArtifacts":           {req: &orchestratorclient.ListTaskArtifactsRequest{}},
		"ListTasks":                   {req: &orchestratorclient.ListTasksRequest{}},
		"ListWorkflows":               {req: &orchestratorclient.ListWorkflowsRequest{}},
		"PromptSession":               {req: &orchestratorclient.PromptSessionRequest{}},
		"ReadmitTask":                 {req: &orchestratorclient.ReadmitTaskRequest{}},
		"RegisterFleetNode":           {req: &orchestratorclient.RegisterFleetNodeRequest{}},
		"RemoveFleetNode":             {req: &orchestratorclient.RemoveFleetNodeRequest{}},
		"RotateSessionAccount":        {req: &orchestratorclient.RotateSessionAccountRequest{SessionID: "s1"}},
		"SessionStatus":               {req: &orchestratorclient.SessionStatusRequest{}},
		"SpawnChildJob":               {req: &orchestratorclient.SpawnChildJobRequest{}},
		"SpawnSession":                {req: &orchestratorclient.SpawnSessionRequest{Backend: "claude"}},
		"StartJob":                    {req: &orchestratorclient.StartJobRequest{}},
		"SubmitTaskArtifacts":         {req: &orchestratorclient.SubmitTaskArtifactsRequest{}},
		"SubscribeJobEvents":          {req: &orchestratorclient.SubscribeJobEventsRequest{}, stream: true},
		"SubscribeSessionEvents":      {req: &orchestratorclient.SubscribeSessionEventsRequest{SessionID: "s1"}, stream: true},
		"UpdateWorkflow":              {req: &orchestratorclient.UpdateWorkflowRequest{Name: "w1", Definition: def}},
		"GetTenantSpendPolicy":        {req: &orchestratorclient.GetTenantSpendPolicyRequest{}},
		"SetTenantSpendPolicy":        {req: &orchestratorclient.SetTenantSpendPolicyRequest{}},
		"GetCostSettings":             {req: &orchestratorclient.GetCostSettingsRequest{}},
		"SetCostSettings":             {req: &orchestratorclient.SetCostSettingsRequest{}},
		"DeclareFleetPlacementSpec":   {req: &orchestratorclient.DeclareFleetPlacementSpecRequest{Spec: &orchestratorclient.FleetPlacementSpec{Name: "p1"}}},
		"GetFleetPlacementSpec":       {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}},
		"ListFleetPlacementSpecs":     {req: &orchestratorclient.ListFleetPlacementSpecsRequest{}},
		"WithdrawFleetPlacementSpec":  {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}},
		"GetFleetPlacementDrift":      {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}},
		"ListFleetPlacementConsents":  {req: &orchestratorclient.ListFleetPlacementConsentsRequest{}},
		"AnswerFleetPlacementConsent": {req: &orchestratorclient.AnswerFleetPlacementConsentRequest{ID: "c1", State: "granted"}},
	}
}

// TestOrchestratorReach_QUIC calls every orchestrator client method over
// HTTP/3 and asserts that each one reaches the server under /api/v1/, except
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
			reached := false
			for _, r := range sent {
				if _, path, ok := strings.Cut(r, " "); ok && strings.HasPrefix(path, "/api/v1/") {
					reached = true
				}
			}
			if !reached {
				t.Errorf("%s: no request under /api/v1/ reached the server (sent %v, err %v)", name, sent, err)
			}
		})
	}
}

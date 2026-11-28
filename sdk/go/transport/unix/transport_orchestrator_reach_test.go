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

package unix

import (
	"context"
	"net"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// reachOrchestrator and reachSDLC answer every RPC Unimplemented; the test
// only asks whether the call reached the server.
type reachOrchestrator struct {
	pb.UnimplementedOrchestratorServiceServer
}

type reachSDLC struct {
	pb.UnimplementedSDLCServiceServer
}

// reachCase is one orchestrator client method: the request its client hands
// the transport, whether it is a server stream, and the suffix of the gRPC
// FullMethod the server records for it.
type reachCase struct {
	req    interface{}
	stream bool
	rpc    string
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

// notCarriedOverGRPC are client methods the gRPC dialect has no handler for.
var notCarriedOverGRPC = map[string]bool{
	"DisableFleetNode": true, "EnableFleetNode": true,
}

func orchestratorReachCases() map[string]reachCase {
	def := &orchestratorclient.WorkflowDefinition{Name: "w1", Mode: orchestratorclient.WorkflowModeConstrained}
	return map[string]reachCase{
		"AttentionList":          {req: &orchestratorclient.AttentionListRequest{}, rpc: "/ListAttention"},
		"AttentionResolve":       {req: &orchestratorclient.AttentionResolveRequest{}, rpc: "/ResolveAttention"},
		"BlockTask":              {req: &orchestratorclient.BlockTaskRequest{}, rpc: "/BlockTask"},
		"CancelJob":              {req: &orchestratorclient.CancelJobRequest{}, rpc: "/CancelJob"},
		"ClaimTask":              {req: &orchestratorclient.ClaimTaskRequest{}, rpc: "/ClaimTask"},
		"CloseSession":           {req: &orchestratorclient.CloseSessionRequest{}, rpc: "/CloseSession"},
		"CompleteTask":           {req: &orchestratorclient.CompleteTaskRequest{}, rpc: "/CompleteTask"},
		"CreateTask":             {req: &orchestratorclient.CreateTaskRequest{}, rpc: "/CreateTask"},
		"CreateWorkflow":         {req: &orchestratorclient.CreateWorkflowRequest{Definition: def}, rpc: "/CreateWorkflow"},
		"DecomposeTask":          {req: &orchestratorclient.DecomposeTaskRequest{}, rpc: "/DecomposeTask"},
		"DeleteTask":             {req: &orchestratorclient.DeleteTaskRequest{}, rpc: "/DeleteTask"},
		"DeleteWorkflow":         {req: &orchestratorclient.DeleteWorkflowRequest{}, rpc: "/DeleteWorkflow"},
		"DisableFleetNode":       {req: &orchestratorclient.DisableFleetNodeRequest{}},
		"EnableFleetNode":        {req: &orchestratorclient.EnableFleetNodeRequest{}},
		"FleetNodeHeartbeat":     {req: &orchestratorclient.FleetNodeHeartbeatRequest{}, rpc: "/FleetNodeHeartbeat"},
		"GetFleetNode":           {req: &orchestratorclient.GetFleetNodeRequest{}, rpc: "/GetFleetNode"},
		"GetJobStatus":           {req: &orchestratorclient.GetJobStatusRequest{}, rpc: "/GetJobStatus"},
		"GetSessionConversation": {req: &orchestratorclient.GetSessionConversationRequest{SessionID: "s1"}, rpc: "/GetSessionConversation"},
		"GetSessionHistory":      {req: &orchestratorclient.GetSessionHistoryRequest{}, rpc: "/SubscribeSessionEvents"},
		"GetSpendReport":         {req: &orchestratorclient.GetSpendReportRequest{}, rpc: "/GetSpendReport"},
		"GetTask":                {req: &orchestratorclient.GetTaskRequest{}, rpc: "/GetTask"},
		"GetTaskArtifact":        {req: &orchestratorclient.GetTaskArtifactRequest{}, rpc: "/GetTaskArtifact"},
		"GetWorkflow":            {req: &orchestratorclient.GetWorkflowRequest{}, rpc: "/GetWorkflow"},
		// The client's Health request type is unexported; both dialects ignore it.
		"Health":                      {req: nil, rpc: "/Health"},
		"Inference":                   {req: &orchestratorclient.InferenceRequest{}, rpc: "/Inference"},
		"InferenceStream":             {req: &orchestratorclient.InferenceRequest{}, stream: true, rpc: "/InferenceStream"},
		"ListBackendModels":           {req: &orchestratorclient.ListBackendModelsRequest{BackendName: "claude"}, rpc: "/ListBackendModels"},
		"ListBackends":                {req: &orchestratorclient.ListBackendsRequest{}, rpc: "/ListBackends"},
		"ListFleetNodes":              {req: &orchestratorclient.ListFleetNodesRequest{}, rpc: "/ListFleetNodes"},
		"ListJobs":                    {req: &orchestratorclient.ListJobsRequest{}, rpc: "/ListJobs"},
		"ListRunners":                 {req: &orchestratorclient.ListRunnersRequest{}, rpc: "/ListRunners"},
		"ListSessions":                {req: &orchestratorclient.ListSessionsRequest{}, rpc: "/ListSessions"},
		"ListTaskArtifacts":           {req: &orchestratorclient.ListTaskArtifactsRequest{}, rpc: "/ListTaskArtifacts"},
		"ListTasks":                   {req: &orchestratorclient.ListTasksRequest{}, rpc: "/ListTasks"},
		"ListWorkflows":               {req: &orchestratorclient.ListWorkflowsRequest{}, rpc: "/ListWorkflows"},
		"PromptSession":               {req: &orchestratorclient.PromptSessionRequest{}, rpc: "/PromptSession"},
		"ReadmitTask":                 {req: &orchestratorclient.ReadmitTaskRequest{}, rpc: "/ReadmitTask"},
		"RegisterFleetNode":           {req: &orchestratorclient.RegisterFleetNodeRequest{}, rpc: "/RegisterFleetNode"},
		"RemoveFleetNode":             {req: &orchestratorclient.RemoveFleetNodeRequest{}, rpc: "/RemoveFleetNode"},
		"RotateSessionAccount":        {req: &orchestratorclient.RotateSessionAccountRequest{SessionID: "s1"}, rpc: "/RotateSessionAccount"},
		"SessionStatus":               {req: &orchestratorclient.SessionStatusRequest{}, rpc: "/SessionStatus"},
		"SpawnChildJob":               {req: &orchestratorclient.SpawnChildJobRequest{}, rpc: "/SpawnChildJob"},
		"SpawnSession":                {req: &orchestratorclient.SpawnSessionRequest{Backend: "claude"}, rpc: "/SpawnSession"},
		"StartJob":                    {req: &orchestratorclient.StartJobRequest{}, rpc: "/StartJob"},
		"SubmitTaskArtifacts":         {req: &orchestratorclient.SubmitTaskArtifactsRequest{}, rpc: "/SubmitTaskArtifacts"},
		"SubscribeJobEvents":          {req: &orchestratorclient.SubscribeJobEventsRequest{}, stream: true, rpc: "/SubscribeJobEvents"},
		"SubscribeSessionEvents":      {req: &orchestratorclient.SubscribeSessionEventsRequest{SessionID: "s1"}, stream: true, rpc: "/SubscribeSessionEvents"},
		"UpdateWorkflow":              {req: &orchestratorclient.UpdateWorkflowRequest{Name: "w1", Definition: def}, rpc: "/UpdateWorkflow"},
		"GetTenantSpendPolicy":        {req: &orchestratorclient.GetTenantSpendPolicyRequest{}, rpc: "/GetTenantSpendPolicy"},
		"SetTenantSpendPolicy":        {req: &orchestratorclient.SetTenantSpendPolicyRequest{}, rpc: "/SetTenantSpendPolicy"},
		"GetCostSettings":             {req: &orchestratorclient.GetCostSettingsRequest{}, rpc: "/GetCostSettings"},
		"SetCostSettings":             {req: &orchestratorclient.SetCostSettingsRequest{}, rpc: "/SetCostSettings"},
		"DeclareFleetPlacementSpec":   {req: &orchestratorclient.DeclareFleetPlacementSpecRequest{Spec: &orchestratorclient.FleetPlacementSpec{Name: "p1"}}, rpc: "/DeclareFleetPlacementSpec"},
		"GetFleetPlacementSpec":       {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, rpc: "/GetFleetPlacementSpec"},
		"ListFleetPlacementSpecs":     {req: &orchestratorclient.ListFleetPlacementSpecsRequest{}, rpc: "/ListFleetPlacementSpecs"},
		"WithdrawFleetPlacementSpec":  {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, rpc: "/WithdrawFleetPlacementSpec"},
		"GetFleetPlacementDrift":      {req: &orchestratorclient.FleetPlacementSpecRequest{Name: "p1"}, rpc: "/GetFleetPlacementDrift"},
		"ListFleetPlacementConsents":  {req: &orchestratorclient.ListFleetPlacementConsentsRequest{}, rpc: "/ListFleetPlacementConsents"},
		"AnswerFleetPlacementConsent": {req: &orchestratorclient.AnswerFleetPlacementConsentRequest{ID: "c1", State: "granted"}, rpc: "/AnswerFleetPlacementConsent"},
	}
}

// TestOrchestratorReach_Unix calls every orchestrator client method over a
// real Unix socket and asserts that each one reaches the gRPC server, except
// the methods the gRPC dialect does not carry, which must fail client-side
// without sending anything.
func TestOrchestratorReach_Unix(t *testing.T) {
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
	)
	record := func(fullMethod string) {
		mu.Lock()
		defer mu.Unlock()
		seen[fullMethod] = true
	}
	recordedSuffix := func(suffix string) bool {
		mu.Lock()
		defer mu.Unlock()
		for m := range seen {
			if strings.HasSuffix(m, suffix) {
				return true
			}
		}
		return false
	}

	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (interface{}, error) {
			record(info.FullMethod)
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			record(info.FullMethod)
			return h(srv, ss)
		}),
	)
	pb.RegisterOrchestratorServiceServer(server, &reachOrchestrator{})
	pb.RegisterSDLCServiceServer(server, &reachSDLC{})
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

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
			// Forget earlier calls, so each method must reach the server itself.
			mu.Lock()
			seen = map[string]bool{}
			mu.Unlock()
			var err error
			if c.stream {
				var s transport.Stream
				if s, err = tr.RequestStream(ctx, "quicbaas.Orchestrator", name, c.req); err == nil {
					_, err = s.Recv()
					_ = s.Close()
				}
			} else {
				_, err = tr.Request(ctx, "quicbaas.Orchestrator", name, c.req)
			}

			if notCarriedOverGRPC[name] {
				if err == nil || !strings.Contains(err.Error(), "unsupported orchestrator method") {
					t.Errorf("%s: want the client-side unsupported error, got %v", name, err)
				}
				if recordedSuffix("/" + name) {
					t.Errorf("%s: the server saw a call the dialect should not send", name)
				}
				return
			}
			if err != nil && strings.Contains(err.Error(), "unsupported orchestrator") {
				t.Errorf("%s: the gRPC dialect does not carry it: %v", name, err)
			}
			if !recordedSuffix(c.rpc) {
				t.Errorf("%s: no server call ending in %s (err: %v)", name, c.rpc, err)
			}
		})
	}
}

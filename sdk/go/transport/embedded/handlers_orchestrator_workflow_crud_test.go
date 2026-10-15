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

package embedded

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/workflowcrud"
)

func TestWorkflowCRUD_RoundTripsInProcess(t *testing.T) {
	tr, err := New(workflowcrud.NewStore())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	workflowcrud.Exercise(t, orchestratorSDK.NewClient(tr), errors.IsAlreadyExists)
}

func TestWorkflowCRUDHandlers_RejectBadRequests(t *testing.T) {
	ctx := context.Background()
	store := workflowcrud.NewStore()
	wrongType := &orchestratorSDK.ListWorkflowsRequest{}
	for verb, handle := range map[string]func(context.Context, service.QuicBaaS, interface{}) (interface{}, error){
		"CreateWorkflow": handleOrchestratorCreateWorkflow,
		"GetWorkflow":    handleOrchestratorGetWorkflow,
		"UpdateWorkflow": handleOrchestratorUpdateWorkflow,
		"DeleteWorkflow": handleOrchestratorDeleteWorkflow,
	} {
		if _, err := handle(ctx, store, wrongType); err == nil {
			t.Errorf("%s accepted the wrong request type", verb)
		}
	}
	if _, err := handleOrchestratorCreateWorkflow(ctx, store, &orchestratorSDK.CreateWorkflowRequest{TenantID: "t"}); err == nil {
		t.Error("CreateWorkflow accepted a request without a definition")
	}
	if _, err := handleOrchestratorUpdateWorkflow(ctx, store, &orchestratorSDK.UpdateWorkflowRequest{TenantID: "t", Name: "w"}); err == nil {
		t.Error("UpdateWorkflow accepted a request without a definition")
	}
}

func TestWorkflowCRUD_RefusedWithoutAnOrchestrator(t *testing.T) {
	// The base mock returns a nil Agent(): the node runs no orchestrator.
	client := orchestratorSDK.NewClient(newNoOrchestratorTransport(t))
	ctx := context.Background()
	def := workflowcrud.Definition(t)
	if _, err := client.CreateWorkflow(ctx, &orchestratorSDK.CreateWorkflowRequest{TenantID: "t", Definition: def}); err == nil {
		t.Error("CreateWorkflow succeeded on a node without an orchestrator")
	}
	if _, err := client.GetWorkflow(ctx, &orchestratorSDK.GetWorkflowRequest{TenantID: "t", Name: def.Name}); err == nil {
		t.Error("GetWorkflow succeeded on a node without an orchestrator")
	}
	if _, err := client.UpdateWorkflow(ctx, &orchestratorSDK.UpdateWorkflowRequest{TenantID: "t", Name: def.Name, Definition: def}); err == nil {
		t.Error("UpdateWorkflow succeeded on a node without an orchestrator")
	}
	if err := client.DeleteWorkflow(ctx, &orchestratorSDK.DeleteWorkflowRequest{TenantID: "t", Name: def.Name}); err == nil {
		t.Error("DeleteWorkflow succeeded on a node without an orchestrator")
	}
}

func newNoOrchestratorTransport(t *testing.T) *Transport {
	t.Helper()
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return tr
}

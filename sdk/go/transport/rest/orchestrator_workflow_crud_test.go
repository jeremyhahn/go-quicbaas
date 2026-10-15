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
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/workflowcrud"
)

func TestWorkflowCRUD_RoundTripsOverREST(t *testing.T) {
	server := httptest.NewServer(workflowcrud.HTTPHandler(workflowcrud.NewStore().Agent()))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	workflowcrud.Exercise(t, orchestrator.NewClient(tr), errors.IsConflict)
}

func TestWorkflowCRUDHandlers_RejectBadRequests(t *testing.T) {
	h := &orchestratorHandler{}
	ctx := context.Background()
	wrongType := &orchestrator.ListWorkflowsRequest{}
	for verb, handle := range map[string]func(context.Context, interface{}) (interface{}, error){
		"CreateWorkflow": h.handleCreateWorkflow,
		"GetWorkflow":    h.handleGetWorkflow,
		"UpdateWorkflow": h.handleUpdateWorkflow,
		"DeleteWorkflow": h.handleDeleteWorkflow,
	} {
		if _, err := handle(ctx, wrongType); !errors.IsInvalidInput(err) {
			t.Errorf("%s with the wrong request type: got %v, want invalid input", verb, err)
		}
	}
	if _, err := h.handleCreateWorkflow(ctx, &orchestrator.CreateWorkflowRequest{TenantID: "t"}); !errors.IsInvalidInput(err) {
		t.Errorf("CreateWorkflow without a definition: got %v, want invalid input", err)
	}
	if _, err := h.handleUpdateWorkflow(ctx, &orchestrator.UpdateWorkflowRequest{TenantID: "t", Name: "w"}); !errors.IsInvalidInput(err) {
		t.Errorf("UpdateWorkflow without a definition: got %v, want invalid input", err)
	}
}

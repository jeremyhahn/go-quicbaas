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

package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// failingModelsAgentService lists models by failing with err.
type failingModelsAgentService struct {
	*mockAgentService
	err error
}

func (f *failingModelsAgentService) ListBackendModels(context.Context, string) ([]string, error) {
	return nil, f.err
}

// listModelsFailingWith answers a model listing whose agent service fails
// with err.
func listModelsFailingWith(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: &failingModelsAgentService{mockAgentService: &mockAgentService{}, err: err},
	}
	w := httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(w, orchReq(http.MethodGet, "/x", nil,
		map[string]string{"nodeID": "agent-09", "backendName": "ollama"}))
	return w
}

func routedFailure(err error) error {
	return &orchestration.DaemonRoutingError{
		Backend: "ollama",
		Address: "agent-09.example.test:9091",
		Op:      "ListModels",
		Err:     err,
	}
}

// TestHandleListFleetNodeBackendModels_RoutingFailureNamesTheDaemon
// (go-quicbaas#795): a daemon that cannot be reached is a 503 and one that
// fails is a 502, each naming the daemon address, never a bare 500.
func TestHandleListFleetNodeBackendModels_RoutingFailureNamesTheDaemon(t *testing.T) {
	unreachable := listModelsFailingWith(t, routedFailure(
		&orchestration.RemoteBackendDialError{Host: "agent-09.example.test:9091", Err: errors.New("connection refused")}))
	assert.Equal(t, http.StatusServiceUnavailable, unreachable.Code)
	assert.Contains(t, unreachable.Body.String(), "agent-09.example.test:9091 is unreachable")

	failed := listModelsFailingWith(t, routedFailure(
		&orchestration.RemoteBackendRPCError{Op: "ListModels", Err: status.Error(codes.Internal, "ollama crashed")}))
	assert.Equal(t, http.StatusBadGateway, failed.Code)
	assert.Contains(t, failed.Body.String(), "agent-09.example.test:9091")
}

// TestHandleListFleetNodeBackendModels_DaemonRefusalKeepsItsMapping: a
// refusal the daemon reasons about is not a routing failure; a permission
// denial stays a 403 and an unclassified error stays a 500.
func TestHandleListFleetNodeBackendModels_DaemonRefusalKeepsItsMapping(t *testing.T) {
	denied := listModelsFailingWith(t, routedFailure(
		&orchestration.RemoteBackendRPCError{Op: "ListModels", Err: status.Error(codes.PermissionDenied, "scope")}))
	assert.Equal(t, http.StatusForbidden, denied.Code)

	internal := listModelsFailingWith(t, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, internal.Code)
}

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
	"context"
	"errors"
	"testing"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// errStoreSealed stands in for a store that cannot answer.
var errStoreSealed = errors.New("barrier sealed")

type failingWorkflowGetter struct{}

func (failingWorkflowGetter) Get(string, string) (*WorkflowDefinition, error) {
	return nil, errStoreSealed
}

type fakeFleetNodes struct {
	agents []*FleetNode
	err    error
}

func (f fakeFleetNodes) ListFleetNodes(context.Context, string, *FleetNodeFilter) ([]*FleetNode, error) {
	return f.agents, f.err
}

type fakeTenantResolver struct {
	known map[string]bool
	err   error
}

func (f fakeTenantResolver) TenantExists(_ context.Context, tenantID string) (bool, error) {
	return f.known[tenantID], f.err
}

// assertReferenceOutcome checks err is nil, a not-found or an unavailable
// reference error, exactly as want says.
func assertReferenceOutcome(t *testing.T, err error, want string) {
	t.Helper()
	var notFound *qerrors.ReferenceNotFoundError
	var unavailable *qerrors.ReferenceUnavailableError
	switch want {
	case "ok":
		if err != nil {
			t.Fatalf("want no error, got %v", err)
		}
	case "not_found":
		if !errors.As(err, &notFound) {
			t.Fatalf("want ReferenceNotFoundError, got %T %v", err, err)
		}
	case "unavailable":
		if !errors.As(err, &unavailable) {
			t.Fatalf("want ReferenceUnavailableError, got %T %v", err, err)
		}
		if errors.As(err, &notFound) {
			t.Fatal("an unavailable store must not read as not found")
		}
	default:
		t.Fatalf("unknown outcome %q", want)
	}
}

func TestRequireWorkflow(t *testing.T) {
	loader := NewWorkflowLoader()
	if err := loader.Register("tenant-1", &WorkflowDefinition{
		Name: "sdlc", Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{{Role: "worker", Backend: "ollama", Model: "m"}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	tests := []struct {
		name      string
		workflows WorkflowGetter
		ref       string
		want      string
	}{
		{"exists", loader, "sdlc", "ok"},
		{"missing", loader, "nope", "not_found"},
		{"store fails", failingWorkflowGetter{}, "sdlc", "unavailable"},
		{"no store attached", nil, "sdlc", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertReferenceOutcome(t, RequireWorkflow(tt.workflows, "default_workflow", "tenant-1", tt.ref), tt.want)
		})
	}
}

func TestRequireFleetNode(t *testing.T) {
	fleet := fakeFleetNodes{agents: []*FleetNode{
		nil,
		{ID: "agent-1", Name: "worker", Host: "worker-host"},
	}}
	tests := []struct {
		name  string
		nodes FleetNodeLister
		ref   string
		want  string
	}{
		{"by id", fleet, "agent-1", "ok"},
		{"by name", fleet, "worker", "ok"},
		{"by host", fleet, "worker-host", "ok"},
		{"missing", fleet, "missing-host", "not_found"},
		{"store fails", fakeFleetNodes{err: errStoreSealed}, "agent-1", "unavailable"},
		{"no store attached", nil, "agent-1", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertReferenceOutcome(t, RequireFleetNode(context.Background(), tt.nodes, "host_affinity", "tenant-1", tt.ref), tt.want)
		})
	}
}

func TestRequireTenant(t *testing.T) {
	tenants := fakeTenantResolver{known: map[string]bool{"acme": true}}
	tests := []struct {
		name     string
		resolver TenantResolver
		ref      string
		want     string
	}{
		{"exists", tenants, "acme", "ok"},
		{"missing", tenants, "globex", "not_found"},
		{"store fails", fakeTenantResolver{err: errStoreSealed}, "acme", "unavailable"},
		{"no store attached", nil, "acme", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertReferenceOutcome(t, RequireTenant(context.Background(), tt.resolver, "tenant_id", tt.ref), tt.want)
		})
	}
}

func TestStartJob_WorkflowNameReference(t *testing.T) {
	tests := []struct {
		name   string
		loader *WorkflowLoader
		want   string
	}{
		{"missing workflow", NewWorkflowLoader(), "not_found"},
		{"no workflow registry attached", nil, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newMockBackendService(t)
			if tt.loader != nil {
				svc.SetWorkflowLoader(tt.loader)
			}
			_, err := svc.StartJob(context.Background(), &StartJobRequest{
				TenantID: "tenant-1", Description: "x", WorkflowName: "does-not-exist",
			})
			defer cancelAllJobs(svc)
			assertReferenceOutcome(t, err, tt.want)
		})
	}
}

func TestRegisterFleetNode_TenantReference(t *testing.T) {
	tests := []struct {
		name     string
		resolver TenantResolver
		tenantID string
		want     string
	}{
		{"known tenant", fakeTenantResolver{known: map[string]bool{"tenant-1": true}}, "tenant-1", "ok"},
		{"unknown tenant", fakeTenantResolver{known: map[string]bool{}}, "tenant-1", "not_found"},
		{"tenant directory fails", fakeTenantResolver{err: errStoreSealed}, "tenant-1", "unavailable"},
		{"no tenant directory on this node", nil, "tenant-1", "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl := newTestService(t)
			impl.SetFleetRegistry(NewInMemoryFleetRegistry())
			if tt.resolver != nil {
				impl.SetTenantResolver(tt.resolver)
			}
			_, err := impl.RegisterFleetNode(context.Background(), &RegisterFleetNodeRequest{
				TenantID: tt.tenantID, Name: "worker", Host: "worker-host", NodeType: "claude-code",
			})
			assertReferenceOutcome(t, err, tt.want)
		})
	}
}

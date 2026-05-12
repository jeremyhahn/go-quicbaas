// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

//go:build desktop

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// stubAgentDeployServiceBinding captures the spec so assertions can
// inspect what the binding forwards on.
type stubAgentDeployServiceBinding struct {
	result *deploy.Result
	err    error
	seen   *deploy.Spec
}

func (s *stubAgentDeployServiceBinding) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	s.seen = spec
	return s.result, s.err
}

// testDeployLogger returns a logger suitable for binding tests.
func testDeployLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	return log
}

func TestAgentDeployBinding_DeploySuccess(t *testing.T) {
	stub := &stubAgentDeployServiceBinding{
		result: &deploy.Result{Host: "h", InstalledPath: "/p", ServiceStatus: "systemd"},
	}
	b := NewAgentDeployBinding(testDeployLogger(t), NewConnectionBinding(testDeployLogger(t)))
	b.newService = func(service.AgentLister) service.AgentDeployService { return stub }
	b.ctx = context.Background()

	spec := &deploy.Spec{Host: "remote", SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
		Server: "c", Tenant: "t", Binary: "bin",
		AgentBind: "a", Backends: []string{"acp"}}
	result, err := b.Deploy(spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.Host != "h" {
		t.Fatalf("host=%q", result.Host)
	}
	if stub.seen != spec {
		t.Fatal("spec not forwarded")
	}
}

func TestAgentDeployBinding_NilSpec(t *testing.T) {
	b := NewAgentDeployBinding(testDeployLogger(t), NewConnectionBinding(testDeployLogger(t)))
	_, err := b.Deploy(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var reqErr *GUIRequestRequiredError
	if !errors.As(err, &reqErr) {
		t.Fatalf("want GUIRequestRequiredError, got %T: %v", err, err)
	}
}

func TestAgentDeployBinding_ServiceError(t *testing.T) {
	want := &deploy.SSHDialError{Host: "x"}
	stub := &stubAgentDeployServiceBinding{err: want}
	b := NewAgentDeployBinding(testDeployLogger(t), NewConnectionBinding(testDeployLogger(t)))
	b.newService = func(service.AgentLister) service.AgentDeployService { return stub }
	b.ctx = context.Background()

	_, err := b.Deploy(&deploy.Spec{Host: "x", SSHUser: "u"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped cause, got %v", err)
	}
}

func TestAgentDeployBinding_NilContextFallsBack(t *testing.T) {
	var capturedCtx context.Context
	b := NewAgentDeployBinding(testDeployLogger(t), NewConnectionBinding(testDeployLogger(t)))
	b.newService = func(service.AgentLister) service.AgentDeployService {
		return deployCaptureService{fn: func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
			capturedCtx = ctx
			return &deploy.Result{}, nil
		}}
	}
	if _, err := b.Deploy(&deploy.Spec{}); err != nil {
		t.Fatal(err)
	}
	if capturedCtx == nil {
		t.Fatal("expected fallback background context")
	}
}

type deployCaptureService struct {
	fn func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)
}

func (d deployCaptureService) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	return d.fn(ctx, spec)
}

func TestLoadAgentLister_NoConnection(t *testing.T) {
	conn := NewConnectionBinding(testDeployLogger(t))
	b := NewAgentDeployBinding(testDeployLogger(t), conn)
	if lister := b.loadAgentLister(); lister != nil {
		t.Fatal("expected nil lister when disconnected")
	}
}

// stubSDKLister satisfies the service.AgentLister contract so the
// factory test can exercise the wiring with a live adapter.
type stubSDKLister struct{}

func (stubSDKLister) ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
	return &orchestratorSDK.ListAgentsResponse{}, nil
}

func TestDefaultAgentDeployServiceFactory(t *testing.T) {
	if s := defaultAgentDeployServiceFactory(nil); s == nil {
		t.Fatal("nil lister produced nil service")
	}
	if s := defaultAgentDeployServiceFactory(stubSDKLister{}); s == nil {
		t.Fatal("non-nil lister produced nil service")
	}
}

func TestOrchestratorAgentListerAdapter_Forwards(t *testing.T) {
	called := false
	adapter := &orchestratorAgentListerAdapter{c: stubOrchestratorLister{fn: func() {
		called = true
	}}}
	_, err := adapter.ListAgents(context.Background(), &orchestratorSDK.ListAgentsRequest{TenantID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected forward")
	}
}

// stubOrchestratorLister satisfies orchestratorListerAPI for the
// adapter test above.
type stubOrchestratorLister struct {
	fn func()
}

func (s stubOrchestratorLister) ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
	s.fn()
	return &orchestratorSDK.ListAgentsResponse{}, nil
}

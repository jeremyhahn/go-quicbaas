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

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// FleetDeployBinding exposes the agent-deploy workflow to the Wails
// frontend. It delegates every Deploy call to the service layer built
// on top of pkg/fleet/deploy primitives, with an SDK-backed orchestrator
// client wired in for the "wait for online" poll.
//
// The binding is deliberately thin: flag-to-spec translation and result
// formatting happen on the frontend. This keeps the Go IPC surface
// narrow and consistent with the CLI subcommand.
type FleetDeployBinding struct {
	logger     *logger.Logger
	conn       *ConnectionBinding
	ctx        context.Context
	newService func(lister service.FleetNodeLister) service.FleetDeployService
}

// NewFleetDeployBinding creates a new FleetDeployBinding that shares
// the SDK client managed by ConnectionBinding.
func NewFleetDeployBinding(log *logger.Logger, conn *ConnectionBinding) *FleetDeployBinding {
	return &FleetDeployBinding{
		logger:     log,
		conn:       conn,
		newService: defaultFleetDeployServiceFactory,
	}
}

// startup stores the Wails context for downstream deploy calls.
func (b *FleetDeployBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is a no-op — deploys are one-shot and don't hold resources.
func (b *FleetDeployBinding) shutdown(ctx context.Context) {}

// Deploy runs a full SSH → SFTP → systemd/nohup → wait-online workflow
// described by spec. The returned *deploy.Result is marshalled by Wails
// into the TypeScript shape the frontend renders.
func (b *FleetDeployBinding) Deploy(spec *deploy.Spec) (*deploy.Result, error) {
	if spec == nil {
		return nil, NewGUIRequestRequiredError("FleetDeployBinding.Deploy")
	}

	svc := b.newService(b.loadFleetNodeLister())

	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = deployContext(ctx, spec)

	result, err := svc.Deploy(ctx, spec)
	if err != nil {
		return nil, NewGUITransportError("FleetDeployBinding.Deploy", err)
	}
	return result, nil
}

// loadFleetNodeLister returns a fleet registry lister backed by the
// currently-connected SDK client. When no client is connected we
// surface nil — the service skips its wait-online phase cleanly.
func (b *FleetDeployBinding) loadFleetNodeLister() service.FleetNodeLister {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil
	}
	return &orchestratorFleetNodeListerAdapter{c: client.Orchestrator()}
}

// orchestratorFleetNodeListerAdapter narrows the orchestrator sub-client to
// the one method the deploy service needs.
type orchestratorFleetNodeListerAdapter struct {
	c orchestratorListerAPI
}

// orchestratorListerAPI captures the one method consumed from the SDK
// orchestrator client. Declaring it here makes the binding testable
// without pulling the whole SDK surface into the fake.
type orchestratorListerAPI interface {
	ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error)
}

// ListFleetNodes forwards to the underlying SDK sub-client.
func (a *orchestratorFleetNodeListerAdapter) ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
	return a.c.ListFleetNodes(ctx, req)
}

// defaultFleetDeployServiceFactory is the production wiring used when
// the binding is constructed for real. Free functions let tests swap
// the factory without reflecting on an unexported field.
func defaultFleetDeployServiceFactory(lister service.FleetNodeLister) service.FleetDeployService {
	deps := service.FleetDeployServiceDeps{}
	if lister != nil {
		deps.FleetNodeLister = lister
	}
	return service.NewFleetDeployService(deps)
}

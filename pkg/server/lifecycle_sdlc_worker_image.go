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

package server

import (
	"context"
	"log/slog"
	"os"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// =============================================================================
// The worker image refresh
// =============================================================================
//
// THE BUILD RUNS ON THE DAEMON'S HOST, through the daemon's own container
// engine (Server.orchContainerRuntime, the same ExecContainerRuntime the
// orchestration service spawns sandboxes with), and not behind the agent RPC.
// Three facts decide it:
//
//   - Worker images are not built anywhere else today: `make
//     build-worker-image` runs on the node, and no agent RPC builds or pulls
//     on the control plane's behalf (the agent pulls only its own image map,
//     at its own start).
//   - The gate's smoke is an orchestrator session on a tenant's pooled
//     credential, which only the control plane can spawn; building elsewhere
//     would split one pass across two hosts with no shared engine.
//   - Distribution to the rest of the fleet is the REGISTRY's job, not a
//     build's: with sdlc.worker_image.registry set, the promoted image is
//     pushed and bound by digest, and every agent running with --auto-pull
//     fetches it before its first spawn of it (pkg/orchestration/fleet, Runtime.ensureImage).

// sdlcWorkerImageLoop is the running refresh loop and the means to stop it.
type sdlcWorkerImageLoop struct {
	stop context.CancelFunc
	done chan struct{}
}

// startSDLCWorkerImage starts the refresh loop when this node can run it.
//
// A NODE THAT CANNOT RUN IT SAYS WHY AND CARRIES ON. The refresh is upkeep,
// not a precondition of the module: a node without an engine still serves
// every binding it has. What it must never do is fail QUIETLY, so each reason
// is logged at the level it deserves — a broken build at error, a deliberate
// opt-out at info.
func (s *Server) startSDLCWorkerImage(bindings *service.SDLCService, sessions orchestration.AgentService, projects sdlc.ProjectReader) error {
	cfg := s.config.SDLC.WorkerImage
	if !cfg.Enabled {
		s.logger.Info("sdlc: worker image refresh disabled (sdlc.worker_image.enabled=false)")
		return nil
	}
	if s.orchContainerRuntime == nil {
		s.logger.Warn("sdlc: worker image refresh not started: this node has no container engine (docker or podman) to build on")
		return nil
	}
	tenants, ok := s.sdlcBackstopTenants()
	if !ok {
		return nil
	}
	pins, err := softwarefactoryworker.ToolchainPins()
	if err != nil {
		s.logger.WithError(err).Error("sdlc: worker image refresh not started; bound images will not be kept current")
		return nil
	}
	catalog, err := s.agentCatalogService()
	if err != nil {
		return &SDLCInitError{Component: "worker image refresh", Cause: err}
	}
	hosts := newLocalFleetNodeHosts(s.fleetRegistry)
	refresher, err := newSDLCWorkerImageRefresher(cfg, pins, workerimage.Deps{
		Engine:   s.orchContainerRuntime,
		Tenants:  tenants,
		Bindings: bindings,
		Agents:   catalog,
		Auditor:  &workerImageAuditor{resolve: s.auditLoggerForStores},
		Hosts:    hosts,
		Fleet:    hosts,
		Observer: workerimage.NewPrometheusObserver(),
		Logger:   slog.Default(),
	}, sessions, projects, s.workerImageSecrets())
	if err != nil {
		return &SDLCInitError{Component: "worker image refresh", Cause: err}
	}
	bindings.SetWorkerImageRefresher(refresher)

	ctx, stop := context.WithCancel(context.Background())
	loop := &sdlcWorkerImageLoop{stop: stop, done: make(chan struct{})}
	go func() {
		defer close(loop.done)
		refresher.Run(ctx, workerimage.Schedule{
			RefreshInterval: cfg.RefreshInterval,
			PassTimeout:     cfg.PassTimeout,
		})
	}()
	s.sdlcWorkerImage = loop
	mode := "local (bindings get the local tag)"
	if cfg.Registry != "" {
		mode = "registry " + cfg.Registry
	}
	s.logger.Infof("sdlc: worker image refresh started (repository=%s mode=%s refresh=%s smoke_model=%s)",
		cfg.Repository, mode, cfg.RefreshInterval, cfg.SmokeModel)
	return nil
}

// workerImageSecrets is the tenant secret reader the push credential is read
// through, or nil on a node with no secrets service — which refuses a
// configured credential at startup rather than at the first push.
func (s *Server) workerImageSecrets() orchestration.AccountSecretResolver {
	if s.quicBaaSService == nil || s.quicBaaSService.Secrets() == nil {
		return nil
	}
	return service.NewAccountSecretResolver(s.quicBaaSService.Secrets())
}

// newSDLCWorkerImageRefresher assembles the refresher from configuration: the
// registry client, the push credential, the smoker and this node's identity.
// deps carries every collaborator the configuration does not decide; secrets
// is what a configured push credential is read through.
func newSDLCWorkerImageRefresher(cfg config.SDLCWorkerImage, pins softwarefactoryworker.Pins, deps workerimage.Deps,
	sessions orchestration.AgentService, projects sdlc.ProjectReader,
	secrets orchestration.AccountSecretResolver) (*workerimage.Refresher, error) {
	registry, err := workerimage.NewNPMRegistry(cfg.NPMRegistryURL, nil)
	if err != nil {
		return nil, err
	}
	deps.Versions = registry
	smoker, err := workerimage.NewSessionSmoker(sessions, projects)
	if err != nil {
		return nil, err
	}
	deps.Smoker = smoker

	// The credential is a tenant secret, read only when a pass pushes. None
	// configured pushes with the engine's own login.
	var credential workerimage.CredentialSource
	if cfg.RegistryUsername != "" || cfg.RegistryTokenSecret != "" || cfg.RegistryTokenTenant != "" {
		secretCredential, credErr := workerimage.NewSecretCredential(secrets,
			cfg.RegistryTokenTenant, cfg.RegistryTokenSecret, cfg.RegistryUsername)
		if credErr != nil {
			return nil, credErr
		}
		credential = secretCredential
	}
	return workerimage.NewRefresher(workerimage.Config{
		Repository: cfg.Repository,
		Registry:   cfg.Registry,
		Credential: credential,
		SmokeModel: cfg.SmokeModel,
		SmokeHost:  cfg.SmokeHost,
		Base:       version.Version,
		Pins:       pins,
		// The ids the sandboxed spawn runs as, which is this process's own.
		UID: os.Getuid(),
		GID: os.Getgid(),
	}, deps)
}

// stopSDLCWorkerImage ends the loop and waits for it. A pass in flight is
// cancelled; a pass cancelled before promotion changes no binding.
func (s *Server) stopSDLCWorkerImage() {
	if s.sdlcWorkerImage == nil {
		return
	}
	s.sdlcWorkerImage.stop()
	<-s.sdlcWorkerImage.done
	s.sdlcWorkerImage = nil
}

// workerImageAuditor puts the refresh's events on the platform audit trail.
// Like the orchestration store auditor it resolves the logger per event,
// because the refresh can start before IAM publishes one.
type workerImageAuditor struct {
	resolve func() iam.AuditLogger
}

var _ workerimage.Auditor = (*workerImageAuditor)(nil)

// auditResourceTypes maps each refresh action to the resource it acts on.
var auditResourceTypes = map[string]string{
	workerimage.AuditActionRefresh: "sdlc_worker_image",
	workerimage.AuditActionPromote: "sdlc_binding",
}

// RecordWorkerImageEvent implements workerimage.Auditor.
func (a *workerImageAuditor) RecordWorkerImageEvent(_ context.Context, event workerimage.AuditEvent) {
	logger := a.resolve()
	if logger == nil {
		return
	}
	tenantID := event.TenantID
	if tenantID == "" {
		tenantID = iam.SystemTenantID
	}
	outcome := iam.AuditOutcomeSuccess
	if !event.Success {
		outcome = iam.AuditOutcomeFailure
	}
	details := make(map[string]interface{}, len(event.Details))
	for k, v := range event.Details {
		if v != "" {
			details[k] = v
		}
	}
	logger.LogAsync(&iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategoryAdministration,
		TenantID:  tenantID,
		Action:    event.Action,
		Outcome:   outcome,
		Resource:  &iam.AuditResource{Type: auditResourceTypes[event.Action], ID: event.Resource},
		Details:   details,
	})
}

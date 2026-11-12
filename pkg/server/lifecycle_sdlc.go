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

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// =============================================================================
// SDLC module — the host
// =============================================================================
//
// The SDLC module of the Software Factory owns its own JOB SHAPE and nothing
// else. This file mounts it into the engine's seams: the forge_issue
// TaskSource the poller sweeps, the forge_review validator the reviewing
// phase asks, and the sdlc JobRunner that drives an issue-driven job's states
// — issue, branch, work, verify, publish, review, merge, cleanup. It also
// serves the module's own surface: the binding service the transports call
// and the webhook ingress.
//
// THE ENGINE NEVER IMPORTS THIS MODULE. Registration goes the other way: the
// host registers a forwarder into orchestration's registries, so
// pkg/orchestration knows a runner named "sdlc" and nothing about pkg/sdlc.
//
// THE SEAM REGISTRIES ARE PROCESS-GLOBAL AND APPEND-ONLY, while a server
// starts and stops (a restart in one process is exactly what the e2e rig
// does). So what is registered — once — is a thin forwarder that reads the
// LIVE runtime through an atomic pointer; a stopped module answers every
// seam with sdlc.ErrUnavailable rather than acting on a dead store.

// ErrSDLCUnavailable reports the module being asked for before it is built,
// or on a node where it is disabled. Never a silent no-op: a webhook route
// answering 200 while the module behind it does not exist is the failure
// mode this subsystem is built to avoid.
var ErrSDLCUnavailable = sdlc.ErrUnavailable

// sdlcRuntime is everything the module needs to serve one node, constructed
// once at boot. One value means one nil check decides availability.
type sdlcRuntime struct {
	store    *sdlc.Store
	routes   *sdlc.WebhookRouteIndex
	service  *service.SDLCService
	source   *sdlc.IssueSource
	review   *sdlc.ReviewValidator
	hooks    *sdlc.Hooks
	runner   *sdlc.Runner
	reviewer *sdlc.ReviewRunner
	ingestor *sdlc.WebhookIngestor
	webhook  *sdlc.WebhookHTTPHandler

	// triage is the front half of the loop: which issues are ready to be
	// worked. It is a third JobRunner beside workflow and sdlc, and
	// triageJobs is what a verified delivery nudges to start one.
	triage     *sdlc.TriageRunner
	triageJobs *sdlc.TriageScheduler

	// preflight runs an onboarding's verification commands once in the
	// worker's image before autonomy is switched on.
	preflight *sdlc.PreflightRunner

	// reviewFloor is the back half's backstop: the pass that finds the open
	// pull requests which are green and unjudged and asks the dispatcher for
	// each, so a review does not depend on a delivery having arrived.
	reviewFloor *sdlc.ReviewSweeper

	// train lands approved changes in batches: a fourth JobRunner, one job
	// per project at a time, and trainJobs is what a queued change nudges to
	// start one.
	train     *sdlc.TrainRunner
	trainJobs *sdlc.TrainScheduler
}

// sdlcModule is the process-wide registration: registered once, pointed at
// whichever runtime is live.
var sdlcModule struct {
	once sync.Once
	err  error
	live atomic.Pointer[sdlcRuntime]
}

// liveSDLC returns the running module or nil.
func liveSDLC() *sdlcRuntime {
	return sdlcModule.live.Load()
}

// registerSDLCModule registers the forwarders into the engine's registries
// exactly once per process.
func registerSDLCModule() error {
	sdlcModule.once.Do(func() {
		if err := orchestration.RegisterTaskSource(sdlc.TaskSourceKindForgeIssue, sdlcSourceForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "task source registration", Cause: err}
			return
		}
		if err := orchestration.RegisterValidator(sdlc.ValidatorKindForgeReview, sdlcReviewForwarder); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "validator registration", Cause: err}
			return
		}
		// container_command needs nothing from the running module: it runs
		// the criterion's command through the job's own container, so it is
		// registered directly rather than forwarded through liveSDLC.
		if err := orchestration.RegisterValidator(sdlc.ValidatorKindContainerCommand, sdlc.ValidateInContainer); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "validator registration", Cause: err}
			return
		}
		if err := orchestration.RegisterJobRunner(sdlcRunnerForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "job runner registration", Cause: err}
			return
		}
		if err := orchestration.RegisterJobRunner(sdlcReviewRunnerForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "review runner registration", Cause: err}
			return
		}
		if err := orchestration.RegisterJobRunner(sdlcTriageForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "triage runner registration", Cause: err}
			return
		}
		if err := orchestration.RegisterJobRunner(sdlcPreflightForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "preflight runner registration", Cause: err}
			return
		}
		if err := orchestration.RegisterJobRunner(sdlcTrainForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "merge train runner registration", Cause: err}
			return
		}
		if err := orchestration.RegisterTaskConclusionObserver(sdlc.JobRunnerName, sdlcConclusionForwarder{}); err != nil {
			sdlcModule.err = &SDLCInitError{Component: "task conclusion observer registration", Cause: err}
		}
	})
	return sdlcModule.err
}

// sdlcConclusionForwarder is the registered task-conclusion observer: the
// moment a Task the module adopted stops, whatever stopped it.
//
// It is a SEPARATE seam from the lifecycle moments because a conclusion is not
// a job's: the attempt budget is spent after the run has returned, and a
// reaped job's task is concluded on a node whose run never existed. A stopped
// module answers unavailable rather than acting on a dead store, exactly as
// the other forwarders do.
type sdlcConclusionForwarder struct{}

var _ orchestration.TaskConclusionObserver = sdlcConclusionForwarder{}

// TaskConcluded implements orchestration.TaskConclusionObserver.
func (sdlcConclusionForwarder) TaskConcluded(ctx context.Context, conclusion orchestration.TaskConclusion) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.hooks.TaskConcluded(ctx, conclusion)
}

// sdlcSourceForwarder is the registered forge_issue TaskSource.
type sdlcSourceForwarder struct{}

// Name implements orchestration.TaskSource.
func (sdlcSourceForwarder) Name() string { return sdlc.TaskSourceName }

// Poll implements orchestration.TaskSource. A stopped module is an outage
// the poller reports once, not a sweep error on every tick.
func (sdlcSourceForwarder) Poll(ctx context.Context, project *orchestration.Project) ([]orchestration.TaskSourceSpec, error) {
	rt := liveSDLC()
	if rt == nil {
		return nil, &orchestration.TaskSourceUnavailableError{Source: sdlc.TaskSourceName, Cause: ErrSDLCUnavailable}
	}
	return rt.source.Poll(ctx, project)
}

// sdlcReviewForwarder is the registered forge_review ValidatorFunc.
func sdlcReviewForwarder(ctx context.Context, job *orchestration.JobContext, criterion orchestration.ReviewCriterion) (orchestration.ValidationResult, error) {
	rt := liveSDLC()
	if rt == nil {
		return orchestration.ValidationResult{Name: criterion.Name, Passed: false}, ErrSDLCUnavailable
	}
	return rt.review.Validate(ctx, job, criterion)
}

// sdlcRunnerForwarder is the registered sdlc JobRunner.
//
// Its states, its refusals and its resume point are STATIC — they are the
// module's shape, not its runtime — so the engine knows them from the moment
// the process registers, which is what the stall detector's thresholds and
// the UI's stepper read. Only Run needs the live module, and a stopped module
// refuses to drive a job rather than half-driving one.
type sdlcRunnerForwarder struct{}

var (
	_ orchestration.JobRunner  = sdlcRunnerForwarder{}
	_ orchestration.JobResumer = sdlcRunnerForwarder{}
)

// Name implements orchestration.JobRunner.
func (sdlcRunnerForwarder) Name() string { return sdlc.JobRunnerName }

// States implements orchestration.JobRunner.
func (sdlcRunnerForwarder) States() []orchestration.JobStateSpec { return sdlc.RunnerStates() }

// Validate implements orchestration.JobRunner.
func (sdlcRunnerForwarder) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	if liveSDLC() == nil {
		return ErrSDLCUnavailable
	}
	return sdlc.ValidateRunnerConfig(cfg)
}

// ResumeState implements orchestration.JobResumer.
func (sdlcRunnerForwarder) ResumeState(interrupted orchestration.JobState) orchestration.JobState {
	return sdlc.RunnerResumeState(interrupted)
}

// Run implements orchestration.JobRunner.
func (sdlcRunnerForwarder) Run(ctx context.Context, job *orchestration.JobRun) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.runner.Run(ctx, job)
}

// sdlcTriageForwarder is the registered triage JobRunner: the front half of
// the loop, which decides which issues are ready to be worked and marks
// them.
//
// Like the delivery forwarder, its states, refusals and LEASE POLICY are
// static — they are the module's shape, not its runtime — so the engine
// knows them from the moment the process registers. The lease policy
// especially: it is read when a job takes its lease, which can happen before
// this node's module finished starting, and a triage job that took an
// ON-BUDGET lease because the policy was not yet readable would spend a
// worker slot for its whole run.
type sdlcTriageForwarder struct{}

var (
	_ orchestration.JobRunner      = sdlcTriageForwarder{}
	_ orchestration.JobLeasePolicy = sdlcTriageForwarder{}
)

// Name implements orchestration.JobRunner.
func (sdlcTriageForwarder) Name() string { return sdlc.TriageRunnerName }

// States implements orchestration.JobRunner.
func (sdlcTriageForwarder) States() []orchestration.JobStateSpec { return sdlc.TriageRunnerStates() }

// Validate implements orchestration.JobRunner.
func (sdlcTriageForwarder) Validate(ctx context.Context, cfg *orchestration.JobConfig) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.triage.Validate(ctx, cfg)
}

// LeaseSubject implements orchestration.JobLeasePolicy: one triage job per
// project at a time.
func (sdlcTriageForwarder) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return sdlc.TriageLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobLeasePolicy: triage does not
// spend the project's worker concurrency budget.
func (sdlcTriageForwarder) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (sdlcTriageForwarder) Run(ctx context.Context, job *orchestration.JobRun) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.triage.Run(ctx, job)
}

// sdlcPreflightForwarder is the registered preflight JobRunner: an
// onboarding's one run of the verification commands before autonomy. Its
// states and lease policy are static for the reason sdlcTriageForwarder's are.
type sdlcPreflightForwarder struct{}

var (
	_ orchestration.JobRunner      = sdlcPreflightForwarder{}
	_ orchestration.JobLeasePolicy = sdlcPreflightForwarder{}
)

// Name implements orchestration.JobRunner.
func (sdlcPreflightForwarder) Name() string { return sdlc.PreflightRunnerName }

// States implements orchestration.JobRunner.
func (sdlcPreflightForwarder) States() []orchestration.JobStateSpec {
	return sdlc.PreflightRunnerStates()
}

// Validate implements orchestration.JobRunner.
func (sdlcPreflightForwarder) Validate(ctx context.Context, cfg *orchestration.JobConfig) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.preflight.Validate(ctx, cfg)
}

// LeaseSubject implements orchestration.JobLeasePolicy: one preflight per
// project at a time.
func (sdlcPreflightForwarder) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return sdlc.PreflightLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobLeasePolicy: the preflight is
// not delivery work and does not spend the project's worker budget.
func (sdlcPreflightForwarder) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (sdlcPreflightForwarder) Run(ctx context.Context, job *orchestration.JobRun) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.preflight.Run(ctx, job)
}

// sdlcTrainForwarder is the registered merge-train JobRunner.
//
// Like the triage forwarder, its states, refusals, resume table and LEASE
// POLICY are static: the lease policy is read when a job takes its lease, and
// a train that took an on-budget lease because the module had not started
// would hold a worker slot through a pipeline it does not run.
type sdlcTrainForwarder struct{}

var (
	_ orchestration.JobRunner         = sdlcTrainForwarder{}
	_ orchestration.JobResumer        = sdlcTrainForwarder{}
	_ orchestration.JobLeasePolicy    = sdlcTrainForwarder{}
	_ orchestration.StartedWorkRunner = sdlcTrainForwarder{}
)

// Name implements orchestration.JobRunner.
func (sdlcTrainForwarder) Name() string { return sdlc.TrainRunnerName }

// States implements orchestration.JobRunner.
func (sdlcTrainForwarder) States() []orchestration.JobStateSpec { return sdlc.TrainRunnerStates() }

// Validate implements orchestration.JobRunner.
func (sdlcTrainForwarder) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	if liveSDLC() == nil {
		return ErrSDLCUnavailable
	}
	return sdlc.ValidateTrainConfig(cfg)
}

// ResumeState implements orchestration.JobResumer.
func (sdlcTrainForwarder) ResumeState(interrupted orchestration.JobState) orchestration.JobState {
	return sdlc.TrainResumeState(interrupted)
}

// LeaseSubject implements orchestration.JobLeasePolicy: one train per project.
func (sdlcTrainForwarder) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return sdlc.TrainLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobLeasePolicy.
func (sdlcTrainForwarder) OffProjectBudget() bool { return true }

// FinishesStartedWork implements orchestration.StartedWorkRunner: a train
// lands pull requests the project's started tasks already opened and had
// approved, so a parked project still runs it. Declared on the forwarder for
// the reason the lease policy is: the engine asks the REGISTERED runner.
func (sdlcTrainForwarder) FinishesStartedWork() bool { return true }

// Run implements orchestration.JobRunner.
func (sdlcTrainForwarder) Run(ctx context.Context, job *orchestration.JobRun) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.train.Run(ctx, job)
}

// sdlcReviewRunnerForwarder is the registered review JobRunner.
//
// Same shape as sdlcRunnerForwarder and for the same reason: the states are
// the module's shape and are known from registration, so the stall detector
// and the UI can read them before any runtime exists; only Run needs the live
// module.
type sdlcReviewRunnerForwarder struct{}

var (
	_ orchestration.JobRunner         = sdlcReviewRunnerForwarder{}
	_ orchestration.JobResumer        = sdlcReviewRunnerForwarder{}
	_ orchestration.JobLeasePolicy    = sdlcReviewRunnerForwarder{}
	_ orchestration.StartedWorkRunner = sdlcReviewRunnerForwarder{}
)

// Name implements orchestration.JobRunner.
func (sdlcReviewRunnerForwarder) Name() string { return sdlc.ReviewJobRunnerName }

// States implements orchestration.JobRunner.
func (sdlcReviewRunnerForwarder) States() []orchestration.JobStateSpec {
	return sdlc.ReviewRunnerStates()
}

// Validate implements orchestration.JobRunner.
func (sdlcReviewRunnerForwarder) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	if liveSDLC() == nil {
		return ErrSDLCUnavailable
	}
	return sdlc.ValidateReviewJobConfig(cfg)
}

// ResumeState implements orchestration.JobResumer.
func (sdlcReviewRunnerForwarder) ResumeState(orchestration.JobState) orchestration.JobState {
	return orchestration.JobStateInitializing
}

// LeaseSubject implements orchestration.JobLeasePolicy, serializing on the pull
// request under review so two reviews of the same head cannot run at once.
//
// IT MUST MATCH ReviewRunner's, and it is repeated here rather than delegated
// because runnerOffProjectBudget and leaseSubjectFor type-assert the REGISTERED
// runner, which is this forwarder — the live ReviewRunner behind it is never
// asked. That is the whole defect below.
func (sdlcReviewRunnerForwarder) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	subject, err := sdlc.ReviewJobSubjectFrom(cfg)
	if err != nil {
		return ""
	}
	return sdlc.ReviewLeaseSubject(strings.TrimSpace(cfg.ProjectID), subject.PullRequest)
}

// OffProjectBudget implements orchestration.JobLeasePolicy: a review does not
// spend the project's worker concurrency budget.
//
// THIS FORWARDER DID NOT IMPLEMENT JobLeasePolicy AT ALL, and that was a
// deadlock (go-quicbaas#938). ReviewRunner has declared OffProjectBudget since
// it was written, but runnerOffProjectBudget asserts the policy on the runner
// the engine REGISTERED — this forwarder — so every review lease counted
// against MaxConcurrency. An sdlc job parked waiting for a verdict holds a
// slot, so on a project at its limit the review that would release it was
// refused with "project concurrency exceeded", and neither side could move.
// Measured on the burn-in node 2026-09-13 at MaxConcurrency 1:
//
//	job lease: project concurrency exceeded: live=1 limit=1
//
// The triage forwarder beside this one had both methods from the start, which
// is why triage kept running while review could not.
func (sdlcReviewRunnerForwarder) OffProjectBudget() bool { return true }

// FinishesStartedWork implements orchestration.StartedWorkRunner: a review
// judges a pull request that is already open, which is work in flight, so a
// parked project still reviews it. A delivery waiting on its verdict would
// otherwise never finish.
func (sdlcReviewRunnerForwarder) FinishesStartedWork() bool { return true }

// Run implements orchestration.JobRunner.
func (sdlcReviewRunnerForwarder) Run(ctx context.Context, job *orchestration.JobRun) error {
	rt := liveSDLC()
	if rt == nil {
		return ErrSDLCUnavailable
	}
	return rt.reviewer.Run(ctx, job)
}

// startSDLC constructs the module and mounts it into the engine.
//
// FAIL-CLOSED THROUGHOUT. Every construction error refuses the boot rather
// than leaving the node running with a partial module: a daemon that
// answers its webhook route and drops deliveries looks like a working
// module with a quiet forge, and is diagnosed in days instead of seconds.
func (s *Server) startSDLC() error {
	cfg := s.config.SDLC

	node := s.getQRDBNode()
	if node == nil {
		return &SDLCInitError{Component: "qrdb node", Cause: ErrSDLCUnavailable}
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		// The resolver is what confines each tenant's records to its own
		// keyspace; without it every tenant's bindings would share one store.
		return &SDLCInitError{Component: "tenant store resolver", Cause: ErrSDLCUnavailable}
	}
	store, err := sdlc.NewStore(s.liveTenantStoreResolver())
	if err != nil {
		return &SDLCInitError{Component: "store", Cause: err}
	}

	if s.qrdbClient == nil {
		return &SDLCInitError{Component: "qrdb client", Cause: ErrSDLCUnavailable}
	}
	shard, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return &SDLCInitError{Component: "platform shard", Cause: err}
	}
	transactor, err := s.sdlcTransactor(shard)
	if err != nil {
		return &SDLCInitError{Component: "route index arbiter", Cause: err}
	}
	routes, err := sdlc.NewWebhookRouteIndex(transactor, shard.KV())
	if err != nil {
		return &SDLCInitError{Component: "webhook route index", Cause: err}
	}

	guard, err := sdlc.NewRepoHostGuard(cfg.RepoHosts, cfg.AllowLocalRepos)
	if err != nil {
		return &SDLCInitError{Component: "repository host guard", Cause: err}
	}
	verification, err := s.executionGuard()
	if err != nil {
		return &SDLCInitError{Component: "verification command allowlist", Cause: err}
	}
	if s.quicBaaSService == nil {
		return &SDLCInitError{Component: "service facade", Cause: ErrSDLCUnavailable}
	}
	secrets := s.quicBaaSService.Secrets()
	if secrets == nil {
		return &SDLCInitError{Component: "secrets service", Cause: ErrSDLCUnavailable}
	}
	agents := s.quicBaaSService.Agent()
	if agents == nil {
		return &SDLCInitError{Component: "orchestration service", Cause: ErrSDLCUnavailable}
	}
	projectSvc := s.quicBaaSService.Project()
	if projectSvc == nil {
		return &SDLCInitError{Component: "project service", Cause: ErrSDLCUnavailable}
	}
	projectStores, ok := projectSvc.(interface {
		Store() orchestration.ProjectStore
	})
	if !ok || projectStores.Store() == nil {
		return &SDLCInitError{Component: "project store", Cause: ErrSDLCUnavailable}
	}
	if s.gitProvisioner == nil {
		return &SDLCInitError{Component: "git provisioner", Cause: ErrSDLCUnavailable}
	}
	// Every slot a binding holds names an Agent or pool of this catalog.
	if s.orchAgentDirectory == nil {
		return &SDLCInitError{Component: "agent catalog", Cause: ErrSDLCUnavailable}
	}
	agentCatalog := s.orchAgentDirectory
	// Bindings written before Agents existed are migrated once, here, before
	// anything reads one: a legacy binding decodes with its slots empty.
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
	s.migrateBindingAgents(migrateCtx, store)
	migrateCancel()

	// The attention inbox's barrier_sealed reader, built from the SAME
	// broker-side TenantBarrierServicer facade every transport shares (see
	// pkg/server/tenant_barrier_wiring.go). nil before the embedded qrdb
	// node's tenant service is reachable — an honest "not yet available",
	// not a reason to refuse the module (same pattern as Accounts below).
	var barrierReader sdlc.AttentionBarrierReader
	if barrierSvc := s.buildTenantBarrierService(); barrierSvc != nil {
		barrierReader = newTenantBarrierAttentionReader(barrierSvc)
	}

	// The tenant SSH key store, read by OnboardRepo to check a repository's
	// signing key exists. Assigned only when present, so a node with no key
	// store passes a nil interface rather than a typed nil.
	var sshKeys service.SSHKeyGetter
	if sshClient := s.quicBaaSService.SSHClient(); sshClient != nil {
		sshKeys = service.NewSSHService(sshClient)
	}

	rulers := sdlc.RulerPolicy{Default: cfg.Rulers.Default, Tenants: cfg.Rulers.Tenants}
	forgeTokens, err := newSDLCForgeTokenSources(cfg.Forge.TokenCommands)
	if err != nil {
		return &SDLCInitError{Component: "forge token commands", Cause: err}
	}
	sdlcService, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Projects: projectSvc, ProjectWriter: projectStores.Store(),
		Secrets: secrets, Guard: guard, Verification: verification, Tasks: agents,
		// s.orchAccountStore is nil on a node with no secretsManager wired
		// (no pooled accounts configured at all) — an honest "no accounts",
		// not a reason to refuse the attention inbox's account_error alert.
		Accounts: s.orchAccountStore,
		Barrier:  barrierReader,
		// The node's ruler policy, refused at startup when malformed, and the
		// audit sink every operator issue filing is recorded on.
		Rulers:  rulers,
		Auditor: newSDLCIssueAuditor(s.auditLoggerForStores),
		Agents:  agentCatalog,
		SSHKeys: sshKeys,
		// The node's public origin, which onboarding provisions forge
		// webhooks to point at. Validated at load; refused here too, because
		// a module that reached this line without one would bind projects
		// nothing ever delivers to.
		WebhookPublicURL: cfg.Webhook.PublicURL,
		ForgeTransport: sdlc.ForgeTransport{
			ReadTimeout:       cfg.Forge.ReadTimeout,
			WriteTimeout:      cfg.Forge.WriteTimeout,
			HeavyWriteTimeout: cfg.Forge.HeavyWriteTimeout,
			ReadAttempts:      cfg.Forge.ReadAttempts,
			RetryBackoff:      cfg.Forge.RetryBackoff,
		},
		// The node's forge token commands; a forge type without one reads
		// each binding's token secret.
		ForgeTokens: forgeTokens,
	})
	if err != nil {
		return &SDLCInitError{Component: "binding service", Cause: err}
	}
	sdlcService.SetProjectNudger(s)
	agentCatalog.RegisterUsageReporter(sdlcService)

	source, err := sdlc.NewIssueSource(sdlcService, sdlcService, store, cfg.IssuePageLimit)
	if err != nil {
		return &SDLCInitError{Component: "issue source", Cause: err}
	}
	// Contracts are rendered from each project's resolved sdlc set, the same
	// chain a job resolves at launch.
	promptResolver, err := s.promptResolver()
	if err != nil {
		return &SDLCInitError{Component: "issue source prompts", Cause: err}
	}
	source.SetPromptResolver(promptResolver)
	// THE OWNERSHIP PREDICATE'S EVIDENCE, from the engine's job leases. A lease
	// already names the task's source reference — the issue reference this
	// source emits — so "is a live session already working this issue" is a read
	// of records that exist rather than a new index to keep.
	//
	// FLEET-WIDE, which is where it earns its keep: the task store's guarded
	// source index already refuses a second task for one issue WITHIN a project,
	// so the case only this catches is two projects bound to one repository.
	//
	// Reported and not substituted if the lease store is absent: the source
	// still polls, with one predicate weaker. See SetLiveWorkReader.
	if s.orchJobLeaseStore != nil && s.orchLeaseTTL > 0 {
		live, liveErr := sdlc.NewLeaseWorkReader(s.orchJobLeaseStore, s.orchLeaseTTL)
		if liveErr != nil {
			return &SDLCInitError{Component: "live work reader", Cause: liveErr}
		}
		source.SetLiveWorkReader(live)
	} else {
		s.logger.Warn("SDLC issue source: no job lease store; readiness cannot tell whether another project's live session already owns an issue")
	}
	// Issues skipped because a pull request already closes them land on the
	// platform audit trail with that pull request's number (go-quicbaas#1054).
	source.SetAdoptionAuditor(newSDLCAdoptionAuditor(s.auditLoggerForStores))
	review, err := sdlc.NewReviewValidator(store, sdlcService, sdlcService, agentCatalog)
	if err != nil {
		return &SDLCInitError{Component: "review validator", Cause: err}
	}
	signing, err := service.NewSDLCSigningAgent(s.gitProvisioner)
	if err != nil {
		return &SDLCInitError{Component: "signing agent", Cause: err}
	}
	// The merge train's scheduler is built before the hooks, because a change
	// the review approves is queued by the hooks and the queue wakes a train.
	trainJobs, err := sdlc.NewTrainScheduler(sdlc.TrainSchedulerConfig{Bindings: sdlcService, Trains: store, Jobs: agents})
	if err != nil {
		return &SDLCInitError{Component: "merge train scheduler", Cause: err}
	}
	hooks, err := sdlc.NewHooks(sdlc.HooksConfig{
		Projects: projectStores.Store(), Bindings: sdlcService, Deliveries: store, Tasks: agents,
		Forges:        sdlcService,
		MergeAttempts: cfg.MergeAttempts,
		Trains:        store,
		TrainNudger:   trainJobs,
		Surfacing: sdlc.SurfacePolicy{
			RetryAttempts: cfg.Surfacing.RetryAttempts,
			RetryInterval: cfg.Surfacing.RetryInterval,
			PostTimeout:   cfg.Surfacing.PostTimeout,
		},
	})
	if err != nil {
		return &SDLCInitError{Component: "lifecycle hooks", Cause: err}
	}
	// The sockets root is where every attempt's signing agent binds, and
	// NewRunner creates it 0700 and refuses a root no socket could be bound
	// under. That refusal is a STARTUP failure on purpose: the arithmetic is
	// fixed, so a root that cannot work never works, and discovering it at
	// the first delivery would be one failed job per issue on a deployment
	// that looks configured.
	//
	// Preparing it also sweeps the session directories a process that was
	// KILLED mid-attempt left behind. The root outlives the daemon, so
	// without the sweep every hard kill costs it one dead socket for ever
	// (go-quicbaas#633); the daemon is the only thing that knows the root,
	// and startup is the only moment nothing of its own is bound under it.
	socketsRoot, socketsErr := s.socketsRoot()
	if socketsErr != nil {
		return &SDLCInitError{Component: "sockets root", Cause: socketsErr}
	}
	runner, err := sdlc.NewRunner(sdlc.RunnerConfig{
		Hooks: hooks, Tokens: sdlcService, Signing: signing, Guard: guard, CheckoutRoot: cfg.CheckoutRoot,
		SocketsRoot: socketsRoot,
	})
	if err != nil {
		return &SDLCInitError{Component: "job runner", Cause: err}
	}
	if swept := runner.SocketSweep(); len(swept.Removed) > 0 {
		s.logger.Infof("sdlc: sockets root %s swept: %d orphaned signing agent session(s) removed",
			swept.Root, len(swept.Removed))
	}
	// EVERY RUNNER GETS THE SAME FORGE ACCESS. Triage and the reviewer clone
	// the same repositories the delivery runner does, over the same
	// transports, so they are handed the same signing agent and the same
	// sockets root — at the checkout scope, which serves the access key and no
	// commit signing key. Handing the delivery runner one and a reader none is
	// what go-quicbaas#929 was: triage could not authenticate an ssh clone and
	// spent one job per sweep finding that out inside a container.
	// The node's default triage agent, taken in each tenant that defines it.
	triageAgents := sdlc.TriageAgentPolicy{Default: agentRefFromConfig(cfg.TriageAgent), Agents: agentCatalog}
	if err := triageAgents.Default.Validate("sdlc.triage_agent"); err != nil {
		return &SDLCInitError{Component: "default triage agent", Cause: err}
	}
	triage, triageJobs, err := newSDLCTriage(sdlc.TriageRunnerConfig{
		Bindings: sdlcService, Forges: sdlcService, Tokens: sdlcService, Signing: signing, Guard: guard,
		CheckoutRoot: cfg.CheckoutRoot, SocketsRoot: socketsRoot, IssueLimit: cfg.IssuePageLimit,
		TriageAgents: triageAgents,
	}, agents, store)
	if err != nil {
		return err
	}
	preflight, err := sdlc.NewPreflightRunner(sdlc.PreflightRunnerConfig{
		Bindings: sdlcService, Tokens: sdlcService, Signing: signing, Guard: guard,
		CheckoutRoot: cfg.CheckoutRoot, SocketsRoot: socketsRoot,
	})
	if err != nil {
		return &SDLCInitError{Component: "preflight runner", Cause: err}
	}
	// The train builds in a container through the same forge access seam,
	// at the delivery scope: its merge commits are signed as a worker's are.
	// It starts the review job that judges its own pull request, so it holds
	// the engine as the dispatcher does.
	train, err := sdlc.NewTrainRunner(sdlc.TrainRunnerConfig{
		Bindings: sdlcService, Forges: sdlcService, Trains: store, Jobs: agents, Tokens: sdlcService, Signing: signing,
		Guard: guard, CheckoutRoot: cfg.CheckoutRoot, SocketsRoot: socketsRoot, MergeAttempts: cfg.MergeAttempts,
	})
	if err != nil {
		return &SDLCInitError{Component: "merge train runner", Cause: err}
	}
	// The reviewer reads the train ledger: a merge train's pull request is
	// reviewed only while its train is waiting on the verdict.
	reviewer, err := sdlc.NewReviewRunner(sdlc.ReviewRunnerConfig{
		Agents:   agentCatalog,
		Bindings: sdlcService, Forges: sdlcService, Trains: store, Tokens: sdlcService, Signing: signing,
		Guard: guard, CheckoutRoot: cfg.CheckoutRoot, SocketsRoot: socketsRoot,
	})
	if err != nil {
		return &SDLCInitError{Component: "review runner", Cause: err}
	}
	dispatcher, err := sdlc.NewReviewDispatcher(sdlcService, sdlcService, agents, sdlcService)
	if err != nil {
		return &SDLCInitError{Component: "review dispatcher", Cause: err}
	}
	// A review is held, not started, while its seat's Agent has no usable
	// pooled account (every one errored, revoked or over budget).
	if availability, ok := agents.(sdlc.ReviewerAccountAvailability); ok {
		dispatcher.SetAccountAvailability(availability)
	}
	// An operator's reviewer override dispatches through the same dispatcher:
	// it cancels nothing, and the newly selected seat reviews from now on.
	sdlcService.SetReviewDispatcher(dispatcher)
	// An onboarding's preflight runs as a job on this engine.
	sdlcService.SetPreflightJobs(agents)
	ingestor, err := sdlc.NewWebhookIngestor(routes, sdlcService, sdlcService, sdlcService, sdlcService, dispatcher)
	if err != nil {
		return &SDLCInitError{Component: "webhook ingestor", Cause: err}
	}
	// The floor under the dispatcher, driven by the same trigger the delivery
	// path uses: a project whose hook is missing or broken is reviewed from
	// the pass alone, and a backlog that accumulated while it was broken is
	// recovered without re-running anybody's CI.
	reviewFloor, err := sdlc.NewReviewSweeper(sdlc.ReviewSweeperConfig{
		Bindings: sdlcService, Forges: sdlcService, Overrides: sdlcService, Reviews: dispatcher,
	})
	if err != nil {
		return &SDLCInitError{Component: "review floor", Cause: err}
	}
	// A verified issue delivery wakes triage as well as the poller: the
	// poller asks whether a TRIGGERED issue appeared, triage asks whether an
	// untriaged one is now ready, and neither answers the other's question.
	ingestor.SetTriageNudger(triageJobs)
	// The floor under BLOCKED work. A task that parked on a checkable
	// condition — another change landing, a dependency becoming reachable, an
	// owner's ruling posted on its issue — clears itself here; without it the
	// only thing that ever returned one to the queue was a person, and a fleet
	// parks tasks faster than a person clears them. It is wired to the
	// engine's SERVICE rather than its store because only the service
	// ANNOUNCES a readmission, which is what takes `factory:blocked` back off
	// the issue. Built here, before the webhook handler exists, because a
	// verified issue comment reaches the same ruling path at once; the pass
	// itself starts last, below.
	unblock, err := sdlc.NewUnblockSweeper(sdlc.UnblockSweeperConfig{
		Tasks: agents, Bindings: sdlcService, Forges: sdlcService, Rulers: rulers,
	})
	if err != nil {
		return &SDLCInitError{Component: "unblock sweep", Cause: err}
	}
	ingestor.SetRulingReadmitter(unblock)
	webhook, err := sdlc.NewWebhookHTTPHandler(ingestor, sdlcWebhookPathParams, cfg.Webhook.MaxBody, s.logSDLCWebhookError)
	if err != nil {
		return &SDLCInitError{Component: "webhook handler", Cause: err}
	}

	if err := registerSDLCModule(); err != nil {
		return err
	}
	rt := &sdlcRuntime{
		store: store, routes: routes, service: sdlcService, source: source, review: review,
		hooks: hooks, runner: runner, reviewer: reviewer, ingestor: ingestor, webhook: webhook,
		triage: triage, triageJobs: triageJobs, preflight: preflight, reviewFloor: reviewFloor,
		train: train, trainJobs: trainJobs,
	}
	sdlcModule.live.Store(rt)
	s.sdlc = rt
	s.quicBaaSService.SetSDLCService(sdlcService)
	// The repair sweep starts LAST and only after the runtime is live: it
	// posts through the module's own hooks, so a pass that ran before the
	// module was mounted would be writing to the forge on behalf of a module
	// the engine cannot yet reach.
	s.startSDLCLabelReconcile(hooks, sdlcService)
	// The backstops start last, for the same reason: all three start work
	// through the engine that runs this module's own runners.
	s.startSDLCBackstops(triageJobs, reviewFloor, unblock, trainJobs)
	// The worker image refresh rebinds projects through the binding service,
	// and smokes candidates through the engine, so it too starts once both
	// are live.
	if err := s.startSDLCWorkerImage(sdlcService, agents, projectStores.Store()); err != nil {
		return err
	}
	s.logger.Info("sdlc: module started (source=forge_issue validator=forge_review runner=sdlc runner=triage runner=merge_train)")
	return nil
}

// stopSDLC detaches the module: the registered seams answer unavailable
// from here on, and the facade stops serving the binding service.
// newSDLCTriage builds the triage runner and the scheduler that nudges it.
//
// BOTH HOLD THE ONE STORE THE RERUN MARKS LIVE IN (go-quicbaas#894). A nudge
// that loses the project's lease leaves its mark there, and the run in flight
// takes it there and lists the issues again; a scheduler marking one store
// and a runner reading another would drop the nudge exactly as before. The
// store is set here, over whatever cfg carries, so no caller can split them.
func newSDLCTriage(cfg sdlc.TriageRunnerConfig, jobs sdlc.TriageJobStarter, store *sdlc.Store) (*sdlc.TriageRunner, *sdlc.TriageScheduler, error) {
	cfg.Clusters, cfg.Reruns = store, store
	runner, err := sdlc.NewTriageRunner(cfg)
	if err != nil {
		return nil, nil, &SDLCInitError{Component: "triage runner", Cause: err}
	}
	scheduler, err := sdlc.NewTriageScheduler(sdlc.TriageSchedulerConfig{
		Bindings: cfg.Bindings, Jobs: jobs, TriageAgents: cfg.TriageAgents, Reruns: store,
	})
	if err != nil {
		return nil, nil, &SDLCInitError{Component: "triage scheduler", Cause: err}
	}
	return runner, scheduler, nil
}

// newSDLCForgeTokenSources builds one command token source per forge type in
// sdlc.forge.token_commands. Each command learns its forge type from its
// environment; the base URL is added per call. No commands yields nil.
func newSDLCForgeTokenSources(commands map[string]config.SDLCForgeTokenCommand) (map[sdlc.ForgeType]sdlc.ForgeTokenSource, error) {
	if len(commands) == 0 {
		return nil, nil
	}
	sources := make(map[sdlc.ForgeType]sdlc.ForgeTokenSource, len(commands))
	for forgeType, command := range commands {
		source, err := sdlc.NewCommandTokenSource(sdlc.CommandTokenSourceConfig{
			Argv:          command.Argv,
			Env:           []string{sdlc.ForgeTokenEnvType + "=" + forgeType},
			RefreshBefore: command.RefreshBefore,
			Timeout:       command.Timeout,
		})
		if err != nil {
			return nil, err
		}
		sources[sdlc.ForgeType(forgeType)] = source
	}
	return sources, nil
}

func (s *Server) stopSDLC() {
	if s.sdlc == nil {
		return
	}
	// The sweep is stopped BEFORE the module detaches: a pass still running
	// against a detached runtime would be posting outcomes through hooks
	// whose stores are on their way down.
	s.stopSDLCLabelReconcile()
	s.stopSDLCBackstops()
	s.stopSDLCWorkerImage()
	sdlcModule.live.CompareAndSwap(s.sdlc, nil)
	s.sdlc = nil
	if s.quicBaaSService != nil {
		s.quicBaaSService.SetSDLCService(nil)
	}
	s.logger.Info("sdlc: module stopped")
}

// sdlcTransactor picks the substrate that decides the route index's guarded
// writes, from the DEPLOYMENT MODE rather than from whichever handles happen
// to be present — the same rule as the task claim arbiter. Single-node modes
// arbitrate in-process, the only substrate they have; cluster-like modes
// must reach the transaction plane, and failing to is fatal.
func (s *Server) sdlcTransactor(shard *qrdbsdk.Client) (cas.Transactor, error) {
	if !config.IsClusterLikeMode(s.getEffectiveMode()) {
		return cas.NewLocalTransactor(shard.KV())
	}
	tx := shard.Transaction()
	if err := verifyTransactionPlane(context.Background(), tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// verifyTransactionPlane proves the plane answers BEFORE the module claims
// to have started: an embedded standalone client returns a NON-NIL
// transaction client whose servicer is nil, so a nil check is not enough.
// The probe is a transaction whose compare cannot hold and whose branches
// carry no operations: it decides nothing and leaves no key behind.
func verifyTransactionPlane(ctx context.Context, tx cas.Transactor) error {
	_, err := tx.ExecuteTransaction(ctx, &transport.TransactionRequest{
		Compares: []transport.Compare{{
			Key:            []byte(sdlcTransactionProbeKey),
			Target:         transport.CompareValue,
			Result:         transport.CompareEqual,
			Value:          []byte("\x00probe-never-matches"),
			CreateRevision: 0,
		}},
	})
	return err
}

// sdlcTransactionProbeKey is the key the boot probe compares against. It is
// never written.
const sdlcTransactionProbeKey = "__sys:sdlc/boot-probe"

// sdlcWebhookPathParams extracts the project id from the route. Injected
// rather than assumed by pkg/sdlc, which holds no router.
func sdlcWebhookPathParams(r *http.Request) string {
	return chi.URLParam(r, "projectID")
}

// logSDLCWebhookError records a refused or failed delivery. The handler
// tells the caller deliberately little; this tells the operator everything.
func (s *Server) logSDLCWebhookError(r *http.Request, err error) {
	if s.logger == nil {
		return
	}
	s.logger.WithError(err).Warnf("sdlc.webhook: %s (%s)", sdlcWebhookFailure(err), r.URL.Path)
}

// sdlcWebhookFailure names what an error handed to the webhook's error hook
// means. A triage wake that failed rode on an ACCEPTED delivery, and logging
// it as a refusal would send the operator looking at the forge's hook
// instead of the triage scheduler.
func sdlcWebhookFailure(err error) string {
	var wake *sdlc.TriageWakeError
	if errors.As(err, &wake) {
		return "delivery accepted but triage was not woken"
	}
	return "delivery refused"
}

// SDLCWebhookHandler returns the module's webhook handler, or nil when the
// module is not running; the REST layer decides what an absent module tells
// a caller (503, so a forge retries).
func (s *Server) SDLCWebhookHandler() http.HandlerFunc {
	if s.sdlc == nil || s.sdlc.webhook == nil {
		return nil
	}
	return s.sdlc.webhook.ServeHTTP
}

// SDLCService returns the module's binding service, or nil when it is not
// running. Nil is a legitimate answer — the module is opt-in.
func (s *Server) SDLCService() *service.SDLCService {
	if s.sdlc == nil {
		return nil
	}
	return s.sdlc.service
}

// SDLCInitError reports a component of the module that could not be built.
// It names the component because "sdlc failed to start" sends an operator
// reading the whole subsystem.
type SDLCInitError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *SDLCInitError) Error() string {
	return "sdlc: " + e.Component + ": " + e.Cause.Error()
}

// Unwrap supports errors.Is and errors.As on the cause.
func (e *SDLCInitError) Unwrap() error { return e.Cause }

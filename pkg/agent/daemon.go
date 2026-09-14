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

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Enroller produces an AgentIdentity from a daemon configuration. The
// production implementation lives in pkg/agent/enrollment.go (Phase 3);
// tests inject a fake to exercise lifecycle transitions without talking
// to the control plane.
type Enroller interface {
	Enroll(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error)
}

// HeartbeatSender delivers periodic liveness updates to the control plane
// orchestrator. Phase 4 swaps in the gRPC bidi-stream implementation;
// the skeleton accepts any implementation of this one-shot interface.
type HeartbeatSender interface {
	SendHeartbeat(ctx context.Context, identity *orchestration.AgentIdentity, status HeartbeatStatus) error
}

// HeartbeatStatus captures the state reported on each heartbeat tick.
type HeartbeatStatus struct {
	ActiveSessions int
	SandboxPolicy  orchestration.SandboxPolicy
	Backends       []string
	HasContainerRT bool
	SequenceNumber uint64

	// MaxConcurrentJobs is how many orchestration jobs this host declares it
	// can run at once (Config.MaxConcurrentJobs). Sent on every heartbeat so
	// the control plane's fleet capacity tracks a host being drained or added
	// with nothing to keep in step by hand; zero reports nothing, which is not
	// the same as reporting no slots. See
	// orchestration.AgentRegistration.MaxConcurrentJobs.
	MaxConcurrentJobs int

	// BindAddr is the daemon's reachable spawn-server address
	// ("host:port"). Sent on every heartbeat so the control plane can repair
	// the registry's Host column after a restart-driven recovery — the
	// recovery path reconstructs the registry entry from the cert
	// (which only carries a hostname) and needs the heartbeat body to
	// supply the port a dispatcher will dial for inference RPCs.
	BindAddr string
}

// SpawnServer exposes the AgentRuntime gRPC service on the daemon's bind
// address. Phase 4 provides the concrete implementation; the skeleton
// accepts any Start/Stop lifecycle wrapper.
type SpawnServer interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Addr() string
}

// Daemon is the top-level lifecycle manager for the agent runtime. It owns
// the config, the Runtime, identity, and background loops.
type Daemon struct {
	cfg         *Config
	runtime     *Runtime
	enroller    Enroller
	heartbeat   HeartbeatSender
	spawnServer SpawnServer
	logger      interfaces.Logger

	identity atomic.Pointer[orchestration.AgentIdentity]
	state    atomic.Int32 // daemonState
	hbSeq    atomic.Uint64

	startOnce sync.Once
	stopOnce  sync.Once
	failOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
	runErr    atomic.Pointer[error]
}

// daemonState reports the daemon's lifecycle phase. Monotonically
// increasing except on explicit reset via a fresh NewDaemon call.
type daemonState int32

const (
	stateNew daemonState = iota
	stateEnrolling
	stateRunning
	stateDraining
	stateStopped
	stateFailed
)

// State returns a human-readable lifecycle label. Public for diagnostic
// and status endpoints.
func (d *Daemon) State() string {
	switch daemonState(d.state.Load()) {
	case stateNew:
		return "new"
	case stateEnrolling:
		return "enrolling"
	case stateRunning:
		return "running"
	case stateDraining:
		return "draining"
	case stateStopped:
		return "stopped"
	case stateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// DaemonDeps injects production dependencies into NewDaemon. In tests this
// can be populated with fakes for each interface.
type DaemonDeps struct {
	Runtime     *Runtime
	Enroller    Enroller
	Heartbeat   HeartbeatSender
	SpawnServer SpawnServer
	Logger      interfaces.Logger
}

// NewDaemon constructs a daemon from validated config plus its dependencies.
func NewDaemon(cfg *Config, deps DaemonDeps) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.Runtime == nil {
		return nil, ErrDaemonNoRuntime
	}
	if deps.Enroller == nil {
		return nil, ErrDaemonNoEnroller
	}
	if deps.SpawnServer == nil {
		return nil, ErrDaemonNoSpawnServer
	}
	if deps.Logger == nil {
		return nil, ErrDaemonNoLogger
	}
	if err := ensureWorkDir(cfg.WorkDir); err != nil {
		return nil, err
	}
	return &Daemon{
		cfg:         cfg,
		runtime:     deps.Runtime,
		enroller:    deps.Enroller,
		heartbeat:   deps.Heartbeat,
		spawnServer: deps.SpawnServer,
		logger:      deps.Logger,
		stopCh:      make(chan struct{}),
	}, nil
}

// Start brings the daemon online: enroll → start spawn server → launch
// heartbeat loop. Returns once the daemon is running (background loops
// continue until Stop is called).
func (d *Daemon) Start(ctx context.Context) error {
	var startErr error
	d.startOnce.Do(func() {
		d.state.Store(int32(stateEnrolling))
		d.logger.Infof("agent daemon enrolling with server=%s tenant=%s",
			d.cfg.ServerAddr, d.cfg.TenantID)

		identity, err := d.enrollWithRetry(ctx)
		if err != nil {
			d.state.Store(int32(stateFailed))
			d.recordRunErr(&OpError{Op: "enrollment", Cause: err})
			startErr = err
			return
		}
		d.identity.Store(identity)

		// BEFORE THE SPAWN SERVER ACCEPTS ANYTHING. A sweep claims containers
		// by this daemon's owner label, and once this daemon has spawned one
		// of its own the two are indistinguishable — so the one safe moment
		// is while it holds none. A sweep that cannot be completed is a
		// startup failure for the reason go-quicbaas#676 gives: an orphan
		// that survives its sweep accumulates without bound.
		sweep, sweepErr := d.runtime.SweepOrphanContainers(ctx)
		if sweepErr != nil {
			d.state.Store(int32(stateFailed))
			d.recordRunErr(&OpError{Op: "container sweep", Cause: sweepErr})
			startErr = sweepErr
			return
		}
		if len(sweep.Removed) > 0 {
			d.logger.Infof("agent daemon: swept %d orphaned worker container(s) left by a previous run",
				len(sweep.Removed))
		}

		if err := d.spawnServer.Start(ctx); err != nil {
			d.state.Store(int32(stateFailed))
			d.recordRunErr(&OpError{Op: "spawn server", Cause: err})
			startErr = err
			return
		}

		// Image warmup is best-effort: air-gapped deployments can run with
		// pre-loaded images and any pull errors are logged, not fatal.
		if prepErr := d.runtime.PrepareImages(ctx, d.cfg.AutoPull); prepErr != nil {
			d.logger.Warnf("agent daemon image prepare: %v", prepErr)
		}

		d.state.Store(int32(stateRunning))
		d.logger.Infof("agent daemon running at %s (sandbox policy=%s)",
			d.spawnServer.Addr(), d.runtime.SandboxResolver().Policy().String())

		if d.heartbeat != nil {
			d.wg.Add(1)
			go d.heartbeatLoop()
		}
	})
	return startErr
}

// enrollWithRetry drives the enrollment attempt loop.
//
// A TRANSIENT failure means the control plane is not reachable yet, which
// is routine when an agent host boots ahead of its server or the server is
// restarting. The daemon waits it out with capped exponential backoff for
// as long as ctx lives, logging every wait so the journal shows a daemon
// that is waiting rather than one that is wedged. There is deliberately no
// attempt budget: a daemon that gave up would only be restarted by its
// supervisor straight back into the same wait, with the backoff reset.
//
// A TERMINAL failure returns immediately so the process exits and the unit
// lands in failed, where monitoring can see it. See enroll_retry.go for
// which failures fall in which class and why.
func (d *Daemon) enrollWithRetry(ctx context.Context) (*orchestration.AgentIdentity, error) {
	backoff := enrollBackoff{
		base: d.cfg.EffectiveEnrollRetryBase(),
		max:  d.cfg.EffectiveEnrollRetryMax(),
	}
	for attempt := 1; ; attempt++ {
		identity, err := d.enroller.Enroll(ctx, d.cfg)
		if err == nil {
			if attempt > 1 {
				d.logger.Infof("agent daemon enrolled on attempt %d", attempt)
			}
			return identity, nil
		}

		class, reason := classifyEnrollment(err)
		if class != EnrollmentTransient {
			return nil, &EnrollmentTerminalError{Reason: reason, Cause: err}
		}

		// Check cancellation before arming the timer so an already-dead
		// context ends the loop deterministically rather than racing a
		// short backoff window in the select below.
		select {
		case <-ctx.Done():
			return nil, &EnrollmentAbandonedError{Attempts: attempt, Cause: ctx.Err(), Last: err}
		default:
		}

		wait, jitterErr := backoff.delay(attempt)
		if jitterErr != nil {
			// The schedule still caps and still makes progress; it has
			// only lost the jitter that keeps a whole fleet from
			// retrying in lockstep. Report it and wait the full window.
			d.logger.Warnf("agent daemon enrollment backoff: %v; waiting the full window", jitterErr)
		}
		d.logger.Infof(
			"agent daemon enrollment attempt %d failed, class=%s (%s); retrying in %s: %v",
			attempt, class, reason, wait, err)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, &EnrollmentAbandonedError{Attempts: attempt, Cause: ctx.Err(), Last: err}
		case <-timer.C:
		}
	}
}

// Stop drains in-flight work and tears down background loops. Safe to
// call multiple times; only the first invocation performs the teardown.
func (d *Daemon) Stop(ctx context.Context) error {
	var stopErr error
	d.stopOnce.Do(func() {
		if cur := daemonState(d.state.Load()); cur == stateNew {
			d.state.Store(int32(stateStopped))
			return
		}
		d.state.Store(int32(stateDraining))
		// signalReregisterFailure may have already closed stopCh; the
		// failOnce gate makes that a single-shot close, so we only do
		// it here when the loop has not yet been signalled.
		d.failOnce.Do(func() {
			close(d.stopCh)
		})

		drainCtx, cancel := context.WithTimeout(ctx, d.cfg.EffectiveShutdownGrace())
		defer cancel()

		if err := d.spawnServer.Stop(drainCtx); err != nil {
			d.logger.Warnf("agent daemon spawn server stop: %v", err)
			stopErr = err
		}

		d.wg.Wait()
		d.state.Store(int32(stateStopped))
		d.logger.Infof("agent daemon stopped")
	})
	return stopErr
}

// Identity returns the current AgentIdentity, or nil before enrollment
// completes. Safe for concurrent callers.
func (d *Daemon) Identity() *orchestration.AgentIdentity {
	return d.identity.Load()
}

// Runtime exposes the internal runtime for wiring (e.g., the spawn server
// calls Daemon.Runtime().Spawn).
func (d *Daemon) Runtime() *Runtime {
	return d.runtime
}

// Config returns the immutable daemon configuration.
func (d *Daemon) Config() *Config {
	return d.cfg
}

// RunError returns the terminal run error, or nil if the daemon is healthy
// or was stopped cleanly.
func (d *Daemon) RunError() error {
	if p := d.runErr.Load(); p != nil {
		return *p
	}
	return nil
}

// heartbeatLoop delivers periodic liveness updates. Exits when stopCh is
// closed. Transient send failures are logged and retried on the next tick.
func (d *Daemon) heartbeatLoop() {
	defer d.wg.Done()
	interval := d.cfg.EffectiveHeartbeat()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.sendHeartbeat(context.Background())
		}
	}
}

func (d *Daemon) sendHeartbeat(ctx context.Context) {
	identity := d.identity.Load()
	if identity == nil {
		return
	}
	status := HeartbeatStatus{
		SandboxPolicy:     d.runtime.SandboxResolver().Policy(),
		Backends:          d.runtime.Backends(),
		HasContainerRT:    d.runtime.SandboxResolver().HasContainerRuntime(),
		MaxConcurrentJobs: d.cfg.MaxConcurrentJobs,
		SequenceNumber:    d.hbSeq.Add(1),
		BindAddr:          d.spawnServer.Addr(),
	}
	if err := d.heartbeat.SendHeartbeat(ctx, identity, status); err != nil {
		d.logger.Warnf("agent daemon heartbeat (seq=%d): %v", status.SequenceNumber, err)
		// Terminal re-register failure → record + signal the loop to
		// exit so systemd restarts the unit from a fresh process state.
		// All other errors are transient; the next tick retries.
		if errors.Is(err, ErrAgentReRegisterFailed) {
			d.recordRunErr(err)
			d.signalReregisterFailure()
		}
	}
}

// signalReregisterFailure asks the heartbeatLoop to exit cleanly. Uses
// a dedicated sync.Once so the close-of-stopCh is idempotent, and so a
// follow-up Stop() call still gets to run the spawn-server teardown
// (Stop's own stopOnce stays intact). State transitions to "failed"
// for diagnostic visibility — operators see the daemon entered the
// terminal state from the run-error path, not a clean stop.
func (d *Daemon) signalReregisterFailure() {
	d.failOnce.Do(func() {
		d.state.Store(int32(stateFailed))
		close(d.stopCh)
	})
}

func (d *Daemon) recordRunErr(err error) {
	d.runErr.Store(&err)
}

// ensureWorkDir creates the daemon's work directory tree with 0700
// permissions. Existing directories are left alone.
func ensureWorkDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return &OpError{Op: "agent: resolve work dir", Cause: err}
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return &OpError{Op: "agent: create work dir", Cause: err}
	}
	return nil
}

// Daemon error sentinels.
var (
	ErrDaemonNoRuntime     = errors.New("agent: daemon requires a Runtime")
	ErrDaemonNoEnroller    = errors.New("agent: daemon requires an Enroller")
	ErrDaemonNoSpawnServer = errors.New("agent: daemon requires a SpawnServer")
	ErrDaemonNoLogger      = errors.New("agent: daemon requires a logger")
)

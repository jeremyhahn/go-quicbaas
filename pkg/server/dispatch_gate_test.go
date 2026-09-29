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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// slowRecoverer is an agent service whose recovery gave up on its boot budget
// while a resumed job was still waiting for its lease.
type slowRecoverer struct {
	orchestration.AgentService
	settled chan struct{}
}

func (r *slowRecoverer) RecoverJobs(context.Context) error {
	return &orchestration.JobRecoveryError{Message: "boot budget ran out", Cause: context.DeadlineExceeded}
}

func (r *slowRecoverer) ResumedJobsSettled() <-chan struct{} { return r.settled }

type recoveringService struct {
	service.QuicBaaS
	agent *slowRecoverer
}

func (s *recoveringService) Agent() orchestration.AgentService { return s.agent }

func newGateTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return log
}

// TestRecoverOrchestrationJobs_AFailedRecoveryStillGatesTheDispatcher is the
// regression for Codex #1075 round 3: recovery that gave up at its deadline
// must leave the dispatcher waiting on the resumed jobs, not start it.
func TestRecoverOrchestrationJobs_AFailedRecoveryStillGatesTheDispatcher(t *testing.T) {
	settled := make(chan struct{})
	cfg := &config.Config{}
	cfg.Server.ShutdownTimeout = time.Second
	s := &Server{
		config:          cfg,
		logger:          newGateTestLogger(t),
		quicBaaSService: &recoveringService{agent: &slowRecoverer{settled: settled}},
	}

	s.recoverOrchestrationJobs()

	if s.dispatchGate == nil {
		t.Fatal("a failed recovery left no gate, so the dispatcher would start at once")
	}
	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan bool, 1)
	go func() { released <- awaitDispatchGate(ctx, s.dispatchGate, s.logger) }()
	select {
	case <-released:
		t.Fatal("the dispatcher was released while a resumed job still had no lease")
	default:
	}

	close(settled)
	if !<-released {
		t.Fatal("the dispatcher was not released once every resumed job settled")
	}
	cancel()
}

func TestAwaitDispatchGate(t *testing.T) {
	log := newGateTestLogger(t)
	if !awaitDispatchGate(context.Background(), nil, log) {
		t.Fatal("a nil gate is open")
	}
	closed := make(chan struct{})
	close(closed)
	if !awaitDispatchGate(context.Background(), closed, log) {
		t.Fatal("a closed gate is open")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if awaitDispatchGate(ctx, make(chan struct{}), log) {
		t.Fatal("a dispatcher stopped before its gate opened must not sweep")
	}
}

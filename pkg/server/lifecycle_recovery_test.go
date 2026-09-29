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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// recoveringAgent is an agent service whose only real method is the
// engine's restart path; the embedded interface is never called.
type recoveringAgent struct {
	orchestration.AgentService
	recovered int
}

func (a *recoveringAgent) RecoverJobs(context.Context) error {
	a.recovered++
	return nil
}

// ResumedJobsSettled reports nothing resumed, so nothing to wait for.
func (a *recoveringAgent) ResumedJobsSettled() <-chan struct{} {
	settled := make(chan struct{})
	close(settled)
	return settled
}

// agentOnlyService answers the agent service and nothing else.
type agentOnlyService struct {
	service.QuicBaaS
	agent orchestration.AgentService
}

func (s *agentOnlyService) Agent() orchestration.AgentService { return s.agent }

// TestRecoverOrchestrationJobs_RunsTheEngineRecoveryOnce: the lifecycle step
// that Start runs after every module has registered hands recovery to the
// engine exactly once.
func TestRecoverOrchestrationJobs_RunsTheEngineRecoveryOnce(t *testing.T) {
	agent := &recoveringAgent{}
	s := &Server{
		config:          &config.Config{Server: config.ServerConfig{ShutdownTimeout: time.Second}},
		quicBaaSService: &agentOnlyService{agent: agent},
	}
	s.recoverOrchestrationJobs()
	if agent.recovered != 1 {
		t.Fatalf("RecoverJobs ran %d times, want 1", agent.recovered)
	}
}

// TestRecoverOrchestrationJobs_NothingToRecoverThrough: no service, or an
// agent service without a restart path, is a no-op rather than a panic.
func TestRecoverOrchestrationJobs_NothingToRecoverThrough(t *testing.T) {
	(&Server{config: &config.Config{}}).recoverOrchestrationJobs()
	s := &Server{
		config:          &config.Config{},
		quicBaaSService: &agentOnlyService{agent: struct{ orchestration.AgentService }{}},
	}
	s.recoverOrchestrationJobs()
}

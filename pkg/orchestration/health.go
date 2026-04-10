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

package orchestration

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// DefaultHealthCheckInterval is the default interval between health checks.
	DefaultHealthCheckInterval = 30 * time.Second

	// DefaultHealthCheckTimeout is the default timeout for a single health check.
	DefaultHealthCheckTimeout = 10 * time.Second
)

// HealthCheckReport contains the results of checking all active sessions.
type HealthCheckReport struct {
	Timestamp     int64                  `json:"timestamp"`
	TotalSessions int                    `json:"total_sessions"`
	Healthy       int                    `json:"healthy"`
	Unhealthy     int                    `json:"unhealthy"`
	Closed        int                    `json:"closed"`
	Results       []*SessionHealthResult `json:"results"`
}

// SessionHealthResult contains the health check result for a single session.
type SessionHealthResult struct {
	SessionID string       `json:"session_id"`
	Backend   string       `json:"backend"`
	State     SessionState `json:"state"`
	Healthy   bool         `json:"healthy"`
	Error     string       `json:"error,omitempty"`
}

// AgentHealthMonitor periodically checks agent session health via the
// AgentService. It uses atomic.Bool for lock-free running state management
// and a done channel for graceful shutdown signaling.
type AgentHealthMonitor struct {
	service  AgentService
	logger   interfaces.Logger
	interval time.Duration
	timeout  time.Duration
	running  atomic.Bool
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewAgentHealthMonitor creates a new AgentHealthMonitor with default interval
// and timeout settings. The monitor must be started explicitly via Start().
func NewAgentHealthMonitor(service AgentService, logger interfaces.Logger) *AgentHealthMonitor {
	return &AgentHealthMonitor{
		service:  service,
		logger:   logger,
		interval: DefaultHealthCheckInterval,
		timeout:  DefaultHealthCheckTimeout,
		done:     make(chan struct{}),
	}
}

// Start begins periodic health checks in a background goroutine. If the
// monitor is already running, Start returns ErrHealthMonitorAlreadyRunning.
// The goroutine respects context cancellation and can be stopped via Stop().
func (m *AgentHealthMonitor) Start(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return &AgentServiceError{
			Sentinel: ErrHealthMonitorAlreadyRunning,
		}
	}

	m.wg.Add(1)
	go m.run(ctx)

	m.logger.Infof("agent health monitor started: interval=%s timeout=%s", m.interval, m.timeout)
	return nil
}

// Stop halts the health monitor and waits for the background goroutine to
// exit. If the monitor is not running, Stop returns ErrHealthMonitorNotRunning.
func (m *AgentHealthMonitor) Stop() error {
	if !m.running.CompareAndSwap(true, false) {
		return &AgentServiceError{
			Sentinel: ErrHealthMonitorNotRunning,
		}
	}

	close(m.done)
	m.wg.Wait()

	// Reset done channel for potential restart
	m.done = make(chan struct{})

	m.logger.Info("agent health monitor stopped")
	return nil
}

// CheckAll performs a health check on all active sessions. Sessions in the
// "active" or "idle" state are checked for liveness. Sessions that return
// an error or are in "error"/"closed" state are marked unhealthy.
func (m *AgentHealthMonitor) CheckAll(ctx context.Context) *HealthCheckReport {
	checkCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	listResp, err := m.service.ListSessions(checkCtx, &ListSessionsRequest{})
	if err != nil {
		m.logger.Warnf("agent health check: failed to list sessions: %s", err.Error())
		return &HealthCheckReport{
			Timestamp: time.Now().UnixNano(),
		}
	}

	report := &HealthCheckReport{
		Timestamp:     time.Now().UnixNano(),
		TotalSessions: len(listResp.Sessions),
		Results:       make([]*SessionHealthResult, 0, len(listResp.Sessions)),
	}

	for _, session := range listResp.Sessions {
		result := m.checkSingleSession(checkCtx, session)
		report.Results = append(report.Results, result)

		switch {
		case result.State == SessionStateClosed:
			report.Closed++
		case result.Healthy:
			report.Healthy++
		default:
			report.Unhealthy++
		}
	}

	if report.Unhealthy > 0 {
		m.logger.Warnf("agent health check: %d/%d sessions unhealthy",
			report.Unhealthy, report.TotalSessions)
	}

	return report
}

// CheckSession checks a single session's health by querying its status
// through the AgentService.
func (m *AgentHealthMonitor) CheckSession(ctx context.Context, sessionID string) (*SessionHealthResult, error) {
	if sessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}

	checkCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	statusResp, err := m.service.SessionStatus(checkCtx, &SessionStatusRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return &SessionHealthResult{
			SessionID: sessionID,
			State:     SessionStateError,
			Healthy:   false,
			Error:     err.Error(),
		}, nil
	}

	healthy := statusResp.State == SessionStateActive ||
		statusResp.State == SessionStateIdle

	return &SessionHealthResult{
		SessionID: sessionID,
		State:     statusResp.State,
		Healthy:   healthy,
	}, nil
}

// run is the background goroutine loop that periodically calls CheckAll.
func (m *AgentHealthMonitor) run(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.running.Store(false)
			return
		case <-m.done:
			return
		case <-ticker.C:
			if !m.running.Load() {
				return
			}
			m.CheckAll(ctx)
		}
	}
}

// checkSingleSession queries the status of one session and builds a result.
func (m *AgentHealthMonitor) checkSingleSession(ctx context.Context, session *SessionInfo) *SessionHealthResult {
	result := &SessionHealthResult{
		SessionID: session.SessionID,
		Backend:   session.Backend,
	}

	statusResp, err := m.service.SessionStatus(ctx, &SessionStatusRequest{
		SessionID: session.SessionID,
	})
	if err != nil {
		result.State = SessionStateError
		result.Healthy = false
		result.Error = err.Error()
		m.logger.Warnf("agent health check: session %s unhealthy: %s",
			session.SessionID, err.Error())
		return result
	}

	result.State = statusResp.State
	result.Healthy = statusResp.State == SessionStateActive ||
		statusResp.State == SessionStateIdle

	if !result.Healthy {
		m.logger.Warnf("agent health check: session %s in state %q",
			session.SessionID, statusResp.State)
	}

	return result
}

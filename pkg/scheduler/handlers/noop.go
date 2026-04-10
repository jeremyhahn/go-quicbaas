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

// Package handlers provides built-in job handlers for the scheduler.
package handlers

import (
	"context"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// Standard test handler names used by integration tests.
const (
	HandlerNoop        = "noop"
	HandlerTest        = "test-handler"
	HandlerCron        = "cron-handler"
	HandlerRetry       = "retry-handler"
	HandlerDelayed     = "delayed-handler"
	HandlerPriority    = "priority-handler"
	HandlerTimeout     = "timeout-handler"
	HandlerConcurrent  = "concurrent-handler"
	HandlerGracePeriod = "kms-grace-period"
)

// NoopHandler is a no-operation handler that always succeeds.
// Used for testing and as a placeholder for jobs that don't need processing.
type NoopHandler struct {
	name string
}

// NewNoopHandler creates a new noop handler with the given name.
func NewNoopHandler(name string) *NoopHandler {
	return &NoopHandler{name: name}
}

// Name returns the handler name for registration.
func (h *NoopHandler) Name() string {
	return h.name
}

// Handle executes the noop job - always returns success.
func (h *NoopHandler) Handle(_ context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
	return &scheduler.JobResult{
		Success: true,
		Message: fmt.Sprintf("Job %d executed successfully by noop handler '%s'", job.ID, h.name),
	}, nil
}

// Handler returns the JobHandler function for registration.
func (h *NoopHandler) Handler() scheduler.JobHandler {
	return h.Handle
}

// StandardTestHandlers returns all standard test handlers for integration testing.
// These handlers are noop handlers that always succeed.
func StandardTestHandlers() []Handler {
	return []Handler{
		NewNoopHandler(HandlerNoop),
		NewNoopHandler(HandlerTest),
		NewNoopHandler(HandlerCron),
		NewNoopHandler(HandlerRetry),
		NewNoopHandler(HandlerDelayed),
		NewNoopHandler(HandlerPriority),
		NewNoopHandler(HandlerTimeout),
		NewNoopHandler(HandlerConcurrent),
		NewNoopHandler(HandlerGracePeriod),
	}
}

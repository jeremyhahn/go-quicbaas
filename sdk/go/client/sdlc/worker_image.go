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

package sdlc

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// The Software Factory worker image refresh
// =============================================================================
//
// HOST VERBS. The refresh keeps every tenant's bound worker image current, so
// neither verb takes a tenant and both are authorized by the cross-tenant
// worker_images:* permissions a system-tenant operator holds.

// WorkerImageStatusRequest reads the refresh loop's state. It carries no
// field.
type WorkerImageStatusRequest struct{}

// RefreshWorkerImageRequest queues a full refresh pass. It carries no field.
type RefreshWorkerImageRequest struct{}

// WorkerImageStatus is the refresh loop's state as an operator reads it.
type WorkerImageStatus struct {
	// Serving reports whether the refresh loop runs on the node.
	Serving bool `json:"serving" yaml:"serving"`

	// Running reports a pass in progress, and RunningSince when it began.
	Running      bool      `json:"running" yaml:"running"`
	RunningSince time.Time `json:"running_since,omitempty" yaml:"running_since,omitempty"`

	// Queued reports a requested pass not yet started.
	Queued bool `json:"queued" yaml:"queued"`

	// Last is the most recent pass that finished, nil before the first.
	Last *WorkerImageReport `json:"last,omitempty" yaml:"last,omitempty"`
}

// WorkerImageAgents are the two agent CLI versions an image carries.
type WorkerImageAgents struct {
	ClaudeCode string `json:"claude_code" yaml:"claude_code"`
	Codex      string `json:"codex" yaml:"codex"`
}

// WorkerImageCheck is one probe or smoke the gate ran.
type WorkerImageCheck struct {
	Name   string `json:"name" yaml:"name"`
	Passed bool   `json:"passed" yaml:"passed"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// WorkerImageBound is one managed image bindings name, as a pass found it.
type WorkerImageBound struct {
	Image string `json:"image" yaml:"image"`

	// Agents are the CLI versions its labels declare; empty when unreadable.
	Agents WorkerImageAgents `json:"agents" yaml:"agents"`

	// Uses counts the binding roles (worker, reviewer, triage) naming it.
	Uses  int  `json:"uses" yaml:"uses"`
	Stale bool `json:"stale" yaml:"stale"`
}

// WorkerImagePromotion is one Agent a pass repointed, or tried to.
type WorkerImagePromotion struct {
	TenantID string   `json:"tenant_id" yaml:"tenant_id"`
	AgentID  string   `json:"agent_id" yaml:"agent_id"`
	From     []string `json:"from" yaml:"from"`
	To       string   `json:"to" yaml:"to"`

	// Error is why the Agent was NOT updated; empty on success.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

// WorkerImageReport is what one refresh pass did.
type WorkerImageReport struct {
	// Trigger is schedule or manual; Outcome is current, promoted,
	// partial or failed.
	Trigger    string    `json:"trigger" yaml:"trigger"`
	Outcome    string    `json:"outcome" yaml:"outcome"`
	StartedAt  time.Time `json:"started_at" yaml:"started_at"`
	FinishedAt time.Time `json:"finished_at" yaml:"finished_at"`

	// Latest are the registry versions the pass worked towards.
	Latest WorkerImageAgents `json:"latest" yaml:"latest"`

	// Bound are the distinct managed images bindings named, with the CLI
	// versions each carries.
	Bound []WorkerImageBound `json:"bound,omitempty" yaml:"bound,omitempty"`

	// Stale are the bound images found behind Latest; Skipped the tenants
	// whose bindings could not be read.
	Stale   []string `json:"stale,omitempty" yaml:"stale,omitempty"`
	Skipped []string `json:"skipped,omitempty" yaml:"skipped,omitempty"`

	// Candidate is the new image's local content tag, ImageID the exact build
	// the gate ran against.
	Candidate string `json:"candidate,omitempty" yaml:"candidate,omitempty"`
	ImageID   string `json:"image_id,omitempty" yaml:"image_id,omitempty"`

	// Reference is what the bindings were rebound to: the digest-pinned
	// registry reference, or the local tag in local mode.
	Reference string `json:"reference,omitempty" yaml:"reference,omitempty"`
	Digest    string `json:"digest,omitempty" yaml:"digest,omitempty"`

	Probes     []WorkerImageCheck     `json:"probes,omitempty" yaml:"probes,omitempty"`
	Smokes     []WorkerImageCheck     `json:"smokes,omitempty" yaml:"smokes,omitempty"`
	Promotions []WorkerImagePromotion `json:"promotions,omitempty" yaml:"promotions,omitempty"`

	// Error is why the pass failed or was partial.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

// WorkerImageStatus implements Client.
func (c *client) WorkerImageStatus(ctx context.Context) (*WorkerImageStatus, error) {
	return c.workerImage(ctx, "sdlc.WorkerImageStatus", "WorkerImageStatus", &WorkerImageStatusRequest{})
}

// RefreshWorkerImage implements Client.
func (c *client) RefreshWorkerImage(ctx context.Context) (*WorkerImageStatus, error) {
	return c.workerImage(ctx, "sdlc.RefreshWorkerImage", "RefreshWorkerImage", &RefreshWorkerImageRequest{})
}

// workerImage sends one worker image verb and returns the status it answers.
func (c *client) workerImage(ctx context.Context, op, method string, req interface{}) (*WorkerImageStatus, error) {
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	status, ok := resp.(*WorkerImageStatus)
	if !ok || status == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return status, nil
}

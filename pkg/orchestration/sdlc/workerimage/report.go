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

package workerimage

import (
	"time"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
)

// Trigger is what started a pass.
type Trigger string

const (
	// TriggerSchedule is the pass on the refresh interval (daily by default).
	TriggerSchedule Trigger = "schedule"

	// TriggerManual is a pass an operator requested.
	TriggerManual Trigger = "manual"
)

// Outcome is how a pass ended.
type Outcome string

const (
	// OutcomeCurrent means every bound image already carries the latest CLIs;
	// nothing was built.
	OutcomeCurrent Outcome = "current"

	// OutcomePromoted means a new image passed its gate and every binding that
	// named a stale image now names it.
	OutcomePromoted Outcome = "promoted"

	// OutcomePartial means the pass did not cover the whole fleet: the new
	// image was promoted but at least one binding could not be updated, or at
	// least one tenant's bindings could not be read (whether or not the
	// readable ones needed a new image). The report names which.
	OutcomePartial Outcome = "partial"

	// OutcomeFailed means no binding was changed — including a pass that could
	// read no tenant's bindings, or whose promotion would downgrade a CLI.
	OutcomeFailed Outcome = "failed"
)

// CheckResult is one probe or smoke the gate ran.
type CheckResult struct {
	// Name identifies the check.
	Name string `json:"name"`

	// Passed reports whether it held.
	Passed bool `json:"passed"`

	// Detail is what the check printed or answered, truncated. It is an
	// operator's diagnostic: the report is served only to system operators.
	Detail string `json:"detail,omitempty"`
}

// BoundImage is one managed image bindings name, as a pass found it.
type BoundImage struct {
	// Image is the reference the bindings name.
	Image string `json:"image"`

	// Agents are the CLI versions its labels declare; empty when it carries
	// none or this host does not hold it.
	Agents softwarefactoryworker.Agents `json:"agents"`

	// Uses counts the binding roles (worker, reviewer, triage) naming it.
	Uses int `json:"uses"`

	// Stale reports it was behind the registry.
	Stale bool `json:"stale"`
}

// Promotion is one Agent the pass repointed, or tried to.
type Promotion struct {
	TenantID string   `json:"tenant_id"`
	AgentID  string   `json:"agent_id"`
	From     []string `json:"from"`
	To       string   `json:"to"`

	// Error is why the Agent was NOT updated; empty on success.
	Error string `json:"error,omitempty"`
}

// Report is what one pass did.
type Report struct {
	Trigger    Trigger   `json:"trigger"`
	Outcome    Outcome   `json:"outcome"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// Latest are the registry's versions the pass worked towards.
	Latest softwarefactoryworker.Agents `json:"latest"`

	// Bound are the distinct managed images bindings named when the pass
	// looked, with the CLI versions each carries.
	Bound []BoundImage `json:"bound,omitempty"`

	// Stale are the bound image references found behind Latest.
	Stale []string `json:"stale,omitempty"`

	// Skipped are the tenants whose bindings could not be read ("tenant:
	// reason") — a sealed tenant barrier, typically. Their bindings were
	// neither judged nor rebound.
	Skipped []string `json:"skipped,omitempty"`

	// Candidate is the new image's local content tag, and ImageID the exact
	// build the gate ran against.
	Candidate string `json:"candidate,omitempty"`
	ImageID   string `json:"image_id,omitempty"`

	// Reference is what the bindings were rebound to: the digest-pinned
	// registry reference when a registry is configured, the local tag when
	// not. Digest is the registry manifest digest.
	Reference string `json:"reference,omitempty"`
	Digest    string `json:"digest,omitempty"`

	Probes     []CheckResult `json:"probes,omitempty"`
	Smokes     []CheckResult `json:"smokes,omitempty"`
	Promotions []Promotion   `json:"promotions,omitempty"`

	// Error is why the pass failed or was partial.
	Error string `json:"error,omitempty"`
}

// Status is the refresher's state as an operator reads it.
type Status struct {
	// Serving reports whether the refresh loop runs on this node.
	Serving bool `json:"serving"`

	// Running reports a pass in progress, and RunningSince when it began.
	Running      bool      `json:"running"`
	RunningSince time.Time `json:"running_since,omitempty"`

	// Queued reports a requested pass not yet started.
	Queued bool `json:"queued"`

	// Last is the most recent pass that finished, nil before the first.
	Last *Report `json:"last,omitempty"`
}

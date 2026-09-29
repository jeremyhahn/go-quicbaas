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
	"errors"
	"strconv"
	"strings"
	"time"
)

// Sentinels every error in this package unwraps to, so a transport can map a
// failure onto a status without knowing its concrete type.
var (
	// ErrInvalidConfig means the refresher was configured with something it
	// cannot run with.
	ErrInvalidConfig = errors.New("worker image: invalid configuration")

	// ErrRegistry means the package registry could not say what is latest.
	ErrRegistry = errors.New("worker image: package registry unavailable")

	// ErrBusy means a pass is already running or already requested.
	ErrBusy = errors.New("worker image: a refresh is already in progress")

	// ErrNotRunning means the refresh loop is not running on this node, so a
	// requested pass would never be picked up.
	ErrNotRunning = errors.New("worker image: the refresh loop is not running on this node")

	// ErrGate means the new image failed its gate.
	ErrGate = errors.New("worker image: the new image failed its gate")

	// ErrCredential means the registry push credential could not be loaded.
	ErrCredential = errors.New("worker image: registry credential unavailable")

	// ErrBindingsUnreadable means no tenant's bindings could be read, so the
	// pass could judge nothing.
	ErrBindingsUnreadable = errors.New("worker image: no tenant's bindings could be read")

	// ErrLocalPromotion means a pass in local mode would have repointed Agents
	// to a tag only this host's engine holds while other fleet nodes, which
	// cannot pull it, are registered.
	ErrLocalPromotion = errors.New("worker image: refusing to promote a host-local tag while remote fleet nodes exist")

	// ErrDowngrade means promoting the registry's latest CLIs would move a
	// bound image backwards in one of them.
	ErrDowngrade = errors.New("worker image: the promotion would downgrade a bound image")
)

// InvalidConfigError names a configuration field that is missing or wrong.
type InvalidConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *InvalidConfigError) Error() string {
	return ErrInvalidConfig.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel.
func (e *InvalidConfigError) Unwrap() error { return ErrInvalidConfig }

// RegistryError is a package registry that did not answer usefully.
type RegistryError struct {
	Package string
	Status  int
	Cause   error
}

// Error implements the error interface.
func (e *RegistryError) Error() string {
	msg := ErrRegistry.Error() + ": " + e.Package
	if e.Status != 0 {
		msg += ": HTTP " + strconv.Itoa(e.Status)
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *RegistryError) Unwrap() []error { return withSentinel(ErrRegistry, e.Cause) }

// RefreshInProgressError refuses a request for a pass while one is running or
// already queued.
type RefreshInProgressError struct {
	// Since is when the running pass started; zero when the request found a
	// pass queued rather than running.
	Since time.Time
}

// Error implements the error interface.
func (e *RefreshInProgressError) Error() string {
	if e.Since.IsZero() {
		return ErrBusy.Error() + " (one is already queued)"
	}
	return ErrBusy.Error() + " (running since " + e.Since.UTC().Format(time.RFC3339) + ")"
}

// Unwrap returns the sentinel.
func (e *RefreshInProgressError) Unwrap() error { return ErrBusy }

// RefresherNotRunningError refuses a request no loop would pick up.
type RefresherNotRunningError struct{}

// Error implements the error interface.
func (e *RefresherNotRunningError) Error() string { return ErrNotRunning.Error() }

// Unwrap returns the sentinel.
func (e *RefresherNotRunningError) Unwrap() error { return ErrNotRunning }

// GateError is a candidate image that failed one of its checks.
type GateError struct {
	// Image is the candidate's local tag.
	Image string

	// Failed names the checks that did not pass.
	Failed []string
}

// Error implements the error interface.
func (e *GateError) Error() string {
	return ErrGate.Error() + ": " + e.Image + ": " + strings.Join(e.Failed, "; ")
}

// Unwrap returns the sentinel.
func (e *GateError) Unwrap() error { return ErrGate }

// CredentialError is a registry credential that could not be read or is not
// usable.
type CredentialError struct {
	// Source names where the credential was read from: the tenant and secret
	// reference, "tenant:ref".
	Source  string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *CredentialError) Error() string {
	msg := ErrCredential.Error() + ": " + e.Source + ": " + e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *CredentialError) Unwrap() []error { return withSentinel(ErrCredential, e.Cause) }

// withSentinel is the Unwrap list for an error carrying a sentinel and an
// optional cause.
func withSentinel(sentinel, cause error) []error {
	if cause != nil {
		return []error{sentinel, cause}
	}
	return []error{sentinel}
}

// ProbeWorkspaceError is a probe whose host-side workspace could not be
// prepared, inspected or removed.
type ProbeWorkspaceError struct {
	Probe string
	Cause error
}

// Error implements the error interface.
func (e *ProbeWorkspaceError) Error() string {
	return ErrGate.Error() + ": probe " + strconv.Quote(e.Probe) + " workspace: " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause.
func (e *ProbeWorkspaceError) Unwrap() []error { return withSentinel(ErrGate, e.Cause) }

// UnsupportedPlatformError is a host that cannot answer a question a probe
// asks of it.
type UnsupportedPlatformError struct {
	What string
}

// Error implements the error interface.
func (e *UnsupportedPlatformError) Error() string {
	return ErrGate.Error() + ": this platform cannot report " + e.What
}

// Unwrap returns the sentinel.
func (e *UnsupportedPlatformError) Unwrap() error { return ErrGate }

// errSmokeNotContainerized is a smoke session the orchestrator ran outside any
// container, which therefore says nothing about the candidate image.
var errSmokeNotContainerized = errors.New("the session was not containerized, so it did not run in the candidate")

// errSmokeWrongAnswer is a session that ran and did not answer the question.
var errSmokeWrongAnswer = errors.New("the agent did not answer " + SmokeAnswer)

// SmokeError is a smoke session that did not answer from the candidate.
type SmokeError struct {
	Backend string
	Model   string

	// Stage is where it failed: project, spawn, prompt, answer or close.
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *SmokeError) Error() string {
	model := e.Model
	if model == "" {
		model = "(backend default)"
	}
	return ErrGate.Error() + ": smoke " + e.Backend + "/" + model + " failed at " + e.Stage + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause.
func (e *SmokeError) Unwrap() []error { return withSentinel(ErrGate, e.Cause) }

// BindingsUnreadableError is a pass that could read no tenant's bindings. It
// is a failure, never "current": a pass that judged nothing has not found the
// fleet up to date.
type BindingsUnreadableError struct {
	// Skipped are the tenants and why each could not be read ("tenant:
	// reason").
	Skipped []string
}

// Error implements the error interface.
func (e *BindingsUnreadableError) Error() string {
	return ErrBindingsUnreadable.Error() + ": " + strings.Join(e.Skipped, "; ")
}

// Unwrap returns the sentinel.
func (e *BindingsUnreadableError) Unwrap() error { return ErrBindingsUnreadable }

// DowngradeRefusedError is a stale bound image the registry's latest would
// move backwards in one CLI while moving it forwards in the other. The build
// installs both CLIs at the registry's latest, so promoting it would undo the
// newer one; the pass refuses rather than trade one CLI for the other.
type DowngradeRefusedError struct {
	// Image is the bound image that would regress.
	Image string

	// Component is the CLI package that would move backwards.
	Component string

	// Bound is the version the image carries, Latest the registry's.
	Bound  string
	Latest string
}

// Error implements the error interface.
func (e *DowngradeRefusedError) Error() string {
	return ErrDowngrade.Error() + ": " + e.Image + " carries " + e.Component + " " + e.Bound +
		", newer than the registry's latest " + e.Latest
}

// Unwrap returns the sentinel.
func (e *DowngradeRefusedError) Unwrap() error { return ErrDowngrade }

// LocalPromotionRefusedError is a local-mode pass (no registry configured)
// that found stale Agents in a tenant whose fleet has nodes other than this
// one. Promoting them to the local tag would hand every other node an image
// it does not hold and cannot pull, so the pass changes nothing and says so.
// The fix is sdlc.worker_image.registry.
type LocalPromotionRefusedError struct {
	// TenantID is the tenant whose Agents would have been repointed.
	TenantID string

	// Nodes are the remote fleet nodes, "name@host", sorted.
	Nodes []string
}

// Error implements the error interface.
func (e *LocalPromotionRefusedError) Error() string {
	return ErrLocalPromotion.Error() + ": tenant " + e.TenantID + " has remote fleet nodes (" +
		strings.Join(e.Nodes, ", ") + "); set sdlc.worker_image.registry so every node can pull the promoted image"
}

// Unwrap returns the sentinel.
func (e *LocalPromotionRefusedError) Unwrap() error { return ErrLocalPromotion }

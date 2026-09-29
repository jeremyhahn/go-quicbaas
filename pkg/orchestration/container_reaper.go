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

package orchestration

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The running reaper
// =============================================================================
//
// The startup sweep (container_sweep.go) removes everything a previous run of
// the daemon left, because at startup nothing can be live. A RUNNING daemon
// needs the narrower question: which of MY containers does no live session of
// mine hold? A container can come loose from its session while the daemon
// keeps running — a Close whose removal failed, a spawn torn down after its
// container started, a session the backend forgot — and on 2026-09-24 the
// burn-in node's disk filled with sandbox containers of 10-15 GB each. The
// reaper is the backstop for all of those.
//
// WHAT MAKES IT SAFE IS THE SESSION LABEL. Every sandbox container carries the
// session ID it was launched for (ContainerSessionLabel), assigned BEFORE the
// launch so the label is never a placeholder. A container whose session the
// daemon's backends still hold is never touched. A container whose session is
// not held is removed only once it is older than the grace period, because a
// spawn launches its container before the handshake that registers the
// session — the grace is what keeps an in-flight spawn from being reaped.

// Labels every sandbox container carries besides ContainerOwnerLabel. They
// are what lets the reaper and an operator map a container back to the
// session, job and tenant it serves.
const (
	// ContainerAgentLabel names the backend type that launched the container.
	ContainerAgentLabel = "quicbaas.agent"

	// ContainerSessionLabel is the ID of the session the container runs.
	ContainerSessionLabel = "quicbaas.session"

	// ContainerJobLabel is the ID of the job that spawned the session. Empty
	// for a session no job spawned.
	ContainerJobLabel = "quicbaas.job"

	// ContainerTenantLabel is the tenant the session belongs to.
	ContainerTenantLabel = "quicbaas.tenant"
)

// Reaper defaults. The grace period must exceed the longest spawn: the image
// pull, the container start and the stream-json handshake bounds
// (streamJSONInitTimeout + streamJSONDrainTimeout), because a session is
// registered only once its handshake is done.
const (
	// DefaultContainerReapInterval is how often a running daemon reaps.
	DefaultContainerReapInterval = 5 * time.Minute

	// DefaultContainerReapGrace is how old an unheld container must be
	// before it is reaped.
	DefaultContainerReapGrace = 15 * time.Minute
)

// containerIdentity is what a sandbox container is labelled with: who it runs
// for. It is decided before the launch so no label is ever a placeholder.
type containerIdentity struct {
	SessionID string
	JobID     string
	TenantID  string
}

// labels returns the container's label set: the identity plus the backend
// type and, when set, the owning daemon.
func (id containerIdentity) labels(agent, owner string) map[string]string {
	labels := map[string]string{
		ContainerAgentLabel:   agent,
		ContainerSessionLabel: id.SessionID,
	}
	if id.JobID != "" {
		labels[ContainerJobLabel] = id.JobID
	}
	if id.TenantID != "" {
		labels[ContainerTenantLabel] = id.TenantID
	}
	if owner != "" {
		labels[ContainerOwnerLabel] = owner
	}
	return labels
}

// OwnedContainer is one container a daemon's owner label claims, as the
// runtime reports it.
type OwnedContainer struct {
	// ID is the runtime's full container ID.
	ID string

	// Name is the container name, without the runtime's leading slash.
	Name string

	// CreatedAt is when the runtime created the container. Zero when the
	// runtime reported a time that could not be read.
	CreatedAt time.Time

	// SessionID, JobID and TenantID are the container's identity labels.
	SessionID string
	JobID     string
	TenantID  string
}

// OwnedContainerLister is a container runtime that can enumerate the
// containers an owner label claims, with their identity, and remove one.
type OwnedContainerLister interface {
	// ListOwned returns every container, running or exited, labelled with
	// owner.
	ListOwned(ctx context.Context, owner string) ([]OwnedContainer, error)

	// Remove force-removes a container.
	Remove(ctx context.Context, containerID string) error
}

// ContainerRemoval records one container a reaper removed and why. It is the
// audit record of the removal.
type ContainerRemoval struct {
	Container OwnedContainer
	Reason    string
}

// ContainerReaperConfig configures a ContainerReaper.
type ContainerReaperConfig struct {
	// Runtime enumerates and removes containers. Required.
	Runtime OwnedContainerLister

	// Owner is the ContainerOwnerLabel value the reaper claims containers
	// by. Required: an unowned reaper could only reap everything.
	Owner string

	// Live reports whether a session ID is held by one of the daemon's
	// backends. Required.
	Live func(sessionID string) bool

	// Grace is how old an unheld container must be before it is reaped.
	// Zero means DefaultContainerReapGrace; negative is refused.
	Grace time.Duration

	// Now is the clock. Nil means time.Now.
	Now func() time.Time

	// Logger receives one audit line per removal. Required: a removal
	// nobody can read about is not audited.
	Logger interfaces.Logger
}

// ContainerReaper removes the sandbox containers of one daemon that no live
// session of that daemon holds. See the file comment.
type ContainerReaper struct {
	runtime OwnedContainerLister
	owner   string
	live    func(sessionID string) bool
	grace   time.Duration
	now     func() time.Time
	logger  interfaces.Logger
}

// NewContainerReaper validates cfg and returns a reaper.
func NewContainerReaper(cfg ContainerReaperConfig) (*ContainerReaper, error) {
	owner := strings.TrimSpace(cfg.Owner)
	switch {
	case owner == "":
		return nil, &ContainerSweepRefusedError{
			Reason: "the reaper claims containers by owner label and was given no owner; " +
				"an unowned reaper could only reap everything",
		}
	case cfg.Runtime == nil:
		return nil, &InputValidationError{Field: "runtime", Message: "cannot be nil"}
	case cfg.Live == nil:
		return nil, &InputValidationError{Field: "live", Message: "cannot be nil"}
	case cfg.Logger == nil:
		return nil, &InputValidationError{Field: "logger", Message: "cannot be nil"}
	case cfg.Grace < 0:
		return nil, &InputValidationError{Field: "grace", Message: "cannot be negative"}
	}
	grace := cfg.Grace
	if grace == 0 {
		grace = DefaultContainerReapGrace
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &ContainerReaper{
		runtime: cfg.Runtime,
		owner:   owner,
		live:    cfg.Live,
		grace:   grace,
		now:     now,
		logger:  cfg.Logger,
	}, nil
}

// Reap removes every owned container no live session holds and that is past
// the grace period, and reports what it removed.
//
// A removal that fails does not stop the pass: every other orphan is still
// removed, and the first failure is returned alongside the report.
func (r *ContainerReaper) Reap(ctx context.Context) (ContainerSweep, error) {
	sweep := ContainerSweep{Owner: r.owner}
	containers, err := r.runtime.ListOwned(ctx, r.owner)
	if err != nil {
		return sweep, err
	}
	now := r.now()
	var firstErr error
	for _, c := range containers {
		reason, reap := r.verdict(c, now)
		if !reap {
			continue
		}
		if removeErr := r.runtime.Remove(ctx, c.ID); removeErr != nil {
			var notFound *ContainerNotFoundError
			if !errors.As(removeErr, &notFound) {
				if firstErr == nil {
					firstErr = &ContainerSweepError{Owner: r.owner, ContainerID: c.ID, Cause: removeErr}
				}
				continue
			}
		}
		sweep.Removed = append(sweep.Removed, c.ID)
		sweep.Removals = append(sweep.Removals, ContainerRemoval{Container: c, Reason: reason})
		r.logger.Warnf("container reaper: removed %s (name=%s session=%s job=%s tenant=%s age=%s): %s",
			truncateID(c.ID), c.Name, c.SessionID, c.JobID, c.TenantID,
			now.Sub(c.CreatedAt).Round(time.Second), reason)
	}
	return sweep, firstErr
}

// verdict decides one container: the reason it is reaped, or false to keep it.
func (r *ContainerReaper) verdict(c OwnedContainer, now time.Time) (string, bool) {
	session := strings.TrimSpace(c.SessionID)
	if session != "" && r.live(session) {
		return "", false
	}
	// An unreadable creation time cannot be proven past the grace period.
	if c.CreatedAt.IsZero() || now.Sub(c.CreatedAt) < r.grace {
		return "", false
	}
	if session == "" {
		return "the container names no session", true
	}
	return "session " + session + " is not held by this daemon", true
}

// Run reaps on every tick until ctx ends. report, when non-nil, receives every
// pass's outcome; it is how the caller logs and how a test observes a pass.
func (r *ContainerReaper) Run(ctx context.Context, ticks <-chan time.Time, report func(ContainerSweep, error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweep, err := r.Reap(ctx)
			if report != nil {
				report(sweep, err)
			}
		}
	}
}

// ListOwned returns the containers carrying the owner label with their
// identity labels and creation time. One ps names them and one inspect
// describes them all.
func (r *ExecContainerRuntime) ListOwned(ctx context.Context, owner string) ([]OwnedContainer, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, &ContainerSweepRefusedError{Reason: "no owner label to list containers by"}
	}
	ids, err := r.ownedContainers(ctx, owner)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	args := append([]string{"inspect", "--format", ownedContainerFormat}, ids...)
	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		// A container removed between the ps and the inspect is the one
		// ordinary race here; the next pass sees a consistent host.
		if !isNotFoundOutput(stderr.String()) {
			return nil, &ContainerSweepError{
				Owner: owner,
				Cause: &CommandError{Command: r.command, Op: "inspect", Output: strings.TrimSpace(stderr.String())},
			}
		}
	}
	return parseOwnedContainers(stdout.String()), nil
}

// ownedContainerFormat is the inspect template ListOwned reads: one
// tab-separated line per container. A label the container lacks renders empty.
const ownedContainerFormat = `{{.Id}}	{{.Name}}	{{.Created}}	` +
	`{{index .Config.Labels "` + ContainerSessionLabel + `"}}	` +
	`{{index .Config.Labels "` + ContainerJobLabel + `"}}	` +
	`{{index .Config.Labels "` + ContainerTenantLabel + `"}}`

// parseOwnedContainers reads ownedContainerFormat output. A line with too few
// fields is skipped: it describes nothing the reaper could act on safely.
func parseOwnedContainers(out string) []OwnedContainer {
	var containers []OwnedContainer
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 6 || strings.TrimSpace(fields[0]) == "" {
			continue
		}
		containers = append(containers, OwnedContainer{
			ID:        strings.TrimSpace(fields[0]),
			Name:      strings.TrimPrefix(strings.TrimSpace(fields[1]), "/"),
			CreatedAt: parseContainerCreated(strings.TrimSpace(fields[2])),
			SessionID: strings.TrimSpace(fields[3]),
			JobID:     strings.TrimSpace(fields[4]),
			TenantID:  strings.TrimSpace(fields[5]),
		})
	}
	return containers
}

// parseContainerCreated reads a container's creation time in docker's
// RFC 3339 form or podman's Go time.Time form. Anything else is zero, which
// the reaper treats as "cannot be proven old".
func parseContainerCreated(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

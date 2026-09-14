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
)

// =============================================================================
// The orphan sweep
// =============================================================================
//
// A SESSION THAT ENDS RELEASES ITS CONTAINER — see ACPBackend.Close, which
// fires the process cancel that ends `docker run --rm`. What no teardown can
// survive is the process not running any more: a daemon killed between a spawn
// and its close leaves the container it started running, with nothing left
// that knows the session it belonged to (go-quicbaas#750, and the same shape
// as the signing-socket sweep #676).
//
// So startup sweeps. It is the one moment the daemon knows which containers
// could be its own and has none of its own running yet.
//
// WHAT MAKES THE SWEEP EXACT IS THE LABEL. Every container an agent backend
// launches carries ContainerOwnerLabel naming the daemon that launched it, and
// the sweep asks the runtime for exactly that label and removes exactly what
// comes back. A container this daemon did not launch has no such label and is
// never enumerated, let alone removed — the sweep removes only what it
// positively recognises.
//
// THE OWNER IS THE DAEMON'S OWN STATE DIRECTORY, which is what makes "its own"
// well defined across a restart and across co-tenancy. A restarted daemon has
// the same work directory and so recognises its predecessor's containers; two
// daemons on one host have different ones — sharing a work directory would
// already mean sharing credentials.json and the session records under it, and
// is not a supported configuration. This is the invariant the sweep rests on,
// and it is why the label is not simply "quicbaas".

// ContainerOwnerLabel names the daemon that launched a container. Its value is
// that daemon's work directory; see the file comment for why.
const ContainerOwnerLabel = "quicbaas.owner"

// ContainerSweep is what a startup sweep did, for the log line an operator
// reads. Removed is empty on the ordinary startup that found nothing.
type ContainerSweep struct {
	// Owner is the label value the sweep claimed containers by.
	Owner string

	// Removed lists the containers it removed, in the order the runtime
	// reported them.
	Removed []string
}

// ContainerOwnerSweeper is a container runtime that can find and remove the
// containers a previous run of this daemon left behind.
//
// It is a separate interface from ContainerRuntime because it is a startup
// concern rather than a session one, and because a runtime that cannot
// enumerate its own containers is still a perfectly good runtime for
// launching them.
type ContainerOwnerSweeper interface {
	// SweepOwned removes every container labelled with owner and reports
	// what it removed.
	SweepOwned(ctx context.Context, owner string) (ContainerSweep, error)
}

// SweepOwned removes every container this runtime's owner label claims.
//
// It is deliberately not "remove every quicbaas container": the name prefix
// says what launched a container, the label says WHICH daemon did, and only
// the second is safe to act on when a host runs more than one.
func (r *ExecContainerRuntime) SweepOwned(ctx context.Context, owner string) (ContainerSweep, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ContainerSweep{}, &ContainerSweepRefusedError{
			Reason: "the sweep claims containers by owner label and was given no owner; " +
				"an unowned sweep could only be a sweep of everything",
		}
	}

	ids, err := r.ownedContainers(ctx, owner)
	if err != nil {
		return ContainerSweep{}, err
	}

	sweep := ContainerSweep{Owner: owner}
	for _, id := range ids {
		if removeErr := r.Remove(ctx, id); removeErr != nil {
			// A container that is already gone is the outcome asked for. Any
			// other refusal is reported: a sweep that could not be completed
			// is not a sweep, and the caller decides whether to start anyway.
			var notFound *ContainerNotFoundError
			if !errors.As(removeErr, &notFound) {
				return sweep, &ContainerSweepError{Owner: owner, ContainerID: id, Cause: removeErr}
			}
		}
		sweep.Removed = append(sweep.Removed, id)
		r.logger.Infof("container sweep: removed orphaned container %s left by a previous run", truncateID(id))
	}
	return sweep, nil
}

// ownedContainers lists the ids, running or exited, that carry the owner
// label. Exited ones are included because a killed daemon's `--rm` never
// fired and the record is as much a leak as the process was.
func (r *ExecContainerRuntime) ownedContainers(ctx context.Context, owner string) ([]string, error) {
	cmd := exec.CommandContext(ctx, r.command,
		"ps", "-a", "--no-trunc", "--filter", "label="+ContainerOwnerLabel+"="+owner, "--format", "{{.ID}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &ContainerSweepError{
			Owner: owner,
			Cause: &CommandError{Command: r.command, Op: "ps", Output: strings.TrimSpace(stderr.String())},
		}
	}
	var ids []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

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
	"context"
	"errors"
	"time"
)

// containerReleaseTimeout bounds the removal a teardown issues. It runs on a
// context the spawn's or the close's cancellation cannot reach, because a
// cancelled spawn is exactly the case whose container must still go.
const containerReleaseTimeout = 2 * time.Minute

// SetGoBuildCache mounts the shared per-tenant Go module and build cache into
// every sandbox container this backend launches (go_build_cache.go). The zero
// value, the default, leaves each container its own caches.
func (b *ACPBackend) SetGoBuildCache(cache GoBuildCache) {
	b.goBuildCache = cache
}

// HoldsSession reports whether this backend holds a live session with this
// ID. It is what the container reaper asks before it removes a container: a
// container whose session is held is never touched.
func (b *ACPBackend) HoldsSession(sessionID string) bool {
	val, ok := b.sessions.Load(sessionID)
	if !ok {
		return false
	}
	sess, ok := val.(*acpSession)
	return ok && !sess.closed.Load()
}

// newContainerIdentity decides who a sandbox container runs for, BEFORE it is
// launched. registerID is the ID the session will be registered under: the
// resumed session's own ID, or a fresh one. The job comes from the request,
// or from the turn attribution when the request does not carry it.
func newContainerIdentity(ctx context.Context, req *SpawnRequest, registerID string) (containerIdentity, error) {
	sessionID := registerID
	if sessionID == "" {
		generated, err := generateSessionID()
		if err != nil {
			return containerIdentity{}, err
		}
		sessionID = generated
	}
	jobID := req.JobID
	if jobID == "" {
		jobID = TurnAttributionFromContext(ctx).JobID
	}
	return containerIdentity{
		SessionID: sessionID,
		JobID:     jobID,
		TenantID:  StorageTenant(ctx),
	}, nil
}

// releaseContainer ends an interactive sandbox container and reaps the
// runtime client that launched it. It is the one teardown every interactive
// container path uses: a failed handshake and an ordinary Close alike.
//
// THE CONTAINER IS REMOVED BY NAME, NOT BY KILLING ITS CLIENT. Killing the
// `docker run -i --rm` client only closes the container's stdin; --rm fires
// when the container's process exits, and an agent that does not exit on EOF
// — one mid-turn, or one waiting on its own MCP servers — keeps the container
// running with nothing left that knows it exists. That is how the burn-in node
// filled its disk. `rm -f` ends the container whatever its process is doing,
// and the client then exits on its own.
//
// The removal runs on a context detached from ctx's cancellation and bounded
// by containerReleaseTimeout. A container already gone is success.
func (b *ACPBackend) releaseContainer(ctx context.Context, containerID string, procCancel context.CancelFunc, wait func(context.Context) ContainerExit) error {
	var releaseErr error
	if containerID != "" && b.containerRuntime != nil {
		removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerReleaseTimeout)
		err := b.containerRuntime.Remove(removeCtx, containerID)
		cancel()
		var notFound *ContainerNotFoundError
		if err != nil && !errors.As(err, &notFound) {
			releaseErr = &ContainerStopError{ContainerID: containerID, Cause: err}
		}
	}
	if procCancel != nil {
		procCancel()
	}
	// The client was killed or its container removed; how it exited is not
	// the teardown's answer. Reaping it is the point: the wait reaps the
	// client in the background even when the bound below ends first.
	if exit := readContainerExit(wait); !exit.Known && b.logger != nil {
		b.logger.Debugf("container %s: runtime client exit not reported within %s", truncateID(containerID), containerExitWaitBound)
	}
	return releaseErr
}

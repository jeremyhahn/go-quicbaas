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
	"strconv"
)

// =============================================================================
// Placement versus routing
// =============================================================================
//
// THE RESOLVER ANSWERS TWO DIFFERENT QUESTIONS, and they must not share a
// fallback. PLACEMENT — where should a new session go — may pass over an agent
// that has gone quiet and choose another, or this daemon's own backend. ROUTING
// — where does an existing session live — has exactly one right answer, the
// host it was spawned on. A routing call that fell back to the local backend
// asked a backend that never held the session; its session-not-found was then
// read as "already gone", and a Close marked a session concluded while its
// remote container, and the forge token beside it, kept running
// (go-quicbaas#981 review). So placement is marked on its context, the
// resolver's freshness filter applies only there, and a remote session's
// operations reach its recorded host or fail with SessionHostUnavailableError.

// spawnPlacementKey marks a resolution made to place a new session.
type spawnPlacementKey struct{}

// WithSpawnPlacement marks ctx as placing a new session: the resolver may pass
// over agents that are not fit to take new work.
func WithSpawnPlacement(ctx context.Context) context.Context {
	return context.WithValue(ctx, spawnPlacementKey{}, true)
}

// SpawnPlacementFromContext reports whether ctx places a new session.
func SpawnPlacementFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(spawnPlacementKey{}).(bool)
	return v
}

// JobSlotHosts is the set of agent hosts on which a spawn's job already has a
// live session, and so already holds a slot (LiveJobsByHost counts a job once
// per host however many sessions it runs there).
type JobSlotHosts map[string]struct{}

// Holds reports whether the job already holds a slot on host.
func (h JobSlotHosts) Holds(host string) bool {
	_, ok := h[host]
	return ok
}

type jobSlotHostsKey struct{}

// WithJobSlotHosts marks ctx as placing a spawn whose job already holds a slot
// on each of hosts. Placement admits such a spawn on those hosts by health
// alone; an affinity by itself proves no slot and is admitted like any new job.
func WithJobSlotHosts(ctx context.Context, hosts JobSlotHosts) context.Context {
	if len(hosts) == 0 {
		return ctx
	}
	return context.WithValue(ctx, jobSlotHostsKey{}, hosts)
}

// JobSlotHostsFromContext returns the hosts WithJobSlotHosts recorded, or nil.
func JobSlotHostsFromContext(ctx context.Context) JobSlotHosts {
	if ctx == nil {
		return nil
	}
	hosts, _ := ctx.Value(jobSlotHostsKey{}).(JobSlotHosts)
	return hosts
}

// remoteSessionPinKey carries the remote session an operation is routed for.
type remoteSessionPinKey struct{}

// remoteSessionPin is the session an operation must reach on its own host.
type remoteSessionPin struct {
	sessionID string
	host      string
}

// withRemoteSessionPin marks ctx as routing an operation of a session that
// lives on a fleet node: it reaches that agent or fails, and never falls back
// to this daemon's backend table.
func withRemoteSessionPin(ctx context.Context, sessionID, host string) context.Context {
	return context.WithValue(ctx, remoteSessionPinKey{}, remoteSessionPin{sessionID: sessionID, host: host})
}

// remoteSessionPinFrom returns the pin ctx carries, if any.
func remoteSessionPinFrom(ctx context.Context) (remoteSessionPin, bool) {
	if ctx == nil {
		return remoteSessionPin{}, false
	}
	pin, ok := ctx.Value(remoteSessionPinKey{}).(remoteSessionPin)
	return pin, ok
}

// ErrSessionHostUnavailable is the sentinel for an operation on a remote
// session whose host cannot be reached.
var ErrSessionHostUnavailable = errors.New("orchestration: the session's host is unavailable")

// SessionHostUnavailableError reports an operation on a session that lives on
// a fleet node the resolver cannot reach. The session is NOT treated as gone:
// the agent may be alive behind a missed heartbeat, and only its own answer —
// or the reconciler's positive evidence of absence — may conclude it.
type SessionHostUnavailableError struct {
	// SessionID is the session.
	SessionID string

	// Host is the agent the session was spawned on.
	Host string
}

// Error implements the error interface.
func (e *SessionHostUnavailableError) Error() string {
	return ErrSessionHostUnavailable.Error() + ": session " + e.SessionID + " lives on agent " +
		strconv.Quote(e.Host) + ", which cannot be reached; it is not routed elsewhere"
}

// Unwrap returns the sentinel.
func (e *SessionHostUnavailableError) Unwrap() error { return ErrSessionHostUnavailable }

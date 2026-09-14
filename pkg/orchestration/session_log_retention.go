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

// DefaultSessionRetentionTurns is the number of most-recent turns a session
// transcript keeps when no tenant setting says otherwise.
//
// A turn is a prompt and everything the agent produced answering it, so 200 of
// them is a long working conversation — well past the point an operator scrolls
// — while bounding a single session's sub-keyspace to a few thousand keys. The
// bound matters because a transcript is append-only and a long-lived session
// has no other ceiling: without it one runaway agent loop grows a keyspace no
// operator ever reads and no process ever reclaims.
const DefaultSessionRetentionTurns = 200

// SessionRetention bounds how much of each session's transcript is kept.
//
// Retention is expressed in TURNS rather than events because a turn is the unit
// an operator reads: "the last 200 exchanges" is a promise about the
// conversation, whereas "the last 5000 events" is a promise about an encoding
// detail that changes whenever a backend becomes chattier about its tool use.
// The prune is still exact and cheap — the metadata record carries the sequence
// each retained turn began at, so dropping the oldest turn is a bounded delete
// rather than a scan.
type SessionRetention struct {
	// MaxTurns is the deployment-wide number of turns kept per session. Zero
	// or negative selects DefaultSessionRetentionTurns.
	MaxTurns int

	// PerTenant overrides MaxTurns for named tenants. A value of zero or less
	// for a tenant disables pruning for it entirely — an explicit choice to
	// keep everything, for a tenant under a legal hold.
	PerTenant map[string]int
}

// TurnsFor resolves the number of turns retained for one tenant. A tenant with
// no override gets MaxTurns; a deployment that set none gets the default.
//
// The tenant is normalised the same way the keyspace is, so a setting written
// for the system tenant applies to the untenanted in-process callers that are
// filed under it.
func (r SessionRetention) TurnsFor(tenantID string) int {
	if turns, ok := r.PerTenant[logTenant(tenantID)]; ok {
		return turns
	}
	if r.MaxTurns > 0 {
		return r.MaxTurns
	}
	return DefaultSessionRetentionTurns
}

// SessionRetentionError reports that a transcript could not be pruned to its
// retention bound. The append that triggered the prune has already succeeded —
// a session whose history grew one turn past its bound is a storage-accounting
// problem, not a lost turn — so the condition is reported rather than returned.
type SessionRetentionError struct {
	// TenantID and SessionID address the transcript that could not be pruned.
	TenantID  string
	SessionID string

	// Sequence is the event whose deletion failed.
	Sequence uint64

	// Cause is the underlying store failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionRetentionError) Error() string {
	return "orchestration session log: prune tenant=" + e.TenantID +
		" session=" + e.SessionID + ": " + e.Cause.Error()
}

// Unwrap exposes the store failure to errors.Is/As.
func (e *SessionRetentionError) Unwrap() error { return e.Cause }

// SessionTranscriptDeleteError reports that a session's transcript could not be
// removed. Unlike a prune failure this IS returned: a caller deleting a session
// asked for its conversation to be gone, and a partial delete that reported
// success would leave readable history behind a record that no longer exists.
type SessionTranscriptDeleteError struct {
	// TenantID and SessionID address the transcript.
	TenantID  string
	SessionID string

	// Key is the store key whose removal failed.
	Key string

	// Cause is the underlying store failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionTranscriptDeleteError) Error() string {
	return "orchestration session log: delete transcript tenant=" + e.TenantID +
		" session=" + e.SessionID + " key=" + e.Key + ": " + e.Cause.Error()
}

// Unwrap exposes the store failure to errors.Is/As.
func (e *SessionTranscriptDeleteError) Unwrap() error { return e.Cause }

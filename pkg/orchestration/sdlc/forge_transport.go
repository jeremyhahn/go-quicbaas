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

import "time"

// =============================================================================
// Forge transport policy
// =============================================================================
//
// ONE DEADLINE FOR EVERY CALL WAS WRONG IN BOTH DIRECTIONS. A listing that
// takes thirty seconds is a forge in trouble and worth asking again; a squash
// merge of a large repository that takes forty is a forge doing its job. Under
// the single bound the burn-in node lost a triage listing, a review read and a
// merge — and the merge had LANDED: Gitea finished the squash after the client
// had stopped waiting, and the job was recorded failed on a change that was on
// main (go-quicbaas#1034).
//
// So the policy is split by what a call costs the forge and by what repeating
// it would do. Reads are cheap and safe to repeat: a short deadline, retried a
// bounded number of times. Writes are not repeated blindly: a short one gets a
// longer deadline, a heavy one (a merge, a pull request's creation) the longest,
// and an unanswered one is settled by reading the forge's state — see the
// adapter's write-policy table.

// DefaultForgeTransport is the engine's defaults layer for ForgeTransport; a
// zero field in a configured policy takes the value here. The shipped node
// configuration states the same values and a drift test pins the two.
var DefaultForgeTransport = ForgeTransport{
	ReadTimeout:       30 * time.Second,
	WriteTimeout:      time.Minute,
	HeavyWriteTimeout: 5 * time.Minute,
	ReadAttempts:      3,
	RetryBackoff:      time.Second,
}

// ForgeTransport bounds how long a forge adapter waits on one call and how
// hard it presses a forge that did not answer.
//
// It is the NODE's policy rather than a binding's: how slow the forge may be is
// a property of the host it runs on and the load that host carries, which every
// project on that forge shares.
type ForgeTransport struct {
	// ReadTimeout bounds one attempt at a read.
	ReadTimeout time.Duration

	// WriteTimeout bounds a write the forge answers without heavy work: a
	// label, a comment, a commit status.
	WriteTimeout time.Duration

	// HeavyWriteTimeout bounds a write the forge does real work for before it
	// answers: merging a pull request, opening one.
	HeavyWriteTimeout time.Duration

	// ReadAttempts bounds how many times an unanswered read (or a write the
	// adapter has proven safe to repeat) is attempted.
	ReadAttempts int

	// RetryBackoff is the first wait between attempts; each retry doubles it.
	RetryBackoff time.Duration
}

// Resolved fills every zero field from DefaultForgeTransport.
func (t ForgeTransport) Resolved() ForgeTransport {
	out := t
	if out.ReadTimeout == 0 {
		out.ReadTimeout = DefaultForgeTransport.ReadTimeout
	}
	if out.WriteTimeout == 0 {
		out.WriteTimeout = DefaultForgeTransport.WriteTimeout
	}
	if out.HeavyWriteTimeout == 0 {
		out.HeavyWriteTimeout = DefaultForgeTransport.HeavyWriteTimeout
	}
	if out.ReadAttempts == 0 {
		out.ReadAttempts = DefaultForgeTransport.ReadAttempts
	}
	if out.RetryBackoff == 0 {
		out.RetryBackoff = DefaultForgeTransport.RetryBackoff
	}
	return out
}

// Validate refuses a negative field. Zero is not refused: it takes the
// default. A negative deadline would cancel every call before it was made, and
// a negative budget would attempt nothing.
func (t ForgeTransport) Validate() error {
	fields := []struct {
		name     string
		negative bool
	}{
		{"forge.read_timeout", t.ReadTimeout < 0},
		{"forge.write_timeout", t.WriteTimeout < 0},
		{"forge.heavy_write_timeout", t.HeavyWriteTimeout < 0},
		{"forge.read_attempts", t.ReadAttempts < 0},
		{"forge.retry_backoff", t.RetryBackoff < 0},
	}
	for _, field := range fields {
		if field.negative {
			return &InvalidInputError{Field: field.name, Message: "must not be negative; zero takes the default"}
		}
	}
	return nil
}

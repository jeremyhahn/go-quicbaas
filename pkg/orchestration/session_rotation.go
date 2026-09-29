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

import "errors"

// =============================================================================
// Session rotation policy — what a worker session does when its account rotates
//
// A persistent worker session materializes its pooled provider credential once,
// at spawn. When the pool replaces that credential (a host refresh, an OAuth
// renewal, a new version of the account's secret) the session is holding a copy
// the provider no longer honours, and it has to be replaced before its next
// turn (SessionAccountFreshness, go-quicbaas#997). What the replacement keeps is
// the project's choice:
//
//   - resume: the replacement continues the SAME conversation. The session's
//     transcript is carried into it, so the worker picks up where it was.
//   - fresh: the replacement starts a new conversation on the same branch and
//     is told only why it is new.
//
// Resume is the default (owner ruling, 2026-09-24): a rotation is the pool's
// doing, not the work's, so it should cost the work nothing — including the
// context the worker had built up. Fresh exists for a project whose sessions
// are better restarted clean, for example one whose turns routinely run close
// to the context limit, where a replayed transcript would crowd the next turn.
// =============================================================================

// SessionRotationPolicy names what a persistent worker session does when the
// pooled provider account under it rotates.
type SessionRotationPolicy string

const (
	// SessionRotationUnset is the zero value. It means SessionRotationResume;
	// see SessionRotationPolicy.Effective.
	SessionRotationUnset SessionRotationPolicy = ""

	// SessionRotationResume continues the same conversation in the
	// replacement session.
	SessionRotationResume SessionRotationPolicy = "resume"

	// SessionRotationFresh starts the replacement session on a new
	// conversation on the same branch.
	SessionRotationFresh SessionRotationPolicy = "fresh"
)

// DefaultSessionRotation is the policy a project that never chose one runs.
const DefaultSessionRotation = SessionRotationResume

// sessionRotationPolicies is the set of policies a project record may name.
// An unrecognized value is refused rather than read as the default, so a typo
// in "fresh" cannot silently leave a project resuming.
var sessionRotationPolicies = map[SessionRotationPolicy]struct{}{
	SessionRotationUnset:  {},
	SessionRotationResume: {},
	SessionRotationFresh:  {},
}

// Validate refuses a policy this daemon does not know.
func (p SessionRotationPolicy) Validate() error {
	if _, ok := sessionRotationPolicies[p]; ok {
		return nil
	}
	return &SessionRotationPolicyInvalidError{Policy: string(p)}
}

// Effective is the policy in force: the stored one, or the default when the
// project never set one.
func (p SessionRotationPolicy) Effective() SessionRotationPolicy {
	if p == SessionRotationUnset {
		return DefaultSessionRotation
	}
	return p
}

// ErrSessionRotationPolicyInvalid indicates a project named a session rotation
// policy that does not exist.
var ErrSessionRotationPolicyInvalid = errors.New("project: invalid session rotation policy")

// SessionRotationPolicyInvalidError reports an unrecognized session rotation
// policy.
type SessionRotationPolicyInvalidError struct {
	// Policy is the value that was refused.
	Policy string
}

// Error implements the error interface.
func (e *SessionRotationPolicyInvalidError) Error() string {
	return ErrSessionRotationPolicyInvalid.Error() + ": " + `"` + e.Policy + `"` +
		` (want "` + string(SessionRotationResume) + `" or "` + string(SessionRotationFresh) + `")`
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionRotationPolicyInvalidError) Unwrap() error { return ErrSessionRotationPolicyInvalid }

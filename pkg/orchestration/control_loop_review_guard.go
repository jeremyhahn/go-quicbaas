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

// resolvedReviewBackend is the backend the review prompt would actually run on.
//
// It mirrors handleReview's routing exactly — the configured reviewer first,
// then the lead's backend for either of the lead fallbacks — because a guard
// that models the routing loosely is a guard that passes the case it exists to
// catch. The reviewer SESSION is reported through cl.reviewerBackend, which is
// set beside it when the session is spawned.
func (cl *ControlLoop) resolvedReviewBackend() string {
	if cl.reviewerBackend != "" {
		return cl.reviewerBackend
	}
	// THE WORKFLOW'S REVIEWER ROLE COUNTS EVEN WHEN NO SESSION WAS SPAWNED.
	// cl.reviewerBackend is set beside the reviewer SESSION, so a job that has
	// not reached that spawn — or whose reviewer role lacks the
	// persistent_sessions capability — would otherwise be judged on the lead's
	// backend here and refused for a reviewer it does have. Reading the spec is
	// what makes the guard agree with the configuration rather than with how far
	// the run happens to have got.
	if spec := cl.resolveNodeSpec("reviewer"); spec != nil && spec.Backend != "" {
		return spec.Backend
	}
	// No reviewer at all: both remaining routes end on the lead's backend — the
	// lead's own session, or a stateless prompt that takes LeadBackend.
	if cl.job != nil && cl.job.Config != nil {
		return cl.job.Config.LeadBackend
	}
	return ""
}

// ReviewBackendRefusedError is a review that would have run on a backend the
// owner has excluded from reviewing.
//
// Claude Code writes this fleet's code, and a reviewer from the same family
// cannot find what the author's own reasoning missed — which is the property
// ReviewPolicy.RequireIndependentReviewer enforces on the SDLC path and this
// path had no equivalent of. Owner ruling 2026-09-13.
type ReviewBackendRefusedError struct {
	JobID   string
	Backend string
}

func (e *ReviewBackendRefusedError) Error() string {
	return "orchestration: job " + e.JobID + " would review on backend " + e.Backend +
		", which does not review in this fleet: a review runs on codex or on the local " +
		"qwen model. Configure a \"reviewer\" role on one of those."
}

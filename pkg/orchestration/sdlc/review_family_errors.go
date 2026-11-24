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

// ReviewerFamilyRefusedError is a reviewer from a model family that does not
// review in this fleet.
//
// It is distinct from ReviewerNotIndependentError, which compares the reviewer
// against the WORKER and is gated on ReviewPolicy.RequireIndependentReviewer.
// This one is unconditional: Claude Code writes this fleet's code, and no
// policy setting admits it as a judge of its own family's work (owner ruling
// 2026-09-13). A binding that names one is refused before a model is spent.
type ReviewerFamilyRefusedError struct {
	ProjectID string
	Agent     string
	Family    string
}

func (e *ReviewerFamilyRefusedError) Error() string {
	return "sdlc: project " + e.ProjectID + " binds reviewer agent " + e.Agent +
		" (model family " + e.Family + "), which does not review in this fleet: a review runs " +
		"on codex or on the local qwen model. Change the seat's agent_id."
}

// Unwrap returns ErrReview, as ReviewerNotIndependentError does: both refuse a
// review seat whose verdict could never count.
func (e *ReviewerFamilyRefusedError) Unwrap() error { return ErrReview }

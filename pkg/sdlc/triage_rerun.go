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

import (
	"context"
	"time"
)

// =============================================================================
// The rerun mark (go-quicbaas#894)
// =============================================================================
//
// A triage run lists the project's issues ONCE, as it starts. A nudge that
// arrives while the run holds the project's lease cannot start a job of its
// own, and the run in flight has already listed: the issue the nudge was about
// would wait for the periodic sweep, silently. So the losing nudge leaves a
// mark, and the run takes it — before it lists, where the listing satisfies
// it, and after it cleans up, where taking one sends the same job round again
// under the lease it already holds.
//
// The mark is one record per project, written only where none exists and
// removed only as read, so two nudges leave one mark and a take never removes
// a mark set after it read.

const triageRerunEntityType = "sdlc_triage_rerun"

// triageRerunRounds bounds how many times one job lists again because nudges
// kept arriving. Past it the mark is left for the next nudge or sweep, so a
// busy project cannot hold its lease for ever.
const triageRerunRounds = 3

// triageRerunMark is the stored mark.
type triageRerunMark struct {
	ProjectID string    `json:"project_id"`
	MarkedAt  time.Time `json:"marked_at"`
}

// triageRerunKey is where a project's mark lives.
func triageRerunKey(projectID string) string {
	return recordKey(triageRerunEntityType, entityIDFor(projectID))
}

// The Store keeps the rerun marks.
var _ TriageRerunMarks = (*Store)(nil)

// MarkTriageRerun records that the project wants triaging again. A mark that
// is already there is success.
func (s *Store) MarkTriageRerun(ctx context.Context, tenantID, projectID string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "MarkTriageRerun", tenantID)
	if err != nil {
		return err
	}
	mark := &triageRerunMark{ProjectID: projectID, MarkedAt: time.Now().UTC()}
	_, err = storeRecord(ctx, "MarkTriageRerun", store, triageRerunKey(projectID), nil, mark)
	return err
}

// TakeTriageRerun removes the project's mark and reports whether there was
// one. A mark replaced between the read and the remove is a new mark, and is
// taken in its turn.
func (s *Store) TakeTriageRerun(ctx context.Context, tenantID, projectID string) (bool, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return false, err
	}
	store, err := s.guardedKV(ctx, "TakeTriageRerun", tenantID)
	if err != nil {
		return false, err
	}
	for {
		held, changed, err := removeRecord(ctx, "TakeTriageRerun", store, triageRerunKey(projectID), func(m *triageRerunMark) bool {
			return m.ProjectID == projectID
		})
		if err != nil {
			return false, err
		}
		if !changed {
			return held != nil, nil
		}
		if err := ctx.Err(); err != nil {
			return false, backendError("TakeTriageRerun", err)
		}
	}
}

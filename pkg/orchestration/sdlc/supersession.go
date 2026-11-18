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

package sdlc

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Supersession — the CI runs a moved head left behind
// =============================================================================
//
// A pull request whose head moves leaves the previous head's CI runs behind.
// Head pinning already stops the engine ACTING on one: the gate counts only
// the checks bound to the delivery's head, and Complete refuses on a head that
// moved. A displaced run is therefore waste rather than incorrectness, and for
// a long time that was the argument for recording nothing about it.
//
// It is the wrong argument, and the measurement that settles it is from
// 2026-09-12: a displaced run is still VISIBLE on the forge, and on Gitea
// re-running one cancels the run on the REAL head. Two lanes found a run on an
// older head, re-ran it, and paid a full CI cycle each. Nothing about the run
// said it was obsolete, because nothing had ever written that down.
//
// So every displacement is recorded on the Delivery, together with what the
// engine was actually able to do about it:
//
//   - finished  — the run had already completed, so it consumes nothing. It is
//     recorded anyway: a completed run on a stale head is precisely
//     the one a reader finds, believes, and re-runs.
//   - cancelled — the forge stopped it and its capacity is back.
//   - orphaned  — it is still running and this forge publishes no way to stop
//     it. That is Gitea, and it is the case ForgeCapabilities
//     exists to express: the engine records an orphan rather than
//     claiming a cancellation it never made.
//
// THE RECORD IS THE POINT, NOT THE CANCELLATION. A cancel is attempted where
// the platform has one and its failure is recorded rather than returned,
// because a review gate must not fail on a courtesy call the forge refused.
// What must never happen is a displacement that leaves no trace: that is the
// state the two lanes were reading when they re-ran a stale run.

// SupersessionDisposition says what became of a displaced CI run.
type SupersessionDisposition string

const (
	// SupersessionFinished is a run that had already completed when the head
	// moved past it. It holds no capacity and is recorded for the reader.
	SupersessionFinished SupersessionDisposition = "finished"

	// SupersessionCancelled is a run the forge stopped on request.
	SupersessionCancelled SupersessionDisposition = "cancelled"

	// SupersessionOrphaned is a run still in flight that nothing will stop:
	// either the forge publishes no cancel route or the cancel was refused.
	SupersessionOrphaned SupersessionDisposition = "orphaned"
)

// String returns the disposition's name.
func (d SupersessionDisposition) String() string { return string(d) }

// Leaks reports whether the run is still consuming CI capacity that nothing
// will reclaim. It is what the outcome comment surfaces: a finished or
// cancelled displacement is bookkeeping, an orphan is a runner nobody freed.
func (d SupersessionDisposition) Leaks() bool { return d == SupersessionOrphaned }

// SupersessionRecord is one CI run the delivery's head moved past.
//
// It is the type the forge seam's comments have named since the seam was
// written — ForgeBackend's three rules, ForgeCapabilities.CancelCheckRuns and
// the Gitea adapter's own Capabilities all describe it — and until now it did
// not exist. A declared capability whose only implementation returns
// ErrForgeUnsupported, described by prose naming a type nobody wrote, reads as
// implemented when it is not.
type SupersessionRecord struct {
	// Name is the check context the run reported under.
	Name string `json:"name"`

	// ForgeRunID identifies the run on the forge, empty on a platform whose
	// checks carry no addressable id. IT IS THE FIELD A READER MATCHES: the
	// run somebody is about to re-run is named by its id, not by its context.
	ForgeRunID string `json:"forge_run_id,omitempty"`

	// HeadSHA is the head the run was started for and SupersededBy the head
	// that displaced it. Both are kept because the pair is the whole fact: a
	// run on a head is not stale, a run on a head something else replaced is.
	HeadSHA      string `json:"head_sha"`
	SupersededBy string `json:"superseded_by"`

	// Status is what the run was doing at the moment it was displaced.
	Status CheckStatus `json:"status"`

	// Disposition says what became of it.
	Disposition SupersessionDisposition `json:"disposition"`

	// Detail says WHY the disposition is what it is, for an orphan in
	// particular: the platform has no cancel route, or the cancel was
	// refused and this is what it said. Empty when the disposition speaks for
	// itself.
	Detail string `json:"detail,omitempty"`

	// ObservedAt is when the engine noticed the displacement.
	ObservedAt time.Time `json:"observed_at"`
}

// Identity names one displacement, so a delivery re-read and re-applied never
// records the same one twice. The run id alone is not enough: a forge whose
// checks carry no id would collapse every context into one record.
func (r SupersessionRecord) Identity() string {
	return r.HeadSHA + "\x00" + r.SupersededBy + "\x00" + r.Name + "\x00" + r.ForgeRunID
}

// Describe renders the record as one line for a person reading an issue.
func (r SupersessionRecord) Describe() string {
	line := "`" + r.Name + "`"
	if r.ForgeRunID != "" {
		line += " (run `" + r.ForgeRunID + "`)"
	}
	line += " on " + shortSHA(r.HeadSHA) + ", " + r.Disposition.String()
	if r.Detail != "" {
		line += ": " + r.Detail
	}
	return line
}

// MaxSupersededRuns bounds how many displacements one delivery keeps.
//
// A bound is required rather than tidy: the list grows with every pushed head
// times every check context, the Delivery is one guarded write, and an attempt
// that pushed forty times would carry its whole CI history into every update.
// The NEWEST are kept, because the run a reader is looking at is the one they
// just found on the forge.
const MaxSupersededRuns = 32

// RecordSupersession folds displacements onto the delivery, deduplicated by
// identity and bounded by MaxSupersededRuns, and reports whether anything was
// added. Nothing added means nothing to write, which is what keeps a re-read
// after a version conflict from producing an endless update.
func (d *Delivery) RecordSupersession(records []SupersessionRecord) bool {
	if d == nil || len(records) == 0 {
		return false
	}
	known := make(map[string]bool, len(d.SupersededRuns))
	for _, held := range d.SupersededRuns {
		known[held.Identity()] = true
	}
	added := false
	for _, record := range records {
		if record.HeadSHA == "" || record.SupersededBy == "" || known[record.Identity()] {
			continue
		}
		known[record.Identity()] = true
		d.SupersededRuns = append(d.SupersededRuns, record)
		added = true
	}
	if !added {
		return false
	}
	if excess := len(d.SupersededRuns) - MaxSupersededRuns; excess > 0 {
		d.SupersededRuns = append(d.SupersededRuns[:0], d.SupersededRuns[excess:]...)
	}
	return true
}

// SupersededRun returns what the delivery knows about a forge run id.
//
// IT IS THE STALE-RERUN GUARD. A caller about to re-run a CI run asks this
// first, because on a forge that keys a re-run to the workflow rather than to
// the commit — Gitea does — re-running a displaced run cancels the run on the
// real head. The newest record for the id wins: a context re-run across
// several heads has several records and only the last one describes where the
// delivery actually is.
func (d *Delivery) SupersededRun(forgeRunID string) (SupersessionRecord, bool) {
	if d == nil || strings.TrimSpace(forgeRunID) == "" {
		return SupersessionRecord{}, false
	}
	for i := len(d.SupersededRuns) - 1; i >= 0; i-- {
		if d.SupersededRuns[i].ForgeRunID == forgeRunID {
			return d.SupersededRuns[i], true
		}
	}
	return SupersessionRecord{}, false
}

// OrphanedRuns returns the displacements still holding CI capacity, newest
// first. It is what the outcome comment names, so a person handed a parked
// change is told which runs are still burning a runner and must not be
// re-run.
func (d *Delivery) OrphanedRuns() []SupersessionRecord {
	if d == nil {
		return nil
	}
	orphans := make([]SupersessionRecord, 0, len(d.SupersededRuns))
	for _, record := range d.SupersededRuns {
		if record.Disposition.Leaks() {
			orphans = append(orphans, record)
		}
	}
	sort.SliceStable(orphans, func(i, j int) bool {
		return orphans[i].ObservedAt.After(orphans[j].ObservedAt)
	})
	return orphans
}

// supersedeChecks builds the record for every check the delivery holds on a
// head newHead replaces, attempting a cancel on whatever is still running.
//
// IT RETURNS NO ERROR, AND THAT IS THE DESIGN. Every call site is a review
// round that has already read the forge and is about to judge a head; failing
// that round because a courtesy cancel was refused would turn a bookkeeping
// problem into a stalled delivery. A cancel that cannot be made or will not be
// made is an ORPHAN, which is a fact worth recording and not an error worth
// propagating — exactly what ForgeCapabilities.CancelCheckRuns was declared
// for.
//
// An empty answer is the common case: a head that did not move supersedes
// nothing, and neither does a delivery with no recorded checks.
func supersedeChecks(ctx context.Context, forge ForgeBackend, repo RepoRef, delivery *Delivery, newHead string, now time.Time) []SupersessionRecord {
	if delivery == nil {
		return nil
	}
	previous := strings.TrimSpace(delivery.HeadSHA)
	next := strings.TrimSpace(newHead)
	if previous == "" || next == "" || previous == next {
		return nil
	}
	cancellable := forge != nil && forge.Capabilities().CancelCheckRuns
	records := make([]SupersessionRecord, 0, len(delivery.Checks))
	for _, check := range delivery.Checks {
		if check.HeadSHA != previous {
			continue
		}
		record := SupersessionRecord{
			Name:         check.Name,
			ForgeRunID:   check.ForgeRunID,
			HeadSHA:      previous,
			SupersededBy: next,
			Status:       check.Status,
			ObservedAt:   now.UTC(),
		}
		record.Disposition, record.Detail = disposeRun(ctx, forge, repo, delivery, check, cancellable)
		records = append(records, record)
	}
	return records
}

// disposeRun decides what became of one displaced check and why.
func disposeRun(ctx context.Context, forge ForgeBackend, repo RepoRef, delivery *Delivery, check CheckRecord, cancellable bool) (SupersessionDisposition, string) {
	if check.Status.Finished() {
		return SupersessionFinished, ""
	}
	// THE GUARD APPLIES TO THE ENGINE TOO, and this is the one place the
	// engine acts on a run by id. A forge that reuses a run id across heads —
	// Gitea's commit statuses are numbered per repository, not per commit —
	// hands back an id this delivery has ALREADY displaced once. Cancelling it
	// a second time is the failure this whole file exists for, committed by
	// the engine rather than by a person: the id now addresses the run on the
	// current head, and stopping that is the full CI cycle two lanes paid for
	// on 2026-09-12.
	if prior, displaced := delivery.SupersededRun(check.ForgeRunID); displaced {
		return SupersessionOrphaned, "run `" + check.ForgeRunID + "` is already recorded as superseded on " +
			shortSHA(prior.HeadSHA) + "; cancelling it again would stop the run on the current head"
	}
	if !cancellable {
		detail := "this forge publishes no route to cancel a running check"
		if forge != nil {
			detail = string(forge.Type()) + " publishes no route to cancel a running check"
		}
		return SupersessionOrphaned, detail
	}
	if check.ForgeRunID == "" {
		return SupersessionOrphaned, "the forge reported no run id to cancel"
	}
	// THE ERROR IS RECORDED, NOT RETURNED. A refused cancel leaves a run in
	// flight, which is exactly what an orphan is; reporting it up would fail
	// the review round that was only passing through.
	if err := forge.CancelCheckRun(ctx, repo, check.ForgeRunID); err != nil {
		return SupersessionOrphaned, "the cancel was refused: " + err.Error()
	}
	return SupersessionCancelled, ""
}

// supersessionLine renders the orphaned displacements for an outcome comment,
// empty when there are none.
//
// ONLY THE ORPHANS. A finished or cancelled displacement is recorded for a
// reader who goes looking; an orphan is a runner still burning and the one
// thing a person handed a parked change has to be told without looking.
func supersessionLine(delivery *Delivery) string {
	orphans := delivery.OrphanedRuns()
	if len(orphans) == 0 {
		return ""
	}
	described := make([]string, 0, len(orphans))
	for _, orphan := range orphans {
		described = append(described, orphan.Describe())
	}
	return "**Orphaned CI runs (" + strconv.Itoa(len(orphans)) +
		", do not re-run — a re-run cancels the run on the current head):** " +
		strings.Join(described, "; ")
}

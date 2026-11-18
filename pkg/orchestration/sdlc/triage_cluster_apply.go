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
	"errors"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Admitting ready issues — alone, or as one cluster
// =============================================================================
//
// A READY VERDICT IS NOT APPLIED THE MOMENT IT IS REACHED. It is held until
// every candidate of the pass has been judged, because whether an issue should
// be its own task depends on which OTHER issues turned out ready beside it.
// The cost of holding is one pass: a job that dies before admitting leaves the
// issues untriggered and unlabelled, which the next pass judges again — the
// recoverable direction.

// auditTriageClustered records a cluster triage formed.
const auditTriageClustered = "sdlc:triage_clustered"

// deferReady holds a ready verdict for admission at the end of the pass. The
// hold on the issue was already ruled out by apply.
func (s *triageRun) deferReady(_ context.Context, issue ForgeIssue, _ TriageVerdict, _ string) error {
	s.ready = append(s.ready, issue)
	return nil
}

// admitReady admits every issue judged ready this pass: related ones together
// as one cluster, the rest alone.
//
// EACH ADMISSION IS ITS OWN, for the reason judge treats each issue as its own:
// one issue whose label will not stick must not cost the others their
// admission. A failure is recorded against the issues it concerns.
func (s *triageRun) admitReady(ctx context.Context) {
	if len(s.ready) == 0 {
		return
	}
	byNumber := make(map[int]ForgeIssue, len(s.ready))
	candidates := make([]ClusterCandidate, 0, len(s.ready))
	for _, issue := range s.ready {
		byNumber[issue.Number] = issue
		candidates = append(candidates, ClusterCandidate{Issue: issue.Number, Footprint: s.footprints[issue.Number]})
	}
	for _, planned := range PlanClusters(candidates, s.binding.Clustering.Size()) {
		if err := ctx.Err(); err != nil {
			return
		}
		var err error
		if len(planned.Members) == 0 {
			err = s.applyReady(ctx, byNumber[planned.Anchor])
		} else {
			err = s.applyCluster(ctx, planned, byNumber)
		}
		if err != nil {
			for _, number := range planned.Issues() {
				s.job.Audit("sdlc:triage_failed", "issue="+strconv.Itoa(number)+" error="+err.Error())
			}
			s.job.Emit(orchestration.JobStateExecuting,
				"issue(s) "+issueList(planned.Issues())+" could not be admitted: "+err.Error())
		}
	}
}

// applyCluster admits a cluster: the record first, then every member, then
// the anchor's trigger label LAST.
//
// THE ORDER IS THE RECOVERY. The record goes first so that the moment the
// trigger lands — which is when the issue source may adopt the anchor — the
// anchor's contract already carries its members. The trigger goes last so
// that a pass which dies half way leaves an anchor that is not yet triggered,
// which the next pass re-judges and re-clusters, rather than a task that was
// adopted without the members it was meant to cover.
func (s *triageRun) applyCluster(ctx context.Context, planned PlannedCluster, issues map[int]ForgeIssue) error {
	record := &IssueCluster{
		ProjectID: s.binding.ProjectID,
		Anchor:    planned.Anchor,
		Members:   append([]int(nil), planned.Members...),
		Shared:    append([]string(nil), planned.Shared...),
		Head:      s.headSHA,
	}
	if err := s.runner.clusters.PutIssueCluster(ctx, s.job.TenantID, record); err != nil {
		return &TriageVerdictError{Issue: planned.Anchor, Reason: "the cluster could not be recorded", Cause: err}
	}
	for _, number := range planned.Members {
		if err := s.admitMember(ctx, issues[number], planned); err != nil {
			return err
		}
	}
	anchor := issues[planned.Anchor]
	if _, err := s.forge.CommentOnIssue(ctx, s.repo, anchor.Number, s.clusterComment(clusterAnchorComment(planned))); err != nil {
		return &TriageVerdictError{Issue: anchor.Number, Reason: "the cluster could not be announced", Cause: err}
	}
	if err := s.applyReady(ctx, anchor); err != nil {
		return err
	}
	s.job.Audit(auditTriageClustered, "anchor="+strconv.Itoa(planned.Anchor)+
		" members="+formatClusterIssues(planned.Members)+" shared="+strings.Join(planned.Shared, ","))
	s.job.Emit(orchestration.JobStateExecuting,
		"issues "+issueList(planned.Issues())+" admitted as one task under #"+strconv.Itoa(planned.Anchor))
	return nil
}

// admitMember tells a member which issue carries its work and labels it.
//
// A MEMBER NEVER GETS THE TRIGGER, and that is what keeps it from being
// adopted on its own: the issue source adopts triggered issues, and this one's
// work is the anchor's task. needs-info is cleared for the reason applyReady
// clears it: the verdict that stands is ready.
func (s *triageRun) admitMember(ctx context.Context, issue ForgeIssue, planned PlannedCluster) error {
	if _, err := s.forge.CommentOnIssue(ctx, s.repo, issue.Number, s.clusterComment(clusterMemberComment(planned))); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the member could not be told where its work is", Cause: err}
	}
	if needsInfo := s.binding.Forge.Label(LabelNeedsInfo); needsInfo != "" && issueHasLabel(issue, needsInfo) {
		if err := s.forge.RemoveIssueLabel(ctx, s.repo, issue.Number, needsInfo); err != nil {
			return &TriageVerdictError{Issue: issue.Number, Reason: "the needs-info label could not be cleared", Cause: err}
		}
	}
	label := s.binding.Forge.Label(LabelClustered)
	if label == "" || issueHasLabel(issue, label) {
		// A binding that displays no clustered label keeps the comment as the
		// record, which is the part a person reads; the cluster record is
		// what the module decides from either way.
		return nil
	}
	if err := s.forge.AddIssueLabels(ctx, s.repo, issue.Number, []string{label}); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the clustered label could not be applied", Cause: err}
	}
	return nil
}

// clusterComment tags a cluster comment as triage's own word on the issue, at
// the head the facts were measured against.
func (s *triageRun) clusterComment(body string) string {
	return body + s.verdictMarker(clusteredVerdictCode)
}

// clusteredVerdictCode is the verdict a cluster comment records. It is not a
// TriageOutcome: it names no judgement about the issue, only where its work
// went, and no pass re-reads it as one — a clustered member is kept out of
// triage by its record, not by its comment.
const clusteredVerdictCode = "clustered"

// clusteredElsewhere reports whether an issue is a MEMBER of a live cluster,
// and so is not triage's to judge: its work is its anchor's task.
//
// A CLUSTER WHOSE ANCHOR HAS CLOSED IS DISSOLVED HERE. The anchor's pull
// request closes every member when it merges, so a member still open after its
// anchor closed was left behind — the change was abandoned, or the anchor was
// closed by hand. Either way the member is owed a verdict of its own, and it
// gets one on this pass. A dissolve that fails leaves the member skipped and
// is recorded; the next pass tries again.
func (s *triageRun) clusteredElsewhere(ctx context.Context, number int) (bool, error) {
	cluster, err := s.runner.clusters.GetIssueCluster(ctx, s.job.TenantID, s.binding.ProjectID, number)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, &TriageVerdictError{Issue: number, Reason: "the issue's cluster could not be read", Cause: err}
	}
	if cluster.Anchor == number {
		// The anchor is judged as any issue is; its trigger label is what
		// keeps a live one out of triage.
		return false, nil
	}
	if s.isOpenCandidate(cluster.Anchor) {
		s.job.Audit("sdlc:triage_skipped", "issue="+strconv.Itoa(number)+
			" reason=clustered into #"+strconv.Itoa(cluster.Anchor))
		return true, nil
	}
	if err := s.runner.clusters.DeleteIssueCluster(ctx, s.job.TenantID, cluster); err != nil {
		return false, &TriageVerdictError{Issue: number, Reason: "the cluster of a closed anchor could not be dissolved", Cause: err}
	}
	s.job.Audit("sdlc:triage_cluster_dissolved", "issue="+strconv.Itoa(number)+
		" anchor="+strconv.Itoa(cluster.Anchor)+" reason=the anchor is closed and this member is still open")
	return false, nil
}

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
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Issue clustering — related ready issues become one task and one pull request
// =============================================================================
//
// WHY. Every pull request pays a pipeline, a review and a merge. A backlog of
// small issues against one package paid all three once per issue, and each of
// them made the next one stale on the way in. Batching at PLANNING time — one
// task, one branch, one pull request that closes every issue in it — pays them
// once.
//
// HOW RELATED IS DECIDED, AND WHY IT COSTS NO MODEL TURN. Triage already
// measures, in its own checkout, the files an issue names and the files the
// identifiers it names are defined in (triage_facts.go). Those files and the
// directories that hold them are the issue's FOOTPRINT: the part of the tree
// the change will most likely touch. Two ready issues whose footprints share a
// file or a package are clustered. Nothing is asked of a model; the evidence is
// what git said.
//
// WHAT A CLUSTER IS ON THE FORGE. The lowest-numbered issue is the ANCHOR: it
// alone gets the trigger label, so the issue source adopts exactly one task,
// named after it. Every other issue is a MEMBER: it gets a comment naming the
// anchor and the clustered label, and never the trigger. The task's contract
// carries every member's text, its pull request closes every member, and a
// member is never adopted on its own.
//
// WHAT A CLUSTER IS IN THE MODULE. An IssueCluster record, one copy per issue,
// so "is this issue clustered, and into what" is one read whichever issue asks.
// Labels are display: the record is what the issue source and triage decide
// from.

// DefaultClusterMaxSize is how many issues one cluster may cover when the
// binding says nothing.
//
// FOUR, because a cluster is still ONE change a reviewer must read and one
// change a red pipeline sends back whole: past a handful, the saving on
// pipelines is spent again on a review nobody can hold in their head.
const DefaultClusterMaxSize = 4

// MaxClusterSize bounds what a binding may configure.
const MaxClusterSize = 10

// ClusteringPolicy is a project's issue-clustering configuration.
//
// ON BY DEFAULT, for the reason MergeTrainPolicy is: the zero value a binding
// written before clustering existed carries is the default.
type ClusteringPolicy struct {
	// Disabled turns clustering off; every ready issue becomes its own task.
	Disabled bool `json:"disabled,omitempty"`

	// MaxSize bounds how many issues one cluster covers, the anchor
	// included. Zero takes DefaultClusterMaxSize.
	MaxSize int `json:"max_size,omitempty"`
}

// Size is how many issues one task may cover: one when clustering is off.
func (p ClusteringPolicy) Size() int {
	switch {
	case p.Disabled:
		return 1
	case p.MaxSize <= 0:
		return DefaultClusterMaxSize
	default:
		return p.MaxSize
	}
}

// Validate refuses a bound outside 0..MaxClusterSize.
func (p ClusteringPolicy) Validate() error {
	if p.MaxSize < 0 || p.MaxSize > MaxClusterSize {
		return &InvalidInputError{
			Field:   "clustering.max_size",
			Message: "must be between 1 and " + strconv.Itoa(MaxClusterSize) + ", or zero for the default",
		}
	}
	return nil
}

// MetadataClusterIssues is the task metadata key listing the MEMBER issues a
// clustered task also closes, comma-separated, in issue order. The anchor is
// the task's own issue and is not repeated here.
const MetadataClusterIssues = "cluster_issues"

// =============================================================================
// Footprints and the plan
// =============================================================================

// Footprint key prefixes. A file and a package are different kinds of
// overlap and are kept apart, so a directory named like a file cannot match
// one.
const (
	footprintFile    = "file:"
	footprintPackage = "pkg:"
)

// TriageFootprint is the part of the tree an issue's measured facts point at:
// every file it names or whose identifiers it names, and the directory each
// sits in, sorted.
//
// THE REPOSITORY ROOT IS NOT A PACKAGE. Every top-level file — the Makefile,
// the README, go.mod — sits in it, and treating it as shared ground would
// cluster any two issues that mention any two top-level files. Two issues
// naming the SAME top-level file still overlap on the file itself.
//
// A path the checkout does not have still counts. An issue asking for a new
// file in an existing package is about that package, which is exactly the
// overlap clustering is looking for.
func TriageFootprint(facts TriageFacts) []string {
	keys := map[string]bool{}
	add := func(file string) {
		file = strings.TrimPrefix(path.Clean(strings.TrimSpace(file)), "./")
		if file == "" || file == "." || strings.HasPrefix(file, "../") {
			return
		}
		keys[footprintFile+file] = true
		if dir := path.Dir(file); dir != "." && dir != "/" {
			keys[footprintPackage+dir] = true
		}
	}
	for _, fact := range facts.Paths {
		add(fact.Subject)
	}
	for _, fact := range facts.Identifiers {
		for _, match := range fact.Matches {
			// git grep -n answers "path:line:text".
			if file, _, ok := strings.Cut(match, ":"); ok {
				add(file)
			}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// ClusterCandidate is one ready issue offered to the planner.
type ClusterCandidate struct {
	Issue     int
	Footprint []string
}

// PlannedCluster is one group the planner formed: the anchor, the members,
// and the footprint keys that tied them together.
type PlannedCluster struct {
	Anchor  int
	Members []int
	Shared  []string
}

// Issues is the anchor followed by the members.
func (c PlannedCluster) Issues() []int {
	return append([]int{c.Anchor}, c.Members...)
}

// PlanClusters groups ready issues whose footprints overlap, each group at
// most maxSize issues.
//
// DETERMINISTIC AND BOUNDED BY CONSTRUCTION. Issues are taken in number order;
// each joins the FIRST group it overlaps that still has room, or starts its
// own. So the lowest-numbered issue of a group is its anchor, the same input
// always plans the same groups on every node, and no chain of overlaps can
// grow a group past its bound — the issue that would is left to start the
// next one. An issue with no footprint overlaps nothing and stays alone,
// which is the safe answer for an issue whose text named no code at all.
func PlanClusters(candidates []ClusterCandidate, maxSize int) []PlannedCluster {
	ordered := append([]ClusterCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Issue < ordered[j].Issue })

	type group struct {
		issues []int
		keys   map[string]bool
		shared map[string]bool
	}
	var groups []*group
	for _, candidate := range ordered {
		var joined *group
		if maxSize > 1 {
			for _, g := range groups {
				if len(g.issues) >= maxSize {
					continue
				}
				overlap := false
				for _, key := range candidate.Footprint {
					if g.keys[key] {
						overlap = true
						g.shared[key] = true
					}
				}
				if overlap {
					joined = g
					break
				}
			}
		}
		if joined == nil {
			joined = &group{keys: map[string]bool{}, shared: map[string]bool{}}
			groups = append(groups, joined)
		}
		joined.issues = append(joined.issues, candidate.Issue)
		for _, key := range candidate.Footprint {
			joined.keys[key] = true
		}
	}

	planned := make([]PlannedCluster, 0, len(groups))
	for _, g := range groups {
		shared := make([]string, 0, len(g.shared))
		for key := range g.shared {
			shared = append(shared, key)
		}
		sort.Strings(shared)
		planned = append(planned, PlannedCluster{Anchor: g.issues[0], Members: g.issues[1:], Shared: shared})
	}
	return planned
}

// =============================================================================
// The record
// =============================================================================

const (
	issueClusterEntityType = "sdlc_issue_cluster"
	issueClusterEntityName = "issue cluster"
)

// IssueCluster records that a set of issues is delivered as one task.
//
// ONE COPY PER ISSUE: Issue names the slot a copy sits in, and every copy
// carries the same Anchor and Members. Any issue of the cluster therefore
// answers "am I clustered, and into what" with one read.
type IssueCluster struct {
	// ID is the DAO entity id, derived from ProjectID and Issue.
	ID uint64 `json:"-"`

	ProjectID string `json:"project_id"`

	// Issue is the issue this copy is filed under.
	Issue int `json:"issue"`

	// Anchor is the issue whose task delivers the cluster.
	Anchor int `json:"anchor"`

	// Members are the other issues the anchor's task closes, in order.
	Members []int `json:"members"`

	// Shared are the footprint keys that tied the cluster together: the
	// evidence a person reads to see why these issues travel together.
	Shared []string `json:"shared,omitempty"`

	// Head is the default-branch commit the facts were measured at.
	Head string `json:"head,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (c *IssueCluster) EntityID() uint64 { return c.ID }

// SetEntityID implements the DAO entity contract.
func (c *IssueCluster) SetEntityID(id uint64) { c.ID = id }

// Issues is the anchor followed by the members.
func (c *IssueCluster) Issues() []int { return append([]int{c.Anchor}, c.Members...) }

// Validate refuses a cluster that covers nothing or names an issue twice.
func (c *IssueCluster) Validate() error {
	if c == nil {
		return &InvalidInputError{Field: "cluster", Message: "must not be nil"}
	}
	if err := validateKeySegment("project_id", c.ProjectID); err != nil {
		return err
	}
	if c.Anchor <= 0 || len(c.Members) == 0 {
		return &InvalidInputError{Field: "cluster", Message: "a cluster is an anchor issue and at least one member"}
	}
	seen := map[int]bool{c.Anchor: true}
	for _, member := range c.Members {
		if member <= 0 || seen[member] {
			return &InvalidInputError{Field: "cluster.members", Message: "every member is a distinct issue other than the anchor"}
		}
		seen[member] = true
	}
	return nil
}

// issueClusterKey is the identity one copy's entity id derives from.
func issueClusterKey(projectID string, issue int) string {
	return "cluster\x00" + projectID + "\x00" + strconv.Itoa(issue)
}

// issueClusterRecordKey is the store key of the cluster copy filed under
// issue, the key its DAO writes.
func issueClusterRecordKey(projectID string, issue int) string {
	return recordKey(issueClusterEntityType, entityIDFor(issueClusterKey(projectID, issue)))
}

// IssueClusterReader answers which cluster an issue belongs to.
type IssueClusterReader interface {
	// GetIssueCluster returns the cluster issue belongs to, or a
	// NotFoundError when it belongs to none.
	GetIssueCluster(ctx context.Context, tenantID, projectID string, issue int) (*IssueCluster, error)
}

// IssueClusterStore reads and writes clusters. *Store satisfies it.
type IssueClusterStore interface {
	IssueClusterReader

	// PutIssueCluster files the cluster under every issue it covers.
	PutIssueCluster(ctx context.Context, tenantID string, cluster *IssueCluster) error

	// DeleteIssueCluster removes every copy of the cluster.
	DeleteIssueCluster(ctx context.Context, tenantID string, cluster *IssueCluster) error
}

var _ IssueClusterStore = (*Store)(nil)

func (s *Store) issueClusterDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*IssueCluster], error) {
	return entityDAO(ctx, s, op, tenantID, issueClusterEntityType, func() *IssueCluster { return &IssueCluster{} })
}

// GetIssueCluster implements IssueClusterReader.
func (s *Store) GetIssueCluster(ctx context.Context, tenantID, projectID string, issue int) (*IssueCluster, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	dao, err := s.issueClusterDAO(ctx, "GetIssueCluster", tenantID)
	if err != nil {
		return nil, err
	}
	id := entityIDFor(issueClusterKey(projectID, issue))
	cluster, err := dao.Get(ctx, id)
	notFound := &NotFoundError{Entity: issueClusterEntityName, TenantID: tenantID, ID: projectID + "#" + strconv.Itoa(issue)}
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return nil, notFound
		}
		return nil, backendError("GetIssueCluster", err)
	}
	if cluster.ProjectID != projectID || cluster.Issue != issue {
		return nil, notFound
	}
	cluster.ID = id
	return cluster, nil
}

// PutIssueCluster implements IssueClusterStore.
func (s *Store) PutIssueCluster(ctx context.Context, tenantID string, cluster *IssueCluster) error {
	if err := cluster.Validate(); err != nil {
		return err
	}
	dao, err := s.issueClusterDAO(ctx, "PutIssueCluster", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	for _, issue := range cluster.Issues() {
		copyOf := *cluster
		copyOf.Issue = issue
		copyOf.Members = append([]int(nil), cluster.Members...)
		copyOf.Shared = append([]string(nil), cluster.Shared...)
		copyOf.ID = entityIDFor(issueClusterKey(cluster.ProjectID, issue))
		copyOf.CreatedAt, copyOf.UpdatedAt = now, now
		if err := dao.Save(ctx, &copyOf); err != nil {
			return backendError("PutIssueCluster", err)
		}
	}
	return nil
}

// DeleteIssueCluster implements IssueClusterStore. A copy already gone is not
// an error: a dissolve that crashed half way must be able to finish. Each copy
// is removed only as read: a copy re-filed into a newer cluster after the
// read is left to it, and a copy this cluster re-filed after the read is kept
// and the dissolve refused with a VersionConflictError.
func (s *Store) DeleteIssueCluster(ctx context.Context, tenantID string, cluster *IssueCluster) error {
	if err := cluster.Validate(); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "DeleteIssueCluster", tenantID)
	if err != nil {
		return err
	}
	for _, issue := range cluster.Issues() {
		held, changed, err := removeRecord(ctx, "DeleteIssueCluster", store, issueClusterRecordKey(cluster.ProjectID, issue), func(c *IssueCluster) bool {
			// A slot re-filed into a newer cluster is not this cluster's
			// to remove.
			return c.ProjectID == cluster.ProjectID && c.Issue == issue && c.Anchor == cluster.Anchor
		})
		if err != nil {
			return err
		}
		if changed {
			return &VersionConflictError{
				Entity: issueClusterEntityName, TenantID: tenantID,
				ID: cluster.ProjectID + "#" + strconv.Itoa(issue), Expected: held.UpdatedAt.UnixNano(),
			}
		}
	}
	return nil
}

// clusterOfTask reads the member issues a task's metadata says it also closes.
//
// A MALFORMED LIST IS REFUSED rather than read around: a task that closes the
// wrong issues, or silently fewer than it was adopted for, leaves work
// reported done that nobody did.
func clusterOfTask(metadata map[string]string) ([]int, error) {
	raw := strings.TrimSpace(metadata[MetadataClusterIssues])
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	members := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || number <= 0 {
			return nil, &InvalidInputError{
				Field:   "task.metadata." + MetadataClusterIssues,
				Message: "expected comma-separated issue numbers, got " + strconv.Quote(raw),
			}
		}
		members = append(members, number)
	}
	return members, nil
}

// formatClusterIssues renders member issue numbers for task metadata.
func formatClusterIssues(members []int) string {
	parts := make([]string, 0, len(members))
	for _, member := range members {
		parts = append(parts, strconv.Itoa(member))
	}
	return strings.Join(parts, ",")
}

// =============================================================================
// What clustering writes for people to read
// =============================================================================

// clusterAnchorComment is posted on the anchor: which issues its task now also
// covers, and why.
func clusterAnchorComment(cluster PlannedCluster) string {
	var b strings.Builder
	b.WriteString("Triage clustered related issues into this one: its task and pull request also cover ")
	b.WriteString(issueList(cluster.Members))
	b.WriteString(", and the pull request closes all of them.")
	b.WriteString(clusterEvidence(cluster.Shared))
	return b.String()
}

// clusterMemberComment is posted on each member: which issue carries its work.
func clusterMemberComment(cluster PlannedCluster) string {
	var b strings.Builder
	b.WriteString("Triage clustered this issue into #" + strconv.Itoa(cluster.Anchor) +
		": one task and one pull request cover " + issueList(cluster.Issues()) +
		", and that pull request closes this issue. Follow #" + strconv.Itoa(cluster.Anchor) + " for progress.")
	b.WriteString(clusterEvidence(cluster.Shared))
	return b.String()
}

// clusterEvidence names the files and packages the issues share.
func clusterEvidence(shared []string) string {
	if len(shared) == 0 {
		return "\n"
	}
	names := make([]string, 0, len(shared))
	for _, key := range shared {
		names = append(names, "`"+strings.TrimPrefix(strings.TrimPrefix(key, footprintFile), footprintPackage)+"`")
	}
	return "\n\nThey touch the same code: " + strings.Join(names, ", ") + ".\n"
}

// issueList renders issue numbers as "#1, #2 and #3".
func issueList(issues []int) string {
	refs := make([]string, 0, len(issues))
	for _, issue := range issues {
		refs = append(refs, "#"+strconv.Itoa(issue))
	}
	switch len(refs) {
	case 0:
		return ""
	case 1:
		return refs[0]
	default:
		return strings.Join(refs[:len(refs)-1], ", ") + " and " + refs[len(refs)-1]
	}
}

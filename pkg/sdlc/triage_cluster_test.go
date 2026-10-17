//go:build codec_json

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
	"reflect"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Footprints and the plan
// =============================================================================

func TestTriageFootprint_FilesAndTheirPackagesButNotTheRoot(t *testing.T) {
	facts := TriageFacts{
		Paths: []TriageFact{
			{Subject: "pkg/luks2/kdf.go", Present: true},
			{Subject: "./pkg/luks2/new.go", Present: false},
			{Subject: "Makefile", Present: true},
			{Subject: "../escape.go", Present: false},
		},
		Identifiers: []TriageFact{
			{Subject: "DeriveKey", Present: true, Matches: []string{"pkg/crypto/derive.go:12:func DeriveKey() {}"}},
			{Subject: "Missing", Present: false},
		},
	}
	got := TriageFootprint(facts)
	want := []string{
		"file:Makefile",
		"file:pkg/crypto/derive.go",
		"file:pkg/luks2/kdf.go",
		"file:pkg/luks2/new.go",
		"pkg:pkg/crypto",
		"pkg:pkg/luks2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("footprint = %v\nwant       %v", got, want)
	}
	if len(TriageFootprint(TriageFacts{})) != 0 {
		t.Fatal("an issue that names nothing has a footprint")
	}
}

func TestPlanClusters_GroupsOverlapsAndLeavesTheRestAlone(t *testing.T) {
	planned := PlanClusters([]ClusterCandidate{
		{Issue: 12, Footprint: []string{"pkg:pkg/luks2", "file:pkg/luks2/kdf.go"}},
		{Issue: 5, Footprint: []string{"pkg:pkg/luks2", "file:pkg/luks2/header.go"}},
		{Issue: 9, Footprint: []string{"pkg:pkg/api"}},
		{Issue: 3},
	}, 4)
	if len(planned) != 3 {
		t.Fatalf("planned = %+v, want the luks2 pair and two alone", planned)
	}
	pair := planned[1]
	if pair.Anchor != 5 || !reflect.DeepEqual(pair.Members, []int{12}) || !reflect.DeepEqual(pair.Shared, []string{"pkg:pkg/luks2"}) {
		t.Fatalf("pair = %+v, want anchor 5 (lowest) with member 12 sharing pkg/luks2", pair)
	}
	if planned[0].Anchor != 3 || len(planned[0].Members) != 0 {
		t.Fatalf("an issue with no footprint was clustered: %+v", planned[0])
	}
	if planned[2].Anchor != 9 || len(planned[2].Members) != 0 {
		t.Fatalf("an issue that overlaps nothing was clustered: %+v", planned[2])
	}
}

// TestPlanClusters_TheBoundHoldsEvenWhenEverythingOverlaps proves no chain of
// overlaps grows a cluster past its size: the issue that would is left to start
// the next one.
func TestPlanClusters_TheBoundHoldsEvenWhenEverythingOverlaps(t *testing.T) {
	var candidates []ClusterCandidate
	for issue := 1; issue <= 7; issue++ {
		candidates = append(candidates, ClusterCandidate{Issue: issue, Footprint: []string{"pkg:pkg/shared"}})
	}
	planned := PlanClusters(candidates, 3)
	if len(planned) != 3 {
		t.Fatalf("planned %d groups, want 3 (3+3+1): %+v", len(planned), planned)
	}
	for _, group := range planned {
		if len(group.Issues()) > 3 {
			t.Fatalf("a group of %d exceeds the bound: %+v", len(group.Issues()), group)
		}
	}
	if !reflect.DeepEqual(planned[0].Issues(), []int{1, 2, 3}) || !reflect.DeepEqual(planned[2].Issues(), []int{7}) {
		t.Fatalf("groups = %+v", planned)
	}
}

func TestPlanClusters_ASizeOfOneClustersNothing(t *testing.T) {
	planned := PlanClusters([]ClusterCandidate{
		{Issue: 1, Footprint: []string{"pkg:a"}},
		{Issue: 2, Footprint: []string{"pkg:a"}},
	}, 1)
	if len(planned) != 2 || len(planned[0].Members)+len(planned[1].Members) != 0 {
		t.Fatalf("planned = %+v, want every issue alone", planned)
	}
}

func TestPlanClusters_IsDeterministic(t *testing.T) {
	in := []ClusterCandidate{
		{Issue: 8, Footprint: []string{"pkg:b", "pkg:a"}},
		{Issue: 2, Footprint: []string{"pkg:a"}},
		{Issue: 5, Footprint: []string{"pkg:b"}},
	}
	first := PlanClusters(in, 4)
	for i := 0; i < 20; i++ {
		if again := PlanClusters(in, 4); !reflect.DeepEqual(again, first) {
			t.Fatalf("plan changed between runs:\n%+v\n%+v", first, again)
		}
	}
}

func TestClusteringPolicy_DefaultsAndBounds(t *testing.T) {
	if got := (ClusteringPolicy{}).Size(); got != DefaultClusterMaxSize {
		t.Fatalf("zero policy size = %d, want %d", got, DefaultClusterMaxSize)
	}
	if got := (ClusteringPolicy{Disabled: true, MaxSize: 6}).Size(); got != 1 {
		t.Fatalf("a disabled policy clusters %d issues together, want 1", got)
	}
	if err := (ClusteringPolicy{MaxSize: MaxClusterSize + 1}).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an oversized cluster bound was accepted: %v", err)
	}
	if err := (ClusteringPolicy{MaxSize: -1}).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a negative cluster bound was accepted: %v", err)
	}
	binding := testBinding()
	binding.Clustering.MaxSize = -1
	if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a binding with a negative cluster bound was bound: %v", err)
	}
}

// =============================================================================
// The record
// =============================================================================

func TestIssueClusterStore_EveryIssueFindsItsCluster(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	cluster := &IssueCluster{ProjectID: testProject, Anchor: 5, Members: []int{12, 14}, Shared: []string{"pkg:pkg/luks2"}}
	if err := store.PutIssueCluster(ctx, testTenant, cluster); err != nil {
		t.Fatal(err)
	}
	for _, issue := range []int{5, 12, 14} {
		held, err := store.GetIssueCluster(ctx, testTenant, testProject, issue)
		if err != nil {
			t.Fatalf("issue %d: %v", issue, err)
		}
		if held.Anchor != 5 || !reflect.DeepEqual(held.Members, []int{12, 14}) || held.Issue != issue {
			t.Fatalf("issue %d reads %+v", issue, held)
		}
	}
	if _, err := store.GetIssueCluster(ctx, testTenant, testProject, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unclustered issue: %v", err)
	}
	// 14 is re-filed into a newer cluster; dissolving the old one must not
	// take it away from its new anchor.
	if err := store.PutIssueCluster(ctx, testTenant, &IssueCluster{ProjectID: testProject, Anchor: 14, Members: []int{20}}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteIssueCluster(ctx, testTenant, cluster); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteIssueCluster(ctx, testTenant, cluster); err != nil {
		t.Fatalf("a second dissolve failed: %v", err)
	}
	if _, err := store.GetIssueCluster(ctx, testTenant, testProject, 12); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a dissolved member still reads clustered: %v", err)
	}
	if held, err := store.GetIssueCluster(ctx, testTenant, testProject, 14); err != nil || held.Anchor != 14 {
		t.Fatalf("the re-filed issue = %+v %v, want its newer cluster", held, err)
	}
}

func TestIssueCluster_ValidateRefusesACoverOfNothing(t *testing.T) {
	cases := map[string]*IssueCluster{
		"no members":        {ProjectID: testProject, Anchor: 1},
		"no anchor":         {ProjectID: testProject, Members: []int{2}},
		"anchor as member":  {ProjectID: testProject, Anchor: 1, Members: []int{1}},
		"a repeated member": {ProjectID: testProject, Anchor: 1, Members: []int{2, 2}},
		"a bad project":     {ProjectID: "a/b", Anchor: 1, Members: []int{2}},
		"nil":               nil,
	}
	for name, cluster := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cluster.Validate(); err == nil {
				t.Fatal("refused nothing")
			}
		})
	}
}

func TestClusterOfTask_ReadsTheMembersAndRefusesGarbage(t *testing.T) {
	members, err := clusterOfTask(map[string]string{MetadataClusterIssues: "12, 14"})
	if err != nil || !reflect.DeepEqual(members, []int{12, 14}) {
		t.Fatalf("members = %v %v", members, err)
	}
	if members, err := clusterOfTask(nil); err != nil || members != nil {
		t.Fatalf("a task with no cluster = %v %v", members, err)
	}
	if _, err := clusterOfTask(map[string]string{MetadataClusterIssues: "12,x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed list = %v", err)
	}
	if got := formatClusterIssues([]int{3, 4}); got != "3,4" {
		t.Fatalf("formatted = %q", got)
	}
}

func TestClusterContract_CarriesEveryIssueVerbatim(t *testing.T) {
	contract, err := ClusterContract(sdlcPrompts, "Issue: #5\n\n## Fix kdf", []int{12, 14}, []string{"pkg:pkg/luks2"},
		[]string{"Issue: #12\n\n## Fix header", "Issue: #14\n\n## Fix trailer"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Fix kdf", "## Fix header", "## Fix trailer", "#12 and #14", "`pkg/luks2`"} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract lacks %q:\n%s", want, contract)
		}
	}
	if strings.Index(contract, "## Fix kdf") > strings.Index(contract, "## Fix header") {
		t.Fatal("the anchor's contract is not first")
	}
	if _, err := ClusterContract(sdlcPrompts, "", []int{1}, nil, []string{"x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no anchor: %v", err)
	}
	if _, err := ClusterContract(sdlcPrompts, "a", []int{1, 2}, nil, []string{"x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a member without a contract: %v", err)
	}
}

// =============================================================================
// Triage clusters what it finds ready
// =============================================================================

// clusterRig is a triage rig whose checkout holds one package two issues
// both name, and one package only a third names.
func clusterRig(t *testing.T) *triageRig {
	t.Helper()
	rig := newTriageRig(t)
	rig.binding.Forge.Labels[LabelClustered] = "factory:clustered"
	if err := rig.store.PutBinding(context.Background(), testTenant, rig.binding); err != nil {
		t.Fatal(err)
	}
	rig.container.seedFile("pkg/luks2/kdf.go", "package luks2\nfunc DeriveKey() {}")
	rig.container.seedFile("pkg/luks2/header.go", "package luks2\nfunc ReadHeader() {}")
	rig.container.seedFile("pkg/api/server.go", "package api\nfunc Serve() {}")
	rig.forge.putIssue(21, "kdf: `DeriveKey` in pkg/luks2/kdf.go ignores the salt")
	rig.forge.putIssue(22, "header: pkg/luks2/header.go misreads the version")
	rig.forge.putIssue(23, "api: pkg/api/server.go leaks a goroutine")
	rig.answers("A verifiable objective, scoped to one change.\n\nSDLC-TRIAGE: ready")
	return rig
}

// TestTriageClustering_RelatedReadyIssuesBecomeOneTask is the feature: two
// ready issues in one package become one task under the lower-numbered one,
// the other is told where its work went and never gets the trigger, and an
// unrelated ready issue stays its own task.
func TestTriageClustering_RelatedReadyIssuesBecomeOneTask(t *testing.T) {
	rig := clusterRig(t)
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	trigger := rig.binding.Forge.TriggerLabel
	if !containsLabel(rig.labels(21), trigger) {
		t.Fatalf("the anchor #21 = %v, want the trigger", rig.labels(21))
	}
	if member := rig.labels(22); containsLabel(member, trigger) || !containsLabel(member, "factory:clustered") {
		t.Fatalf("the member #22 = %v, want clustered and never triggered", member)
	}
	if !containsLabel(rig.labels(23), trigger) {
		t.Fatalf("the unrelated #23 = %v, want its own trigger", rig.labels(23))
	}
	if comments := rig.comments(22); len(comments) != 1 || !strings.Contains(comments[0], "clustered this issue into #21") {
		t.Fatalf("member comments = %v", comments)
	}
	if comments := rig.comments(21); len(comments) != 1 || !strings.Contains(comments[0], "also cover #22") {
		t.Fatalf("anchor comments = %v", comments)
	}
	cluster, err := rig.store.GetIssueCluster(context.Background(), testTenant, testProject, 22)
	if err != nil || cluster.Anchor != 21 || !reflect.DeepEqual(cluster.Members, []int{22}) {
		t.Fatalf("recorded cluster = %+v %v", cluster, err)
	}
	if details, ok := rig.auditDetails(auditTriageClustered); !ok || !strings.Contains(details, "anchor=21 members=22") {
		t.Fatalf("clustering audit = %q", details)
	}
}

func TestTriageClustering_OffAdmitsEveryReadyIssueAlone(t *testing.T) {
	rig := clusterRig(t)
	rig.binding.Clustering.Disabled = true
	if err := rig.store.PutBinding(context.Background(), testTenant, rig.binding); err != nil {
		t.Fatal(err)
	}
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	trigger := rig.binding.Forge.TriggerLabel
	for _, issue := range []int{21, 22, 23} {
		if !containsLabel(rig.labels(issue), trigger) {
			t.Fatalf("#%d = %v, want triggered on its own", issue, rig.labels(issue))
		}
	}
	if rig.audited(auditTriageClustered) {
		t.Fatal("a project with clustering off clustered")
	}
}

// TestTriageClustering_AFailedRecordAdmitsNothingOfTheCluster proves the
// order: without its record, the anchor would be adopted without its members.
func TestTriageClustering_AFailedRecordAdmitsNothingOfTheCluster(t *testing.T) {
	rig := clusterRig(t)
	clusters := newMemoryClusters()
	clusters.putErr = errors.New("store sealed")
	rig.runner.clusters = clusters
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	trigger := rig.binding.Forge.TriggerLabel
	if containsLabel(rig.labels(21), trigger) || containsLabel(rig.labels(22), trigger) {
		t.Fatalf("a cluster with no record was admitted: #21=%v #22=%v", rig.labels(21), rig.labels(22))
	}
	if !containsLabel(rig.labels(23), trigger) {
		t.Fatal("one cluster's failure cost an unrelated issue its admission")
	}
}

// TestTriageClustering_AMemberIsNotJudgedWhileItsAnchorIsOpen proves a member
// stays out of triage — its work is its anchor's task — and is judged again
// once its anchor has closed without it.
func TestTriageClustering_AMemberIsNotJudgedWhileItsAnchorIsOpen(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	run := triageRunFor(forge, binding)
	clusters := run.runner.clusters.(*memoryClusters)
	if err := clusters.PutIssueCluster(context.Background(), testTenant,
		&IssueCluster{ProjectID: testProject, Anchor: 21, Members: []int{22}}); err != nil {
		t.Fatal(err)
	}
	member := forge.putIssue(22, "header")
	run.open = []ForgeIssue{{Number: 21, State: "open"}, *member}
	run.discussions = map[int][]ForgeComment{}
	candidate, err := run.isCandidate(context.Background(), member, binding.Forge.OwnedLabels())
	if err != nil || candidate {
		t.Fatalf("a member of an open anchor's cluster = %v %v, want skipped", candidate, err)
	}

	run.open = []ForgeIssue{*member}
	candidate, err = run.isCandidate(context.Background(), member, binding.Forge.OwnedLabels())
	if err != nil || !candidate {
		t.Fatalf("a member whose anchor closed = %v %v, want judged again", candidate, err)
	}
	if _, err := clusters.GetIssueCluster(context.Background(), testTenant, testProject, 22); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the orphaned cluster was not dissolved: %v", err)
	}
}

func TestTriageClustering_AnUnreadableClusterSkipsTheIssue(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	run := triageRunFor(forge, binding)
	run.runner.clusters.(*memoryClusters).getErr = errors.New("store sealed")
	issue := forge.putIssue(22, "header")
	candidate, err := run.isCandidate(context.Background(), issue, binding.Forge.OwnedLabels())
	if candidate || !errors.Is(err, ErrTriageVerdict) {
		t.Fatalf("isCandidate = %v %v, want skipped with a verdict error", candidate, err)
	}
}

// =============================================================================
// The anchor's task carries the cluster
// =============================================================================

func TestIssueSource_AnAnchorCarriesItsMembersAndAMemberIsNotAdopted(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.PutBinding(ctx, testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.putIssue(21, "Fix kdf", "task:ready")
	forge.putIssue(22, "Fix header", "task:ready")
	forge.putIssue(24, "Fix trailer")
	forge.issues[24].State = "closed"
	if err := store.PutIssueCluster(ctx, testTenant, &IssueCluster{
		ProjectID: testProject, Anchor: 21, Members: []int{22, 24}, Shared: []string{"pkg:pkg/luks2"},
	}); err != nil {
		t.Fatal(err)
	}
	source, err := NewIssueSource(store, &fakeForges{forge: forge}, store, 10)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := source.Poll(ctx, testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Source.Ref != IssueRefString(testRepoRef, 21) {
		t.Fatalf("specs = %+v, want the anchor alone", specs)
	}
	spec := specs[0]
	if spec.Metadata[MetadataClusterIssues] != "22" {
		t.Fatalf("cluster metadata = %q, want only the open member", spec.Metadata[MetadataClusterIssues])
	}
	for _, want := range []string{"Fix kdf", "Fix header", "Also covered by this task"} {
		if !strings.Contains(spec.Description, want) {
			t.Fatalf("contract lacks %q:\n%s", want, spec.Description)
		}
	}
	if strings.Contains(spec.Description, "Fix trailer") {
		t.Fatal("a member closed since triage was handed to the worker as work")
	}
}

// =============================================================================
// The delivery closes every issue in the cluster
// =============================================================================

func TestHooks_AClusteredTaskClosesEveryIssueAndLabelsEveryMember(t *testing.T) {
	rig := newRunnerRig(t)
	rig.forge.putIssue(26, "Fix the other thing", "factory:clustered")
	task := testTaskRecord(25)
	task.Description = issueContract
	task.Metadata = map[string]string{MetadataClusterIssues: "26"}
	rig.tasks.replace(task)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Forge.Labels[LabelClustered] = "factory:clustered"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var body string
	for _, pr := range rig.forge.pullRequests() {
		body = pr.Body
	}
	if !strings.Contains(body, "Closes #25") || !strings.Contains(body, "Closes #26") {
		t.Fatalf("pull request body does not close the cluster:\n%s", body)
	}
	merges := rig.forge.mergeCalls()
	if len(merges) != 1 || !strings.Contains(merges[0].spec.CommitMessage, "Closes #26") {
		t.Fatalf("merge = %+v, want its message to close the member", merges)
	}
	if labels := rig.forge.issueLabels(26); !containsLabel(labels, "factory:merged") || containsLabel(labels, "factory:clustered") {
		t.Fatalf("member labels = %v, want merged in place of clustered", labels)
	}
	if !containsLabel(rig.forge.issueLabels(25), "factory:merged") {
		t.Fatalf("anchor labels = %v", rig.forge.issueLabels(25))
	}
}

var _ orchestration.TaskSource = (*IssueSource)(nil)

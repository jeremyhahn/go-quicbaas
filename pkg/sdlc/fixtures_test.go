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
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Shared test doubles for the module
// =============================================================================

// testSocketsRoot is a sockets root a session socket actually fits under.
//
// t.TempDir() embeds the TEST'S NAME in the path, and this package has test
// names past 70 characters — long enough on their own to blow the kernel's
// 107-byte sun_path budget and turn every runner test into a startup
// refusal. A short prefix is what a real deployment's root looks like anyway.
func testSocketsRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "qbs")
	if err != nil {
		t.Fatalf("sockets root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// memoryResolver hands each tenant its own in-memory store.
type memoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
	err    error
}

func newMemoryResolver() *memoryResolver {
	return &memoryResolver{stores: map[string]qrdbsdk.KVStore{}}
}

// Resolve implements qrdbsdk.TenantStoreResolver.
func (r *memoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(newMemoryResolver())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

const (
	testTenant   = "tenant-a"
	testProject  = "proj-1"
	testTask     = "task-1"
	fixtureOwner = "acme"
	fixtureRepo  = "api"

	// fakeReviewerLogin is the forge account the fake forge attributes a
	// submitted review to, and the login the review runner's fixtures bind
	// as the reviewer. The two must agree: the runner recognises its OWN
	// standing verdict by login, so a fixture whose forge and binding
	// disagreed would never dedup and every dedup test would pass for the
	// wrong reason.
	fakeReviewerLogin = "quicbaas-reviewer"
)

var testRepoRef = RepoRef{Owner: fixtureOwner, Repo: fixtureRepo}

// testSSHHostKey is a forge host key the tests pin. It is GENERATED ONCE per
// process rather than written down, so no key of any provenance is committed
// and no fixture can be mistaken for a real forge's identity.
var testSSHHostKey = generateTestSSHHostKey()

// testSigningKey is the attempt signing key's public half every test signs
// with: a real ed25519 key from a fixed seed, so the publisher's fingerprint
// check sees a key it can parse and every run sees the same one.
var testSigningKey = func() string {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	key, err := ssh.NewPublicKey(ed25519.NewKeyFromSeed(seed).Public())
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " agent@example.test"
}()

// testSigningFingerprint is testSigningKey's SHA-256 fingerprint, which is
// what git reports (%GF) for a commit it signed.
var testSigningFingerprint = func() string {
	fp, err := sshKeyFingerprint(testSigningKey)
	if err != nil {
		panic(err)
	}
	return fp
}()

// generateTestSSHHostKey renders a fresh ed25519 public key as the OpenSSH
// public-key line ForgeConfig.SSHHostKey holds.
func generateTestSSHHostKey() string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic("sdlc test fixtures: generating a host key: " + err.Error())
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		panic("sdlc test fixtures: wrapping the host key: " + err.Error())
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

func testForgeType(name string) ForgeType { return ForgeType("fake-" + name) }

func testBinding() *ProjectBinding {
	return &ProjectBinding{
		ProjectID: testProject,
		Forge: ForgeConfig{
			Type: ForgeGitea, BaseURL: "http://forge.test", Owner: fixtureOwner, Repo: fixtureRepo,
			Labels: map[string]string{
				LabelInProgress: "factory:in-progress",
				LabelInReview:   "factory:in-review",
				LabelMerged:     "factory:merged",
				LabelBlocked:    "factory:blocked",
				LabelNeedsInfo:  "factory:needs-info",
			},
			TriggerLabel: "task:ready", WebhookSecretRef: "wh", TokenSecretRef: "tok",
		},
		Strategy:     GitHubFlowName,
		Worker:       orchestration.AgentRefTo(testCoderAgent),
		Verification: []string{"make build", "make test"},
		Review: ReviewPolicy{RequiredApprovals: 1, RequireIndependentReviewer: true, RequireGreenChecks: true,
			Reviewers: map[string]string{"codex": ModelFamilyOpenAI, "claude-bot": ModelFamilyAnthropic}},
		MaxReviewRounds: 3,
	}
}

// The Agents every fixture binding references.
const (
	// testCoderAgent writes code: claude, the Anthropic family.
	testCoderAgent = "claude-coder"

	// testReviewerAgent reviews: codex, the OpenAI family.
	testReviewerAgent = "codex-reviewer"

	// testQwenAgent is the advisory alternate: a local qwen model.
	testQwenAgent = "qwen-reviewer"

	// testTriageAgent triages on the NPU.
	testTriageAgent = "npu-triage"

	// testClaudeReviewerAgent is a Claude reviewer, which the fleet refuses.
	testClaudeReviewerAgent = "claude-reviewer"

	// testMysteryAgent writes code on a backend whose family nobody knows.
	testMysteryAgent = "mystery-coder"

	// testCodexCoderAgent writes code on codex, the OpenAI family.
	testCodexCoderAgent = "codex-coder"

	// testQwenCoderAgent writes code on a local qwen model.
	testQwenCoderAgent = "qwen-coder"

	// testBackupReviewerAgent is a second codex reviewer, the failover seat.
	testBackupReviewerAgent = "codex-reviewer-2"

	// testOpencodeQwenAgent reviews with opencode on the local review model,
	// the seat that is handed a model server endpoint.
	testOpencodeQwenAgent = "qwen-opencode-reviewer"

	// testOpencodeHostedAgent reviews with opencode on a hosted model, which
	// reaches its provider through its own configuration.
	testOpencodeHostedAgent = "gpt-opencode-reviewer"
)

// testAgents is the tenant agent catalog the fixtures resolve against.
func testAgents() orchestration.AgentCatalogue {
	agent := func(id, backend, model string, roles ...orchestration.AgentRole) orchestration.Agent {
		return orchestration.Agent{ID: id, Name: id, Backend: backend, Model: model, Roles: roles}
	}
	agents := []orchestration.Agent{
		agent(testCoderAgent, "claude", "claude-opus-5", orchestration.AgentRoleCoding, orchestration.AgentRoleTriage),
		agent(testReviewerAgent, "codex", "gpt-5.5", orchestration.AgentRoleReview, orchestration.AgentRoleTriage),
		agent(testQwenAgent, "ollama-agent", "qwen3.8:27b-q4_K_M", orchestration.AgentRoleReview),
		agent(testTriageAgent, "openvino", "qwen3-8b", orchestration.AgentRoleTriage),
		agent(testClaudeReviewerAgent, "claude", "claude-opus-5", orchestration.AgentRoleReview),
		agent(testMysteryAgent, "a-backend-no-family-is-known-for", "mystery", orchestration.AgentRoleCoding),
		agent(testCodexCoderAgent, "codex", "gpt-5.5", orchestration.AgentRoleCoding),
		agent(testQwenCoderAgent, "qwen", "qwen3.8:27b-q4_K_M", orchestration.AgentRoleCoding),
		agent(testBackupReviewerAgent, "codex", "gpt-5.5", orchestration.AgentRoleReview),
		agent(testOpencodeQwenAgent, "opencode", "ollama/review-q8:latest", orchestration.AgentRoleReview),
		agent(testOpencodeHostedAgent, "opencode", "openai/gpt-5.5", orchestration.AgentRoleReview),
	}
	catalogue := orchestration.AgentCatalogue{Agents: map[string]orchestration.Agent{}, Pools: map[string]orchestration.AgentPool{}}
	for _, a := range agents {
		catalogue.Agents[a.ID] = a
	}
	catalogue.Pools["coders"] = orchestration.AgentPool{ID: "coders", Name: "Coders", Agents: []string{testCoderAgent}}
	return catalogue
}

// fixedAgents is an AgentCatalog answering one catalogue, or one error.
type fixedAgents struct {
	catalogue orchestration.AgentCatalogue
	err       error
}

func (f fixedAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	return f.catalogue, f.err
}

// testAgentCatalog is the fixtures' catalog.
func testAgentCatalog() AgentCatalog { return fixedAgents{catalogue: testAgents()} }

func testOrchProject() *orchestration.Project {
	return &orchestration.Project{
		ID: testProject, TenantID: testTenant, Name: "api", DefaultWorkflow: "sdlc-test",
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/api.git", DefaultBranch: "main",
			Author:  orchestration.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent-api"}},
		},
	}
}

func testTaskRecord(issue int) *orchestration.Task {
	return &orchestration.Task{
		ID: testTask, TenantID: testTenant, ProjectID: testProject, Subject: "Fix the thing",
		Status: orchestration.TaskStatusInProgress, Attempts: 1, AttemptBudget: 3, SpawnedJobID: "job-1",
		Source: &orchestration.TaskSourceRef{Kind: TaskSourceKindForgeIssue, Ref: IssueRefString(testRepoRef, issue), URL: "http://forge.test/acme/api/issues/" + strconv.Itoa(issue)},
	}
}

func testJob(attempt int) *orchestration.JobContext {
	return &orchestration.JobContext{
		JobID: "job-1", TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: attempt, Iteration: 1,
		Workflow: &orchestration.WorkflowDefinition{
			Name: "sdlc-test", Mode: orchestration.WorkflowModeConstrained,
			Agents:   []orchestration.AgentRoleSpec{{Role: "lead", Backend: "claude"}, {Role: "worker", Backend: "claude", Model: "claude-opus-5"}},
			Settings: orchestration.WorkflowSettings{PollInterval: "1ms"},
		},
	}
}

// fakeForge is an in-memory ForgeBackend that records every write.
type fakeForge struct {
	mu sync.Mutex

	forgeType ForgeType
	caps      ForgeCapabilities

	// branchSHA is the tip GetBranch reports; empty takes fakeForgeHeadSHA.
	// A test moves it to model a merge landing on the default branch.
	branchSHA string
	// getBranchErr, when set, fails every GetBranch: the default branch's
	// head cannot be read.
	getBranchErr error

	issues        map[int]*ForgeIssue
	comments      map[int][]ForgeComment
	prs           map[int]*ForgePullRequest
	closed        []int
	closeIssueErr error
	reviews       map[int][]ForgeReview
	checks        map[string][]ForgeCheckRun
	logs          map[string]string
	tags          map[string]*ForgeTag

	// The onboarding surface, modelled rather than stubbed: labels and
	// webhooks are state a test mutates and reads back, so idempotency is
	// observable here and not only against a wire fixture.
	labels     []ForgeLabel
	webhooks   []ForgeWebhook
	repoFiles  map[string][]ForgeFile
	protection map[string]*ForgeBranchProtection

	// prLabels models what Gitea actually is: a pull request IS an issue, so
	// the label routes address it by the same number. A double that dropped a
	// label written to a pull request would let the park's pull-request label
	// regress unnoticed.
	prLabels map[int][]string

	// cancelled records the run ids CancelCheckRun was asked to stop, and
	// cancelErr is what it answers when the capability is declared.
	cancelled []string
	cancelErr error

	nextPR        int
	nextReviewID  int64
	nextCommentID int64
	labelAdds     []string
	labelRemovals []string
	merges        []mergeCall
	listReviews   int

	// listOpenPRs and listChecks count the two calls the review floor makes
	// on every pass over every bound project, so the forge budget that keeps
	// the pass affordable is a property a test can hold rather than a claim.
	listOpenPRs  int
	listChecks   int
	postFailures int
	submitted    []ReviewSubmission
	statuses     []CommitStatus
	statusHeads  []string
	files        map[int][]ForgeChangedFile

	// reruns records every automatic re-run the engine asked for, so a test
	// can assert both that a void red was re-run and that a genuine one was
	// not. rerunErr makes the forge refuse, which is how a platform without
	// the route behaves.
	reruns   []ForgeCheckRun
	rerunErr error

	// branches are the branches CreateBranch cut, by name, holding the
	// commit they were cut at; a pull request opened from one reports that
	// commit as its head, as a real forge does.
	branches        map[string]string
	deletedBranches []string

	// deleteBranchErr, when set, is what DeleteBranch answers instead of
	// deleting.
	deleteBranchErr error

	// onlyKnownBranches makes GetBranch answer NotFound for a branch that is
	// neither the default branch nor one the test or CreateBranch cut, which
	// is how a forge answers for a branch that does not exist. Off, every
	// branch reports the default tip, which is what the tests written before
	// branches were modelled rely on.
	onlyKnownBranches bool

	// closedPRs are the pull requests ClosePullRequest closed, and
	// closePRErr is what it answers instead.
	closedPRs  []int
	closePRErr error

	// filed are the issues CreateIssue filed, in order, and createIssueErr
	// is what it answers instead. collaborators is what ListCollaborators
	// reports, and collaboratorsErr what it answers instead.
	filed            []IssueSpec
	createIssueErr   error
	collaborators    []ForgeUser
	collaboratorsErr error

	getPRNil bool
	// getPRErrs are answered by the next GetPullRequest calls, one each,
	// before any read succeeds: a forge that fails a few reads and recovers.
	getPRErrs        []error
	listReviewsErr   error
	submitErr        error
	submitStoredHead string
	statusErr        error
	listFilesErr     error

	// onCreatePullRequest runs under the forge's own lock as a pull request
	// is opened, so a test can act as the reviewer and the pipeline do on the
	// head the module just published.
	onCreatePullRequest func(*ForgePullRequest)

	// onGetPullRequest runs under the forge's own lock on the STORED record
	// each time a pull request is read, so a test can act as an author
	// pushing between two of a run's reads. What it writes is what every
	// later read sees, which is what makes a moved head durable rather than
	// one call's illusion.
	onGetPullRequest func(*ForgePullRequest)

	// onListCheckRuns runs under the forge's own lock each time a commit's
	// runs are read, so a test can interrupt a run while it waits on CI.
	onListCheckRuns func(sha string)

	// onListReviews runs under the forge's own lock each time a pull
	// request's reviews are listed, so a test can have a verdict arrive while
	// something is waiting on it.
	onListReviews func(number int)

	// onCommentOnIssue runs under the forge's own lock after a comment is
	// posted, so a test can interrupt a run between two of its comments.
	onCommentOnIssue func(number int)

	createPRErr     error
	listOpenPRsErr  error
	listChecksErr   error
	listIssuesErr   error
	issueListings   int
	getIssueErr     error
	getIssueNil     bool
	listCommentsErr error
	commentErr      error
	addLabelErr     error
	removeLabelErr  error
	mergeErr        error
	getTagErr       error
	mergeNotReadyN  int
	verifyErr       error
	parseEvent      *ForgeEvent
	parseErr        error

	listLabelsErr     error
	ensureLabelsErr   error
	webhooksErr       error
	ensureWebhookErr  error
	deleteWebhookErr  error
	repoFilesErr      error
	protectionErr     error
	ensureWebhookSpec *WebhookSpec
	ensureWebhookSeen int
	deleteWebhookSeen int

	// login is the account AuthenticatedUser reports; empty takes
	// fakeForgeLogin. authErr is what it answers instead.
	login   string
	authErr error
}

// fakeForgeLogin is the account the fake forge's token authenticates as
// unless a test names another.
const fakeForgeLogin = "agent-fake"

func (f *fakeForge) AuthenticatedUser(context.Context) (*TokenIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authErr != nil {
		return nil, f.authErr
	}
	if f.login == "" {
		return &TokenIdentity{Username: fakeForgeLogin}, nil
	}
	return &TokenIdentity{Username: f.login}, nil
}

type mergeCall struct {
	number int
	spec   MergeSpec
	head   string
}

func newFakeForge() *fakeForge {
	return &fakeForge{
		// The fake models GITEA, so it declares Gitea's capabilities: a
		// double that under-declared would let a runner branch on a false
		// flag and never exercise the path production takes.
		forgeType: ForgeGitea,
		caps: ForgeCapabilities{
			CheckRunLogs: true, RerunCheckRuns: true, SubmitReviews: true, CommitStatuses: true,
		},
		issues: map[int]*ForgeIssue{}, comments: map[int][]ForgeComment{}, prs: map[int]*ForgePullRequest{},
		reviews: map[int][]ForgeReview{}, checks: map[string][]ForgeCheckRun{}, logs: map[string]string{},
		tags:      map[string]*ForgeTag{},
		repoFiles: map[string][]ForgeFile{}, protection: map[string]*ForgeBranchProtection{},
		files:    map[int][]ForgeChangedFile{},
		prLabels: map[int][]string{},
		nextPR:   100, nextReviewID: 1, nextCommentID: 1,
	}
}

// =============================================================================
// fakeForge: the onboarding surface
// =============================================================================

func (f *fakeForge) Labels(context.Context, RepoRef) ([]ForgeLabel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listLabelsErr != nil {
		return nil, f.listLabelsErr
	}
	return append([]ForgeLabel(nil), f.labels...), nil
}

// EnsureLabels models the contract the adapters implement: create what is
// missing, leave every existing label exactly as it is.
func (f *fakeForge) EnsureLabels(_ context.Context, _ RepoRef, specs []LabelSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureLabelsErr != nil {
		return f.ensureLabelsErr
	}
	for _, spec := range missingLabels(f.labels, specs) {
		f.labels = append(f.labels, ForgeLabel{
			Name: spec.Name, Color: spec.Color, Description: spec.Description,
		})
	}
	return nil
}

func (f *fakeForge) Webhooks(context.Context, RepoRef) ([]ForgeWebhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.webhooksErr != nil {
		return nil, f.webhooksErr
	}
	return append([]ForgeWebhook(nil), f.webhooks...), nil
}

func (f *fakeForge) DeleteWebhook(_ context.Context, _ RepoRef, hookURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteWebhookSeen++
	if f.deleteWebhookErr != nil {
		return f.deleteWebhookErr
	}
	kept := make([]ForgeWebhook, 0, len(f.webhooks))
	for _, hook := range f.webhooks {
		if hook.URL != hookURL {
			kept = append(kept, hook)
		}
	}
	f.webhooks = kept
	return nil
}

func (f *fakeForge) RepoFiles(_ context.Context, _ RepoRef, _, dir string) ([]ForgeFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.repoFilesErr != nil {
		return nil, f.repoFilesErr
	}
	return append([]ForgeFile(nil), f.repoFiles[dir]...), nil
}

func (f *fakeForge) BranchProtection(_ context.Context, _ RepoRef, branch string) (*ForgeBranchProtection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.protectionErr != nil {
		return nil, f.protectionErr
	}
	return f.protection[branch], nil
}

func (f *fakeForge) putIssue(number int, title string, labels ...string) *ForgeIssue {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue := &ForgeIssue{Number: number, Title: title, Body: "body of " + strconv.Itoa(number), State: "open",
		Labels: append([]string(nil), labels...), URL: "http://forge.test/acme/api/issues/" + strconv.Itoa(number)}
	f.issues[number] = issue
	return issue
}

func (f *fakeForge) Type() ForgeType                 { return f.forgeType }
func (f *fakeForge) Capabilities() ForgeCapabilities { return f.caps }

func (f *fakeForge) ListIssues(_ context.Context, _ RepoRef, filter IssueFilter) ([]ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueListings++
	if f.listIssuesErr != nil {
		return nil, f.listIssuesErr
	}
	numbers := make([]int, 0, len(f.issues))
	for n := range f.issues {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	var out []ForgeIssue
	for _, n := range numbers {
		issue := f.issues[n]
		// "all" is the forge's own wildcard and the real adapter passes it
		// straight through (GiteaBackend.ListIssues), so the double honours
		// it too: a caller reconciling stale state must see closed issues.
		if filter.State != "" && filter.State != "all" && issue.State != filter.State {
			continue
		}
		if len(filter.Labels) > 0 && !hasAll(issue.Labels, filter.Labels) {
			continue
		}
		out = append(out, *issue)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *fakeForge) GetIssue(_ context.Context, _ RepoRef, number int) (*ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getIssueErr != nil {
		return nil, f.getIssueErr
	}
	// An adapter answering neither an issue nor an error breaks GetIssue's
	// contract; the sweep must refuse rather than dereference it.
	if f.getIssueNil {
		return nil, nil
	}
	issue, ok := f.issues[number]
	if !ok {
		// A pull request IS an issue on Gitea: reading its number as an
		// issue answers the pull request's labels.
		if pr, isPR := f.prs[number]; isPR {
			return &ForgeIssue{Number: number, Title: pr.Title, State: pr.State, Labels: append([]string(nil), f.prLabels[number]...)}, nil
		}
		return nil, &NotFoundError{Entity: "issue", ID: strconv.Itoa(number)}
	}
	copied := *issue
	copied.Labels = append([]string(nil), issue.Labels...)
	return &copied, nil
}

func (f *fakeForge) CommentOnIssue(_ context.Context, _ RepoRef, number int, body string) (*ForgeComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postFailures > 0 {
		f.postFailures--
		return nil, backendError("CommentOnIssue", errors.New("forge down"))
	}
	if f.commentErr != nil {
		return nil, f.commentErr
	}
	comment := ForgeComment{ID: f.nextCommentID, Body: body, Author: "quicbaas", Created: time.Now()}
	f.nextCommentID++
	f.comments[number] = append(f.comments[number], comment)
	if f.onCommentOnIssue != nil {
		f.onCommentOnIssue(number)
	}
	return &comment, nil
}

func (f *fakeForge) ListIssueComments(_ context.Context, _ RepoRef, number int) ([]ForgeComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listCommentsErr != nil {
		return nil, f.listCommentsErr
	}
	return append([]ForgeComment(nil), f.comments[number]...), nil
}

func (f *fakeForge) SetIssueLabels(_ context.Context, _ RepoRef, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		issue.Labels = append([]string(nil), labels...)
	}
	return nil
}

func (f *fakeForge) AddIssueLabels(_ context.Context, _ RepoRef, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addLabelErr != nil {
		return f.addLabelErr
	}
	f.labelAdds = append(f.labelAdds, labels...)
	// ADDING A LABEL AN OBJECT ALREADY CARRIES IS A NO-OP, which is what the
	// ForgeBackend contract says every forge does and what the idempotence of
	// a resumed escalation's label writes rests on. A double that stacked
	// duplicates would fail the resumed park for a reason no forge has.
	if issue, ok := f.issues[number]; ok {
		issue.Labels = mergeLabels(issue.Labels, labels)
	}
	if _, ok := f.prs[number]; ok {
		f.prLabels[number] = mergeLabels(f.prLabels[number], labels)
	}
	return nil
}

// withoutLabel returns held minus label.
func withoutLabel(held []string, label string) []string {
	kept := make([]string, 0, len(held))
	for _, l := range held {
		if l != label {
			kept = append(kept, l)
		}
	}
	return kept
}

// mergeLabels adds what is not already there.
func mergeLabels(held, adding []string) []string {
	present := make(map[string]bool, len(held))
	for _, label := range held {
		present[label] = true
	}
	for _, label := range adding {
		if !present[label] {
			present[label] = true
			held = append(held, label)
		}
	}
	return held
}

// pullRequestLabels is what a pull request carries, which on Gitea is the same
// label set an issue of that number carries.
func (f *fakeForge) pullRequestLabels(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prLabels[number]...)
}

// reviewReads is how many times the review gate asked the forge for verdicts,
// which is one per decisive round. It is the cheapest observable proxy for
// "did this run enter review again".
func (f *fakeForge) reviewReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listReviews
}

// mergeCalls is every merge the forge was asked for.
func (f *fakeForge) mergeCalls() []mergeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mergeCall(nil), f.merges...)
}

// cancelledRuns is every run id CancelCheckRun was asked to stop.
func (f *fakeForge) cancelledRuns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelled...)
}

func (f *fakeForge) RemoveIssueLabel(_ context.Context, _ RepoRef, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeLabelErr != nil {
		return f.removeLabelErr
	}
	f.labelRemovals = append(f.labelRemovals, label)
	if issue, ok := f.issues[number]; ok {
		issue.Labels = withoutLabel(issue.Labels, label)
	}
	if _, ok := f.prs[number]; ok {
		f.prLabels[number] = withoutLabel(f.prLabels[number], label)
	}
	return nil
}

// putTag publishes a tag, which is what makes a release dependency satisfied.
func (f *fakeForge) putTag(repo RepoRef, name, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tags[repo.String()+"@"+name] = &ForgeTag{Name: name, SHA: sha}
}

func (f *fakeForge) GetTag(_ context.Context, repo RepoRef, tag string) (*ForgeTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getTagErr != nil {
		return nil, f.getTagErr
	}
	found, ok := f.tags[repo.String()+"@"+tag]
	if !ok {
		return nil, &NotFoundError{Entity: "tag", ID: repo.String() + "@" + tag}
	}
	return found, nil
}

func (f *fakeForge) CreateBranch(_ context.Context, _ RepoRef, name, fromRef string) (*ForgeBranch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.branches == nil {
		f.branches = map[string]string{}
	}
	f.branches[name] = fromRef
	return &ForgeBranch{Name: name, SHA: fromRef}, nil
}

// fakeForgeHeadSHA is the tip every branch of the double reports.
//
// A REAL FORGE'S BRANCH ALWAYS HAS A COMMIT, and a double that reported none
// made every triage verdict unstamped — which reads as "the tree cannot be
// identified", so every parked issue is re-judged on every pass. The double
// has to have a head for the stamping to be exercised at all.
const fakeForgeHeadSHA = "f00dcafe00000000000000000000000000000000"

func (f *fakeForge) GetBranch(_ context.Context, _ RepoRef, name string) (*ForgeBranch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getBranchErr != nil {
		return nil, f.getBranchErr
	}
	if cut, ok := f.branches[name]; ok {
		return &ForgeBranch{Name: name, SHA: cut}, nil
	}
	if f.onlyKnownBranches && name != "main" {
		return nil, &NotFoundError{Entity: "branch", ID: name}
	}
	sha := f.branchSHA
	if sha == "" {
		sha = fakeForgeHeadSHA
	}
	return &ForgeBranch{Name: name, SHA: sha}, nil
}
func (f *fakeForge) DeleteBranch(_ context.Context, _ RepoRef, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteBranchErr != nil {
		return f.deleteBranchErr
	}
	f.deletedBranches = append(f.deletedBranches, name)
	delete(f.branches, name)
	return nil
}

// ClosePullRequest closes a pull request without merging it, recording which.
func (f *fakeForge) ClosePullRequest(_ context.Context, _ RepoRef, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closePRErr != nil {
		return f.closePRErr
	}
	pr, ok := f.prs[number]
	if !ok {
		return &NotFoundError{Entity: "pull request", ID: strconv.Itoa(number)}
	}
	if pr.Merged || pr.State == "closed" {
		return nil
	}
	pr.State = "closed"
	f.closedPRs = append(f.closedPRs, number)
	return nil
}

func (f *fakeForge) CreatePullRequest(_ context.Context, _ RepoRef, spec PullRequestSpec) (*ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createPRErr != nil {
		return nil, f.createPRErr
	}
	head := "head-" + spec.HeadRef
	if cut, ok := f.branches[spec.HeadRef]; ok {
		head = cut
	}
	pr := &ForgePullRequest{Number: f.nextPR, Title: spec.Title, Body: spec.Body, State: "open", HeadRef: spec.HeadRef,
		BaseRef: spec.BaseRef, URL: "http://forge.test/acme/api/pulls/" + strconv.Itoa(f.nextPR), HeadSHA: head,
		// A fresh pull request with no conflict settles mergeable, as a real
		// forge's check does; a test models a conflict by clearing it.
		Mergeable: true}
	f.prs[pr.Number] = pr
	f.nextPR++
	if f.onCreatePullRequest != nil {
		f.onCreatePullRequest(pr)
	}
	return pr, nil
}

// CloseIssue closes an issue in the fake, recording it so a test can assert
// that triage closed exactly what it said it would.
func (f *fakeForge) CloseIssue(_ context.Context, _ RepoRef, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeIssueErr != nil {
		return f.closeIssueErr
	}
	f.closed = append(f.closed, number)
	if issue, ok := f.issues[number]; ok {
		issue.State = "closed"
	}
	return nil
}

func (f *fakeForge) GetPullRequest(_ context.Context, _ RepoRef, number int) (*ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// An adapter answering neither a pull request nor an error breaks
	// GetPullRequest's contract; a caller must refuse rather than
	// dereference it.
	if f.getPRNil {
		return nil, nil
	}
	if len(f.getPRErrs) > 0 {
		err := f.getPRErrs[0]
		f.getPRErrs = f.getPRErrs[1:]
		return nil, err
	}
	pr, ok := f.prs[number]
	if !ok {
		return nil, &NotFoundError{Entity: "pull request", ID: strconv.Itoa(number)}
	}
	if f.onGetPullRequest != nil {
		f.onGetPullRequest(pr)
	}
	copied := *pr
	return &copied, nil
}

// ListOpenPullRequests answers the head-to-pull-request lookup the review
// dispatcher makes on a pipeline delivery. It returns copies, ordered by
// number, so a caller cannot mutate the fake's records and two reads of one
// fake list identically.
func (f *fakeForge) ListOpenPullRequests(context.Context, RepoRef) ([]ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listOpenPRs++
	if f.listOpenPRsErr != nil {
		return nil, f.listOpenPRsErr
	}
	numbers := make([]int, 0, len(f.prs))
	for number := range f.prs {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	open := make([]ForgePullRequest, 0, len(numbers))
	for _, number := range numbers {
		pr := f.prs[number]
		if pr.State != "" && pr.State != "open" {
			continue
		}
		open = append(open, *pr)
	}
	return open, nil
}

func (f *fakeForge) MergePullRequest(_ context.Context, _ RepoRef, number int, spec MergeSpec, expectedHead string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merges = append(f.merges, mergeCall{number: number, spec: spec, head: expectedHead})
	if f.mergeNotReadyN > 0 {
		f.mergeNotReadyN--
		return &ForgeNotReadyError{Type: f.forgeType, Operation: "merge", Subject: strconv.Itoa(number)}
	}
	if f.mergeErr != nil {
		return f.mergeErr
	}
	if pr, ok := f.prs[number]; ok {
		pr.Merged = true
		pr.State = "closed"
		if pr.MergeSHA == "" {
			pr.MergeSHA = "merged-" + strconv.Itoa(number)
		}
	}
	return nil
}

func (f *fakeForge) ListReviews(_ context.Context, _ RepoRef, number int) ([]ForgeReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listReviewsErr != nil {
		return nil, f.listReviewsErr
	}
	f.listReviews++
	if f.onListReviews != nil {
		f.onListReviews(number)
	}
	return append([]ForgeReview(nil), f.reviews[number]...), nil
}

func (f *fakeForge) ListCheckRuns(_ context.Context, _ RepoRef, sha string) ([]ForgeCheckRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listChecks++
	if f.onListCheckRuns != nil {
		f.onListCheckRuns(sha)
	}
	if f.listChecksErr != nil {
		return nil, f.listChecksErr
	}
	return append([]ForgeCheckRun(nil), f.checks[sha]...), nil
}

// listOpenPRCalls and checkCalls report the forge reads made so far.
func (f *fakeForge) listOpenPRCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listOpenPRs
}

func (f *fakeForge) checkCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listChecks
}

func (f *fakeForge) CheckRunLogs(_ context.Context, _ RepoRef, run ForgeCheckRun, _ int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs[run.Name], nil
}

// CancelCheckRun answers the way the DECLARED CAPABILITY says it must. A
// double that returned nil while reporting CancelCheckRuns false would let a
// caller record a cancellation Gitea can never make, which is the precise
// failure ForgeCapabilities exists to prevent.
func (f *fakeForge) CancelCheckRun(_ context.Context, _ RepoRef, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.caps.CancelCheckRuns {
		return &ForgeUnsupportedError{Type: f.forgeType, Capability: "cancelling check runs"}
	}
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}

func (f *fakeForge) RerunCheckRun(_ context.Context, _ RepoRef, run ForgeCheckRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rerunErr != nil {
		return f.rerunErr
	}
	f.reruns = append(f.reruns, run)
	return nil
}

// rerunCalls copies what the engine asked to be re-run.
func (f *fakeForge) rerunCalls() []ForgeCheckRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ForgeCheckRun(nil), f.reruns...)
}

// SubmitReview records the verdict and stores it where ListReviews will find
// it, which is what makes a second run of the runner see the first run's
// answer exactly as it would on a real forge.
func (f *fakeForge) SubmitReview(_ context.Context, _ RepoRef, number int, submission ReviewSubmission) (*ForgeReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	if err := submission.Validate(); err != nil {
		return nil, err
	}
	f.submitted = append(f.submitted, submission)
	head := submission.HeadSHA
	if f.submitStoredHead != "" {
		head = f.submitStoredHead
	}
	review := ForgeReview{
		ID:        f.nextReviewID,
		Reviewer:  fakeReviewerLogin,
		State:     giteaReviewStates[submission.Verdict],
		Body:      submission.Body,
		HeadSHA:   head,
		Submitted: time.Unix(f.nextReviewID, 0).UTC(),
	}
	f.nextReviewID++
	f.reviews[number] = append(f.reviews[number], review)
	return &review, nil
}

func (f *fakeForge) SetCommitStatus(_ context.Context, _ RepoRef, sha string, status CommitStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return f.statusErr
	}
	if err := status.Validate(); err != nil {
		return err
	}
	f.statuses = append(f.statuses, status)
	f.statusHeads = append(f.statusHeads, sha)
	f.checks[sha] = append(f.checks[sha], ForgeCheckRun{
		ID:      strconv.Itoa(len(f.statuses)),
		Name:    status.Context,
		HeadSHA: sha,
		Status:  string(status.Status),
	})
	return nil
}

func (f *fakeForge) ListPullRequestFiles(_ context.Context, _ RepoRef, number int) ([]ForgeChangedFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listFilesErr != nil {
		return nil, f.listFilesErr
	}
	return append([]ForgeChangedFile(nil), f.files[number]...), nil
}

// publishedStatuses is every commit status the fake was told to publish,
// paired with the commit it was written against.
func (f *fakeForge) publishedStatuses() ([]CommitStatus, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CommitStatus(nil), f.statuses...), append([]string(nil), f.statusHeads...)
}

// lastStatus is the newest commit status the fake was told to publish.
func (f *fakeForge) lastStatus() (CommitStatus, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.statuses) == 0 {
		return CommitStatus{}, false
	}
	return f.statuses[len(f.statuses)-1], true
}

// submissions is every verdict the fake was asked to post.
func (f *fakeForge) submissions() []ReviewSubmission {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ReviewSubmission(nil), f.submitted...)
}

// putReview records a review as if a reviewer had left it on the forge.
func (f *fakeForge) putReview(number int, review ForgeReview) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if review.ID == 0 {
		review.ID = f.nextReviewID
		f.nextReviewID++
	}
	f.reviews[number] = append(f.reviews[number], review)
}

func (f *fakeForge) VerifyWebhook(_ []byte, _ string, _ []byte) error { return f.verifyErr }

func (f *fakeForge) ParseWebhook(string, []byte) (*ForgeEvent, error) {
	if f.parseErr != nil {
		return nil, f.parseErr
	}
	if f.parseEvent != nil {
		return f.parseEvent, nil
	}
	return &ForgeEvent{Kind: EventIssueOpened, Repo: testRepoRef, IssueNumber: 1}, nil
}

// EnsureWebhook models the adapters' contract: create or bring into line,
// idempotently, so a repeat call leaves ONE delivery.
func (f *fakeForge) EnsureWebhook(_ context.Context, _ RepoRef, spec WebhookSpec, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureWebhookSeen++
	if f.ensureWebhookErr != nil {
		return f.ensureWebhookErr
	}
	copied := spec
	f.ensureWebhookSpec = &copied
	for i := range f.webhooks {
		if f.webhooks[i].URL == spec.URL {
			f.webhooks[i].Active = true
			f.webhooks[i].Events = append([]string(nil), spec.Events...)
			return nil
		}
	}
	f.webhooks = append(f.webhooks, ForgeWebhook{
		ID: strconv.Itoa(len(f.webhooks) + 1), URL: spec.URL, Active: true,
		Events: append([]string(nil), spec.Events...),
	})
	return nil
}

func (f *fakeForge) issueLabels(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		return append([]string(nil), issue.Labels...)
	}
	return nil
}

func (f *fakeForge) commentsOn(number int) []ForgeComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ForgeComment(nil), f.comments[number]...)
}

// fakeForges resolves every binding to one fake forge.
type fakeForges struct {
	forge *fakeForge
	err   error

	// tokenRefs is the token secret every ForgeFor call named, so a test can
	// see WHOSE identity an adapter was built for.
	mu        sync.Mutex
	tokenRefs []string
}

// resolvedTokenRefs is every token reference an adapter was built for.
func (f *fakeForges) resolvedTokenRefs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokenRefs...)
}

func (f *fakeForges) ForgeFor(_ context.Context, _ string, cfg ForgeConfig) (ForgeBackend, error) {
	f.mu.Lock()
	f.tokenRefs = append(f.tokenRefs, cfg.TokenSecretRef)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.forge == nil {
		return nil, nil
	}
	return f.forge, nil
}

// fakeProjects is a ProjectReader over a map.
type fakeProjects struct {
	projects map[string]*orchestration.Project
}

func (p *fakeProjects) Get(_ context.Context, tenantID, projectID string) (*orchestration.Project, error) {
	if project, ok := p.projects[projectID]; ok && project.TenantID == tenantID {
		return project, nil
	}
	return nil, &NotFoundError{Entity: "project", TenantID: tenantID, ID: projectID}
}

// fakeTasks is a TaskReader over a map that records submitted artifacts.
type fakeTasks struct {
	mu        sync.Mutex
	tasks     map[string]*orchestration.Task
	artifacts map[string][]orchestration.Artifact
}

func newFakeTasks(tasks ...*orchestration.Task) *fakeTasks {
	f := &fakeTasks{tasks: map[string]*orchestration.Task{}, artifacts: map[string][]orchestration.Artifact{}}
	for _, task := range tasks {
		f.tasks[task.ID] = task
	}
	return f
}

func (f *fakeTasks) GetTask(_ context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[taskID]
	if !ok || task.TenantID != tenantID {
		return nil, &NotFoundError{Entity: "task", TenantID: tenantID, ID: taskID}
	}
	return task, nil
}

func (f *fakeTasks) SubmitTaskArtifacts(_ context.Context, _, taskID string, artifacts []orchestration.Artifact) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artifacts[taskID] = append(f.artifacts[taskID], artifacts...)
	return nil
}

// replace stores task under its id, replacing whatever was there.
func (f *fakeTasks) replace(task *orchestration.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = task
}

func (f *fakeTasks) submitted(taskID string) []orchestration.Artifact {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]orchestration.Artifact(nil), f.artifacts[taskID]...)
}

// fakeReviewTrigger records the reviews the webhook path asked for, whole: a
// pipeline delivery carries a commit and no pull request number, so a double
// that recorded only the number could not show that the head reached the port.
type fakeReviewTrigger struct {
	mu       sync.Mutex
	asked    []ReviewRequest
	started  bool
	err      error
	panicked bool
}

func (f *fakeReviewTrigger) TriggerReview(_ context.Context, req ReviewRequest) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	f.asked = append(f.asked, req)
	return f.started, nil
}

func (f *fakeReviewTrigger) requests() []ReviewRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ReviewRequest(nil), f.asked...)
}

// fakeNudger records nudges.
type fakeNudger struct {
	mu     sync.Mutex
	nudges []string
	err    error
}

func (n *fakeNudger) NudgeProject(_ context.Context, tenantID, projectID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.nudges = append(n.nudges, tenantID+"/"+projectID)
	return nil
}

// immediateWaiter never waits.
type immediateWaiter struct{ waits int }

func (w *immediateWaiter) Wait(ctx context.Context, _ int) error {
	w.waits++
	return ctx.Err()
}

// executingHost is the host a fake agent service runs its sessions on. It
// provisions each spawn's named workspace under its own projects root with the
// WorkspaceManager every real host runs — the daemon's in-process backend and a
// fleet node alike — and releases the spec directory when the session closes.
// That is the whole of the contract a runner may rely on (go-quicbaas#981), so
// the fakes implement it with the production code rather than a copy of it.
type executingHost struct {
	// agentHost is the fleet node this host is, empty for the daemon.
	agentHost string

	mu sync.Mutex

	// root is the host's projects root. Empty refuses every named workspace,
	// as a host with no projects root does.
	root string

	// sessions is each live session's provisioned workspace, released on close.
	sessions map[string]*orchestration.ProvisionedWorkspace

	// provisioned is every workspace provisioned, in spawn order.
	provisioned []*orchestration.ProvisionedWorkspace

	// specs is the spec each spawn carried, copied before provisioning wipes
	// the request's own.
	specs []map[string][]byte
}

// provision makes the workspace a spawn names and answers where, or nil when
// the spawn named none.
func (h *executingHost) provision(sessionID string, req *orchestration.SpawnSessionRequest) (*orchestration.WorkspacePlacement, error) {
	if req.Workspace == nil {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.root == "" {
		return nil, &orchestration.SessionWorkspaceUnsupportedError{Host: "the test host"}
	}
	manager, err := orchestration.NewWorkspaceManager(h.root)
	if err != nil {
		return nil, err
	}
	if req.Workspace.Spec != nil {
		spec := make(map[string][]byte, len(req.Workspace.Spec.Files))
		for name, content := range req.Workspace.Spec.Files {
			spec[name] = append([]byte(nil), content...)
		}
		h.specs = append(h.specs, spec)
	}
	p, err := manager.ProvisionSession(req.Workspace)
	if err != nil {
		return nil, err
	}
	if h.sessions == nil {
		h.sessions = map[string]*orchestration.ProvisionedWorkspace{}
	}
	h.sessions[sessionID] = p
	h.provisioned = append(h.provisioned, p)
	return &orchestration.WorkspacePlacement{AgentHost: h.agentHost, Key: req.Workspace.Key, Dir: p.Dir}, nil
}

// release drops a closed session's spec directory.
func (h *executingHost) release(sessionID string) {
	h.mu.Lock()
	p := h.sessions[sessionID]
	delete(h.sessions, sessionID)
	h.mu.Unlock()
	p.Release()
}

// spec is the spec the n-th spawn carried, nil when there was none.
func (h *executingHost) spec(n int) map[string][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n >= len(h.specs) {
		return nil
	}
	return h.specs[n]
}

// workspaces is every workspace provisioned so far, in spawn order.
func (h *executingHost) workspaces() []*orchestration.ProvisionedWorkspace {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*orchestration.ProvisionedWorkspace(nil), h.provisioned...)
}

func testRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return root
}

// pullRequest reads one recorded pull request, for assertions.
// pullRequests returns a copy of every pull request the forge holds.
func (f *fakeForge) pullRequests() []*ForgePullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*ForgePullRequest, 0, len(f.prs))
	for _, pr := range f.prs {
		copied := *pr
		out = append(out, &copied)
	}
	return out
}

func (f *fakeForge) pullRequest(number int) *ForgePullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prs[number]
}

// =============================================================================
// The worker's container
// =============================================================================
//
// fakeContainer is the container the worker's session runs in, playing the
// one part the runner needs from it: it answers the git commands the
// publisher execs, in the checkout the container's own entrypoint would have
// made. Everything the runner asks of git goes through here, so a runner that
// found some other way to commit or push would fail these tests rather than
// pass them quietly.
// outgoingCommit is one commit a branch carries beyond its base, with git's
// %G? signature status for it.
type outgoingCommit struct {
	SHA    string
	Status string

	// Signer is the signing key's fingerprint git reports (%GF); empty for
	// an unsigned commit.
	Signer string

	// Committer is the committer's address (%ce); empty is the project's
	// own identity, which is who commits in the container.
	Committer string

	// Time is the committer time (%ct); zero is testCommitTime.
	Time time.Time
}

// testCommitTime is when a scripted commit was made unless it says otherwise.
var testCommitTime = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// logLine renders the commit as verifySignedFormat does.
func (c outgoingCommit) logLine() string {
	committer := c.Committer
	if committer == "" {
		committer = "agent-api@example.test"
	}
	when := c.Time
	if when.IsZero() {
		when = testCommitTime
	}
	return strings.Join([]string{c.SHA, c.Status, strconv.FormatInt(when.Unix(), 10), committer, c.Signer}, "\x1f")
}

type fakeContainer struct {
	mu sync.Mutex

	// checkout is the directory the CONTAINER calls its checkout. A command
	// naming anything outside the checkout ROOT it sits in is refused, which
	// is what proves the runner never handed a daemon path to a container —
	// the same refusal the daemon e2e's scripted runtime makes. The root
	// itself is admitted because that is where the clone runs.
	checkout string

	// execs is every command, with the container and directory it named.
	execs []containerExec

	// verifications scripts what a verification command answers, by its
	// command line. A command with no entry that is not a shell built-in
	// this container models is refused rather than passed.
	verifications map[string]*orchestration.ExecResult

	// changed and ahead are the state of the checkout: uncommitted work, and
	// commits the base does not have.
	changed bool
	ahead   bool

	// nothingStaged makes "add --all" stage nothing even though status
	// reported a change: a file rewritten to its committed content between
	// the two, which is the go-quicbaas#1048 refusal.
	nothingStaged bool

	// outgoing is every commit the branch carries beyond its base, with the
	// signature status "git log --format=%H %G?" reports for it. A commit the
	// publisher makes is added as "G" when its configuration signs and "N"
	// when it does not. A test that sets ahead with no outgoing commits gets
	// one signed worker commit; one that models an unsigned worker commit
	// lists it here.
	outgoing []outgoingCommit

	// logErr makes "git log" exit 128, which is git failing to answer rather
	// than answering "unsigned".
	logErr error

	// diffExit makes "diff --cached --quiet" exit with this code instead of
	// answering, and diffExecErr makes the exec itself fail, which is the
	// index probe breaking rather than reporting.
	diffExit    int
	diffExecErr error
	// behind is how many commits the BASE has that the branch does not, which
	// is the other direction entirely from ahead. It is what
	// "rev-list --count HEAD..origin/<base>" answers, and it is how a test
	// models the fault #982 is about: a branch trailing a base that has
	// already fixed the thing its tests are failing on.
	behind int

	// worktrees are the throwaway worktrees that currently exist, by the
	// directory they were added at, holding the ref they were added from. A
	// second "worktree add" on a live directory is refused the way git
	// refuses it.
	worktrees map[string]string

	// worktreeAdds and worktreeRemovals are every worktree the runner made
	// and every one it removed, which is what proves a merge result never
	// outlives the verification it was prepared for.
	worktreeAdds     []worktreeCall
	worktreeRemovals []string

	// merges is every merge attempted, with the directory it ran in.
	merges []gitMergeCall

	// baseHead is the commit a fetch of the base lands on, which is what
	// "rev-parse FETCH_HEAD" answers. It is deliberately NOT the branch's own
	// head: a fixture that answered one with the other could not tell a merge
	// of the base from a merge of the branch with itself.
	baseHead string

	// behindAfterFetch is what the branch is behind by ONCE THE BASE HAS BEEN
	// REFRESHED — the remote having moved since this container last fetched,
	// which is the window a count against the stale ref cannot see. Nil
	// leaves the count alone, which is every container that models a base
	// standing still.
	behindAfterFetch *int

	// worktreeRemoveErr makes every "worktree remove" fail, which is the
	// throwaway worktree that will not go away: the directory stays
	// registered and the run has to say so rather than report it removed.
	worktreeRemoveErr error

	// snapshots is every working-tree snapshot that was written: the index it
	// was staged in, the parent it was given, and the configuration git ran
	// under. It is what says the worker's uncommitted work reached the tree
	// that was judged, and that no ref moved to carry it.
	snapshots []snapshotCall

	// removals is every path removed through the cleanup seam.
	removals []string

	// removeErr makes every file removal fail, which is the throwaway index
	// that will not go away beside a checkout the next attempt will use.
	removeErr error

	// stagedIndex is the index the last "add --all" staged into, and
	// stagedIndexSeed is what that index had been copied from BEFORE the
	// staging — empty when it was staged into from nothing. `git add --all`
	// into an empty index drops every file the repository tracks despite an
	// ignore rule, so "which index, and was it the worker's own" is the whole
	// question (#982, round 3).
	stagedIndex     string
	stagedIndexSeed string

	// copies is every file copy the publisher made, and copyErr makes them
	// all fail — the worker's index that could not be read, which must refuse
	// rather than snapshot a tree missing what an ignore rule matches.
	copies  []copyCall
	copyErr error

	// writeTreeErr and commitTreeErr are the halves of the snapshot refusing.
	writeTreeErr  error
	commitTreeErr error

	// mergeConflicts are the paths a merge leaves unmerged. Non-empty makes
	// every merge refuse, which is the branch that cannot be judged on a
	// merge result at all.
	mergeConflicts []string

	// mergeErr makes every merge fail WITHOUT leaving an unmerged path,
	// which is the other half of a non-zero `git merge`: the worktree that
	// is not there, the configuration git refuses, the disk that filled.
	// Nothing about such a branch needs reconciling, so a run that reported
	// it as a conflict would send the worker after one that does not exist.
	mergeErr error

	// conflictProbeErr makes the unmerged-paths probe itself fail, which is
	// the container that cannot say WHETHER the merge conflicted. The list
	// it leaves behind is empty either way, so a run that read the empty list
	// as an answer could not tell this apart from a clean failure.
	conflictProbeErr error

	// mergeVerifications scripts what a verification command answers IN A
	// MERGE-RESULT WORKTREE, when that differs from what it answers in the
	// checkout. A command with no entry here answers the same either way.
	mergeVerifications map[string]*orchestration.ExecResult

	// head is what rev-parse HEAD answers. A commit advances it, which is
	// what makes a second review round land on a NEW head, and so does a
	// "checkout -B" whose start this container can resolve.
	head string

	// remoteBranches is what "origin/<branch>" resolves to, which is how a
	// test makes the REMOTE move under a checkout. Nil means the fixture
	// does not model the remote at all: an origin/<branch> start resolves to
	// nothing and leaves HEAD where it was, which is what every test written
	// before the remote was modelled expects.
	remoteBranches map[string]string

	// knownCommits is every commit object a checkout can resolve by SHA.
	// Nil means the fixture models no history and any SHA resolves; a
	// populated map is how a test makes a FORCE-PUSH drop the commit a
	// caller pinned to.
	knownCommits map[string]bool

	commits      []string
	commitConfig []string

	// logConfig is the "-c" configuration the last "git log" carried, which
	// is where the signature check names the allowed_signers file.
	logConfig []string

	// logRange is the revision range the last "git log" named, which is how a
	// test proves the signature check reads the ref being pushed.
	logRange string
	pushes   []pushedRef

	// diverged models a local branch that no longer descends from what the
	// remote holds: a worker that rewrote a published commit. Git refuses a
	// plain push of it as non-fast-forward, and so does this container. The
	// factory never forces past that refusal, so it stays set.
	diverged bool

	// stopped models a container that has died under its session: the
	// runtime refuses every exec before a process starts, as docker does with
	// "container … is not running" (go-quicbaas#932).
	stopped bool

	// repos is every directory that is already a git repository, as the
	// container sees it. A clone adds one; the checkout step's probe reads
	// it, which is what makes a resumed attempt skip the clone.
	repos map[string]bool

	// tree is the checkout's files by path, for the probes triage measures
	// (git ls-files, git grep). Nil means the container does not model them,
	// and a probe is refused the way any unmodelled git subcommand is.
	tree map[string]string

	// origins is what "git remote get-url origin" answers, per directory. A
	// clone records the URL it cloned from; a TEST overwrites one to model
	// the fault the pre-flight exists for — an attempt directory a dead
	// predecessor left holding a DIFFERENT repository, which the checkout's
	// idempotence adopts without noticing.
	origins map[string]string

	// heads is the commit each directory's HEAD is on. A clone puts one
	// there; a test EMPTIES one to model a clone killed part-way, which
	// leaves a repository that reads as present and holds no object.
	heads map[string]string

	// branches is the branch each directory's HEAD is on, which is what
	// "rev-parse --abbrev-ref HEAD" answers. A "checkout -B <name>" sets it,
	// and so does a plain "checkout <ref>" in a context repository.
	branches map[string]string

	// hostRoot is the daemon-side directory bind-mounted at the container's
	// checkout root. A clone creates its directory THERE as well, because a
	// bind mount is one filesystem seen twice: what the container writes is
	// what the daemon can then see.
	hostRoot string

	// sshAgentSocket is what SSH_AUTH_SOCK holds INSIDE this container, copied
	// off the spawn request exactly as a real container learns it. It is what
	// decides whether a clone of an ssh remote authenticates; see authenticate.
	sshAgentSocket string

	// credential is what the credential tier's forge token file holds: set by
	// a delivery, cleared by a revocation. credentialAt is how many checkout
	// commands had run when it was first delivered, which is what says it reached
	// the tier before the first command that needed it. Neither command is in
	// execs, which holds only the commands that touch the checkout.
	credential   []byte
	credentialAt int

	// credentialDeliveries and credentialRevoked count the deliveries that
	// wrote the file and the revocations that removed it.
	credentialDeliveries int
	credentialRevoked    int

	clones    []clonedRepo
	configs   []gitConfigSet
	fetches   []fetchCall
	checkouts []checkoutCall

	statusErr   error
	commitErr   error
	pushErr     error
	cloneErr    error
	checkoutErr error

	// cloneErrFor makes the clone of ONE repository fail, by its directory
	// name. That is the shape the forge actually produces: the project is
	// served and a context repository the project declared is not.
	cloneErrFor map[string]error

	// cloneSilent makes the clone of one repository REPORT SUCCESS and create
	// nothing. It is the fault the pre-flight exists for that no failing exit
	// status would ever reveal.
	cloneSilent map[string]bool

	// headErr makes "git rev-parse HEAD" refuse, which is what a checkout
	// whose HEAD the runner cannot read looks like from the runner's side.
	// The repository probe — "rev-parse --git-dir" — is unaffected: it asks
	// a different question and a test about one must not silence the other.
	headErr error
}

// clonedRepo is one repository the checkout step cloned, and where.
type clonedRepo struct {
	URL    string
	Dir    string
	Config []string
}

// gitConfigSet is one repository-local configuration the checkout step wrote.
type gitConfigSet struct {
	Dir   string
	Key   string
	Value string
}

// fetchCall is one fetch: the checkout it ran in and the refs it named.
type fetchCall struct {
	Dir    string
	Remote string
	Refs   []string
	Config []string
}

// checkoutCall is one branch or ref the checkout step put on a repository.
type checkoutCall struct {
	Dir  string
	Args []string
}

// defaultBaseHead is the commit a fetch of the base lands on when a test does
// not say otherwise.
const defaultBaseHead = "base-head-0"

// snapshotCall is one working-tree snapshot: the index git staged it in, the
// parent the commit object was given, and the "-c key=value" pairs it ran
// under.
type snapshotCall struct {
	Index string

	// Seed is what that index was copied from before anything was staged
	// into it, empty when it was staged into from nothing.
	Seed    string
	Parent  string
	Config  []string
	Commit  string
	WorkDir string
}

// worktreeCall is one throwaway worktree the runner added: where it went,
// the ref it was made from, and the checkout that made it.
type worktreeCall struct {
	Dir  string
	Ref  string
	From string
}

// gitMergeCall is one git merge: the worktree it ran in, the ref merged in,
// and the flags it carried — which is where "no commit" is proved.
type gitMergeCall struct {
	Dir  string
	Ref  string
	Args []string
}

// copyCall is one file copy: what was copied where, and from which checkout.
type copyCall struct {
	From string
	To   string
	Dir  string
}

// containerExec is one command the runner ran in the container.
type containerExec struct {
	ContainerID string
	WorkDir     string
	Cmd         []string
	Env         map[string]string
}

// pushedRef is one publication: the local branch and the ref it was
// published as.
type pushedRef struct {
	Branch       string
	RemoteBranch string

	// Args is the whole push argv after "push", so a test can assert what the
	// publication did and did not pass.
	Args []string
}

// exec answers one command. Unknown commands are refused rather than
// succeeding: a publisher that grew a step this container does not model
// must be noticed, not humoured.
func (c *fakeContainer) exec(containerID string, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if handled, result, err := c.credentialTier(containerID, spec); handled {
		return result, err
	}
	c.execs = append(c.execs, containerExec{
		ContainerID: containerID, WorkDir: spec.WorkDir, Cmd: append([]string(nil), spec.Cmd...), Env: spec.Env,
	})
	if root := path.Dir(c.checkout); c.checkout != "" && spec.WorkDir != root && !strings.HasPrefix(spec.WorkDir, root+"/") {
		return nil, &orchestration.ContainerExecError{
			ContainerID: containerID,
			Cause: &InvalidInputError{Field: "work_dir", Message: "the command named " + spec.WorkDir +
				", which is outside this container's checkout root " + root},
		}
	}
	if c.stopped {
		return nil, &orchestration.ContainerNotRunningError{
			ContainerID: containerID,
			Message:     "Error response from daemon: container " + containerID + " is not running",
		}
	}
	if len(spec.Cmd) == 0 {
		return nil, &InvalidInputError{Field: "cmd", Message: "a container was asked to run nothing"}
	}
	if spec.Cmd[0] == verificationShell {
		return c.verify(spec.WorkDir, spec.Cmd)
	}
	if spec.Cmd[0] == removeCommand {
		return c.remove(spec.Cmd[1:])
	}
	if spec.Cmd[0] == copyCommand {
		return c.copy(spec.WorkDir, spec.Cmd[1:])
	}
	if spec.Cmd[0] != "git" {
		return nil, &InvalidInputError{Field: "cmd", Message: "this container runs git, rm, cp and verification commands"}
	}
	config, args := splitGitConfig(spec.Cmd[1:])
	if len(args) == 0 {
		return nil, &InvalidInputError{Field: "cmd", Message: "git was run with configuration and no subcommand"}
	}
	// "-C <name>" names a directory relative to the one the command runs in;
	// the checkout step uses it to ask about a repository that may not exist
	// yet, which a --workdir into a missing directory could not do.
	dir := spec.WorkDir
	if len(args) >= 2 && args[0] == "-C" {
		dir = path.Join(dir, args[1])
		args = args[2:]
	}
	// Two diffs are modelled: the publisher's "diff --cached --quiet" index
	// probe and the merge result's unmerged-paths probe.
	subcommand := args[0]
	if subcommand == "diff" && slices.Contains(args, "--diff-filter=U") {
		subcommand = "diff-unmerged"
	}
	switch subcommand {
	case "clone":
		if c.cloneErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.cloneErr.Error()}, nil
		}
		// git clone --origin origin <url> <name>
		url, name := args[len(args)-2], args[len(args)-1]
		if refusal := c.authenticate(url, config); refusal != nil {
			return refusal, nil
		}
		if err := c.cloneErrFor[name]; err != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: err.Error()}, nil
		}
		if c.cloneSilent[name] {
			c.clones = append(c.clones, clonedRepo{URL: url, Dir: path.Join(dir, name), Config: config})
			return &orchestration.ExecResult{Stderr: "Cloning into '" + name + "'...\n"}, nil
		}
		target := path.Join(dir, name)
		c.markRepo(target, url)
		if c.hostRoot != "" {
			if err := os.MkdirAll(filepath.Join(c.hostRoot, name), 0o700); err != nil {
				return nil, err
			}
		}
		c.clones = append(c.clones, clonedRepo{URL: url, Dir: target, Config: config})
		return &orchestration.ExecResult{Stderr: "Cloning into '" + name + "'...\n"}, nil
	case "config":
		if len(args) < 3 {
			return nil, &InvalidInputError{Field: "cmd", Message: "git config was run without a key and a value"}
		}
		c.configs = append(c.configs, gitConfigSet{Dir: dir, Key: args[1], Value: args[2]})
		return &orchestration.ExecResult{}, nil
	case "fetch":
		if len(args) < 2 {
			return nil, &InvalidInputError{Field: "cmd", Message: "git fetch named no remote"}
		}
		c.fetches = append(c.fetches, fetchCall{Dir: dir, Remote: args[1], Refs: args[2:], Config: config})
		// A FETCH IS WHERE THE BASE MOVING BECOMES VISIBLE. Until one runs,
		// this container answers with whatever it last saw — which is exactly
		// how a checkout-time fetch leaves a count blind to a base that moved
		// during the worker's turn.
		if c.behindAfterFetch != nil {
			c.behind = *c.behindAfterFetch
			c.baseHead = "base-head-moved"
		}
		return &orchestration.ExecResult{}, nil
	case "checkout":
		if c.checkoutErr != nil {
			return &orchestration.ExecResult{ExitCode: 1, Stderr: c.checkoutErr.Error()}, nil
		}
		c.checkouts = append(c.checkouts, checkoutCall{Dir: dir, Args: args[1:]})
		// WHICH BRANCH HEAD IS ON is recorded either way: "-B <name>" puts it
		// on the branch it names, a plain "checkout <ref>" on the ref it names.
		// The pre-flight asks for it, and a fixture that did not model it
		// would report every checkout as detached.
		c.setBranch(dir, args[1:])
		// "checkout -B <name> <start>" is the one form that MOVES HEAD here.
		// A plain "checkout <ref>" is what a context repository gets, in its
		// own directory, and must not touch the project checkout's HEAD.
		if len(args) == 4 && args[1] == "-B" {
			resolved, ok := c.resolveStart(args[3])
			if !ok {
				return &orchestration.ExecResult{
					ExitCode: 128,
					Stderr:   "fatal: '" + args[3] + "' is not a commit and a branch cannot be created from it\n",
				}, nil
			}
			if resolved != "" {
				c.head = resolved
			}
		}
		return &orchestration.ExecResult{}, nil
	case "status":
		if c.statusErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.statusErr.Error()}, nil
		}
		if c.changed {
			return &orchestration.ExecResult{Stdout: " M WORK.md\n"}, nil
		}
		return &orchestration.ExecResult{}, nil
	case "add":
		// "add --all" staged into GIT_INDEX_FILE is the snapshot of the
		// working tree, and the ONE thing it must never do is stage into the
		// worker's own index.
		if index := spec.Env[gitIndexFileEnv]; index != "" {
			if index == path.Join(dir, ".git", "index") {
				return nil, &InvalidInputError{Field: "env", Message: "a snapshot staged into the worker's own index"}
			}
			c.stagedIndex = index
			c.stagedIndexSeed = c.copiedFrom(index)
		}
		return &orchestration.ExecResult{}, nil
	case "diff":
		// "diff --cached --quiet" answers whether the index holds anything to
		// commit: exit 1 when it does, 0 when it does not. nothingStaged
		// models the race go-quicbaas#1048 reports — status saw a change that
		// "add --all" then staged as nothing.
		if c.diffExecErr != nil {
			return nil, c.diffExecErr
		}
		if c.diffExit != 0 {
			return &orchestration.ExecResult{ExitCode: c.diffExit, Stderr: "fatal: unable to read the index"}, nil
		}
		if c.changed && !c.nothingStaged {
			return &orchestration.ExecResult{ExitCode: 1}, nil
		}
		return &orchestration.ExecResult{}, nil
	case "write-tree":
		if spec.Env[gitIndexFileEnv] == "" {
			return nil, &InvalidInputError{Field: "env", Message: "a tree was written from the worker's own index"}
		}
		if c.writeTreeErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.writeTreeErr.Error() + "\n"}, nil
		}
		return &orchestration.ExecResult{Stdout: "tree-" + itoa(len(c.snapshots)+1) + "\n"}, nil
	case "commit-tree":
		// git commit-tree <tree> -p <parent> -m <message>
		if c.commitTreeErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.commitTreeErr.Error() + "\n"}, nil
		}
		commit := "snapshot-" + itoa(len(c.snapshots)+1)
		parent := ""
		for i, arg := range args {
			if arg == "-p" && i+1 < len(args) {
				parent = args[i+1]
			}
		}
		c.snapshots = append(c.snapshots, snapshotCall{
			Index: c.stagedIndex, Seed: c.stagedIndexSeed, Parent: parent,
			Config: config, Commit: commit, WorkDir: dir,
		})
		// NOTHING ABOUT THE BRANCH MOVES. commit-tree writes an object; a
		// fixture that advanced HEAD or cleared c.changed here would model a
		// commit, which is the one thing a snapshot must not be.
		return &orchestration.ExecResult{Stdout: commit + "\n"}, nil
	case "commit":
		if c.commitErr != nil {
			return &orchestration.ExecResult{ExitCode: 1, Stderr: c.commitErr.Error()}, nil
		}
		c.commits = append(c.commits, args[len(args)-1])
		c.commitConfig = config
		c.changed = false
		c.ahead = true
		c.head = "head-" + itoa(len(c.commits))
		commit := outgoingCommit{SHA: c.head, Status: "N"}
		if slices.Contains(config, "commit.gpgsign=true") {
			commit.Status = "G"
			for _, cfg := range config {
				if key, ok := strings.CutPrefix(cfg, "user.signingkey=key::"); ok {
					if fp, err := sshKeyFingerprint(key); err == nil {
						commit.Signer = fp
					}
				}
			}
		}
		c.outgoing = append(c.outgoing, commit)
		return &orchestration.ExecResult{}, nil
	case "log":
		if c.logErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.logErr.Error()}, nil
		}
		c.logConfig = config
		c.logRange = args[len(args)-1]
		outgoing := c.outgoing
		if c.ahead && len(outgoing) == 0 {
			outgoing = []outgoingCommit{{SHA: "worker-1", Status: "G", Signer: testSigningFingerprint}}
		}
		var out strings.Builder
		for _, commit := range outgoing {
			out.WriteString(commit.logLine() + "\n")
		}
		return &orchestration.ExecResult{Stdout: out.String()}, nil
	case "rev-list":
		// THE RANGE SAYS WHICH QUESTION IS BEING ASKED. "origin/<base>..HEAD"
		// counts what the branch carries that its base does not;
		// "HEAD..origin/<base>" counts the other way. Answering one with the
		// other would make every branch that is merely ahead look behind.
		if strings.HasPrefix(args[len(args)-1], "HEAD..") {
			return &orchestration.ExecResult{Stdout: itoa(c.behind) + "\n"}, nil
		}
		if c.ahead {
			return &orchestration.ExecResult{Stdout: "1\n"}, nil
		}
		return &orchestration.ExecResult{Stdout: "0\n"}, nil
	case "worktree":
		return c.worktree(dir, args[1:])
	case "merge":
		ref := args[len(args)-1]
		c.merges = append(c.merges, gitMergeCall{Dir: dir, Ref: ref, Args: args[1:]})
		if c.mergeErr != nil {
			// git's own shape for a merge that never got as far as the
			// trees: a non-zero exit, a reason on stderr, and NO unmerged
			// path anywhere.
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.mergeErr.Error() + "\n"}, nil
		}
		if len(c.mergeConflicts) > 0 {
			return &orchestration.ExecResult{
				ExitCode: 1,
				Stdout:   "Auto-merging " + c.mergeConflicts[0] + "\nCONFLICT (content): Merge conflict in " + c.mergeConflicts[0] + "\n",
				Stderr:   "Automatic merge failed; fix conflicts and then commit the result.\n",
			}, nil
		}
		return &orchestration.ExecResult{Stdout: "Automatic merge went well; stopped before committing as requested\n"}, nil
	case "diff-unmerged":
		// The unmerged-paths probe a refused merge is asked for.
		if c.conflictProbeErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.conflictProbeErr.Error() + "\n"}, nil
		}
		if len(c.mergeConflicts) == 0 {
			return &orchestration.ExecResult{}, nil
		}
		return &orchestration.ExecResult{Stdout: strings.Join(c.mergeConflicts, "\n") + "\n"}, nil
	case "push":
		for _, arg := range args[1:] {
			// The factory never force-pushes, leased or not (handbook
			// version-control §5b). A push that asks for it is a defect, and
			// this container refuses it loudly rather than modelling it.
			if arg == "-f" || strings.Contains(arg, "force") {
				return nil, &InvalidInputError{Field: "cmd", Message: "a force push was requested: " + strings.Join(args, " ")}
			}
		}
		if c.pushErr != nil {
			return &orchestration.ExecResult{ExitCode: 1, Stderr: c.pushErr.Error()}, nil
		}
		branch, remote, _ := strings.Cut(args[len(args)-1], ":")
		if c.diverged {
			return &orchestration.ExecResult{ExitCode: 1,
				Stderr: " ! [rejected]        " + branch + " -> " + branch + " (non-fast-forward)"}, nil
		}
		c.pushes = append(c.pushes, pushedRef{Branch: branch, RemoteBranch: remote, Args: append([]string(nil), args[1:]...)})
		return &orchestration.ExecResult{}, nil
	case "ls-files":
		if c.tree == nil {
			return nil, &InvalidInputError{Field: "cmd", Message: "this container does not model git ls-files"}
		}
		return &orchestration.ExecResult{Stdout: lsFiles(c.tree, args[1:])}, nil
	case "grep":
		if c.tree == nil {
			return nil, &InvalidInputError{Field: "cmd", Message: "this container does not model git grep"}
		}
		needle := args[len(args)-1]
		var out strings.Builder
		for file, content := range c.tree {
			for n, line := range strings.Split(content, "\n") {
				if strings.Contains(line, needle) {
					out.WriteString(file + ":" + strconv.Itoa(n+1) + ":" + line + "\n")
				}
			}
		}
		if out.Len() == 0 {
			return &orchestration.ExecResult{ExitCode: 1}, nil
		}
		return &orchestration.ExecResult{Stdout: out.String()}, nil
	case "rev-parse":
		if len(args) > 1 && args[1] == "FETCH_HEAD" {
			// The commit the last fetch landed on, which is the base as the
			// remote holds it — never this checkout's HEAD.
			if c.baseHead == "" {
				return &orchestration.ExecResult{Stdout: defaultBaseHead + "\n"}, nil
			}
			return &orchestration.ExecResult{Stdout: c.baseHead + "\n"}, nil
		}
		if len(args) > 2 && args[1] == "--git-path" {
			// Where THIS checkout keeps the path it was asked about. It is
			// not always under ".git/": a checkout that is itself a linked
			// worktree keeps its index elsewhere, which is why the publisher
			// asks git rather than assuming.
			return &orchestration.ExecResult{Stdout: path.Join(".git", args[2]) + "\n"}, nil
		}
		if len(args) > 1 && args[1] == "--git-dir" {
			if c.repos[dir] {
				return &orchestration.ExecResult{Stdout: ".git\n"}, nil
			}
			return &orchestration.ExecResult{ExitCode: 128, Stderr: "not a git repository: " + dir}, nil
		}
		if len(args) > 2 && args[1] == "--abbrev-ref" {
			branch := c.branches[dir]
			if branch == "" {
				// git's own answer for a checkout on no branch.
				branch = "HEAD"
			}
			return &orchestration.ExecResult{Stdout: branch + "\n"}, nil
		}
		if c.headErr != nil {
			return &orchestration.ExecResult{ExitCode: 128, Stderr: c.headErr.Error()}, nil
		}
		return &orchestration.ExecResult{Stdout: c.headOf(dir) + "\n"}, nil
	case "remote":
		if len(args) < 3 || args[1] != "get-url" {
			return nil, &InvalidInputError{Field: "cmd", Message: "this container models only git remote get-url"}
		}
		origin, ok := c.origins[dir]
		if !ok {
			return &orchestration.ExecResult{
				ExitCode: 2,
				Stderr:   "error: No such remote '" + args[2] + "'\n",
			}, nil
		}
		return &orchestration.ExecResult{Stdout: origin + "\n"}, nil
	}
	return nil, &InvalidInputError{Field: "cmd", Message: "this container does not model git " + args[0]}
}

// worktree models "git worktree add" and "git worktree remove", which is how
// a merge result is prepared and thrown away. The caller holds the lock.
func (c *fakeContainer) worktree(from string, args []string) (*orchestration.ExecResult, error) {
	if len(args) < 2 {
		return nil, &InvalidInputError{Field: "cmd", Message: "git worktree was run without a subcommand and a directory"}
	}
	switch args[0] {
	case "list":
		// git's own porcelain: one stanza per worktree, the checkout first.
		var b strings.Builder
		b.WriteString("worktree " + from + "\nHEAD " + c.headOf(from) + "\n\n")
		for dir, ref := range c.worktrees {
			b.WriteString("worktree " + dir + "\nHEAD " + ref + "\ndetached\n\n")
		}
		return &orchestration.ExecResult{Stdout: b.String()}, nil
	case "add":
		// git worktree add --detach <dir> <ref>
		if len(args) < 3 {
			return nil, &InvalidInputError{Field: "cmd", Message: "git worktree add named no directory and ref"}
		}
		dir, ref := args[len(args)-2], args[len(args)-1]
		if _, live := c.worktrees[dir]; live {
			return &orchestration.ExecResult{
				ExitCode: 128,
				Stderr:   "fatal: '" + dir + "' already exists\n",
			}, nil
		}
		if c.worktrees == nil {
			c.worktrees = map[string]string{}
		}
		c.worktrees[dir] = ref
		c.worktreeAdds = append(c.worktreeAdds, worktreeCall{Dir: dir, Ref: ref, From: from})
		return &orchestration.ExecResult{}, nil
	case "remove":
		dir := args[len(args)-1]
		c.worktreeRemovals = append(c.worktreeRemovals, dir)
		if c.worktreeRemoveErr != nil {
			return &orchestration.ExecResult{ExitCode: 1, Stderr: c.worktreeRemoveErr.Error() + "\n"}, nil
		}
		if _, live := c.worktrees[dir]; !live {
			// git's own answer, and the ordinary one: nothing to remove.
			return &orchestration.ExecResult{
				ExitCode: 128,
				Stderr:   "fatal: '" + dir + "' is not a working tree\n",
			}, nil
		}
		delete(c.worktrees, dir)
		return &orchestration.ExecResult{}, nil
	}
	return nil, &InvalidInputError{Field: "cmd", Message: "this container does not model git worktree " + args[0]}
}

// remove models "rm -f <path>", the one non-git command the publisher runs.
// A file that was never there is a SUCCESS, exactly as rm -f answers, which
// is what makes a real failure distinguishable from an ordinary one.
// credentialTier answers the two commands that write and remove the forge
// credential on the tmpfs; handled is false for every other command. A
// stopped container refuses them as it refuses everything else.
func (c *fakeContainer) credentialTier(containerID string, spec *orchestration.ExecSpec) (handled bool, result *orchestration.ExecResult, err error) {
	cmd := spec.Cmd
	delivery := len(cmd) == 5 && cmd[0] == verificationShell && cmd[2] == deliverForgeCredentialScript
	revocation := len(cmd) == 4 && cmd[0] == removeCommand && cmd[3] == orchestration.WorkspaceForgeCredentialFile
	if !delivery && !revocation {
		return false, nil, nil
	}
	if c.stopped {
		return true, nil, &orchestration.ContainerNotRunningError{
			ContainerID: containerID,
			Message:     "Error response from daemon: container " + containerID + " is not running",
		}
	}
	if revocation {
		c.credential = nil
		c.credentialRevoked++
		return true, &orchestration.ExecResult{}, nil
	}
	if cmd[4] != orchestration.WorkspaceForgeCredentialFile || len(spec.Stdin) == 0 {
		return true, nil, &InvalidInputError{Field: "cmd", Message: "a forge credential was delivered empty or off the credential tier"}
	}
	c.credential = bytes.Clone(spec.Stdin)
	c.credentialDeliveries++
	if c.credentialDeliveries == 1 {
		c.credentialAt = len(c.execs)
	}
	return true, &orchestration.ExecResult{}, nil
}

func (c *fakeContainer) remove(args []string) (*orchestration.ExecResult, error) {
	force := false
	var target string
	for _, arg := range args {
		if arg == "-f" {
			force = true
			continue
		}
		target = arg
	}
	if target == "" {
		return nil, &InvalidInputError{Field: "cmd", Message: "rm named no path"}
	}
	c.removals = append(c.removals, target)
	if c.removeErr != nil {
		return &orchestration.ExecResult{ExitCode: 1, Stderr: c.removeErr.Error() + "\n"}, nil
	}
	if !force {
		return nil, &InvalidInputError{Field: "cmd", Message: "a cleanup removed a file without -f, so a file that was never there is a failure"}
	}
	return &orchestration.ExecResult{}, nil
}

// copy models "cp <from> <to>", which is how the throwaway index is seeded
// from the worker's own. Git has no command that copies an index.
func (c *fakeContainer) copy(dir string, args []string) (*orchestration.ExecResult, error) {
	if len(args) != 2 {
		return nil, &InvalidInputError{Field: "cmd", Message: "cp names exactly a source and a destination"}
	}
	c.copies = append(c.copies, copyCall{From: args[0], To: args[1], Dir: dir})
	if c.copyErr != nil {
		return &orchestration.ExecResult{ExitCode: 1, Stderr: c.copyErr.Error() + "\n"}, nil
	}
	return &orchestration.ExecResult{}, nil
}

// copiedFrom answers what a path was last copied from, empty when nothing
// was. The caller holds the lock.
func (c *fakeContainer) copiedFrom(to string) string {
	from := ""
	for _, copied := range c.copies {
		if copied.To == to {
			from = copied.From
		}
	}
	return from
}

// resolveStart answers what "checkout -B <name> <start>" puts HEAD on, and
// whether this container can resolve the start at all. The caller holds the
// lock.
func (c *fakeContainer) resolveStart(start string) (string, bool) {
	if branch, isRemote := strings.CutPrefix(start, gitRemote+"/"); isRemote {
		if c.remoteBranches == nil {
			return "", true
		}
		sha, known := c.remoteBranches[branch]
		return sha, known
	}
	if c.knownCommits == nil {
		return start, true
	}
	return start, c.knownCommits[start]
}

// currentHead is the commit the container's checkout is on.
func (c *fakeContainer) currentHead() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head
}

// authenticate answers the refusal a forge that REQUIRES credentials gives a
// clone this container could not authenticate, or nil when it could.
//
// The rule is git's own, not an invention: an ssh remote is authenticated by a
// client key, which reaches a container as an agent socket named by
// SSH_AUTH_SOCK, and an https remote by a credential helper git is configured
// with. A container holding neither gets the error the real forge gives.
//
// IT IS WHAT MAKES THIS FIXTURE ABLE TO SEE go-quicbaas#929 AT ALL. A
// permissive container clones whatever it is asked to, so a runner that built a
// spawn no real forge would talk to looked perfectly correct in every test.
//
// The caller holds the lock.
func (c *fakeContainer) authenticate(url string, config []string) *orchestration.ExecResult {
	switch orchestration.CheckoutCredentialFor(url) {
	case orchestration.CheckoutCredentialSSHAgent:
		if strings.TrimSpace(c.sshAgentSocket) != "" {
			return nil
		}
		return &orchestration.ExecResult{
			ExitCode: 128,
			Stderr: "Cloning into '" + path.Base(url) + "'...\ngit@forge.test: Permission denied (publickey).\r\n" +
				"fatal: Could not read from remote repository.",
		}
	case orchestration.CheckoutCredentialForgeToken:
		for _, setting := range config {
			if strings.HasPrefix(setting, "credential.helper=") && len(setting) > len("credential.helper=") {
				return nil
			}
		}
		return &orchestration.ExecResult{
			ExitCode: 128,
			Stderr:   "fatal: could not read Username for 'https://forge.test': terminal prompts disabled",
		}
	default:
		return nil
	}
}

// markRepo records that a directory is now a git repository cloned from url:
// it is a repository, its origin is that url, and its HEAD is on a commit.
//
// ALL THREE, because all three are what a real clone leaves behind and all
// three are what the pre-flight asks for. A fixture that recorded only the
// first would make every clone it models look like a repository with no
// remote and no commit in it.
//
// The caller holds the lock.
func (c *fakeContainer) markRepo(dir, url string) {
	if c.repos == nil {
		c.repos = map[string]bool{}
	}
	c.repos[dir] = true
	if c.origins == nil {
		c.origins = map[string]string{}
	}
	c.origins[dir] = url
	if c.heads == nil {
		c.heads = map[string]string{}
	}
	c.heads[dir] = "cloned-" + path.Base(dir)
}

// headOf is the commit a directory's HEAD is on: the project checkout's own
// mutable head, which a commit and a "checkout -B" both move and tests assert
// against, or the synthetic one a clone recorded for a context repository.
// The caller holds the lock.
func (c *fakeContainer) headOf(dir string) string {
	// The scalar wins wherever it is set: a test that sets or moves `head` is
	// asserting about the project checkout, and a fixture that models no
	// checkout path at all has only the scalar to answer with.
	if c.head != "" && (c.checkout == "" || dir == c.checkout) {
		return c.head
	}
	return c.heads[dir]
}

// setBranch records which branch a checkout landed on, from the operands of a
// "git checkout": "-B <name> <start>" lands on <name>, a plain "checkout <ref>"
// on <ref>. Any other form leaves the record alone rather than guessing.
// The caller holds the lock.
func (c *fakeContainer) setBranch(dir string, operands []string) {
	branch := ""
	switch {
	case len(operands) == 3 && operands[0] == "-B":
		branch = operands[1]
	case len(operands) == 1:
		branch = operands[0]
	default:
		return
	}
	if c.branches == nil {
		c.branches = map[string]string{}
	}
	c.branches[dir] = branch
}

// verify answers one verification command. "true" and "false" are the shell
// built-ins the runner's own tests verify with and mean here exactly what
// they mean in a shell; anything else a test needs is scripted, and a
// command this container models neither way is refused rather than quietly
// passed.
func (c *fakeContainer) verify(workDir string, cmd []string) (*orchestration.ExecResult, error) {
	if len(cmd) != 3 || cmd[1] != "-c" {
		return nil, &InvalidInputError{Field: "cmd", Message: "a verification command is the shell, -c, and one command line"}
	}
	// A MERGE-RESULT WORKTREE IS A DIFFERENT TREE, so a command may answer
	// differently in it. That is the entire fault #982 is about, and a
	// fixture that answered identically either way could not show it.
	if strings.HasPrefix(path.Base(workDir), mergeResultDirPrefix) {
		if scripted, ok := c.mergeVerifications[cmd[2]]; ok {
			return scripted, nil
		}
	}
	if scripted, ok := c.verifications[cmd[2]]; ok {
		return scripted, nil
	}
	switch cmd[2] {
	case "true":
		return &orchestration.ExecResult{}, nil
	case "false":
		return &orchestration.ExecResult{ExitCode: 1, Stderr: "false: the command failed\n"}, nil
	}
	return nil, &InvalidInputError{Field: "cmd", Message: "this container does not model " + cmd[2]}
}

// scriptVerification is what one verification command answers.
func (c *fakeContainer) scriptVerification(command string, result *orchestration.ExecResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.verifications == nil {
		c.verifications = map[string]*orchestration.ExecResult{}
	}
	c.verifications[command] = result
}

// scriptMergeResultVerification is what one verification command answers when
// it runs on the MERGE RESULT rather than on the branch as it stands.
func (c *fakeContainer) scriptMergeResultVerification(command string, result *orchestration.ExecResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mergeVerifications == nil {
		c.mergeVerifications = map[string]*orchestration.ExecResult{}
	}
	c.mergeVerifications[command] = result
}

// splitGitConfig separates the leading "-c key=value" pairs from the
// subcommand, so a test can assert on the configuration git was given
// without parsing the whole vector.
func splitGitConfig(argv []string) (config []string, args []string) {
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-c" && i+1 < len(argv) {
			config = append(config, argv[i+1])
			i++
			continue
		}
		return config, argv[i:]
	}
	return config, nil
}

// commandsRun renders every command the container was asked to run, one
// space-joined line each.
func (c *fakeContainer) commandsRun() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.execs))
	for _, e := range c.execs {
		out = append(out, strings.Join(e.Cmd, " "))
	}
	return out
}

// clonesMade is every repository the checkout step cloned.
func (c *fakeContainer) clonesMade() []clonedRepo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]clonedRepo(nil), c.clones...)
}

// seedRepo makes dir look like a repository already cloned from url: the mount
// a dead predecessor left behind, which the clone's idempotence adopts.
// seedFile puts one file in the checkout the probes see.
func (c *fakeContainer) seedFile(path, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tree == nil {
		c.tree = map[string]string{}
	}
	c.tree[path] = content
}

func (c *fakeContainer) seedRepo(dir, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.markRepo(dir, url)
}

// configsWritten is every repository-local configuration it wrote.
func (c *fakeContainer) configsWritten() []gitConfigSet {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]gitConfigSet(nil), c.configs...)
}

// fetchesRun is every fetch it ran.
func (c *fakeContainer) fetchesRun() []fetchCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]fetchCall(nil), c.fetches...)
}

// checkoutsRun is every checkout it ran.
func (c *fakeContainer) checkoutsRun() []checkoutCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]checkoutCall(nil), c.checkouts...)
}

// dirsUsed is every working directory the runner named inside the container.
func (c *fakeContainer) dirsUsed() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, e := range c.execs {
		if !seen[e.WorkDir] {
			seen[e.WorkDir] = true
			out = append(out, e.WorkDir)
		}
	}
	return out
}

// =============================================================================
// The signing agent
// =============================================================================

// fakeSigningAgent stands in for the KMS-backed agent, keeping the two
// properties the runner depends on: the directory is CREATED by the session
// (so the socket the spawn mounts exists before the container starts) and it
// is REMOVED on stop (so nothing is left in the tenant's tree).
type fakeSigningAgent struct {
	mu        sync.Mutex
	dirs      []string
	scopes    []SigningScope
	stops     int
	publicKey string
	err       error
}

func (a *fakeSigningAgent) StartSigningAgent(
	_ context.Context,
	project *orchestration.Project,
	socketDir string,
	scope SigningScope,
) (*SigningSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return nil, a.err
	}
	if project == nil {
		return nil, &InvalidInputError{Field: "project", Message: "a signing agent serves a project's identity"}
	}
	if _, err := os.Stat(socketDir); err == nil {
		return nil, &InvalidInputError{Field: "socket_dir", Message: "already exists: " + socketDir}
	}
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		return nil, err
	}
	socket := filepath.Join(socketDir, SigningSocketFile)
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		return nil, err
	}
	a.dirs = append(a.dirs, socketDir)
	a.scopes = append(a.scopes, scope)
	key := a.publicKey
	// A CHECKOUT SCOPE SERVES NO COMMIT SIGNING KEY, so it answers with no
	// public half — exactly as the KMS-backed agent does. The fake keeps that
	// property because the seam refuses a delivery scope that answers with
	// none, and a fake that always answered with one could not see it.
	if scope == SigningScopeCheckout {
		key = ""
	} else if key == "" {
		key = testSigningKey
	}
	return &SigningSession{
		SocketPath: socket,
		PublicKey:  key,
		Stop: func(context.Context) {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.stops++
			_ = os.RemoveAll(socketDir)
		},
	}, nil
}

// startedScopes is every scope the agent was asked for, in order. It is what
// proves a reader asked for the checkout scope and the delivery runner for the
// delivery one.
func (a *fakeSigningAgent) startedScopes() []SigningScope {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]SigningScope(nil), a.scopes...)
}

// startedDirs is every socket directory the agent was asked for.
func (a *fakeSigningAgent) startedDirs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.dirs...)
}

// stopCount is how many sessions were torn down.
func (a *fakeSigningAgent) stopCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stops
}

// memoryClusters is an in-memory IssueClusterStore, for tests that build a
// triage run by hand rather than through a rig with a store.
type memoryClusters struct {
	mu       sync.Mutex
	clusters map[int]*IssueCluster
	getErr   error
	putErr   error
}

func newMemoryClusters() *memoryClusters {
	return &memoryClusters{clusters: map[int]*IssueCluster{}}
}

func (m *memoryClusters) GetIssueCluster(_ context.Context, tenantID, projectID string, issue int) (*IssueCluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	cluster, ok := m.clusters[issue]
	if !ok || cluster.ProjectID != projectID {
		return nil, &NotFoundError{Entity: issueClusterEntityName, TenantID: tenantID, ID: projectID}
	}
	copied := *cluster
	copied.Issue = issue
	return &copied, nil
}

func (m *memoryClusters) PutIssueCluster(_ context.Context, _ string, cluster *IssueCluster) error {
	if err := cluster.Validate(); err != nil {
		return err
	}
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, issue := range cluster.Issues() {
		copied := *cluster
		m.clusters[issue] = &copied
	}
	return nil
}

func (m *memoryClusters) DeleteIssueCluster(_ context.Context, _ string, cluster *IssueCluster) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, issue := range cluster.Issues() {
		if held, ok := m.clusters[issue]; ok && held.Anchor == cluster.Anchor {
			delete(m.clusters, issue)
		}
	}
	return nil
}

// lsFiles answers "git ls-files -- <pathspec>..." over a tree the way git
// does for the two pathspec forms the module sends: a path from the root, and
// ":(glob)**/<path>", the same path under any directory.
func lsFiles(tree map[string]string, args []string) string {
	var out strings.Builder
	specs := args
	if i := slices.Index(args, "--"); i >= 0 {
		specs = args[i+1:]
	}
	files := make([]string, 0, len(tree))
	for file := range tree {
		files = append(files, file)
	}
	slices.Sort(files)
	for _, file := range files {
		for _, spec := range specs {
			rest, glob := strings.CutPrefix(spec, ":(glob)**/")
			if file == spec || (glob && (file == rest || strings.HasSuffix(file, "/"+rest))) {
				out.WriteString(file + "\n")
				break
			}
		}
	}
	return out.String()
}

// deleted reports the branches DeleteBranch removed, in order.
func (f *fakeForge) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletedBranches...)
}

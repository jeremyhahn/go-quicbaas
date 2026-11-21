//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/githubitest"
)

// The live GitHub merge-train e2e: a real node, bound with forge type
// github to the scratch repository githubitest.LoadEnv names, lands a train
// of approved pull requests through a queue/<n> pull request whose CI is the
// scratch repository's queue-ci.yml. Without the environment every test
// here FAILS, naming each missing variable; it never skips.
const (
	githubTrainProject  = "sdlc-github-train-e2e"
	githubTrainTokenRef = "github-train-e2e-token"
	githubTrainTrigger  = "train-itest:ready"

	// githubTrainDeadline bounds each wait on GitHub: a worker round, an
	// Actions run on the queue head and the merge all happen inside it.
	githubTrainDeadline = 30 * time.Minute
	githubTrainPoll     = 5 * time.Second
)

// githubTrainRig is one node bound to one per-run base branch.
type githubTrainRig struct {
	srv       *server.Server
	backend   *e2eAgentBackend
	scratch   *githubitest.Scratch
	projectID string
	base      string
}

// startGitHubTrain cuts the per-run base, installs the queue CI on it, boots
// a factory node and binds the project to the scratch repository. Cleanup is
// registered before the node, so the node stops first and the scratch
// branches, pull requests and issues are removed after it.
func startGitHubTrain(t *testing.T, env githubitest.Env) *githubTrainRig {
	t.Helper()
	scratch, err := githubitest.New(env, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := scratch.Cleanup(context.Background()); err != nil {
			t.Errorf("cleaning up the scratch repository: %v", err)
		}
	})
	ctx := context.Background()
	base := scratch.Namespace() + "-base"
	require.NoError(t, scratch.CutBase(ctx, base), "cutting the per-run base")
	_, err = scratch.InstallQueueCI(ctx, base)
	require.NoError(t, err, "installing the queue CI on the base")

	srv := startFactoryNode(t, t.TempDir(), true, time.Hour, 1)
	t.Cleanup(func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Logf("node shutdown: %v", err)
		}
	})
	backend := newE2EBackend()
	projectID := registerGitHubTrainProject(t, srv, backend, env, base)
	return &githubTrainRig{srv: srv, backend: backend, scratch: scratch, projectID: projectID, base: base}
}

// registerGitHubTrainProject is registerFactoryProject for the scratch
// repository: the same tenant, signing key, scripted worker and container
// runtime, but the binding is forge type github on the scratch repository,
// the forge token from the environment is the binding's token secret, the
// strategy is github-flow (which lands by squash) and the train is on.
// Discovery is by poll: no forge can reach this node's webhook origin.
func registerGitHubTrainProject(t *testing.T, srv *server.Server, backend *e2eAgentBackend, env githubitest.Env, base string) string {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	orchCtx := orchestration.ContextWithTenant(ctx, loopE2ETenant)
	ensureFactoryTenant(t, srv)

	secrets := srv.Service().Secrets()
	require.NotNil(t, secrets)
	_, err := secrets.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: githubTrainTokenRef, Data: []byte(env.Token)})
	require.NoError(t, err, "storing the forge token")

	sshClient := srv.Service().SSHClient()
	require.NotNil(t, sshClient, "the node has no SSH key store to mint the signing key in")
	_, err = sshClient.Keys().Generate(ctx, xkmsssh.GenerateRequest{
		Scope: loopE2EKeyScope, Owner: loopE2EKeyOwner, Name: loopE2EKeyName, Algorithm: "ed25519", Comment: "sdlc github train e2e",
	})
	require.NoError(t, err, "minting the signing key")

	agents := srv.Service().Agent()
	require.NotNil(t, agents)
	require.NoError(t, agents.RegisterBackend(orchCtx, &orchestration.RegisterBackendRequest{Name: loopE2EBackend, Backend: backend}))
	workerRef := defineFactoryWorker(t, orchCtx, agents)
	runtimeSetter, ok := agents.(interface {
		SetContainerRuntime(orchestration.ContainerRuntime)
	})
	require.True(t, ok, "the agent service does not accept a container runtime")
	runtimeSetter.SetContainerRuntime(backend.runtime)

	projects := srv.Service().Project()
	require.NotNil(t, projects)
	created, err := projects.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID: loopE2ETenant, Name: githubTrainProject,
		WorkspacePath: factoryProjectWorkspace(t),
		GitRepoURL:    env.CloneURL(), GitDefaultBranch: base,
		GitAuthorName: "agent-" + githubTrainProject, GitAuthorEmail: "agent-" + githubTrainProject + "@example.internal",
		GitSigningEnabled: true, GitSigningKeyName: loopE2EKeyName, GitSigningKeyScope: loopE2EKeyScope, GitSigningKeyOwner: loopE2EKeyOwner,
	})
	require.NoError(t, err, "creating the project")

	_, err = srv.SDLCService().Bind(ctx, loopE2ETenant, &sdlc.ProjectBinding{
		ProjectID: created.Project.ID,
		Forge: sdlc.ForgeConfig{
			Type: sdlc.ForgeGitHub, BaseURL: env.BaseURL, Owner: env.Owner, Repo: env.Repo,
			TriggerLabel: githubTrainTrigger,
			Labels: map[string]string{
				sdlc.LabelInProgress: "train-itest:in-progress", sdlc.LabelInReview: "train-itest:in-review",
				sdlc.LabelMerged: "train-itest:merged", sdlc.LabelEscalated: "train-itest:escalated",
				sdlc.LabelTrainEjected: "train-itest:ejected",
			},
			TokenSecretRef: githubTrainTokenRef,
		},
		Strategy:        sdlc.GitHubFlowName,
		Worker:          workerRef,
		Verification:    []string{"test -f WORK.md"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1},
		MaxReviewRounds: 3,
		Discovery:       sdlc.DiscoveryPoll,
		MergeTrain:      sdlc.MergeTrainPolicy{MaxSize: 4},
	}, service.BindOptions{})
	require.NoError(t, err, "binding the project to the scratch repository")
	return created.Project.ID
}

// pollGitHub calls done every githubTrainPoll until it reports true, and
// fails the test when the deadline passes first. An error from done is kept
// and reported at the deadline: GitHub answers transient failures.
func pollGitHub(t *testing.T, what string, done func(ctx context.Context) (bool, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), githubTrainDeadline)
	defer cancel()
	ticker := time.NewTicker(githubTrainPoll)
	defer ticker.Stop()
	var last error
	for {
		ok, err := done(ctx)
		if err == nil && ok {
			return
		}
		if err != nil {
			last = err
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: not reached within %s (last error: %v)", what, githubTrainDeadline, last)
		case <-ticker.C:
		}
	}
}

// openDaemonCar opens an issue carrying the trigger label and waits for the
// daemon's own delivery to open its pull request into the base. Any open
// pull request into the base that is neither a train branch nor one of the
// fixture's own branches is the daemon's.
func (r *githubTrainRig) openDaemonCar(t *testing.T, fixtureBranches ...string) githubitest.PullRequest {
	t.Helper()
	_, err := r.scratch.OpenIssue(context.Background(), "Train car A", "The scripted worker writes WORK.md.", githubTrainTrigger)
	require.NoError(t, err, "opening car A's issue")
	var car githubitest.PullRequest
	pollGitHub(t, "the daemon opens car A's pull request", func(ctx context.Context) (bool, error) {
		prs, err := r.scratch.ListPullRequests(ctx, r.base)
		if err != nil {
			return false, err
		}
		for _, pr := range prs {
			if pr.State != "open" || strings.HasPrefix(pr.Head.Ref, sdlc.TrainBranchPrefix) || containsString(fixtureBranches, pr.Head.Ref) {
				continue
			}
			car = pr
			return true, nil
		}
		return false, nil
	})
	r.scratch.TrackPullRequest(car.Number)
	r.scratch.TrackBranch(car.Head.Ref)
	return car
}

// openFixtureCar commits one file on its own branch, opens the pull request
// as the fixture and adopts it with its issue, as an operator brings in a
// pull request the daemon did not open.
func (r *githubTrainRig) openFixtureCar(t *testing.T, name, path string) githubitest.PullRequest {
	t.Helper()
	ctx := context.Background()
	branch := r.scratch.Namespace() + "-" + name
	require.NoError(t, r.scratch.CutBranch(ctx, branch, r.base))
	_, err := r.scratch.CommitFile(ctx, branch, path, "train car "+name+"\n", "add "+path)
	require.NoError(t, err)
	issue, err := r.scratch.OpenIssue(ctx, "Train car "+name, "Opened by the fixture, adopted with its pull request.")
	require.NoError(t, err)
	number, err := r.scratch.OpenPR(ctx, branch, r.base, "Train car "+name)
	require.NoError(t, err)
	_, err = r.srv.SDLCService().AdoptIssue(factoryCtx(), loopE2ETenant, sdlc.AdoptIssueRequest{
		ProjectID: r.projectID, Issue: issue, PullRequest: number,
	})
	require.NoError(t, err, "adopting car %s", name)
	pr, err := r.scratch.GetPullRequest(ctx, number)
	require.NoError(t, err)
	return pr
}

// approveAll approves every car at its head in one pass, so every car is
// approved before the train runner next forms a train.
func (r *githubTrainRig) approveAll(t *testing.T, cars ...githubitest.PullRequest) {
	t.Helper()
	for _, car := range cars {
		require.NoError(t, r.scratch.Approve(context.Background(), car.Number, car.Head.SHA), "approving #%d", car.Number)
	}
}

// awaitSettled waits until every car's train entry is landed or ejected
// and returns the entries by pull request. Adopting a pull request no train
// opened must not start a worker attempt: the only worker spawned is car A's,
// and a second one fails the wait at once (#2221).
func (r *githubTrainRig) awaitSettled(t *testing.T, cars ...githubitest.PullRequest) map[int]*sdlc.TrainEntry {
	t.Helper()
	settled := map[int]*sdlc.TrainEntry{}
	pollGitHub(t, "every car lands or is ejected", func(ctx context.Context) (bool, error) {
		if spawned := r.backend.spawnCount(); spawned > 1 {
			t.Fatalf("%d worker sessions spawned; adopting a pull request no train opened must not start a worker attempt", spawned)
		}
		entries, err := r.srv.SDLCService().Store().ListTrainEntries(factoryCtx(), loopE2ETenant, r.projectID)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if entry.State == sdlc.TrainEntryLanded || entry.State == sdlc.TrainEntryEjected {
				settled[entry.PullRequest] = entry
			}
		}
		for _, car := range cars {
			if settled[car.Number] == nil {
				return false, nil
			}
		}
		return true, nil
	})
	return settled
}

// awaitBranchesGone waits until none of branches is on the repository.
func (r *githubTrainRig) awaitBranchesGone(t *testing.T, branches ...string) {
	t.Helper()
	pollGitHub(t, "the landed branches are deleted", func(ctx context.Context) (bool, error) {
		for _, branch := range branches {
			exists, err := r.scratch.BranchExists(ctx, branch)
			if err != nil || exists {
				return false, err
			}
		}
		return true, nil
	})
}

// trainPullRequests lists the queue/* pull requests into the base.
func (r *githubTrainRig) trainPullRequests(t *testing.T) []githubitest.PullRequest {
	t.Helper()
	prs, err := r.scratch.ListPullRequests(context.Background(), r.base)
	require.NoError(t, err)
	var trains []githubitest.PullRequest
	for _, pr := range prs {
		if strings.HasPrefix(pr.Head.Ref, sdlc.TrainBranchPrefix) {
			r.scratch.TrackPullRequest(pr.Number)
			r.scratch.TrackBranch(pr.Head.Ref)
			trains = append(trains, pr)
		}
	}
	return trains
}

// requireOnBase asserts which car files the base's tree holds.
func (r *githubTrainRig) requireOnBase(t *testing.T, want map[string]bool) {
	t.Helper()
	for path, present := range want {
		exists, err := r.scratch.FileExists(context.Background(), r.base, path)
		require.NoError(t, err)
		require.Equal(t, present, exists, "%s on the base", path)
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TestGitHubTrain_TwoApprovedCarsOneAdoptedLandAsOneSquash is #2221's green
// run: car A is the daemon's own delivery, car B a pull request the daemon
// did not open, brought in through adopt. One queue/<n> pull request is
// built, ci-gate runs once on its head, the train lands by squash and every
// branch it carried is deleted.
func TestGitHubTrain_TwoApprovedCarsOneAdoptedLandAsOneSquash(t *testing.T) {
	env, err := githubitest.LoadEnv()
	if err != nil {
		t.Fatal(err)
	}
	rig := startGitHubTrain(t, env)
	ctx := context.Background()

	carB := rig.openFixtureCar(t, "car-b", "car-b.txt")
	carA := rig.openDaemonCar(t, carB.Head.Ref)
	rig.approveAll(t, carA, carB)
	settled := rig.awaitSettled(t, carA, carB)

	trains := rig.trainPullRequests(t)
	require.Len(t, trains, 1, "one queue/<n> pull request carries both cars")
	train, err := rig.scratch.GetPullRequest(ctx, trains[0].Number)
	require.NoError(t, err)
	require.True(t, train.Merged, "the train pull request is merged")

	runs, err := rig.scratch.CheckRuns(ctx, train.Head.SHA)
	require.NoError(t, err)
	var gates []githubitest.CheckRun
	for _, run := range runs {
		if run.Name == githubitest.QueueCICheck {
			gates = append(gates, run)
		}
	}
	require.Len(t, gates, 1, "ci-gate runs once on the queue head")
	require.Equal(t, "success", gates[0].Conclusion)

	parents, err := rig.scratch.CommitParents(ctx, train.MergeCommitSHA)
	require.NoError(t, err)
	require.Len(t, parents, 1, "the train lands as a squash commit")
	head, err := rig.scratch.BranchHead(ctx, rig.base)
	require.NoError(t, err)
	require.Equal(t, train.MergeCommitSHA, head, "the squash commit is the base's head")

	for _, car := range []githubitest.PullRequest{carA, carB} {
		entry := settled[car.Number]
		require.Equal(t, sdlc.TrainEntryLanded, entry.State, "car #%d", car.Number)
		require.Equal(t, train.Number, entry.TrainPullRequest, "car #%d rode the one train", car.Number)
		require.Equal(t, train.MergeCommitSHA, entry.MergeSHA, "car #%d landed in the squash commit", car.Number)
		landed, err := rig.scratch.GetPullRequest(ctx, car.Number)
		require.NoError(t, err)
		require.Equal(t, "closed", landed.State, "car #%d is closed once it lands", car.Number)
	}
	rig.requireOnBase(t, map[string]bool{"WORK.md": true, "car-b.txt": true})
	rig.awaitBranchesGone(t, carA.Head.Ref, carB.Head.Ref, train.Head.Ref)
}

// TestGitHubTrain_ACarThatBreaksQueueCIIsEjectedAndTheRestLand is #2221's
// red run: of three cars the middle one adds BREAK-CI, which fails ci-gate
// on every queue head that holds it. Bisection ejects that car, naming the
// failed check, and the other two land.
func TestGitHubTrain_ACarThatBreaksQueueCIIsEjectedAndTheRestLand(t *testing.T) {
	env, err := githubitest.LoadEnv()
	if err != nil {
		t.Fatal(err)
	}
	rig := startGitHubTrain(t, env)

	carB := rig.openFixtureCar(t, "car-b", githubitest.BreakCIFile)
	carC := rig.openFixtureCar(t, "car-c", "car-c.txt")
	carA := rig.openDaemonCar(t, carB.Head.Ref, carC.Head.Ref)
	rig.approveAll(t, carA, carB, carC)
	settled := rig.awaitSettled(t, carA, carB, carC)
	rig.trainPullRequests(t)

	ejected := settled[carB.Number]
	require.Equal(t, sdlc.TrainEntryEjected, ejected.State, "the car that breaks ci-gate is ejected")
	require.NotNil(t, ejected.Ejection)
	require.Equal(t, sdlc.TrainEjectChecksFailed, ejected.Ejection.Reason)
	require.Contains(t, ejected.Ejection.Detail+ejected.Ejection.Findings, githubitest.QueueCICheck,
		"the ejection names the failed check")

	for _, car := range []githubitest.PullRequest{carA, carC} {
		entry := settled[car.Number]
		require.Equal(t, sdlc.TrainEntryLanded, entry.State, "car #%d lands", car.Number)
		require.NotEmpty(t, entry.MergeSHA)
	}
	rig.requireOnBase(t, map[string]bool{"WORK.md": true, "car-c.txt": true, githubitest.BreakCIFile: false})
	rig.awaitBranchesGone(t, carA.Head.Ref, carC.Head.Ref)
}

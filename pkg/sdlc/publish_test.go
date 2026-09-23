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
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The publisher
// =============================================================================
//
// The publisher is the only git in this module, and it runs entirely inside
// the worker's container. These tests drive the REAL command builder against
// a scripted container, so what they measure is the argument vectors the
// daemon actually emits — which is where a credential leak or an unsigned
// commit would live.

// newTestPublisher wires the real publisher to a scripted container.
func newTestPublisher(container *fakeContainer, signing *SigningSession) *publisher {
	project := testOrchProject()
	return &publisher{
		exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			return container.exec("container-1", spec)
		},
		project: project,
		signing: signing,
		taskID:  testTask,
	}
}

// testPublishPlan is the branch under review.
func testPublishPlan() publishPlan {
	return publishPlan{
		WorkDir: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		Branch:  "issue/25-fix-the-thing",
		Base:    "main",
		Message: "Fix the thing\n\nCloses #25",
	}
}

// TestPublisher_CommitsSignedWithTheProjectIdentity is the property the whole
// phase exists for: the commit is made INSIDE the container, authored as the
// project's agent identity and signed against the socket the spawn mounted —
// and the signing decision is STATED, never inherited from whatever the
// worker image's global git config happens to say.
func TestPublisher_CommitsSignedWithTheProjectIdentity(t *testing.T) {
	container := &fakeContainer{changed: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	head, err := pub.Publish(testPublishPlan())
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if head != "head-1" {
		t.Fatalf("head = %q, want the commit the container made", head)
	}
	if len(container.commits) != 1 || !strings.HasPrefix(container.commits[0], "Fix the thing") {
		t.Fatalf("commits = %+v", container.commits)
	}
	config := strings.Join(container.commitConfig, " ")
	for _, want := range []string{
		"user.name=" + testOrchProject().Git.Author.Name,
		"user.email=" + testOrchProject().Git.Author.Email,
		"gpg.format=ssh",
		"user.signingkey=key::" + testSigningKey,
		"commit.gpgsign=true",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("commit config %q is missing %q", config, want)
		}
	}
	if strings.Contains(config, "commit.gpgsign=false") {
		t.Error("the commit was explicitly unsigned while a signing agent was running")
	}
}

// TestPublisher_WithoutASigningAgentTheCommitIsExplicitlyUnsigned: a project
// that signs nothing still says so, because a worker image whose global git
// config sets commit.gpgsign would otherwise decide for the project — and a
// commit carrying a signature the project did not choose is a provenance
// claim that is wrong and permanent.
func TestPublisher_WithoutASigningAgentTheCommitIsExplicitlyUnsigned(t *testing.T) {
	container := &fakeContainer{changed: true}
	pub := newTestPublisher(container, nil)

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	config := strings.Join(container.commitConfig, " ")
	if !strings.Contains(config, "commit.gpgsign=false") {
		t.Errorf("commit config %q leaves signing to the image", config)
	}
	if strings.Contains(config, "user.signingkey") {
		t.Errorf("an unsigned commit named a signing key: %q", config)
	}
}

// TestPublisher_EveryCommandRunsInTheContainersCheckout: the directory named
// is the CONTAINER's, not the host path the daemon bind-mounted it from. A
// command carrying the host path would be a command only the daemon could
// run.
func TestPublisher_EveryCommandRunsInTheContainersCheckout(t *testing.T) {
	container := &fakeContainer{changed: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	plan := testPublishPlan()

	if _, err := pub.Publish(plan); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if dirs := container.dirsUsed(); len(dirs) != 1 || dirs[0] != plan.WorkDir {
		t.Fatalf("directories used = %v, want only the container's checkout %q", dirs, plan.WorkDir)
	}
	for _, cmd := range container.commandsRun() {
		if !strings.HasPrefix(cmd, "git ") {
			t.Errorf("the publisher ran something other than git: %q", cmd)
		}
	}
}

// TestPublisher_TheForgeTokenTravelsAsAPathNeverAValue: the push authenticates
// with the token the spawn mounted read-only, and the credential helper reads
// it from that FILE when git asks. A helper carrying the value would put a
// tenant's forge token in an argument vector every local process can read.
func TestPublisher_TheForgeTokenTravelsAsAPathNeverAValue(t *testing.T) {
	container := &fakeContainer{changed: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	pub.project.Git.RepoURL = "https://forge.test/acme/api.git"

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	var pushCmd string
	for _, cmd := range container.commandsRun() {
		if strings.Contains(cmd, " push ") {
			pushCmd = cmd
		}
	}
	if pushCmd == "" {
		t.Fatal("nothing was pushed")
	}
	tokenFile := path.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceForgeTokenFile)
	if !strings.Contains(pushCmd, tokenFile) {
		t.Errorf("the push does not read the mounted token file: %q", pushCmd)
	}
	if !strings.Contains(pushCmd, "credential.helper= ") {
		t.Errorf("the image's own credential helpers were not cleared: %q", pushCmd)
	}
	if strings.Contains(pushCmd, "a-forge-api-token") {
		t.Fatal("the forge token VALUE reached the argument vector")
	}
}

// TestPublisher_AnSSHRemoteCarriesNoCredentialHelper: an ssh:// remote
// authenticates through the agent socket the container already has, so a
// token helper on that push would be configuration for a credential that
// does not exist.
func TestPublisher_AnSSHRemoteCarriesNoCredentialHelper(t *testing.T) {
	container := &fakeContainer{changed: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	pub.project.Git.RepoURL = "ssh://git@forge.test/acme/api.git"

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for _, cmd := range container.commandsRun() {
		if strings.Contains(cmd, "credential.helper") {
			t.Errorf("an ssh remote was given a token credential helper: %q", cmd)
		}
	}
}

// TestPublisher_TheRefspecIsTheStrategysDecision: a reviewed change publishes
// its own branch; the direct-push carve-out publishes that branch ONTO the
// target, which is the whole of what the carve-out changes about the push.
func TestPublisher_TheRefspecIsTheStrategysDecision(t *testing.T) {
	for name, tc := range map[string]struct {
		remoteBranch string
		wantRemote   string
	}{
		"reviewed":  {remoteBranch: "", wantRemote: ""},
		"carve-out": {remoteBranch: "main", wantRemote: "main"},
	} {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{changed: true}
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
			plan := testPublishPlan()
			plan.RemoteBranch = tc.remoteBranch

			if _, err := pub.Publish(plan); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if len(container.pushes) != 1 {
				t.Fatalf("pushes = %+v, want one", container.pushes)
			}
			got := container.pushes[0]
			if got.Branch != plan.Branch || got.RemoteBranch != tc.wantRemote {
				t.Fatalf("pushed %q → %q, want %q → %q", got.Branch, got.RemoteBranch, plan.Branch, tc.wantRemote)
			}
		})
	}
}

// TestPublisher_AheadWithoutChangesStillPublishes: a resumed attempt whose
// worker committed before the interruption has nothing uncommitted and
// everything to publish. Refusing it would lose the work the container
// already did.
func TestPublisher_AheadWithoutChangesStillPublishes(t *testing.T) {
	container := &fakeContainer{changed: false, ahead: true, head: "head-prior"}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	head, err := pub.Publish(testPublishPlan())
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if head != "head-prior" {
		t.Fatalf("head = %q, want the commit already in the checkout", head)
	}
	if len(container.commits) != 0 {
		t.Fatalf("an empty commit was made: %+v", container.commits)
	}
}

// TestPublisher_AChangeThatStagesNothingIsNotCommitted is go-quicbaas#1048:
// status reported a change, "add --all" then staged nothing (a file rewritten
// to its committed content in between), and "git commit" exited 1 on "nothing
// to commit" — which refused an attempt whose worker had already committed the
// real work. The publisher asks the index, skips the empty commit, and
// publishes the commits the branch already carries.
func TestPublisher_AChangeThatStagesNothingIsNotCommitted(t *testing.T) {
	container := &fakeContainer{changed: true, nothingStaged: true, ahead: true, head: "head-worker"}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	head, err := pub.Publish(testPublishPlan())
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(container.commits) != 0 {
		t.Fatalf("an empty commit was attempted: %+v", container.commits)
	}
	if head != "head-worker" {
		t.Fatalf("head = %q, want the worker's own commit", head)
	}
	if len(container.pushes) != 1 {
		t.Fatalf("pushes = %d, want the branch published once", len(container.pushes))
	}
}

// TestPublisher_AChangeThatStagesNothingAndIsNotAheadIsNoChanges: the same
// race on a branch with no commits of its own is an attempt that produced
// nothing — the ordinary NoChangesError, not a failed commit.
func TestPublisher_AChangeThatStagesNothingAndIsNotAheadIsNoChanges(t *testing.T) {
	container := &fakeContainer{changed: true, nothingStaged: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	_, err := pub.Publish(testPublishPlan())
	var none *NoChangesError
	if !errors.As(err, &none) {
		t.Fatalf("err = %v, want NoChangesError", err)
	}
	if len(container.commits) != 0 || len(container.pushes) != 0 {
		t.Fatalf("commits %+v pushes %+v, want neither", container.commits, container.pushes)
	}
}

// TestPublisher_AnUnsignedWorkerCommitIsNotPublished is the review finding on
// go-quicbaas#1049: a branch whose WORKER committed with signing switched off
// (or with another key) must not reach the forge. The check runs against the
// allowed_signers file the daemon wrote, and nothing is pushed.
func TestPublisher_AnUnsignedWorkerCommitIsNotPublished(t *testing.T) {
	for _, status := range []string{"N", "B", "U", "E"} {
		t.Run(status, func(t *testing.T) {
			container := &fakeContainer{ahead: true, head: "head-worker",
				outgoing: []outgoingCommit{{SHA: "good-1", Status: "G", Signer: testSigningFingerprint}, {SHA: "bad-2", Status: status}}}
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

			_, err := pub.Publish(testPublishPlan())
			var unsigned *UnsignedCommitError
			if !errors.As(err, &unsigned) || !errors.Is(err, ErrUnsignedCommit) {
				t.Fatalf("err = %v, want UnsignedCommitError", err)
			}
			if unsigned.Commit != "bad-2" || unsigned.Status != status {
				t.Fatalf("refused %s (%s), want bad-2 (%s)", unsigned.Commit, unsigned.Status, status)
			}
			if len(container.pushes) != 0 {
				t.Fatalf("pushes = %+v, want none", container.pushes)
			}
			if !slices.Contains(container.logConfig, "gpg.ssh.allowedSignersFile="+allowedSignersPath) {
				t.Fatalf("log config = %v, want the allowed_signers file named", container.logConfig)
			}
		})
	}
}

// TestPublisher_AGoodSignatureByAnotherKeyIsNotPublished is the second review
// finding on go-quicbaas#1049: git picks verification from the signature's own
// format, so a trusted OpenPGP signature (or an SSH one by some other key)
// reports "G" without the allowed_signers file deciding anything. Only the
// attempt's own SSH key fingerprint passes.
func TestPublisher_AGoodSignatureByAnotherKeyIsNotPublished(t *testing.T) {
	for name, signer := range map[string]string{
		"openpgp":       "0123456789ABCDEF0123456789ABCDEF01234567",
		"other ssh key": "SHA256:not-the-attempts-key",
		"none reported": "",
	} {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{ahead: true, head: "head-worker",
				outgoing: []outgoingCommit{{SHA: "gpg-1", Status: "G", Signer: signer}}}
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

			_, err := pub.Publish(testPublishPlan())
			var unsigned *UnsignedCommitError
			if !errors.As(err, &unsigned) || unsigned.Commit != "gpg-1" || unsigned.Signer != signer {
				t.Fatalf("err = %v, want UnsignedCommitError naming gpg-1 and signer %q", err, signer)
			}
			if len(container.pushes) != 0 {
				t.Fatalf("pushes = %+v, want none", container.pushes)
			}
		})
	}
}

// TestPublisher_AnUnparseableSigningKeyIsRefused: a signing session whose
// public half does not parse cannot vouch for anything, and nothing is pushed.
func TestPublisher_AnUnparseableSigningKeyIsRefused(t *testing.T) {
	container := &fakeContainer{ahead: true, head: "head-worker"}
	pub := newTestPublisher(container, &SigningSession{PublicKey: "not a key"})

	_, err := pub.Push(testPublishPlan())
	var sessionErr *SigningSessionError
	if !errors.As(err, &sessionErr) {
		t.Fatalf("err = %v, want SigningSessionError", err)
	}
	if len(container.pushes) != 0 {
		t.Fatalf("pushes = %+v, want none", container.pushes)
	}
}

// TestPublisher_TheCheckReadsTheRefBeingPushedNotHEAD is the round-3 review
// finding on go-quicbaas#1049: a worker that leaves an unsigned commit on the
// delivery branch and checks out a signed ancestor must not get the branch
// verified by way of HEAD. The range ends at refs/heads/<branch>, the exact
// source of the push, on both the publish and the teardown path, and the head
// reported is that ref's.
func TestPublisher_TheCheckReadsTheRefBeingPushedNotHEAD(t *testing.T) {
	plan := testPublishPlan()
	want := "origin/" + plan.Base + "..refs/heads/" + plan.Branch
	for name, push := range map[string]func(*publisher) error{
		"publish": func(p *publisher) error { _, err := p.Publish(plan); return err },
		"push":    func(p *publisher) error { _, err := p.Push(plan); return err },
	} {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{ahead: true, head: "head-worker"}
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
			if err := push(pub); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if container.logRange != want {
				t.Fatalf("verified %q, want %q", container.logRange, want)
			}
			sawBranchHead := false
			for _, cmd := range container.commandsRun() {
				if strings.HasSuffix(cmd, "rev-parse refs/heads/"+plan.Branch) {
					sawBranchHead = true
				}
				if strings.HasSuffix(cmd, "rev-parse HEAD") {
					t.Fatalf("the head was read from HEAD, not the pushed branch: %q", cmd)
				}
			}
			if !sawBranchHead {
				t.Fatal("the pushed branch's head was never read")
			}
		})
	}
}

// TestPublisher_PushItselfVerifies: a caller that goes straight to Push (the
// teardown publish of a failed attempt does) is held to the same check.
func TestPublisher_PushItselfVerifies(t *testing.T) {
	container := &fakeContainer{ahead: true, head: "head-worker",
		outgoing: []outgoingCommit{{SHA: "bad-1", Status: "N"}}}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	if _, err := pub.Push(testPublishPlan()); !errors.Is(err, ErrUnsignedCommit) {
		t.Fatalf("err = %v, want ErrUnsignedCommit", err)
	}
	if len(container.pushes) != 0 {
		t.Fatalf("pushes = %+v, want none", container.pushes)
	}
}

// TestPublisher_SignedWorkerCommitsArePublished: every outgoing commit signed
// by the attempt's key passes, including the worker's own.
func TestPublisher_SignedWorkerCommitsArePublished(t *testing.T) {
	container := &fakeContainer{changed: true, ahead: true, head: "head-worker",
		outgoing: []outgoingCommit{{SHA: "worker-1", Status: "G", Signer: testSigningFingerprint}}}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(container.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(container.pushes))
	}
}

// TestPublisher_AProjectThatSignsNothingIsNotVerified: with no signing
// session there is no key to verify against, and no log is run.
func TestPublisher_AProjectThatSignsNothingIsNotVerified(t *testing.T) {
	container := &fakeContainer{ahead: true, head: "head-worker",
		outgoing: []outgoingCommit{{SHA: "worker-1", Status: "N"}}}
	pub := newTestPublisher(container, nil)

	if _, err := pub.Publish(testPublishPlan()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for _, cmd := range container.commandsRun() {
		if strings.Contains(cmd, " log ") {
			t.Fatalf("ran %q for a project that signs nothing", cmd)
		}
	}
}

// TestPublisher_ASignatureCheckGitCannotAnswerIsRefused: git failing to list
// the outgoing commits is not "all signed".
func TestPublisher_ASignatureCheckGitCannotAnswerIsRefused(t *testing.T) {
	container := &fakeContainer{ahead: true, logErr: errors.New("fatal: bad revision")}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	_, err := pub.Publish(testPublishPlan())
	var cmdErr *PublishCommandError
	if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 128 {
		t.Fatalf("err = %v, want PublishCommandError exit 128", err)
	}
	if len(container.pushes) != 0 {
		t.Fatalf("pushes = %+v, want none", container.pushes)
	}
}

// TestPublisher_AnIndexProbeGitCannotAnswerIsRefused is the second review
// finding on go-quicbaas#1049: "diff --cached --quiet" exiting other than 0 or
// 1 is git failing, and must stop the publication before any commit or push,
// even on a branch that is already ahead.
func TestPublisher_AnIndexProbeGitCannotAnswerIsRefused(t *testing.T) {
	for _, ahead := range []bool{false, true} {
		container := &fakeContainer{changed: true, ahead: ahead, diffExit: 128}
		pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

		_, err := pub.Publish(testPublishPlan())
		var cmdErr *PublishCommandError
		if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 128 || cmdErr.Command != "diff --cached --quiet" {
			t.Fatalf("ahead=%v: err = %v, want PublishCommandError for the index probe", ahead, err)
		}
		if len(container.commits) != 0 || len(container.pushes) != 0 {
			t.Fatalf("ahead=%v: commits %+v pushes %+v, want neither", ahead, container.commits, container.pushes)
		}
	}
}

// TestPublisher_AnIndexProbeThatCannotRunIsRefused: the exec itself failing
// is returned as it is, and nothing is committed or pushed.
func TestPublisher_AnIndexProbeThatCannotRunIsRefused(t *testing.T) {
	for _, ahead := range []bool{false, true} {
		execErr := errors.New("container gone")
		container := &fakeContainer{changed: true, ahead: ahead, diffExecErr: execErr}
		pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

		if _, err := pub.Publish(testPublishPlan()); !errors.Is(err, execErr) {
			t.Fatalf("ahead=%v: err = %v, want the exec failure", ahead, err)
		}
		if len(container.commits) != 0 || len(container.pushes) != 0 {
			t.Fatalf("ahead=%v: commits %+v pushes %+v, want neither", ahead, container.commits, container.pushes)
		}
	}
}

// TestPublisher_NothingToPublishIsRefused: an attempt that changed nothing
// and is not ahead has produced no delivery, and opening a pull request for
// it would point at a branch identical to its base.
func TestPublisher_NothingToPublishIsRefused(t *testing.T) {
	container := &fakeContainer{}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

	_, err := pub.Publish(testPublishPlan())
	var none *NoChangesError
	if !errors.As(err, &none) || !errors.Is(err, ErrNoChanges) {
		t.Fatalf("err = %v, want NoChangesError", err)
	}
	if len(container.pushes) != 0 {
		t.Fatalf("an empty attempt was pushed: %+v", container.pushes)
	}
}

// TestPublisher_ARefusedCommandIsTypedAndScrubsItsOutput: a git command that
// exits non-zero fails the publish, its output stays on the field verbatim,
// and the error string says what happened with every known credential shape
// redacted. An error string reaches every log and, through this module's
// surfacing path, a comment on a forge issue — where a push that echoed a
// token would publish it (go-quicbaas#751 keeps the rule and drops the
// blindness).
func TestPublisher_ARefusedCommandIsTypedAndScrubsItsOutput(t *testing.T) {
	const leak = "remote: rejected; token ghp_SUPERSECRETVALUE was denied"
	for name, arrange := range map[string]func(c *fakeContainer){
		"the commit is refused": func(c *fakeContainer) { c.changed = true; c.commitErr = errors.New(leak) },
		"the push is refused":   func(c *fakeContainer) { c.changed = true; c.pushErr = errors.New(leak) },
		"the status is refused": func(c *fakeContainer) { c.statusErr = errors.New(leak) },
	} {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{}
			arrange(container)
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})

			_, err := pub.Publish(testPublishPlan())
			var refused *PublishCommandError
			if !errors.As(err, &refused) || !errors.Is(err, ErrRunner) {
				t.Fatalf("err = %v, want PublishCommandError", err)
			}
			if refused.Output != leak {
				t.Errorf("the output was not kept for a caller that can ask: %q", refused.Output)
			}
			if strings.Contains(err.Error(), "ghp_SUPERSECRETVALUE") {
				t.Fatalf("the command's output was rendered into the error string: %q", err.Error())
			}
			if !strings.Contains(err.Error(), redactedMarker) {
				t.Errorf("the token was dropped rather than redacted: %q", err.Error())
			}
			if !strings.Contains(err.Error(), "remote: rejected") {
				t.Errorf("the rejection went with the token: %q", err.Error())
			}
		})
	}
}

// TestPublisher_AnUnreachableContainerIsNotAnEmptyAttempt: an exec that could
// not run at all is a failure, not a "no". Answering false for the
// ahead-check would silently discard an attempt whose commits are real.
func TestPublisher_AnUnreachableContainerIsNotAnEmptyAttempt(t *testing.T) {
	gone := &orchestration.SessionNotContainerizedError{SessionID: "session-1"}
	pub := &publisher{
		exec:    func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) { return nil, gone },
		project: testOrchProject(),
		taskID:  testTask,
	}
	if _, err := pub.Publish(testPublishPlan()); !errors.Is(err, orchestration.ErrSessionNotContainerized) {
		t.Fatalf("err = %v, want the session refusal to surface", err)
	}
}

// TestPublisher_AnUnresolvableBaseIsRefusedRatherThanReadAsNotAhead: git
// exiting non-zero on the rev-list is not "zero commits". Reading it that way
// would let a real delivery be discarded as empty.
func TestPublisher_AnUnresolvableBaseIsRefusedRatherThanReadAsNotAhead(t *testing.T) {
	container := &fakeContainer{}
	pub := &publisher{
		exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			if len(spec.Cmd) > 1 && spec.Cmd[1] == "rev-list" {
				return &orchestration.ExecResult{ExitCode: 128, Stderr: "unknown revision"}, nil
			}
			return container.exec("container-1", spec)
		},
		project: testOrchProject(),
		taskID:  testTask,
	}
	_, err := pub.AheadOfBase(testPublishPlan())
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want PublishCommandError", err)
	}
}

// TestPublisher_AnUncountableAnswerIsRefused: git answering something that is
// not a number means the probe did not measure what it claims to.
func TestPublisher_AnUncountableAnswerIsRefused(t *testing.T) {
	pub := &publisher{
		exec: func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			return &orchestration.ExecResult{Stdout: "many\n"}, nil
		},
		project: testOrchProject(),
		taskID:  testTask,
	}
	if _, err := pub.AheadOfBase(testPublishPlan()); !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// TestCredentialHelperArgs_OnlyHTTPSCarriesOne pins the one scheme that
// authenticates with a bearer token.
func TestCredentialHelperArgs_OnlyHTTPSCarriesOne(t *testing.T) {
	for url, want := range map[string]bool{
		"https://forge.test/acme/api.git":   true,
		"ssh://git@forge.test/acme/api.git": false,
		"git@forge.test:acme/api.git":       false,
		"file:///srv/repos/api.git":         false,
	} {
		got := len(credentialHelperArgs(url)) > 0
		if got != want {
			t.Errorf("credentialHelperArgs(%q) present = %v, want %v", url, got, want)
		}
	}
}

// TestPublisher_ForceIsLeasedOrAbsent is the mid-attempt publication's own
// property. A plain push fails the job whenever the worker amended or rebased
// its own commits between turns, which is normal work — so the post-turn push
// asks for a force. It must be a LEASED force: the lease refuses the push when
// the remote moved since this checkout last saw it, so the one thing a force
// must never do, discard a commit this container never observed, is refused by
// git rather than trusted to the caller.
func TestPublisher_ForceIsLeasedOrAbsent(t *testing.T) {
	for name, tc := range map[string]struct {
		force     bool
		wantForce bool
	}{
		"AFinalPublicationDoesNotForce": {force: false, wantForce: false},
		"APostTurnPublicationForces":    {force: true, wantForce: true},
	} {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{changed: true}
			pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
			plan := testPublishPlan()
			plan.Force = tc.force

			if _, err := pub.Publish(plan); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if len(container.pushes) != 1 {
				t.Fatalf("pushes = %+v, want one", container.pushes)
			}
			if container.pushes[0].Force != tc.wantForce {
				t.Fatalf("push force = %v, want %v", container.pushes[0].Force, tc.wantForce)
			}
		})
	}
}

// TestPublisher_ForceStillRefusesWhatGitRefuses: a leased force that the remote
// rejects is a failure, not a success that silently discarded work. The whole
// value of --force-with-lease is that this path exists.
func TestPublisher_ForceStillRefusesWhatGitRefuses(t *testing.T) {
	container := &fakeContainer{
		changed: true,
		pushErr: errors.New("stale info: remote ref is at a commit this checkout never saw"),
	}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	plan := testPublishPlan()
	plan.Force = true

	_, err := pub.Publish(plan)
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("Publish() = %T %v, want a PublishCommandError", err, err)
	}
	if !strings.Contains(refused.Command, "--force-with-lease") {
		t.Errorf("the refusal names the command %q, want the leased force in it", refused.Command)
	}
}

// TestForceWithLease_PinsTheLeaseToThePublishedRef covers the lease flag. It
// is pinned to the ref actually being published when the plan names a lease,
// and it is git's own tracking-ref lease when the plan does not. It is never a
// bare force.
func TestForceWithLease_PinsTheLeaseToThePublishedRef(t *testing.T) {
	cases := []struct {
		name string
		plan publishPlan
		want string
	}{
		{"no recorded head leases on the tracking ref", publishPlan{Branch: "issue/7-x"}, "--force-with-lease"},
		{"a recorded head pins the lease", publishPlan{Branch: "issue/7-x", Lease: "abc123"},
			"--force-with-lease=issue/7-x:abc123"},
		{"the lease names the remote ref it guards", publishPlan{Branch: "local", RemoteBranch: "issue/7-x", Lease: "abc123"},
			"--force-with-lease=issue/7-x:abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := forceWithLease(tc.plan)
			if got != tc.want {
				t.Fatalf("forceWithLease = %q, want %q", got, tc.want)
			}
			if got == "--force" || got == "-f" {
				t.Fatal("an unleased force may discard a commit this checkout never saw")
			}
		})
	}
}

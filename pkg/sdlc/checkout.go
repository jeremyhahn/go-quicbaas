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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The checkout — the container clones, the runner drives it
// =============================================================================
//
// THE DAEMON MAKES NO CHECKOUT, AND NO IMAGE ENTRYPOINT MAKES ONE EITHER.
// The daemon creates the empty directory the container's /workspace is
// bind-mounted through and hands the container a workspace spec; the clone
// itself is a sequence of git commands the runner execs INSIDE the container,
// through the same seam the publish steps use. Every path named below is a
// path the container knows, and nothing here touches the daemon's filesystem.
//
// IT IS DRIVEN, NOT DELEGATED, because the spawn path clears the image's
// entrypoint (acp_backend.go, spawnContainerJSONRPC) and runs the agent binary
// as the container's command. An image ENTRYPOINT that cloned would never run,
// so the charter's "Your branch is already checked out" would be a promise
// nothing kept. Driving it here also means the clone's failure is the JOB's
// failure, with the git output on the job as evidence, rather than a container
// that came up with an empty directory and a worker that had to discover that
// for itself.
//
// WHAT THE COMMANDS CARRY is what publish.go's already carry: configuration in
// argv, credentials never. The forge token is read out of the mounted file by
// the credential helper at the moment git asks for it — the same helper shape,
// from the same builder — so the value reaches no argument vector, no
// environment and no log line.
//
// THE SPEC IS THE ONE CONTRACT. What this drives is the WorkspaceSpec the
// runner materialized into the spec directory a moment earlier: the same
// bytes the container reads at $QUICBAAS_WORKSPACE_SPEC
// (/run/quicbaas/workspace/workspace.json). There is one description of the
// checkout, and both sides act on it.

// gitDirProbe asks git whether a directory is already a repository. Its EXIT
// STATUS is the answer, so an absent directory is a "no" rather than a
// failure — which is what makes the whole step idempotent for a resumed
// attempt whose mount already holds the clone a dead node made.
var gitDirProbe = []string{"rev-parse", "--git-dir"}

// maxCheckoutStepOutput caps how much of one command's output is kept as
// evidence. A clone that fails on a large repository can print a great deal;
// the job's audit trail is a record, not a transcript.
const maxCheckoutStepOutput = 4096

// auditCheckout is the audit event the checkout's commands are recorded under.
const auditCheckout = "sdlc:workspace_checkout"

// checkoutStep is one command the checkout ran and what it said.
type checkoutStep struct {
	// Command is the argument vector, joined. It is configuration only.
	Command string

	// Output is the command's combined output, capped.
	Output string
}

// gitSteps runs git inside a worker's container and keeps every command it
// ran, with what the command said, as the job's evidence.
//
// IT IS SHARED BY THE CLONER AND THE PRE-FLIGHT because they are the same
// seam used twice: the same exec, the same argument-vector-only discipline,
// the same capped evidence, and the same "a non-zero exit is an answer, not a
// failure" distinction between probe and run. A second copy of it would be a
// second place for the evidence to be dropped from.
type gitSteps struct {
	// exec runs one command in the container, returning what it did.
	exec func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error)

	// taskID names the attempt in every error.
	taskID string

	// steps is every command run, in order, with its output.
	steps []checkoutStep
}

// cloner makes the attempt's checkout inside the worker's container.
type cloner struct {
	gitSteps

	// project supplies the commit identity written into the checkout.
	project *orchestration.Project

	// spec is the workspace spec the daemon materialized for this spawn.
	spec orchestration.WorkspaceSpec
}

// Checkout clones the project into <checkout_root>/<project name>, clones each
// context repository beside it, puts the planned branch on the project
// checkout, and writes the project's commit identity into it.
//
// It is IDEMPOTENT. A resumed attempt is bind-mounted through the directory
// its predecessor's container cloned into, so every clone is skipped when the
// directory is already a repository and the branch is re-fetched and reset
// onto its remote either way.
func (c *cloner) Checkout() error {
	if err := c.validate(); err != nil {
		return err
	}
	root := c.spec.CheckoutRoot
	projectDir := path.Join(root, c.spec.Project.Name)
	if err := c.clone(root, c.spec.Project); err != nil {
		return err
	}
	if err := c.identify(projectDir); err != nil {
		return err
	}
	if err := c.branch(projectDir); err != nil {
		return err
	}
	for _, repo := range c.spec.ContextRepos {
		if err := c.clone(root, repo); err != nil {
			return err
		}
		if _, err := c.run(path.Join(root, repo.Name), "checkout", repo.Ref); err != nil {
			return err
		}
	}
	return nil
}

// Evidence renders every command the checkout ran and what it said, for the
// job's audit trail. It is available whether the checkout succeeded or not:
// the commands that ran before a failure are the whole point of keeping them.
func (c *gitSteps) Evidence() string {
	var b strings.Builder
	for _, step := range c.steps {
		b.WriteString("$ " + step.Command + "\n")
		if step.Output != "" {
			b.WriteString(step.Output + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// validate refuses a spec that names no checkout this could make. The spec
// comes from NewWorkspaceSpec, which refuses the same things; checking again
// here is what keeps a hand-built spec from turning into a git command with an
// empty operand.
func (c *cloner) validate() error {
	switch {
	case strings.TrimSpace(c.spec.CheckoutRoot) == "":
		return &InvalidInputError{Field: "workspace.checkout_root", Message: "the container clones into a named directory"}
	case strings.TrimSpace(c.spec.Project.Name) == "":
		return &InvalidInputError{Field: "workspace.project.name", Message: "names the directory the project is cloned into"}
	case strings.TrimSpace(c.spec.Project.RepoURL) == "":
		return &InvalidInputError{Field: "workspace.project.repo_url", Message: "names the remote the container clones"}
	case strings.TrimSpace(c.spec.Branch.Name) == "":
		return &InvalidInputError{Field: "workspace.branch.name", Message: "the container checks out a named branch"}
	case c.spec.Branch.Create && strings.TrimSpace(c.spec.Branch.Base) == "":
		return &InvalidInputError{Field: "workspace.branch.base", Message: "a new branch needs the ref it starts from"}
	}
	return nil
}

// clone puts one repository in its own directory under parent, unless one is
// already there.
//
// The command runs in PARENT and names the directory relatively, so the only
// directory it has to be able to enter is the checkout root — which is the
// container's own working directory and the bind mount the daemon created.
func (c *cloner) clone(parent string, repo orchestration.WorkspaceRepoSpec) error {
	present, err := c.isRepository(parent, repo.Name)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	args := credentialHelperArgs(repo.RepoURL)
	args = append(args, "clone", "--origin", gitRemote, repo.RepoURL, repo.Name)
	_, err = c.run(parent, args...)
	return err
}

// isRepository reports whether name under parent is already a git repository.
func (c *cloner) isRepository(parent, name string) (bool, error) {
	args := append([]string{"-C", name}, gitDirProbe...)
	result, err := c.probe(parent, args...)
	if err != nil {
		return false, err
	}
	return result.Succeeded(), nil
}

// identify writes the project's commit author, and its signing configuration
// when the attempt signs, into the checkout, so a git the WORKER runs commits
// under the same identity and key the publisher does.
//
// The publisher states the identity on its own invocations as well
// (gitConfigArgs) and does not depend on this: what is written here is for
// everything else that runs in the checkout.
func (c *cloner) identify(dir string) error {
	if c.project == nil {
		return &InvalidInputError{Field: "project", Message: "the checkout is stamped with the project's commit identity"}
	}
	identity := []gitConfigField{
		{"user.name", c.project.Git.Author.Name},
		{"user.email", c.project.Git.Author.Email},
	}
	if c.spec.SigningKey != "" {
		// A worker that commits on its own signs with the attempt's key, and
		// the publisher refuses any outgoing commit that is not signed by it.
		identity = append(identity, signingConfig(c.spec.SigningKey)...)
	}
	for _, field := range identity {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		if _, err := c.run(dir, "config", field.Key, field.Value); err != nil {
			return err
		}
	}
	return nil
}

// branch puts the planned branch on the project checkout.
//
// A NEW branch is cut from its base; a branch a PRIOR ATTEMPT pushed is
// fetched and continued, which is what keeps one pull request and its reviews
// across a resume. The base is fetched either way, because it is the ref the
// publisher asks "is there anything to publish" against.
//
// A PINNED PLAN STARTS FROM THE COMMIT, NOT FROM THE REMOTE BRANCH. The
// branch is still fetched — that is how the object arrives — but the reset
// names the commit the caller captured, so an author who pushed in the
// meantime moves the branch and does not move this checkout. If the push was
// a force-push that dropped the captured commit, the reset finds no such
// object and the whole checkout FAILS, which is the correct answer: the tree
// the caller means to act on no longer exists, and acting on the one that
// replaced it would be acting on something nothing read.
func (c *cloner) branch(dir string) error {
	plan := c.spec.Branch
	start := gitRemote + "/" + plan.Base
	refs := make([]string, 0, 2)
	if plan.Base != "" {
		refs = append(refs, plan.Base)
	}
	if !plan.Create {
		refs = append(refs, plan.Name)
		start = gitRemote + "/" + plan.Name
	}
	if len(refs) > 0 {
		args := credentialHelperArgs(c.spec.Project.RepoURL)
		args = append(args, "fetch", gitRemote)
		args = append(args, refs...)
		if _, err := c.run(dir, args...); err != nil {
			return err
		}
	}
	if pinned := strings.TrimSpace(plan.Commit); pinned != "" {
		start = pinned
	}
	if _, err := c.run(dir, "checkout", "-B", plan.Name, start); err != nil {
		return err
	}
	return c.verifyPinnedHead(dir)
}

// verifyPinnedHead reads the checkout's HEAD back and refuses one that is not
// the commit the plan pinned.
//
// THE RESET IS NOT THE PROOF. "checkout -B" reports success for reasons that
// have nothing to do with which commit HEAD ended on — a ref that resolved to
// something else, a hook, a repository that was not the one the caller meant
// — and the whole value of pinning is that the caller may then state, as a
// fact, which tree was read. So the fact is READ BACK rather than assumed,
// and a checkout that cannot prove it is on the pinned commit is a failure
// rather than a review of an unknown tree.
//
// An unpinned plan verifies nothing: there is no commit it was promised to be
// on, and HEAD is wherever the remote branch was, which is exactly right.
func (c *cloner) verifyPinnedHead(dir string) error {
	want := strings.TrimSpace(c.spec.Branch.Commit)
	if want == "" {
		return nil
	}
	result, err := c.run(dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	got := strings.TrimSpace(result.Stdout)
	if !strings.EqualFold(got, want) {
		return &CheckoutHeadMismatchError{TaskID: c.taskID, Want: want, Got: got}
	}
	return nil
}

// run executes one git command in the container and refuses a non-zero exit.
func (c *gitSteps) run(workDir string, args ...string) (*orchestration.ExecResult, error) {
	result, err := c.probe(workDir, args...)
	if err != nil {
		return nil, err
	}
	if !result.Succeeded() {
		return nil, &CheckoutCommandError{
			TaskID:   c.taskID,
			Command:  strings.Join(args, " "),
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
		}
	}
	return result, nil
}

// probe executes one git command whose EXIT STATUS is the answer, so a
// non-zero exit is a "no" rather than a failure. Every command run either way
// is recorded as evidence.
func (c *gitSteps) probe(workDir string, args ...string) (*orchestration.ExecResult, error) {
	result, err := c.exec(&orchestration.ExecSpec{
		Cmd:     append([]string{gitCommand}, args...),
		WorkDir: workDir,
	})
	if err != nil {
		return nil, err
	}
	c.record(args, result)
	return result, nil
}

// record keeps one command and what it said.
func (c *gitSteps) record(args []string, result *orchestration.ExecResult) {
	output := strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
	if len(output) > maxCheckoutStepOutput {
		output = output[:maxCheckoutStepOutput] + "\n… truncated"
	}
	c.steps = append(c.steps, checkoutStep{Command: gitCommand + " " + strings.Join(args, " "), Output: output})
}

// =============================================================================
// The one checkout every runner drives
// =============================================================================

// sessionExec is the one seam a runner runs a command in its session's
// container through. Every git command the module runs — the clone, the
// pre-flight's probes, the publish — goes through it, so there is no path by
// which one of them could run on the daemon instead.
func sessionExec(
	ctx context.Context,
	job *orchestration.JobRun,
	sessionID string,
) func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	return func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		return job.Exec(ctx, &orchestration.ExecSessionRequest{SessionID: sessionID, Spec: spec})
	}
}

// containerCheckout is one container's checkout: the clone that makes it and
// the pre-flight that proves it, with every command recorded on the job.
//
// THE DELIVERY RUNNER, THE REVIEWER AND TRIAGE ALL USE THIS ONE TYPE. They
// used to hold a copy each — the same cloner literal, the same audit call, the
// same evidence — and a copy is a place a runner comes to differ from its
// siblings without anybody reading either one noticing (go-quicbaas#929).
type containerCheckout struct {
	// job is the run the evidence is recorded on and the container is reached
	// through.
	job *orchestration.JobRun

	// sessionID names the container the commands run in.
	sessionID string

	// spec is the workspace spec the daemon materialized for this spawn: the
	// same bytes the container reads, so both sides act on one description.
	spec orchestration.WorkspaceSpec

	// project supplies the commit identity written into the checkout.
	project *orchestration.Project

	// taskID names the attempt in every error.
	taskID string
}

// Make clones the project and its context repositories inside the container
// and puts the planned branch on the project checkout.
//
// THE COMMANDS ARE THE EVIDENCE. Every one of them, with what it said, is
// recorded on the job whether the checkout succeeded or failed: a clone that
// could not reach the forge is the most common way an attempt dies, and the
// only place a person can see why is the job it died on.
func (c containerCheckout) Make(ctx context.Context) error {
	cl := &cloner{
		gitSteps: gitSteps{exec: sessionExec(ctx, c.job, c.sessionID), taskID: c.taskID},
		project:  c.project,
		spec:     c.spec,
	}
	err := cl.Checkout()
	if evidence := cl.Evidence(); evidence != "" {
		c.job.Audit(auditCheckout, evidence)
	}
	return err
}

// Verify proves the checkout is the one the spec described, before anything is
// prompted on it, and records every probe it ran.
//
// THE PROBES ARE THE EVIDENCE, whether it passed or failed. A pre-flight that
// refused and said only that leaves an operator to re-run the same commands by
// hand; the point of running them here is that nobody has to.
func (c containerCheckout) Verify(ctx context.Context) error {
	p := &preflight{
		gitSteps: gitSteps{exec: sessionExec(ctx, c.job, c.sessionID), taskID: c.taskID},
		spec:     c.spec,
	}
	err := p.Verify()
	if evidence := p.Evidence(); evidence != "" {
		c.job.Audit(auditPreflight, evidence)
	}
	return err
}

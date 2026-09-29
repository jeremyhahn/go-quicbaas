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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Building a train — git, in the train's container
// =============================================================================
//
// THE DAEMON RUNS NO GIT, here as everywhere. Every command below is an
// argument vector executed in the train's own container, in the checkout the
// shared seam made there (checkout.go), signing against the project's agent
// exactly as a worker's commit does (publish.go). The train is built the way a
// person would build it by hand: start from the target, merge each approved
// head on top in queue order, and push the result under a name nothing else
// uses.
//
// A MERGE, NEVER A REBASE OR A SQUASH. Each approved head stays an ancestor of
// the train, so the tree the pipeline tests is exactly "the target plus these
// changes" and nothing was rewritten to get there; how the train finally lands
// on the target is the project's strategy's decision, made at landing.

// auditTrainBuild is the audit event the build's commands are recorded under.
const auditTrainBuild = "sdlc:train_build"

// trainBuilder builds one train's branch in a container.
type trainBuilder interface {
	// Base answers the commit the checkout started from: the target's tip.
	Base() (string, error)

	// Add merges one approved head onto the train. It answers the train's new
	// head, or an ejection when the change cannot join the train — its branch
	// no longer carries the approved head, or it does not merge.
	Add(car TrainCar) (string, *TrainEjection, error)

	// Push publishes the train under its branch and answers the head pushed.
	Push(branch string) (string, error)

	// Evidence is every command run so far, for the job's audit trail.
	Evidence() string
}

// containerTrainBuilder is the trainBuilder that runs in the train's
// container.
type containerTrainBuilder struct {
	gitSteps

	// publisher pushes the train through the one path every publication
	// takes, which verifies every outgoing commit's signature first.
	publisher *publisher

	// project supplies the commit identity and the repository URL.
	project *orchestration.Project

	// signing is the agent the merge commits are signed against.
	signing *SigningSession

	// workDir is the checkout inside the container.
	workDir string

	// target is the branch the train is built on.
	target string
}

var _ trainBuilder = (*containerTrainBuilder)(nil)

// Base implements trainBuilder.
func (b *containerTrainBuilder) Base() (string, error) {
	result, err := b.run(b.workDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// Add implements trainBuilder.
//
// THE FETCH IS BY BRANCH AND THE HEAD IS CHECKED AFTER. A forge serves a
// branch; whether it serves an arbitrary commit depends on server settings the
// module does not control. So the branch is fetched and what arrived is
// compared with the approved head: a branch that moved on is a change whose
// approval no longer covers what would ride, and it is ejected for the gate to
// judge again.
//
// A CONFLICT IS ABORTED, NEVER RESOLVED. Resolving it would put a line in the
// train that no reviewer approved; the change is ejected with the conflicted
// files named, and its worker merges the target into its own branch.
func (b *containerTrainBuilder) Add(car TrainCar) (string, *TrainEjection, error) {
	fetch := credentialHelperArgs(b.project.Git.RepoURL)
	fetch = append(fetch, "fetch", gitRemote, "refs/heads/"+car.Branch)
	if _, err := b.run(b.workDir, fetch...); err != nil {
		return "", nil, err
	}
	fetched, err := b.run(b.workDir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", nil, err
	}
	if got := strings.TrimSpace(fetched.Stdout); !strings.EqualFold(got, car.HeadSHA) {
		return "", &TrainEjection{
			Reason: TrainEjectHeadMoved,
			Detail: "`" + car.Branch + "` is at " + shortSHA(got) + ", not the approved " + shortSHA(car.HeadSHA) + ".",
		}, nil
	}
	if ejection, err := b.verifyCar(car); ejection != nil || err != nil {
		return "", ejection, err
	}
	merge := gitConfigArgs(b.project, b.signing)
	merge = append(merge, "merge", "--no-ff", "--no-edit", "-m", trainMergeMessage(car), car.HeadSHA)
	result, err := b.probe(b.workDir, merge...)
	if err != nil {
		return "", nil, err
	}
	if !result.Succeeded() {
		return b.abortConflict()
	}
	head, err := b.run(b.workDir, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(head.Stdout), nil, nil
}

// verifyCar checks the commits the car would bring onto the train — those its
// approved head carries that the train does not yet — against the project's
// trusted signing keys, before anything is merged.
//
// A CAR IS JUDGED BY ITS OWN COMMITS, AND ALONE. The same check runs again on
// the whole branch when the train is pushed, but a refusal there names a
// commit and not a change, so the whole train was abandoned and rebuilt,
// identically, for ever (go-luks2 PR #210, 2026-09-25). Checked here, the one
// change that carries an untrusted commit is ejected with the commit named and
// the rest of the train rides on.
func (b *containerTrainBuilder) verifyCar(car TrainCar) (*TrainEjection, error) {
	err := b.publisher.verifyRange(b.workDir, "HEAD.."+car.HeadSHA)
	var unsigned *UnsignedCommitError
	if !errors.As(err, &unsigned) {
		return nil, err
	}
	detail := "Commit `" + shortSHA(unsigned.Commit) + "` is not signed by a key this project trusts for it (git signature status " +
		strconv.Quote(unsigned.Status)
	if unsigned.Signer != "" {
		detail += ", signer `" + unsigned.Signer + "`"
	}
	return &TrainEjection{Reason: TrainEjectUnsigned, Detail: detail + ")."}, nil
}

// abortConflict names the conflicted files, aborts the merge so the train is
// exactly as it was before this change, and answers the ejection.
func (b *containerTrainBuilder) abortConflict() (string, *TrainEjection, error) {
	conflicted, err := b.run(b.workDir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return "", nil, err
	}
	if _, err := b.run(b.workDir, "merge", "--abort"); err != nil {
		return "", nil, err
	}
	files := strings.Fields(conflicted.Stdout)
	detail := "The change conflicts with the target and the changes queued before it."
	if len(files) > 0 {
		detail += " Conflicted: `" + strings.Join(files, "`, `") + "`."
	}
	return "", &TrainEjection{Reason: TrainEjectConflict, Detail: detail}, nil
}

// Push implements trainBuilder.
func (b *containerTrainBuilder) Push(branch string) (string, error) {
	return b.publisher.Push(publishPlan{WorkDir: b.workDir, Branch: branch, Base: b.target})
}

// trainMergeMessage is the message of the commit that merges one change onto
// the train.
func trainMergeMessage(car TrainCar) string {
	return "Merge #" + strconv.Itoa(car.PullRequest) + ": " + strings.TrimSpace(car.Title)
}

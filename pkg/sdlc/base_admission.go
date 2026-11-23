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
	"strings"
	"time"
)

// DefaultMainFixLabel is the label that marks an issue as a fix for the base
// branch. While the base branch is red, only issues carrying it are adopted:
// new feature work started on a broken base inherits the breakage.
const DefaultMainFixLabel = "main-fix"

// BaseState is the CI state of the head of a project's base branch, read
// once per issue sweep.
type BaseState struct {
	Branch  string
	HeadSHA string
	CI      CIState
}

// Red reports whether the base branch's head has a failed check. Only
// CIFailure is red: a pending head may still turn green, and a head with no
// checks at all says nothing about the base. Reading either as red would let
// an unknown base stop all work.
func (b BaseState) Red() bool {
	return b.CI == CIFailure
}

// readBaseState reads the CI state of the head of branch, by the merge
// gate's definition of the checks (ciStateOf). A blank branch is the
// project's fallback default branch.
func readBaseState(ctx context.Context, forge ForgeBackend, repo RepoRef, branch string, now time.Time) (BaseState, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = FallbackDefaultBranch
	}
	head, err := forge.GetBranch(ctx, repo, branch)
	if err != nil {
		return BaseState{}, err
	}
	runs, err := forge.ListCheckRuns(ctx, repo, head.SHA)
	if err != nil {
		return BaseState{}, err
	}
	return BaseState{Branch: branch, HeadSHA: head.SHA, CI: ciStateOf(runs, head.SHA, now)}, nil
}

// admitOnBase reports whether an issue with labels may be adopted on base.
// Every issue is admitted unless the base is red (see BaseState.Red: pending
// and no-checks are not red, so an unknown base never stops all work); on a
// red base only an issue labelled mainFixLabel is, the label matched
// case-insensitively and without surrounding space.
func admitOnBase(base BaseState, labels []string, mainFixLabel string) bool {
	if !base.Red() {
		return true
	}
	want := strings.TrimSpace(mainFixLabel)
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

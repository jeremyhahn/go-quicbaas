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
	"strings"
	"testing"
)

// TestVerifyLabelsDefined_AcceptsEveryDefinedLabel proves the read-back
// passes once each spec is defined, matching names case-insensitively.
func TestVerifyLabelsDefined_AcceptsEveryDefinedLabel(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	after := []ForgeLabel{{Name: "Factory:Blocked"}, {Name: "bug"}}
	specs := []LabelSpec{{Name: "factory:blocked"}, {Name: "bug"}}
	if err := verifyLabelsDefined(ForgeGitLab, repo, after, specs); err != nil {
		t.Fatalf("verifyLabelsDefined: %v", err)
	}
}

// TestVerifyLabelsDefined_NamesTheLabelsStillMissing proves a spec the
// repository still lacks is reported, by name, as a write that did not land.
func TestVerifyLabelsDefined_NamesTheLabelsStillMissing(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	err := verifyLabelsDefined(ForgeGitLab, repo, []ForgeLabel{{Name: "bug"}}, []LabelSpec{{Name: "bug"}, {Name: "triage"}})
	var notApplied *ForgeWriteNotAppliedError
	if !errors.As(err, &notApplied) || notApplied.Type != ForgeGitLab {
		t.Fatalf("err = %v, want ForgeWriteNotAppliedError from gitlab", err)
	}
	if !strings.Contains(notApplied.Want, "triage") || strings.Contains(notApplied.Want, "bug") {
		t.Errorf("want = %q, want only the missing label", notApplied.Want)
	}
}

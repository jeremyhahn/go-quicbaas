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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestReviewerConfig_ValidateAcceptsNoneAndRefusesHalf(t *testing.T) {
	complete := ReviewerConfig{Agent: testReviewerAgent, Login: "bot", TokenSecretRef: "tok"}
	for name, tc := range map[string]struct {
		cfg    ReviewerConfig
		wantOK bool
	}{
		// A project reviewed by people, or by automation outside this
		// module, binds no reviewer. That is a configuration, not a gap.
		"unconfigured":    {ReviewerConfig{}, true},
		"complete":        {complete, true},
		"no login":        {ReviewerConfig{Agent: testReviewerAgent, TokenSecretRef: "tok"}, false},
		"no token":        {ReviewerConfig{Agent: testReviewerAgent, Login: "bot"}, false},
		"login no agent":  {ReviewerConfig{Login: "bot"}, false},
		"token no agent":  {ReviewerConfig{TokenSecretRef: "tok"}, false},
		"malformed agent": {ReviewerConfig{Agent: "Not An Agent", Login: "bot", TokenSecretRef: "tok"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("refused a valid reviewer: %v", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("accepted a half-configured reviewer: %v", err)
			}
		})
	}
}

func TestReviewerConfig_TheReviewerActsThroughItsOwnToken(t *testing.T) {
	// A verdict posted with the author's own token is a change approving
	// itself: the forge would record the author as the reviewer and the
	// independence rule would have nothing to distinguish.
	author := ForgeConfig{Type: ForgeGitea, BaseURL: "http://forge.test", Owner: "acme", Repo: "api", TokenSecretRef: "worker-tok"}
	reviewer := ReviewerConfig{Agent: testReviewerAgent, Login: "bot", TokenSecretRef: "reviewer-tok"}
	got := reviewer.ForgeConfigFor(author)
	if got.TokenSecretRef != "reviewer-tok" {
		t.Fatalf("token ref = %q, want the reviewer's", got.TokenSecretRef)
	}
	if !got.ReviewerIdentity {
		t.Fatal("the reviewer's config is not marked as the reviewer's identity")
	}
	if author.ReviewerIdentity {
		t.Fatal("the author's config was marked as the reviewer's identity")
	}
	if got.Owner != author.Owner || got.Repo != author.Repo || got.BaseURL != author.BaseURL {
		t.Fatalf("the repository changed: %+v", got)
	}
	// And the author's own configuration is untouched, because a shared
	// struct mutated in place would switch the WORKER onto the reviewer's
	// credential.
	if author.TokenSecretRef != "worker-tok" {
		t.Fatal("ForgeConfigFor mutated the caller's configuration")
	}
}

func TestReviewerConfig_StatusContext(t *testing.T) {
	bare := ReviewerConfig{Agent: testReviewerAgent}
	if bare.Context() != DefaultReviewStatusContext {
		t.Fatalf("context = %q", bare.Context())
	}
	named := ReviewerConfig{StatusContext: "  ci/review  "}
	if named.Context() != "ci/review" {
		t.Fatalf("context = %q", named.Context())
	}
	if SeatFamily(testAgents(), bare) != ModelFamilyOpenAI {
		t.Fatalf("family = %q", SeatFamily(testAgents(), bare))
	}
	if SeatFamily(testAgents(), ReviewerConfig{Agent: "ghost"}) != "" {
		t.Fatal("an undefined agent has no family")
	}
	if !bare.Configured() || (ReviewerConfig{}).Configured() {
		t.Fatal("Configured")
	}
}

func TestProjectBinding_ValidatesItsReviewer(t *testing.T) {
	binding := testBinding()
	if err := binding.Validate(); err != nil {
		t.Fatalf("the fixture does not validate: %v", err)
	}
	binding.Reviewer = ReviewerConfig{Agent: testReviewerAgent}
	if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a half-configured reviewer was bound: %v", err)
	}
}

func TestReviewJobSubject_RoundTripsThroughJobParams(t *testing.T) {
	subject := ReviewJobSubject{PullRequest: 41}
	cfg := &orchestration.JobConfig{Params: subject.Params()}
	got, err := ReviewJobSubjectFrom(cfg)
	if err != nil {
		t.Fatalf("ReviewJobSubjectFrom: %v", err)
	}
	if got != subject {
		t.Fatalf("got %+v, want %+v", got, subject)
	}
	if subject.String() != "pull request #41" {
		t.Fatalf("String = %q", subject.String())
	}
	// THE SUBJECT CARRIES NO HEAD, on purpose: a job that carried one would
	// review the tree that was current when the hook fired.
	if len(subject.Params()) != 1 {
		t.Fatalf("params = %+v; the subject grew a field", subject.Params())
	}
}

func TestReviewJobSubject_RefusesWhatNamesNoPullRequest(t *testing.T) {
	for name, cfg := range map[string]*orchestration.JobConfig{
		"absent":       {},
		"blank":        {Params: map[string]string{ParamPullRequest: "  "}},
		"not a number": {Params: map[string]string{ParamPullRequest: "#41"}},
		"zero":         {Params: map[string]string{ParamPullRequest: "0"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReviewJobSubjectFrom(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	var nilCfg *orchestration.JobConfig
	if _, err := ReviewJobSubjectFrom(nilCfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil config was accepted: %v", err)
	}
}

func TestJobConfig_ParamsAreOpaqueAndCopied(t *testing.T) {
	// The engine never reads a key and never defaults one; and a caller that
	// keeps and later mutates the map it passed cannot change the
	// configuration a running job was validated against.
	var nilCfg *orchestration.JobConfig
	if _, ok := nilCfg.Param(ParamPullRequest); ok {
		t.Fatal("a nil config answered a parameter")
	}
	cfg := &orchestration.JobConfig{}
	if _, ok := cfg.Param(ParamPullRequest); ok {
		t.Fatal("an empty config answered a parameter")
	}
	cfg.Params = map[string]string{ParamPullRequest: "41"}
	if value, ok := cfg.Param(ParamPullRequest); !ok || value != "41" {
		t.Fatalf("Param = %q %v", value, ok)
	}
}

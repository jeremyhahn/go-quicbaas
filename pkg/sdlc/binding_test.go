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
	"errors"
	"strings"
	"testing"
)

func TestProjectBinding_ValidateAcceptsACompleteBinding(t *testing.T) {
	if err := testBinding().Validate(); err != nil {
		t.Fatalf("complete binding refused: %v", err)
	}
}

func TestProjectBinding_ValidateRefusesEachMissingPart(t *testing.T) {
	cases := map[string]struct {
		mutate func(*ProjectBinding)
		want   error
		field  string
	}{
		"nil":                 {func(b *ProjectBinding) {}, ErrInvalidInput, "binding"},
		"project id":          {func(b *ProjectBinding) { b.ProjectID = "" }, ErrInvalidKeySegment, "project_id"},
		"project id with /":   {func(b *ProjectBinding) { b.ProjectID = "a/b" }, ErrInvalidKeySegment, "project_id"},
		"forge type":          {func(b *ProjectBinding) { b.Forge.Type = "" }, ErrInvalidInput, "forge.type"},
		"base url":            {func(b *ProjectBinding) { b.Forge.BaseURL = " " }, ErrInvalidInput, "forge.base_url"},
		"owner":               {func(b *ProjectBinding) { b.Forge.Owner = "" }, ErrInvalidInput, "forge.owner/repo"},
		"trigger label":       {func(b *ProjectBinding) { b.Forge.TriggerLabel = "" }, ErrInvalidInput, "forge.trigger_label"},
		"token ref":           {func(b *ProjectBinding) { b.Forge.TokenSecretRef = "" }, ErrInvalidInput, "forge.token_secret_ref"},
		"unknown strategy":    {func(b *ProjectBinding) { b.Strategy = "nope" }, ErrStrategyNotRegistered, "nope"},
		"no strategy":         {func(b *ProjectBinding) { b.Strategy = "" }, ErrStrategyNotRegistered, "available"},
		"no worker backend":   {func(b *ProjectBinding) { b.Worker.Backend = " " }, ErrInvalidInput, "worker.backend"},
		"blank verification":  {func(b *ProjectBinding) { b.Verification = []string{"make test", " "} }, ErrInvalidInput, "verification[1]"},
		"negative rounds":     {func(b *ProjectBinding) { b.MaxReviewRounds = -1 }, ErrInvalidInput, "max_review_rounds"},
		"no model is fine":    {func(b *ProjectBinding) { b.Worker.Model = "" }, nil, ""},
		"no verification ok":  {func(b *ProjectBinding) { b.Verification = nil }, nil, ""},
		"zero rounds is fine": {func(b *ProjectBinding) { b.MaxReviewRounds = 0 }, nil, ""},
		"trigger collides":    {func(b *ProjectBinding) { b.Forge.Labels[LabelInProgress] = "Task:Ready" }, ErrInvalidInput, "trigger label"},
		"zero approvals":      {func(b *ProjectBinding) { b.Review.RequiredApprovals = 0 }, ErrInvalidInput, "review.required_approvals"},
		"negative settle":     {func(b *ProjectBinding) { b.Review.ChecksSettleWindow = -1 }, ErrInvalidInput, "review.checks_settle_window"},
		"blank reviewer":      {func(b *ProjectBinding) { b.Review.Reviewers = map[string]string{"codex": " "} }, ErrInvalidInput, "review.reviewers"},
		"unknown analyzer":    {func(b *ProjectBinding) { b.Ranking.AnalyzerName = "nope" }, ErrAnalyzerNotRegistered, "nope"},
		"known analyzer okay": {func(b *ProjectBinding) { b.Ranking.AnalyzerName = DeclaredAnalyzerName }, nil, ""},
		"host key not a key": {
			func(b *ProjectBinding) { b.Forge.SSHHostKey = "ssh-ed25519 not-base64" },
			ErrInvalidInput, "forge.ssh_host_key",
		},
		"host key with host pattern": {
			func(b *ProjectBinding) { b.Forge.SSHHostKey = "forge.invalid " + testSSHHostKey },
			ErrInvalidInput, "forge.ssh_host_key",
		},
		"host key two lines": {
			func(b *ProjectBinding) { b.Forge.SSHHostKey = testSSHHostKey + "\n" + testSSHHostKey },
			ErrInvalidInput, "forge.ssh_host_key",
		},
		"host key pinned is fine":  {func(b *ProjectBinding) { b.Forge.SSHHostKey = testSSHHostKey }, nil, ""},
		"no host key is fine here": {func(b *ProjectBinding) { b.Forge.SSHHostKey = "" }, nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var binding *ProjectBinding
			if name != "nil" {
				binding = testBinding()
				tc.mutate(binding)
			}
			err := binding.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name %q", err.Error(), tc.field)
			}
		})
	}
}

// TestForgeConfig_SSHHostKeyIsOptionalButMustBeAKey states the split the
// binding cannot decide alone: the field is optional HERE, because whether an
// ssh checkout needs one depends on the project's repository URL, which lives
// on the orchestration Project. What the binding does decide is that a
// pinned value is a real OpenSSH public key, so a typo is caught on bind
// rather than by a container that dies mid-job.
func TestForgeConfig_SSHHostKeyIsOptionalButMustBeAKey(t *testing.T) {
	cfg := testBinding().Forge
	cfg.SSHHostKey = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an https-only binding needs no host key: %v", err)
	}
	cfg.SSHHostKey = "  " + testSSHHostKey + "  "
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a pinned host key was refused: %v", err)
	}
	cfg.SSHHostKey = "ssh-ed25519 AAAA-not-a-key"
	err := cfg.Validate()
	var typed *InvalidInputError
	if !errors.As(err, &typed) || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %T %v, want *InvalidInputError", err, err)
	}
	if typed.Field != "forge.ssh_host_key" {
		t.Fatalf("the refusal must name the field the operator fixes: %q", typed.Field)
	}
}

func TestForgeConfig_LabelsAndOwnership(t *testing.T) {
	cfg := testBinding().Forge
	if cfg.Label(LabelMerged) != "factory:merged" || cfg.Label("nothing") != "" {
		t.Fatalf("Label lookup wrong")
	}
	owned := cfg.OwnedLabels()
	for _, want := range []string{"task:ready", "factory:in-progress", "factory:merged"} {
		if !owned[want] {
			t.Fatalf("%s not owned: %v", want, owned)
		}
	}
	if owned["bug"] {
		t.Fatal("a label the module never writes is owned")
	}
	if cfg.RepoRef() != testRepoRef {
		t.Fatalf("RepoRef = %v", cfg.RepoRef())
	}
	var collision *TriggerLabelCollisionError
	cfg.Labels[LabelBlocked] = "TASK:READY"
	if err := cfg.Validate(); !errors.As(err, &collision) || collision.Key != LabelBlocked {
		t.Fatalf("case-insensitive collision not refused: %v", err)
	}
}

func TestReviewPolicy_SettleWindowDefaults(t *testing.T) {
	var p ReviewPolicy
	if p.SettleWindow() != DefaultChecksSettleWindow {
		t.Fatalf("zero window = %v", p.SettleWindow())
	}
	p.ChecksSettleWindow = DefaultChecksSettleWindow * 2
	if p.SettleWindow() != DefaultChecksSettleWindow*2 {
		t.Fatalf("explicit window = %v", p.SettleWindow())
	}
	p.Reviewers = map[string]string{"codex": ModelFamilyOpenAI}
	if p.ReviewerModelFamily("codex") != ModelFamilyOpenAI || p.ReviewerModelFamily("alice") != "" {
		t.Fatal("reviewer family lookup wrong")
	}
}

// TestProjectBinding_ReviewRoundsTakesTheDefaultWhenUnset: a binding that
// leaves the bound open still has one — an unbounded review loop would send
// work back forever rather than escalating it to a person.
func TestProjectBinding_ReviewRoundsTakesTheDefaultWhenUnset(t *testing.T) {
	binding := testBinding()
	for _, configured := range []int{0, -1} {
		binding.MaxReviewRounds = configured
		if got := binding.ReviewRounds(); got != DefaultMaxReviewRounds {
			t.Errorf("ReviewRounds() with %d = %d, want the default %d", configured, got, DefaultMaxReviewRounds)
		}
	}
	binding.MaxReviewRounds = 7
	if got := binding.ReviewRounds(); got != 7 {
		t.Errorf("ReviewRounds() = %d, want the configured 7", got)
	}
}

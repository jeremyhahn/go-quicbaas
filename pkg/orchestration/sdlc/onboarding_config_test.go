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
	"reflect"
	"testing"
)

// TestOnboardingLabelSpecs_DerivesTheVocabularyFromTheBinding proves the
// specs come from the binding's own Labels map rather than from a second list
// here.
//
// One declaration of the vocabulary, or a project that renames a label
// onboards under a name it never uses and every write of that label is
// refused — silently, which is the failure this whole feature exists to stop.
func TestOnboardingLabelSpecs_DerivesTheVocabularyFromTheBinding(t *testing.T) {
	forge := ForgeConfig{
		Labels:       map[string]string{LabelBlocked: "needs-human", LabelMerged: "shipped"},
		TriggerLabel: "go",
	}

	specs := OnboardingConfig{}.LabelSpecs(forge)

	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	if want := []string{"go", "needs-human", "shipped"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v sorted", names, want)
	}
	for _, spec := range specs {
		if spec.Color == "" {
			t.Errorf("%q has no colour; a forge that requires one would refuse the creation", spec.Name)
		}
	}
}

// TestOnboardingLabelSpecs_CollapsesAVocabularyThatReusesOneLabel: a project
// may display two states with the same forge label, and creating it twice
// would make a duplicate nothing can tell apart.
func TestOnboardingLabelSpecs_CollapsesAVocabularyThatReusesOneLabel(t *testing.T) {
	forge := ForgeConfig{
		Labels:       map[string]string{LabelBlocked: "parked", LabelNeedsInfo: "parked"},
		TriggerLabel: "go",
	}

	if specs := (OnboardingConfig{}).LabelSpecs(forge); len(specs) != 2 {
		t.Fatalf("specs = %+v, want two: the shared label once and the trigger", specs)
	}
}

// TestOnboardingLabelSpecs_HonoursAnOperatorsColour covers the one override.
func TestOnboardingLabelSpecs_HonoursAnOperatorsColour(t *testing.T) {
	forge := ForgeConfig{Labels: map[string]string{LabelBlocked: "parked"}, TriggerLabel: "go"}
	cfg := OnboardingConfig{LabelColors: map[string]string{LabelBlocked: "#AABBCC"}}

	for _, spec := range cfg.LabelSpecs(forge) {
		if spec.Name != "parked" {
			continue
		}
		if spec.Color != "aabbcc" {
			t.Fatalf("colour = %q, want aabbcc normalized from #AABBCC", spec.Color)
		}
		return
	}
	t.Fatal("the label was not derived at all")
}

// TestOnboardingConfig_DefaultsCoverTheWholeFleet: a binding that says nothing
// about onboarding still onboards correctly, which is what makes twenty-eight
// repositories twenty-eight rows rather than twenty-eight decisions.
func TestOnboardingConfig_DefaultsCoverTheWholeFleet(t *testing.T) {
	cfg := OnboardingConfig{}

	if got := cfg.Branch(); got != DefaultProtectedBranch {
		t.Errorf("Branch = %q, want %q", got, DefaultProtectedBranch)
	}
	if got := cfg.StatusContexts(); !reflect.DeepEqual(got, DefaultRequiredStatusContexts) {
		t.Errorf("StatusContexts = %v, want %v", got, DefaultRequiredStatusContexts)
	}
	if got := cfg.Events(); !reflect.DeepEqual(got, DefaultWebhookEvents) {
		t.Errorf("Events = %v, want %v", got, DefaultWebhookEvents)
	}
	if got := cfg.Forbidden(); len(got) != len(DefaultForbiddenPaths) {
		t.Errorf("Forbidden = %v, want the cutover's four workflows", got)
	}
}

// TestDefaultWebhookEvents_SubscribeToReviews proves the default hook
// delivers pull request reviews. GitHub sends them as their own event, not
// under pull_request, so a review submitted there would otherwise never
// reach the delivery pipeline.
func TestDefaultWebhookEvents_SubscribeToReviews(t *testing.T) {
	want := []string{"issues", "pull_request", "pull_request_review", "status"}
	if !reflect.DeepEqual(DefaultWebhookEvents, want) {
		t.Errorf("DefaultWebhookEvents = %v, want %v", DefaultWebhookEvents, want)
	}
}

// TestOnboardingConfig_DefaultsAreNotAliased proves a caller cannot mutate the
// package defaults through a value it was handed. A single shared slice would
// let one project's edit change every other project's onboarding.
func TestOnboardingConfig_DefaultsAreNotAliased(t *testing.T) {
	cfg := OnboardingConfig{}

	contexts := cfg.StatusContexts()
	contexts[0] = "tampered"
	if DefaultRequiredStatusContexts[0] == "tampered" {
		t.Fatal("a caller's edit reached the package default")
	}

	events := cfg.Events()
	events[0] = "tampered"
	if DefaultWebhookEvents[0] == "tampered" {
		t.Fatal("a caller's edit reached the package default")
	}
}

// TestOnboardingConfig_ForbiddenDirsGroupsByDirectory proves the check lists
// each directory once however many forbidden entries it holds.
func TestOnboardingConfig_ForbiddenDirsGroupsByDirectory(t *testing.T) {
	cfg := OnboardingConfig{ForbiddenPaths: []string{
		"/.github/workflows/automerge.yml",
		".github/workflows/task-worker.yml",
		"scripts/conformance.sh",
	}}

	dirs := cfg.ForbiddenDirs()
	if len(dirs) != 2 {
		t.Fatalf("dirs = %v, want two directories", dirs)
	}
	want := []string{".github/workflows/automerge.yml", ".github/workflows/task-worker.yml"}
	if got := dirs[".github/workflows"]; !reflect.DeepEqual(got, want) {
		t.Errorf("workflows = %v, want %v with the leading slash normalized away", got, want)
	}
}

// TestOnboardingConfig_Validate refuses what could not be carried out, and
// accepts what can.
func TestOnboardingConfig_Validate(t *testing.T) {
	cases := map[string]struct {
		cfg     OnboardingConfig
		wantErr bool
	}{
		"empty is valid":      {OnboardingConfig{}, false},
		"full is valid":       {OnboardingConfig{WebhookEvents: []string{"issues"}, RequiredStatusContexts: []string{"ci"}, ForbiddenPaths: []string{"a.yml"}, LabelColors: map[string]string{LabelBlocked: "#ff0000"}}, false},
		"bare hex is valid":   {OnboardingConfig{LabelColors: map[string]string{LabelBlocked: "ff0000"}}, false},
		"blank event":         {OnboardingConfig{WebhookEvents: []string{" "}}, true},
		"blank context":       {OnboardingConfig{RequiredStatusContexts: []string{""}}, true},
		"blank path":          {OnboardingConfig{ForbiddenPaths: []string{"  "}}, true},
		"short colour":        {OnboardingConfig{LabelColors: map[string]string{LabelBlocked: "#fff"}}, true},
		"non-hex colour":      {OnboardingConfig{LabelColors: map[string]string{LabelBlocked: "zzzzzz"}}, true},
		"colour with a name":  {OnboardingConfig{LabelColors: map[string]string{LabelBlocked: "red"}}, true},
		"empty colour string": {OnboardingConfig{LabelColors: map[string]string{LabelBlocked: ""}}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestProjectBinding_ValidateChecksOnboarding proves the binding's own
// validation reaches the onboarding block, so a bad colour is refused at bind
// time rather than at the first provisioning pass.
func TestProjectBinding_ValidateChecksOnboarding(t *testing.T) {
	binding := testBinding()
	binding.Onboarding.LabelColors = map[string]string{LabelBlocked: "not-a-colour"}

	if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestLabelSpec_Normalize covers the comparison form.
func TestLabelSpec_Normalize(t *testing.T) {
	got := LabelSpec{Name: "  factory:blocked ", Color: " #D93F0B ", Description: " text "}.Normalize()
	want := LabelSpec{Name: "factory:blocked", Color: "d93f0b", Description: "text"}
	if got != want {
		t.Fatalf("Normalize = %+v, want %+v", got, want)
	}
	if bare := (LabelSpec{Name: "x"}).Normalize(); bare.Color != DefaultLabelColor {
		t.Errorf("a spec with no colour normalized to %q, want the default", bare.Color)
	}
}

// TestOnboardingConfig_ExplicitValuesWinOverTheDefaults is the other half of
// the defaults test: a repository that differs says so, and what it says is
// what is used.
func TestOnboardingConfig_ExplicitValuesWinOverTheDefaults(t *testing.T) {
	cfg := OnboardingConfig{
		// The blanks are dropped rather than carried: a blank event
		// subscribes to nothing and a blank context could never be required.
		WebhookEvents:          []string{"issues", "  "},
		ProtectedBranch:        "  trunk  ",
		RequiredStatusContexts: []string{"CI / CI Gate (pull_request)", ""},
	}

	if got := cfg.Events(); !reflect.DeepEqual(got, []string{"issues"}) {
		t.Errorf("Events = %v, want [issues]", got)
	}
	if got := cfg.Branch(); got != "trunk" {
		t.Errorf("Branch = %q, want trunk trimmed", got)
	}
	if got := cfg.StatusContexts(); !reflect.DeepEqual(got, []string{"CI / CI Gate (pull_request)"}) {
		t.Errorf("StatusContexts = %v", got)
	}
	if got := cfg.Forbidden(); !reflect.DeepEqual(got, DefaultForbiddenPaths) {
		// Unset stays the default even when its neighbours are set.
		t.Errorf("Forbidden = %v, want the cutover's four", got)
	}
}

// TestOnboardingLabelSpecs_FallsBackForAKeyWithNoPalletteEntry: a binding may
// name a label key the module has no colour for, and the spec still has to be
// creatable.
func TestOnboardingLabelSpecs_FallsBackForAKeyWithNoPalletteEntry(t *testing.T) {
	forge := ForgeConfig{Labels: map[string]string{"a_key_nobody_coloured": "custom"}, TriggerLabel: "go"}

	for _, spec := range (OnboardingConfig{}).LabelSpecs(forge) {
		if spec.Name != "custom" {
			continue
		}
		if spec.Color != DefaultLabelColor {
			t.Fatalf("colour = %q, want the neutral default", spec.Color)
		}
		return
	}
	t.Fatal("the label was not derived")
}

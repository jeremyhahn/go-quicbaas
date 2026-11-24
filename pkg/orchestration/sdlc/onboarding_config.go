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
	"path"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// OnboardingConfig — what a driven repository must look like
// =============================================================================
//
// ONBOARDING IS A DATA OPERATION. Taking a repository from unmanaged to driven
// writes to the FORGE's configuration — its webhooks and its labels — from one
// binding record, and commits nothing to the repository itself. That is the
// whole reason this record exists: the system it replaces shipped orchestration
// as a workflow file copied into every repository, so a fix reached whichever
// repositories somebody remembered to copy it to. Twenty-eight repositories is
// twenty-eight rows here, not twenty-eight edits.
//
// Every field has a default that suits the company's own fleet, so a binding
// that says nothing about onboarding still onboards correctly. A field is for
// the repository that differs.

// OnboardingConfig declares what onboarding provisions and what conformance
// checks for one bound repository.
type OnboardingConfig struct {
	// WebhookEvents are the forge events the delivery subscribes to. Empty
	// takes DefaultWebhookEvents.
	//
	// A forge may EXPAND one of these into a family; that is the forge's
	// business and the adapter's read-back accounts for it.
	WebhookEvents []string `json:"webhook_events,omitempty"`

	// ProtectedBranch is the branch whose protection conformance reads.
	// Empty takes DefaultProtectedBranch.
	ProtectedBranch string `json:"protected_branch,omitempty"`

	// RequiredStatusContexts are the commit-status contexts that branch's
	// protection must require. Empty takes DefaultRequiredStatusContexts.
	//
	// THESE ARE THE GATES, and a protected branch that does not require them
	// lets a change land without the check quicbaas is the only writer of —
	// which looks exactly like a passing pipeline to everyone reading the
	// pull request. A repository whose CI publishes under its own context
	// name adds it here.
	RequiredStatusContexts []string `json:"required_status_contexts,omitempty"`

	// ForbiddenPaths are repository paths that must NOT exist, checked at
	// ForbiddenPathsRef. Empty takes DefaultForbiddenPaths.
	//
	// This is the drift check for the cutover: the orchestration workflows
	// were deleted from every managed repository on purpose, and one that
	// creeps back competes with quicbaas for the same pull requests. Stated
	// as paths rather than as "workflows" because what counts as an
	// orchestration file is this module's policy and will change as the
	// module does — an adapter method called ListWorkflows would have pushed
	// that policy into every platform's adapter.
	ForbiddenPaths []string `json:"forbidden_paths,omitempty"`

	// ForbiddenPathsRef is the ref the forbidden-path check reads. Empty
	// reads the repository's default branch, which is the one a resurrected
	// workflow would actually run from.
	ForbiddenPathsRef string `json:"forbidden_paths_ref,omitempty"`

	// LabelColors overrides the fill a label is CREATED with, keyed by the
	// module's label key (LabelInProgress, ...) plus TriggerLabelKey. Empty
	// keys take DefaultLabelColors.
	//
	// Applied on creation only. An existing label's colour is never changed;
	// see LabelSpec.
	LabelColors map[string]string `json:"label_colors,omitempty"`
}

// TriggerLabelKey is the module label key of the trigger label, so the trigger
// can be coloured and described alongside the display vocabulary without being
// one of it.
const TriggerLabelKey = "trigger"

// DefaultProtectedBranch is the branch conformance reads when a binding names
// none.
const DefaultProtectedBranch = "main"

// DefaultRequiredStatusContexts is the protection requirement a binding that
// names none is checked against.
//
// IT IS WHAT QUICBAAS ITSELF WRITES, which is what makes it the default: a
// protected branch that does not require the module's own review status can
// be merged past the module's verdict, and nothing on the pull request would
// show that it had been. The name is the review runner's own
// DefaultReviewStatusContext rather than a second constant of the same value
// — two would be one rename away from disagreeing, and the disagreement
// would show up as a conformance pass on a branch nothing gates.
var DefaultRequiredStatusContexts = []string{DefaultReviewStatusContext}

// DefaultWebhookEvents are the forge events an onboarded repository delivers.
//
// Issues drive adoption, pull requests and their reviews drive the delivery
// pipeline, and statuses drive the checks gate. Nothing else is subscribed to:
// a delivery the ingestor maps to EventUnsupported costs a verification and an
// HTTP round trip to be discarded. Reviews are named on their own because
// GitHub delivers them apart from pull_request; Gitea's pull_request family
// and GitLab's merge request hook already carry them.
var DefaultWebhookEvents = []string{"issues", "pull_request", "pull_request_review", "status"}

// DefaultForbiddenPaths are the orchestration workflows deleted from every
// managed repository at the 2026-09-12 cutover.
//
// A repository that grows one back has a second orchestrator racing quicbaas
// for the same pull requests, and the two would take turns undoing each
// other's merges — which is why this is a conformance finding and not a
// stylistic preference.
var DefaultForbiddenPaths = []string{
	".github/workflows/automerge.yml",
	".github/workflows/claude-review.yml",
	".github/workflows/opencode-review.yml",
	".github/workflows/task-worker.yml",
}

// DefaultLabelColors are the fills the module's own vocabulary is created
// with: work in flight amber, parked states red, an ending green.
//
// Applied at creation only, so a repository that recolours them keeps its
// choice for ever.
var DefaultLabelColors = map[string]string{
	LabelInProgress:   "fbca04",
	LabelInReview:     "0e8a16",
	LabelMerged:       "5319e7",
	LabelBlocked:      "d93f0b",
	LabelNeedsInfo:    "d4c5f9",
	LabelEscalated:    "b60205",
	LabelClustered:    "c5def5",
	LabelTrainEjected: "e99695",
	TriggerLabelKey:   "1d76db",
}

// defaultLabelDescriptions are the hover texts the module's vocabulary is
// created with, so an operator reading the repository's label list learns what
// each one means without reading this package.
var defaultLabelDescriptions = map[string]string{
	LabelInProgress:   "QuicBaaS Software Factory: an attempt is working on this issue",
	LabelInReview:     "QuicBaaS Software Factory: this issue's pull request is under review",
	LabelMerged:       "QuicBaaS Software Factory: this issue's pull request has landed",
	LabelBlocked:      "QuicBaaS Software Factory: parked for a person",
	LabelNeedsInfo:    "QuicBaaS Software Factory: waiting on an answer",
	LabelEscalated:    "QuicBaaS Software Factory: the engine gave up; a person is needed",
	LabelClustered:    "QuicBaaS Software Factory: covered by another issue's task and pull request",
	LabelTrainEjected: "QuicBaaS Software Factory: the merge train ejected this pull request",
	TriggerLabelKey:   "QuicBaaS Software Factory: hand this issue to the factory",
}

// Events returns the webhook events to subscribe to.
func (c OnboardingConfig) Events() []string {
	events := trimmedNonEmpty(c.WebhookEvents)
	if len(events) == 0 {
		return append([]string(nil), DefaultWebhookEvents...)
	}
	return events
}

// Branch returns the branch whose protection conformance reads.
func (c OnboardingConfig) Branch() string {
	if branch := strings.TrimSpace(c.ProtectedBranch); branch != "" {
		return branch
	}
	return DefaultProtectedBranch
}

// StatusContexts returns the contexts the protected branch must require.
func (c OnboardingConfig) StatusContexts() []string {
	contexts := trimmedNonEmpty(c.RequiredStatusContexts)
	if len(contexts) == 0 {
		return append([]string(nil), DefaultRequiredStatusContexts...)
	}
	return contexts
}

// Forbidden returns the repository paths that must not exist, normalized to
// forward-slash paths with no leading separator.
func (c OnboardingConfig) Forbidden() []string {
	declared := trimmedNonEmpty(c.ForbiddenPaths)
	if len(declared) == 0 {
		declared = append([]string(nil), DefaultForbiddenPaths...)
	}
	normalized := make([]string, 0, len(declared))
	for _, p := range declared {
		normalized = append(normalized, strings.Trim(path.Clean(p), "/"))
	}
	sort.Strings(normalized)
	return normalized
}

// ForbiddenDirs groups Forbidden by the directory each path lives in, so the
// check lists each directory ONCE however many forbidden entries it holds.
//
// Returned sorted, and the members of each directory sorted, so two passes
// over the same repository produce findings in the same order — a report whose
// order changed between runs would make a diff of two reports unreadable.
func (c OnboardingConfig) ForbiddenDirs() map[string][]string {
	dirs := make(map[string][]string)
	for _, p := range c.Forbidden() {
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		dirs[dir] = append(dirs[dir], p)
	}
	for dir := range dirs {
		sort.Strings(dirs[dir])
	}
	return dirs
}

// LabelSpecs returns every label the module writes for a binding — the display
// vocabulary plus the trigger — with the colour and description each is
// CREATED with.
//
// Derived from the binding's own Labels map rather than from a list here, so
// there is exactly one declaration of the vocabulary and a project that renames
// a label onboards under the name it actually uses. Returned sorted by name so
// two calls produce the same order.
func (c OnboardingConfig) LabelSpecs(forge ForgeConfig) []LabelSpec {
	byKey := make(map[string]string, len(forge.Labels)+1)
	for key, label := range forge.Labels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			byKey[key] = trimmed
		}
	}
	if trigger := strings.TrimSpace(forge.TriggerLabel); trigger != "" {
		byKey[TriggerLabelKey] = trigger
	}

	specs := make([]LabelSpec, 0, len(byKey))
	seen := make(map[string]bool, len(byKey))
	for key, name := range byKey {
		// A vocabulary that maps two keys onto one forge label is legal — a
		// project may display "merged" and "in_review" with the same label —
		// and must produce one spec, not two creations of the same name.
		if lowered := strings.ToLower(name); seen[lowered] {
			continue
		} else {
			seen[lowered] = true
		}
		specs = append(specs, LabelSpec{
			Name:        name,
			Color:       c.labelColor(key),
			Description: defaultLabelDescriptions[key],
		}.Normalize())
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// labelColor returns the fill a label key is created with.
func (c OnboardingConfig) labelColor(key string) string {
	if color := strings.TrimSpace(c.LabelColors[key]); color != "" {
		return color
	}
	if color := DefaultLabelColors[key]; color != "" {
		return color
	}
	return DefaultLabelColor
}

// Validate refuses an onboarding configuration that could not be carried out.
func (c OnboardingConfig) Validate() error {
	for i, event := range c.WebhookEvents {
		if strings.TrimSpace(event) == "" {
			return &InvalidInputError{
				Field:   "onboarding.webhook_events[" + strconv.Itoa(i) + "]",
				Message: "must name an event; a blank one subscribes to nothing",
			}
		}
	}
	for i, context := range c.RequiredStatusContexts {
		if strings.TrimSpace(context) == "" {
			return &InvalidInputError{
				Field:   "onboarding.required_status_contexts[" + strconv.Itoa(i) + "]",
				Message: "must name a status context; a blank one could never be required",
			}
		}
	}
	for i, p := range c.ForbiddenPaths {
		if strings.TrimSpace(p) == "" {
			return &InvalidInputError{
				Field:   "onboarding.forbidden_paths[" + strconv.Itoa(i) + "]",
				Message: "must name a path; a blank one would forbid the repository root",
			}
		}
	}
	for key, color := range c.LabelColors {
		if !isHexColor(color) {
			return &InvalidInputError{
				Field:   "onboarding.label_colors[" + key + "]",
				Message: "must be six hexadecimal digits, with or without a leading '#'; got " + quoteName(color),
			}
		}
	}
	return nil
}

// isHexColor reports whether a colour is six hexadecimal digits, optionally
// prefixed with '#'.
func isHexColor(color string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(trimmed) != 6 {
		return false
	}
	for _, r := range trimmed {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// trimmedNonEmpty returns the non-blank entries of a list, trimmed.
func trimmedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

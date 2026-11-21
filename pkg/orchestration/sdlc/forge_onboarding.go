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
	"sort"
	"strings"
)

// =============================================================================
// The onboarding half of the forge seam
// =============================================================================
//
// Taking a repository from "not managed" to "quicbaas is driving it" is a
// REPOSITORY-LEVEL conversation, where everything else in forge.go is an
// issue-, branch- or pull-request-level one. The methods live here rather than
// in forge.go for that reason and for one more: nothing quicbaas needs may be
// a file committed to the managed repository.
//
// THAT CONSTRAINT IS THE WHOLE DESIGN. The system this replaces shipped its
// orchestration engine as a 75KB workflow copied into every repository, so a
// fix reached whichever repositories somebody remembered to copy it to and
// silently missed the rest. Onboarding here is a write to the FORGE's own
// configuration — webhook, labels — driven from one ProjectBinding record, so
// changing how orchestration works never means editing 28 repositories.
//
// PORTABILITY IS IN THE VOCABULARY, NOT IN A SWITCH. Every method below is
// stated in terms all three target platforms have: a repository has labels, it
// has webhooks, it has a file tree, and it has protected branches. What differs
// between platforms is declared in ForgeCapabilities and read as data, never
// discovered with a type switch — see the AdditiveLabels precedent.

// LabelSpec is a label the module wants the repository to define.
//
// NAME IS THE IDENTITY and the only field compared when deciding whether a
// label exists. Colour and description are applied ON CREATION and never used
// to judge an existing label: a repository's own colour choice is a human's,
// and a provisioning pass that "corrected" it would be editing somebody else's
// display for no orchestration benefit.
type LabelSpec struct {
	// Name is the label as it appears on an issue. Required.
	Name string `json:"name"`

	// Color is the six-hex-digit fill applied when the label is CREATED,
	// without a leading '#'. Empty takes DefaultLabelColor.
	Color string `json:"color,omitempty"`

	// Description is the hover text applied when the label is CREATED.
	Description string `json:"description,omitempty"`
}

// DefaultLabelColor is the fill a LabelSpec with no colour is created with: a
// neutral grey, chosen so a repository that later assigns its own meaningful
// colour is visibly different from one that has not.
const DefaultLabelColor = "ededed"

// Normalize returns the spec with its comparison form settled: the name
// trimmed, the colour lowercased without its leading '#', and the default
// colour supplied when none was given.
func (s LabelSpec) Normalize() LabelSpec {
	s.Name = strings.TrimSpace(s.Name)
	s.Color = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s.Color), "#"))
	if s.Color == "" {
		s.Color = DefaultLabelColor
	}
	s.Description = strings.TrimSpace(s.Description)
	return s
}

// Validate refuses a spec the forge could not create.
func (s LabelSpec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return &InvalidInputError{Field: "label.name", Message: "a label must have a name"}
	}
	return nil
}

// ForgeLabel is a label as the forge reports it.
type ForgeLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// ForgeWebhook is a configured delivery as the forge reports it.
//
// THE SECRET IS NOT A FIELD, and its absence is deliberate rather than an
// omission to fill in later. No forge returns a configured webhook secret, so
// a field for it could only ever be empty — and an empty field invites a
// comparison that silently passes for every repository, which is worse than
// having no comparison at all.
type ForgeWebhook struct {
	// ID is the forge's identifier for the delivery.
	ID string `json:"id"`

	// URL is where the forge delivers.
	URL string `json:"url"`

	// Active reports whether the forge will actually deliver. A configured
	// but inactive webhook is the failure mode worth naming separately: it
	// looks present to anyone reading a list and delivers nothing.
	Active bool `json:"active"`

	// Events are the event names the delivery subscribes to, as the forge
	// stores them — which may be the EXPANSION of what was asked for; see
	// giteaEventFamilies.
	Events []string `json:"events,omitempty"`
}

// ForgeFile is one entry in a repository's tree.
//
// Enough to answer "does this path exist and is it a file", which is all the
// stale-workflow check asks. Content is deliberately absent: reading it would
// make a conformance pass cost a fetch per file for a question that never needs
// the bytes.
type ForgeFile struct {
	// Path is the entry's path from the repository root.
	Path string `json:"path"`

	// Name is the entry's final path segment.
	Name string `json:"name"`

	// IsDir reports whether the entry is a directory.
	IsDir bool `json:"is_dir"`
}

// ForgeBranchProtection is a protected branch's rule as the forge reports it.
//
// Only the parts an orchestrator's correctness depends on. Who may push and who
// may approve are the owner's policy and are not this module's business to read
// or to change; whether the branch requires the status contexts quicbaas itself
// writes is precisely this module's business, because a protection that does
// not require them lets a change land without the gate quicbaas is the only
// writer of.
type ForgeBranchProtection struct {
	// Branch is the branch or pattern the rule protects.
	Branch string `json:"branch"`

	// RequireStatusChecks reports whether the rule gates merges on status
	// contexts at all. False with a non-empty RequiredContexts is a
	// configured-but-disabled rule, which reads as protected and enforces
	// nothing.
	RequireStatusChecks bool `json:"require_status_checks"`

	// RequiredContexts are the status-check contexts the rule demands.
	RequiredContexts []string `json:"required_contexts,omitempty"`
}

// RequiresContext reports whether the protection demands the named status
// context.
//
// Case-insensitive on the context name and false when status checks are
// switched off, because a rule that lists a context it does not enforce
// demands nothing.
func (p *ForgeBranchProtection) RequiresContext(context string) bool {
	if p == nil || !p.RequireStatusChecks {
		return false
	}
	wanted := strings.ToLower(strings.TrimSpace(context))
	for _, held := range p.RequiredContexts {
		if strings.ToLower(strings.TrimSpace(held)) == wanted {
			return true
		}
	}
	return false
}

// ForgeOnboarding is the repository-level surface onboarding and conformance
// need.
//
// SEPARATE INTERFACE, ONE IMPLEMENTATION SET: ForgeBackend embeds it, so an
// adapter is still complete or not registered, and the seam stays a single
// thing to implement. Declaring it apart keeps the repository-level vocabulary
// legible next to the issue-level one, and keeps this file's additions from
// fighting the review-plane additions landing in forge.go alongside them.
type ForgeOnboarding interface {
	// Labels returns every label the repository defines.
	//
	// WHOLE, paginated by the adapter. A prefix would report a label the
	// repository plainly defines as missing, sending an operator to fix a
	// vocabulary that is already correct.
	Labels(ctx context.Context, repo RepoRef) ([]ForgeLabel, error)

	// EnsureLabels creates every spec the repository does not already define,
	// verified by read-back.
	//
	// NON-DESTRUCTIVE BY CONSTRUCTION — there is no delete path and no rename
	// path, and an existing label is left exactly as it is, colour and
	// description included. Labels carry history: every past pull request and
	// every saved search refers to them by name, so a provisioning pass that
	// renamed one would rewrite a repository's record of itself to make a
	// config file tidier.
	//
	// Existing labels are matched case-insensitively, because forges treat
	// label names that way and creating "Factory:Blocked" beside
	// "factory:blocked" would produce a duplicate nothing can tell apart.
	EnsureLabels(ctx context.Context, repo RepoRef, specs []LabelSpec) error

	// Webhooks returns the repository's configured deliveries.
	//
	// Never their secrets; see ForgeWebhook.
	Webhooks(ctx context.Context, repo RepoRef) ([]ForgeWebhook, error)

	// DeleteWebhook removes the delivery to url, verified by read-back.
	//
	// Removing a delivery that is not there is NOT an error, for the reason
	// removing an absent label is not: offboarding has to be safe to repeat,
	// and a caller made to check first would be back to read-then-write.
	DeleteWebhook(ctx context.Context, repo RepoRef, url string) error

	// RepoFiles lists the entries directly under dir at ref. An absent
	// directory is an empty list and no error — a repository that never had
	// the directory is the desired state, not a fault.
	//
	// THE ADAPTER LISTS; QUICBAAS JUDGES. Which paths are stale orchestration
	// is this module's policy and changes as the module changes, so it is
	// stated in a binding and evaluated here. An adapter method called
	// ListWorkflows would have pushed that policy into every platform's
	// adapter and made a GitLab port invent an equivalent of a GitHub Actions
	// directory.
	RepoFiles(ctx context.Context, repo RepoRef, ref, dir string) ([]ForgeFile, error)

	// BranchProtection returns the rule protecting branch, or nil when the
	// branch is not protected.
	//
	// NIL IS A REAL ANSWER, not an error: "this branch is unprotected" is a
	// conformance finding to report, and a caller made to distinguish it from
	// a NotFoundError would get it wrong.
	//
	// Adapters whose platform reports BranchProtectionContexts false return
	// ErrForgeUnsupported. They do not return nil: a caller told a branch was
	// unprotected would report a fixable finding for a platform on which
	// there is nothing to fix.
	BranchProtection(ctx context.Context, repo RepoRef, branch string) (*ForgeBranchProtection, error)
}

// missingLabels returns the specs the repository does not already define,
// compared case-insensitively on name.
//
// Shared by the provisioning path and the conformance path so the two can never
// disagree about what "missing" means — a repair that fixed a different set
// from the one the report named would leave an operator running it forever.
func missingLabels(defined []ForgeLabel, specs []LabelSpec) []LabelSpec {
	have := make(map[string]bool, len(defined))
	for _, label := range defined {
		have[strings.ToLower(strings.TrimSpace(label.Name))] = true
	}
	missing := make([]LabelSpec, 0, len(specs))
	for _, spec := range specs {
		normalized := spec.Normalize()
		if normalized.Name == "" || have[strings.ToLower(normalized.Name)] {
			continue
		}
		// Guard against a spec list that names the same label twice: the
		// second would be created as a duplicate the forge cannot tell apart.
		have[strings.ToLower(normalized.Name)] = true
		missing = append(missing, normalized)
	}
	return missing
}

// labelNames renders a label set for an error message, sorted and lowercased
// so two reads of the same repository compare equal.
func labelNames(labels []ForgeLabel) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, strings.ToLower(strings.TrimSpace(label.Name)))
	}
	sort.Strings(names)
	return names
}

// verifyLabelsDefined reads back an EnsureLabels pass: every spec must now be
// defined.
func verifyLabelsDefined(forge ForgeType, repo RepoRef, after []ForgeLabel, specs []LabelSpec) error {
	stillMissing := missingLabels(after, specs)
	if len(stillMissing) == 0 {
		return nil
	}
	names := make([]string, 0, len(stillMissing))
	for _, spec := range stillMissing {
		names = append(names, spec.Name)
	}
	return &ForgeWriteNotAppliedError{
		Type: forge, Operation: "EnsureLabels", Subject: repo.String(),
		Want: "labels " + strings.Join(names, " "),
		Got:  "[" + strings.Join(labelNames(after), " ") + "]",
	}
}

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
	"strconv"
	"strings"
)

// =============================================================================
// Reviewer profiles — a project default, named alternates, and which of them
// may merge code
// =============================================================================
//
// A BINDING NAMES ONE DEFAULT REVIEWER AND ANY NUMBER OF ALTERNATES. The
// default is ProjectBinding.Reviewer, exactly the field every binding already
// carries, so a binding written before profiles existed means today what it
// meant yesterday. The alternates are ProjectBinding.Reviewers. Which profile
// reviews one pull request is the default unless an OPERATOR has recorded an
// override for that pull request (reviewer_override.go) — never a label, a
// comment or anything a webhook payload carries, for the reasons
// review_config.go gives.
//
// ONLY A PROFILE THAT COUNTS TOWARD MERGE CAN LAND A CHANGE. The default
// counts unless it says otherwise; an alternate does not unless it says so.
// An advisory profile's verdict is posted to the forge and shown, and the gate
// reads past it in both directions: it cannot approve a change and it cannot
// block one. That is what makes it safe to point a new model at real pull
// requests before anyone trusts it.
//
// ONE REQUIRED STATUS CONTEXT, HOWEVER MANY REVIEWERS. Every profile that
// counts publishes under the default reviewer's context (quicbaas/review
// unless the binding says otherwise), so branch protection requires one name
// and never has to change when a reviewer is added. An advisory profile
// publishes under a context of its own — quicbaas/review/<name> by default —
// so its status can never satisfy the required one, and the gate and both
// review dispatch paths read past it.
//
// EVERY PROFILE IS ITS OWN FORGE IDENTITY. Distinct logins are what let the
// runner recognise its own standing verdict, the gate tell an advisory
// verdict from a binding one, and the independence rule attribute a family to
// each approval; distinct tokens are what make the logins mean anything.

// DefaultReviewerProfileName is the name the default reviewer answers to when
// the binding gives it none.
const DefaultReviewerProfileName = "default"

// maxReviewerProfileName bounds a profile name. It appears in a status
// context, which forges cap, and in a listing a person reads.
const maxReviewerProfileName = 64

// validReviewerProfileName reports whether name is lowercase letters, digits,
// '.', '_' and '-', starting with a letter or digit. The vocabulary is narrow
// because the name becomes part of a commit status context and of a URL.
func validReviewerProfileName(name string) bool {
	if name == "" || len(name) > maxReviewerProfileName {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '.' || c == '_' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// ProfileName is the name this reviewer answers to: its own, or
// DefaultReviewerProfileName when it has none.
func (r ReviewerConfig) ProfileName() string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	return DefaultReviewerProfileName
}

// countsOr resolves CountsTowardMerge against the role's default.
func (r ReviewerConfig) countsOr(fallback bool) bool {
	if r.CountsTowardMerge == nil {
		return fallback
	}
	return *r.CountsTowardMerge
}

// ReviewerProfile is one reviewer resolved against its binding: its name, its
// merge role and the status context it publishes under are all decided, so
// nothing downstream re-derives them.
//
// The embedded config's Name and StatusContext are the RESOLVED values, which
// is what makes Context() and ProfileName() correct on a profile without a
// second code path.
type ReviewerProfile struct {
	ReviewerConfig

	// Default reports whether this is the binding's default reviewer.
	Default bool

	// Counts reports whether this profile's verdicts decide whether a change
	// may land. False is an advisory profile.
	Counts bool
}

// Advisory reports whether this profile's verdicts are shown and not counted.
func (p ReviewerProfile) Advisory() bool { return !p.Counts }

// RequiredReviewContext is the one status context every merge-counting
// profile publishes under, and so the one branch protection requires.
//
// It is the default reviewer's own context when the default counts, and
// DefaultReviewStatusContext when it does not: an advisory default publishes
// under a context of its own like any other advisory profile, and the context
// a counting alternate publishes under must not move with it.
func (b *ProjectBinding) RequiredReviewContext() string {
	if b.Reviewer.countsOr(true) {
		return b.Reviewer.Context()
	}
	return DefaultReviewStatusContext
}

// advisoryContextFor is the status context an advisory profile publishes
// under: its own, or the required context's namespace plus its name.
func advisoryContextFor(r ReviewerConfig) string {
	if ctx := strings.TrimSpace(r.StatusContext); ctx != "" {
		return ctx
	}
	return DefaultReviewStatusContext + "/" + r.ProfileName()
}

// resolveProfile decides a profile's name, merge role and status context.
func (b *ProjectBinding) resolveProfile(r ReviewerConfig, isDefault bool) ReviewerProfile {
	counts := r.countsOr(isDefault)
	r.Name = r.ProfileName()
	if counts {
		r.StatusContext = b.RequiredReviewContext()
	} else {
		r.StatusContext = advisoryContextFor(r)
	}
	return ReviewerProfile{ReviewerConfig: r, Default: isDefault, Counts: counts}
}

// DefaultReviewerProfile is the profile that reviews every pull request no
// operator has chosen another reviewer for.
func (b *ProjectBinding) DefaultReviewerProfile() ReviewerProfile {
	return b.resolveProfile(b.Reviewer, true)
}

// ReviewerProfiles returns every configured profile, the default first and
// then the alternates in the order the binding lists them. A project that is
// not self-reviewed has none.
func (b *ProjectBinding) ReviewerProfiles() []ReviewerProfile {
	if !b.Reviewer.Configured() {
		return nil
	}
	profiles := make([]ReviewerProfile, 0, 1+len(b.Reviewers))
	profiles = append(profiles, b.DefaultReviewerProfile())
	for _, alternate := range b.Reviewers {
		profiles = append(profiles, b.resolveProfile(alternate, false))
	}
	return profiles
}

// ReviewerProfile returns the profile named name. Empty names the default.
//
// A NAME THE BINDING DOES NOT CARRY IS REFUSED, never read as the default. An
// override naming a profile that has since been removed is an operator's
// choice this module can no longer honour, and quietly substituting another
// reviewer would publish a verdict from someone the operator did not ask for.
func (b *ProjectBinding) ReviewerProfile(name string) (ReviewerProfile, error) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	if wanted == "" {
		return b.DefaultReviewerProfile(), nil
	}
	for _, profile := range b.ReviewerProfiles() {
		if profile.Name == wanted {
			return profile, nil
		}
	}
	return ReviewerProfile{}, &ReviewerProfileNotFoundError{ProjectID: b.ProjectID, Profile: name}
}

// ReviewerStatusContexts are the status contexts every profile publishes
// under: the required one and each advisory profile's own.
//
// They are the reviewers' OUTPUT, and both review dispatch paths read past
// every one of them when they ask whether a head is green enough to review —
// a status a review published must never decide whether a review may start.
func (b *ProjectBinding) ReviewerStatusContexts() []string {
	profiles := b.ReviewerProfiles()
	contexts := make([]string, 0, len(profiles))
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		ctx := strings.ToLower(profile.Context())
		if seen[ctx] {
			continue
		}
		seen[ctx] = true
		contexts = append(contexts, profile.Context())
	}
	return contexts
}

// AdvisoryReviewers is the set of logins and status contexts whose verdicts
// the gate reads past.
//
// THE ZERO VALUE IS "NOTHING IS ADVISORY", which is what a project with one
// reviewer — every project before profiles — has, so a caller that builds a
// GateInput without it judges exactly as before.
type AdvisoryReviewers struct {
	logins   map[string]bool
	contexts map[string]bool
}

// AdvisoryReviewers returns the binding's advisory logins and contexts.
func (b *ProjectBinding) AdvisoryReviewers() AdvisoryReviewers {
	advisory := AdvisoryReviewers{logins: map[string]bool{}, contexts: map[string]bool{}}
	for _, profile := range b.ReviewerProfiles() {
		if profile.Counts {
			continue
		}
		advisory.logins[strings.ToLower(strings.TrimSpace(profile.Login))] = true
		advisory.contexts[strings.ToLower(strings.TrimSpace(profile.Context()))] = true
	}
	return advisory
}

// Login reports whether a verdict from login is advisory.
func (a AdvisoryReviewers) Login(login string) bool {
	return a.logins[strings.ToLower(strings.TrimSpace(login))]
}

// Context reports whether a check named name is an advisory reviewer's status.
func (a AdvisoryReviewers) Context(name string) bool {
	return a.contexts[strings.ToLower(strings.TrimSpace(name))]
}

// Binding returns the reviews whose verdicts the gate counts: every review
// but an advisory profile's.
func (a AdvisoryReviewers) Binding(reviews []ReviewRecord) []ReviewRecord {
	if len(a.logins) == 0 {
		return reviews
	}
	kept := make([]ReviewRecord, 0, len(reviews))
	for _, review := range reviews {
		if !a.Login(review.Reviewer) {
			kept = append(kept, review)
		}
	}
	return kept
}

// BindingChecks returns the checks the gate judges: every check but an
// advisory profile's status.
func (a AdvisoryReviewers) BindingChecks(checks []CheckRecord) []CheckRecord {
	if len(a.contexts) == 0 {
		return checks
	}
	kept := make([]CheckRecord, 0, len(checks))
	for _, check := range checks {
		if !a.Context(check.Name) {
			kept = append(kept, check)
		}
	}
	return kept
}

// reviewPolicy is the binding's review policy with every profile's login
// mapped to its model family, so the independence rule attributes an approval
// from any profile without the operator repeating the mapping by hand. Every
// login key is in its normalized spelling (normalizeLogin).
//
// A PROFILE'S DERIVED FAMILY IS AUTHORITATIVE. It is computed from the Agent
// the seat names — the backend and model the runner actually dispatches; an
// entry in ReviewPolicy.Reviewers mapping that same login to another family
// would let the author's own family pass the independence rule by being called
// something else. CheckAgents refuses such an entry, and this is the second
// fence, for a binding stored before the refusal existed. An explicit entry
// still attributes a login the binding does not dispatch — a person, an
// external bot — and a seat whose family cannot be derived.
func (b *ProjectBinding) reviewPolicy(agents bindingAgents) ReviewPolicy {
	policy := b.Review
	profiles := b.ReviewerProfiles()
	if len(profiles) == 0 && len(policy.Reviewers) == 0 {
		return policy
	}
	families := make(map[string]string, len(policy.Reviewers)+len(profiles))
	for login, family := range policy.Reviewers {
		families[normalizeLogin(login)] = family
	}
	for _, profile := range profiles {
		login := normalizeLogin(profile.Login)
		if login == "" {
			continue
		}
		if family := agents.seatFamilies[profile.Name]; family != "" {
			families[login] = family
		}
	}
	policy.Reviewers = families
	return policy
}

// validateReviewers checks the default reviewer and every alternate, and the
// rules that only make sense across them.
func (b *ProjectBinding) validateReviewers() error {
	if err := b.Reviewer.Validate(); err != nil {
		return err
	}
	if len(b.Reviewers) > 0 && !b.Reviewer.Configured() {
		return &InvalidInputError{
			Field:   "reviewer",
			Message: "alternates need a default reviewer; a project that is not self-reviewed has no reviewer to override",
		}
	}
	for i, alternate := range b.Reviewers {
		field := "reviewers[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(alternate.Name) == "" {
			return &InvalidInputError{Field: field + ".name", Message: "required; an alternate is chosen by its name"}
		}
		if !alternate.Configured() {
			return &InvalidInputError{Field: field + ".agent_id", Message: "required; an alternate with no agent reviews nothing"}
		}
		if err := alternate.validateAs(field); err != nil {
			return err
		}
	}
	return b.validateReviewerIdentities()
}

// validateReviewerIdentities refuses two profiles that could be mistaken for
// one another: a shared name, login, token or advisory status context.
func (b *ProjectBinding) validateReviewerIdentities() error {
	profiles := b.ReviewerProfiles()
	names := make(map[string]string, len(profiles))
	logins := make(map[string]string, len(profiles))
	tokens := make(map[string]string, len(profiles))
	contexts := map[string]string{strings.ToLower(b.RequiredReviewContext()): "the required review context"}
	required := b.RequiredReviewContext()
	for i, profile := range profiles {
		field := "reviewer"
		if !profile.Default {
			field = "reviewers[" + strconv.Itoa(i-1) + "]"
		}
		if err := claimIdentity(names, strings.ToLower(profile.Name), field+".name", profile.Name); err != nil {
			return err
		}
		login := strings.ToLower(strings.TrimSpace(profile.Login))
		if err := claimIdentity(logins, login, field+".login", profile.Name); err != nil {
			return err
		}
		if err := claimIdentity(tokens, strings.TrimSpace(profile.TokenSecretRef), field+".token_secret_ref", profile.Name); err != nil {
			return err
		}
		if profile.Counts && profile.Default {
			continue
		}
		if profile.Counts {
			stated := strings.TrimSpace(b.Reviewers[i-1].StatusContext)
			if stated != "" && !strings.EqualFold(stated, required) {
				return &InvalidInputError{
					Field: field + ".status_context",
					Message: "a profile that counts toward merge publishes under the required context " +
						strconv.Quote(required) + "; leave it empty",
				}
			}
			continue
		}
		if err := claimIdentity(contexts, strings.ToLower(profile.Context()), field+".status_context", profile.Name); err != nil {
			return err
		}
	}
	return nil
}

// claimIdentity records one profile's claim on a value, refusing a second.
func claimIdentity(held map[string]string, value, field, profile string) error {
	if owner, taken := held[value]; taken {
		return &DuplicateReviewerIdentityError{Field: field, Profile: profile, HeldBy: owner}
	}
	held[value] = profile
	return nil
}

// DuplicateReviewerIdentityError reports two reviewer profiles sharing a name,
// a login, a token or a status context. Each profile is its own identity, or
// the runner, the gate and the independence rule cannot tell them apart.
type DuplicateReviewerIdentityError struct {
	Field   string
	Profile string
	HeldBy  string
}

// Error implements the error interface.
func (e *DuplicateReviewerIdentityError) Error() string {
	return "sdlc: invalid " + e.Field + ": reviewer profile " + strconv.Quote(e.Profile) +
		" repeats a value already held by " + strconv.Quote(e.HeldBy) +
		"; every profile needs its own name, login, token and status context"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *DuplicateReviewerIdentityError) Unwrap() error { return ErrInvalidInput }

// ReviewerProfileNotFoundError reports a profile name the binding does not
// carry: an override naming a profile that was removed, or a request naming
// one that never existed.
type ReviewerProfileNotFoundError struct {
	ProjectID string
	Profile   string
}

// Error implements the error interface.
func (e *ReviewerProfileNotFoundError) Error() string {
	return "sdlc: invalid reviewer profile: project " + e.ProjectID + " has no reviewer profile " +
		strconv.Quote(e.Profile) + "; name one the binding carries, or clear the override"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ReviewerProfileNotFoundError) Unwrap() error { return ErrInvalidInput }

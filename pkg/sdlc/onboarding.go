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
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Onboarding and conformance
// =============================================================================
//
// TWO VERBS, AND THE SEPARATION BETWEEN THEM IS THE POINT.
//
//   - Onboard WRITES: it provisions the forge's webhook and creates the labels
//     the binding declares. Idempotent, so running it again is safe and repairs
//     whatever has drifted.
//   - Check READS: it answers "is this project correctly onboarded, and if not,
//     what is wrong" and changes nothing.
//
// A verb that repaired while reporting would be the more convenient one and
// the wrong one. An operator asking what state a repository is in must be able
// to ask without changing it — and the report is worth far more than the
// repair, because the failure this whole module exists to prevent is the
// SILENT one. A check nobody runs is indistinguishable from a check that does
// not exist; a check that answers "not conformant" without a reason is barely
// better.
//
// EVERY FINDING NAMES ITS CLASS, ITS SUBJECT AND ITS REMEDY. "Not conformant"
// is useless. "label_missing: factory:blocked — onboard the project to create
// it" is a thing somebody can act on at three in the morning.

// ConformanceClass names one way a project can fail to be correctly onboarded.
//
// A distinct class per failure mode, because the modes are repaired
// differently: a missing webhook is provisioning, a stale workflow is a commit
// to the repository, and a missing protection context is a setting only an
// owner may change.
type ConformanceClass string

const (
	// ConformanceBindingInvalid reports a stored binding that no longer
	// validates — a strategy that was unregistered, a trigger label that
	// collided with a display label.
	ConformanceBindingInvalid ConformanceClass = "binding_invalid"

	// ConformanceWebhookMissing reports a repository with no delivery to
	// this node.
	ConformanceWebhookMissing ConformanceClass = "webhook_missing"

	// ConformanceWebhookInactive reports a delivery the forge has switched
	// off. It is separate from missing because it LOOKS present to anyone
	// reading the repository's settings and delivers nothing.
	ConformanceWebhookInactive ConformanceClass = "webhook_inactive"

	// ConformanceWebhookMisdirected reports a delivery for this project
	// pointing at a different node — the state a restored backup or a
	// migrated deployment leaves behind, where two nodes each believe they
	// are driving the repository.
	ConformanceWebhookMisdirected ConformanceClass = "webhook_misdirected"

	// ConformanceWebhookEventsMissing reports a delivery subscribed to less
	// than the engine needs. The webhook exists and events arrive, so
	// nothing looks broken; the deliveries that would have prompted a
	// reconcile simply never come.
	ConformanceWebhookEventsMissing ConformanceClass = "webhook_events_missing"

	// ConformanceLabelMissing reports a label the binding declares and the
	// repository does not define. It is the silent orchestration failure the
	// conformance verb was built for: the forge refuses the label write, the
	// ending never displays, and nothing tells anybody the repository was
	// never set up.
	ConformanceLabelMissing ConformanceClass = "label_missing"

	// ConformanceStaleWorkflow reports an orchestration workflow still in the
	// repository — a second orchestrator racing quicbaas for the same pull
	// requests.
	ConformanceStaleWorkflow ConformanceClass = "stale_workflow"

	// ConformanceProtectionMissing reports an unprotected branch.
	ConformanceProtectionMissing ConformanceClass = "protection_missing"

	// ConformanceProtectionContextMissing reports a protected branch that
	// does not require a status context quicbaas writes, so a change can land
	// past the module's own verdict.
	ConformanceProtectionContextMissing ConformanceClass = "protection_context_missing"

	// ConformanceCheckUnsupported reports a check the PLATFORM cannot answer.
	// It is not a failure: it is the honest alternative to reporting a
	// finding for a setting that does not exist on that forge.
	ConformanceCheckUnsupported ConformanceClass = "check_unsupported"

	// ConformanceCheckFailed reports a check that could not be run — the
	// forge was unreachable, the token lacked the scope.
	//
	// IT COUNTS AS NON-CONFORMANT. A pass whose reads failed knows nothing,
	// and reporting that as conformant is how a fleet dashboard turns an
	// outage into a clean bill of health.
	ConformanceCheckFailed ConformanceClass = "check_failed"
)

// ConformanceStatus is one check's verdict.
type ConformanceStatus string

const (
	// StatusPass means the check ran and the project satisfies it.
	StatusPass ConformanceStatus = "pass"

	// StatusFail means the check ran and produced findings.
	StatusFail ConformanceStatus = "fail"

	// StatusUnsupported means the platform cannot answer the check.
	StatusUnsupported ConformanceStatus = "unsupported"

	// StatusNotApplicable means the binding does not ask for the thing
	// checked — a poll-only project has no webhook to be missing.
	StatusNotApplicable ConformanceStatus = "not_applicable"

	// StatusError means the check could not be run.
	StatusError ConformanceStatus = "error"
)

// Check names identify a conformance check in a report, stable across
// releases so a dashboard can key on them.
const (
	CheckBinding    = "binding"
	CheckWebhook    = "webhook"
	CheckLabels     = "labels"
	CheckWorkflows  = "workflows"
	CheckProtection = "protection"
)

// ConformanceFinding is one thing that is wrong, and what to do about it.
type ConformanceFinding struct {
	// Class is the failure mode.
	Class ConformanceClass `json:"class"`

	// Check names the conformance check that produced the finding.
	Check string `json:"check"`

	// Subject is what the finding is about: the label name, the repository
	// path, the status context, the webhook URL.
	Subject string `json:"subject,omitempty"`

	// Detail says what was observed.
	Detail string `json:"detail"`

	// Remedy says what fixes it. Present on every finding: a report that
	// says only what is wrong makes the reader guess at the repair, and the
	// repairs differ — some are an Onboard call, some are a commit, one is a
	// setting only the repository's owner may change.
	Remedy string `json:"remedy"`
}

// ConformanceCheckResult is one check's outcome, so a caller can show
// "webhook ok, labels ok, protection unsupported" rather than only failures.
type ConformanceCheckResult struct {
	Name     string            `json:"name"`
	Status   ConformanceStatus `json:"status"`
	Findings int               `json:"findings"`
}

// ConformanceReport answers "is this project correctly onboarded, and if not,
// what is wrong".
type ConformanceReport struct {
	ProjectID string `json:"project_id"`
	TenantID  string `json:"tenant_id"`

	// Repo is the bound repository as "owner/repo".
	Repo string `json:"repo,omitempty"`

	// Conformant is true only when every check that RAN passed. A check that
	// errored makes the report non-conformant; a check the platform does not
	// support, or that the binding does not ask for, does not.
	Conformant bool `json:"conformant"`

	// WebhookURL is where this node expects the repository to deliver, so a
	// misdirected webhook can be read against what it should be.
	WebhookURL string `json:"webhook_url,omitempty"`

	// Checks is every check and its verdict, in a stable order.
	Checks []ConformanceCheckResult `json:"checks"`

	// Findings is everything wrong, in check order.
	Findings []ConformanceFinding `json:"findings,omitempty"`

	// CheckedAt is when the pass ran.
	CheckedAt time.Time `json:"checked_at"`
}

// OnboardingReport is what a provisioning pass DID, plus the conformance
// report the repository is in afterwards.
//
// The two together, because "onboarding succeeded" and "the repository is now
// conformant" are different claims: provisioning can succeed while a stale
// workflow it is not allowed to delete remains.
type OnboardingReport struct {
	ProjectID string `json:"project_id"`
	TenantID  string `json:"tenant_id"`
	Repo      string `json:"repo,omitempty"`

	// WebhookURL is the delivery this node provisioned, empty for a
	// poll-only project.
	WebhookURL string `json:"webhook_url,omitempty"`

	// WebhookEnsured reports that the delivery was created or brought into
	// line with the spec.
	WebhookEnsured bool `json:"webhook_ensured"`

	// LabelsCreated names the labels this pass created, empty on a repeat
	// run. It is the idempotency evidence an operator sees.
	LabelsCreated []string `json:"labels_created,omitempty"`

	// Conformance is the state the repository is in after provisioning.
	Conformance *ConformanceReport `json:"conformance,omitempty"`
}

// =============================================================================
// Onboarder
// =============================================================================

// Onboarder provisions a bound repository's forge configuration and reports
// its conformance.
//
// It holds no store: every call is given the binding it works on, so the
// service layer stays the only thing that reads bindings and the onboarder
// stays testable against a forge alone.
type Onboarder struct {
	forges   ForgeResolver
	secrets  WebhookSecretResolver
	hookBase string
}

// WebhookPathPrefix is the ingress route a provisioned delivery targets. It
// mirrors the mounted REST route POST /api/v1/sdlc/webhook/{projectID}; a
// drift test pins the pair, because a prefix that disagreed with the router
// would provision 28 repositories to deliver into a 404.
const WebhookPathPrefix = "/api/v1/sdlc/webhook/"

// NewOnboarder wires the onboarder.
//
// hookBase is this NODE's externally reachable base URL, and it is required
// for a reason worth stating: "the webhook points at this node" is a property
// of the node, not of the project, and a per-project URL would let 28 bindings
// disagree about where a delivery goes. An empty one is refused rather than
// defaulted — guessing a public address would provision deliveries into the
// void and report them provisioned.
func NewOnboarder(forges ForgeResolver, secrets WebhookSecretResolver, hookBase string) (*Onboarder, error) {
	if forges == nil {
		return nil, &InvalidInputError{Field: "forges", Message: "onboarding writes to the forge"}
	}
	if secrets == nil {
		return nil, &InvalidInputError{
			Field:   "secrets",
			Message: "the webhook secret comes from the secrets engine; a literal would be a credential in a config file",
		}
	}
	trimmed, err := NormalizeWebhookPublicURL(hookBase)
	if err != nil {
		return nil, err
	}
	return &Onboarder{forges: forges, secrets: secrets, hookBase: trimmed}, nil
}

// NormalizeWebhookPublicURL trims a node's public base URL and refuses one
// the webhook route cannot be appended to.
//
// THE ROUTE IS APPENDED TEXTUALLY, which makes the accepted shape narrower
// than "a URL". `https://factory.example?x=1` parses, has a scheme and a
// host, and passes any check that asks only those two questions — and
// produces `https://factory.example?x=1/api/v1/sdlc/webhook/<project>`, which
// a forge delivers to the ROOT path with the route buried in a query string.
// A fragment swallows the route the same way, and userinfo puts a credential
// in a delivery URL the forge stores and displays.
//
// None of that fails loudly. Onboarding provisions the delivery, read-back
// verification compares the same malformed string and agrees, and conformance
// reports the repository healthy while every delivery goes nowhere — across
// as many repositories as the fleet has. So the refusal is at the edge, where
// one operator reads one message, rather than at the first delivery that did
// not arrive.
//
// A PATH IS ALLOWED and deliberately so: a node mounted behind a reverse
// proxy at `https://factory.example/quicbaas` appends correctly.
func NormalizeWebhookPublicURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "required; without this node's public base URL a provisioned delivery has nowhere to go",
		}
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must be a URL: " + err.Error(),
		}
	}
	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must be an absolute http or https URL; a forge delivers over neither scheme otherwise, got " + quoteName(raw),
		}
	case parsed.Host == "":
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must name a host a forge can reach; got " + quoteName(raw),
		}
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must carry no query string; the webhook route is appended to it, and a query swallows the route, got " + quoteName(raw),
		}
	case parsed.Fragment != "":
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must carry no fragment; the webhook route is appended to it, and a fragment swallows the route, got " + quoteName(raw),
		}
	case parsed.User != nil:
		return "", &InvalidInputError{
			Field:   "webhook_public_url",
			Message: "must carry no userinfo; the forge stores and displays the delivery URL, so a credential in it is a credential published",
		}
	}
	return trimmed, nil
}

// WebhookURL is where this node expects one project's deliveries.
func (o *Onboarder) WebhookURL(projectID string) string {
	return o.hookBase + WebhookPathPrefix + url.PathEscape(projectID)
}

// Onboard provisions the repository's forge configuration and returns what it
// did together with the conformance the repository is in afterwards.
//
// IDEMPOTENT, and the idempotency is the feature. Running it on an
// already-onboarded project creates nothing and changes nothing; running it on
// one whose webhook was deleted by hand puts the webhook back. That is what
// makes onboarding 28 repositories a loop somebody can re-run without thinking
// about which ones already worked.
func (o *Onboarder) Onboard(ctx context.Context, tenantID string, binding *ProjectBinding) (*OnboardingReport, error) {
	if err := o.checkArgs(tenantID, binding); err != nil {
		return nil, err
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	forge, err := o.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, err
	}

	report := &OnboardingReport{
		ProjectID: binding.ProjectID,
		TenantID:  tenantID,
		Repo:      binding.Repo().String(),
	}
	created, err := o.ensureLabels(ctx, forge, binding)
	if err != nil {
		return nil, err
	}
	report.LabelsCreated = created

	// LABELS FIRST, WEBHOOK SECOND. A repository that starts delivering
	// before its labels exist hands the engine work it cannot display the
	// state of, and the label write fails at exactly the moment an operator
	// is watching something else.
	ensured, hookURL, err := o.ensureWebhook(ctx, tenantID, forge, binding)
	if err != nil {
		return nil, err
	}
	report.WebhookEnsured = ensured
	report.WebhookURL = hookURL

	report.Conformance = o.Check(ctx, tenantID, binding)
	return report, nil
}

// Offboard removes what onboarding provisioned that would otherwise keep
// running: the webhook.
//
// LABELS ARE NOT REMOVED, and never will be. They carry history — every past
// pull request and every saved search refers to them — so deleting them on
// unbind would rewrite a repository's record of itself to tidy up after a
// configuration change. The webhook is different: left behind it delivers to a
// node that no longer drives the project, which is a live wrong behaviour
// rather than a stale display.
func (o *Onboarder) Offboard(ctx context.Context, tenantID string, binding *ProjectBinding) error {
	if err := o.checkArgs(tenantID, binding); err != nil {
		return err
	}
	forge, err := o.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return err
	}
	return forge.DeleteWebhook(ctx, binding.Repo(), o.WebhookURL(binding.ProjectID))
}

// Check reports whether the project is correctly onboarded. It changes
// nothing.
//
// IT ALWAYS RETURNS A REPORT. A pass that could not reach the forge answers
// with a non-conformant report carrying a check_failed finding, rather than an
// error — because the caller is a dashboard asking about 28 projects, and one
// unreachable forge must show as one red row, not as a failed page.
func (o *Onboarder) Check(ctx context.Context, tenantID string, binding *ProjectBinding) *ConformanceReport {
	report := &ConformanceReport{
		TenantID:  tenantID,
		CheckedAt: time.Now().UTC(),
	}
	if binding == nil {
		report.add(CheckBinding, StatusFail, ConformanceFinding{
			Class:  ConformanceBindingInvalid,
			Check:  CheckBinding,
			Detail: "the project has no binding",
			Remedy: "bind the project to a forge repository",
		})
		report.settle()
		return report
	}
	report.ProjectID = binding.ProjectID
	report.Repo = binding.Repo().String()
	report.WebhookURL = o.WebhookURL(binding.ProjectID)

	if err := binding.Validate(); err != nil {
		// A binding that does not validate cannot be checked against a forge:
		// the repository it names, the labels it declares and the token it
		// resolves are all in question. Report the one finding that matters
		// and stop, rather than producing a cascade of consequences.
		report.add(CheckBinding, StatusFail, ConformanceFinding{
			Class:   ConformanceBindingInvalid,
			Check:   CheckBinding,
			Subject: binding.ProjectID,
			Detail:  err.Error(),
			Remedy:  "re-bind the project with a binding that validates",
		})
		report.settle()
		return report
	}
	report.add(CheckBinding, StatusPass)

	forge, err := o.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		report.add(CheckWebhook, StatusError, ConformanceFinding{
			Class:   ConformanceCheckFailed,
			Check:   CheckWebhook,
			Subject: binding.Repo().String(),
			Detail:  "the forge adapter could not be built: " + err.Error(),
			Remedy:  "check the binding's token_secret_ref resolves and the forge is reachable",
		})
		report.settle()
		return report
	}

	// An ordered list rather than a map: the report's order is part of its
	// contract, so a diff between two passes reads as a change in the
	// repository rather than in Go's map iteration.
	checks := []struct {
		name string
		run  func(context.Context, ForgeBackend, *ProjectBinding, *ConformanceReport)
	}{
		{CheckWebhook, o.checkWebhook},
		{CheckLabels, o.checkLabels},
		{CheckWorkflows, o.checkWorkflows},
		{CheckProtection, o.checkProtection},
	}
	for _, check := range checks {
		check.run(ctx, forge, binding, report)
	}
	report.settle()
	return report
}

// checkArgs refuses a call that names no tenant or no binding.
func (o *Onboarder) checkArgs(tenantID string, binding *ProjectBinding) error {
	if strings.TrimSpace(tenantID) == "" {
		return &InvalidInputError{Field: "tenant_id", Message: "onboarding is tenant-scoped"}
	}
	if binding == nil {
		return &InvalidInputError{Field: "binding", Message: "must not be nil"}
	}
	return nil
}

// =============================================================================
// Provisioning
// =============================================================================

// ensureLabels creates the declared vocabulary the repository is missing and
// returns what it created.
//
// The missing set is computed HERE as well as inside the adapter, because the
// report has to name what changed. Both sides compute it with missingLabels,
// so the number in the report and the set the adapter writes cannot disagree.
func (o *Onboarder) ensureLabels(ctx context.Context, forge ForgeBackend, binding *ProjectBinding) ([]string, error) {
	specs := binding.Onboarding.LabelSpecs(binding.Forge)
	if len(specs) == 0 {
		return nil, nil
	}
	defined, err := forge.Labels(ctx, binding.Repo())
	if err != nil {
		return nil, err
	}
	missing := missingLabels(defined, specs)
	if len(missing) == 0 {
		return nil, nil
	}
	if err := forge.EnsureLabels(ctx, binding.Repo(), specs); err != nil {
		return nil, err
	}
	created := make([]string, 0, len(missing))
	for _, spec := range missing {
		created = append(created, spec.Name)
	}
	sort.Strings(created)
	return created, nil
}

// ensureWebhook provisions the repository's delivery to this node.
//
// A binding that names no webhook secret is POLL-ONLY by its own declaration
// (ForgeConfig.WebhookSecretRef), and no delivery is provisioned for it. That
// is a configuration, not a degraded mode: the polling reconciler is the floor
// every correctness claim in this module rests on, and webhooks are hints on
// top of it.
func (o *Onboarder) ensureWebhook(
	ctx context.Context, tenantID string, forge ForgeBackend, binding *ProjectBinding,
) (bool, string, error) {
	ref := strings.TrimSpace(binding.Forge.WebhookSecretRef)
	if ref == "" {
		return false, "", nil
	}
	secret, err := o.secrets.WebhookSecret(ctx, tenantID, ref)
	if err != nil {
		return false, "", err
	}
	// The secret is adapter-bound for the length of this call and wiped on
	// the way out: it is a copy the resolver made for us to zeroize.
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	if len(secret) == 0 {
		return false, "", &WebhookSecretMissingError{
			ProjectID: binding.ProjectID,
			Reason:    "the secret named by " + ref + " resolved empty",
		}
	}

	hookURL := o.WebhookURL(binding.ProjectID)
	spec := WebhookSpec{URL: hookURL, Events: binding.Onboarding.Events(), SecretRef: ref}
	if err := forge.EnsureWebhook(ctx, binding.Repo(), spec, secret); err != nil {
		return false, hookURL, err
	}
	return true, hookURL, nil
}

// =============================================================================
// Checks
// =============================================================================

// checkWebhook reports whether the repository delivers to this node.
func (o *Onboarder) checkWebhook(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, report *ConformanceReport) {
	if strings.TrimSpace(binding.Forge.WebhookSecretRef) == "" {
		// Poll-only by declaration. Not a finding: see ensureWebhook.
		report.add(CheckWebhook, StatusNotApplicable)
		return
	}
	hooks, err := forge.Webhooks(ctx, binding.Repo())
	if err != nil {
		report.add(CheckWebhook, StatusError, ConformanceFinding{
			Class:   ConformanceCheckFailed,
			Check:   CheckWebhook,
			Subject: binding.Repo().String(),
			Detail:  "the repository's webhooks could not be read: " + err.Error(),
			Remedy:  "check the forge is reachable and the binding's token may administer the repository",
		})
		return
	}

	wanted := o.WebhookURL(binding.ProjectID)
	var mine *ForgeWebhook
	strays := make([]string, 0)
	for i := range hooks {
		switch {
		case hooks[i].URL == wanted:
			mine = &hooks[i]
		case isSDLCWebhookURL(hooks[i].URL, binding.ProjectID):
			// A delivery for THIS project aimed somewhere else: the state a
			// restored backup or a migrated node leaves behind, where two
			// nodes each believe they drive the repository.
			strays = append(strays, hooks[i].URL)
		}
	}

	findings := make([]ConformanceFinding, 0, len(strays)+2)
	sort.Strings(strays)
	for _, stray := range strays {
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceWebhookMisdirected,
			Check:   CheckWebhook,
			Subject: stray,
			Detail:  "this project's deliveries also go to " + stray + ", which is not this node",
			Remedy:  "remove the delivery on the forge, or unbind the project from the node that owns it",
		})
	}

	if mine == nil {
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceWebhookMissing,
			Check:   CheckWebhook,
			Subject: wanted,
			Detail:  "the repository has no delivery to " + wanted,
			Remedy:  "onboard the project to provision it",
		})
		report.add(CheckWebhook, StatusFail, findings...)
		return
	}
	if !mine.Active {
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceWebhookInactive,
			Check:   CheckWebhook,
			Subject: wanted,
			Detail:  "the delivery to " + wanted + " exists but the forge has it switched off",
			Remedy:  "onboard the project to re-activate it",
		})
	}
	if missing := missingEvents(binding.Onboarding.Events(), mine.Events); len(missing) > 0 {
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceWebhookEventsMissing,
			Check:   CheckWebhook,
			Subject: strings.Join(missing, " "),
			Detail:  "the delivery does not subscribe to " + strings.Join(missing, ", "),
			Remedy:  "onboard the project to bring the subscription into line",
		})
	}
	report.add(CheckWebhook, statusFor(findings), findings...)
}

// isSDLCWebhookURL reports whether a configured delivery is one of this
// module's, for the given project, on SOME node.
//
// Matched on the ingress path rather than on the host, which is the whole
// point: a delivery to another node's address carrying this project's ingress
// path is exactly the misdirection worth reporting, and a host comparison
// could not see it.
func isSDLCWebhookURL(hookURL, projectID string) bool {
	parsed, err := url.Parse(hookURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), WebhookPathPrefix+projectID)
}

// checkLabels reports every declared label the repository does not define.
func (o *Onboarder) checkLabels(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, report *ConformanceReport) {
	specs := binding.Onboarding.LabelSpecs(binding.Forge)
	if len(specs) == 0 {
		report.add(CheckLabels, StatusNotApplicable)
		return
	}
	defined, err := forge.Labels(ctx, binding.Repo())
	if err != nil {
		report.add(CheckLabels, StatusError, ConformanceFinding{
			Class:   ConformanceCheckFailed,
			Check:   CheckLabels,
			Subject: binding.Repo().String(),
			Detail:  "the repository's labels could not be read: " + err.Error(),
			Remedy:  "check the forge is reachable and the binding's token may read the repository",
		})
		return
	}
	missing := missingLabels(defined, specs)
	findings := make([]ConformanceFinding, 0, len(missing))
	for _, spec := range missing {
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceLabelMissing,
			Check:   CheckLabels,
			Subject: spec.Name,
			Detail: "the binding declares " + spec.Name + " and the repository does not define it; " +
				"every write of that label will be refused and the issue will never display its state",
			Remedy: "onboard the project to create it",
		})
	}
	report.add(CheckLabels, statusFor(findings), findings...)
}

// checkWorkflows reports orchestration files still in the repository.
func (o *Onboarder) checkWorkflows(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, report *ConformanceReport) {
	ref := binding.Onboarding.ForbiddenPathsRef
	dirs := binding.Onboarding.ForbiddenDirs()

	// Sorted so the report's order is the repository's, not the map's.
	names := make([]string, 0, len(dirs))
	for dir := range dirs {
		names = append(names, dir)
	}
	sort.Strings(names)

	findings := make([]ConformanceFinding, 0)
	for _, dir := range names {
		entries, err := forge.RepoFiles(ctx, binding.Repo(), ref, dir)
		if err != nil {
			report.add(CheckWorkflows, StatusError, ConformanceFinding{
				Class:   ConformanceCheckFailed,
				Check:   CheckWorkflows,
				Subject: dir,
				Detail:  "the repository's " + quoteName(dir) + " could not be listed: " + err.Error(),
				Remedy:  "check the forge is reachable and the binding's token may read the repository's contents",
			})
			return
		}
		present := make(map[string]bool, len(entries))
		for _, entry := range entries {
			present[strings.Trim(entry.Path, "/")] = true
		}
		for _, forbidden := range dirs[dir] {
			if !present[forbidden] {
				continue
			}
			findings = append(findings, ConformanceFinding{
				Class:   ConformanceStaleWorkflow,
				Check:   CheckWorkflows,
				Subject: forbidden,
				Detail: forbidden + " is still in the repository; it is a second orchestrator " +
					"competing with quicbaas for the same pull requests",
				Remedy: "delete the file from the repository's default branch",
			})
		}
	}
	report.add(CheckWorkflows, statusFor(findings), findings...)
}

// checkProtection reports a protected branch that does not require the status
// contexts quicbaas writes.
func (o *Onboarder) checkProtection(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, report *ConformanceReport) {
	branch := binding.Onboarding.Branch()
	if !forge.Capabilities().BranchProtectionContexts {
		report.add(CheckProtection, StatusUnsupported, ConformanceFinding{
			Class:   ConformanceCheckUnsupported,
			Check:   CheckProtection,
			Subject: branch,
			Detail:  string(forge.Type()) + " cannot require named status contexts on a protected branch",
			Remedy:  "none; the platform has no such setting to fix",
		})
		return
	}

	protection, err := forge.BranchProtection(ctx, binding.Repo(), branch)
	if err != nil {
		// ErrForgeUnsupported from an adapter whose capability says otherwise
		// is a wiring error, not an outage; it is still an error to report,
		// and reporting it distinctly is how the disagreement gets noticed.
		status, class := StatusError, ConformanceCheckFailed
		if errors.Is(err, ErrForgeUnsupported) {
			status, class = StatusUnsupported, ConformanceCheckUnsupported
		}
		report.add(CheckProtection, status, ConformanceFinding{
			Class:   class,
			Check:   CheckProtection,
			Subject: branch,
			Detail:  "the protection on " + branch + " could not be read: " + err.Error(),
			Remedy:  "check the forge is reachable and the binding's token may administer the repository",
		})
		return
	}

	contexts := binding.Onboarding.StatusContexts()
	if protection == nil {
		report.add(CheckProtection, StatusFail, ConformanceFinding{
			Class:   ConformanceProtectionMissing,
			Check:   CheckProtection,
			Subject: branch,
			Detail: branch + " is not protected, so a change can land without " +
				strings.Join(contexts, ", "),
			Remedy: "protect the branch on the forge and require " + strings.Join(contexts, ", "),
		})
		return
	}

	findings := make([]ConformanceFinding, 0, len(contexts))
	for _, wanted := range contexts {
		if protection.RequiresContext(wanted) {
			continue
		}
		findings = append(findings, ConformanceFinding{
			Class:   ConformanceProtectionContextMissing,
			Check:   CheckProtection,
			Subject: wanted,
			Detail: "the protection on " + branch + " does not require " + wanted +
				", so a change can land past it",
			// NOT an Onboard call. Who may merge is the repository owner's
			// policy, and provisioning that behind their back would be this
			// module widening its own authority.
			Remedy: "require " + wanted + " on the forge's protection rule for " + branch,
		})
	}
	report.add(CheckProtection, statusFor(findings), findings...)
}

// statusFor maps a check's findings onto its verdict.
func statusFor(findings []ConformanceFinding) ConformanceStatus {
	if len(findings) == 0 {
		return StatusPass
	}
	return StatusFail
}

// add records one check's verdict and its findings.
func (r *ConformanceReport) add(name string, status ConformanceStatus, findings ...ConformanceFinding) {
	r.Checks = append(r.Checks, ConformanceCheckResult{Name: name, Status: status, Findings: len(findings)})
	r.Findings = append(r.Findings, findings...)
}

// settle computes the report's verdict.
//
// FAIL-CLOSED on an errored check: a pass whose reads failed knows nothing
// about the repository, and calling that conformant is how a fleet dashboard
// turns an outage into a clean bill of health.
func (r *ConformanceReport) settle() {
	r.Conformant = true
	for _, check := range r.Checks {
		if check.Status == StatusFail || check.Status == StatusError {
			r.Conformant = false
			return
		}
	}
}

// FindingsOfClass returns the report's findings of one class, for a caller
// that wants to act on one failure mode.
func (r *ConformanceReport) FindingsOfClass(class ConformanceClass) []ConformanceFinding {
	out := make([]ConformanceFinding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if finding.Class == class {
			out = append(out, finding)
		}
	}
	return out
}

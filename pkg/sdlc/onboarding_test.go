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
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// =============================================================================
// Onboarding, against a stateful forge
// =============================================================================
//
// These exercise the REAL Gitea adapter against the in-memory Gitea in
// forge_gitea_stateful_test.go, so the properties under test — idempotency,
// drift repair, non-destruction — are properties of a sequence of HTTP calls
// against a server that remembers, not of a double agreeing with the code.

const onboardingHookBase = "https://node.test"

// onboardTestBinding is the fixture binding, pointed at the repository the
// stateful Gitea serves.
func onboardTestBinding() *ProjectBinding {
	binding := testBinding()
	binding.Forge.Owner = testRepo.Owner
	binding.Forge.Repo = testRepo.Repo
	return binding
}

// giteaForges resolves every binding onto one real Gitea adapter.
type giteaForges struct {
	backend ForgeBackend
	err     error
}

func (f *giteaForges) ForgeFor(context.Context, string, ForgeConfig) (ForgeBackend, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.backend, nil
}

// onboardingSecrets is the webhook secret the fixture binding names, resolved
// through the package's own staticSecrets (webhook_test.go) so there is one
// resolver double in the suite rather than two that could drift.
//
// It hands back a COPY on every call, as the real resolver does: the onboarder
// zeroizes what it is given, and a shared slice would empty on the first pass
// and make the second look like a missing secret.
func onboardingSecrets() *staticSecrets {
	return &staticSecrets{secrets: map[string][]byte{"wh": []byte("hmac")}}
}

// newOnboarderAgainstGitea wires an onboarder onto a fresh stateful Gitea.
func newOnboarderAgainstGitea(t *testing.T) (*Onboarder, *giteafake.Server) {
	t.Helper()
	backend, gitea := newStatefulGitea(t)
	onboarder, err := NewOnboarder(&giteaForges{backend: backend}, onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}
	return onboarder, gitea
}

// =============================================================================
// Construction
// =============================================================================

// TestNewOnboarder_RefusesAnUnusableConfiguration proves an onboarder that
// could only fail at the first repository is refused at construction.
//
// The public URL case is the one that matters: a guessed or empty base would
// provision twenty-eight repositories to deliver into the void and report
// every one of them provisioned.
func TestNewOnboarder_RefusesAnUnusableConfiguration(t *testing.T) {
	forges := &giteaForges{}
	secrets := onboardingSecrets()
	cases := map[string]struct {
		forges  ForgeResolver
		secrets WebhookSecretResolver
		base    string
	}{
		"no forges":        {nil, secrets, onboardingHookBase},
		"no secrets":       {forges, nil, onboardingHookBase},
		"relative url":     {forges, secrets, "/api"},
		"scheme only":      {forges, secrets, "https://"},
		"not a url at all": {forges, secrets, "node.test"},

		// AN ORIGIN, NOT MERELY A URL. The webhook route is appended to this
		// value textually, so each of these has a scheme and a host and
		// still cannot address the webhook endpoint. See
		// TestWebhookURL_IsRefusedRatherThanBuiltWrong.
		"query":    {forges, secrets, "https://node.test?x=1"},
		"fragment": {forges, secrets, "https://node.test#frag"},
		"userinfo": {forges, secrets, "https://u:p@node.test"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			onboarder, err := NewOnboarder(tc.forges, tc.secrets, tc.base)
			if onboarder != nil {
				t.Fatal("an unusable onboarder was constructed")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestOnboarderWebhookURL_MirrorsTheMountedRoute pins the provisioned URL
// against the ingress route. A prefix that drifted from the router would
// provision every managed repository to deliver into a 404, and every
// delivery would be lost silently.
func TestOnboarderWebhookURL_MirrorsTheMountedRoute(t *testing.T) {
	onboarder, _ := newOnboarderAgainstGitea(t)

	got := onboarder.WebhookURL("proj-1")
	want := "https://node.test/api/v1/sdlc/webhook/proj-1"
	if got != want {
		t.Fatalf("WebhookURL = %q, want %q", got, want)
	}
	if !strings.HasSuffix(strings.TrimSuffix(want, "proj-1"), WebhookPathPrefix) {
		t.Errorf("the URL does not carry the ingress prefix %q", WebhookPathPrefix)
	}
}

// TestOnboarderWebhookURL_EscapesTheProjectID: a project id is operator input
// and must not be able to reshape the path it is placed in.
func TestOnboarderWebhookURL_EscapesTheProjectID(t *testing.T) {
	onboarder, _ := newOnboarderAgainstGitea(t)

	got := onboarder.WebhookURL("a/b")
	if strings.Contains(strings.TrimPrefix(got, onboardingHookBase+WebhookPathPrefix), "/") {
		t.Fatalf("WebhookURL = %q, want the project id escaped into one path segment", got)
	}
}

// =============================================================================
// Onboarding
// =============================================================================

// TestOnboard_ProvisionsTheWebhookAndTheVocabulary is the headline: a
// repository nobody has touched comes out driven.
func TestOnboard_ProvisionsTheWebhookAndTheVocabulary(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()

	report, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	if !report.WebhookEnsured {
		t.Error("the report does not claim a webhook was provisioned")
	}
	wantHook := []string{"https://node.test/api/v1/sdlc/webhook/" + testProject}
	if got := gitea.HookURLs(); !reflect.DeepEqual(got, wantHook) {
		t.Fatalf("hooks = %v, want %v", got, wantHook)
	}

	// Every declared label plus the trigger, and nothing else.
	want := []string{
		"factory:blocked", "factory:in-progress", "factory:in-review",
		"factory:merged", "factory:needs-info", "task:ready",
	}
	if got := gitea.LabelNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	created := append([]string(nil), report.LabelsCreated...)
	sort.Strings(created)
	if !reflect.DeepEqual(created, want) {
		t.Errorf("LabelsCreated = %v, want %v", created, want)
	}
}

// TestOnboard_IsIdempotent is the property that makes onboarding twenty-eight
// repositories a loop nobody has to keep notes about: run it twice and there
// is one webhook and one of each label.
func TestOnboard_IsIdempotent(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	// Protection is the one thing onboarding deliberately does NOT provision
	// — who may merge is the owner's setting — so the repository carries it
	// already for the conformance assertion below to mean what it says.
	gitea.PutProtection("main", true, DefaultReviewStatusContext)

	first, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("first Onboard: %v", err)
	}
	second, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("second Onboard: %v", err)
	}

	if len(first.LabelsCreated) == 0 {
		t.Fatal("the first pass created nothing")
	}
	if len(second.LabelsCreated) != 0 {
		t.Errorf("the second pass created %v; an idempotent pass creates nothing", second.LabelsCreated)
	}
	if got := gitea.HookURLs(); len(got) != 1 {
		t.Errorf("hooks = %v, want exactly one after two passes", got)
	}
	if created := gitea.Calls("POST /labels"); created != 6 {
		t.Errorf("created %d labels over two passes, want 6", created)
	}
	if created := gitea.Calls("POST /hooks"); created != 1 {
		t.Errorf("created %d deliveries over two passes, want 1", created)
	}
	if !second.Conformance.Conformant {
		t.Errorf("an onboarded project reported non-conformant: %+v", second.Conformance.Findings)
	}
}

// TestOnboard_RepairsAWebhookDeletedByHand is drift repair. Somebody removes
// the delivery in the forge's UI; the next pass puts it back, and nothing else
// changes.
func TestOnboard_RepairsAWebhookDeletedByHand(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("first Onboard: %v", err)
	}

	gitea.DropHooks()
	if got := gitea.HookURLs(); len(got) != 0 {
		t.Fatalf("the fixture did not drop the delivery: %v", got)
	}

	report, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("repair Onboard: %v", err)
	}
	if got := gitea.HookURLs(); len(got) != 1 {
		t.Fatalf("hooks = %v, want the delivery back", got)
	}
	if len(report.LabelsCreated) != 0 {
		t.Errorf("the repair pass recreated labels %v; only the webhook had drifted", report.LabelsCreated)
	}
}

// TestOnboard_RepairsADeactivatedWebhook covers the drift that looks fine in
// the forge's settings list and delivers nothing.
func TestOnboard_RepairsADeactivatedWebhook(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutProtection("main", true, DefaultReviewStatusContext)
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("first Onboard: %v", err)
	}
	gitea.DeactivateHooks()

	if report := onboarder.Check(context.Background(), testTenant, binding); report.Conformant {
		t.Fatal("a deactivated delivery read as conformant")
	} else if len(report.FindingsOfClass(ConformanceWebhookInactive)) != 1 {
		t.Fatalf("findings = %+v, want one webhook_inactive", report.Findings)
	}

	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("repair Onboard: %v", err)
	}
	if report := onboarder.Check(context.Background(), testTenant, binding); !report.Conformant {
		t.Fatalf("the delivery was not re-activated: %+v", report.Findings)
	}
}

// TestOnboard_LeavesAnOperatorsOwnLabelExactlyAsItIs is the non-destruction
// property at the onboarding level.
func TestOnboard_LeavesAnOperatorsOwnLabelExactlyAsItIs(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	gitea.PutLabel("factory:blocked", "ff00ff", "we use this for something else too")

	if _, err := onboarder.Onboard(context.Background(), testTenant, onboardTestBinding()); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	held, ok := gitea.Label("factory:blocked")
	if !ok {
		t.Fatal("onboarding destroyed a label")
	}
	if held.Color != "ff00ff" || held.Description != "we use this for something else too" {
		t.Errorf("label = %+v, want the operator's colour and description untouched", held)
	}
}

// TestOnboard_ProvisionsNoWebhookForAPollOnlyProject: a binding that names no
// webhook secret is poll-only by its own declaration, and provisioning a
// delivery it could never verify would be a permanently failing ingress.
func TestOnboard_ProvisionsNoWebhookForAPollOnlyProject(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	binding.Forge.WebhookSecretRef = ""
	gitea.PutProtection("main", true, DefaultReviewStatusContext)

	report, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if report.WebhookEnsured || report.WebhookURL != "" {
		t.Errorf("report = %+v, want no webhook provisioned", report)
	}
	if got := gitea.HookURLs(); len(got) != 0 {
		t.Errorf("hooks = %v, want none", got)
	}
	if len(report.LabelsCreated) == 0 {
		t.Error("a poll-only project still needs its label vocabulary")
	}
	if !report.Conformance.Conformant {
		t.Errorf("a correctly configured poll-only project reported non-conformant: %+v", report.Conformance.Findings)
	}
}

// TestOnboard_RefusesBadInput is the error half of the write path.
func TestOnboard_RefusesBadInput(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)

	cases := map[string]struct {
		tenant  string
		binding *ProjectBinding
	}{
		"no tenant":  {"", onboardTestBinding()},
		"no binding": {testTenant, nil},
		"binding that does not validate": {testTenant, func() *ProjectBinding {
			b := onboardTestBinding()
			b.Forge.TriggerLabel = ""
			return b
		}()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := onboarder.Onboard(context.Background(), tc.tenant, tc.binding); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
	if wrote := gitea.Calls("POST /labels") + gitea.Calls("POST /hooks"); wrote != 0 {
		t.Errorf("refused calls still wrote %d times to the forge", wrote)
	}
}

// TestOnboard_RefusesAnEmptyWebhookSecret: an unsigned delivery cannot be
// verified and every one would be discarded, so provisioning one is worse
// than provisioning none.
func TestOnboard_RefusesAnEmptyWebhookSecret(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	onboarder, err := NewOnboarder(&giteaForges{backend: backend}, &staticSecrets{}, onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}

	_, err = onboarder.Onboard(context.Background(), testTenant, onboardTestBinding())
	if !errors.Is(err, ErrWebhookSecretMissing) {
		t.Fatalf("error = %v, want ErrWebhookSecretMissing", err)
	}
	if got := gitea.HookURLs(); len(got) != 0 {
		t.Errorf("hooks = %v, want none", got)
	}
}

// newPollOnlyOnboarder is an onboarder on a node with no webhook public URL,
// which takes no webhook deliveries (go-quicbaas#2225).
func newPollOnlyOnboarder(t *testing.T, secrets WebhookSecretResolver) (*Onboarder, *fakeForge) {
	t.Helper()
	forge := newFakeForge()
	onboarder, err := NewOnboarder(&fakeForges{forge: forge}, secrets, "")
	if err != nil {
		t.Fatalf("NewOnboarder with no public URL: %v", err)
	}
	return onboarder, forge
}

// TestNewOnboarder_AcceptsNoPublicURL proves a node with no public URL can
// onboard at all: empty means poll-only, not misconfigured.
func TestNewOnboarder_AcceptsNoPublicURL(t *testing.T) {
	for name, base := range map[string]string{"absent": "", "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			onboarder, err := NewOnboarder(&giteaForges{}, onboardingSecrets(), base)
			if err != nil {
				t.Fatalf("NewOnboarder(%q): %v", base, err)
			}
			if onboarder == nil {
				t.Fatal("NewOnboarder returned no onboarder")
			}
		})
	}
}

// TestOnboard_APollOnlyBindingNeedsNoPublicURL proves a binding that names no
// webhook secret onboards on a node with no public URL and provisions nothing.
func TestOnboard_APollOnlyBindingNeedsNoPublicURL(t *testing.T) {
	onboarder, forge := newPollOnlyOnboarder(t, onboardingSecrets())
	binding := testBinding()
	binding.Forge.WebhookSecretRef = ""

	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if forge.ensureWebhookSeen != 0 || len(forge.webhooks) != 0 {
		t.Errorf("EnsureWebhook calls = %d, hooks = %+v, want none", forge.ensureWebhookSeen, forge.webhooks)
	}
	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckWebhook); got != StatusNotApplicable {
		t.Errorf("webhook check = %q, want not applicable", got)
	}
}

// TestOnboard_AWebhookBindingIsRefusedWithNoPublicURL proves a binding that
// asks for deliveries is refused on a node that takes none, before its secret
// is resolved and without touching the forge's webhooks: nothing is
// provisioned into the void and reported provisioned.
func TestOnboard_AWebhookBindingIsRefusedWithNoPublicURL(t *testing.T) {
	// No secrets at all: reaching the resolver would answer
	// ErrWebhookSecretMissing instead.
	onboarder, forge := newPollOnlyOnboarder(t, &staticSecrets{})
	binding := testBinding()

	_, err := onboarder.Onboard(context.Background(), testTenant, binding)
	var missing *WebhookPublicURLMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want WebhookPublicURLMissingError", err)
	}
	if missing.ProjectID != binding.ProjectID {
		t.Errorf("ProjectID = %q, want %q", missing.ProjectID, binding.ProjectID)
	}
	if !errors.Is(err, ErrWebhookPublicURLMissing) {
		t.Errorf("error = %v, want ErrWebhookPublicURLMissing", err)
	}
	if !strings.Contains(err.Error(), "sdlc.webhook.public_url") {
		t.Errorf("error %q does not name the setting", err)
	}
	if forge.ensureWebhookSeen != 0 {
		t.Errorf("EnsureWebhook calls = %d, want none", forge.ensureWebhookSeen)
	}
}

// TestCheck_AWebhookBindingWithNoPublicURLIsAFinding proves conformance does
// not call a binding healthy when it asks for deliveries this node cannot take.
func TestCheck_AWebhookBindingWithNoPublicURLIsAFinding(t *testing.T) {
	onboarder, forge := newPollOnlyOnboarder(t, onboardingSecrets())
	forge.caps = ForgeCapabilities{BranchProtectionContexts: true}

	report := onboarder.Check(context.Background(), testTenant, testBinding())
	if got := statusOf(report, CheckWebhook); got != StatusFail {
		t.Fatalf("webhook check = %q, want fail", got)
	}
	if report.Conformant {
		t.Error("the project was reported conformant")
	}
	findings := report.FindingsOfClass(ConformanceWebhookMissing)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one webhook_missing", report.Findings)
	}
	if !strings.Contains(findings[0].Remedy, "sdlc.webhook.public_url") {
		t.Errorf("remedy %q does not name the setting", findings[0].Remedy)
	}
}

// TestOnboard_SurfacesAForgeFailure: a provisioning pass that could not write
// must fail loudly, because the alternative is a project the operator
// believes is onboarded.
func TestOnboard_SurfacesAForgeFailure(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	gitea.FailOnce("GET /labels")

	if _, err := onboarder.Onboard(context.Background(), testTenant, onboardTestBinding()); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// =============================================================================
// Offboarding
// =============================================================================

// TestOffboard_RemovesTheDeliveryAndKeepsTheLabels.
//
// The asymmetry is deliberate. A webhook left behind delivers to a node that
// no longer drives the project — a live wrong behaviour. Labels left behind
// are a stale display, and deleting them would rewrite the history of every
// pull request that ever carried one.
func TestOffboard_RemovesTheDeliveryAndKeepsTheLabels(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	if err := onboarder.Offboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Offboard: %v", err)
	}
	if got := gitea.HookURLs(); len(got) != 0 {
		t.Errorf("hooks = %v, want none after offboarding", got)
	}
	if got := gitea.LabelNames(); len(got) != 6 {
		t.Errorf("labels = %v, want all six kept; offboarding destroyed issue history", got)
	}
}

// TestOffboard_IsSafeToRepeat covers unbinding a project that was never
// onboarded.
func TestOffboard_IsSafeToRepeat(t *testing.T) {
	onboarder, _ := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()

	for round := 1; round <= 2; round++ {
		if err := onboarder.Offboard(context.Background(), testTenant, binding); err != nil {
			t.Fatalf("round %d: Offboard: %v", round, err)
		}
	}
}

// TestOffboard_RefusesBadInput is the error half.
func TestOffboard_RefusesBadInput(t *testing.T) {
	onboarder, _ := newOnboarderAgainstGitea(t)

	if err := onboarder.Offboard(context.Background(), "", onboardTestBinding()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no tenant: error = %v, want ErrInvalidInput", err)
	}
	if err := onboarder.Offboard(context.Background(), testTenant, nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no binding: error = %v, want ErrInvalidInput", err)
	}
}

// =============================================================================
// Conformance
// =============================================================================

// classesIn collects a report's finding classes, sorted, for comparison.
func classesIn(report *ConformanceReport) []string {
	classes := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		classes = append(classes, string(finding.Class))
	}
	sort.Strings(classes)
	return classes
}

// statusOf returns one check's verdict in a report.
func statusOf(report *ConformanceReport, check string) ConformanceStatus {
	for _, result := range report.Checks {
		if result.Name == check {
			return result.Status
		}
	}
	return ""
}

// TestCheck_ReportsACorrectlyOnboardedProjectClean is the true-negative: a
// conformance verb that finds fault everywhere gets ignored, which costs more
// than having no check at all.
func TestCheck_ReportsACorrectlyOnboardedProjectClean(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutProtection("main", true, DefaultReviewStatusContext)
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	report := onboarder.Check(context.Background(), testTenant, binding)
	if !report.Conformant {
		t.Fatalf("findings = %+v, want none", report.Findings)
	}
	for _, check := range []string{CheckBinding, CheckWebhook, CheckLabels, CheckWorkflows, CheckProtection} {
		if got := statusOf(report, check); got != StatusPass {
			t.Errorf("check %q = %q, want pass", check, got)
		}
	}
	if report.Repo != testRepo.String() || report.ProjectID != testProject {
		t.Errorf("report names %s/%s, want %s/%s", report.ProjectID, report.Repo, testProject, testRepo)
	}
}

// TestCheck_ReportsEachFailureClassDistinctly is the reason this verb exists.
//
// "Not conformant" is useless. Each of these is repaired differently — a
// missing webhook is an Onboard call, a stale workflow is a commit, a missing
// protection context is a setting only the repository's owner may change — so
// a report that merged them would tell an operator nothing they could act on.
func TestCheck_ReportsEachFailureClassDistinctly(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()

	// Nothing provisioned, one label present, a workflow that should be gone,
	// and a protected branch that does not require the module's own context.
	gitea.PutLabel("task:ready", "1d76db", "")
	gitea.PutDir(".github/workflows", "ci.yml", "automerge.yml", "opencode-review.yml")
	gitea.PutProtection("main", true, "CI / CI Gate (pull_request)")

	report := onboarder.Check(context.Background(), testTenant, binding)
	if report.Conformant {
		t.Fatal("a repository in this state reported conformant")
	}

	want := []string{
		"label_missing", "label_missing", "label_missing", "label_missing", "label_missing",
		"protection_context_missing",
		"stale_workflow", "stale_workflow",
		"webhook_missing",
	}
	if got := classesIn(report); !reflect.DeepEqual(got, want) {
		t.Fatalf("classes = %v, want %v", got, want)
	}

	// The label the repository DOES define must not be reported, and ci.yml
	// must not be: the check names the files the cutover deleted, not every
	// workflow.
	for _, finding := range report.Findings {
		if finding.Subject == "task:ready" {
			t.Error("a label the repository defines was reported missing")
		}
		if finding.Subject == ".github/workflows/ci.yml" {
			t.Error("an ordinary CI workflow was reported as orchestration drift")
		}
	}
	for _, finding := range report.Findings {
		if finding.Remedy == "" {
			t.Errorf("finding %+v carries no remedy; the reader is left to guess at the repair", finding)
		}
		if finding.Check == "" {
			t.Errorf("finding %+v names no check", finding)
		}
	}
}

// TestCheck_ReportsAnUnprotectedBranchSeparatelyFromAMissingContext: the two
// are different repairs — one creates a rule, the other edits one — and a
// single class would send an operator to the wrong screen.
func TestCheck_ReportsAnUnprotectedBranchSeparatelyFromAMissingContext(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	unprotected := onboarder.Check(context.Background(), testTenant, binding)
	if len(unprotected.FindingsOfClass(ConformanceProtectionMissing)) != 1 {
		t.Fatalf("findings = %+v, want one protection_missing", unprotected.Findings)
	}

	gitea.PutProtection("main", true, "something-else")
	partial := onboarder.Check(context.Background(), testTenant, binding)
	if len(partial.FindingsOfClass(ConformanceProtectionMissing)) != 0 {
		t.Error("a protected branch was still reported unprotected")
	}
	if len(partial.FindingsOfClass(ConformanceProtectionContextMissing)) != 1 {
		t.Fatalf("findings = %+v, want one protection_context_missing", partial.Findings)
	}
}

// TestCheck_ReportsARuleThatListsAContextItDoesNotEnforce: protection with
// status checks switched off looks configured and gates nothing.
func TestCheck_ReportsARuleThatListsAContextItDoesNotEnforce(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	gitea.PutProtection("main", false, DefaultReviewStatusContext)

	report := onboarder.Check(context.Background(), testTenant, binding)
	if len(report.FindingsOfClass(ConformanceProtectionContextMissing)) != 1 {
		t.Fatalf("findings = %+v, want one protection_context_missing", report.Findings)
	}
}

// TestCheck_ReportsADeliveryPointingAtAnotherNode covers the state a restored
// backup or a migrated deployment leaves behind, where two nodes each believe
// they drive the repository.
func TestCheck_ReportsADeliveryPointingAtAnotherNode(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	gitea.PutHook("https://old-node.test"+WebhookPathPrefix+testProject, true, "issues")
	// Another PROJECT's delivery on the same repository is not drift.
	gitea.PutHook("https://old-node.test"+WebhookPathPrefix+"other-project", true, "issues")

	report := onboarder.Check(context.Background(), testTenant, binding)
	misdirected := report.FindingsOfClass(ConformanceWebhookMisdirected)
	if len(misdirected) != 1 {
		t.Fatalf("findings = %+v, want exactly one webhook_misdirected", report.Findings)
	}
	if !strings.Contains(misdirected[0].Subject, "old-node.test") {
		t.Errorf("subject = %q, want the stray node's URL", misdirected[0].Subject)
	}
}

// TestCheck_ReportsADeliverySubscribedToTooLittle: the webhook exists,
// deliveries arrive, nothing looks broken, and the events that would have
// prompted a reconcile never come.
func TestCheck_ReportsADeliverySubscribedToTooLittle(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutHook(onboardingHookBase+WebhookPathPrefix+testProject, true, "issues")

	report := onboarder.Check(context.Background(), testTenant, binding)
	missing := report.FindingsOfClass(ConformanceWebhookEventsMissing)
	if len(missing) != 1 {
		t.Fatalf("findings = %+v, want one webhook_events_missing", report.Findings)
	}
	if !strings.Contains(missing[0].Detail, "pull_request") {
		t.Errorf("detail = %q, want it to name the missing events", missing[0].Detail)
	}
}

// TestCheck_ReportsAnInvalidBindingAndStops: a binding that does not validate
// puts the repository, the vocabulary and the token all in question, so
// reporting the consequences would bury the cause.
func TestCheck_ReportsAnInvalidBindingAndStops(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	binding.Strategy = "no-such-strategy"

	report := onboarder.Check(context.Background(), testTenant, binding)
	if report.Conformant {
		t.Fatal("an invalid binding reported conformant")
	}
	if got := classesIn(report); !reflect.DeepEqual(got, []string{"binding_invalid"}) {
		t.Fatalf("classes = %v, want only binding_invalid", got)
	}
	if reads := gitea.Calls("GET /labels") + gitea.Calls("GET /hooks"); reads != 0 {
		t.Errorf("the forge was read %d times for a binding that cannot be checked", reads)
	}
}

// TestCheck_ReportsAMissingBinding: an unbound project is a real answer the
// dashboard has to render, not an error page.
func TestCheck_ReportsAMissingBinding(t *testing.T) {
	onboarder, _ := newOnboarderAgainstGitea(t)

	report := onboarder.Check(context.Background(), testTenant, nil)
	if report.Conformant {
		t.Fatal("an unbound project reported conformant")
	}
	if got := classesIn(report); !reflect.DeepEqual(got, []string{"binding_invalid"}) {
		t.Fatalf("classes = %v, want binding_invalid", got)
	}
}

// TestCheck_AnUnreadableForgeIsNotConformant is the fail-closed property.
//
// A pass whose reads failed knows nothing about the repository. Reporting that
// as conformant is how a fleet dashboard turns an outage into a clean bill of
// health — the exact failure this whole verb exists to prevent, one level up.
func TestCheck_AnUnreadableForgeIsNotConformant(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	gitea.FailOnce("GET /hooks")

	report := onboarder.Check(context.Background(), testTenant, onboardTestBinding())
	if report.Conformant {
		t.Fatal("a pass that could not read the forge reported conformant")
	}
	if len(report.FindingsOfClass(ConformanceCheckFailed)) == 0 {
		t.Fatalf("findings = %+v, want a check_failed", report.Findings)
	}
	if got := statusOf(report, CheckWebhook); got != StatusError {
		t.Errorf("webhook check = %q, want error", got)
	}
}

// TestCheck_AnUnresolvableForgeAdapterIsOneRedRow: a dashboard asking about
// twenty-eight projects must show one unreachable forge as one red row, not
// as a failed page.
func TestCheck_AnUnresolvableForgeAdapterIsOneRedRow(t *testing.T) {
	onboarder, err := NewOnboarder(
		&giteaForges{err: &BackendError{Op: "ForgeFor", Cause: errors.New("token secret is absent")}},
		onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}

	report := onboarder.Check(context.Background(), testTenant, onboardTestBinding())
	if report.Conformant {
		t.Fatal("a project whose adapter could not be built reported conformant")
	}
	if len(report.FindingsOfClass(ConformanceCheckFailed)) != 1 {
		t.Fatalf("findings = %+v, want one check_failed", report.Findings)
	}
}

// TestCheck_ReportsAnUnsupportedCheckRatherThanAFinding: a platform that
// cannot express required status contexts has nothing to fix, and reporting a
// finding would send somebody looking for a setting that does not exist.
func TestCheck_ReportsAnUnsupportedCheckRatherThanAFinding(t *testing.T) {
	forge := newFakeForge()
	forge.caps = ForgeCapabilities{} // no BranchProtectionContexts
	onboarder, err := NewOnboarder(&fakeForges{forge: forge}, onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}
	binding := testBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckProtection); got != StatusUnsupported {
		t.Fatalf("protection check = %q, want unsupported", got)
	}
	if report.Conformant != true {
		t.Errorf("an unsupported check made the project non-conformant: %+v", report.Findings)
	}
	if len(report.FindingsOfClass(ConformanceCheckUnsupported)) != 1 {
		t.Errorf("findings = %+v, want one check_unsupported", report.Findings)
	}
}

// TestCheck_APollOnlyProjectHasNoWebhookToBeMissing: poll-only is a
// configuration, not a degraded mode.
func TestCheck_APollOnlyProjectHasNoWebhookToBeMissing(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	binding.Forge.WebhookSecretRef = ""
	gitea.PutProtection("main", true, DefaultReviewStatusContext)
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckWebhook); got != StatusNotApplicable {
		t.Fatalf("webhook check = %q, want not_applicable", got)
	}
	if !report.Conformant {
		t.Errorf("a poll-only project reported non-conformant: %+v", report.Findings)
	}
}

// TestCheck_ReportsFindingsInAStableOrder: a report whose order changed
// between runs would make a diff of two reports unreadable, which is how a
// drift dashboard is actually used.
func TestCheck_ReportsFindingsInAStableOrder(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutDir(".github/workflows", "automerge.yml", "task-worker.yml")

	first := classesIn(onboarder.Check(context.Background(), testTenant, binding))
	for round := 0; round < 5; round++ {
		if got := classesIn(onboarder.Check(context.Background(), testTenant, binding)); !reflect.DeepEqual(got, first) {
			t.Fatalf("round %d: classes = %v, want %v", round, got, first)
		}
	}
}

// TestCheck_ChangesNothing is the read-only contract. An operator asking what
// state a repository is in must be able to ask without changing it.
func TestCheck_ChangesNothing(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()

	onboarder.Check(context.Background(), testTenant, binding)

	writes := gitea.Calls("POST /labels") + gitea.Calls("POST /hooks") +
		gitea.Calls("PATCH /hooks/1") + gitea.Calls("DELETE /hooks/1")
	if writes != 0 {
		t.Fatalf("a read-only conformance pass wrote to the forge %d times", writes)
	}
	if len(gitea.LabelNames()) != 0 || len(gitea.HookURLs()) != 0 {
		t.Fatal("a read-only conformance pass provisioned the repository")
	}
}

// TestCheck_ReportsALabelListingThatFailedDistinctlyFromAMissingLabel.
//
// "The repository does not define factory:blocked" and "the repository's
// labels could not be read" lead to different actions, and a pass that merged
// them would send an operator to create labels on a forge that is down.
func TestCheck_ReportsALabelListingThatFailedDistinctlyFromAMissingLabel(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	// The webhook check reads hooks first; let that pass and fail the labels.
	gitea.PutHook(onboardingHookBase+WebhookPathPrefix+testProject, true, "issues", "pull_request", "status")
	gitea.FailOnce("GET /labels")

	report := onboarder.Check(context.Background(), testTenant, binding)
	if report.Conformant {
		t.Fatal("a pass whose label read failed reported conformant")
	}
	if got := statusOf(report, CheckLabels); got != StatusError {
		t.Fatalf("labels check = %q, want error", got)
	}
	if len(report.FindingsOfClass(ConformanceLabelMissing)) != 0 {
		t.Error("a failed read was reported as missing labels")
	}
	if len(report.FindingsOfClass(ConformanceCheckFailed)) != 1 {
		t.Fatalf("findings = %+v, want one check_failed", report.Findings)
	}
}

// TestCheck_ReportsAWorkflowListingThatFailed is the same distinction on the
// stale-workflow check. "The directory is not there" is the desired state; a
// forge that could not be read is not.
func TestCheck_ReportsAWorkflowListingThatFailed(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutDir(".github/workflows", "ci.yml")
	gitea.FailOnce("GET /contents/.github/workflows")

	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckWorkflows); got != StatusError {
		t.Fatalf("workflows check = %q, want error", got)
	}
	if len(report.FindingsOfClass(ConformanceStaleWorkflow)) != 0 {
		t.Error("a failed read was reported as stale orchestration")
	}
}

// TestCheck_ReportsAProtectionReadThatFailed.
func TestCheck_ReportsAProtectionReadThatFailed(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	gitea.PutProtection("main", true, DefaultReviewStatusContext)
	gitea.FailOnce("GET /branch_protections/main")

	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckProtection); got != StatusError {
		t.Fatalf("protection check = %q, want error", got)
	}
	if len(report.FindingsOfClass(ConformanceProtectionMissing)) != 0 {
		t.Error("a failed read was reported as an unprotected branch")
	}
}

// TestCheck_AnAdapterThatContradictsItsCapabilityIsReportedUnsupported.
//
// The capability says the platform CAN require status contexts and the
// adapter answers ErrForgeUnsupported: a wiring disagreement rather than an
// outage. Reporting it as unsupported keeps it out of the failure count while
// still putting it in the report, which is how the disagreement gets noticed.
func TestCheck_AnAdapterThatContradictsItsCapabilityIsReportedUnsupported(t *testing.T) {
	forge := newFakeForge()
	forge.caps = ForgeCapabilities{BranchProtectionContexts: true}
	forge.protectionErr = &ForgeUnsupportedError{Type: ForgeGitea, Capability: "branch protection contexts"}
	onboarder, err := NewOnboarder(&fakeForges{forge: forge}, onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}
	binding := testBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}

	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckProtection); got != StatusUnsupported {
		t.Fatalf("protection check = %q, want unsupported", got)
	}
	if !report.Conformant {
		t.Errorf("an unsupported check made the project non-conformant: %+v", report.Findings)
	}
}

// TestCheck_IgnoresAWebhookWhoseURLCannotBeParsed: a forge may hold a
// delivery somebody typed by hand, and it must not be mistaken for one of
// this project's.
func TestCheck_IgnoresAWebhookWhoseURLCannotBeParsed(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	gitea.PutHook("://not a url", true, "issues")
	gitea.PutProtection("main", true, DefaultReviewStatusContext)

	report := onboarder.Check(context.Background(), testTenant, binding)
	if len(report.FindingsOfClass(ConformanceWebhookMisdirected)) != 0 {
		t.Errorf("an unparseable delivery was reported as this project's: %+v", report.Findings)
	}
	if !report.Conformant {
		t.Errorf("findings = %+v, want none", report.Findings)
	}
}

// TestOffboard_SurfacesAForgeFailure: an unbind whose delivery could not be
// removed must say so, because the caller decides whether to retry.
func TestOffboard_SurfacesAForgeFailure(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	if _, err := onboarder.Onboard(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	gitea.FailOnce("GET /hooks")

	if err := onboarder.Offboard(context.Background(), testTenant, binding); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// TestOffboard_SurfacesAnUnresolvableAdapter covers the other early exit.
func TestOffboard_SurfacesAnUnresolvableAdapter(t *testing.T) {
	onboarder, err := NewOnboarder(
		&giteaForges{err: &BackendError{Op: "ForgeFor", Cause: errors.New("token absent")}},
		onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}

	if err := onboarder.Offboard(context.Background(), testTenant, onboardTestBinding()); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// TestOnboard_SurfacesASecretsFailure: a webhook secret that cannot be read
// is not an empty secret, and provisioning must not continue on either.
func TestOnboard_SurfacesASecretsFailure(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	onboarder, err := NewOnboarder(
		&giteaForges{backend: backend},
		&staticSecrets{err: errors.New("barrier sealed")}, onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}

	if _, err := onboarder.Onboard(context.Background(), testTenant, onboardTestBinding()); err == nil {
		t.Fatal("a secrets failure was swallowed")
	}
	if got := gitea.HookURLs(); len(got) != 0 {
		t.Errorf("hooks = %v, want none", got)
	}
}

// TestOnboard_ProvisionsNothingForABindingWithNoVocabulary: a binding may
// declare no display labels at all, and the label pass must then be a no-op
// rather than a write of the empty set.
func TestOnboard_ProvisionsNothingForABindingWithNoVocabulary(t *testing.T) {
	onboarder, gitea := newOnboarderAgainstGitea(t)
	binding := onboardTestBinding()
	binding.Forge.Labels = nil
	gitea.PutLabel(binding.Forge.TriggerLabel, "1d76db", "")
	gitea.PutProtection("main", true, DefaultReviewStatusContext)

	report, err := onboarder.Onboard(context.Background(), testTenant, binding)
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if len(report.LabelsCreated) != 0 {
		t.Errorf("LabelsCreated = %v, want none", report.LabelsCreated)
	}
	if wrote := gitea.Calls("POST /labels"); wrote != 0 {
		t.Errorf("wrote %d labels for a vocabulary already present", wrote)
	}
	if !report.Conformance.Conformant {
		t.Errorf("findings = %+v, want none", report.Conformance.Findings)
	}
}

// TestCheck_ABindingThatDeclaresNoLabelsHasNoVocabularyToBeMissing.
//
// It is not conceivable today — Validate requires a trigger label — but the
// check has to answer "not applicable" rather than "pass" if it ever is, so a
// dashboard cannot show a vacuous green.
func TestCheck_ABindingThatDeclaresNoLabelsHasNoVocabularyToBeMissing(t *testing.T) {
	forge := newFakeForge()
	forge.caps = ForgeCapabilities{BranchProtectionContexts: true}
	forge.protection = map[string]*ForgeBranchProtection{
		"main": {Branch: "main", RequireStatusChecks: true, RequiredContexts: []string{DefaultReviewStatusContext}},
	}
	onboarder, err := NewOnboarder(&fakeForges{forge: forge}, onboardingSecrets(), onboardingHookBase)
	if err != nil {
		t.Fatalf("NewOnboarder: %v", err)
	}
	binding := testBinding()
	binding.Forge.Labels = nil
	binding.Forge.WebhookSecretRef = ""
	// Reach past Validate deliberately: the record is what Check reads, and
	// the check's own behaviour on an empty vocabulary is what is under test.
	binding.Forge.TriggerLabel = "go"
	report := onboarder.Check(context.Background(), testTenant, binding)
	if got := statusOf(report, CheckLabels); got == StatusNotApplicable {
		t.Fatal("a binding with a trigger label reported no vocabulary")
	}

	// Now with nothing at all in the vocabulary, which LabelSpecs returns
	// empty for.
	if specs := (OnboardingConfig{}).LabelSpecs(ForgeConfig{}); len(specs) != 0 {
		t.Fatalf("specs = %+v, want none", specs)
	}
}

// =============================================================================
// The origin the webhook route is appended to
// =============================================================================

// TestWebhookURL_IsRefusedRatherThanBuiltWrong is the reason the accepted
// shape is narrower than "a URL".
//
// WebhookURL concatenates. An origin carrying a query or a fragment therefore
// produces a delivery URL whose PATH is "/" and whose route is buried in the
// part a forge does not route on — and nothing downstream notices: the hook is
// provisioned, EnsureWebhook reads it back and compares the same malformed
// string, and conformance reports the repository healthy while every delivery
// lands on the wrong endpoint. Across a fleet that is every repository at once,
// so the refusal is at construction, where one operator reads one message.
func TestWebhookURL_IsRefusedRatherThanBuiltWrong(t *testing.T) {
	for name, tc := range map[string]struct {
		base    string
		wantErr bool
		// want is the delivery URL a valid origin must produce.
		want string
	}{
		"query":          {base: "https://node.test?x=1", wantErr: true},
		"bare query":     {base: "https://node.test?", wantErr: true},
		"fragment":       {base: "https://node.test#frag", wantErr: true},
		"userinfo":       {base: "https://u:p@node.test", wantErr: true},
		"plain":          {base: "https://node.test", want: "https://node.test" + WebhookPathPrefix + "p1"},
		"trailing slash": {base: "https://node.test/", want: "https://node.test" + WebhookPathPrefix + "p1"},
		"behind a proxy": {base: "https://node.test/quicbaas", want: "https://node.test/quicbaas" + WebhookPathPrefix + "p1"},
	} {
		t.Run(name, func(t *testing.T) {
			onboarder, err := NewOnboarder(&giteaForges{}, onboardingSecrets(), tc.base)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("origin %q was accepted; it would have provisioned %q",
						tc.base, tc.base+WebhookPathPrefix+"p1")
				}
				if !errors.Is(err, ErrInvalidInput) {
					t.Errorf("error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("origin %q was refused: %v", tc.base, err)
			}
			if got := onboarder.WebhookURL("p1"); got != tc.want {
				t.Errorf("WebhookURL = %q, want %q", got, tc.want)
			}
			// And what it produced really does address the webhook route,
			// rather than merely looking like it does.
			parsed, parseErr := url.Parse(onboarder.WebhookURL("p1"))
			if parseErr != nil {
				t.Fatalf("the delivery URL does not parse: %v", parseErr)
			}
			if !strings.HasSuffix(parsed.Path, WebhookPathPrefix+"p1") {
				t.Errorf("the route is not in the PATH: path=%q query=%q fragment=%q",
					parsed.Path, parsed.RawQuery, parsed.Fragment)
			}
		})
	}
}

// TestNormalizeWebhookPublicURL_TrimsRatherThanRewrites proves the accepted
// value is returned as the caller gave it, minus trailing slashes: an origin
// silently rewritten is an origin the operator cannot recognise in a forge's
// webhook list.
func TestNormalizeWebhookPublicURL_TrimsRatherThanRewrites(t *testing.T) {
	got, err := NormalizeWebhookPublicURL("  https://node.test/quicbaas//  ")
	if err != nil {
		t.Fatalf("a usable origin was refused: %v", err)
	}
	if want := "https://node.test/quicbaas"; got != want {
		t.Errorf("normalized = %q, want %q", got, want)
	}
}

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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// =============================================================================
// Binding a project onboards its repository
// =============================================================================
//
// The gap these close is the one the whole feature is about: EnsureWebhook was
// implemented, read-back-verified and CALLED BY NOTHING, and every managed
// repository's labels were created by hand. A binding that does not provision
// is a project the operator believes is driven and that never receives a
// delivery.

// expectedWebhookURL is where the rig's node expects deliveries for the
// fixture project.
const expectedWebhookURL = sdlcTestPublicURL + sdlc.WebhookPathPrefix + sdlcTestProject

// TestSDLCService_BindProvisionsTheRepository is the headline.
func TestSDLCService_BindProvisionsTheRepository(t *testing.T) {
	rig := newSDLCServiceRig(t)

	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	hooks := rig.forge.HookURLs()
	if len(hooks) != 1 || hooks[0] != expectedWebhookURL {
		t.Fatalf("hooks = %v, want [%s]", hooks, expectedWebhookURL)
	}
	// The fixture binding declares only a trigger label, so that is the
	// whole vocabulary onboarding has to create.
	if names := rig.forge.LabelNames(); len(names) != 1 || names[0] != "task:ready" {
		t.Fatalf("labels = %v, want [task:ready]", names)
	}
}

// TestSDLCService_BindIsIdempotent: re-binding a project is how an operator
// changes its policy, and it must not accumulate deliveries on the forge.
func TestSDLCService_BindIsIdempotent(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()

	for round := 1; round <= 2; round++ {
		if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
			t.Fatalf("round %d: Bind: %v", round, err)
		}
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want exactly one after two binds", hooks)
	}
	if created := rig.forge.Calls("POST /labels"); created != 1 {
		t.Errorf("created %d labels over two binds, want 1", created)
	}
}

// TestSDLCService_BindLeavesARecoverableStateWhenTheForgeRefuses.
//
// The order is load-bearing: the route index and the binding exist before the
// forge is told to deliver, so a provisioning failure leaves a project BOUND
// but not driven — which Conformance names exactly and Onboard repairs. The
// other order would leave a repository delivering to a node with no record of
// it, and nothing able to find the delivery to remove.
func TestSDLCService_BindLeavesARecoverableStateWhenTheForgeRefuses(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	rig.forge.FailOnce("GET /labels")

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); !errors.Is(err, sdlc.ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}

	// Bound, and honest about being unprovisioned.
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("the binding was not stored: %v", err)
	}
	report, err := rig.svc.Conformance(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("Conformance: %v", err)
	}
	if report.Conformant {
		t.Fatal("an unprovisioned project reported conformant")
	}
	if len(report.FindingsOfClass(sdlc.ConformanceWebhookMissing)) != 1 {
		t.Fatalf("findings = %+v, want a webhook_missing", report.Findings)
	}

	// And repairable by the explicit verb.
	if _, err := rig.svc.Onboard(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want the delivery provisioned by the repair", hooks)
	}
}

// =============================================================================
// Unbinding offboards
// =============================================================================

// TestSDLCService_UnbindRemovesTheDelivery: a webhook left behind delivers to
// a node that no longer drives the project.
func TestSDLCService_UnbindRemovesTheDelivery(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want none after unbinding", hooks)
	}
	// The labels stay: they carry the history of every pull request that
	// ever wore one.
	if names := rig.forge.LabelNames(); len(names) != 1 {
		t.Errorf("labels = %v, want the vocabulary kept", names)
	}
}

// TestSDLCService_UnbindOfAnUnboundProjectIsNotAnError keeps unbinding
// repeatable.
func TestSDLCService_UnbindOfAnUnboundProjectIsNotAnError(t *testing.T) {
	rig := newSDLCServiceRig(t)

	if err := rig.svc.Unbind(context.Background(), sdlcTestTenant, "never-bound"); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
}

// TestSDLCService_UnbindRefusesWhenTheDeliveryCannotBeRemoved.
//
// Deleting the binding first would leave a live delivery nothing can any
// longer identify the owner of: the route index has forgotten the project, so
// every delivery answers 401 for ever and no later unbind can find the hook.
// Refusing leaves a state the operator can retry.
func TestSDLCService_UnbindRefusesWhenTheDeliveryCannotBeRemoved(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	rig.forge.FailOnce("GET /hooks")

	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
	if _, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("the binding was removed despite the refusal: %v", err)
	}

	// Retryable, which is the point of refusing.
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Errorf("hooks = %v, want none", hooks)
	}
}

// =============================================================================
// Rebinding to different forge coordinates offboards the old repository
// =============================================================================
//
// Once the replacement is stored, the old repository and forge are recorded
// nowhere: Conformance and Unbind resolve only the new binding, so a webhook
// the old binding provisioned would deliver to this node, answered 401, for
// ever, with no verb able to find it.

// rebindTarget is a second repository, on a second forge, for project P to
// move to.
func rebindTarget(t *testing.T, rig *sdlcServiceRig) (*giteafake.Server, *sdlc.ProjectBinding) {
	t.Helper()
	other := giteafake.New(t, "acme", "web")
	binding := rig.binding()
	binding.Forge.BaseURL = other.APIURL()
	binding.Forge.Repo = "web"
	return other, binding
}

// TestSDLCService_RebindToAnotherRepositoryRemovesTheOldDelivery is the
// headline: P bound to A, rebound to B, and A holds no delivery.
func TestSDLCService_RebindToAnotherRepositoryRemovesTheOldDelivery(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind A: %v", err)
	}
	other, rebound := rebindTarget(t, rig)

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rebound); err != nil {
		t.Fatalf("Bind B: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Fatalf("repo A hooks = %v, want none after rebinding to B", hooks)
	}
	if hooks := other.HookURLs(); len(hooks) != 1 || hooks[0] != expectedWebhookURL {
		t.Fatalf("repo B hooks = %v, want [%s]", hooks, expectedWebhookURL)
	}
}

// TestSDLCService_RebindIsRefusedWhenTheOldDeliveryCannotBeRemoved: the old
// binding stays stored, and so stays the one record naming the hook, and a
// retry completes the rebind.
func TestSDLCService_RebindIsRefusedWhenTheOldDeliveryCannotBeRemoved(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind A: %v", err)
	}
	other, rebound := rebindTarget(t, rig)
	rig.forge.FailOnce("GET /hooks")

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rebound); !errors.Is(err, sdlc.ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
	got, err := rig.svc.Binding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("the old binding is not readable after the refusal: %v", err)
	}
	if got.Forge.Repo != "api" || got.Forge.BaseURL != rig.forge.APIURL() {
		t.Fatalf("stored binding = %+v, want repo A left in place", got.Forge)
	}
	if hooks := other.HookURLs(); len(hooks) != 0 {
		t.Fatalf("repo B hooks = %v, want nothing provisioned by a refused rebind", hooks)
	}

	// Retryable, which is the point of refusing.
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rebound); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Errorf("repo A hooks = %v, want none", hooks)
	}
	if hooks := other.HookURLs(); len(hooks) != 1 {
		t.Errorf("repo B hooks = %v, want one", hooks)
	}
}

// TestSDLCService_RebindToPollOnlyRemovesTheDelivery: a binding with no
// webhook secret provisions no webhook, so without an offboard the one it
// replaced keeps delivering.
func TestSDLCService_RebindToPollOnlyRemovesTheDelivery(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	pollOnly := rig.binding()
	pollOnly.Forge.WebhookSecretRef = ""

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, pollOnly); err != nil {
		t.Fatalf("rebind poll-only: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want none once the binding is poll-only", hooks)
	}
}

// TestSDLCService_RebindToTheSameRepositoryDoesNotOffboard: the replacement's
// onboarding reconciles the existing hook in place, so deleting it first
// would only open a window with no delivery.
func TestSDLCService_RebindToTheSameRepositoryDoesNotOffboard(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	replacement := rig.binding()
	replacement.Review.RequiredApprovals = 2

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, replacement); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if created := rig.forge.Calls("POST /hooks"); created != 1 {
		t.Errorf("created %d hooks over a same-repository rebind, want 1 (reconciled in place)", created)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want the one delivery kept", hooks)
	}
}

// =============================================================================
// Onboard and Conformance
// =============================================================================

// TestSDLCService_OnboardRepairsDrift: the explicit repair verb.
func TestSDLCService_OnboardRepairsDrift(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	rig.forge.DropHooks()

	report, err := rig.svc.Onboard(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if !report.WebhookEnsured {
		t.Error("the report does not claim the delivery was restored")
	}
	if len(report.LabelsCreated) != 0 {
		t.Errorf("LabelsCreated = %v, want none; only the webhook had drifted", report.LabelsCreated)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want the delivery back", hooks)
	}
}

// TestSDLCService_OnboardRefusesAnUnboundProject: there is nothing to
// provision from.
func TestSDLCService_OnboardRefusesAnUnboundProject(t *testing.T) {
	rig := newSDLCServiceRig(t)

	if _, err := rig.svc.Onboard(context.Background(), sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestSDLCService_OnboardAndConformanceAreTenantScoped is the bad-input half
// of both verbs.
func TestSDLCService_OnboardAndConformanceAreTenantScoped(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()

	if _, err := rig.svc.Onboard(ctx, "", sdlcTestProject); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Errorf("Onboard with no tenant: error = %v, want ErrInvalidInput", err)
	}
	if _, err := rig.svc.Conformance(ctx, "", sdlcTestProject); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Errorf("Conformance with no tenant: error = %v, want ErrInvalidInput", err)
	}
}

// TestSDLCService_ConformanceReportsAnUnboundProjectRatherThanFailing.
//
// "This project has no binding" is precisely the answer a fleet dashboard
// asking about twenty-eight projects needs rendered as a row.
func TestSDLCService_ConformanceReportsAnUnboundProjectRatherThanFailing(t *testing.T) {
	rig := newSDLCServiceRig(t)

	report, err := rig.svc.Conformance(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("Conformance: %v", err)
	}
	if report.Conformant {
		t.Fatal("an unbound project reported conformant")
	}
	if len(report.FindingsOfClass(sdlc.ConformanceBindingInvalid)) != 1 {
		t.Fatalf("findings = %+v, want one binding_invalid", report.Findings)
	}
	if report.ProjectID != sdlcTestProject {
		t.Errorf("report names project %q, want %q", report.ProjectID, sdlcTestProject)
	}
}

// TestSDLCService_ConformanceRefusesAProjectThatDoesNotExist: a question
// about the wrong subject is not an answer about this one.
func TestSDLCService_ConformanceRefusesAProjectThatDoesNotExist(t *testing.T) {
	rig := newSDLCServiceRig(t)

	if _, err := rig.svc.Conformance(context.Background(), sdlcTestTenant, "no-such-project"); err == nil {
		t.Fatal("a missing project answered with a report")
	}
}

// TestSDLCService_ConformanceChangesNothing is the read-only contract at the
// service boundary.
func TestSDLCService_ConformanceChangesNothing(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding()); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	rig.forge.DropHooks()
	labelWrites := rig.forge.Calls("POST /labels")

	report, err := rig.svc.Conformance(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("Conformance: %v", err)
	}
	if report.Conformant {
		t.Fatal("a repository with no delivery reported conformant")
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Errorf("a read-only report provisioned a delivery: %v", hooks)
	}
	if rig.forge.Calls("POST /labels") != labelWrites {
		t.Error("a read-only report wrote labels")
	}
}

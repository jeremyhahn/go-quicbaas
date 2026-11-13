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
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// Webhook ingest — a verified delivery nudges a poll
// =============================================================================
//
// A delivery is routed to its tenant, verified against the binding's secret
// through the forge adapter, parsed, checked against the bound repository,
// and then NUDGES the engine's task-source poller for that project. The
// poller is the one path an issue becomes a Task through; the webhook only
// makes it look sooner.

// WebhookRouter resolves the tenant a project's deliveries belong to.
type WebhookRouter interface {
	Tenant(ctx context.Context, projectID string) (string, error)
}

// WebhookSecretResolver reads the webhook HMAC secret a binding names.
type WebhookSecretResolver interface {
	WebhookSecret(ctx context.Context, tenantID, secretRef string) ([]byte, error)
}

// WebhookDelivery is one inbound delivery, as the transport read it.
//
// Header is the request's headers when the transport has them. Each forge
// names its event, delivery and signature headers differently, and which
// forge sent a delivery is known only once the project's binding is read, so
// an empty EventType, DeliveryID or Signature is filled from Header by the
// BOUND forge's names — never by guessing from which headers are present.
type WebhookDelivery struct {
	ProjectID  string
	EventType  string
	DeliveryID string
	Signature  string
	Payload    []byte
	Header     http.Header
}

// webhookHeaderNames are the headers one forge's deliveries arrive with.
type webhookHeaderNames struct {
	event     string
	delivery  string
	signature string
}

// forgeWebhookHeaders maps a forge to the headers its deliveries carry.
var forgeWebhookHeaders = map[ForgeType]webhookHeaderNames{
	ForgeGitea:  {event: GiteaEventHeader, delivery: GiteaDeliveryHeader, signature: GiteaSignatureHeader},
	ForgeGitHub: {event: GitHubEventHeader, delivery: GitHubDeliveryHeader, signature: GitHubSignatureHeader},
	ForgeGitLab: {event: GitLabEventHeader, delivery: GitLabDeliveryHeader, signature: GitLabSignatureHeader},
}

// fromHeaders fills the delivery's empty fields from its headers, by the
// names the given forge uses. A forge with no entry leaves it unchanged, and
// the validation that follows refuses what is still missing.
func (d WebhookDelivery) fromHeaders(forge ForgeType) WebhookDelivery {
	names, ok := forgeWebhookHeaders[forge]
	if !ok || d.Header == nil {
		return d
	}
	if d.EventType == "" {
		d.EventType = d.Header.Get(names.event)
	}
	if d.DeliveryID == "" {
		d.DeliveryID = d.Header.Get(names.delivery)
	}
	if d.Signature == "" {
		d.Signature = d.Header.Get(names.signature)
	}
	return d
}

// WebhookIngestResult is what a verified delivery led to.
type WebhookIngestResult struct {
	TenantID  string
	ProjectID string
	Event     *ForgeEvent

	// Ignored is the project's discovery mode when that mode does not admit
	// the delivery path (`poll`); empty when the delivery was acted on. An
	// ignored delivery was still verified and is NOT an error — the forge must
	// not retry it — but nothing was nudged, woken, readmitted or dispatched.
	Ignored DiscoveryMode

	// Nudged says the poller was asked to sweep the project.
	Nudged bool

	// Parked is the refusal a PARKED project gave the delivery's new work,
	// as an *orchestration.ProjectParkedError; nil while the project is
	// open. It does NOT fail the delivery: the forge must not retry it, and
	// nothing is queued to replay it — the first sweep after a resume reads
	// the forge afresh. No triage pass is started for it, while the parts of
	// a delivery that serve work already started (an owner's ruling, a
	// review) still act. Carried for the caller to log.
	Parked error

	// Triaged says a triage pass was started for the project, which happens
	// only for the issue deliveries that can change a verdict.
	Triaged bool

	// TriageErr is why a triage pass the delivery should have started was
	// not, as a *TriageWakeError; nil when triage was woken or had no reason
	// to be. It does NOT fail the delivery, so it is carried here for the
	// caller to log rather than returned.
	TriageErr error

	// ReviewStarted says a review job was started for the delivery's pull
	// request.
	ReviewStarted bool

	// RulingReadmitted says an issue comment was an owner's ruling on the
	// issue's parked task, and the task was readmitted.
	RulingReadmitted bool

	// RulingError is why an issue comment could not be examined as a
	// ruling. It does not fail the delivery: the unblock sweep reads the
	// same comments on its next pass. Carried for the caller to log.
	RulingError error
}

// RulingReadmitter readmits the parked task an issue's owner ruling clears.
// UnblockSweeper satisfies it.
type RulingReadmitter interface {
	ReadmitOnRuling(ctx context.Context, tenantID, projectID string, issue int) (bool, error)
}

var _ RulingReadmitter = (*UnblockSweeper)(nil)

// WebhookIngestor verifies deliveries and nudges the poller.
type WebhookIngestor struct {
	routes   WebhookRouter
	bindings BindingReader
	secrets  WebhookSecretResolver
	forges   ForgeResolver
	nudger   ProjectNudger
	reviews  ReviewTrigger

	// triage is woken for the deliveries that can change a triage verdict.
	// It is SET rather than constructed because the two nudges answer
	// different questions and fail independently: the poller's nudge asks
	// "has a TRIGGERED issue appeared", triage's asks "is any untriaged
	// issue now ready". A node with no triage runtime still ingests
	// deliveries and still feeds the poller.
	triage ProjectNudger

	// rulings readmits a parked task when a verified issue-comment delivery
	// is an owner's ruling on it. Set rather than constructed, for the same
	// reason as triage: a node without it still ingests every delivery, and
	// the unblock sweep still finds the ruling on its own pass.
	rulings RulingReadmitter
}

// NewWebhookIngestor wires the ingestor.
//
// Every collaborator is required EXCEPT reviews, and its absence is a real
// configuration rather than an unfinished one: a node that ingests
// deliveries but runs no reviewer — because no project on it binds one, or
// because reviewing is another node's job — passes nil and the review half
// of the dispatch does nothing. Requiring it would force every such
// deployment to supply a trigger it has nothing for.
func NewWebhookIngestor(routes WebhookRouter, bindings BindingReader, secrets WebhookSecretResolver, forges ForgeResolver, nudger ProjectNudger, reviews ReviewTrigger) (*WebhookIngestor, error) {
	switch {
	case routes == nil:
		return nil, &InvalidInputError{Field: "routes", Message: "a delivery names no tenant; the route index supplies it"}
	case bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the binding holds the secret reference and the repository"}
	case secrets == nil:
		return nil, &InvalidInputError{Field: "secrets", Message: "a signature cannot be verified without the secret"}
	case forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "the adapter verifies and parses the delivery"}
	case nudger == nil:
		return nil, &InvalidInputError{Field: "nudger", Message: "a verified delivery that nudges nothing changes nothing"}
	}
	return &WebhookIngestor{routes: routes, bindings: bindings, secrets: secrets, forges: forges, nudger: nudger, reviews: reviews}, nil
}

// SetTriageNudger gives the ingestor the triage scheduler to wake. A nil
// nudger leaves triage driven by its periodic pass alone, which is the
// correct behaviour on a node whose triage runtime is not mounted.
func (i *WebhookIngestor) SetTriageNudger(triage ProjectNudger) {
	i.triage = triage
}

// SetRulingReadmitter gives the ingestor the ruling path to call for a
// verified issue-comment delivery. nil leaves rulings to the unblock sweep.
func (i *WebhookIngestor) SetRulingReadmitter(rulings RulingReadmitter) {
	i.rulings = rulings
}

// Ingest verifies one delivery and nudges the poller for its project.
func (i *WebhookIngestor) Ingest(ctx context.Context, delivery WebhookDelivery) (*WebhookIngestResult, error) {
	// A delivery with headers still to read is checked in two steps: what
	// needs no forge now, the rest once the binding names the forge.
	early := validateDelivery
	if delivery.Header != nil {
		early = validateEnvelope
	}
	if err := early(delivery); err != nil {
		return nil, err
	}
	tenantID, err := i.routes.Tenant(ctx, delivery.ProjectID)
	if err != nil {
		return nil, err
	}
	binding, err := i.bindings.GetBinding(ctx, tenantID, delivery.ProjectID)
	if err != nil {
		return nil, err
	}
	delivery = delivery.fromHeaders(binding.Forge.Type)
	if err := validateDelivery(delivery); err != nil {
		return nil, err
	}
	if binding.Forge.WebhookSecretRef == "" {
		return nil, &WebhookSecretMissingError{ProjectID: binding.ProjectID, Reason: "the binding names no webhook secret"}
	}
	secret, err := i.secrets.WebhookSecret(ctx, tenantID, binding.Forge.WebhookSecretRef)
	if err != nil {
		return nil, backendError("WebhookSecret", err)
	}
	defer secure.Wipe(secret)
	if len(secret) == 0 {
		return nil, &WebhookSecretMissingError{
			ProjectID: binding.ProjectID,
			Reason:    "the secret named by " + binding.Forge.WebhookSecretRef + " resolved empty",
		}
	}
	forge, err := i.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	if forge == nil {
		return nil, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	if err := forge.VerifyWebhook(delivery.Payload, delivery.Signature, secret); err != nil {
		return nil, err
	}
	event, err := forge.ParseWebhook(delivery.EventType, delivery.Payload)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, &InvalidInputError{Field: "payload", Message: "the adapter parsed the delivery to no event at all"}
	}
	if err := matchRepo(binding, event.Repo); err != nil {
		return nil, err
	}
	result := &WebhookIngestResult{TenantID: tenantID, ProjectID: binding.ProjectID, Event: event}
	// THE MODE IS ASKED AFTER THE DELIVERY IS PROVEN, never before: an
	// unsigned or misrouted delivery is refused whatever the project's mode,
	// and only a genuine one is answered "ignored".
	if !binding.Discovery.Admits(DiscoveryPathDelivery) {
		result.Ignored = binding.Discovery.Effective()
		return result, nil
	}
	if event.Kind == EventUnsupported {
		return result, nil
	}
	switch err := i.nudger.NudgeProject(ctx, tenantID, binding.ProjectID); {
	case err == nil:
		result.Nudged = true
	case errors.Is(err, orchestration.ErrProjectParked):
		result.Parked = err
	default:
		return nil, backendError("NudgeProject", err)
	}
	// Triage is woken BEFORE the review is dispatched, because a failed wake
	// does not fail the delivery and a failed review dispatch does: ordered
	// the other way, a review trigger that is down would also suppress an
	// unrelated triage pass on the same delivery. A parked project is not
	// triaged at all: judging new issues is intake.
	if result.Parked == nil {
		result.Triaged, result.TriageErr = i.wakeTriage(ctx, tenantID, binding.ProjectID, event.Kind)
	}
	result.RulingReadmitted, result.RulingError = i.readmitOnRuling(ctx, tenantID, binding.ProjectID, event)

	// THE REVIEW IS DISPATCHED AFTER THE NUDGE AND NEVER INSTEAD OF IT. The
	// poller stays the single path an issue becomes a Task through, whatever
	// else a delivery is about; the review is an additional consequence of a
	// pull-request event, not an alternative to reconciling the project.
	started, err := DispatchReview(ctx, i.reviews, tenantID, binding, event)
	if err != nil {
		return nil, err
	}
	result.ReviewStarted = started
	return result, nil
}

// wakeTriage starts a triage pass for the deliveries that can change a
// verdict, and reports whether it did and, if it could not, why.
//
// A FAILURE HERE DOES NOT FAIL THE DELIVERY. The forge retries a delivery
// this handler rejects, and re-verifying and re-nudging the poller to reach
// a triage pass that the periodic backstop will make anyway is a poor trade.
// The failure is carried on the result for the caller to log.
func (i *WebhookIngestor) wakeTriage(ctx context.Context, tenantID, projectID string, kind ForgeEventKind) (bool, error) {
	if i.triage == nil || !TriageWakesOn(kind) {
		return false, nil
	}
	if err := i.triage.NudgeProject(ctx, tenantID, projectID); err != nil {
		return false, &TriageWakeError{TenantID: tenantID, ProjectID: projectID, Cause: err}
	}
	return true, nil
}

// readmitOnRuling hands a verified issue comment to the ruling path.
//
// A FAILURE HERE DOES NOT FAIL THE DELIVERY, for the reason wakeTriage gives:
// the forge would retry it, and the unblock sweep reads the same comments on
// its next pass anyway.
func (i *WebhookIngestor) readmitOnRuling(ctx context.Context, tenantID, projectID string, event *ForgeEvent) (bool, error) {
	if i.rulings == nil || event.Kind != EventIssueCommented || event.IssueNumber <= 0 {
		return false, nil
	}
	return i.rulings.ReadmitOnRuling(ctx, tenantID, projectID, event.IssueNumber)
}

// validateEnvelope refuses a delivery whose path or body is unusable, which
// is everything that can be checked before the bound forge is known.
func validateEnvelope(d WebhookDelivery) error {
	switch {
	case d.ProjectID == "":
		return &InvalidInputError{Field: "project_id", Message: "the path names no project"}
	case len(d.Payload) == 0:
		return &InvalidInputError{Field: "payload", Message: "an empty body carries nothing to verify"}
	}
	return nil
}

// validateDelivery refuses a delivery the ingestor cannot act on.
func validateDelivery(d WebhookDelivery) error {
	if err := validateEnvelope(d); err != nil {
		return err
	}
	switch {
	case d.EventType == "":
		return &InvalidInputError{Field: "event_type", Message: "the delivery names no event"}
	case d.Signature == "":
		return &InvalidInputError{Field: "signature", Message: "an unsigned delivery cannot be verified"}
	}
	return nil
}

// matchRepo refuses a delivery about a repository the binding does not
// name. A delivery naming no repository is accepted; the forge already
// routed it to this project's hook.
func matchRepo(binding *ProjectBinding, delivered RepoRef) error {
	if delivered.Owner == "" && delivered.Repo == "" {
		return nil
	}
	configured := binding.Repo()
	if strings.EqualFold(configured.Owner, delivered.Owner) && strings.EqualFold(configured.Repo, delivered.Repo) {
		return nil
	}
	return &WebhookRepoMismatchError{ProjectID: binding.ProjectID, Configured: configured, Delivered: delivered}
}

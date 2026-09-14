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
	"strings"
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
type WebhookDelivery struct {
	ProjectID  string
	EventType  string
	DeliveryID string
	Signature  string
	Payload    []byte
}

// WebhookIngestResult is what a verified delivery led to.
type WebhookIngestResult struct {
	TenantID  string
	ProjectID string
	Event     *ForgeEvent

	// Nudged says the poller was asked to sweep the project.
	Nudged bool

	// Triaged says a triage pass was started for the project, which happens
	// only for the issue deliveries that can change a verdict.
	Triaged bool

	// ReviewStarted says a review job was started for the delivery's pull
	// request.
	ReviewStarted bool
}

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

// Ingest verifies one delivery and nudges the poller for its project.
func (i *WebhookIngestor) Ingest(ctx context.Context, delivery WebhookDelivery) (*WebhookIngestResult, error) {
	if err := validateDelivery(delivery); err != nil {
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
	if binding.Forge.WebhookSecretRef == "" {
		return nil, &WebhookSecretMissingError{ProjectID: binding.ProjectID, Reason: "the binding names no webhook secret"}
	}
	secret, err := i.secrets.WebhookSecret(ctx, tenantID, binding.Forge.WebhookSecretRef)
	if err != nil {
		return nil, backendError("WebhookSecret", err)
	}
	defer zeroize(secret)
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
	if event.Kind == EventUnsupported {
		return result, nil
	}
	if err := i.nudger.NudgeProject(ctx, tenantID, binding.ProjectID); err != nil {
		return nil, backendError("NudgeProject", err)
	}
	result.Nudged = true
	// Triage is woken BEFORE the review is dispatched, because waking it
	// cannot fail and dispatching a review can: ordered the other way, a
	// review trigger that is down would also suppress an unrelated triage
	// pass on the same delivery.
	result.Triaged = i.wakeTriage(ctx, tenantID, binding.ProjectID, event.Kind)

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
// verdict, and reports whether it did.
//
// A FAILURE HERE DOES NOT FAIL THE DELIVERY. The forge retries a delivery
// this handler rejects, and re-verifying and re-nudging the poller to reach
// a triage pass that the periodic backstop will make anyway is a poor trade.
// The failure is carried on the result for the caller to log.
func (i *WebhookIngestor) wakeTriage(ctx context.Context, tenantID, projectID string, kind ForgeEventKind) bool {
	if i.triage == nil || !TriageWakesOn(kind) {
		return false
	}
	if err := i.triage.NudgeProject(ctx, tenantID, projectID); err != nil {
		return false
	}
	return true
}

// validateDelivery refuses a delivery the ingestor cannot act on.
func validateDelivery(d WebhookDelivery) error {
	switch {
	case d.ProjectID == "":
		return &InvalidInputError{Field: "project_id", Message: "the path names no project"}
	case d.EventType == "":
		return &InvalidInputError{Field: "event_type", Message: "the delivery names no event"}
	case len(d.Payload) == 0:
		return &InvalidInputError{Field: "payload", Message: "an empty body carries nothing to verify"}
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

// zeroize overwrites a secret the ingestor is done with.
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

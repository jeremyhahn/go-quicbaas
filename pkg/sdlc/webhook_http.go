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
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// =============================================================================
// Webhook HTTP ingress
// =============================================================================
//
// The handler reads a bounded body, hands the delivery to the ingestor, and
// tells the forge as little as it can: every authentication failure —
// unknown project, missing secret, bad signature — is the same 401, so an
// anonymous caller cannot tell which it hit. What the OPERATOR is told is
// everything, through onError — including a failure on an ACCEPTED
// delivery, such as a triage wake that could not start.

// WebhookPathParams extracts the project id from the request path. Injected
// so the module holds no router.
type WebhookPathParams func(r *http.Request) (projectID string)

// WebhookHTTPHandler serves the webhook ingress route.
type WebhookHTTPHandler struct {
	ingestor *WebhookIngestor
	params   WebhookPathParams
	maxBody  int64
	onError  func(*http.Request, error)
}

// NewWebhookHTTPHandler wires the handler; the body bound must be positive
// because verification buffers the whole body.
func NewWebhookHTTPHandler(ingestor *WebhookIngestor, params WebhookPathParams, maxBody int64, onError func(*http.Request, error)) (*WebhookHTTPHandler, error) {
	if ingestor == nil {
		return nil, &InvalidInputError{Field: "ingestor", Message: "must not be nil"}
	}
	if params == nil {
		return nil, &InvalidInputError{Field: "params", Message: "must not be nil; a delivery whose path cannot be read names no project"}
	}
	if maxBody <= 0 {
		return nil, &InvalidInputError{
			Field:   "max_body",
			Message: "must be positive; verification buffers the whole body, so an unbounded one is an unauthenticated memory sink",
		}
	}
	return &WebhookHTTPHandler{ingestor: ingestor, params: params, maxBody: maxBody, onError: onError}, nil
}

type webhookAcceptedResponse struct {
	Accepted bool   `json:"accepted"`
	Nudged   bool   `json:"nudged"`
	Parked   bool   `json:"parked,omitempty"`
	Ignored  string `json:"ignored,omitempty"`
}

type webhookRefusedResponse struct {
	Error string `json:"error"`
}

// ServeHTTP implements http.Handler.
func (h *WebhookHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.refuse(w, r, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.refuse(w, r, http.StatusRequestEntityTooLarge, "payload too large", err)
			return
		}
		h.refuse(w, r, http.StatusBadRequest, "could not read the request body", err)
		return
	}
	result, err := h.ingestor.Ingest(r.Context(), WebhookDelivery{
		ProjectID:  h.params(r),
		EventType:  r.Header.Get(GiteaEventHeader),
		DeliveryID: r.Header.Get(GiteaDeliveryHeader),
		Signature:  r.Header.Get(GiteaSignatureHeader),
		Payload:    body,
	})
	if err != nil {
		status, reason := webhookRefusal(err)
		h.refuse(w, r, status, reason, err)
		return
	}
	// A triage wake that failed did not fail the delivery — the forge must
	// not retry it — but the operator is told, or webhook-driven triage
	// could be down with nothing anywhere saying so.
	if result.TriageErr != nil && h.onError != nil {
		h.onError(r, result.TriageErr)
	}
	if result.RulingError != nil && h.onError != nil {
		h.onError(r, result.RulingError)
	}
	// A parked project's refusal is acknowledged, not retried, and logged so
	// the deliveries it declined are on record.
	if result.Parked != nil && h.onError != nil {
		h.onError(r, result.Parked)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if encodeErr := json.NewEncoder(w).Encode(webhookAcceptedResponse{Accepted: true, Nudged: result.Nudged, Parked: result.Parked != nil, Ignored: string(result.Ignored)}); encodeErr != nil && h.onError != nil {
		h.onError(r, encodeErr)
	}
}

func (h *WebhookHTTPHandler) refuse(w http.ResponseWriter, r *http.Request, status int, reason string, err error) {
	if h.onError != nil && err != nil {
		h.onError(r, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(webhookRefusedResponse{Error: reason}); encodeErr != nil && h.onError != nil {
		h.onError(r, encodeErr)
	}
}

// webhookRefusals maps a refusal to what the caller is told, most specific
// first so a wrapped sentinel is matched before a general one.
var webhookRefusals = []struct {
	sentinel error
	status   int
	reason   string
}{
	{ErrWebhookUnverified, http.StatusUnauthorized, "unauthenticated"},
	{ErrWebhookSecretMissing, http.StatusUnauthorized, "unauthenticated"},
	{ErrWebhookRepoMismatch, http.StatusBadRequest, "delivery does not match this endpoint"},
	{ErrInvalidInput, http.StatusBadRequest, "malformed delivery"},
	{ErrNotFound, http.StatusUnauthorized, "unauthenticated"},
	{ErrForgeNotRegistered, http.StatusNotImplemented, "no adapter for this forge"},
	{ErrBackend, http.StatusBadGateway, "upstream failure"},
}

// webhookRefusal maps an ingest error to a status and a public reason.
func webhookRefusal(err error) (int, string) {
	for _, refusal := range webhookRefusals {
		if errors.Is(err, refusal.sentinel) {
			return refusal.status, refusal.reason
		}
	}
	return http.StatusInternalServerError, "internal error"
}

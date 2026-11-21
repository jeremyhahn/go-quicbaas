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
	"bytes"
	"context"
	"errors"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// Webhook route index — projectID → tenantID on the system plane
// =============================================================================
//
// A webhook delivery arrives on an unauthenticated route carrying only the
// project id the forge was configured with; the tenant is deliberately
// absent from that URL so the tenancy layout is not published to anyone who
// can read a webhook configuration. This index is the platform-owned record
// that routes the delivery to the tenant that bound the project, written by a
// guarded create so two tenants can never claim one project id.

const webhookRouteEntity = "webhook route"

// ErrWebhookRouteConflict indicates a project id already routed to another
// tenant.
var ErrWebhookRouteConflict = errors.New("sdlc: webhook route held by another tenant")

// WebhookRouteConflictError reports a route another tenant holds.
type WebhookRouteConflictError struct {
	ProjectID string
}

// Error implements the error interface.
func (e *WebhookRouteConflictError) Error() string {
	return "sdlc: webhook route for project " + e.ProjectID + " is held by another tenant"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *WebhookRouteConflictError) Unwrap() error { return ErrWebhookRouteConflict }

// WebhookRouteIndex records which tenant a project's deliveries route to.
type WebhookRouteIndex struct {
	writer *cas.Writer
	kv     cas.Reader
}

// NewWebhookRouteIndex builds the index over the platform shard.
func NewWebhookRouteIndex(tx cas.Transactor, kv cas.Reader) (*WebhookRouteIndex, error) {
	if tx == nil || kv == nil {
		return nil, &InvalidInputError{Field: "webhook route index", Message: "needs the platform shard's transaction and KV clients"}
	}
	writer, err := cas.NewWriter(tx, kv, backendError)
	if err != nil {
		return nil, err
	}
	return &WebhookRouteIndex{writer: writer, kv: kv}, nil
}

// Record routes projectID to tenantID. Re-recording the same tenant is a
// no-op; another tenant's claim is refused.
func (i *WebhookRouteIndex) Record(ctx context.Context, projectID, tenantID string) error {
	key, err := WebhookRouteKey(projectID)
	if err != nil {
		return err
	}
	if tenantID == "" {
		return &InvalidInputError{Field: "tenant_id", Message: "must not be empty; an unowned route would resolve deliveries to nobody"}
	}
	created, err := i.writer.Create(ctx, key, []byte(tenantID))
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	held, err := i.value(ctx, "Record", key)
	if err != nil {
		return err
	}
	if bytes.Equal(held, []byte(tenantID)) {
		return nil
	}
	return &WebhookRouteConflictError{ProjectID: projectID}
}

// Tenant returns the tenant projectID routes to.
func (i *WebhookRouteIndex) Tenant(ctx context.Context, projectID string) (string, error) {
	key, err := WebhookRouteKey(projectID)
	if err != nil {
		return "", err
	}
	held, err := i.value(ctx, "Tenant", key)
	if err != nil {
		return "", err
	}
	if len(held) == 0 {
		return "", &NotFoundError{Entity: webhookRouteEntity, ID: projectID}
	}
	return string(held), nil
}

// Forget removes the route when tenantID holds it. Forgetting an absent
// route, or one another tenant holds, is a no-op.
func (i *WebhookRouteIndex) Forget(ctx context.Context, projectID, tenantID string) error {
	key, err := WebhookRouteKey(projectID)
	if err != nil {
		return err
	}
	held, err := i.value(ctx, "Forget", key)
	if err != nil {
		return err
	}
	if len(held) == 0 || !bytes.Equal(held, []byte(tenantID)) {
		return nil
	}
	if err := i.kv.Delete(ctx, key); err != nil {
		return backendError("Forget", err)
	}
	return nil
}

func (i *WebhookRouteIndex) value(ctx context.Context, op, key string) ([]byte, error) {
	data, err := i.kv.Get(ctx, key)
	if err != nil {
		var missing *transport.KeyNotFoundError
		if errors.As(err, &missing) {
			return nil, nil
		}
		return nil, backendError(op+".read", err)
	}
	return data, nil
}

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

package rest

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// accountRESTBasePath is the REST mount for pooled provider accounts. It
// mirrors pkg/api/rest/handlers_account.go registerAccountRoutes.
const accountRESTBasePath = "/api/v1/agent-accounts"

// accountRESTHandler maps the SDK account verbs onto the REST routes and
// decodes the server's response envelopes into the SDK types.
type accountRESTHandler struct {
	transport *Transport
}

// newAccountRESTHandler creates a new account REST handler.
func newAccountRESTHandler(t *Transport) *accountRESTHandler {
	return &accountRESTHandler{transport: t}
}

// isAccountService reports whether service names the Account service.
func isAccountService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Account"
}

// accountEnvelope is the single-account response envelope the server writes
// for create / get / update / revoke.
type accountEnvelope struct {
	Account   *account.AccountInfo `json:"account"`
	Timestamp time.Time            `json:"timestamp"`
}

// accountListEnvelope is the list response envelope, paging included.
type accountListEnvelope struct {
	Accounts  []*account.AccountInfo `json:"accounts"`
	Timestamp time.Time              `json:"timestamp"`
	page.Envelope
}

// accountAckEnvelope is the acknowledgement envelope for rotate / set-default.
type accountAckEnvelope struct {
	AccountID string    `json:"account_id"`
	Timestamp time.Time `json:"timestamp"`
}

// accountDeleteEnvelope is the delete response envelope.
type accountDeleteEnvelope struct {
	AccountID string    `json:"account_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// accountUsageEnvelope is the usage response envelope.
type accountUsageEnvelope struct {
	Usage     *account.UsageReport `json:"usage"`
	Timestamp time.Time            `json:"timestamp"`
}

// accountMethodHandler handles one SDK account method.
type accountMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// handleRequest routes account requests using map-based O(1) dispatch.
func (h *accountRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handlers := map[string]accountMethodHandler{
		"Create":           h.handleCreate,
		"Get":              h.handleGet,
		"List":             h.handleList,
		"Update":           h.handleUpdate,
		"Delete":           h.handleDelete,
		"RotateCredential": h.handleRotateCredential,
		"Revoke":           h.handleRevoke,
		"SetDefault":       h.handleSetDefault,
		"Usage":            h.handleUsage,
		"Materialize":      h.handleMaterialize,
	}

	handler, ok := handlers[method]
	if !ok {
		return nil, errors.NewInvalidInput("account."+method, "unknown account method")
	}
	return handler(ctx, req)
}

// itemPath builds the /{id} path with the required tenant_id query parameter.
func accountItemPath(accountID, tenantID, suffix string) string {
	return accountRESTBasePath + "/" + url.PathEscape(accountID) + suffix +
		"?tenant_id=" + url.QueryEscape(tenantID)
}

// handleCreate handles POST /api/v1/agent-accounts.
func (h *accountRESTHandler) handleCreate(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.CreateAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Create", "invalid request type")
	}
	var env accountEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, accountRESTBasePath, sdkReq, &env); err != nil {
		return nil, err
	}
	return env.Account, nil
}

// handleGet handles GET /api/v1/agent-accounts/{id}.
func (h *accountRESTHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.GetAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Get", "invalid request type")
	}
	var env accountEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, ""), nil, &env); err != nil {
		return nil, err
	}
	return env.Account, nil
}

// handleList handles GET /api/v1/agent-accounts.
func (h *accountRESTHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.ListAccountsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.List", "invalid request type")
	}
	query := url.Values{}
	query.Set("tenant_id", sdkReq.TenantID)
	if sdkReq.Provider != "" {
		query.Set("provider", string(sdkReq.Provider))
	}
	if sdkReq.ProjectID != "" {
		query.Set("project_id", sdkReq.ProjectID)
	}
	if sdkReq.Status != "" {
		query.Set("status", sdkReq.Status)
	}
	setPageQuery(query, sdkReq.Params)

	var env accountListEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		accountRESTBasePath+"?"+query.Encode(), nil, &env); err != nil {
		return nil, err
	}
	return &account.ListAccountsResponse{Accounts: env.Accounts, Envelope: env.Envelope}, nil
}

// handleUpdate handles PUT /api/v1/agent-accounts/{id}.
func (h *accountRESTHandler) handleUpdate(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.UpdateAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Update", "invalid request type")
	}
	var env accountEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPut,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, ""), sdkReq, &env); err != nil {
		return nil, err
	}
	return env.Account, nil
}

// handleDelete handles DELETE /api/v1/agent-accounts/{id}.
func (h *accountRESTHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.DeleteAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Delete", "invalid request type")
	}
	var env accountDeleteEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, ""), nil, &env); err != nil {
		return nil, err
	}
	return &account.DeleteAccountResponse{AccountID: env.AccountID, Deleted: env.Deleted}, nil
}

// handleRotateCredential handles POST /api/v1/agent-accounts/{id}/rotate.
func (h *accountRESTHandler) handleRotateCredential(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.RotateCredentialRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.RotateCredential", "invalid request type")
	}
	body := struct {
		Credential *account.Credential `json:"credential"`
	}{Credential: sdkReq.Credential}

	var env accountAckEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, "/rotate"), &body, &env); err != nil {
		return nil, err
	}
	return &account.AccountAckResponse{AccountID: env.AccountID}, nil
}

// handleRevoke handles POST /api/v1/agent-accounts/{id}/revoke.
func (h *accountRESTHandler) handleRevoke(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.RevokeAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Revoke", "invalid request type")
	}
	body := struct {
		Reason string `json:"reason,omitempty"`
	}{Reason: sdkReq.Reason}

	var env accountEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, "/revoke"), &body, &env); err != nil {
		return nil, err
	}
	return env.Account, nil
}

// handleSetDefault handles POST /api/v1/agent-accounts/{id}/default.
func (h *accountRESTHandler) handleSetDefault(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.SetDefaultAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.SetDefault", "invalid request type")
	}
	var env accountAckEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, "/default"), nil, &env); err != nil {
		return nil, err
	}
	return &account.AccountAckResponse{AccountID: env.AccountID}, nil
}

// handleUsage handles GET /api/v1/agent-accounts/{id}/usage.
func (h *accountRESTHandler) handleUsage(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.GetAccountUsageRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Usage", "invalid request type")
	}
	var env accountUsageEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, "/usage"), nil, &env); err != nil {
		return nil, err
	}
	if env.Usage == nil {
		return nil, errors.NewInternal("account.Usage", "server returned no usage report")
	}
	return env.Usage, nil
}

// materializeEnvelope is the credential-release response envelope. It is the
// ONE account envelope in this file that carries secret material.
type materializeEnvelope struct {
	AccountID       string                      `json:"account_id"`
	Provider        account.Provider            `json:"provider"`
	DisplayName     string                      `json:"display_name,omitempty"`
	ProjectID       string                      `json:"project_id,omitempty"`
	RefreshPolicy   account.RefreshPolicy       `json:"refresh_policy,omitempty"`
	Credential      *account.ReleasedCredential `json:"credential"`
	ExpiresAt       time.Time                   `json:"expires_at,omitempty"`
	UpdatedAt       time.Time                   `json:"updated_at,omitempty"`
	LastRefreshedAt time.Time                   `json:"last_refreshed_at,omitempty"`
	LeaseID         string                      `json:"lease_id,omitempty"`
	LeaseExpiresAt  time.Time                   `json:"lease_expires_at,omitempty"`
	Timestamp       time.Time                   `json:"timestamp"`
}

// handleMaterialize handles POST /api/v1/agent-accounts/{id}/materialize.
func (h *accountRESTHandler) handleMaterialize(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*account.MaterializeCredentialRequest)
	if !ok {
		return nil, errors.NewInvalidInput("account.Materialize", "invalid request type")
	}
	body := struct {
		ProjectID string `json:"project_id,omitempty"`
		JobID     string `json:"job_id,omitempty"`
		LeaseID   string `json:"lease_id,omitempty"`
	}{ProjectID: sdkReq.ProjectID, JobID: sdkReq.JobID, LeaseID: sdkReq.LeaseID}

	var env materializeEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		accountItemPath(sdkReq.AccountID, sdkReq.TenantID, "/materialize"), &body, &env); err != nil {
		return nil, err
	}
	return &account.MaterializedCredential{
		AccountID:       env.AccountID,
		Provider:        env.Provider,
		DisplayName:     env.DisplayName,
		ProjectID:       env.ProjectID,
		RefreshPolicy:   env.RefreshPolicy,
		Credential:      env.Credential,
		ExpiresAt:       env.ExpiresAt,
		UpdatedAt:       env.UpdatedAt,
		LastRefreshedAt: env.LastRefreshedAt,
		LeaseID:         env.LeaseID,
		LeaseExpiresAt:  env.LeaseExpiresAt,
	}, nil
}

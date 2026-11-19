package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TenantClient serves the qrdb per-tenant barrier verbs over the SDK's own
// REST transport (go-quicbaas#2554). go-qrdb's REST sub-client builds its
// own http.Client, which follows redirects with net/http's default policy:
// a 307/308 to an http:// Location resends a replayable JSON body, and with
// it an unseal key or a Shamir share, in plaintext. The SDK cannot reach
// that client to add a redirect policy, so the barrier verbs are sent here
// instead, through the transport's guarded client.
//
// The verbs that carry a secret mark their request context with
// withSecretHop, so the transport refuses any redirect hop and any non-https
// hop before the secret is sent: TenantBarrierInit, TenantBarrierUnseal,
// TenantBarrierUnsealShare, TenantBarrierShareClaimComplete (whose response
// carries a Shamir share) and TenantBarrierShamirSealVote.
//
// Each overridden verb keeps the HTTP method, path, request body and
// response handling of the matching method in go-qrdb sdk/go transport/rest.
// The embedded go-qrdb client serves every verb not overridden here.
type TenantClient struct {
	qrdbtransport.TenantClient
	t *Transport
}

var _ qrdbtransport.TenantClient = (*TenantClient)(nil)

// Tenants returns the tenant client bound to this transport. inner serves
// every verb TenantClient does not override.
func (t *Transport) Tenants(inner qrdbtransport.TenantClient) *TenantClient {
	return &TenantClient{TenantClient: inner, t: t}
}

const tenantClientOp = "rest.TenantClient"

// tenantPath returns /v1/tenants/{id}/barrier<suffix>, refusing a nil
// request (isNil) or an empty tenant id before anything is sent.
func tenantPath(verb string, isNil bool, tenantID, suffix string) (string, error) {
	if isNil {
		return "", errors.New(errors.CodeInvalidInput, tenantClientOp, verb+": request is nil")
	}
	if tenantID == "" {
		return "", errors.New(errors.CodeInvalidInput, tenantClientOp, verb+": tenant id is empty")
	}
	return "/v1/tenants/" + url.PathEscape(tenantID) + "/barrier" + suffix, nil
}

// officerPath returns the tenant barrier path of one officer's claim step,
// refusing an empty username.
func officerPath(verb, tenantID, username, step string) (string, error) {
	if username == "" {
		return "", errors.New(errors.CodeInvalidInput, tenantClientOp, verb+": username is empty")
	}
	return tenantPath(verb, false, tenantID, "/officers/"+url.PathEscape(username)+"/claim/"+step)
}

// doJSONResult sends the request through the transport's guarded client and
// decodes a non-empty response into result.
func (c *TenantClient) doJSONResult(ctx context.Context, method, path string, reqBody, result any) error {
	return c.t.Barrier().doJSONResult(ctx, method, path, reqBody, result)
}

// tenantBarrierInitWireRequest is the REST wire shape of a tenant barrier
// init: the tenant id travels in the path.
type tenantBarrierInitWireRequest struct {
	Passphrase []byte `json:"passphrase,omitempty"`
	Threshold  int    `json:"threshold,omitempty"`
	Shares     int    `json:"shares,omitempty"`
	Strategy   string `json:"strategy,omitempty"`
}

// TenantBarrierInit maps to POST /v1/tenants/{id}/barrier/init. A 204 (the
// none/passphrase strategies) is populated from the request; a 200 (Shamir)
// carries the share metadata.
func (c *TenantClient) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	path, err := tenantPath("TenantBarrierInit", req == nil, tenantIDOf(req), "/init")
	if err != nil {
		return nil, err
	}
	ctx = withSecretHop(ctx, "TenantBarrierInit")
	body, err := json.Marshal(tenantBarrierInitWireRequest{
		Passphrase: req.Passphrase,
		Threshold:  req.Threshold,
		Shares:     req.Shares,
		Strategy:   req.Strategy,
	})
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, tenantClientOp, "failed to encode request body")
	}
	resp, err := c.t.doRawRequest(ctx, http.MethodPost, path, body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNoContent {
		return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID, Strategy: req.Strategy}, nil
	}
	if len(resp.Body) == 0 {
		return nil, errors.New(errors.CodeInternal, tenantClientOp, "empty response from "+path)
	}
	var out qrdbtransport.TenantBarrierInitResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, tenantClientOp, "failed to decode response")
	}
	return &out, nil
}

func tenantIDOf(req *qrdbtransport.TenantBarrierInitRequest) string {
	if req == nil {
		return ""
	}
	return req.TenantID
}

// TenantBarrierUnseal maps to POST /v1/tenants/{id}/barrier/unseal. The key
// is Key, or Share when Key is empty.
func (c *TenantClient) TenantBarrierUnseal(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierUnseal", req == nil, tenantID, "/unseal")
	if err != nil {
		return err
	}
	ctx = withSecretHop(ctx, "TenantBarrierUnseal")
	key := req.Key
	if len(key) == 0 {
		key = req.Share
	}
	return c.t.doJSONRequest(ctx, http.MethodPost, path, struct {
		Key []byte `json:"key"`
	}{Key: key}, nil)
}

// TenantBarrierUnsealShare maps to POST /v1/tenants/{id}/barrier/unseal-share.
func (c *TenantClient) TenantBarrierUnsealShare(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierUnsealShare", req == nil, tenantID, "/unseal-share")
	if err != nil {
		return nil, err
	}
	ctx = withSecretHop(ctx, "TenantBarrierUnsealShare")
	var out qrdbtransport.TenantBarrierUnsealProgress
	if err := c.doJSONResult(ctx, http.MethodPost, path, struct {
		Share string `json:"share"`
	}{Share: req.Share}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TenantBarrierShareClaimComplete maps to
// POST /v1/tenants/{id}/barrier/officers/{username}/claim/complete. The
// response carries the officer's Shamir share.
func (c *TenantClient) TenantBarrierShareClaimComplete(ctx context.Context, req *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	if req == nil {
		_, err := tenantPath("TenantBarrierShareClaimComplete", true, "", "")
		return nil, err
	}
	path, err := officerPath("TenantBarrierShareClaimComplete", req.TenantID, req.Username, "complete")
	if err != nil {
		return nil, err
	}
	ctx = withSecretHop(ctx, "TenantBarrierShareClaimComplete")
	var out qrdbtransport.ShareClaimCompleteResponse
	if err := c.doJSONResult(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TenantBarrierShamirSealVote maps to POST /v1/tenants/{id}/barrier/seal-vote.
func (c *TenantClient) TenantBarrierShamirSealVote(ctx context.Context, req *qrdbtransport.TenantShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierShamirSealVote", req == nil, tenantID, "/seal-vote")
	if err != nil {
		return nil, err
	}
	ctx = withSecretHop(ctx, "TenantBarrierShamirSealVote")
	var out qrdbtransport.ShamirSealVoteResponse
	if err := c.doJSONResult(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

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
// Every barrier verb, secret-carrying or not (TenantBarrierSeal,
// TenantBarrierStatus and the officer verbs too), is sent with the headers
// the transport was built with, so it carries the platform bearer set by
// WithAuthToken whenever the transport sends it; go-qrdb's REST sub-client
// cannot add an Authorization header and reached protected routes
// anonymously. Tenant CRUD stays on the embedded go-qrdb client.
//
// Because they carry that bearer, the barrier verbs never follow a redirect
// to a non-https URL or to a host:port other than the one the request was
// sent to: the transport's client refuses such a hop before the bearer is
// sent and the verb fails with a CodeForbidden error wrapping
// ErrInsecureRedirect. A same-host https redirect is still followed. They
// never send it on a non-https first hop either: the transport refuses the
// hop before anything is sent and the verb fails with a CodeForbidden error
// wrapping ErrPlaintextBearer. Without WithAuthToken they still work over
// http.
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

// ErrInsecureRedirect is wrapped by the error a bearer-carrying tenant verb
// returns when it refuses a redirect to a non-https URL or to a host:port
// other than the one the request was sent to. It is a plain error, not a
// QBError, so errors.Is matches it alone and never the secret-hop refusals.
var ErrInsecureRedirect = stderrors.New("rest: refusing a redirect that would carry the bearer over plaintext or to another host")

// ErrPlaintextBearer is wrapped by the error a tenant barrier verb returns
// when it refuses to send the platform bearer on a hop that is not https,
// the first hop included. Like ErrInsecureRedirect it is a plain error.
var ErrPlaintextBearer = stderrors.New("rest: refusing to send the bearer over plaintext http")

// tenantBearerHopKey marks a request context as a tenant barrier verb whose
// hops tenantRedirectPolicy and secretHopRoundTripper check.
type tenantBearerHopKey struct{}

// withTenantBearerHop marks ctx for tenantRedirectPolicy.
func withTenantBearerHop(ctx context.Context) context.Context {
	return context.WithValue(ctx, tenantBearerHopKey{}, true)
}

// tenantRedirectPolicy wraps an http.Client CheckRedirect (prev, nil for
// net/http's default of stopping after 10 redirects). For a request marked
// by withTenantBearerHop whose first hop carried an Authorization header,
// it refuses a redirect to a non-https URL or to a host:port other than the
// first hop's with ErrInsecureRedirect; every other redirect goes to prev.
func tenantRedirectPolicy(prev func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if marked, _ := req.Context().Value(tenantBearerHopKey{}).(bool); marked && len(via) > 0 &&
			via[0].Header.Get("Authorization") != "" {
			if req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("%w: %s", ErrInsecureRedirect, req.URL.Redacted())
			}
		}
		if prev != nil {
			return prev(req, via)
		}
		if len(via) >= 10 {
			return stderrors.New("stopped after 10 redirects")
		}
		return nil
	}
}

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

// doTenantRequest sends the request through the transport's guarded client
// and refuses any status not in allowed: the tenant barrier routes answer
// only 200 or 204, so a 3xx handed back by net/http (a 307 without a
// Location, or a client whose CheckRedirect stops at the redirect) is never
// taken as success.
func (c *TenantClient) doTenantRequest(ctx context.Context, verb, method, path string, reqBody any, allowed ...int) (*rawResponse, error) {
	var body []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, tenantClientOp, "failed to encode request body")
		}
		body = data
	}
	resp, err := c.t.doRawRequest(withTenantBearerHop(ctx), method, path, body, nil)
	if err != nil {
		if stderrors.Is(err, ErrInsecureRedirect) {
			return nil, errors.Wrap(err, errors.CodeForbidden, tenantClientOp, verb+": refused an insecure redirect")
		}
		if stderrors.Is(err, ErrPlaintextBearer) {
			return nil, errors.Wrap(err, errors.CodeForbidden, tenantClientOp, verb+": refused to send the bearer over plaintext")
		}
		return nil, err
	}
	for _, status := range allowed {
		if resp.StatusCode == status {
			return resp, nil
		}
	}
	return nil, errors.New(errors.CodeInternal, tenantClientOp,
		verb+": unexpected HTTP status "+strconv.Itoa(resp.StatusCode)+" from "+path)
}

// doTenantResult sends the request, accepts only 200 and decodes the
// non-empty response into result.
func (c *TenantClient) doTenantResult(ctx context.Context, verb, method, path string, reqBody, result any) error {
	resp, err := c.doTenantRequest(ctx, verb, method, path, reqBody, http.StatusOK)
	if err != nil {
		return err
	}
	if len(resp.Body) == 0 {
		return errors.New(errors.CodeInternal, tenantClientOp, "empty response from "+path)
	}
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return errors.Wrap(err, errors.CodeInternal, tenantClientOp, "failed to decode response")
	}
	return nil
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
// carries the share metadata. Any other status is an error.
func (c *TenantClient) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	path, err := tenantPath("TenantBarrierInit", req == nil, tenantIDOf(req), "/init")
	if err != nil {
		return nil, err
	}
	ctx = withSecretHop(ctx, "TenantBarrierInit")
	resp, err := c.doTenantRequest(ctx, "TenantBarrierInit", http.MethodPost, path, tenantBarrierInitWireRequest{
		Passphrase: req.Passphrase,
		Threshold:  req.Threshold,
		Shares:     req.Shares,
		Strategy:   req.Strategy,
	}, http.StatusOK, http.StatusNoContent)
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
// is Key, or Share when Key is empty. Only 200 and 204 succeed; any other
// status is an error.
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
	_, err = c.doTenantRequest(ctx, "TenantBarrierUnseal", http.MethodPost, path, struct {
		Key []byte `json:"key"`
	}{Key: key}, http.StatusOK, http.StatusNoContent)
	return err
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
	if err := c.doTenantResult(ctx, "TenantBarrierUnsealShare", http.MethodPost, path, struct {
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
	if err := c.doTenantResult(ctx, "TenantBarrierShareClaimComplete", http.MethodPost, path, req, &out); err != nil {
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
	if err := c.doTenantResult(ctx, "TenantBarrierShamirSealVote", http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TenantBarrierSeal maps to POST /v1/tenants/{id}/barrier/seal, with no
// body. Only 200 and 204 succeed; any other status is an error.
func (c *TenantClient) TenantBarrierSeal(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierSeal", req == nil, tenantID, "/seal")
	if err != nil {
		return err
	}
	_, err = c.doTenantRequest(ctx, "TenantBarrierSeal", http.MethodPost, path, nil, http.StatusOK, http.StatusNoContent)
	return err
}

// TenantBarrierStatus maps to GET /v1/tenants/{id}/barrier/status.
func (c *TenantClient) TenantBarrierStatus(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierStatus", req == nil, tenantID, "/status")
	if err != nil {
		return nil, err
	}
	var out qrdbtransport.TenantBarrierStatusResponse
	if err := c.doTenantResult(ctx, "TenantBarrierStatus", http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TenantBarrierOfficerRegister maps to POST /v1/tenants/{id}/barrier/officers.
// csr_pem and cert_pem travel as plain PEM text.
func (c *TenantClient) TenantBarrierOfficerRegister(ctx context.Context, req *qrdbtransport.TenantOfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	var tenantID string
	if req != nil {
		tenantID = req.TenantID
	}
	path, err := tenantPath("TenantBarrierOfficerRegister", req == nil, tenantID, "/officers")
	if err != nil {
		return nil, err
	}
	var wire officerRegisterWireResponse
	if err := c.doTenantResult(ctx, "TenantBarrierOfficerRegister", http.MethodPost, path, &officerRegisterWireRequest{
		Username:          req.Username,
		CSRPEM:            string(req.CSRPEM),
		AuthenticatorType: req.AuthenticatorType,
		AuthenticatorRef:  req.AuthenticatorRef,
	}, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.OfficerRegisterResponse{
		CertPEM:    []byte(wire.CertPEM),
		ShareIndex: wire.ShareIndex,
	}, nil
}

// TenantBarrierOfficerList maps to GET /v1/tenants/{id}/barrier/officers.
func (c *TenantClient) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	path, err := tenantPath("TenantBarrierOfficerList", false, tenantID, "/officers")
	if err != nil {
		return nil, err
	}
	var wire officersWireResponse
	if err := c.doTenantResult(ctx, "TenantBarrierOfficerList", http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Officers, nil
}

// TenantBarrierOfficerClaimStatus maps to
// GET /v1/tenants/{id}/barrier/officers/claim-status.
func (c *TenantClient) TenantBarrierOfficerClaimStatus(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	path, err := tenantPath("TenantBarrierOfficerClaimStatus", false, tenantID, "/officers/claim-status")
	if err != nil {
		return nil, err
	}
	var wire officersWireResponse
	if err := c.doTenantResult(ctx, "TenantBarrierOfficerClaimStatus", http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Officers, nil
}

// TenantBarrierShareClaimBegin maps to
// POST /v1/tenants/{id}/barrier/officers/{username}/claim/begin.
func (c *TenantClient) TenantBarrierShareClaimBegin(ctx context.Context, req *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if req == nil {
		_, err := tenantPath("TenantBarrierShareClaimBegin", true, "", "")
		return nil, err
	}
	path, err := officerPath("TenantBarrierShareClaimBegin", req.TenantID, req.Username, "begin")
	if err != nil {
		return nil, err
	}
	var out qrdbtransport.ShareClaimBeginResponse
	if err := c.doTenantResult(ctx, "TenantBarrierShareClaimBegin", http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

package rest

import (
	"context"
	"encoding/base64"
	"net/http"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// CeremonyClient serves the qrdb ceremony verbs over the SDK's own REST
// transport, so every ceremony request carries the headers the transport was
// built with, the platform bearer set by WithAuthToken among them. go-qrdb's
// REST sub-client has no way to add an Authorization header, so over REST
// an operational node's default-deny gate refused it with 401.
//
// Each method keeps the HTTP method, path, request body and response type
// of the matching method in go-qrdb sdk/go transport/rest
// (transport_ceremony.go), including the SO PIN resolution of the verbs
// that carry one.
//
// The verbs that carry a secret mark their request context with
// withSecretHop, so the transport refuses any redirect hop and any non-https
// hop before the secret is sent: CeremonyInitialize and CeremonySignCSR
// (which carry an SO PIN), CeremonyCompleteClaimShare (whose response
// carries a Shamir share) and CeremonySubmitUnsealShare (which carries one).
type CeremonyClient struct {
	t *Transport
}

var _ qrdbtransport.CeremonyClient = (*CeremonyClient)(nil)

// Ceremony returns the ceremony client bound to this transport.
func (t *Transport) Ceremony() *CeremonyClient {
	return &CeremonyClient{t: t}
}

// doJSONResult sends a ceremony request and decodes the response into
// result, refusing an empty body: a zero CeremonyStatusResponse would
// report a state the server never sent.
func (c *CeremonyClient) doJSONResult(ctx context.Context, method, path string, reqBody, result any) error {
	return doJSONResult(ctx, c.t, "rest.CeremonyClient", method, path, reqBody, result)
}

// completeClaimShareWire is the REST wire shape of a share claim
// completion: the signature travels base64-encoded, as go-qrdb sends it.
type completeClaimShareWire struct {
	Username  string `json:"username"`
	NonceHex  string `json:"nonce_hex"`
	Signature string `json:"signature"`
}

// CeremonyInitialize starts the initialization ceremony. Maps to
// POST /v1/ceremony/initialize. The SO PIN travels in the request's so_pin
// field, taken from the PIN bound by WithSOPIN when the request has none.
func (c *CeremonyClient) CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	ctx = withSecretHop(ctx, "CeremonyInitialize")
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "CeremonyInitialize", req.SOPin)
	if err != nil {
		return nil, err
	}
	defer release()
	body := *req
	body.SOPin = soPIN
	var resp qrdbtransport.CeremonyInitializeResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/initialize", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyStatus returns the current ceremony state. Maps to
// GET /v1/ceremony/status.
func (c *CeremonyClient) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	var resp qrdbtransport.CeremonyStatusResponse
	if err := c.doJSONResult(ctx, http.MethodGet, "/v1/ceremony/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyClaimCertBegin starts a certificate claim challenge. Maps to
// POST /v1/ceremony/claim-cert/begin.
func (c *CeremonyClient) CeremonyClaimCertBegin(ctx context.Context, req *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error) {
	var resp qrdbtransport.ClaimCertBeginResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/claim-cert/begin", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyClaimCertComplete completes a certificate claim. Maps to
// POST /v1/ceremony/claim-cert/complete.
func (c *CeremonyClient) CeremonyClaimCertComplete(ctx context.Context, req *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error) {
	var resp qrdbtransport.ClaimCertCompleteResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/claim-cert/complete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyBeginClaimShare starts the Shamir share claim handshake. Maps to
// POST /v1/ceremony/officer-share/begin.
func (c *CeremonyClient) CeremonyBeginClaimShare(ctx context.Context, req *qrdbtransport.BeginClaimShareRequest) (*qrdbtransport.BeginClaimShareResponse, error) {
	var resp qrdbtransport.BeginClaimShareResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/officer-share/begin", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyCompleteClaimShare completes the Shamir share claim handshake.
// Maps to POST /v1/ceremony/officer-share/complete. The response carries
// the officer's share.
func (c *CeremonyClient) CeremonyCompleteClaimShare(ctx context.Context, req *qrdbtransport.CompleteClaimShareRequest) (*qrdbtransport.CompleteClaimShareResponse, error) {
	ctx = withSecretHop(ctx, "CeremonyCompleteClaimShare")
	wire := completeClaimShareWire{
		Username:  req.Username,
		NonceHex:  req.NonceHex,
		Signature: base64.StdEncoding.EncodeToString(req.Signature),
	}
	var resp qrdbtransport.CompleteClaimShareResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/officer-share/complete", wire, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonySignCSR submits a CSR for signing. Maps to
// POST /v1/ceremony/sign-csr. The SO PIN travels in the request's so_pin
// field, taken from the PIN bound by WithSOPIN when the request has none.
func (c *CeremonyClient) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	ctx = withSecretHop(ctx, "CeremonySignCSR")
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "CeremonySignCSR", req.SOPin)
	if err != nil {
		return nil, err
	}
	defer release()
	body := *req
	body.SOPin = soPIN
	var resp qrdbtransport.SignCSRInitResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/sign-csr", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyClaimShareBegin issues a factor challenge. Maps to
// POST /v1/ceremony/claim-share/begin.
func (c *CeremonyClient) CeremonyClaimShareBegin(ctx context.Context, req *qrdbtransport.CeremonyClaimShareBeginRequest) (*qrdbtransport.CeremonyClaimShareBeginResponse, error) {
	var resp qrdbtransport.CeremonyClaimShareBeginResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/claim-share/begin", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyClaimShareComplete verifies the factor response. Maps to
// POST /v1/ceremony/claim-share/complete.
func (c *CeremonyClient) CeremonyClaimShareComplete(ctx context.Context, req *qrdbtransport.CeremonyClaimShareCompleteRequest) (*qrdbtransport.CeremonyCustodianInfo, error) {
	var resp qrdbtransport.CeremonyCustodianInfo
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/ceremony/claim-share/complete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonySubmitUnsealShare submits a custodian's share toward the unseal
// quorum. Maps to POST /v1/barrier/unseal-share.
func (c *CeremonyClient) CeremonySubmitUnsealShare(ctx context.Context, req *qrdbtransport.CeremonySubmitUnsealShareRequest) (*qrdbtransport.CeremonySubmitUnsealShareResponse, error) {
	ctx = withSecretHop(ctx, "CeremonySubmitUnsealShare")
	var resp qrdbtransport.CeremonySubmitUnsealShareResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/barrier/unseal-share", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CeremonyEnrollExternalOfficer enrols an officer whose key material lives
// outside go-qrdb. Maps to POST /v1/init/enroll-external-officer.
func (c *CeremonyClient) CeremonyEnrollExternalOfficer(ctx context.Context, req *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error) {
	var resp qrdbtransport.EnrollExternalOfficerResponse
	if err := c.doJSONResult(ctx, http.MethodPost, "/v1/init/enroll-external-officer", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

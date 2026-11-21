package rest

import (
	"context"
	"encoding/json"
	"net/http"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// BarrierClient serves the qrdb barrier verbs over the SDK's own REST
// transport, so every barrier request carries the headers the transport was
// built with, the platform bearer set by WithAuthToken among them
// (go-quicbaas#2538). go-qrdb's REST sub-client has no way to add an
// Authorization header, so over REST it reached the server's default-deny
// gate anonymously and was refused with 401.
//
// Each method keeps the HTTP method, path, request body and response type
// of the matching method in go-qrdb sdk/go transport/rest
// (transport_barrier.go), including the SO PIN resolution of the verbs
// that carry one.
type BarrierClient struct {
	t *Transport
}

var _ qrdbtransport.BarrierClient = (*BarrierClient)(nil)

// Barrier returns the barrier client bound to this transport.
func (t *Transport) Barrier() *BarrierClient {
	return &BarrierClient{t: t}
}

// bindRequestSOPIN resolves the SO PIN a call carries (its request's own
// SOPin field, or the PIN bound by WithSOPIN) and, when the PIN came from the
// request field alone, binds it to the returned context with WithSOPIN. The
// transport's SOPINRoundTripper then guards every hop of the call, so a
// request-field PIN is refused over http:// and across a redirect to a plain
// or foreign host exactly as a bound one is. The caller defers release.
func bindRequestSOPIN(ctx context.Context, method, requestPIN string) (context.Context, string, func(), error) {
	noop := func() {}
	pin, err := qrdbtransport.ResolveSOPIN(ctx, method, requestPIN)
	if err != nil || pin == "" {
		return ctx, pin, noop, err
	}
	boundPIN, bound, err := qrdbtransport.SOPINFromContext(ctx)
	clear(boundPIN)
	if err != nil {
		return ctx, "", noop, err
	}
	if bound {
		return ctx, pin, noop, nil
	}
	pinBytes := []byte(pin)
	boundCtx, release, err := qrdbtransport.WithSOPIN(ctx, pinBytes)
	clear(pinBytes)
	if err != nil {
		return ctx, "", noop, err
	}
	return boundCtx, pin, release, nil
}

// doJSONResult sends a barrier request whose verb returns a value and
// decodes the response into result. Unlike Transport.doJSONRequest, which
// leaves result untouched on an empty body, it refuses an empty response:
// a zero value (a BarrierStatusResponse with Sealed=false, an empty officer
// list) would report a state the server never sent (go-quicbaas#2538).
func (b *BarrierClient) doJSONResult(ctx context.Context, method, path string, reqBody, result any) error {
	return doJSONResult(ctx, b.t, "rest.BarrierClient", method, path, reqBody, result)
}

// doJSONResult sends a JSON request through t and decodes the response into
// result, refusing an empty body. component names the caller in the errors
// it returns.
func doJSONResult(ctx context.Context, t *Transport, component, method, path string, reqBody, result any) error {
	var body []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, component,
				"failed to encode request body")
		}
		body = data
	}

	resp, err := t.doRawRequest(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	if len(resp.Body) == 0 {
		return errors.New(errors.CodeInternal, component,
			"empty response from "+path)
	}
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return errors.Wrap(err, errors.CodeInternal, component,
			"failed to decode response")
	}
	return nil
}

// officerRegisterWireRequest is the REST wire shape of an officer
// registration: csr_pem travels as plain PEM text, not the base64 that
// encoding/json would make of the transport-level []byte.
type officerRegisterWireRequest struct {
	Username          string `json:"username"`
	CSRPEM            string `json:"csr_pem"`
	AuthenticatorType string `json:"authenticator_type,omitempty"`
	AuthenticatorRef  string `json:"authenticator_ref,omitempty"`
}

// officerRegisterWireResponse is the REST wire shape of an officer
// registration response; cert_pem is plain PEM text.
type officerRegisterWireResponse struct {
	CertPEM    string `json:"cert_pem"`
	ShareIndex int    `json:"share_index"`
}

func officerRegisterRequestToWire(req *qrdbtransport.OfficerRegisterRequest) *officerRegisterWireRequest {
	return &officerRegisterWireRequest{
		Username:          req.Username,
		CSRPEM:            string(req.CSRPEM),
		AuthenticatorType: req.AuthenticatorType,
		AuthenticatorRef:  req.AuthenticatorRef,
	}
}

// officersWireResponse is the REST wire shape of the officer listings.
type officersWireResponse struct {
	Officers []*qrdbtransport.OfficerStatus `json:"officers"`
}

// BarrierInitialize maps to POST /v1/barrier/initialize.
func (b *BarrierClient) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "BarrierInitialize", req.SOPin)
	if err != nil {
		return err
	}
	defer release()
	body := *req
	body.SOPin = soPIN
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/initialize", &body, nil)
}

// BarrierUnseal maps to POST /v1/barrier/unseal.
func (b *BarrierClient) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/unseal", req, nil)
}

// BarrierSeal maps to POST /v1/barrier/seal. The SO PIN, when one is
// configured, comes from the PIN bound by qrdbtransport.WithSOPIN.
func (b *BarrierClient) BarrierSeal(ctx context.Context) error {
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierSeal", "")
	if err != nil {
		return err
	}
	var body any
	if soPIN != "" {
		body = map[string]string{"so_pin": soPIN}
	}
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/seal", body, nil)
}

// BarrierStatus maps to GET /v1/barrier/status.
func (b *BarrierClient) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	var resp qrdbtransport.BarrierStatusResponse
	if err := b.doJSONResult(ctx, http.MethodGet, "/v1/barrier/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierInitializeShamir maps to POST /v1/barrier/shamir/initialize.
func (b *BarrierClient) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "BarrierInitializeShamir", req.SOPin)
	if err != nil {
		return nil, err
	}
	defer release()
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierInitializeShamirResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/shamir/initialize", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierUnsealWithShare maps to POST /v1/barrier/shamir/unseal-share.
func (b *BarrierClient) BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	var resp qrdbtransport.BarrierUnsealShareResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/shamir/unseal-share", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierUnsealWithShares maps to POST /v1/barrier/shamir/unseal-shares.
func (b *BarrierClient) BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error {
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/shamir/unseal-shares", req, nil)
}

// BarrierRekey maps to POST /v1/barrier/rekey.
func (b *BarrierClient) BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "BarrierRekey", req.SOPin)
	if err != nil {
		return nil, err
	}
	defer release()
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierRekeyResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/rekey", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierEnableEncryption maps to POST /v1/barrier/enable-encryption.
func (b *BarrierClient) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	var resp qrdbtransport.BarrierEnableEncryptionResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/enable-encryption", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierGenerateRecoveryKeys maps to POST /v1/barrier/recovery/generate.
func (b *BarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	ctx, soPIN, release, err := bindRequestSOPIN(ctx, "BarrierGenerateRecoveryKeys", req.SOPin)
	if err != nil {
		return nil, err
	}
	defer release()
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierRecoveryKeysResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/recovery/generate", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierRecoverWithKeys maps to POST /v1/barrier/recovery/recover.
func (b *BarrierClient) BarrierRecoverWithKeys(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/recovery/recover", req, nil)
}

// BarrierDeleteRecoveryKeys maps to DELETE /v1/barrier/recovery.
func (b *BarrierClient) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	return b.t.doJSONRequest(ctx, http.MethodDelete, "/v1/barrier/recovery", nil, nil)
}

// BarrierHasRecoveryKeys maps to GET /v1/barrier/recovery/status.
func (b *BarrierClient) BarrierHasRecoveryKeys(ctx context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	var resp qrdbtransport.BarrierHasRecoveryKeysResponse
	if err := b.doJSONResult(ctx, http.MethodGet, "/v1/barrier/recovery/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierGenerateRootToken maps to POST /v1/barrier/root-token.
func (b *BarrierClient) BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	var resp qrdbtransport.BarrierRootTokenResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/root-token", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// OfficerRegister maps to POST /v1/barrier/officers.
func (b *BarrierClient) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return b.registerOfficer(ctx, "/v1/barrier/officers", req)
}

// OfficerList maps to GET /v1/barrier/officers.
func (b *BarrierClient) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return b.listOfficers(ctx, "/v1/barrier/officers")
}

// OfficerClaimStatus maps to GET /v1/barrier/officers/claim-status.
func (b *BarrierClient) OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return b.listOfficers(ctx, "/v1/barrier/officers/claim-status")
}

// ShareClaimBegin maps to POST /v1/barrier/officers/{username}/claim/begin.
func (b *BarrierClient) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return b.claimBegin(ctx, "/v1/barrier/officers/"+req.Username+"/claim/begin", req)
}

// ShareClaimComplete maps to POST /v1/barrier/officers/{username}/claim/complete.
func (b *BarrierClient) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return b.claimComplete(ctx, "/v1/barrier/officers/"+req.Username+"/claim/complete", req)
}

// ShamirSealVote maps to POST /v1/barrier/shamir/seal-vote.
func (b *BarrierClient) ShamirSealVote(ctx context.Context, req *qrdbtransport.ShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	var resp qrdbtransport.ShamirSealVoteResponse
	if err := b.doJSONResult(ctx, http.MethodPost, "/v1/barrier/shamir/seal-vote", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RecoveryOfficerRegister maps to POST /v1/barrier/recovery/officers.
func (b *BarrierClient) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return b.registerOfficer(ctx, "/v1/barrier/recovery/officers", req)
}

// RecoveryShareClaimBegin maps to
// POST /v1/barrier/recovery/officers/{username}/claim/begin.
func (b *BarrierClient) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return b.claimBegin(ctx, "/v1/barrier/recovery/officers/"+req.Username+"/claim/begin", req)
}

// RecoveryShareClaimComplete maps to
// POST /v1/barrier/recovery/officers/{username}/claim/complete.
func (b *BarrierClient) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return b.claimComplete(ctx, "/v1/barrier/recovery/officers/"+req.Username+"/claim/complete", req)
}

func (b *BarrierClient) registerOfficer(ctx context.Context, path string, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	var wire officerRegisterWireResponse
	if err := b.doJSONResult(ctx, http.MethodPost, path, officerRegisterRequestToWire(req), &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.OfficerRegisterResponse{
		CertPEM:    []byte(wire.CertPEM),
		ShareIndex: wire.ShareIndex,
	}, nil
}

func (b *BarrierClient) listOfficers(ctx context.Context, path string) ([]*qrdbtransport.OfficerStatus, error) {
	var wire officersWireResponse
	if err := b.doJSONResult(ctx, http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Officers, nil
}

func (b *BarrierClient) claimBegin(ctx context.Context, path string, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	var resp qrdbtransport.ShareClaimBeginResponse
	if err := b.doJSONResult(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (b *BarrierClient) claimComplete(ctx context.Context, path string, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	var resp qrdbtransport.ShareClaimCompleteResponse
	if err := b.doJSONResult(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

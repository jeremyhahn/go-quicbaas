// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rest

import (
	"context"
	"net/http"
	"net/url"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// BarrierClient serves the qrdb barrier verbs through this transport's own
// HTTP client. Each method sends the same HTTP method, path and body as the
// matching method of go-qrdb's REST transport (transport/rest/
// transport_barrier.go). The QUIC transport embeds this transport with an
// HTTP/3 client, so with --transport quic the barrier verbs go over HTTP/3
// and carry the SDK's bearer (go-quicbaas#2533).
type BarrierClient struct {
	t *Transport
}

var _ qrdbtransport.BarrierClient = (*BarrierClient)(nil)

// Barrier returns a qrdb BarrierClient that sends its requests through t.
func (t *Transport) Barrier() *BarrierClient {
	return &BarrierClient{t: t}
}

// officerRegisterWireRequest is the REST wire shape of an officer
// registration: csr_pem travels as plain PEM text, never base64 (as in
// go-qrdb's REST transport).
type officerRegisterWireRequest struct {
	Username          string `json:"username"`
	CSRPEM            string `json:"csr_pem"`
	AuthenticatorType string `json:"authenticator_type,omitempty"`
	AuthenticatorRef  string `json:"authenticator_ref,omitempty"`
}

// officerRegisterWireResponse is the REST wire shape of an officer
// registration response: cert_pem is plain PEM text.
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

func (b *BarrierClient) registerOfficer(ctx context.Context, path string, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	var wire officerRegisterWireResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, path, officerRegisterRequestToWire(req), &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.OfficerRegisterResponse{
		CertPEM:    []byte(wire.CertPEM),
		ShareIndex: wire.ShareIndex,
	}, nil
}

func (b *BarrierClient) listOfficers(ctx context.Context, path string) ([]*qrdbtransport.OfficerStatus, error) {
	var wire struct {
		Officers []*qrdbtransport.OfficerStatus `json:"officers"`
	}
	if err := b.t.doJSONRequest(ctx, http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Officers, nil
}

// BarrierInitialize maps to POST /v1/barrier/initialize.
func (b *BarrierClient) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/initialize", req, nil)
}

// BarrierUnseal maps to POST /v1/barrier/unseal.
func (b *BarrierClient) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	return b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/unseal", req, nil)
}

// BarrierSeal maps to POST /v1/barrier/seal. The SO PIN, when one is
// bound by qrdbtransport.WithSOPIN, travels as the so_pin body field.
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
	if err := b.t.doJSONRequest(ctx, http.MethodGet, "/v1/barrier/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierInitializeShamir maps to POST /v1/barrier/shamir/initialize.
func (b *BarrierClient) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierInitializeShamir", req.SOPin)
	if err != nil {
		return nil, err
	}
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierInitializeShamirResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/shamir/initialize", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierUnsealWithShare maps to POST /v1/barrier/shamir/unseal-share.
func (b *BarrierClient) BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	var resp qrdbtransport.BarrierUnsealShareResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/shamir/unseal-share", req, &resp); err != nil {
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
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierRekey", req.SOPin)
	if err != nil {
		return nil, err
	}
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierRekeyResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/rekey", &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierEnableEncryption maps to POST /v1/barrier/enable-encryption.
func (b *BarrierClient) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	var resp qrdbtransport.BarrierEnableEncryptionResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/enable-encryption", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierGenerateRecoveryKeys maps to POST /v1/barrier/recovery/generate.
func (b *BarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierGenerateRecoveryKeys", req.SOPin)
	if err != nil {
		return nil, err
	}
	body := *req
	body.SOPin = soPIN

	var resp qrdbtransport.BarrierRecoveryKeysResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/recovery/generate", &body, &resp); err != nil {
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
	if err := b.t.doJSONRequest(ctx, http.MethodGet, "/v1/barrier/recovery/status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierGenerateRootToken maps to POST /v1/barrier/root-token.
func (b *BarrierClient) BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	var resp qrdbtransport.BarrierRootTokenResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/root-token", req, &resp); err != nil {
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
	var resp qrdbtransport.ShareClaimBeginResponse
	path := "/v1/barrier/officers/" + url.PathEscape(req.Username) + "/claim/begin"
	if err := b.t.doJSONRequest(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ShareClaimComplete maps to POST /v1/barrier/officers/{username}/claim/complete.
func (b *BarrierClient) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	var resp qrdbtransport.ShareClaimCompleteResponse
	path := "/v1/barrier/officers/" + url.PathEscape(req.Username) + "/claim/complete"
	if err := b.t.doJSONRequest(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ShamirSealVote maps to POST /v1/barrier/shamir/seal-vote.
func (b *BarrierClient) ShamirSealVote(ctx context.Context, req *qrdbtransport.ShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	var resp qrdbtransport.ShamirSealVoteResponse
	if err := b.t.doJSONRequest(ctx, http.MethodPost, "/v1/barrier/shamir/seal-vote", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RecoveryOfficerRegister maps to POST /v1/barrier/recovery/officers.
func (b *BarrierClient) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return b.registerOfficer(ctx, "/v1/barrier/recovery/officers", req)
}

// RecoveryShareClaimBegin maps to POST
// /v1/barrier/recovery/officers/{username}/claim/begin.
func (b *BarrierClient) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	var resp qrdbtransport.ShareClaimBeginResponse
	path := "/v1/barrier/recovery/officers/" + url.PathEscape(req.Username) + "/claim/begin"
	if err := b.t.doJSONRequest(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RecoveryShareClaimComplete maps to POST
// /v1/barrier/recovery/officers/{username}/claim/complete.
func (b *BarrierClient) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	var resp qrdbtransport.ShareClaimCompleteResponse
	path := "/v1/barrier/recovery/officers/" + url.PathEscape(req.Username) + "/claim/complete"
	if err := b.t.doJSONRequest(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

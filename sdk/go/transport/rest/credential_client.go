package rest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// CredentialClient serves the qrdb named-credential verbs over the SDK's own
// REST transport, so every credential request carries the headers the
// transport was built with, the platform bearer set by WithAuthToken among
// them. go-qrdb's REST sub-client has no way to add an Authorization header,
// so over REST an operational node's default-deny gate refused it with 401.
//
// Each method keeps the HTTP method, path, request body and response type
// of the matching method in go-qrdb sdk/go transport/rest
// (transport_credentials.go).
//
// The verbs that carry a credential value mark their request context with
// withSecretHop, so the transport refuses any redirect hop and any non-https
// hop before the value is sent or returned: CredentialSubmit,
// CredentialRotate and CredentialStore (which send one), and
// CredentialRetrieveVersion and CredentialRetrieve (whose responses carry
// one).
type CredentialClient struct {
	t *Transport
}

var _ qrdbtransport.CredentialClient = (*CredentialClient)(nil)

// Credentials returns the credential client bound to this transport.
func (t *Transport) Credentials() *CredentialClient {
	return &CredentialClient{t: t}
}

const credentialComponent = "rest.CredentialClient"

// doJSONResult sends a credential request and decodes the response into
// result, refusing an empty body.
func (c *CredentialClient) doJSONResult(ctx context.Context, method, path string, reqBody, result any) error {
	return doJSONResult(ctx, c.t, credentialComponent, method, path, reqBody, result)
}

// credentialPath returns /v1/credentials/{name}, with name path-escaped.
func credentialPath(name string) string {
	return "/v1/credentials/" + url.PathEscape(name)
}

// refuseNilCredentialRequest and refuseEmptyCredentialName refuse a verb
// whose request is nil or names no credential, as go-qrdb's REST transport
// does, before anything is sent.
func refuseNilCredentialRequest(verb string) error {
	return errors.New(errors.CodeInvalidInput, credentialComponent, verb+": request is nil")
}

func refuseEmptyCredentialName(verb string) error {
	return errors.New(errors.CodeInvalidInput, credentialComponent, verb+": credential name is empty")
}

// CredentialSubmit submits a named credential value. Maps to
// POST /v1/credentials/submit (204 No Content).
func (c *CredentialClient) CredentialSubmit(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error {
	ctx = withSecretHop(ctx, "CredentialSubmit")
	return c.t.doJSONRequest(ctx, http.MethodPost, "/v1/credentials/submit", req, nil)
}

// CredentialStrategy returns the active credential management strategy.
// Maps to GET /v1/credentials/strategy.
func (c *CredentialClient) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	var resp qrdbtransport.CredentialStrategyResponse
	if err := c.doJSONResult(ctx, http.MethodGet, "/v1/credentials/strategy", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialDelete removes a named credential. Maps to
// DELETE /v1/credentials/{name} (204 No Content or 200).
func (c *CredentialClient) CredentialDelete(ctx context.Context, name string) error {
	return c.t.doJSONRequest(ctx, http.MethodDelete, credentialPath(name), nil, nil)
}

// CredentialList returns the sorted list of every stored credential name.
// Maps to GET /v1/credentials. req is reserved for future filters, as in
// go-qrdb, and is not sent.
func (c *CredentialClient) CredentialList(ctx context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	var resp qrdbtransport.CredentialListResponse
	if err := c.doJSONResult(ctx, http.MethodGet, "/v1/credentials", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialRotate atomically replaces the current value of a credential.
// Maps to POST /v1/credentials/{name}/rotate.
func (c *CredentialClient) CredentialRotate(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	if req == nil {
		return nil, refuseNilCredentialRequest("CredentialRotate")
	}
	if req.Name == "" {
		return nil, refuseEmptyCredentialName("CredentialRotate")
	}
	ctx = withSecretHop(ctx, "CredentialRotate")
	var resp qrdbtransport.CredentialRotateResponse
	if err := c.doJSONResult(ctx, http.MethodPost, credentialPath(req.Name)+"/rotate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialRetrieveVersion returns the credential value for a specific
// version (0 is the current one). Maps to
// GET /v1/credentials/{name}/versions/{version}.
func (c *CredentialClient) CredentialRetrieveVersion(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
	if req == nil {
		return nil, refuseNilCredentialRequest("CredentialRetrieveVersion")
	}
	if req.Name == "" {
		return nil, refuseEmptyCredentialName("CredentialRetrieveVersion")
	}
	ctx = withSecretHop(ctx, "CredentialRetrieveVersion")
	path := credentialPath(req.Name) + "/versions/" + strconv.FormatUint(req.Version, 10)
	var resp qrdbtransport.CredentialRetrieveVersionResponse
	if err := c.doJSONResult(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialListVersions returns the version history of a credential.
// Values are never returned. Maps to GET /v1/credentials/{name}/versions.
func (c *CredentialClient) CredentialListVersions(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	if req == nil {
		return nil, refuseNilCredentialRequest("CredentialListVersions")
	}
	if req.Name == "" {
		return nil, refuseEmptyCredentialName("CredentialListVersions")
	}
	var resp qrdbtransport.CredentialListVersionsResponse
	if err := c.doJSONResult(ctx, http.MethodGet, credentialPath(req.Name)+"/versions", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CredentialGCExpired reaps historical versions whose grace period has
// elapsed. Maps to POST /v1/credentials/{name}/gc.
func (c *CredentialClient) CredentialGCExpired(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	if req == nil {
		return nil, refuseNilCredentialRequest("CredentialGCExpired")
	}
	if req.Name == "" {
		return nil, refuseEmptyCredentialName("CredentialGCExpired")
	}
	var resp qrdbtransport.CredentialGCResponse
	if err := c.doJSONResult(ctx, http.MethodPost, credentialPath(req.Name)+"/gc", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// credentialValueWire is the response of GET /v1/credentials/{name}/value:
// the value is a []byte field, so encoding/json decodes it from base64 and
// binary content round-trips.
type credentialValueWire struct {
	Value []byte `json:"value"`
}

// CredentialStore persists a credential value. Maps to
// POST /v1/credentials/store with {"name","value"}.
func (c *CredentialClient) CredentialStore(ctx context.Context, name string, value []byte) error {
	ctx = withSecretHop(ctx, "CredentialStore")
	body := struct {
		Name  string `json:"name"`
		Value []byte `json:"value"`
	}{Name: name, Value: value}
	return c.t.doJSONRequest(ctx, http.MethodPost, "/v1/credentials/store", body, nil)
}

// CredentialRetrieve returns the credential value. Maps to
// GET /v1/credentials/{name}/value, answering {"value"}.
func (c *CredentialClient) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	ctx = withSecretHop(ctx, "CredentialRetrieve")
	var resp credentialValueWire
	if err := c.doJSONResult(ctx, http.MethodGet, credentialPath(name)+"/value", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

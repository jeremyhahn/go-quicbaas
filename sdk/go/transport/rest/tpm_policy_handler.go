package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The go-xkms TPM-policy REST surface, under the /api/v1/xkms mount
// prefix quicbaasd serves the go-xkms module at: named policies under
// /tpm/policies, the policy authority under /tpm/authority, the policy of
// a key under /keys/{id}/tpm-policy, and a tenant's TPM mode and vTPM
// under /tenants/{tenantID}/tpm.
const (
	tpmPoliciesPath       = "/api/v1/xkms/tpm/policies"
	tpmAuthorityRotateURL = "/api/v1/xkms/tpm/authority/rotate"
	tpmKeysPath           = "/api/v1/xkms/keys/"
	tpmTenantsPath        = "/api/v1/xkms/tenants/"
)

// tpmPolicyMethods maps the go-xkms TPMPolicyService verbs to their REST
// bridges.
var tpmPolicyMethods = map[string]func(*securityRESTHandler, context.Context, interface{}) (interface{}, error){
	"TPMPolicyCreate":          (*securityRESTHandler).tpmPolicyCreate,
	"TPMPolicyGet":             (*securityRESTHandler).tpmPolicyGet,
	"TPMPolicyList":            (*securityRESTHandler).tpmPolicyList,
	"TPMPolicyUpdate":          (*securityRESTHandler).tpmPolicyUpdate,
	"TPMPolicyRevoke":          (*securityRESTHandler).tpmPolicyRevoke,
	"TPMPolicyVerify":          (*securityRESTHandler).tpmPolicyVerify,
	"TPMPolicyDelete":          (*securityRESTHandler).tpmPolicyDelete,
	"TPMPolicyObjects":         (*securityRESTHandler).tpmPolicyObjects,
	"TPMPolicySetAutoUnseal":   (*securityRESTHandler).tpmPolicySetAutoUnseal,
	"TPMPolicySetDescription":  (*securityRESTHandler).tpmPolicySetDescription,
	"TPMObjectPolicy":          (*securityRESTHandler).tpmObjectPolicy,
	"TPMPolicyRotateAuthority": (*securityRESTHandler).tpmPolicyRotateAuthority,
	"GetTenantTPMMode":         (*securityRESTHandler).tpmGetTenantMode,
	"SetTenantTPMMode":         (*securityRESTHandler).tpmSetTenantMode,
	"TenantVTPMStatus":         (*securityRESTHandler).tpmVTPMStatus,
	"TenantVTPMStart":          (*securityRESTHandler).tpmVTPMStart,
	"TenantVTPMStop":           (*securityRESTHandler).tpmVTPMStop,
}

// handleTPMPolicy dispatches a "quicbaas.TPMPolicy" verb to its go-xkms
// REST route.
func (h *securityRESTHandler) handleTPMPolicy(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := tpmPolicyMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.security.TPMPolicy."+method,
			"unsupported TPM policy method")
	}
	return fn(h, ctx, req)
}

func tpmPolicyOp(method string) string { return "rest.security.TPMPolicy." + method }

// tpmTenantQuery returns the ?tenant_id= query go-xkms reads for a
// cross-tenant caller, or "" for the caller's own tenant.
func tpmTenantQuery(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return "?" + url.Values{"tenant_id": {tenantID}}.Encode()
}

func tpmPolicyNamePath(name, suffix string) string {
	return tpmPoliciesPath + "/" + url.PathEscape(name) + suffix
}

func tpmTenantPath(tenantID, suffix string) string {
	return tpmTenantsPath + url.PathEscape(tenantID) + "/tpm" + suffix
}

// tpmPolicyDo sends one request and decodes the go-xkms response into out.
// A nil body sends no request body.
func tpmPolicyDo[T any](h *securityRESTHandler, ctx context.Context, method, httpMethod, path string, body interface{}) (*T, error) {
	op := tpmPolicyOp(method)
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "failed to encode request")
		}
		raw = b
	}
	resp, err := h.transport.doRawRequest(ctx, httpMethod, path, raw, nil)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode response")
	}
	return &out, nil
}

func tpmPolicyBadRequest(method, want string) error {
	return errors.NewInvalidInput(tpmPolicyOp(method), "invalid request type, expected "+want)
}

func tpmNamedRequest(method string, req interface{}) (*xkmstransport.TPMPolicyRequest, error) {
	r, ok := req.(*xkmstransport.TPMPolicyRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest(method, "*xkmstransport.TPMPolicyRequest")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp(method), "policy name is required")
	}
	return r, nil
}

func tpmTenantID(method string, req interface{}) (string, error) {
	id, ok := req.(string)
	if !ok || id == "" {
		return "", errors.NewInvalidInput(tpmPolicyOp(method), "tenant ID is required")
	}
	return id, nil
}

func (h *securityRESTHandler) tpmPolicyCreate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMPolicyCreateRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicyCreate", "*xkmstransport.TPMPolicyCreateRequest")
	}
	return tpmPolicyDo[xkmstransport.TPMPolicy](h, ctx, "TPMPolicyCreate", http.MethodPost, tpmPoliciesPath, r)
}

func (h *securityRESTHandler) tpmPolicyGet(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := tpmNamedRequest("TPMPolicyGet", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TPMPolicy](h, ctx, "TPMPolicyGet", http.MethodGet,
		tpmPolicyNamePath(r.Name, "")+tpmTenantQuery(r.TenantID), nil)
}

func (h *securityRESTHandler) tpmPolicyList(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMPolicyListRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicyList", "*xkmstransport.TPMPolicyListRequest")
	}
	return tpmPolicyDo[xkmstransport.TPMPolicyListResponse](h, ctx, "TPMPolicyList", http.MethodGet,
		tpmPoliciesPath+tpmTenantQuery(r.TenantID), nil)
}

// tpmPolicyAction posts a named-policy action (update, revoke, verify);
// go-xkms reads the tenant from the body and the name from the path.
func tpmPolicyAction[T any](h *securityRESTHandler, ctx context.Context, method, action string, req interface{}) (interface{}, error) {
	r, err := tpmNamedRequest(method, req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[T](h, ctx, method, http.MethodPost, tpmPolicyNamePath(r.Name, "/"+action), r)
}

func (h *securityRESTHandler) tpmPolicyUpdate(ctx context.Context, req interface{}) (interface{}, error) {
	return tpmPolicyAction[xkmstransport.TPMPolicy](h, ctx, "TPMPolicyUpdate", "update", req)
}

func (h *securityRESTHandler) tpmPolicyRevoke(ctx context.Context, req interface{}) (interface{}, error) {
	return tpmPolicyAction[xkmstransport.TPMPolicy](h, ctx, "TPMPolicyRevoke", "revoke", req)
}

func (h *securityRESTHandler) tpmPolicyVerify(ctx context.Context, req interface{}) (interface{}, error) {
	return tpmPolicyAction[xkmstransport.TPMPolicyVerifyResponse](h, ctx, "TPMPolicyVerify", "verify", req)
}

func (h *securityRESTHandler) tpmPolicyDelete(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMPolicyDeleteRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicyDelete", "*xkmstransport.TPMPolicyDeleteRequest")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp("TPMPolicyDelete"), "policy name is required")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, tpmPolicyOp("TPMPolicyDelete"), "failed to encode request")
	}
	if _, err := h.transport.doRawRequest(ctx, http.MethodDelete,
		tpmPolicyNamePath(r.Name, "")+tpmTenantQuery(r.TenantID), body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *securityRESTHandler) tpmPolicyObjects(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := tpmNamedRequest("TPMPolicyObjects", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TPMPolicyObjectsResponse](h, ctx, "TPMPolicyObjects", http.MethodGet,
		tpmPolicyNamePath(r.Name, "/objects")+tpmTenantQuery(r.TenantID), nil)
}

func (h *securityRESTHandler) tpmPolicySetAutoUnseal(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMPolicyAutoUnsealRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicySetAutoUnseal", "*xkmstransport.TPMPolicyAutoUnsealRequest")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp("TPMPolicySetAutoUnseal"), "policy name is required")
	}
	return tpmPolicyDo[xkmstransport.TPMPolicy](h, ctx, "TPMPolicySetAutoUnseal", http.MethodPost,
		tpmPolicyNamePath(r.Name, "/auto-unseal"), r)
}

func (h *securityRESTHandler) tpmPolicySetDescription(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMPolicyDescriptionRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicySetDescription", "*xkmstransport.TPMPolicyDescriptionRequest")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp("TPMPolicySetDescription"), "policy name is required")
	}
	return tpmPolicyDo[xkmstransport.TPMPolicy](h, ctx, "TPMPolicySetDescription", http.MethodPost,
		tpmPolicyNamePath(r.Name, "/description"), r)
}

func (h *securityRESTHandler) tpmObjectPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMObjectPolicyRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMObjectPolicy", "*xkmstransport.TPMObjectPolicyRequest")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp("TPMObjectPolicy"), "key ID is required")
	}
	q := url.Values{}
	if r.TenantID != "" {
		q.Set("tenant_id", r.TenantID)
	}
	if r.Kind != "" {
		q.Set("kind", r.Kind)
	}
	path := tpmKeysPath + url.PathEscape(r.KeyID) + "/tpm-policy"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return tpmPolicyDo[xkmstransport.TPMObjectPolicyResponse](h, ctx, "TPMObjectPolicy", http.MethodGet, path, nil)
}

func (h *securityRESTHandler) tpmPolicyRotateAuthority(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.TPMAuthorityRotateRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("TPMPolicyRotateAuthority", "*xkmstransport.TPMAuthorityRotateRequest")
	}
	return tpmPolicyDo[xkmstransport.TPMAuthorityRotateResponse](h, ctx, "TPMPolicyRotateAuthority",
		http.MethodPost, tpmAuthorityRotateURL, r)
}

func (h *securityRESTHandler) tpmGetTenantMode(ctx context.Context, req interface{}) (interface{}, error) {
	id, err := tpmTenantID("GetTenantTPMMode", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TenantTPMModeResponse](h, ctx, "GetTenantTPMMode", http.MethodGet,
		tpmTenantPath(id, ""), nil)
}

func (h *securityRESTHandler) tpmSetTenantMode(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*xkmstransport.SetTenantTPMModeRequest)
	if !ok || r == nil {
		return nil, tpmPolicyBadRequest("SetTenantTPMMode", "*xkmstransport.SetTenantTPMModeRequest")
	}
	if r.TenantID == "" {
		return nil, errors.NewInvalidInput(tpmPolicyOp("SetTenantTPMMode"), "tenant ID is required")
	}
	return tpmPolicyDo[xkmstransport.TenantTPMModeResponse](h, ctx, "SetTenantTPMMode", http.MethodPut,
		tpmTenantPath(r.TenantID, ""), r)
}

func (h *securityRESTHandler) tpmVTPMStatus(ctx context.Context, req interface{}) (interface{}, error) {
	id, err := tpmTenantID("TenantVTPMStatus", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TenantVTPMStatusResponse](h, ctx, "TenantVTPMStatus", http.MethodGet,
		tpmTenantPath(id, "/vtpm"), nil)
}

func (h *securityRESTHandler) tpmVTPMStart(ctx context.Context, req interface{}) (interface{}, error) {
	id, err := tpmTenantID("TenantVTPMStart", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TenantVTPMStatusResponse](h, ctx, "TenantVTPMStart", http.MethodPost,
		tpmTenantPath(id, "/vtpm/start"), nil)
}

func (h *securityRESTHandler) tpmVTPMStop(ctx context.Context, req interface{}) (interface{}, error) {
	id, err := tpmTenantID("TenantVTPMStop", req)
	if err != nil {
		return nil, err
	}
	return tpmPolicyDo[xkmstransport.TenantVTPMStatusResponse](h, ctx, "TenantVTPMStop", http.MethodPost,
		tpmTenantPath(id, "/vtpm/stop"), nil)
}

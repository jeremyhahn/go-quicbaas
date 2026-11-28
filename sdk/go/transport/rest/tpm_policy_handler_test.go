package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const tpmPolicyRESTService = "quicbaas.TPMPolicy"

type tpmPolicyRESTCase struct {
	method     string
	req        interface{}
	httpMethod string
	path       string
	query      string
	wantBody   bool
	respType   interface{}
}

func tpmPolicyRESTCases() []tpmPolicyRESTCase {
	named := &xkmstransport.TPMPolicyRequest{Name: "policy-a"}
	return []tpmPolicyRESTCase{
		{"TPMPolicyCreate", &xkmstransport.TPMPolicyCreateRequest{Name: "policy-a"}, http.MethodPost,
			"/api/v1/xkms/tpm/policies", "", true, &xkmstransport.TPMPolicy{}},
		{"TPMPolicyGet", &xkmstransport.TPMPolicyRequest{Name: "policy-a", TenantID: "tenant-a"}, http.MethodGet,
			"/api/v1/xkms/tpm/policies/policy-a", "tenant_id=tenant-a", false, &xkmstransport.TPMPolicy{}},
		{"TPMPolicyList", &xkmstransport.TPMPolicyListRequest{}, http.MethodGet,
			"/api/v1/xkms/tpm/policies", "", false, &xkmstransport.TPMPolicyListResponse{}},
		{"TPMPolicyUpdate", named, http.MethodPost,
			"/api/v1/xkms/tpm/policies/policy-a/update", "", true, &xkmstransport.TPMPolicy{}},
		{"TPMPolicyRevoke", named, http.MethodPost,
			"/api/v1/xkms/tpm/policies/policy-a/revoke", "", true, &xkmstransport.TPMPolicy{}},
		{"TPMPolicyVerify", named, http.MethodPost,
			"/api/v1/xkms/tpm/policies/policy-a/verify", "", true, &xkmstransport.TPMPolicyVerifyResponse{}},
		{"TPMPolicyObjects", named, http.MethodGet,
			"/api/v1/xkms/tpm/policies/policy-a/objects", "", false, &xkmstransport.TPMPolicyObjectsResponse{}},
		{"TPMPolicySetAutoUnseal", &xkmstransport.TPMPolicyAutoUnsealRequest{Name: "policy-a", Enabled: true},
			http.MethodPost, "/api/v1/xkms/tpm/policies/policy-a/auto-unseal", "", true, &xkmstransport.TPMPolicy{}},
		{"TPMPolicySetDescription", &xkmstransport.TPMPolicyDescriptionRequest{Name: "policy-a", Description: "d"},
			http.MethodPost, "/api/v1/xkms/tpm/policies/policy-a/description", "", true, &xkmstransport.TPMPolicy{}},
		{"TPMObjectPolicy", &xkmstransport.TPMObjectPolicyRequest{KeyID: "key-a", Kind: "sealed"}, http.MethodGet,
			"/api/v1/xkms/keys/key-a/tpm-policy", "kind=sealed", false, &xkmstransport.TPMObjectPolicyResponse{}},
		{"TPMPolicyRotateAuthority", &xkmstransport.TPMAuthorityRotateRequest{}, http.MethodPost,
			"/api/v1/xkms/tpm/authority/rotate", "", true, &xkmstransport.TPMAuthorityRotateResponse{}},
		{"GetTenantTPMMode", "tenant-a", http.MethodGet,
			"/api/v1/xkms/tenants/tenant-a/tpm", "", false, &xkmstransport.TenantTPMModeResponse{}},
		{"SetTenantTPMMode", &xkmstransport.SetTenantTPMModeRequest{TenantID: "tenant-a", Mode: "vtpm"},
			http.MethodPut, "/api/v1/xkms/tenants/tenant-a/tpm", "", true, &xkmstransport.TenantTPMModeResponse{}},
		{"TenantVTPMStatus", "tenant-a", http.MethodGet,
			"/api/v1/xkms/tenants/tenant-a/tpm/vtpm", "", false, &xkmstransport.TenantVTPMStatusResponse{}},
		{"TenantVTPMStart", "tenant-a", http.MethodPost,
			"/api/v1/xkms/tenants/tenant-a/tpm/vtpm/start", "", false, &xkmstransport.TenantVTPMStatusResponse{}},
		{"TenantVTPMStop", "tenant-a", http.MethodPost,
			"/api/v1/xkms/tenants/tenant-a/tpm/vtpm/stop", "", false, &xkmstransport.TenantVTPMStatusResponse{}},
	}
}

func TestTPMPolicy_REST_EveryVerbHitsGoXKMSRoute(t *testing.T) {
	for _, tc := range tpmPolicyRESTCases() {
		t.Run(tc.method, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
				gotBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			tr := newSecurityTestTransport(t, server, "")
			defer tr.Close()

			result, err := tr.Request(context.Background(), tpmPolicyRESTService, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if gotMethod != tc.httpMethod || gotPath != tc.path || gotQuery != tc.query {
				t.Fatalf("%s hit %s %s?%s, want %s %s?%s", tc.method, gotMethod, gotPath, gotQuery,
					tc.httpMethod, tc.path, tc.query)
			}
			if tc.wantBody {
				want, _ := json.Marshal(tc.req)
				if string(gotBody) != string(want) {
					t.Fatalf("%s body %s, want %s", tc.method, gotBody, want)
				}
			}
			if reflect.TypeOf(result) != reflect.TypeOf(tc.respType) {
				t.Fatalf("%s result type %T, want %T", tc.method, result, tc.respType)
			}
		})
	}
}

func TestTPMPolicy_REST_ListDecodesPolicies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&xkmstransport.TPMPolicyListResponse{
			Policies: []xkmstransport.TPMPolicy{{Name: "policy-a", Approved: true}},
		})
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	result, err := tr.Request(context.Background(), tpmPolicyRESTService, "TPMPolicyList",
		&xkmstransport.TPMPolicyListRequest{})
	if err != nil {
		t.Fatalf("TPMPolicyList: %v", err)
	}
	resp := result.(*xkmstransport.TPMPolicyListResponse)
	if len(resp.Policies) != 1 || resp.Policies[0].Name != "policy-a" || !resp.Policies[0].Approved {
		t.Fatalf("decoded %+v", resp.Policies)
	}
}

func TestTPMPolicy_REST_DeleteSendsDeleteWithNoResult(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	result, err := tr.Request(context.Background(), tpmPolicyRESTService, "TPMPolicyDelete",
		&xkmstransport.TPMPolicyDeleteRequest{Name: "policy-a"})
	if err != nil || result != nil {
		t.Fatalf("TPMPolicyDelete: result %v err %v", result, err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/xkms/tpm/policies/policy-a" {
		t.Fatalf("hit %s %s", gotMethod, gotPath)
	}
}

func TestTPMPolicy_REST_StatusMapsToSDKErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		is     func(error) bool
	}{
		{http.StatusNotFound, errors.IsNotFound},
		{http.StatusForbidden, errors.IsForbidden},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":"policy-a refused"}`))
		}))
		tr := newSecurityTestTransport(t, server, "")
		_, err := tr.Request(context.Background(), tpmPolicyRESTService, "TPMPolicyGet",
			&xkmstransport.TPMPolicyRequest{Name: "policy-a"})
		if !tc.is(err) {
			t.Errorf("status %d: got %v", tc.status, err)
		}
		_ = tr.Close()
		server.Close()
	}
}

func TestTPMPolicy_REST_RefusesBadRequestsBeforeSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called for a refused request")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	for _, tc := range []struct {
		method string
		req    interface{}
	}{
		{"TPMPolicyGet", struct{}{}},
		{"TPMPolicyGet", &xkmstransport.TPMPolicyRequest{}},
		{"GetTenantTPMMode", ""},
		{"TPMObjectPolicy", &xkmstransport.TPMObjectPolicyRequest{}},
	} {
		if _, err := tr.Request(context.Background(), tpmPolicyRESTService, tc.method, tc.req); !errors.IsInvalidInput(err) {
			t.Errorf("%s(%#v): got %v, want invalid input", tc.method, tc.req, err)
		}
	}
	if _, err := tr.Request(context.Background(), tpmPolicyRESTService, "TPMPolicyNope", nil); !errors.IsUnimplemented(err) {
		t.Errorf("unknown method: got %v, want unimplemented", err)
	}
}

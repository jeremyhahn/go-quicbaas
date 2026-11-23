package quicbaas

import (
	"context"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tpmPolicyVerbCase is one TPMPolicyService verb: the request it sends,
// the response the fake transport returns, and a call through the client.
type tpmPolicyVerbCase struct {
	method string
	req    interface{}
	resp   interface{}
	call   func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error)
	// callNil calls the verb with a nil request (or empty tenant ID).
	callNil func(ctx context.Context, c XKMSTPMPolicyService) error
}

func tpmPolicyVerbCases() []tpmPolicyVerbCase {
	create := &xkmstransport.TPMPolicyCreateRequest{Name: "policy-a", Description: "test policy"}
	named := &xkmstransport.TPMPolicyRequest{Name: "policy-a"}
	list := &xkmstransport.TPMPolicyListRequest{TenantID: "tenant-a"}
	auto := &xkmstransport.TPMPolicyAutoUnsealRequest{Name: "policy-a", Enabled: true}
	desc := &xkmstransport.TPMPolicyDescriptionRequest{Name: "policy-a", Description: "updated"}
	obj := &xkmstransport.TPMObjectPolicyRequest{KeyID: "key-a"}
	rot := &xkmstransport.TPMAuthorityRotateRequest{TenantID: "tenant-a"}
	mode := &xkmstransport.SetTenantTPMModeRequest{TenantID: "tenant-a", Mode: "vtpm"}
	policy := &xkmstransport.TPMPolicy{Name: "policy-a"}
	vtpm := &xkmstransport.TenantVTPMStatusResponse{}
	tmode := &xkmstransport.TenantTPMModeResponse{}
	return []tpmPolicyVerbCase{
		{"TPMPolicyCreate", create, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyCreate(ctx, create)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyCreate(ctx, nil)
				return err
			}},
		{"TPMPolicyGet", named, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyGet(ctx, named)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyGet(ctx, nil)
				return err
			}},
		{"TPMPolicyList", list, &xkmstransport.TPMPolicyListResponse{Policies: []xkmstransport.TPMPolicy{*policy}},
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyList(ctx, list)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyList(ctx, nil)
				return err
			}},
		{"TPMPolicyUpdate", named, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyUpdate(ctx, named)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyUpdate(ctx, nil)
				return err
			}},
		{"TPMPolicyRevoke", named, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyRevoke(ctx, named)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyRevoke(ctx, nil)
				return err
			}},
		{"TPMPolicyVerify", named, &xkmstransport.TPMPolicyVerifyResponse{},
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyVerify(ctx, named)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyVerify(ctx, nil)
				return err
			}},
		{"TPMPolicyObjects", named, &xkmstransport.TPMPolicyObjectsResponse{},
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyObjects(ctx, named)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyObjects(ctx, nil)
				return err
			}},
		{"TPMPolicySetAutoUnseal", auto, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicySetAutoUnseal(ctx, auto)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicySetAutoUnseal(ctx, nil)
				return err
			}},
		{"TPMPolicySetDescription", desc, policy,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicySetDescription(ctx, desc)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicySetDescription(ctx, nil)
				return err
			}},
		{"TPMObjectPolicy", obj, &xkmstransport.TPMObjectPolicyResponse{},
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMObjectPolicy(ctx, obj)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMObjectPolicy(ctx, nil)
				return err
			}},
		{"TPMPolicyRotateAuthority", rot, &xkmstransport.TPMAuthorityRotateResponse{},
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TPMPolicyRotateAuthority(ctx, rot)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TPMPolicyRotateAuthority(ctx, nil)
				return err
			}},
		{"SetTenantTPMMode", mode, tmode,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.SetTenantTPMMode(ctx, mode)
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.SetTenantTPMMode(ctx, nil)
				return err
			}},
		{"GetTenantTPMMode", "tenant-a", tmode,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.GetTenantTPMMode(ctx, "tenant-a")
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.GetTenantTPMMode(ctx, "")
				return err
			}},
		{"TenantVTPMStatus", "tenant-a", vtpm,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TenantVTPMStatus(ctx, "tenant-a")
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TenantVTPMStatus(ctx, "")
				return err
			}},
		{"TenantVTPMStart", "tenant-a", vtpm,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TenantVTPMStart(ctx, "tenant-a")
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TenantVTPMStart(ctx, "")
				return err
			}},
		{"TenantVTPMStop", "tenant-a", vtpm,
			func(ctx context.Context, c XKMSTPMPolicyService) (interface{}, error) {
				return c.TenantVTPMStop(ctx, "tenant-a")
			},
			func(ctx context.Context, c XKMSTPMPolicyService) error {
				_, err := c.TenantVTPMStop(ctx, "")
				return err
			}},
	}
}

type tpmPolicyRecorded struct {
	service, method string
	req             interface{}
}

func newTPMPolicyRecordingClient(resp func(method string) interface{}) (XKMSTPMPolicyService, *[]tpmPolicyRecorded) {
	var calls []tpmPolicyRecorded
	mt := &mockTransport{requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
		calls = append(calls, tpmPolicyRecorded{service, method, req})
		return resp(method), nil
	}}
	return &tpmPolicyTransportClient{t: mt}, &calls
}

func TestSecurity_TPMPolicy_EveryVerbSendsServiceMethodAndRequest(t *testing.T) {
	for _, tc := range tpmPolicyVerbCases() {
		t.Run(tc.method, func(t *testing.T) {
			c, calls := newTPMPolicyRecordingClient(func(string) interface{} { return tc.resp })
			got, err := tc.call(context.Background(), c)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if len(*calls) != 1 {
				t.Fatalf("%s: %d sends, want 1", tc.method, len(*calls))
			}
			rec := (*calls)[0]
			if rec.service != "quicbaas.TPMPolicy" || rec.method != tc.method {
				t.Fatalf("sent %s/%s, want quicbaas.TPMPolicy/%s", rec.service, rec.method, tc.method)
			}
			if rec.req != tc.req {
				t.Fatalf("%s: request was not passed verbatim: %#v", tc.method, rec.req)
			}
			if got != tc.resp {
				t.Fatalf("%s: response %#v, want %#v", tc.method, got, tc.resp)
			}
		})
	}
}

func TestSecurity_TPMPolicy_DeleteSendsRequest(t *testing.T) {
	c, calls := newTPMPolicyRecordingClient(func(string) interface{} { return nil })
	req := &xkmstransport.TPMPolicyDeleteRequest{Name: "policy-a"}
	if err := c.TPMPolicyDelete(context.Background(), req); err != nil {
		t.Fatalf("TPMPolicyDelete: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].method != "TPMPolicyDelete" || (*calls)[0].req != req {
		t.Fatalf("TPMPolicyDelete sends: %#v", *calls)
	}
	if err := c.TPMPolicyDelete(context.Background(), nil); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil delete request: got %v, want invalid input", err)
	}
	var nilCtx context.Context
	if err := c.TPMPolicyDelete(nilCtx, req); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil ctx delete: got %v, want invalid input", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("refused deletes were sent: %d sends", len(*calls))
	}
}

func TestSecurity_TPMPolicy_NilRequestAndCtxRefusedBeforeSend(t *testing.T) {
	for _, tc := range tpmPolicyVerbCases() {
		t.Run(tc.method, func(t *testing.T) {
			c, calls := newTPMPolicyRecordingClient(func(string) interface{} { return tc.resp })
			if err := tc.callNil(context.Background(), c); !sdkerrors.IsInvalidInput(err) {
				t.Fatalf("%s nil request: got %v, want invalid input", tc.method, err)
			}
			var nilCtx context.Context
			if _, err := tc.call(nilCtx, c); !sdkerrors.IsInvalidInput(err) {
				t.Fatalf("%s nil ctx: got %v, want invalid input", tc.method, err)
			}
			if len(*calls) != 0 {
				t.Fatalf("%s: refused calls were sent: %d", tc.method, len(*calls))
			}
		})
	}
}

func TestSecurity_TPMPolicy_WrongResponseTypeIsInternal(t *testing.T) {
	for _, tc := range tpmPolicyVerbCases() {
		t.Run(tc.method, func(t *testing.T) {
			c, _ := newTPMPolicyRecordingClient(func(string) interface{} { return struct{}{} })
			if _, err := tc.call(context.Background(), c); sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Fatalf("%s: got %v, want internal error", tc.method, err)
			}
		})
	}
}

func TestSecurity_TPMPolicy_AccessorIsStableAndNonNil(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	first := c.Security().TPMPolicy()
	if first == nil {
		t.Fatal("Security().TPMPolicy() is nil")
	}
	if second := c.Security().TPMPolicy(); second != first {
		t.Fatal("Security().TPMPolicy() returned a different client on the second call")
	}
}

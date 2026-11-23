package grpc

import (
	"context"
	"strings"
	"sync"
	"testing"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tpmPolicyFakeServer serves TPMPolicyList and TPMPolicyGet; every other
// xkms.v1 RPC answers codes.Unimplemented from the embedded server.
type tpmPolicyFakeServer struct {
	xkmspb.UnimplementedKeystoreServiceServer
	mu         sync.Mutex
	listTenant string
}

func (f *tpmPolicyFakeServer) TPMPolicyList(_ context.Context, req *xkmspb.TPMPolicyListRequest) (*xkmspb.TPMPolicyListResponse, error) {
	f.mu.Lock()
	f.listTenant = req.GetTenantId()
	f.mu.Unlock()
	return &xkmspb.TPMPolicyListResponse{Policies: []*xkmspb.TPMPolicy{{Name: "policy-a", Approved: true}}}, nil
}

func (f *tpmPolicyFakeServer) TPMPolicyGet(context.Context, *xkmspb.TPMPolicyRequest) (*xkmspb.TPMPolicy, error) {
	return nil, status.Error(codes.NotFound, "policy-a not found")
}

// tpmPolicyRPCRecorder records the full method name of every unary call.
type tpmPolicyRPCRecorder struct {
	mu      sync.Mutex
	methods []string
}

func (r *tpmPolicyRPCRecorder) intercept(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (interface{}, error) {
	r.mu.Lock()
	r.methods = append(r.methods, info.FullMethod)
	r.mu.Unlock()
	return h(ctx, req)
}

func (r *tpmPolicyRPCRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.methods
	r.methods = nil
	return out
}

func newTPMPolicyTestHandler(t *testing.T) (*tpmPolicyHandler, *tpmPolicyFakeServer, *tpmPolicyRPCRecorder) {
	t.Helper()
	fake := &tpmPolicyFakeServer{}
	rec := &tpmPolicyRPCRecorder{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		xkmspb.RegisterKeystoreServiceServer(s, fake)
	}, grpc.UnaryInterceptor(rec.intercept))
	return newTPMPolicyHandler(srv.dial(t)), fake, rec
}

func TestTPMPolicy_GRPC_ServiceNameRoutes(t *testing.T) {
	for _, svc := range []string{"quicbaas.TPMPolicy", "TPMPolicy"} {
		if !isTPMPolicyService(svc) {
			t.Errorf("isTPMPolicyService(%q) = false", svc)
		}
	}
	if isTPMPolicyService("quicbaas.PIV") {
		t.Error("isTPMPolicyService(quicbaas.PIV) = true")
	}
}

func TestTPMPolicy_GRPC_ListRoundTrips(t *testing.T) {
	h, fake, rec := newTPMPolicyTestHandler(t)
	result, err := h.handleRequest(context.Background(), "TPMPolicyList",
		&xkmstransport.TPMPolicyListRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("TPMPolicyList: %v", err)
	}
	resp, ok := result.(*xkmstransport.TPMPolicyListResponse)
	if !ok || len(resp.Policies) != 1 || resp.Policies[0].Name != "policy-a" || !resp.Policies[0].Approved {
		t.Fatalf("TPMPolicyList result %#v", result)
	}
	fake.mu.Lock()
	tenant := fake.listTenant
	fake.mu.Unlock()
	if tenant != "tenant-a" {
		t.Fatalf("server saw tenant %q", tenant)
	}
	want := "/" + xkmspb.KeystoreService_ServiceDesc.ServiceName + "/TPMPolicyList"
	if got := rec.take(); len(got) != 1 || got[0] != want {
		t.Fatalf("calls %v, want [%s]", got, want)
	}
}

func TestTPMPolicy_GRPC_NotFoundMapsToSDKError(t *testing.T) {
	h, _, _ := newTPMPolicyTestHandler(t)
	_, err := h.handleRequest(context.Background(), "TPMPolicyGet", &xkmstransport.TPMPolicyRequest{Name: "policy-a"})
	if !errors.IsNotFound(err) {
		t.Fatalf("TPMPolicyGet: got %v, want not found", err)
	}
}

// TestTPMPolicy_GRPC_EveryVerbReachesKeystoreService proves each verb makes
// exactly one xkms.v1 KeystoreService call; the fake answers Unimplemented.
func TestTPMPolicy_GRPC_EveryVerbReachesKeystoreService(t *testing.T) {
	h, _, rec := newTPMPolicyTestHandler(t)
	named := &xkmstransport.TPMPolicyRequest{Name: "policy-a", TenantID: "tenant-a"}
	prefix := "/" + xkmspb.KeystoreService_ServiceDesc.ServiceName + "/"
	for _, tc := range []struct {
		method string
		req    interface{}
	}{
		{"TPMPolicyCreate", &xkmstransport.TPMPolicyCreateRequest{Name: "policy-a"}},
		{"TPMPolicyUpdate", named},
		{"TPMPolicyRevoke", named},
		{"TPMPolicyVerify", named},
		{"TPMPolicyDelete", &xkmstransport.TPMPolicyDeleteRequest{Name: "policy-a"}},
		{"TPMPolicyObjects", named},
		{"TPMPolicySetAutoUnseal", &xkmstransport.TPMPolicyAutoUnsealRequest{Name: "policy-a", Enabled: true}},
		{"TPMPolicySetDescription", &xkmstransport.TPMPolicyDescriptionRequest{Name: "policy-a", Description: "d"}},
		{"TPMObjectPolicy", &xkmstransport.TPMObjectPolicyRequest{KeyID: "key-a", Kind: "sealed"}},
		{"TPMPolicyRotateAuthority", &xkmstransport.TPMAuthorityRotateRequest{}},
		{"SetTenantTPMMode", &xkmstransport.SetTenantTPMModeRequest{TenantID: "tenant-a", Mode: "vtpm"}},
		{"GetTenantTPMMode", "tenant-a"},
		{"TenantVTPMStatus", "tenant-a"},
		{"TenantVTPMStart", "tenant-a"},
		{"TenantVTPMStop", "tenant-a"},
	} {
		_, err := h.handleRequest(context.Background(), tc.method, tc.req)
		if !errors.IsUnimplemented(err) {
			t.Errorf("%s: got %v, want the server's unimplemented status", tc.method, err)
		}
		got := rec.take()
		if len(got) != 1 || !strings.HasPrefix(got[0], prefix) {
			t.Errorf("%s: calls %v, want one %s* call", tc.method, got, prefix)
		}
	}
}

func TestTPMPolicy_GRPC_RefusesBadRequestsBeforeSend(t *testing.T) {
	h, _, rec := newTPMPolicyTestHandler(t)
	for _, tc := range []struct {
		method string
		req    interface{}
	}{
		{"TPMPolicyGet", struct{}{}},
		{"TPMPolicyList", (*xkmstransport.TPMPolicyListRequest)(nil)},
		{"GetTenantTPMMode", ""},
		{"TenantVTPMStart", 7},
	} {
		if _, err := h.handleRequest(context.Background(), tc.method, tc.req); !errors.IsInvalidInput(err) {
			t.Errorf("%s(%#v): got %v, want invalid input", tc.method, tc.req, err)
		}
	}
	if _, err := h.handleRequest(context.Background(), "TPMPolicyNope", nil); !errors.IsUnimplemented(err) {
		t.Errorf("unknown method: got %v, want unimplemented", err)
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("refused requests reached the server: %v", got)
	}
}

package grpc

import (
	"context"
	"fmt"

	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/pbconv"
	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tpmPolicyServicePrefix is the SDK service name of the go-xkms
// TPMPolicyService verbs.
const tpmPolicyServicePrefix = "quicbaas.TPMPolicy"

// isTPMPolicyService reports whether svc names the TPM-policy service.
// Its verbs exchange go-xkms wire structs, which go-xkms serves on the
// xkms.v1 KeystoreService; pbconv.TPMPolicyClient does the conversion.
func isTPMPolicyService(svc string) bool {
	return svc == tpmPolicyServicePrefix || svc == "TPMPolicy"
}

// tpmPolicyHandler bridges "quicbaas.TPMPolicy" verbs onto the go-xkms
// xkms.v1 KeystoreService over the transport's connection. This branch
// also serves the Unix transport, which delegates to gRPC verbatim.
type tpmPolicyHandler struct {
	svc xkmstransport.TPMPolicyService
}

func newTPMPolicyHandler(conn grpc.ClientConnInterface) *tpmPolicyHandler {
	return &tpmPolicyHandler{svc: pbconv.TPMPolicyClient{Client: xkmspb.NewKeystoreServiceClient(conn)}}
}

func tpmPolicyGRPCOp(method string) string { return "tpmPolicyHandler." + method }

func tpmPolicyBadType(method, want string) error {
	return errors.NewInvalidInput(tpmPolicyGRPCOp(method), "invalid request type, expected "+want)
}

// tpmPolicyResult converts a go-xkms gRPC status error to the SDK error
// taxonomy and returns the response otherwise.
func tpmPolicyResult[T any](method string, resp *T, err error) (interface{}, error) {
	if err != nil {
		return nil, convertGRPCError(err, tpmPolicyGRPCOp(method))
	}
	return resp, nil
}

func tpmPolicyAs[T any](method string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, tpmPolicyBadType(method, fmt.Sprintf("%T", (*T)(nil)))
	}
	return r, nil
}

func tpmPolicyTenant(method string, req interface{}) (string, error) {
	id, ok := req.(string)
	if !ok || id == "" {
		return "", errors.NewInvalidInput(tpmPolicyGRPCOp(method), "tenant ID is required")
	}
	return id, nil
}

// handleRequest dispatches one TPM-policy verb by method name.
func (h *tpmPolicyHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "TPMPolicyCreate":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyCreateRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicyCreate(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicyGet", "TPMPolicyUpdate", "TPMPolicyRevoke":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyRequest](method, req)
		if err != nil {
			return nil, err
		}
		var resp *xkmstransport.TPMPolicy
		switch method {
		case "TPMPolicyGet":
			resp, err = h.svc.TPMPolicyGet(ctx, r)
		case "TPMPolicyUpdate":
			resp, err = h.svc.TPMPolicyUpdate(ctx, r)
		default:
			resp, err = h.svc.TPMPolicyRevoke(ctx, r)
		}
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicyList":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyListRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicyList(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicyVerify":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicyVerify(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicyDelete":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyDeleteRequest](method, req)
		if err != nil {
			return nil, err
		}
		if err := h.svc.TPMPolicyDelete(ctx, r); err != nil {
			return nil, convertGRPCError(err, tpmPolicyGRPCOp(method))
		}
		return nil, nil
	case "TPMPolicyObjects":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicyObjects(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicySetAutoUnseal":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyAutoUnsealRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicySetAutoUnseal(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicySetDescription":
		r, err := tpmPolicyAs[xkmstransport.TPMPolicyDescriptionRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicySetDescription(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMObjectPolicy":
		r, err := tpmPolicyAs[xkmstransport.TPMObjectPolicyRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMObjectPolicy(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "TPMPolicyRotateAuthority":
		r, err := tpmPolicyAs[xkmstransport.TPMAuthorityRotateRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.TPMPolicyRotateAuthority(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "SetTenantTPMMode":
		r, err := tpmPolicyAs[xkmstransport.SetTenantTPMModeRequest](method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.SetTenantTPMMode(ctx, r)
		return tpmPolicyResult(method, resp, err)
	case "GetTenantTPMMode":
		id, err := tpmPolicyTenant(method, req)
		if err != nil {
			return nil, err
		}
		resp, err := h.svc.GetTenantTPMMode(ctx, id)
		return tpmPolicyResult(method, resp, err)
	case "TenantVTPMStatus", "TenantVTPMStart", "TenantVTPMStop":
		id, err := tpmPolicyTenant(method, req)
		if err != nil {
			return nil, err
		}
		var resp *xkmstransport.TenantVTPMStatusResponse
		switch method {
		case "TenantVTPMStatus":
			resp, err = h.svc.TenantVTPMStatus(ctx, id)
		case "TenantVTPMStart":
			resp, err = h.svc.TenantVTPMStart(ctx, id)
		default:
			resp, err = h.svc.TenantVTPMStop(ctx, id)
		}
		return tpmPolicyResult(method, resp, err)
	default:
		return nil, errors.NewUnimplemented(tpmPolicyGRPCOp(method), "unsupported TPM policy method")
	}
}

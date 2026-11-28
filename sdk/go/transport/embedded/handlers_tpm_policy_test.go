package embedded

import (
	"context"
	"strings"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestTPMPolicy_Embedded_EveryVerbIsTypedRefusal proves each TPM-policy verb
// is routed and answers a typed Unimplemented naming the method, never an
// unknown-method error.
func TestTPMPolicy_Embedded_EveryVerbIsTypedRefusal(t *testing.T) {
	tr := newPIVTransport(t, nil)
	named := &xkmstransport.TPMPolicyRequest{Name: "policy-a"}
	for _, tc := range []struct {
		method string
		req    interface{}
	}{
		{"TPMPolicyCreate", &xkmstransport.TPMPolicyCreateRequest{Name: "policy-a"}},
		{"TPMPolicyGet", named},
		{"TPMPolicyList", &xkmstransport.TPMPolicyListRequest{}},
		{"TPMPolicyUpdate", named},
		{"TPMPolicyRevoke", named},
		{"TPMPolicyVerify", named},
		{"TPMPolicyDelete", &xkmstransport.TPMPolicyDeleteRequest{Name: "policy-a"}},
		{"TPMPolicyObjects", named},
		{"TPMPolicySetAutoUnseal", &xkmstransport.TPMPolicyAutoUnsealRequest{Name: "policy-a"}},
		{"TPMPolicySetDescription", &xkmstransport.TPMPolicyDescriptionRequest{Name: "policy-a"}},
		{"TPMObjectPolicy", &xkmstransport.TPMObjectPolicyRequest{KeyID: "key-a", Kind: "sealed"}},
		{"TPMPolicyRotateAuthority", &xkmstransport.TPMAuthorityRotateRequest{}},
		{"GetTenantTPMMode", "tenant-a"},
		{"SetTenantTPMMode", &xkmstransport.SetTenantTPMModeRequest{TenantID: "tenant-a", Mode: "vtpm"}},
		{"TenantVTPMStatus", "tenant-a"},
		{"TenantVTPMStart", "tenant-a"},
		{"TenantVTPMStop", "tenant-a"},
	} {
		if _, ok := unaryHandlers[tpmPolicyServiceName+"/"+tc.method]; !ok {
			t.Errorf("%s: not routed", tc.method)
			continue
		}
		_, err := tr.Request(context.Background(), tpmPolicyServiceName, tc.method, tc.req)
		if !errors.IsUnimplemented(err) {
			t.Errorf("%s: got %v, want unimplemented", tc.method, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.method) {
			t.Errorf("%s: refusal %q does not name the method", tc.method, err)
		}
	}
}

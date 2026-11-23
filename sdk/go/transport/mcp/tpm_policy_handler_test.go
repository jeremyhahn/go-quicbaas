package mcp

import (
	"context"
	"strings"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestTPMPolicy_MCP_EveryVerbIsTypedRefusalWithNoToolCall proves each
// TPM-policy verb fails fast with a typed Unimplemented naming the method,
// and that no tool call reaches the server.
func TestTPMPolicy_MCP_EveryVerbIsTypedRefusalWithNoToolCall(t *testing.T) {
	tr, called, cleanup := newMultiToolMCPServer(t, []string{"xkms.piv.listSlots"},
		func(t *testing.T, tool string, _ map[string]interface{}) map[string]interface{} {
			t.Errorf("tool %s called for a TPM-policy verb", tool)
			return map[string]interface{}{}
		})
	defer cleanup()

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
		_, err := tr.Request(context.Background(), tpmPolicyServiceName, tc.method, tc.req)
		if !errors.IsUnimplemented(err) {
			t.Errorf("%s: got %v, want unimplemented", tc.method, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.method) {
			t.Errorf("%s: refusal %q does not name the method", tc.method, err)
		}
	}
	if len(*called) != 0 {
		t.Fatalf("tools called: %v", *called)
	}
}

func TestTPMPolicy_MCP_ServiceNameRoutes(t *testing.T) {
	for _, svc := range []string{"quicbaas.TPMPolicy", "TPMPolicy"} {
		if !isMCPTPMPolicyService(svc) {
			t.Errorf("isMCPTPMPolicyService(%q) = false", svc)
		}
	}
	if isMCPTPMPolicyService(pivServiceName) {
		t.Errorf("isMCPTPMPolicyService(%q) = true", pivServiceName)
	}
}

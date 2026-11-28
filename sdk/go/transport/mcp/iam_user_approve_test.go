package mcp

// The SDK MCP transport maps the IAM user Approve and Reject verbs onto the
// quicbaas_iam_user_approve and quicbaas_iam_user_reject server tools.

import (
	"context"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestIAMUserApprove_MCP_ApproveCallsTool(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_user_approve",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["tenant_id"] != "t-1" || args["user_id"] != "30001" {
				t.Errorf("args = %v, want tenant_id t-1 and user_id 30001", args)
			}
			if _, ok := args["reason"]; ok {
				t.Errorf("approve args carry a reason: %v", args)
			}
		},
		result: map[string]interface{}{"user_id": 30001, "lifecycle": "active"},
	})
	defer cleanup()

	if err := iam.NewUserClient(tr).Approve(context.Background(), "t-1", "30001"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
}

func TestIAMUserApprove_MCP_RejectCallsToolWithReason(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_user_reject",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["tenant_id"] != "t-1" || args["user_id"] != "30001" || args["reason"] != "spam signup" {
				t.Errorf("args = %v, want tenant_id t-1, user_id 30001, reason %q", args, "spam signup")
			}
		},
		result: map[string]interface{}{"user_id": 30001, "lifecycle": "disabled"},
	})
	defer cleanup()

	if err := iam.NewUserClient(tr).Reject(context.Background(), "t-1", "30001", "spam signup"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
}

func TestIAMUserApprove_MCP_ToolErrorSurfaces(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_iam_user_approve",
		result:    map[string]interface{}{"error": "refused"},
		toolError: true,
	})
	defer cleanup()

	if err := iam.NewUserClient(tr).Approve(context.Background(), "t-1", "30001"); err == nil {
		t.Fatal("Approve: want the tool error to surface")
	}
}

func TestIAMUserApprove_MCP_ToolMap(t *testing.T) {
	for method, want := range map[string]string{
		"User.Approve": "quicbaas_iam_user_approve",
		"User.Reject":  "quicbaas_iam_user_reject",
	} {
		if got := iamToolMap[method]; got != want {
			t.Errorf("iamToolMap[%q] = %q, want %q", method, got, want)
		}
	}
}

package grpc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// validatePolicyFullYAML is a full policy document CreatePolicy accepts.
const validatePolicyFullYAML = `apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: p1
spec:
  subjects:
    authenticated: true
  rules:
    - name: r1
      effect: allow
      actions: [read]
      resources: ["*"]
`

// validatePolicyCases are documents ValidatePolicy and CreatePolicy must
// judge alike. wantKey names the key an invalid document's reason must cite.
// A valid spec-only document is not among them: ValidatePolicyRequest
// carries no policy name, so ValidatePolicy refuses it on the missing name.
var validatePolicyCases = []struct {
	name      string
	doc       string
	wantValid bool
	wantKey   string
}{
	{name: "full document", doc: validatePolicyFullYAML, wantValid: true},
	{
		name: "full document with a condition",
		doc: validatePolicyFullYAML + `      when:
        - expression: "subject.dept == 'eng'"
`,
		wantValid: true,
	},
	{
		name: "unknown spec key",
		doc: `rules:
  - name: r1
    effect: allow
    actions: [read]
    resources: ["*"]
bogus: true
`,
		wantKey: "bogus",
	},
	{
		name: "none next to an expression",
		doc: `rules:
  - name: r1
    effect: allow
    actions: [read]
    resources: ["*"]
    when:
      - expression: "subject.dept == 'eng'"
        none:
          - expression: "subject.dept == 'ops'"
`,
		wantKey: "none",
	},
	{
		name: "rule-level subjects",
		doc: `rules:
  - name: r1
    effect: allow
    subjects:
      users: [alice]
    actions: [read]
    resources: ["*"]
`,
		wantKey: "subjects",
	},
	{
		name: "unknown key in a full document",
		doc: strings.Replace(validatePolicyFullYAML, "kind: Policy\n",
			"kind: Policy\nstatus: active\n", 1),
		wantKey: "status",
	},
	{name: "nested condition", doc: nestedConditionSpecYAML},
}

func TestIAMServer_ValidatePolicy_StrictDecode(t *testing.T) {
	for _, tc := range validatePolicyCases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newNestedConditionIAMServer(t)

			resp, err := srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{
				TenantId: "t1", SpecYaml: []byte(tc.doc),
			})

			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, resp.Valid, "errors: %v", resp.Errors)
			if tc.wantValid {
				assert.Empty(t, resp.Errors)
				return
			}
			require.NotEmpty(t, resp.Errors)
			if tc.wantKey != "" {
				assert.Contains(t, strings.Join(resp.Errors, "; "), tc.wantKey)
			}
		})
	}
}

// TestIAMServer_ValidatePolicy_Name checks the document against the name it
// will be stored under: a match is valid, a mismatch is invalid with the
// reason, and an empty name leaves the document's own name unchecked.
func TestIAMServer_ValidatePolicy_Name(t *testing.T) {
	cases := []struct {
		name       string
		reqName    string
		wantValid  bool
		wantReason string
	}{
		{name: "matching name", reqName: "p1", wantValid: true},
		{name: "mismatched name", reqName: "p2", wantValid: false,
			wantReason: `policy metadata.name "p1" does not match the request name "p2"`},
		{name: "empty name", reqName: "", wantValid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := newNestedConditionIAMServer(t)

			resp, err := srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{
				TenantId: "t1", SpecYaml: []byte(validatePolicyFullYAML), Name: tc.reqName,
			})

			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, resp.Valid, "errors: %v", resp.Errors)
			if tc.wantValid {
				assert.Empty(t, resp.Errors)
				return
			}
			require.Len(t, resp.Errors, 1)
			assert.Contains(t, resp.Errors[0], tc.wantReason)
		})
	}
}

func TestIAMServer_ValidatePolicy_AgreesWithCreatePolicy(t *testing.T) {
	for _, tc := range validatePolicyCases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, storage := newNestedConditionIAMServer(t)

			resp, err := srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{
				TenantId: "t1", SpecYaml: []byte(tc.doc),
			})
			require.NoError(t, err)

			_, createErr := srv.CreatePolicy(callerContext("t1"), &iampb.CreatePolicyRequest{
				TenantId: "t1", Name: "p1", SpecYaml: []byte(tc.doc),
			})

			assert.Equal(t, resp.Valid, createErr == nil,
				"ValidatePolicy valid=%v errors=%v, CreatePolicy err=%v", resp.Valid, resp.Errors, createErr)
			assert.Equal(t, createErr == nil, storedGRPCPolicy(storage) != nil)
		})
	}
}

func TestIAMServiceAdapter_ValidatePolicy_RefusalIsNotAnError(t *testing.T) {
	_, adapter, _ := newNestedConditionIAMServer(t)

	valid, reasons, err := adapter.ValidatePolicy(callerContext("t1"), "t1", "",
		[]byte("rules: []\nbogus: true\n"))

	require.NoError(t, err)
	assert.False(t, valid)
	require.Len(t, reasons, 1)
	assert.Contains(t, reasons[0], "bogus")
}

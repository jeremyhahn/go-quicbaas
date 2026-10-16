package iam

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPolicyName(t *testing.T) {
	cases := []struct {
		name     string
		docName  string
		reqName  string
		wantName string
	}{
		{name: "matching name", docName: "p1", reqName: "p1", wantName: "p1"},
		{name: "empty request name keeps the document name", docName: "p1", reqName: "", wantName: "p1"},
		{name: "unnamed document takes the request name", docName: "", reqName: "p1", wantName: "p1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := &Policy{Metadata: PolicyMetadata{Name: tc.docName}}

			require.NoError(t, CheckPolicyName(policy, tc.reqName))
			assert.Equal(t, tc.wantName, policy.Metadata.Name)
		})
	}
}

func TestCheckPolicyName_Mismatch(t *testing.T) {
	policy := &Policy{Metadata: PolicyMetadata{Name: "p1"}}

	err := CheckPolicyName(policy, "p2")

	require.Error(t, err)
	var mismatch *PolicyNameMismatchError
	require.True(t, errors.As(err, &mismatch))
	assert.Equal(t, "p2", mismatch.Requested)
	assert.Equal(t, "p1", mismatch.Document)
	assert.Contains(t, err.Error(), `policy metadata.name "p1" does not match the request name "p2"`)
	assert.Equal(t, "p1", policy.Metadata.Name, "a refused document is left as it was")
}

// TestCheckPolicyName_NilPolicy pins that a name with no policy does not
// panic; the engine's ValidatePolicy reports the nil policy.
func TestCheckPolicyName_NilPolicy(t *testing.T) {
	require.NoError(t, CheckPolicyName(nil, "p1"))
}

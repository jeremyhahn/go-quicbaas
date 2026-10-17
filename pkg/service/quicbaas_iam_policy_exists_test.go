package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestIAMServiceWrapper_CreatePolicy_DuplicateIsAlreadyExists pins that a
// duplicate create carries the ErrAlreadyExists code every transport maps,
// while the *iam.PolicyExistsError REST matches stays in the chain.
func TestIAMServiceWrapper_CreatePolicy_DuplicateIsAlreadyExists(t *testing.T) {
	ctx := context.Background()
	provider, _ := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	req := &CreatePolicyRequest{
		TenantID: "t1",
		Policy:   conditionHappyPolicy(iam.Condition{Expression: "x == 1"}),
	}

	resp, err := w.CreatePolicy(ctx, req)
	require.NoError(t, err, "a fresh create must succeed")
	require.NotNil(t, resp)

	_, err = w.CreatePolicy(ctx, req)
	require.Error(t, err)
	assert.Equal(t, errors.ErrAlreadyExists, errors.Code(err))
	var exists *iam.PolicyExistsError
	require.True(t, stderrors.As(err, &exists), "expected PolicyExistsError in the chain, got %v", err)
	assert.Equal(t, "p1", exists.Name)
}

func TestPolicyCreateError_OtherErrorUnchanged(t *testing.T) {
	cause := &testIAMError{msg: "storage down"}
	err := PolicyCreateError("iam.CreatePolicy", cause)
	assert.Same(t, cause, err)
	assert.NotEqual(t, errors.ErrAlreadyExists, errors.Code(err))
}

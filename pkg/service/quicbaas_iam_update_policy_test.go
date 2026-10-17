// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

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

const updatePolicyWrapperTenant = "tenant-update-policy"

func TestIAMServiceWrapper_UpdatePolicy_TargetsRequestNameAndAnswersFromStore(t *testing.T) {
	provider, pe := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()
	for _, name := range []string{"target", "bystander"} {
		_, err := w.CreatePolicy(ctx, &CreatePolicyRequest{
			TenantID: updatePolicyWrapperTenant,
			Policy:   happyPolicy(updatePolicyWrapperTenant, name),
		})
		require.NoError(t, err)
	}

	// The document carries no name: the request name selects the target.
	doc := happyPolicy(updatePolicyWrapperTenant, "")
	doc.Metadata.Description = "updated"
	resp, err := w.UpdatePolicy(ctx, &UpdatePolicyRequest{
		TenantID: updatePolicyWrapperTenant,
		Name:     "target",
		Policy:   doc,
	})
	require.NoError(t, err)

	stored, err := pe.GetPolicy(ctx, updatePolicyWrapperTenant, "target")
	require.NoError(t, err)
	assert.Equal(t, "updated", stored.Metadata.Description)
	assert.Equal(t, stored, resp.Policy, "response must be the stored policy")
	assert.False(t, resp.Policy.Metadata.Updated.IsZero(), "response must carry the stored Updated stamp")
	assert.Empty(t, doc.Metadata.Name, "the caller's document must not be mutated")

	bystander, err := pe.GetPolicy(ctx, updatePolicyWrapperTenant, "bystander")
	require.NoError(t, err)
	assert.Equal(t, "happy-path policy", bystander.Metadata.Description)
}

func TestIAMServiceWrapper_UpdatePolicy_RefusesMismatchedDocumentName(t *testing.T) {
	provider, pe := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()
	for _, name := range []string{"target", "other"} {
		_, err := w.CreatePolicy(ctx, &CreatePolicyRequest{
			TenantID: updatePolicyWrapperTenant,
			Policy:   happyPolicy(updatePolicyWrapperTenant, name),
		})
		require.NoError(t, err)
	}

	doc := happyPolicy(updatePolicyWrapperTenant, "other")
	doc.Metadata.Description = "redirected"
	_, err := w.UpdatePolicy(ctx, &UpdatePolicyRequest{
		TenantID: updatePolicyWrapperTenant,
		Name:     "target",
		Policy:   doc,
	})
	var mismatch *iam.PolicyNameMismatchError
	require.True(t, stderrors.As(err, &mismatch), "got %T: %v", err, err)
	assert.Equal(t, "target", mismatch.Requested)
	assert.Equal(t, "other", mismatch.Document)
	assert.Equal(t, iam.CheckPolicyName(happyPolicy(updatePolicyWrapperTenant, "other"), "target").Error(), err.Error())
	assert.Equal(t, "other", doc.Metadata.Name, "the caller's document must not be mutated")

	for _, name := range []string{"target", "other"} {
		stored, err := pe.GetPolicy(ctx, updatePolicyWrapperTenant, name)
		require.NoError(t, err)
		assert.Equal(t, "happy-path policy", stored.Metadata.Description, name)
	}
}

func TestIAMServiceWrapper_UpdatePolicy_RefusesMissingInput(t *testing.T) {
	provider, _ := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()

	_, err := w.UpdatePolicy(ctx, nil)
	assert.Error(t, err)
	_, err = w.UpdatePolicy(ctx, &UpdatePolicyRequest{TenantID: updatePolicyWrapperTenant, Name: "target"})
	assert.Error(t, err)
	_, err = w.UpdatePolicy(ctx, &UpdatePolicyRequest{
		TenantID: updatePolicyWrapperTenant,
		Policy:   happyPolicy(updatePolicyWrapperTenant, "target"),
	})
	assert.Error(t, err)
}

func TestIAMServiceWrapper_CreatePolicy_DocumentName(t *testing.T) {
	tests := []struct {
		name    string
		docName string
		wantErr bool
	}{
		{"mismatch refused", "other", true},
		{"match accepted", "target", false},
		{"nameless document takes the requested name", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, pe := newHappyPolicyProvider()
			w := &iamServiceWrapper{provider: provider}
			ctx := context.Background()

			doc := happyPolicy(updatePolicyWrapperTenant, tt.docName)
			resp, err := w.CreatePolicy(ctx, &CreatePolicyRequest{
				TenantID: updatePolicyWrapperTenant,
				Name:     "target",
				Policy:   doc,
			})
			assert.Equal(t, tt.docName, doc.Metadata.Name, "the caller's document must not be mutated")

			if tt.wantErr {
				var mismatch *iam.PolicyNameMismatchError
				require.True(t, stderrors.As(err, &mismatch), "got %T: %v", err, err)
				assert.Equal(t, "target", mismatch.Requested)
				assert.Equal(t, "other", mismatch.Document)
				for _, name := range []string{"target", "other"} {
					_, getErr := pe.GetPolicy(ctx, updatePolicyWrapperTenant, name)
					assert.Error(t, getErr, "a refused create must store nothing under %q", name)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "target", resp.Policy.Metadata.Name)
			stored, err := pe.GetPolicy(ctx, updatePolicyWrapperTenant, "target")
			require.NoError(t, err)
			assert.Equal(t, "target", stored.Metadata.Name)
		})
	}
}

func TestIAMServiceWrapper_CreatePolicy_RefusesNilPolicy(t *testing.T) {
	provider, _ := newHappyPolicyProvider()
	w := &iamServiceWrapper{provider: provider}
	ctx := context.Background()

	_, err := w.CreatePolicy(ctx, nil)
	assert.True(t, errors.IsInvalidInput(err), "nil request: %v", err)
	_, err = w.CreatePolicy(ctx, &CreatePolicyRequest{TenantID: updatePolicyWrapperTenant, Name: "target"})
	assert.True(t, errors.IsInvalidInput(err), "nil policy: %v", err)
}

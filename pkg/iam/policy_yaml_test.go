// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestParsePolicyYAML_SpecOnly(t *testing.T) {
	data := []byte(`rules:
  - name: r1
    effect: allow
    actions: ["read"]
    resources: ["doc/*"]
    when:
      - expression: "x == 1"
`)
	meta := PolicyMetadata{Name: "p1", Tenant: "t1", Description: "d", Priority: 3}

	policy, err := ParsePolicyYAML(data, meta)
	require.NoError(t, err)
	assert.Equal(t, PolicyYAMLAPIVersion, policy.APIVersion)
	assert.Equal(t, "Policy", policy.Kind)
	assert.Equal(t, meta, policy.Metadata)
	require.Len(t, policy.Spec.Rules, 1)
	assert.Equal(t, "x == 1", policy.Spec.Rules[0].When[0].Expression)
}

func TestParsePolicyYAML_FullDocument(t *testing.T) {
	data := []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: from-doc
  tenant: other-tenant
  description: doc description
  priority: 7
spec:
  rules:
    - name: r1
      effect: deny
      actions: ["write"]
      resources: ["doc/*"]
`)

	t.Run("request overrides non-empty fields", func(t *testing.T) {
		policy, err := ParsePolicyYAML(data, PolicyMetadata{Name: "p1", Tenant: "t1"})
		require.NoError(t, err)
		assert.Equal(t, "p1", policy.Metadata.Name)
		assert.Equal(t, "doc description", policy.Metadata.Description)
		assert.Equal(t, 7, policy.Metadata.Priority)
		require.Len(t, policy.Spec.Rules, 1)
		assert.Equal(t, EffectDeny, policy.Spec.Rules[0].Effect)
	})

	t.Run("tenant always comes from the caller", func(t *testing.T) {
		policy, err := ParsePolicyYAML(data, PolicyMetadata{})
		require.NoError(t, err)
		assert.Equal(t, "from-doc", policy.Metadata.Name)
		assert.Empty(t, policy.Metadata.Tenant)
	})
}

func TestParsePolicyYAML_InvalidYAML(t *testing.T) {
	for name, data := range map[string][]byte{
		"syntax":      []byte("rules: [unclosed"),
		"wrong shape": []byte("rules: not-a-list"),
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := ParsePolicyYAML(data, PolicyMetadata{Tenant: "t1"})
			assert.Nil(t, policy)
			var yamlErr *PolicyYAMLError
			require.True(t, stderrors.As(err, &yamlErr), "expected PolicyYAMLError, got %v", err)
			assert.Error(t, yamlErr.Unwrap())
			var coded *dberrors.QBError
			require.True(t, stderrors.As(err, &coded))
			assert.Equal(t, dberrors.ErrValidation, coded.Code)
		})
	}
}

func TestParsePolicyYAML_UnknownKeysRefused(t *testing.T) {
	for name, data := range map[string][]byte{
		// all:, any: and none: are not condition keys. Decoded loosely, the
		// first two became empty conditions and the third dropped its
		// sibling's restriction.
		"all block":               []byte("rules:\n  - name: r1\n    effect: allow\n    when:\n      - all: [a == 1]\n"),
		"any block":               []byte("rules:\n  - name: r1\n    effect: allow\n    unless:\n      - any: [a == 1]\n"),
		"none beside expression":  []byte("rules:\n  - name: r1\n    effect: allow\n    when:\n      - expression: a == 1\n        none: [b == 2]\n"),
		"rule key typo":           []byte("rules:\n  - name: r1\n    effect: allow\n    condition: a == 1\n"),
		"spec key typo":           []byte("subject:\n  roles: [admin]\nrules: []\n"),
		"full document typo":      []byte("apiVersion: iam.quicbaas.io/v1\nkind: Policy\nspec:\n  rule: []\n"),
		"apiVersion without kind": []byte("apiVersion: iam.quicbaas.io/v1\nrules: []\n"),
		"bare string condition":   []byte("rules:\n  - name: r1\n    effect: allow\n    when:\n      - a == 1\n"),
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := ParsePolicyYAML(data, PolicyMetadata{Tenant: "t1"})
			assert.Nil(t, policy)
			var yamlErr *PolicyYAMLError
			require.True(t, stderrors.As(err, &yamlErr), "expected PolicyYAMLError, got %v", err)
			var coded *dberrors.QBError
			require.True(t, stderrors.As(err, &coded))
			assert.Equal(t, dberrors.ErrValidation, coded.Code)
		})
	}
}

func TestParsePolicyYAML_EmptyDocument(t *testing.T) {
	policy, err := ParsePolicyYAML(nil, PolicyMetadata{Name: "p1", Tenant: "t1"})
	require.NoError(t, err)
	assert.Empty(t, policy.Spec.Rules)
	assert.Equal(t, "t1", policy.Metadata.Tenant)
}

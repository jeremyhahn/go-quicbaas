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

// Package main: factory-error coverage for the runCanonicalAuthFactor*
// handlers. A live AuthFactorsClient mock requires a large surface; the
// factory-error branch reaches every runner via the first call and is
// the highest-value error path operators see when the daemon is down.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCanonicalAuthFactorList_FactoryError(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, nil, stderrors.New("factory boom"))
	err := runCanonicalAuthFactorList(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

func TestRunCanonicalAuthFactorAdd_FactoryError(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, nil, stderrors.New("factory boom"))
	err := runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
		label:      "label",
		certPath:   "/dev/null",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

func TestRunCanonicalAuthFactorRemove_RejectsEmptyFactorID(t *testing.T) {
	err := runCanonicalAuthFactorRemove(context.Background(), "", false)
	require.Error(t, err)
	var typed *MissingFactorIDError
	require.ErrorAs(t, err, &typed)
}

func TestRunCanonicalAuthFactorRemove_FactoryError(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, nil, stderrors.New("factory boom"))
	err := runCanonicalAuthFactorRemove(context.Background(), "factor-1", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

func TestRunCanonicalAuthFactorRotate_RejectsEmptyOldID(t *testing.T) {
	err := runCanonicalAuthFactorRotate(context.Background(), "", &canonicalAuthFactorAddFlags{})
	require.Error(t, err)
	var typed *MissingFactorIDError
	require.ErrorAs(t, err, &typed)
}

func TestRunCanonicalAuthFactorRotate_FactoryError(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, nil, stderrors.New("factory boom"))
	err := runCanonicalAuthFactorRotate(context.Background(), "old-id", &canonicalAuthFactorAddFlags{
		factorType: "mtls", label: "lab", certPath: "/dev/null",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

func TestRunCanonicalAuthFactorRelabel_FactoryError(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, nil, stderrors.New("factory boom"))
	err := runCanonicalAuthFactorRelabel(context.Background(), "factor-id", "new label")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

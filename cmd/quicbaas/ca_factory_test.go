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

// Package main: factory-error coverage for createCASDKCertificateClient.
package main

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCASDKCertificateClient_FactoryError(t *testing.T) {
	withFakeSDKFactory(t, nil, stderrors.New("factory boom"))
	c, err := createCASDKCertificateClient()
	require.Error(t, err)
	assert.Nil(t, c)
}

func TestCreateOrchestratorSDKClient_FactoryError(t *testing.T) {
	withFakeSDKFactory(t, nil, stderrors.New("factory boom"))
	c, err := createOrchestratorSDKClient()
	require.Error(t, err)
	assert.Nil(t, c)
}

func TestCreateOrchestratorSDKClient_Happy(t *testing.T) {
	withFakeSDKFactory(t, newFakeSDKClient(nil, nil), nil)
	c, err := createOrchestratorSDKClient()
	require.NoError(t, err)
	require.NotNil(t, c)
}

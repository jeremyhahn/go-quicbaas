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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

// start_kms_errors_tt_test.go drives the two late error branches of startKMS
// that the happy-path KMS tests do not reach: a failure initialising the
// backend factory from go-xkms (initFromXKMSFn) and a failure constructing the
// KMS service (newKMSServiceFn). Both are reached only after the software
// backend and keystore are built successfully, so they are exercised here via
// the established package-level function seams.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// TestServer_StartKMS_TT_InitFromXKMSFailurePropagates covers the
// initialize-from-XKMS error branch of startKMS: when the backend factory
// cannot enumerate go-xkms backends, startKMS aborts with a wrapped error
// naming the failing step.
func TestServer_StartKMS_TT_InitFromXKMSFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true

	sentinel := errors.New("xkms backend enumeration failed")
	initFromXKMSFn = func(*kms.BackendFactory, context.Context, []config.BackendType) error {
		return sentinel
	}

	err := srv.startKMS()
	require.Error(t, err, "an XKMS-init failure must abort startKMS")
	assert.Contains(t, err.Error(), "XKMS",
		"the wrapped error must identify the failing KMS-from-XKMS step")
}

// TestServer_StartKMS_TT_KMSServiceFailurePropagates covers the KMS-service
// construction error branch of startKMS: when newKMSServiceFn fails, startKMS
// aborts with a wrapped error naming the manager-construction step.
func TestServer_StartKMS_TT_KMSServiceFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true

	sentinel := errors.New("kms manager construction failed")
	newKMSServiceFn = func(*kms.KMSServiceOptions) (*kms.KMSService, error) {
		return nil, sentinel
	}

	err := srv.startKMS()
	require.Error(t, err, "a KMS-service construction failure must abort startKMS")
	assert.Contains(t, err.Error(), "KMS manager",
		"the wrapped error must identify the failing KMS-manager step")
}

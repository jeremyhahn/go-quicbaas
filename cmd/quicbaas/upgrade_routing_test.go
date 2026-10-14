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

// Package main: coverage for the upgrade verb tree routing through the SDK
// Operations client (transport-agnostic) rather than a hardcoded gRPC
// connection.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	operationsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
)

// fakeOperationsClient is a hand-rolled OperationsClient that records the
// requests it receives and returns canned responses. It lets the upgrade
// command tests assert that the CLI routes through the SDK Operations
// surface without standing up a server.
type fakeOperationsClient struct {
	upgradeReq    *operationsSDK.UpgradeNodeRequest
	upgradeResp   *operationsSDK.UpgradeNodeResponse
	upgradeErr    error
	statusReq     *operationsSDK.GetUpgradeStatusRequest
	statusResp    *operationsSDK.GetUpgradeStatusResponse
	statusErr     error
	upgradeCalled int
	statusCalled  int
}

func (f *fakeOperationsClient) MigrateData(ctx context.Context, req *operationsSDK.MigrateDataRequest) (*operationsSDK.MigrateDataResponse, error) {
	return nil, nil
}

func (f *fakeOperationsClient) GetMigrationProgress(ctx context.Context, req *operationsSDK.GetMigrationProgressRequest) (*operationsSDK.GetMigrationProgressResponse, error) {
	return nil, nil
}

func (f *fakeOperationsClient) ReplaceNode(ctx context.Context, req *operationsSDK.ReplaceNodeRequest) (*operationsSDK.ReplaceNodeResponse, error) {
	return nil, nil
}

func (f *fakeOperationsClient) UpgradeNode(ctx context.Context, req *operationsSDK.UpgradeNodeRequest) (*operationsSDK.UpgradeNodeResponse, error) {
	f.upgradeCalled++
	f.upgradeReq = req
	return f.upgradeResp, f.upgradeErr
}

func (f *fakeOperationsClient) GetUpgradeStatus(ctx context.Context, req *operationsSDK.GetUpgradeStatusRequest) (*operationsSDK.GetUpgradeStatusResponse, error) {
	f.statusCalled++
	f.statusReq = req
	return f.statusResp, f.statusErr
}

// withOperationsFactory installs a fake OperationsClient for the duration
// of a test and restores the production factory on cleanup.
func withOperationsFactory(t *testing.T, fake *fakeOperationsClient) {
	t.Helper()
	orig := operationsClientFactory
	t.Cleanup(func() { operationsClientFactory = orig })
	operationsClientFactory = func() (OperationsClient, error) {
		return fake, nil
	}
}

// withUpgradeStartGlobals resets the upgrade-start flag globals so the
// routing tests do not leak state from other suites.
func withUpgradeStartGlobals(t *testing.T) {
	t.Helper()
	on, ov, of := upgradeNodeID, upgradeServerAddress, upgradeFollowProgress
	oo := upgradeOutputFormat
	t.Cleanup(func() {
		upgradeNodeID = on
		upgradeServerAddress = ov
		upgradeFollowProgress = of
		upgradeOutputFormat = oo
	})
	upgradeNodeID = 7
	upgradeServerAddress = ""
	upgradeFollowProgress = false
	upgradeOutputFormat = "table"
}

func TestRunUpgradeStart_RoutesThroughSDKOperations(t *testing.T) {
	withUpgradeStartGlobals(t)
	fake := &fakeOperationsClient{
		upgradeResp: &operationsSDK.UpgradeNodeResponse{
			JobID:  "job-123",
			Status: "initiated",
		},
	}
	withOperationsFactory(t, fake)

	cmd := newUpgradeStartCommand()
	// Set the node-id AFTER constructing the command: cobra's Uint64Var flag
	// binding writes its default (0) into upgradeNodeID at registration time,
	// which would otherwise clobber the value under test.
	upgradeNodeID = 7
	err := runUpgradeStart(cmd, []string{"v2.0.0"})
	require.NoError(t, err)

	assert.Equal(t, 1, fake.upgradeCalled, "UpgradeNode should be called exactly once via SDK")
	require.NotNil(t, fake.upgradeReq)
	assert.Equal(t, uint64(7), fake.upgradeReq.NodeID)
	assert.Equal(t, "v2.0.0", fake.upgradeReq.TargetVersion)
}

func TestRunUpgradeStart_PropagatesSDKError(t *testing.T) {
	withUpgradeStartGlobals(t)
	fake := &fakeOperationsClient{
		upgradeErr: stderrors.New("upgrade boom"),
	}
	withOperationsFactory(t, fake)

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v2.0.0"})
	require.Error(t, err)
	assert.Equal(t, 1, fake.upgradeCalled)
}

// TestRunUpgradeStart_NoAddressIsTypedError proves the command invents no
// last-resort address: with neither --server nor --address set it refuses
// with a typed error before touching the SDK.
func TestRunUpgradeStart_NoAddressIsTypedError(t *testing.T) {
	withUpgradeStartGlobals(t)
	origAddress := address
	t.Cleanup(func() { address = origAddress })
	address = ""
	fake := &fakeOperationsClient{}
	withOperationsFactory(t, fake)

	err := runUpgradeStart(newUpgradeStartCommand(), []string{"v2.0.0"})

	var notConfigured *api.NotConfiguredError
	require.ErrorAs(t, err, &notConfigured)
	assert.Zero(t, fake.upgradeCalled, "no address must mean no upgrade call")
}

func TestRunUpgradeStatus_RoutesThroughSDKOperations(t *testing.T) {
	o := upgradeOutputFormat
	oid := upgradeOperationID
	t.Cleanup(func() {
		upgradeOutputFormat = o
		upgradeOperationID = oid
	})
	upgradeOutputFormat = "json"
	upgradeOperationID = "op-9"

	fake := &fakeOperationsClient{
		statusResp: &operationsSDK.GetUpgradeStatusResponse{
			JobID:          "op-9",
			Progress:       42.5,
			Status:         "in_progress",
			CurrentVersion: "v1.0.0",
			TargetVersion:  "v2.0.0",
		},
	}
	withOperationsFactory(t, fake)

	cmd := newUpgradeStatusCommand()
	// Set the operation ID AFTER constructing the command: the StringVar flag
	// binding writes its default ("") into upgradeOperationID at registration
	// time, which would otherwise clobber the value under test.
	upgradeOperationID = "op-9"
	err := runUpgradeStatus(cmd, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, fake.statusCalled)
	require.NotNil(t, fake.statusReq)
	assert.Equal(t, "op-9", fake.statusReq.JobID)
}

func TestRunUpgradeStatus_PropagatesSDKError(t *testing.T) {
	fake := &fakeOperationsClient{statusErr: stderrors.New("upgrade boom")}
	withOperationsFactory(t, fake)

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, 1, fake.statusCalled)
}

func TestUpgradeStatusRespFromSDK_MapsFields(t *testing.T) {
	out := upgradeStatusRespFromSDK(&operationsSDK.GetUpgradeStatusResponse{
		JobID:          "j1",
		Progress:       55.0,
		Status:         "completed",
		CurrentVersion: "v1",
		TargetVersion:  "v2",
	})
	assert.Equal(t, "j1", out.OperationId)
	assert.InDelta(t, 55.0, out.PercentComplete, 0.001)
	assert.Equal(t, "completed", out.Status)
	assert.Equal(t, "v1", out.CurrentVersion)
	assert.Equal(t, "v2", out.TargetVersion)
}

func TestUpgradeStartRespFromSDK_NilSafe(t *testing.T) {
	out := upgradeStartRespFromSDK(nil)
	require.NotNil(t, out)
	assert.Empty(t, out.OperationId)
}

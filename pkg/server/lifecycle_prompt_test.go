//go:build !integration

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

package server

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
)

func TestAttachPromptService_MountsOverTheStore(t *testing.T) {
	registry := prompts.NewRegistry()
	_, err := registry.Register(prompttest.Spec())
	require.NoError(t, err)
	store, err := prompts.NewStore(&memoryTenantResolver{stores: map[string]qrdbsdk.KVStore{}}, prompts.WithRegistry(registry))
	require.NoError(t, err)
	s := &Server{logger: newReconcileTestLogger(t), promptStore: store}
	svc := testutil.NewMockQuicBaaS()
	svc.On("Project").Return(nil)

	require.NoError(t, s.attachPromptService(svc))
	require.NotNil(t, svc.Prompts())

	// The mounted service answers from the node's store.
	got, err := svc.Prompts().List(context.Background(), &service.ListPromptsRequest{
		Scope: "system", Set: prompttest.Set,
	})
	require.NoError(t, err)
	require.Len(t, got.Prompts, len(prompttest.Spec().Prompts))
}

func TestAttachPromptService_NoStoreLeavesItUnmounted(t *testing.T) {
	s := &Server{logger: newReconcileTestLogger(t)}
	svc := testutil.NewMockQuicBaaS()

	require.NoError(t, s.attachPromptService(svc))
	require.Nil(t, svc.Prompts(), "a node without a prompt store must answer Unavailable")
}

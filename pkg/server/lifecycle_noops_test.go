//go:build !integration && !rag && !vector && !timeseries

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

// lifecycle_noops_test.go covers the no-op subsystem stubs that compile
// when their respective build tags are absent. The build constraint pins
// this file to the lean default build so the stub source is visible.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
)

func TestLifecycle_InitTimeSeriesStorage_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initTimeSeriesStorage())
}

func TestLifecycle_StartTimeSeries_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startTimeSeries())
}

func TestLifecycle_StopTimeSeries_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.stopTimeSeries())
}

func TestLifecycle_InitVectorStorage_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initVectorStorage())
}

func TestLifecycle_StartVector_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startVector())
}

func TestLifecycle_StartRAG_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startRAG())
}

func TestLifecycle_StopRAG_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.stopRAG())
}

func TestLifecycle_StartOllama_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startOllama())
}

func TestLifecycle_StopOllama_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.stopOllama())
}

func TestLifecycle_RegisterOllamaService_NoOp(t *testing.T) {
	srv := withTempServer(t)
	// Construct a no-op grpc server so the call is exercised even without
	// the rag tag.
	gs := ggrpc.NewServer()
	assert.NotPanics(t, func() { srv.registerOllamaService(gs) })
}

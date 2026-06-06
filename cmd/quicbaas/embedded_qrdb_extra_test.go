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

// Package main: extended coverage for embedded_qrdb.go — every nil and
// failure branch in buildEmbeddedQRDBServicers / embeddedQRDBOption that
// is not already exercised by the end-to-end test in embedded_qrdb_test.go.
package main

import (
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedQRDBWiringError_Wrap exercises the Unwrap chain explicitly.
// The non-extra test confirms Error() formatting; this one pins ErrorIs.
func TestEmbeddedQRDBWiringError_Wrap(t *testing.T) {
	inner := stderrors.New("io: blew up")
	err := &EmbeddedQRDBWiringError{Stage: "io", Cause: inner}
	assert.True(t, stderrors.Is(err, inner), "Unwrap chain must surface the inner cause")
}

// TestBuildEmbeddedQRDBServicers_NilNodeServices covers the "qrdb node
// services" branch by giving the embeddedServer a zero-value *server.Server
// (whose GetQRDBKVServicer() returns nil because qrdbNode has not been wired).
func TestBuildEmbeddedQRDBServicers_NilNodeServices(t *testing.T) {
	es := &embeddedServer{server: &server.Server{}}
	_, err := buildEmbeddedQRDBServicers(es)
	require.Error(t, err)
	var wireErr *EmbeddedQRDBWiringError
	require.True(t, stderrors.As(err, &wireErr))
	assert.Equal(t, "qrdb node services", wireErr.Stage)
	assert.Nil(t, wireErr.Cause, "node-services branch carries no inner cause")
}

// TestEmbeddedQRDBOption_NilNodeServices surfaces the node-services wiring
// failure through the option wrapper (Wrapf chain).
func TestEmbeddedQRDBOption_NilNodeServices(t *testing.T) {
	es := &embeddedServer{server: &server.Server{}}
	opt, err := embeddedQRDBOption(es)
	require.Error(t, err)
	assert.Nil(t, opt)
	// The Wrapf wrapper preserves the typed inner error in the chain.
	var wireErr *EmbeddedQRDBWiringError
	require.True(t, stderrors.As(err, &wireErr))
	assert.Equal(t, "qrdb node services", wireErr.Stage)
}

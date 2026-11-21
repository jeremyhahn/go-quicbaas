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

//go:build !integration

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNodeSlot_RefusesASecondHolder pins the slot's claim/release contract:
// one holder at a time, re-claim by the holder is fine, and only the holder
// can release it.
func TestNodeSlot_RefusesASecondHolder(t *testing.T) {
	slot := &nodeSlot{}
	a, b := &Server{}, &Server{}

	require.NoError(t, slot.claim(a))
	require.NoError(t, slot.claim(a), "the holder may claim again")

	var running *EmbeddedNodeRunningError
	require.True(t, errors.As(slot.claim(b), &running), "a second Server is refused")

	slot.release(b)
	require.True(t, errors.As(slot.claim(b), &running), "release by a non-holder leaves the holder in place")

	slot.release(a)
	require.NoError(t, slot.claim(b), "the slot is free once the holder releases it")

	unbounded := &nodeSlot{unbounded: true}
	require.NoError(t, unbounded.claim(a))
	require.NoError(t, unbounded.claim(b))
}

// TestInitStandaloneQRDBNode_RefusesASecondNodeInTheProcess proves that with
// a real slot in force a second Server cannot build an embedded node while
// the first runs, and can once the first stops.
func TestInitStandaloneQRDBNode_RefusesASecondNodeInTheProcess(t *testing.T) {
	prev := activeNodeSlot.Swap(&nodeSlot{})
	t.Cleanup(func() { activeNodeSlot.Store(prev) })

	srvA := withTempServer(t)
	require.NoError(t, srvA.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srvA.stopQRDBNode() })

	srvB := withTempServer(t)
	err := srvB.initStandaloneQRDBNode()
	var running *EmbeddedNodeRunningError
	require.True(t, errors.As(err, &running), "second node must be refused, got %v", err)
	require.Nil(t, srvB.getQRDBNode())

	require.NoError(t, srvA.stopQRDBNode())

	require.NoError(t, srvB.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srvB.stopQRDBNode() })
}

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

package sealedwindow

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransition_MarksInitializeAndUnsealOnly(t *testing.T) {
	assert.True(t, VerbBarrierUnseal.Transition())
	assert.True(t, VerbBarrierInitialize.Transition())
	assert.True(t, VerbShamirUnsealShare.Transition())
	assert.False(t, VerbBarrierStatus.Transition())
	assert.False(t, VerbHealth.Transition())
	assert.False(t, VerbOfficerRegister.Transition())
}

func TestRESTTransition_CoversTheUnsealRoutes(t *testing.T) {
	entries := RESTTransition()
	require.NotEmpty(t, entries)
	allow := make(map[string]struct{})
	for _, e := range RESTAllowlist() {
		allow[e] = struct{}{}
	}
	for _, e := range entries {
		_, ok := allow[e]
		assert.True(t, ok, "transition entry %q is not allowlisted", e)
	}
	for _, p := range Table[VerbBarrierUnseal].REST {
		method, path := http.MethodPost, p
		if m, rest, ok := strings.Cut(p, " "); ok {
			method, path = m, rest
		}
		assert.True(t, IsRESTTransition(method, path), "%s %s", method, path)
	}
	for _, p := range Table[VerbBarrierStatus].REST {
		_, path, ok := strings.Cut(p, " ")
		if !ok {
			path = p
		}
		assert.False(t, IsRESTTransition(http.MethodGet, path), path)
	}
}

func TestGRPCFullMethodTransition_IsAllowlistedUnsealAndInitialize(t *testing.T) {
	set := GRPCFullMethodTransition()
	require.NotEmpty(t, set)
	allow := make(map[string]struct{})
	for _, m := range GRPCFullMethodAllowlist() {
		allow[m] = struct{}{}
	}
	sawUnseal := false
	for m := range set {
		_, ok := allow[m]
		assert.True(t, ok, "transition method %q is not allowlisted", m)
		assert.NotContains(t, methodTail(m), "Status", m)
		if strings.HasSuffix(m, "Unseal") {
			sawUnseal = true
		}
	}
	assert.True(t, sawUnseal, "no unseal method in %v", set)
}

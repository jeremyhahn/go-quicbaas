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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestParseCLISandbox(t *testing.T) {
	cases := []struct {
		in    string
		want  orchestratorSDK.SandboxMode
		isErr bool
	}{
		{"", orchestratorSDK.SandboxModeDefault, false},
		{"default", orchestratorSDK.SandboxModeDefault, false},
		{"required", orchestratorSDK.SandboxModeRequired, false},
		{"none", orchestratorSDK.SandboxModeNone, false},
		{"bogus", orchestratorSDK.SandboxModeDefault, true},
	}
	for _, tc := range cases {
		got, err := parseCLISandbox(tc.in)
		if tc.isErr {
			require.Error(t, err, "expected error for input %q", tc.in)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}

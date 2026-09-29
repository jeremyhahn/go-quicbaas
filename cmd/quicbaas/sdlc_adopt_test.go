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
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCAdopt_ReachesTheClient(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	cmd := newSDLCAdoptCommand()
	require.NoError(t, cmd.ParseFlags([]string{"--pull-request", "194", "--ruling", "Land it."}))
	require.NoError(t, runSDLCAdopt(cmd, []string{"go-luks2", "141"}))
	require.NotNil(t, mc.adopted)
	assert.Equal(t, sdlcSDK.AdoptIssueRequest{TenantID: "tenant-a", ProjectID: "go-luks2", Issue: 141, PullRequest: 194, Ruling: "Land it."}, *mc.adopted)
}

func TestSDLCAdopt_RefusesABadIssueAndSurfacesClientFailures(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	newSDLCAdoptCommand()
	for _, arg := range []string{"0", "-1", "abc"} {
		err := runSDLCAdopt(nil, []string{"p", arg})
		require.Error(t, err, arg)
		assert.Contains(t, err.Error(), "issue", arg)
	}
	assert.Nil(t, mc.adopted, "a refused argument reached the client")

	mc.err = errors.New("the issue is closed")
	assert.Error(t, runSDLCAdopt(nil, []string{"p", "1"}))
}

func TestFormatSDLCAdoption(t *testing.T) {
	var out bytes.Buffer
	adoption := &sdlcSDK.Adoption{TaskID: "task-1", Issue: 141, PullRequest: 194, Branch: "issue/141-coverage-gate", Superseded: "task-0"}
	require.NoError(t, formatSDLCAdoption(adoption, "table", &out))
	assert.Contains(t, out.String(), "#194 on issue/141-coverage-gate")
	assert.Contains(t, out.String(), "task-0")
	out.Reset()
	require.NoError(t, formatSDLCAdoption(&sdlcSDK.Adoption{TaskID: "t", Issue: 1}, "json", &out))
	assert.Contains(t, out.String(), `"task_id": "t"`)
	assert.Error(t, formatSDLCAdoption(adoption, "nope", &out))
}

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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func (m *mockSDLCClient) FileIssue(_ context.Context, req *sdlcSDK.FileIssueRequest) (*sdlcSDK.ForgeIssue, error) {
	m.filed, m.tenant = req, req.TenantID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.ForgeIssue{Number: 88, Title: req.Title, Labels: req.Labels, URL: "https://forge.test/acme/api/issues/88"}, nil
}

func (m *mockSDLCClient) Collaborators(_ context.Context, tenantID, projectID string) (*sdlcSDK.CollaboratorsResponse, error) {
	m.tenant, m.got = tenantID, projectID
	if m.err != nil {
		return nil, m.err
	}
	return &sdlcSDK.CollaboratorsResponse{Users: []sdlcSDK.ForgeUser{{Login: "acme"}, {Login: "lead", FullName: "Lead"}}, Count: 2}, nil
}

func withIssueFlags(t *testing.T, flags sdlcIssueFlags) {
	t.Helper()
	orig := sdlcIssue
	sdlcIssue = flags
	t.Cleanup(func() { sdlcIssue = orig })
}

func TestSDLCIssueCreate_FilesThroughTheClient(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	body := filepath.Join(t.TempDir(), "body.md")
	require.NoError(t, os.WriteFile(body, []byte("From a file."), 0o600))
	withIssueFlags(t, sdlcIssueFlags{title: "Follow-up", bodyFile: body, labels: []string{"task:ready"}, parent: 12})

	require.NoError(t, runSDLCIssueCreate(nil, []string{"go-quicbaas"}))
	require.NotNil(t, mc.filed)
	assert.Equal(t, sdlcSDK.FileIssueRequest{
		TenantID: "tenant-a", ProjectID: "go-quicbaas", Title: "Follow-up", Body: "From a file.",
		Labels: []string{"task:ready"}, Parent: 12,
	}, *mc.filed)
}

func TestSDLCIssueCreate_RefusesBadFlagsBeforeTheClient(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	cases := map[string]struct {
		flags sdlcIssueFlags
		want  any
	}{
		"no title":          {sdlcIssueFlags{}, new(*IssueTitleRequiredError)},
		"a negative parent": {sdlcIssueFlags{title: "t", parent: -1}, new(*IssueParentError)},
		"two bodies":        {sdlcIssueFlags{title: "t", body: "b", bodyFile: "f"}, new(*IssueBodyConflictError)},
		"a missing file":    {sdlcIssueFlags{title: "t", bodyFile: filepath.Join(t.TempDir(), "absent")}, new(*IssueBodyFileError)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := issueFilingRequest("p", tc.flags)
			require.Error(t, err)
			assert.True(t, errors.As(err, tc.want), "%v", err)
		})
	}
	withIssueFlags(t, sdlcIssueFlags{})
	assert.Error(t, runSDLCIssueCreate(nil, []string{"p"}))
	assert.Nil(t, mc.filed, "a refused filing reached the client")

	mc.err = errors.New("labels: repository defines no label named nope")
	withIssueFlags(t, sdlcIssueFlags{title: "t", labels: []string{"nope"}})
	assert.Error(t, runSDLCIssueCreate(nil, []string{"p"}))
}

func TestSDLCCollaborators_ListsThroughTheClient(t *testing.T) {
	mc := &mockSDLCClient{}
	withSDLCMock(t, mc)
	require.NoError(t, runSDLCCollaborators(nil, []string{"go-quicbaas"}))
	assert.Equal(t, "go-quicbaas", mc.got)
	assert.Equal(t, "tenant-a", mc.tenant)

	mc.err = errors.New("binding not found")
	assert.Error(t, runSDLCCollaborators(nil, []string{"nope"}))
}

func TestFormatSDLCIssueAndCollaborators(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, formatSDLCIssue(&sdlcSDK.ForgeIssue{Number: 88, Title: "Follow-up", Labels: []string{"task:ready"}}, "table", &out))
	for _, want := range []string{"#88", "Follow-up", "task:ready"} {
		assert.Contains(t, out.String(), want)
	}
	out.Reset()
	require.NoError(t, formatSDLCIssue(&sdlcSDK.ForgeIssue{Number: 88}, "json", &out))
	assert.Contains(t, out.String(), `"number": 88`)
	assert.Error(t, formatSDLCIssue(&sdlcSDK.ForgeIssue{}, "nope", &out))

	out.Reset()
	require.NoError(t, formatSDLCCollaborators(&sdlcSDK.CollaboratorsResponse{Users: []sdlcSDK.ForgeUser{{Login: "lead", FullName: "Lead"}}}, "table", &out))
	assert.Contains(t, out.String(), "lead")
	out.Reset()
	require.NoError(t, formatSDLCCollaborators(&sdlcSDK.CollaboratorsResponse{Users: []sdlcSDK.ForgeUser{{Login: "lead"}}}, "yaml", &out))
	assert.Contains(t, out.String(), "login: lead")
	assert.Error(t, formatSDLCCollaborators(&sdlcSDK.CollaboratorsResponse{}, "nope", &out))
}

func TestRulersLabel_NamesTheOwnerAndTheBindingsRulers(t *testing.T) {
	label := rulersLabel(&sdlcSDK.ProjectBinding{Forge: sdlcSDK.ForgeConfig{Owner: "acme"}, Rulers: []string{"lead"}})
	assert.Equal(t, "acme (owner), lead + node rulers", label)
}

func TestSDLCCommand_HasTheOperatorVerbs(t *testing.T) {
	found := map[string][]string{}
	for _, sub := range newSDLCCommand().Commands() {
		name := strings.Fields(sub.Use)[0]
		for _, verb := range sub.Commands() {
			found[name] = append(found[name], strings.Fields(verb.Use)[0])
		}
		if _, ok := found[name]; !ok {
			found[name] = nil
		}
	}
	assert.Equal(t, []string{"create"}, found["issue"])
	_, ok := found["collaborators"]
	assert.True(t, ok, "sdlc collaborators is not registered")
}

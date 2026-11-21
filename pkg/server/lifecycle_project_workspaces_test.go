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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestWireProjectWorkspaces_RemoteXKMSKeepsSSHCloning covers issue #1909
// (Codex 7453 P1 11): with seal disabled and xkms.url naming a remote sealed
// xkmsd, the node has no local barrier, yet the managed-workspace git path
// must still get both a signer and a host-key pin store. A nil HostKeys makes
// project_git.go refuse every SSH clone.
func TestWireProjectWorkspaces_RemoteXKMSKeepsSSHCloning(t *testing.T) {
	s := newSSHStoreTestServer(t, t.TempDir(), config.XKMSConfig{URL: "https://xkmsd.example.internal:8443", Token: "t"})
	require.Nil(t, s.getQRDBNode(), "the remote-xkms node under test has no local barrier")

	client, err := s.buildSSHClient()
	require.NoError(t, err)
	require.NotNil(t, client, "a remote xkmsd serves SSH keys without a local barrier")
	t.Cleanup(func() { _ = client.Close() })

	svc := mocks.NewMockQuicBaaS()
	svc.MockSSHClient = client
	s.quicBaaSService = svc

	root := t.TempDir()
	workspaces, err := orchestration.NewWorkspaceManager(root)
	require.NoError(t, err)

	require.NoError(t, s.wireProjectWorkspaces(orchestration.NewInMemoryProjectStore(), workspaces))

	git := s.gitProvisioner
	require.NotNil(t, git)
	assert.NotNil(t, git.SSHSigner, "the remote xkmsd signs for SSH clones")
	assert.NotNil(t, git.HostKeys, "host-key pins must not depend on a local barrier")

	info, err := os.Stat(filepath.Join(workspaces.Root(), hostKeysDirName))
	require.NoError(t, err, "host-key pins persist under the projects root")
	assert.True(t, info.IsDir())
}

// TestWireProjectWorkspaces_NoSSHClient leaves SSH unconfigured: a node with
// neither a remote xkmsd nor a sealed local store gets no SSH auth, and
// project_git.go reports that instead of cloning.
func TestWireProjectWorkspaces_NoSSHClient(t *testing.T) {
	s := newSSHStoreTestServer(t, t.TempDir(), config.XKMSConfig{})
	s.quicBaaSService = mocks.NewMockQuicBaaS()

	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, s.wireProjectWorkspaces(orchestration.NewInMemoryProjectStore(), workspaces))
	require.NotNil(t, s.gitProvisioner)
	assert.Nil(t, s.gitProvisioner.SSHSigner)
	assert.Nil(t, s.gitProvisioner.HostKeys)
}

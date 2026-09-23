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
	"path/filepath"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// hostKeysDirName is the directory under the projects root holding the
// xkms host-key pin store used by git-over-SSH provisioning clones.
const hostKeysDirName = ".hostkeys"

// wireProjectWorkspaces composes the managed-workspace provisioner —
// WorkspaceManager (directory lifecycle) + GitProvisioner (clone with
// secrets-store HTTPS tokens or xkms-held SSH keys) — and binds it onto the
// facade's ProjectService, following the same post-construction rebind
// pattern the agent registry uses. projectStore must be the SAME store the
// ProjectService persists to, so background state transitions land on the
// records callers read.
func (s *Server) wireProjectWorkspaces(projectStore orchestration.ProjectStore, workspaces *orchestration.WorkspaceManager) error {
	projectsRoot := workspaces.Root()
	logAdapter := logger.NewAdapter(s.logger)
	git := &service.GitProvisioner{
		Logger: logAdapter,
	}
	if s.quicBaaSService != nil {
		git.Secrets = s.quicBaaSService.Secrets()

		// Compose git-over-SSH auth from xkms: the signer resolver keeps
		// private key material inside the xkms store, and the host-key pin
		// store persists first-contact pins under the projects root so later
		// clones verify against them.
		if sshClient := s.quicBaaSService.SSHClient(); sshClient != nil {
			hostKeyBackend, hkErr := xkmsssh.NewFileStorage(filepath.Join(projectsRoot, hostKeysDirName))
			if hkErr != nil {
				return hkErr
			}
			hostKeys, hkStoreErr := xkmsssh.NewHostKeyStore(hostKeyBackend)
			if hkStoreErr != nil {
				return hkStoreErr
			}
			git.SSHSigner = &service.XKMSSSHSignerResolver{Client: sshClient}
			git.HostKeys = hostKeys
		}
	}

	s.gitProvisioner = git

	provisioner, err := service.NewProjectWorkspaceProvisioner(service.ProjectWorkspaceProvisionerConfig{
		Workspaces: workspaces,
		Git:        git,
		Store:      projectStore,
		Logger:     logAdapter,
	})
	if err != nil {
		return err
	}

	if s.quicBaaSService == nil {
		return nil
	}
	wirer, ok := s.quicBaaSService.Project().(interface {
		SetWorkspaceProvisioner(*service.ProjectWorkspaceProvisioner)
	})
	if !ok {
		return nil
	}
	wirer.SetWorkspaceProvisioner(provisioner)
	s.logger.Infof("Managed project workspaces enabled (root: %s)", projectsRoot)
	return nil
}

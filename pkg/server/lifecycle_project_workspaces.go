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
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

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
		// private key material inside the xkms store, and first-contact
		// host-key pins persist beside the keys — on the remote xkmsd when
		// one holds them, otherwise in the barrier-sealed SSH keyspace — so
		// later clones verify against them. A local-key node with no barrier
		// gets no pin store, and SSH clones fail closed.
		if sshClient := s.quicBaaSService.SSHClient(); sshClient != nil {
			git.SSHSigner = &service.XKMSSSHSignerResolver{Client: sshClient}
			hostKeys, err := s.gitHostKeyStore(sshClient)
			var notSealed *SSHStoreNotSealedError
			switch {
			case errors.As(err, &notSealed):
				s.logger.WithError(err).Warn("git-over-SSH host-key pin store unavailable: SSH clones fail closed")
			case err != nil:
				return err
			default:
				git.HostKeys = hostKeys
			}
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

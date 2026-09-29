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

package service

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Project references — host affinity, git keys, git credential secret
// =============================================================================

// newProjectServiceWithReferences is a project service whose references
// resolve: fleet node agent-1 / worker / host-a and SSH key
// user/tenant-1/signing in tenant-1, and no secrets.
func newProjectServiceWithReferences(t *testing.T) *projectServiceImpl {
	t.Helper()
	svc := newTestProjectService().(*projectServiceImpl)
	svc.SetFleetNodes(newTestFleet(t))
	svc.SetSSHKeys(newFakeSSHKeys("user/tenant-1/signing"))
	svc.SetSecrets(&referenceSecrets{data: map[string][]byte{}})
	return svc
}

func TestProjectService_CreateProject_References(t *testing.T) {
	ctx := context.Background()
	base := func() CreateProjectRequest { return CreateProjectRequest{TenantID: "tenant-1", Name: "api"} }
	tests := []struct {
		name   string
		mutate func(*CreateProjectRequest)
		rig    func(*projectServiceImpl)
		field  string
		want   string
	}{
		{"host affinity by host", func(r *CreateProjectRequest) { r.HostAffinity = "host-a" }, nil, "", "ok"},
		{"host affinity by agent id", func(r *CreateProjectRequest) { r.HostAffinity = "agent-1" }, nil, "", "ok"},
		{"host affinity missing", func(r *CreateProjectRequest) { r.HostAffinity = "host-z" }, nil, "host_affinity", "not_found"},
		{"host affinity, fleet cannot answer", func(r *CreateProjectRequest) { r.HostAffinity = "host-a" },
			func(s *projectServiceImpl) { s.SetFleetNodes(failingFleet{}) }, "host_affinity", "unavailable"},
		{"host affinity, no fleet attached", func(r *CreateProjectRequest) { r.HostAffinity = "host-a" },
			func(s *projectServiceImpl) { s.SetFleetNodes(nil) }, "host_affinity", "unavailable"},
		{"signing key", func(r *CreateProjectRequest) { r.GitSigningEnabled, r.GitSigningKeyName = true, "signing" }, nil, "", "ok"},
		{"signing key missing", func(r *CreateProjectRequest) { r.GitSigningEnabled, r.GitSigningKeyName = true, "ghost" }, nil,
			"git_signing_key", "not_found"},
		{"signing key, key store cannot answer", func(r *CreateProjectRequest) { r.GitSigningEnabled, r.GitSigningKeyName = true, "signing" },
			func(s *projectServiceImpl) { s.SetSSHKeys(&fakeSSHKeys{err: errReferenceStoreSealed}) }, "git_signing_key", "unavailable"},
		{"ssh key", func(r *CreateProjectRequest) {
			r.GitRepoURL, r.GitSSHKeyName = "ssh://git@forge/acme/api.git", "signing"
		}, nil, "", "ok"},
		{"ssh key missing", func(r *CreateProjectRequest) { r.GitRepoURL, r.GitSSHKeyName = "ssh://git@forge/acme/api.git", "ghost" }, nil,
			"git_ssh_key", "not_found"},
		{"ssh key, no key store attached", func(r *CreateProjectRequest) {
			r.GitRepoURL, r.GitSSHKeyName = "ssh://git@forge/acme/api.git", "signing"
		},
			func(s *projectServiceImpl) { s.SetSSHKeys(nil) }, "git_ssh_key", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newProjectServiceWithReferences(t)
			if tt.rig != nil {
				tt.rig(svc)
			}
			req := base()
			tt.mutate(&req)
			_, err := svc.CreateProject(ctx, &req)
			switch tt.want {
			case "ok":
				if err != nil {
					t.Fatalf("CreateProject: %v", err)
				}
			case "not_found":
				requireReferenceNotFound(t, err, tt.field)
			case "unavailable":
				requireReferenceUnavailable(t, err, tt.field)
			}
		})
	}
}

// A git credential secret is resolved only for a managed clone, so the check
// is exercised through a managed workspace. Only the refusals are driven: an
// accepted create starts a real background clone.
func TestProjectService_CreateProject_GitCredentialSecretReference(t *testing.T) {
	managed := func(t *testing.T, secrets SecretsService) ProjectService {
		t.Helper()
		svc, _, _ := newManagedProjectService(t)
		svc.(*projectServiceImpl).SetSecrets(secrets)
		return svc
	}
	req := func() *CreateProjectRequest {
		return &CreateProjectRequest{
			TenantID: "tenant-1", Name: "api", WorkspaceManaged: true,
			GitRepoURL: "https://forge.example.test/acme/api.git", GitCredentialSecretRef: "ghost",
		}
	}

	t.Run("missing", func(t *testing.T) {
		_, err := managed(t, &referenceSecrets{data: map[string][]byte{}}).CreateProject(context.Background(), req())
		requireReferenceNotFound(t, err, "git_credential_secret_ref")
	})
	t.Run("secrets store cannot answer", func(t *testing.T) {
		_, err := managed(t, &referenceSecrets{err: errReferenceStoreSealed}).CreateProject(context.Background(), req())
		requireReferenceUnavailable(t, err, "git_credential_secret_ref")
	})
}

func TestProjectService_UpdateProject_References(t *testing.T) {
	ctx := context.Background()
	svc := newProjectServiceWithReferences(t)
	created, err := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", HostAffinity: "host-a",
		GitSigningEnabled: true, GitSigningKeyName: "signing",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id := created.Project.ID

	t.Run("host affinity changed to a missing node", func(t *testing.T) {
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, HostAffinity: "host-z"})
		requireReferenceNotFound(t, err, "host_affinity")
	})
	t.Run("signing key owner changed to one holding no such key", func(t *testing.T) {
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, GitSigningKeyOwner: "other"})
		requireReferenceNotFound(t, err, "git_signing_key")
	})
	t.Run("ssh key set to a missing key", func(t *testing.T) {
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, GitSSHKeyName: "ghost"})
		requireReferenceNotFound(t, err, "git_ssh_key")
	})
	t.Run("host affinity changed while the fleet cannot answer", func(t *testing.T) {
		svc.SetFleetNodes(failingFleet{})
		defer svc.SetFleetNodes(newTestFleet(t))
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, HostAffinity: "worker"})
		requireReferenceUnavailable(t, err, "host_affinity")
	})
	t.Run("host affinity changed to an existing node by name", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, HostAffinity: "worker"})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if resp.Project.HostAffinity != "worker" {
			t.Fatalf("HostAffinity = %q, want worker", resp.Project.HostAffinity)
		}
	})

	// Stored data is not re-validated: once the referenced objects are gone,
	// an update that does not touch them still saves.
	t.Run("stored references are not re-validated", func(t *testing.T) {
		svc.SetFleetNodes(orchestration.NewInMemoryFleetRegistry())
		svc.SetSSHKeys(newFakeSSHKeys())
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, Description: "touched"})
		if err != nil {
			t.Fatalf("an update touching no reference was refused: %v", err)
		}
		if resp.Project.Description != "touched" {
			t.Fatalf("Description = %q, want touched", resp.Project.Description)
		}
	})
}

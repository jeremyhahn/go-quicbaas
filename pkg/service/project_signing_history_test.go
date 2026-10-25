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
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// signingHistoryRig is a project service whose project signs with
// "signing-kms" and whose KMS resolves any key to resolver's signer.
func signingHistoryRig(t *testing.T, resolver *fakeSignerResolver) (ProjectService, string) {
	t.Helper()
	svc := NewProjectService(orchestration.NewInMemoryProjectStore()).(*projectServiceImpl)
	svc.provisioner = &ProjectWorkspaceProvisioner{git: &GitProvisioner{SSHSigner: resolver}}
	// The signing key a project names is verified on save.
	svc.SetSSHKeys(newFakeSSHKeys("user/tenant-1/signing-kms"))
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api",
		GitAuthorName: "agent", GitAuthorEmail: "agent@example.test",
		GitSigningEnabled: true, GitSigningKeyName: "signing-kms",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return svc, created.Project.ID
}

// TestProjectService_RetireSigningKey_RecordsThePublicHalfFromTheKMS: the
// named key is resolved in the project's tenant and its public half recorded
// with the retirement time asked for; a second retirement of the same key
// never moves its retirement later.
func TestProjectService_RetireSigningKey_RecordsThePublicHalfFromTheKMS(t *testing.T) {
	signer := ed25519TestSigner(t)
	resolver := &fakeSignerResolver{signer: signer}
	svc, id := signingHistoryRig(t, resolver)
	at := time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)

	resp, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id,
		GitSigningRetire: &GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "signing", Scope: "service"}, RetiredAt: at},
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if resolver.asked.Name != "signing" || resolver.asked.Owner != "tenant-1" {
		t.Fatalf("resolved %+v, want the key in the project's tenant", resolver.asked)
	}
	retired := resp.Project.Git.Signing.Retired
	want := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if len(retired) != 1 || retired[0].PublicKey != want || retired[0].Name != "signing" || !retired[0].RetiredAt.Equal(at) {
		t.Fatalf("retired = %+v, want the key's public half at %s", retired, at)
	}

	resp, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id,
		GitSigningRetire: &GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "signing"}},
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if got := resp.Project.Git.Signing.Retired; len(got) != 1 || !got[0].RetiredAt.Equal(at) {
		t.Fatalf("retired = %+v, want the earlier retirement kept", got)
	}
}

// TestProjectService_RetireSigningKey_DefaultsToNow: no time is now.
func TestProjectService_RetireSigningKey_DefaultsToNow(t *testing.T) {
	svc, id := signingHistoryRig(t, &fakeSignerResolver{signer: ed25519TestSigner(t)})
	before := time.Now().Add(-time.Second)
	resp, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id,
		GitSigningRetire: &GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "signing"}},
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if got := resp.Project.Git.Signing.Retired; len(got) != 1 || got[0].RetiredAt.Before(before) || got[0].RetiredAt.After(time.Now()) {
		t.Fatalf("retired = %+v, want retired now", got)
	}
}

// TestProjectService_RetireSigningKey_RefusesWhatItCannotRecord: a nameless
// key, the current key, a future retirement, a key the KMS cannot resolve and
// a server with no key resolver are all refused as invalid requests, and
// nothing is stored.
func TestProjectService_RetireSigningKey_RefusesWhatItCannotRecord(t *testing.T) {
	future := time.Now().Add(time.Hour)
	for name, tc := range map[string]struct {
		resolver   *fakeSignerResolver
		retirement GitSigningRetirement
		field      string
		retireErr  bool
	}{
		"no name":         {&fakeSignerResolver{}, GitSigningRetirement{}, "git_signing_retire_key_name", false},
		"the current key": {&fakeSignerResolver{}, GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "signing-kms"}}, "git_signing_retire_key_name", false},
		"in the future":   {&fakeSignerResolver{}, GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "old"}, RetiredAt: future}, "git_signing_retired_at", false},
		"unresolvable":    {&fakeSignerResolver{err: errors.New("key not found")}, GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "gone"}}, "git_signing_retire_key_name", true},
		"no signer":       {&fakeSignerResolver{}, GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "old"}}, "git_signing_retire_key_name", true},
	} {
		t.Run(name, func(t *testing.T) {
			svc, id := signingHistoryRig(t, tc.resolver)
			retirement := tc.retirement
			_, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
				TenantID: "tenant-1", ProjectID: id, GitSigningRetire: &retirement,
			})
			requireProjectInvalidField(t, err, tc.field)
			var retire *SigningKeyRetirementError
			if errors.As(err, &retire) != tc.retireErr {
				t.Fatalf("err = %v, want SigningKeyRetirementError = %v", err, tc.retireErr)
			}
			got, err := svc.GetProject(context.Background(), &GetProjectRequest{TenantID: "tenant-1", ProjectID: id})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Project.Git.Signing.Retired) != 0 {
				t.Fatalf("a refused retirement was stored: %+v", got.Project.Git.Signing.Retired)
			}
		})
	}
}

// TestProjectService_RetireSigningKey_NeedsAKeyResolver: a server that
// provisions no workspaces resolves no keys, and says so.
func TestProjectService_RetireSigningKey_NeedsAKeyResolver(t *testing.T) {
	svc := newTestProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{TenantID: "tenant-1", Name: "api"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID,
		GitSigningRetire: &GitSigningRetirement{Key: orchestration.KMSKeyRef{Name: "old"}},
	})
	var retire *SigningKeyRetirementError
	if !errors.As(err, &retire) || !strings.Contains(retire.Error(), "resolves no project keys") {
		t.Fatalf("err = %v, want the missing resolver named", err)
	}
}

// TestGitProvisioner_SigningPublicKey: the public half is rendered in
// authorized_keys form; a missing resolver is a typed refusal.
func TestGitProvisioner_SigningPublicKey(t *testing.T) {
	signer := ed25519TestSigner(t)
	got, err := (&GitProvisioner{SSHSigner: &fakeSignerResolver{signer: signer}}).
		SigningPublicKey(context.Background(), "tenant-1", orchestration.KMSKeyRef{Name: "k"})
	if err != nil || !strings.HasPrefix(got, "ssh-ed25519 ") || strings.HasSuffix(got, "\n") {
		t.Fatalf("SigningPublicKey = %q, %v", got, err)
	}
	var credErr *GitCredentialError
	if _, err := (&GitProvisioner{}).SigningPublicKey(context.Background(), "tenant-1", orchestration.KMSKeyRef{Name: "k"}); !errors.As(err, &credErr) {
		t.Fatalf("err = %v, want GitCredentialError", err)
	}
}

func TestParseGitSigningRetirement(t *testing.T) {
	got, err := ParseGitSigningRetirement("old", "service", "agent", "2026-09-24T18:15:00-04:00")
	if err != nil || got == nil || got.Key != (orchestration.KMSKeyRef{Name: "old", Scope: "service", Owner: "agent"}) ||
		!got.RetiredAt.Equal(time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)) {
		t.Fatalf("parsed %+v, %v", got, err)
	}
	if got, err := ParseGitSigningRetirement("old", "", "", ""); err != nil || got == nil || !got.RetiredAt.IsZero() {
		t.Fatalf("parsed %+v, %v; want a retirement at now", got, err)
	}
	if got, err := ParseGitSigningRetirement("", "", "", ""); got != nil || err != nil {
		t.Fatalf("parsed %+v, %v; want nothing asked", got, err)
	}
}

func TestParseGitSigningRetirement_RefusesAHalfOrUnreadableRequest(t *testing.T) {
	for name, tc := range map[string]struct{ name, scope, at, field string }{
		"time without key":  {"", "", "2026-09-24T22:15:00Z", "git_signing_retire_key_name"},
		"scope without key": {"", "tenant", "", "git_signing_retire_key_name"},
		"unreadable time":   {"old", "", "yesterday", "git_signing_retired_at"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseGitSigningRetirement(tc.name, tc.scope, "", tc.at)
			requireProjectInvalidField(t, err, tc.field)
		})
	}
}

// TestGitProvisioner_ProjectSigner_LooksUpUnderTheProjectTenant proves the
// signer lookup runs under the project's tenant even when the caller, like
// the SDLC runner, bound only the orchestration tenant: the SSH key store
// partitions keys by the context tenant, so any other tenant misses the key
// the project's tenant minted.
func TestGitProvisioner_ProjectSigner_LooksUpUnderTheProjectTenant(t *testing.T) {
	resolver := &fakeSignerResolver{signer: ed25519TestSigner(t)}
	p := &GitProvisioner{SSHSigner: resolver}
	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")

	signer, err := p.projectSigner(ctx, "tenant-a", orchestration.KMSKeyRef{Name: "agent-key", Scope: "service", Owner: "sdlc"})
	if err != nil {
		t.Fatalf("projectSigner: %v", err)
	}
	if signer == nil {
		t.Fatal("projectSigner returned no signer")
	}
	if resolver.askedTenant != "tenant-a" {
		t.Fatalf("lookup ran under tenant %q, want the project's tenant-a", resolver.askedTenant)
	}
	if resolver.asked.Owner != "sdlc" {
		t.Fatalf("lookup owner = %q, want the key's own owner", resolver.asked.Owner)
	}
}

// TestGitProvisioner_ProjectSigner_SurfacesTheResolverError proves a missing
// key reaches the caller as the resolver's error, looked up in the project's
// tenant, and a resolver answering no signer is refused.
func TestGitProvisioner_ProjectSigner_SurfacesTheResolverError(t *testing.T) {
	missing := &GitCredentialError{Ref: "agent-key", Reason: "key not found"}
	resolver := &fakeSignerResolver{err: missing}
	p := &GitProvisioner{SSHSigner: resolver}

	_, err := p.projectSigner(context.Background(), "tenant-b", orchestration.KMSKeyRef{Name: "agent-key"})
	if !errors.Is(err, missing) {
		t.Fatalf("err = %v, want the resolver's error", err)
	}
	if resolver.askedTenant != "tenant-b" || resolver.asked.Owner != "tenant-b" {
		t.Fatalf("lookup tenant %q owner %q, want tenant-b for both", resolver.askedTenant, resolver.asked.Owner)
	}

	_, err = (&GitProvisioner{SSHSigner: &fakeSignerResolver{}}).projectSigner(context.Background(), "tenant-b", orchestration.KMSKeyRef{Name: "agent-key"})
	var credErr *GitCredentialError
	if !errors.As(err, &credErr) {
		t.Fatalf("err = %v, want GitCredentialError for a resolver answering no signer", err)
	}
}

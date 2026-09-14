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

package orchestration

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Which credential a transport needs
// =============================================================================

// TestCheckoutCredentialFor_ReadsTheCredentialOffTheTransport: the URL decides
// it, and both spellings of an ssh remote are one answer. This is the fact the
// pre-spawn refusal rests on, so it is asserted per form rather than assumed.
func TestCheckoutCredentialFor_ReadsTheCredentialOffTheTransport(t *testing.T) {
	for name, tc := range map[string]struct {
		url  string
		want CheckoutCredential
	}{
		"ssh url":                {url: "ssh://git@forge.test:2222/acme/api.git", want: CheckoutCredentialSSHAgent},
		"ssh url, default port":  {url: "ssh://git@forge.test/acme/api.git", want: CheckoutCredentialSSHAgent},
		"scp form":               {url: "git@forge.test:acme/api.git", want: CheckoutCredentialSSHAgent},
		"https":                  {url: "https://forge.test/acme/api.git", want: CheckoutCredentialForgeToken},
		"http is not given one":  {url: "http://forge.test/acme/api.git", want: CheckoutCredentialNone},
		"a local path":           {url: "/srv/git/api.git", want: CheckoutCredentialNone},
		"empty":                  {url: "", want: CheckoutCredentialNone},
		"an unrecognised scheme": {url: "git://forge.test/acme/api.git", want: CheckoutCredentialNone},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CheckoutCredentialFor(tc.url); got != tc.want {
				t.Fatalf("CheckoutCredentialFor(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestCheckoutCredentialFor_NeverPutsTheTokenOnACleartextRemote is the same
// fact stated as the rule it is: the credential helper is configured for https
// only, so an http remote must answer "none" rather than "the token". A change
// that made http authenticated here would put a bearer token on the wire in
// the clear, and nothing downstream would notice.
func TestCheckoutCredentialFor_NeverPutsTheTokenOnACleartextRemote(t *testing.T) {
	if got := CheckoutCredentialFor("http://forge.test/acme/api.git"); got == CheckoutCredentialForgeToken {
		t.Fatal("an http remote was told to authenticate with the forge token")
	}
}

// TestCheckoutCredential_String renders every credential, including a value
// outside the set, because these strings reach an operator inside an error.
func TestCheckoutCredential_String(t *testing.T) {
	for credential, want := range map[CheckoutCredential]string{
		CheckoutCredentialNone:       "none",
		CheckoutCredentialForgeToken: "forge token",
		CheckoutCredentialSSHAgent:   "ssh agent",
		CheckoutCredential(200):      "unknown",
	} {
		if got := credential.String(); got != want {
			t.Errorf("CheckoutCredential(%d).String() = %q, want %q", credential, got, want)
		}
	}
}

// =============================================================================
// Whether the spec needs an agent at all
// =============================================================================

// TestWorkspaceSpec_NeedsSSHAgent_CountsEveryRepositoryTheContainerClones: the
// context repositories are cloned by the same container with the same
// credentials, so one on an ssh remote needs the agent exactly as the project
// would. A rule that read the project alone would leave the container to
// discover it inside the clone.
func TestWorkspaceSpec_NeedsSSHAgent_CountsEveryRepositoryTheContainerClones(t *testing.T) {
	for name, tc := range map[string]struct {
		spec WorkspaceSpec
		want bool
	}{
		"https only": {
			spec: WorkspaceSpec{
				Project:      WorkspaceRepoSpec{Name: "api", RepoURL: "https://forge.test/acme/api.git"},
				ContextRepos: []WorkspaceRepoSpec{{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git"}},
			},
			want: false,
		},
		"the project is ssh": {
			spec: WorkspaceSpec{Project: WorkspaceRepoSpec{Name: "api", RepoURL: "ssh://git@forge.test/acme/api.git"}},
			want: true,
		},
		"only a context repository is ssh": {
			spec: WorkspaceSpec{
				Project:      WorkspaceRepoSpec{Name: "api", RepoURL: "https://forge.test/acme/api.git"},
				ContextRepos: []WorkspaceRepoSpec{{Name: "handbook", RepoURL: "git@forge.test:acme/handbook.git"}},
			},
			want: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.spec.NeedsSSHAgent(); got != tc.want {
				t.Fatalf("NeedsSSHAgent() = %v, want %v", got, tc.want)
			}
		})
	}
}

// =============================================================================
// The pre-spawn refusal
// =============================================================================

// TestRequireCheckoutCredentials_AcceptsASpawnThatHoldsWhatEveryRemoteNeeds is
// the good case, per transport: an ssh remote with an agent socket, an https
// remote with a token, and a public http remote with neither.
func TestRequireCheckoutCredentials_AcceptsASpawnThatHoldsWhatEveryRemoteNeeds(t *testing.T) {
	spec := WorkspaceSpec{
		Project: WorkspaceRepoSpec{Name: "api", RepoURL: "ssh://git@forge.test:2222/acme/api.git"},
		ContextRepos: []WorkspaceRepoSpec{
			{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git"},
			{Name: "public", RepoURL: "http://forge.test/acme/public.git"},
		},
	}
	creds := WorkspaceCredentials{ForgeToken: true, SSHAgentSocket: "/run/quicbaas/sockets/ab/agent.sock"}
	if err := RequireCheckoutCredentials(spec, creds); err != nil {
		t.Fatalf("RequireCheckoutCredentials: %v", err)
	}
}

// TestRequireCheckoutCredentials_RefusesAnSSHRemoteWithNoAgent is the defect
// go-quicbaas#929 was, decided where it was always decidable: on the daemon,
// from the spec, before a container is started. The triage runner's spawn
// carried a forge token and no agent socket while the project's remote was
// ssh, and it spent one container per sweep finding that out.
func TestRequireCheckoutCredentials_RefusesAnSSHRemoteWithNoAgent(t *testing.T) {
	spec := WorkspaceSpec{Project: WorkspaceRepoSpec{Name: "api", RepoURL: "ssh://git@forge.test:2222/acme/api.git"}}
	err := RequireCheckoutCredentials(spec, WorkspaceCredentials{ForgeToken: true})
	if !errors.Is(err, ErrCheckoutCredentialMissing) {
		t.Fatalf("err = %v, want ErrCheckoutCredentialMissing", err)
	}
	var missing *CheckoutCredentialMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("want *CheckoutCredentialMissingError, got %T", err)
	}
	if missing.Repo != "api" || missing.Need != CheckoutCredentialSSHAgent {
		t.Fatalf("missing = %+v, want the project named and the ssh agent wanted", missing)
	}
	// The message has to name the repository and the remote: an operator
	// reading the job is fixing a configuration, not a stack trace.
	for _, want := range []string{"api", "ssh://git@forge.test:2222/acme/api.git", "ssh agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
	// A blank socket path is the same absence as no field at all: the seam
	// fills it in from a session, and a session that answered with nothing
	// must not read as a credential.
	blank := RequireCheckoutCredentials(spec, WorkspaceCredentials{SSHAgentSocket: "   "})
	if !errors.Is(blank, ErrCheckoutCredentialMissing) {
		t.Fatalf("a whitespace socket path was accepted as a credential: %v", blank)
	}
}

// TestRequireCheckoutCredentials_RefusesAnHTTPSRemoteWithNoToken: the other
// half of the same rule. A token that could not be resolved leaves the
// credential helper with nothing to answer with, and git would ask for a
// password on a terminal no container has.
func TestRequireCheckoutCredentials_RefusesAnHTTPSRemoteWithNoToken(t *testing.T) {
	spec := WorkspaceSpec{Project: WorkspaceRepoSpec{Name: "api", RepoURL: "https://forge.test/acme/api.git"}}
	err := RequireCheckoutCredentials(spec, WorkspaceCredentials{SSHAgentSocket: "/run/agent.sock"})
	var missing *CheckoutCredentialMissingError
	if !errors.As(err, &missing) || missing.Need != CheckoutCredentialForgeToken {
		t.Fatalf("err = %v, want the forge token named as missing", err)
	}
}

// TestRequireCheckoutCredentials_RefusesAContextRepositoryItCouldNotClone: a
// context repository is as fatal as the project — the worker was told to read
// it — and the refusal names it rather than the project, so the operator fixes
// the right line of the binding.
func TestRequireCheckoutCredentials_RefusesAContextRepositoryItCouldNotClone(t *testing.T) {
	spec := WorkspaceSpec{
		Project:      WorkspaceRepoSpec{Name: "api", RepoURL: "https://forge.test/acme/api.git"},
		ContextRepos: []WorkspaceRepoSpec{{Name: "handbook", RepoURL: "git@forge.test:acme/handbook.git"}},
	}
	err := RequireCheckoutCredentials(spec, WorkspaceCredentials{ForgeToken: true})
	var missing *CheckoutCredentialMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want *CheckoutCredentialMissingError", err)
	}
	if missing.Repo != "handbook" {
		t.Fatalf("refused %q, want the context repository named", missing.Repo)
	}
}
